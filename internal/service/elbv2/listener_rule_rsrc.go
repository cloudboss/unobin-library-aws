package elbv2

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// defaultRulePriority is the priority ELBv2 reports for a listener's default
// rule, read back as the literal string "default". It is not a value a user can
// set: the resource manages non-default rules whose priority is 1..50000, so a
// rule that reads back as the default sentinel leaves its output priority unset
// rather than reporting this placeholder as a chosen value.
const defaultRulePriority = "default"

// rulePropagationTimeout bounds the create-time retries: the wait for a
// just-created rule to become readable, and the recompute-and-retry when an
// auto-assigned priority loses a race to another rule for the next free slot.
// Both clear within seconds in practice; five minutes is the upper bound the
// AWS console and other tools allow for the same races.
const rulePropagationTimeout = 5 * time.Minute

// ListenerRule is one routing rule on an Application Load Balancer listener: an
// ordered set of conditions that match a request and the actions to take when
// they do. The fields mirror the ELBv2 CreateRule API, which an update
// reconciles through ModifyRule, with priority reconciled by the separate
// SetRulePriorities call.
//
// A rule belongs to one listener for its lifetime, so a change to the listener
// ARN replaces the rule; everything else changes in place. The priority orders
// the rule against the listener's other rules and may change without replacing
// the rule. When the priority is omitted, the resource assigns the next free
// slot above the listener's highest non-default rule, retrying if another rule
// claims that slot first.
//
// The cross-field rules on actions and conditions are all declared as
// constraints: each action's type fixes which sub-block it takes, each
// condition sets exactly one matcher with non-empty values, a forward block
// names one to five weighted target groups, and every query-string pair sets
// a value.
type ListenerRule struct {
	ListenerArn string                  `ub:"listener-arn"`
	Priority    *int64                  `ub:"priority"`
	Actions     []ListenerRuleAction    `ub:"actions"`
	Conditions  []ListenerRuleCondition `ub:"conditions"`
	Tags        map[string]string       `ub:"tags"`
}

// ListenerRuleOutput holds the value ELBv2 computes for a rule. The ARN is the
// rule's stable handle and CloudFormation primary identifier, returned by
// CreateRule and read back by DescribeRules.
type ListenerRuleOutput struct {
	Arn string `ub:"arn"`
}

func (r *ListenerRule) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs ELBv2 cannot change on an existing rule. A
// rule is created on one listener and cannot be moved, so changing the listener
// ARN requires a new rule. The priority is deliberately not listed: it is
// modifiable in place through SetRulePriorities.
func (r *ListenerRule) ReplaceFields() []string {
	return []string{"listener-arn"}
}

