package elbv2

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// ListenerDefaultAction is one action in a listener's default rule, a tagged
// union keyed by Type. Type is forward, redirect, or fixed-response (ELBv2
// also defines authentication and jwt-validation types, which need secrets and
// extra configuration that is out of scope here). Exactly one of the per-type
// members is set and it must match Type: a forward names a single
// TargetGroupArn or a Forward block, a redirect names a Redirect block, a
// fixed-response names a FixedResponse block; Listener's Constraints declare
// those rules. Order ranks the actions; when omitted the listener sends the
// one-based position so the cloud assigns a stable order.
type ListenerDefaultAction struct {
	Type           string                 `ub:"type"`
	Order          *int64                 `ub:"order"`
	TargetGroupArn *string                `ub:"target-group-arn"`
	Forward        *ListenerForward       `ub:"forward"`
	Redirect       *ListenerRedirect      `ub:"redirect"`
	FixedResponse  *ListenerFixedResponse `ub:"fixed-response"`
}

// ListenerForward distributes requests across one to five target groups, with
// optional weighting and target-group stickiness. When a forward action also
// names a top-level target-group-arn, that ARN must match the single group
// listed here; Create and Update check the match, which needs a count inside a
// nested list that a constraint cannot take.
type ListenerForward struct {
	TargetGroups []ListenerForwardTargetGroup `ub:"target-groups"`
	Stickiness   *ListenerForwardStickiness   `ub:"stickiness"`
}

// ListenerForwardTargetGroup is one target group in a forward action and its
// relative weight. The weight ranges from 0 to 999; ELBv2 enforces the bound,
// which sits in a list inside a list element where a constraint cannot reach.
type ListenerForwardTargetGroup struct {
	Arn    string `ub:"arn"`
	Weight *int64 `ub:"weight"`
}

// ListenerForwardStickiness keeps a client's requests on the same target group
// for a period. The duration ranges from 1 to 604800 seconds and is required
// when stickiness is enabled.
type ListenerForwardStickiness struct {
	Enabled         *bool  `ub:"enabled"`
	DurationSeconds *int64 `ub:"duration-seconds"`
}

// ListenerRedirect redirects the request to a new URI, reusing any component it
// does not set. StatusCode is required and is HTTP_301 or HTTP_302; Protocol,
// when set, is HTTP, HTTPS, or the reserved #{protocol}.
type ListenerRedirect struct {
	Host       *string `ub:"host"`
	Path       *string `ub:"path"`
	Port       *string `ub:"port"`
	Protocol   *string `ub:"protocol"`
	Query      *string `ub:"query"`
	StatusCode string  `ub:"status-code"`
}

// ListenerFixedResponse returns a canned HTTP response without reaching a
// target. StatusCode is required and must match ^[245]\d\d$, checked in Create
// and Update since a constraint cannot take a pattern; ContentType, when set,
// is one of the five accepted types.
type ListenerFixedResponse struct {
	ContentType *string `ub:"content-type"`
	MessageBody *string `ub:"message-body"`
	StatusCode  string  `ub:"status-code"`
}

// defaultActions expands the desired actions into the SDK type. When the user
// gave no explicit order for an action, it sends the one-based index so ELBv2
// assigns each action a stable order; an order the user set is passed through.
func defaultActions(actions []ListenerDefaultAction) []elbv2types.Action {
	out := make([]elbv2types.Action, 0, len(actions))
	for i, action := range actions {
		expanded := elbv2types.Action{
			Type:           elbv2types.ActionTypeEnum(action.Type),
			TargetGroupArn: action.TargetGroupArn,
		}
		if action.Order != nil {
			expanded.Order = ptr.Int32(action.Order)
		} else {
			expanded.Order = aws.Int32(int32(i + 1))
		}
		if action.Forward != nil {
			expanded.ForwardConfig = forwardConfig(action.Forward)
		}
		if action.Redirect != nil {
			expanded.RedirectConfig = redirectConfig(action.Redirect)
		}
		if action.FixedResponse != nil {
			expanded.FixedResponseConfig = fixedResponseConfig(action.FixedResponse)
		}
		out = append(out, expanded)
	}
	return out
}

