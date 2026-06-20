package eventbridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eventbridge "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// defaultEventBusName is the bus EventBridge uses when a rule names no bus. The
// API treats an omitted bus and the literal "default" the same, so the resource
// keeps EventBusName out of every request when it is unset or "default" and only
// passes a bus name through when the user chose a non-default one.
const defaultEventBusName = "default"

// Rule is an EventBridge rule: a named match, on either an event pattern or a
// schedule, against an event bus. The fields mirror the EventBridge PutRule
// API, which is also the call an update makes. The rule name and event bus fix
// the rule's identity and ARN, so a change to either replaces the rule; the
// description, event pattern, schedule expression, role, state, and tags all
// change in place.
//
// EventBridge enforces these bounds itself, so they are not expressed as
// constraints: the name is at most 64 characters matching ^[0-9A-Za-z_.-]+$,
// the description is at most 512 characters, the schedule expression is at most
// 256 characters, and the event pattern is valid JSON of at most 4096
// characters.
type Rule struct {
	Name               string            `ub:"name"`
	EventBusName       *string           `ub:"event-bus-name"`
	Description        *string           `ub:"description"`
	EventPattern       *string           `ub:"event-pattern"`
	ScheduleExpression *string           `ub:"schedule-expression"`
	RoleArn            *string           `ub:"role-arn"`
	State              *string           `ub:"state"`
	Tags               map[string]string `ub:"tags"`
	ForceDestroy       *bool             `ub:"force-destroy"`
}

// RuleOutput holds the value EventBridge computes for a rule. The ARN is the
// rule's stable handle and CloudFormation primary identifier, read from
// DescribeRule once the rule is visible.
type RuleOutput struct {
	Arn string `ub:"arn"`
}

func (r *Rule) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EventBridge cannot change on an existing rule.
// The name is baked into the rule's ARN at creation, and a rule belongs to one
// event bus for its lifetime, so changing either requires a new rule. Every
// other input is reconciled in place by Update.
func (r *Rule) ReplaceFields() []string {
	return []string{
		"name",
		"event-bus-name",
	}
}

// Defaults marks the collection inputs a rule may omit.
func (r Rule) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the cross-field rules EventBridge places on a rule's
// inputs. A rule matches on an event pattern or a schedule, so at least one of
// the two must be set, though both may be. When state is set it must be one of
// the three valid rule states; an unset state lets EventBridge apply its own
// default of ENABLED.
func (r Rule) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtLeastOneOf(r.EventPattern, r.ScheduleExpression),
		constraint.When(constraint.Present(r.State)).
			Require(constraint.OneOf(r.State,
				"ENABLED", "DISABLED", "ENABLED_WITH_ALL_CLOUDTRAIL_MANAGEMENT_EVENTS")).
			Message("state must be ENABLED, DISABLED, or ENABLED_WITH_ALL_CLOUDTRAIL_MANAGEMENT_EVENTS"),
	}
}