// Defaults marks the collection inputs a rule may omit.
func (r ListenerRule) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules ELBv2 places on a rule's inputs: a priority
// in 1..50000 when given (the default-rule sentinel 99999 is a read-back
// value, never a user input), at least one action and one condition, each
// action's type fixing which sub-block it takes along with the redirect and
// fixed-response enums, the forward target-group and stickiness rules, and
// each condition setting exactly one matcher with non-empty values, a
// query-string matcher's pairs each setting a value.
func (r ListenerRule) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.Priority)).
			Require(constraint.AtLeast(r.Priority, 1), constraint.AtMost(r.Priority, 50000)).
			Message("priority must be between 1 and 50000"),
		constraint.Must(constraint.NotEmpty(r.Actions)).
			Message("a rule requires at least one action"),
		constraint.Must(constraint.NotEmpty(r.Conditions)).
			Message("a rule requires at least one condition"),
		constraint.ForEach(r.Actions,
			func(a ListenerRuleAction) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(a.Type,
						"forward", "redirect", "fixed-response")).
						Message("an action type must be forward, redirect, or fixed-response"),
					constraint.When(constraint.Equals(a.Type, "forward")).
						Require(constraint.Any(
							constraint.All(constraint.Present(a.TargetGroupArn),
								constraint.Absent(a.Forward)),
							constraint.All(constraint.Absent(a.TargetGroupArn),
								constraint.Present(a.Forward))),
							constraint.Absent(a.Redirect), constraint.Absent(a.FixedResponse)).
						Message("a forward action takes exactly one of target-group-arn or forward"),
					constraint.When(constraint.Equals(a.Type, "redirect")).
						Require(constraint.Present(a.Redirect),
							constraint.Absent(a.TargetGroupArn), constraint.Absent(a.Forward),
							constraint.Absent(a.FixedResponse)).
						Message("a redirect action takes a redirect block only"),
					constraint.When(constraint.Equals(a.Type, "fixed-response")).
						Require(constraint.Present(a.FixedResponse),
							constraint.Absent(a.TargetGroupArn), constraint.Absent(a.Forward),
							constraint.Absent(a.Redirect)).
						Message("a fixed-response action takes a fixed-response block only"),
					constraint.When(constraint.Present(a.Order)).
						Require(constraint.AtLeast(a.Order, 1),
							constraint.AtMost(a.Order, 50000)).
						Message("an action order must be between 1 and 50000"),
					constraint.When(constraint.Present(a.Redirect.StatusCode)).
						Require(constraint.OneOf(a.Redirect.StatusCode,
							"HTTP_301", "HTTP_302")).
						Message("a redirect status-code must be HTTP_301 or HTTP_302"),
					constraint.When(constraint.Present(a.Redirect.Protocol)).
						Require(constraint.OneOf(a.Redirect.Protocol,
							"#{protocol}", "HTTP", "HTTPS")).
						Message("a redirect protocol must be HTTP, HTTPS, or #{protocol}"),
					constraint.When(constraint.Present(a.FixedResponse.ContentType)).
						Require(constraint.OneOf(a.FixedResponse.ContentType, "text/plain",
							"text/css", "text/html", "application/javascript",
							"application/json")).
						Message("a fixed-response content-type must be one of the accepted types"),
					constraint.When(constraint.Present(a.Forward)).
						Require(constraint.NotEmpty(a.Forward.TargetGroups),
							constraint.MaxItems(a.Forward.TargetGroups, 5)).
						Message("a forward block takes one to five target-groups"),
					constraint.ForEach(a.Forward.TargetGroups,
						func(g ListenerRuleForwardTargetGroup) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.Present(g.Arn)).
									Message("a forward target-group requires an arn"),
								constraint.When(constraint.Present(g.Weight)).
									Require(constraint.AtLeast(g.Weight, 0),
										constraint.AtMost(g.Weight, 999)).
									Message("a target group weight must be between 0 and 999"),
							}
						}),
					constraint.When(constraint.IsTrue(a.Forward.Stickiness.Enabled)).
						Require(constraint.Present(a.Forward.Stickiness.DurationSeconds)).
						Message("enabled forward stickiness requires duration-seconds"),
					constraint.When(constraint.Present(a.Forward.Stickiness.DurationSeconds)).
						Require(constraint.AtLeast(a.Forward.Stickiness.DurationSeconds, 1),
							constraint.AtMost(a.Forward.Stickiness.DurationSeconds, 604800)).
						Message("stickiness duration-seconds must be between 1 and 604800"),
				}
			}),
		constraint.ForEach(r.Conditions,
			func(c ListenerRuleCondition) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.AtMostOneOf(c.HostHeader, c.HttpHeader, c.HttpRequestMethod,
						c.PathPattern, c.QueryString, c.SourceIp),
					constraint.Must(constraint.Any(constraint.Present(c.HostHeader),
						constraint.Present(c.HttpHeader),
						constraint.Present(c.HttpRequestMethod),
						constraint.Present(c.PathPattern), constraint.Present(c.QueryString),
						constraint.Present(c.SourceIp))).
						Message("a condition requires exactly one matcher"),
					constraint.When(constraint.Present(c.HostHeader)).
						Require(constraint.NotEmpty(c.HostHeader.Values)).
						Message("host-header requires values"),
					constraint.When(constraint.Present(c.HttpHeader)).
						Require(constraint.NotEmpty(c.HttpHeader.Values)).
						Message("http-header requires values"),
					constraint.When(constraint.Present(c.HttpRequestMethod)).
						Require(constraint.NotEmpty(c.HttpRequestMethod.Values)).
						Message("http-request-method requires values"),
					constraint.When(constraint.Present(c.PathPattern)).
						Require(constraint.NotEmpty(c.PathPattern.Values)).
						Message("path-pattern requires values"),
					constraint.When(constraint.Present(c.QueryString)).
						Require(constraint.NotEmpty(c.QueryString.Values)).
						Message("query-string requires values"),
					constraint.When(constraint.Present(c.SourceIp)).
						Require(constraint.NotEmpty(c.SourceIp.Values)).
						Message("source-ip requires values"),
					constraint.ForEach(c.QueryString.Values,
						func(p ListenerRuleQueryStringPair) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.Present(p.Value)).
									Message("a query-string pair requires a value"),
							}
						}),
				}
			}),
	}
}