// forwardConfig expands a forward block into the SDK type.
func forwardConfig(f *ListenerForward) *elbv2types.ForwardActionConfig {
	cfg := &elbv2types.ForwardActionConfig{}
	if len(f.TargetGroups) > 0 {
		groups := make([]elbv2types.TargetGroupTuple, 0, len(f.TargetGroups))
		for _, g := range f.TargetGroups {
			groups = append(groups, elbv2types.TargetGroupTuple{
				TargetGroupArn: aws.String(g.Arn),
				Weight:         ptr.Int32(g.Weight),
			})
		}
		cfg.TargetGroups = groups
	}
	if f.Stickiness != nil {
		cfg.TargetGroupStickinessConfig = &elbv2types.TargetGroupStickinessConfig{
			Enabled:         f.Stickiness.Enabled,
			DurationSeconds: ptr.Int32(f.Stickiness.DurationSeconds),
		}
	}
	return cfg
}

// redirectConfig expands a redirect block into the SDK type.
func redirectConfig(r *ListenerRedirect) *elbv2types.RedirectActionConfig {
	return &elbv2types.RedirectActionConfig{
		StatusCode: elbv2types.RedirectActionStatusCodeEnum(r.StatusCode),
		Host:       r.Host,
		Path:       r.Path,
		Port:       r.Port,
		Protocol:   r.Protocol,
		Query:      r.Query,
	}
}

// fixedResponseConfig expands a fixed-response block into the SDK type.
func fixedResponseConfig(f *ListenerFixedResponse) *elbv2types.FixedResponseActionConfig {
	return &elbv2types.FixedResponseActionConfig{
		StatusCode:  aws.String(f.StatusCode),
		ContentType: f.ContentType,
		MessageBody: f.MessageBody,
	}
}

// validateDefaultActions checks the action rules a constraint cannot express:
// the list must not be explicitly empty (a constraint sees an empty list,
// unlike an omitted one, as present), a fixed-response status code must match
// a pattern, and a forward that sets both target-group-arn and a forward block
// must name exactly that one group, a count inside a nested list. Every other
// action rule is declared in the resource's Constraints.
func validateDefaultActions(actions []ListenerDefaultAction) error {
	if len(actions) == 0 {
		return fmt.Errorf("default-action must list at least one action")
	}
	for i, action := range actions {
		if err := validateDefaultAction(action); err != nil {
			return fmt.Errorf("default-action %d: %w", i, err)
		}
	}
	return nil
}

// validateDefaultAction checks one action's residual rules: the fixed-response
// status pattern and the forward arn-match.
func validateDefaultAction(action ListenerDefaultAction) error {
	if action.FixedResponse != nil && !validFixedResponseStatus(action.FixedResponse.StatusCode) {
		return fmt.Errorf(
			"fixed-response status-code must be a 2xx, 4xx, or 5xx code, got %q",
			action.FixedResponse.StatusCode)
	}
	if action.TargetGroupArn != nil && action.Forward != nil {
		if len(action.Forward.TargetGroups) != 1 {
			return fmt.Errorf(
				"with target-group-arn set, the forward block must name exactly one " +
					"target group matching it")
		}
		if action.Forward.TargetGroups[0].Arn != *action.TargetGroupArn {
			return fmt.Errorf(
				"target-group-arn must match the forward block's target group")
		}
	}
	return nil
}

// validFixedResponseStatus reports whether code matches ^[245]\d\d$: a
// three-digit HTTP status whose first digit is 2, 4, or 5.
func validFixedResponseStatus(code string) bool {
	if len(code) != 3 {
		return false
	}
	if code[0] != '2' && code[0] != '4' && code[0] != '5' {
		return false
	}
	for _, c := range code[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