func (r *Rule) Create(ctx context.Context, cfg *awsCfg) (*RuleOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := r.putRuleInput()
	// Some partitions, such as the ISO partitions, cannot tag a rule as it is
	// created. When the tagged create fails for that reason, create the rule
	// without tags and apply them with a separate call below.
	taggedSeparately := false
	err = r.putRule(ctx, client, in)
	if err != nil && in.Tags != nil &&
		partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		err = r.putRule(ctx, client, in)
	}
	if err != nil {
		return nil, fmt.Errorf("put rule: %w", err)
	}
	// PutRule returns before the rule is consistently visible, so wait for a
	// DescribeRule to find it before reading its settled ARN for the output.
	if err := r.waitVisible(ctx, client); err != nil {
		return nil, err
	}
	if taggedSeparately && len(r.Tags) > 0 {
		if err := r.syncTags(ctx, client); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

func (r *Rule) Read(ctx context.Context, cfg *awsCfg, prior *RuleOutput) (*RuleOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read fetches the rule and returns its computed output. A rule that has gone
// missing is drift, which DescribeRule reports as ResourceNotFound and read
// turns into runtime.ErrNotFound so the runtime recreates it.
func (r *Rule) read(ctx context.Context, client *eventbridge.Client) (*RuleOutput, error) {
	resp, err := client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{
		Name:         aws.String(r.Name),
		EventBusName: r.eventBusName(),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe rule: %w", err)
	}
	return &RuleOutput{Arn: aws.ToString(resp.Arn)}, nil
}

func (r *Rule) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Rule, *RuleOutput],
) (*RuleOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// PutRule replaces the whole rule definition apart from its tags, so it runs
	// only when one of those fields changed. A tag-only change is left to the tag
	// reconcile below rather than replaying the definition, which would reset any
	// field the config omits.
	if r.putRuleChanged(prior.Inputs) {
		in := r.putRuleInput()
		err = r.putRule(ctx, client, in)
		// Some partitions cannot tag a rule through PutRule. When the tagged call
		// fails for that reason, reissue it without tags; the tag reconcile below
		// applies them.
		if err != nil && in.Tags != nil &&
			partition.UnsupportedOperation(region(client), err) {
			in.Tags = nil
			err = r.putRule(ctx, client, in)
		}
		if err != nil {
			return nil, fmt.Errorf("put rule: %w", err)
		}
	}
	// PutRule does not update an existing rule's tags, so reconcile them through
	// the tag API as a set whenever they changed.
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client); err != nil {
			return nil, err
		}
	}
	// The ARN is fixed when the rule is created and an update never changes it,
	// so the prior output still describes the rule.
	return prior.Outputs, nil
}

func (r *Rule) Delete(ctx context.Context, cfg *awsCfg, prior *RuleOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &eventbridge.DeleteRuleInput{
		Name:         aws.String(r.Name),
		EventBusName: r.eventBusName(),
		Force:        aws.ToBool(r.ForceDestroy),
	}
	// A rule that still has targets cannot be deleted; when those targets are
	// being removed concurrently the rule briefly reports as having targets, so
	// retry the delete through that window.
	err = retry.OnError(ctx, isDeleteBlockedByTargets,
		func(ctx context.Context) error {
			_, err := client.DeleteRule(ctx, in)
			return err
		}, retry.WithTimeout(5*time.Minute))
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete rule: %w", err)
	}
	return nil
}

// putRuleInput builds the PutRule request from the rule's inputs. State is
// omitted when unset so EventBridge applies its default of ENABLED, and the
// event bus is omitted unless the user chose a non-default one.
func (r *Rule) putRuleInput() *eventbridge.PutRuleInput {
	in := &eventbridge.PutRuleInput{
		Name:               aws.String(r.Name),
		EventBusName:       r.eventBusName(),
		Description:        r.Description,
		EventPattern:       r.EventPattern,
		ScheduleExpression: r.ScheduleExpression,
		RoleArn:            r.RoleArn,
		Tags:               ruleTags(r.Tags),
	}
	if r.State != nil {
		in.State = eventbridgetypes.RuleState(*r.State)
	}
	return in
}

// putRuleChanged reports whether any field PutRule reconciles differs from the
// prior inputs. The name and event bus are the rule's identity and force a
// replace rather than an update; tags reconcile through the tag API, and
// force-destroy acts only at delete, so none of those is tested here.
func (r *Rule) putRuleChanged(prior Rule) bool {
	return runtime.Changed(prior.Description, r.Description) ||
		runtime.Changed(prior.EventPattern, r.EventPattern) ||
		runtime.Changed(prior.ScheduleExpression, r.ScheduleExpression) ||
		runtime.Changed(prior.RoleArn, r.RoleArn) ||
		runtime.Changed(prior.State, r.State)
}