func (r *ListenerRule) Create(ctx context.Context, cfg any) (*ListenerRuleOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	actions := r.actions()
	conditions := r.conditions()
	arn, err := r.create(ctx, client, actions, conditions, tagList(r.Tags))
	// Some partitions, such as the ISO partitions, cannot tag a rule as it is
	// created. When the tagged create fails for that reason, create the rule
	// without tags and apply them with a separate call below.
	if err != nil && len(r.Tags) > 0 && partition.UnsupportedOperation(region(client), err) {
		arn, err = r.create(ctx, client, actions, conditions, nil)
		if err == nil && len(r.Tags) > 0 {
			err = syncTags(ctx, client, arn, r.Tags)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("create rule: %w", err)
	}
	// CreateRule returns before the rule is consistently readable, so route the
	// output through a Read that tolerates a brief not-found while the create
	// propagates.
	if err := r.waitVisible(ctx, client, arn); err != nil {
		return nil, err
	}
	return r.read(ctx, client, arn)
}

func (r *ListenerRule) Read(
	ctx context.Context, cfg any, prior *ListenerRuleOutput,
) (*ListenerRuleOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Arn)
}

func (r *ListenerRule) Update(
	ctx context.Context, cfg any, prior runtime.Prior[ListenerRule, *ListenerRuleOutput],
) (*ListenerRuleOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	// The priority is reconciled by SetRulePriorities, a separate call from
	// ModifyRule, so it runs on its own only when the priority changed.
	if runtime.Changed(prior.Inputs.Priority, r.Priority) && r.Priority != nil {
		_, err := client.SetRulePriorities(ctx, &elbv2.SetRulePrioritiesInput{
			RulePriorities: []elbv2types.RulePriorityPair{
				{RuleArn: aws.String(arn), Priority: ptr.Int32(r.Priority)},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("set rule priorities: %w", err)
		}
	}
	// ModifyRule replaces the actions and conditions it is given, so set each
	// only when it changed and call ModifyRule only when at least one did. A
	// call with neither would reset nothing but still costs a request, and a
	// blind replace would reset whichever the config omits.
	in := &elbv2.ModifyRuleInput{RuleArn: aws.String(arn)}
	modify := false
	if runtime.Changed(prior.Inputs.Actions, r.Actions) {
		in.Actions = r.actions()
		modify = true
	}
	if runtime.Changed(prior.Inputs.Conditions, r.Conditions) {
		in.Conditions = r.conditions()
		modify = true
	}
	if modify {
		if _, err := client.ModifyRule(ctx, in); err != nil {
			return nil, fmt.Errorf("modify rule: %w", err)
		}
	}
	// ModifyRule does not touch a rule's tags, so reconcile them through the tag
	// API as a set whenever they changed.
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, arn, r.Tags); err != nil {
			return nil, err
		}
	}
	// The ARN is fixed when the rule is created and an update never changes it,
	// so the prior output still describes the rule.
	return prior.Outputs, nil
}

func (r *ListenerRule) Delete(ctx context.Context, cfg any, prior *ListenerRuleOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteRule(ctx, &elbv2.DeleteRuleInput{RuleArn: aws.String(prior.Arn)})
	if err != nil {
		// A rule already gone is a successful delete, not an error.
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete rule: %w", err)
	}
	return nil
}

