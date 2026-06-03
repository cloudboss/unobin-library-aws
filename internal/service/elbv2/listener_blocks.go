package elbv2

import (
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// listenerActionTypes are the default-action types this resource handles. ELBv2
// also defines authenticate-cognito, authenticate-oidc, and jwt-validation, but
// those need secrets and extra configuration that is out of scope here.
var listenerActionTypes = []string{"forward", "redirect", "fixed-response"}

// redirectStatusCodes are the two HTTP redirect codes ELBv2 accepts on a
// redirect action: a permanent 301 or a temporary 302.
var redirectStatusCodes = []string{"HTTP_301", "HTTP_302"}

// redirectProtocols are the protocols a redirect action may target. The
// reserved keyword keeps the request's own protocol; the other two redirect to
// plain HTTP or to HTTPS.
var redirectProtocols = []string{"#{protocol}", "HTTP", "HTTPS"}

// fixedResponseContentTypes are the content types a fixed-response action may
// set on its body.
var fixedResponseContentTypes = []string{
	"text/plain",
	"text/css",
	"text/html",
	"application/javascript",
	"application/json",
}

// ListenerDefaultAction is one action in a listener's default rule, a tagged
// union keyed by Type. Exactly one of the per-type members must be set and it
// must match Type: a forward names a single TargetGroupArn or a Forward block,
// a redirect names a Redirect block, a fixed-response names a FixedResponse
// block. Those rules and the type enum live inside list elements, which unobin
// cannot compile-check today, so Create and Update validate each action before
// the SDK call. Order ranks the actions; when omitted the listener sends the
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
// listed here. These rules are API-validated and checked in Create and Update.
type ListenerForward struct {
	TargetGroups []ListenerForwardTargetGroup `ub:"target-groups"`
	Stickiness   *ListenerForwardStickiness   `ub:"stickiness"`
}

// ListenerForwardTargetGroup is one target group in a forward action and its
// relative weight. The weight ranges from 0 to 999; ELBv2 enforces the bound.
type ListenerForwardTargetGroup struct {
	Arn    string `ub:"arn"`
	Weight *int64 `ub:"weight"`
}

// ListenerForwardStickiness keeps a client's requests on the same target group
// for a period. The duration ranges from 1 to 604800 seconds and is required
// when stickiness is enabled; ELBv2 enforces both rules.
type ListenerForwardStickiness struct {
	Enabled         *bool  `ub:"enabled"`
	DurationSeconds *int64 `ub:"duration-seconds"`
}

// ListenerRedirect redirects the request to a new URI, reusing any component it
// does not set. StatusCode is required and must be HTTP_301 or HTTP_302;
// Protocol, when set, must be HTTP, HTTPS, or the reserved #{protocol}. These
// rules are API-validated and checked in Create and Update.
type ListenerRedirect struct {
	Host       *string `ub:"host"`
	Path       *string `ub:"path"`
	Port       *string `ub:"port"`
	Protocol   *string `ub:"protocol"`
	Query      *string `ub:"query"`
	StatusCode string  `ub:"status-code"`
}

// ListenerFixedResponse returns a canned HTTP response without reaching a
// target. StatusCode is required and must match ^[245]\d\d$; ContentType, when
// set, must be one of the five accepted types. These rules are API-validated
// and checked in Create and Update.
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

// validateDefaultActions checks every action's type enum and its per-type
// required sub-block before the listener is sent to ELBv2. These rules live
// inside a list, which unobin's compile-time constraints cannot reach, so the
// resource enforces them here and returns a descriptive error for a bad action.
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

// validateDefaultAction checks one action: its type is in scope, exactly the
// sub-block its type requires is present, no sub-block for a different type is
// set, and a forward's top-level target-group-arn matches the single group its
// forward block names.
func validateDefaultAction(action ListenerDefaultAction) error {
	if !slices.Contains(listenerActionTypes, action.Type) {
		return fmt.Errorf("type must be one of %v, got %q",
			listenerActionTypes, action.Type)
	}
	switch action.Type {
	case "forward":
		return validateForwardAction(action)
	case "redirect":
		if action.Redirect == nil {
			return fmt.Errorf("a redirect action requires a redirect block")
		}
		if err := validateRedirect(action.Redirect); err != nil {
			return err
		}
		return forbidActionBlocks(action, "redirect")
	case "fixed-response":
		if action.FixedResponse == nil {
			return fmt.Errorf("a fixed-response action requires a fixed-response block")
		}
		if err := validateFixedResponse(action.FixedResponse); err != nil {
			return err
		}
		return forbidActionBlocks(action, "fixed-response")
	}
	return nil
}

// validateForwardAction checks a forward action: it routes through either a
// top-level target-group-arn or a forward block, and when both are set the
// forward block names exactly that one group. No redirect or fixed-response
// block may be set on a forward.
func validateForwardAction(action ListenerDefaultAction) error {
	hasArn := action.TargetGroupArn != nil
	hasForward := action.Forward != nil
	if !hasArn && !hasForward {
		return fmt.Errorf(
			"a forward action requires target-group-arn or a forward block")
	}
	if action.Redirect != nil || action.FixedResponse != nil {
		return fmt.Errorf(
			"a forward action cannot set a redirect or fixed-response block")
	}
	if hasArn && hasForward {
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

// validateRedirect checks a redirect block's required status code and its
// optional protocol against the values ELBv2 accepts.
func validateRedirect(r *ListenerRedirect) error {
	if !slices.Contains(redirectStatusCodes, r.StatusCode) {
		return fmt.Errorf("redirect status-code must be one of %v, got %q",
			redirectStatusCodes, r.StatusCode)
	}
	if r.Protocol != nil && !slices.Contains(redirectProtocols, *r.Protocol) {
		return fmt.Errorf("redirect protocol must be one of %v, got %q",
			redirectProtocols, *r.Protocol)
	}
	return nil
}

// validateFixedResponse checks a fixed-response block's required status code
// against ^[245]\d\d$ and its optional content type against the accepted set.
func validateFixedResponse(f *ListenerFixedResponse) error {
	if !validFixedResponseStatus(f.StatusCode) {
		return fmt.Errorf(
			"fixed-response status-code must be a 2xx, 4xx, or 5xx code, got %q",
			f.StatusCode)
	}
	if f.ContentType != nil && !slices.Contains(fixedResponseContentTypes, *f.ContentType) {
		return fmt.Errorf("fixed-response content-type must be one of %v, got %q",
			fixedResponseContentTypes, *f.ContentType)
	}
	return nil
}

// forbidActionBlocks reports an error when an action of the named type sets a
// sub-block meant for a different action type.
func forbidActionBlocks(action ListenerDefaultAction, kind string) error {
	if kind != "forward" && (action.TargetGroupArn != nil || action.Forward != nil) {
		return fmt.Errorf(
			"a %s action cannot set target-group-arn or a forward block", kind)
	}
	if kind != "redirect" && action.Redirect != nil {
		return fmt.Errorf("a %s action cannot set a redirect block", kind)
	}
	if kind != "fixed-response" && action.FixedResponse != nil {
		return fmt.Errorf("a %s action cannot set a fixed-response block", kind)
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