// putRule calls PutRule and retries it while EventBridge rejects it because the
// rule's IAM role was created moments earlier and is not yet assumable. That
// race clears once the role propagates, so the retry runs over a bounded
// window.
func (r *Rule) putRule(
	ctx context.Context, client *eventbridge.Client, in *eventbridge.PutRuleInput,
) error {
	return retry.OnError(ctx, isRoleNotYetAssumable,
		func(ctx context.Context) error {
			_, err := client.PutRule(ctx, in)
			return err
		})
}

// waitVisible polls DescribeRule until the just-created rule is found, since
// PutRule returns before the rule is consistently readable. A not-found read
// means the create is still propagating, so the wait keeps polling; any other
// error stops it.
func (r *Rule) waitVisible(ctx context.Context, client *eventbridge.Client) error {
	what := fmt.Sprintf("rule %s", r.Name)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		_, err := client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{
			Name:         aws.String(r.Name),
			EventBusName: r.eventBusName(),
		})
		if err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("describe rule: %w", err)
		}
		return true, nil
	})
}

// eventBusName returns the event bus to send on a request, or nil to let
// EventBridge use the default bus. An unset, empty, or "default" bus is left
// off the request rather than passed as the literal "default".
func (r *Rule) eventBusName() *string {
	if r.EventBusName == nil {
		return nil
	}
	name := *r.EventBusName
	if name == "" || name == defaultEventBusName {
		return nil
	}
	return r.EventBusName
}

// syncTags reconciles the rule's tags with the desired set, reading the live
// tags through ListTagsForResource and writing changes with TagResource and
// UntagResource. EventBridge addresses a rule's tags by its ARN, so the tags
// are read to obtain it.
func (r *Rule) syncTags(ctx context.Context, client *eventbridge.Client) error {
	resp, err := client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{
		Name:         aws.String(r.Name),
		EventBusName: r.eventBusName(),
	})
	if err != nil {
		return fmt.Errorf("describe rule: %w", err)
	}
	ruleArn := aws.ToString(resp.Arn)
	return tagsync.Sync(ctx, r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			tagsResp, err := client.ListTagsForResource(ctx,
				&eventbridge.ListTagsForResourceInput{ResourceARN: aws.String(ruleArn)})
			if err != nil {
				return nil, fmt.Errorf("list tags for resource: %w", err)
			}
			current := map[string]string{}
			for _, t := range tagsResp.Tags {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &eventbridge.TagResourceInput{
				ResourceARN: aws.String(ruleArn),
				Tags:        ruleTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &eventbridge.UntagResourceInput{
				ResourceARN: aws.String(ruleArn),
				TagKeys:     remove,
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}

// region returns the region the client is configured for, used to decide
// whether a create that sends tags must retry without them on a partition that
// cannot tag a rule at create time.
func region(client *eventbridge.Client) string {
	return client.Options().Region
}

// isRoleNotYetAssumable reports whether err is the validation error EventBridge
// returns when a rule names an IAM role that was created moments earlier and
// cannot be assumed yet. The role becomes assumable once it propagates, so a
// caller retries. EventBridge gives no typed exception for this, so the match
// is on the error code and message.
func isRoleNotYetAssumable(err error) bool {
	return isValidationException(err, "cannot be assumed by principal")
}

// isDeleteBlockedByTargets reports whether err is the validation error
// EventBridge returns when a rule cannot be deleted because it still has
// targets. While those targets are being removed the rule briefly reports them,
// so a caller retries the delete.
func isDeleteBlockedByTargets(err error) bool {
	return isValidationException(err, "Rule can't be deleted since it has targets")
}

// isValidationException reports whether err is a ValidationException whose
// message contains substr. EventBridge raises several distinct, self-clearing
// conditions as a generic ValidationException, told apart only by message, so a
// caller matches both the code and the message text.
func isValidationException(err error, substr string) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) &&
		apiErr.ErrorCode() == "ValidationException" &&
		strings.Contains(apiErr.ErrorMessage(), substr)
}

// ruleTags converts a desired tag map into the EventBridge SDK tag list.
func ruleTags(tags map[string]string) []eventbridgetypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]eventbridgetypes.Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, eventbridgetypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return out
}