// create issues CreateRule for the rule, choosing a priority. When the priority
// is given, it is sent once. When it is omitted, the priority is computed as one
// above the listener's highest non-default rule and the create is retried on a
// PriorityInUse conflict, recomputing each time, since another rule may claim
// that slot first. It returns the new rule's ARN.
func (r *ListenerRule) create(
	ctx context.Context, client *elbv2.Client,
	actions []elbv2types.Action, conditions []elbv2types.RuleCondition,
	tags []elbv2types.Tag,
) (string, error) {
	in := &elbv2.CreateRuleInput{
		ListenerArn: aws.String(r.ListenerArn),
		Actions:     actions,
		Conditions:  conditions,
		Tags:        tags,
	}
	if r.Priority != nil {
		in.Priority = ptr.Int32(r.Priority)
		resp, err := client.CreateRule(ctx, in)
		if err != nil {
			return "", err
		}
		return aws.ToString(resp.Rules[0].RuleArn), nil
	}
	var arn string
	err := retry.OnError(ctx, isPriorityInUse, func(ctx context.Context) error {
		priority, err := r.nextPriority(ctx, client)
		if err != nil {
			return err
		}
		in.Priority = aws.Int32(priority)
		resp, err := client.CreateRule(ctx, in)
		if err != nil {
			return err
		}
		arn = aws.ToString(resp.Rules[0].RuleArn)
		return nil
	}, retry.WithTimeout(rulePropagationTimeout), retry.WithInterval(time.Second))
	if err != nil {
		return "", err
	}
	return arn, nil
}

// nextPriority returns one above the highest non-default priority among the
// listener's rules, or 1 when the listener has no non-default rules. ELBv2
// reports the default rule's priority as the literal "default", which is skipped
// here.
func (r *ListenerRule) nextPriority(ctx context.Context, client *elbv2.Client) (int32, error) {
	var highest int32
	paginator := elbv2.NewDescribeRulesPaginator(client,
		&elbv2.DescribeRulesInput{ListenerArn: aws.String(r.ListenerArn)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return 0, fmt.Errorf("describe rules: %w", err)
		}
		for _, rule := range page.Rules {
			priority := aws.ToString(rule.Priority)
			if priority == defaultRulePriority {
				continue
			}
			var n int32
			if _, err := fmt.Sscanf(priority, "%d", &n); err != nil {
				continue
			}
			if n > highest {
				highest = n
			}
		}
	}
	return highest + 1, nil
}

// waitVisible polls DescribeRules until the just-created rule is found, since
// CreateRule returns before the rule is consistently readable. A not-found read
// means the create is still propagating, so the wait keeps polling; any other
// error stops it.
func (r *ListenerRule) waitVisible(ctx context.Context, client *elbv2.Client, arn string) error {
	return wait.Until(ctx, fmt.Sprintf("rule %s", arn),
		func(ctx context.Context) (bool, error) {
			_, err := client.DescribeRules(ctx,
				&elbv2.DescribeRulesInput{RuleArns: []string{arn}})
			if err != nil {
				if isNotFound(err) {
					return false, nil
				}
				return false, fmt.Errorf("describe rules: %w", err)
			}
			return true, nil
		}, wait.WithTimeout(rulePropagationTimeout))
}

// read fetches the rule by ARN and returns its computed output. A rule that has
// gone missing is drift, which DescribeRules reports as RuleNotFound and read
// turns into runtime.ErrNotFound so the runtime recreates it.
func (r *ListenerRule) read(
	ctx context.Context, client *elbv2.Client, arn string,
) (*ListenerRuleOutput, error) {
	resp, err := client.DescribeRules(ctx,
		&elbv2.DescribeRulesInput{RuleArns: []string{arn}})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe rules: %w", err)
	}
	if len(resp.Rules) == 0 {
		return nil, runtime.ErrNotFound
	}
	return &ListenerRuleOutput{Arn: aws.ToString(resp.Rules[0].RuleArn)}, nil
}

// actions expands the rule's actions into the SDK type. When an action omits
// its order, it is given a 1-based index so the order is stable across applies
// and matches the order ELBv2 applies them in.
func (r *ListenerRule) actions() []elbv2types.Action {
	out := make([]elbv2types.Action, 0, len(r.Actions))
	for i := range r.Actions {
		out = append(out, r.Actions[i].to(int32(i+1)))
	}
	return out
}

// conditions expands the rule's conditions into the SDK type.
func (r *ListenerRule) conditions() []elbv2types.RuleCondition {
	out := make([]elbv2types.RuleCondition, 0, len(r.Conditions))
	for i := range r.Conditions {
		out = append(out, r.Conditions[i].to())
	}
	return out
}
