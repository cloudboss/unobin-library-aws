package elbv2

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// The blocks below model the structured members ELBv2 accepts on a rule's
// actions and conditions. Each is converted to its SDK type and assembled into
// the CreateRule or ModifyRule request rather than written by its own call. A
// nil sub-block leaves that member unset. The per-type required-sub-block
// rules, the exactly-one-matcher rule, the enums and bounds each block notes,
// and the rules inside a list within a list element (the forward target
// groups, the query-string pairs) are declared in ListenerRule's Constraints;
// string length and pattern rules are enforced by the ELBv2 API.

// Action type strings, the ELBv2 ActionTypeEnum values in scope. They are
// lowercase-hyphenated and name which sub-block an action takes.
const (
	actionTypeForward       = "forward"
	actionTypeRedirect      = "redirect"
	actionTypeFixedResponse = "fixed-response"
)

// ListenerRuleAction is one action a rule takes on a matched request. Type is
// required and one of forward, redirect, or fixed-response (the authentication
// and jwt-validation action types are out of scope). The type fixes which
// sub-block the action takes: a forward action sets either TargetGroupArn (a
// single target group) or a Forward block (one or more weighted target groups);
// a redirect action sets a Redirect block; a fixed-response action sets a
// FixedResponse block. Order is optional and 1..50000; when omitted the rule
// assigns a 1-based index so the order is stable and matches the read-back sort.
type ListenerRuleAction struct {
	Type           string                     `ub:"type"`
	Order          *int64                     `ub:"order"`
	TargetGroupArn *string                    `ub:"target-group-arn"`
	Forward        *ListenerRuleForward       `ub:"forward"`
	Redirect       *ListenerRuleRedirect      `ub:"redirect"`
	FixedResponse  *ListenerRuleFixedResponse `ub:"fixed-response"`
}

// to converts the action to the SDK type, setting only the sub-block its type
// takes. order is the 1-based position to send when the action omits its own
// order, so a rule's actions keep a stable order across applies.
func (a *ListenerRuleAction) to(order int32) elbv2types.Action {
	out := elbv2types.Action{Type: elbv2types.ActionTypeEnum(a.Type)}
	if a.Order != nil {
		out.Order = ptr.Int32(a.Order)
	} else {
		out.Order = aws.Int32(order)
	}
	switch a.Type {
	case actionTypeForward:
		out.TargetGroupArn = a.TargetGroupArn
		out.ForwardConfig = a.Forward.to()
	case actionTypeRedirect:
		out.RedirectConfig = a.Redirect.to()
	case actionTypeFixedResponse:
		out.FixedResponseConfig = a.FixedResponse.to()
	}
	return out
}

// ListenerRuleForward distributes a matched request across one or more target
// groups. TargetGroups is required and holds 1..5 entries; Stickiness optionally
// routes a client's requests to the same target group.
type ListenerRuleForward struct {
	TargetGroups []ListenerRuleForwardTargetGroup `ub:"target-groups"`
	Stickiness   *ListenerRuleForwardStickiness   `ub:"stickiness"`
}

func (f *ListenerRuleForward) to() *elbv2types.ForwardActionConfig {
	if f == nil {
		return nil
	}
	out := &elbv2types.ForwardActionConfig{
		TargetGroupStickinessConfig: f.Stickiness.to(),
	}
	if len(f.TargetGroups) > 0 {
		groups := make([]elbv2types.TargetGroupTuple, 0, len(f.TargetGroups))
		for i := range f.TargetGroups {
			groups = append(groups, f.TargetGroups[i].to())
		}
		out.TargetGroups = groups
	}
	return out
}

// ListenerRuleForwardTargetGroup is one target group in a forward action. Arn is
// required; Weight, in 0..999 and defaulting to 1, sets the share of requests
// routed to this group relative to the others.
type ListenerRuleForwardTargetGroup struct {
	Arn    *string `ub:"arn"`
	Weight *int64  `ub:"weight"`
}

func (g *ListenerRuleForwardTargetGroup) to() elbv2types.TargetGroupTuple {
	return elbv2types.TargetGroupTuple{
		TargetGroupArn: g.Arn,
		Weight:         ptr.Int32(g.Weight),
	}
}

// ListenerRuleForwardStickiness keeps a client's requests on one target group
// for a window. DurationSeconds is 1..604800 and required when Enabled is true;
// Enabled defaults to false.
type ListenerRuleForwardStickiness struct {
	Enabled         *bool  `ub:"enabled"`
	DurationSeconds *int64 `ub:"duration-seconds"`
}

func (s *ListenerRuleForwardStickiness) to() *elbv2types.TargetGroupStickinessConfig {
	if s == nil {
		return nil
	}
	return &elbv2types.TargetGroupStickinessConfig{
		Enabled:         s.Enabled,
		DurationSeconds: ptr.Int32(s.DurationSeconds),
	}
}

// ListenerRuleRedirect responds to a matched request with an HTTP redirect.
// StatusCode is required and one of HTTP_301 or HTTP_302. Host, Path, Port,
// Protocol, and Query are each optional; an omitted component keeps its original
// request value, which ELBv2 represents with the reserved placeholders #{host},
// /#{path}, #{port}, #{protocol}, and #{query}. Protocol is HTTP, HTTPS, or
// #{protocol}.
type ListenerRuleRedirect struct {
	StatusCode string  `ub:"status-code"`
	Host       *string `ub:"host"`
	Path       *string `ub:"path"`
	Port       *string `ub:"port"`
	Protocol   *string `ub:"protocol"`
	Query      *string `ub:"query"`
}

func (rd *ListenerRuleRedirect) to() *elbv2types.RedirectActionConfig {
	if rd == nil {
		return nil
	}
	return &elbv2types.RedirectActionConfig{
		StatusCode: elbv2types.RedirectActionStatusCodeEnum(rd.StatusCode),
		Host:       rd.Host,
		Path:       rd.Path,
		Port:       rd.Port,
		Protocol:   rd.Protocol,
		Query:      rd.Query,
	}
}

// ListenerRuleFixedResponse responds to a matched request with a fixed HTTP
// response. ContentType is required and one of text/plain, text/css, text/html,
// application/javascript, or application/json. StatusCode is optional, a 2xx,
// 4xx, or 5xx code. MessageBody is the optional response body, 0..1024
// characters.
type ListenerRuleFixedResponse struct {
	ContentType string  `ub:"content-type"`
	StatusCode  *string `ub:"status-code"`
	MessageBody *string `ub:"message-body"`
}

func (fr *ListenerRuleFixedResponse) to() *elbv2types.FixedResponseActionConfig {
	if fr == nil {
		return nil
	}
	return &elbv2types.FixedResponseActionConfig{
		ContentType: aws.String(fr.ContentType),
		StatusCode:  fr.StatusCode,
		MessageBody: fr.MessageBody,
	}
}

// ListenerRuleCondition is one matcher a rule applies to a request. Exactly one
// of the six matcher sub-blocks is set: HostHeader, HttpHeader,
// HttpRequestMethod, PathPattern, QueryString, or SourceIp; ListenerRule's
// Constraints declare the rule.
type ListenerRuleCondition struct {
	HostHeader        *ListenerRuleHostHeader        `ub:"host-header"`
	HttpHeader        *ListenerRuleHttpHeader        `ub:"http-header"`
	HttpRequestMethod *ListenerRuleHttpRequestMethod `ub:"http-request-method"`
	PathPattern       *ListenerRulePathPattern       `ub:"path-pattern"`
	QueryString       *ListenerRuleQueryString       `ub:"query-string"`
	SourceIp          *ListenerRuleSourceIp          `ub:"source-ip"`
}

// to converts the condition to the SDK type, setting the matching Field string
// and sub-config for whichever matcher is present.
func (c *ListenerRuleCondition) to() elbv2types.RuleCondition {
	switch {
	case c.HostHeader != nil:
		return elbv2types.RuleCondition{
			Field:            aws.String("host-header"),
			HostHeaderConfig: c.HostHeader.to(),
		}
	case c.HttpHeader != nil:
		return elbv2types.RuleCondition{
			Field:            aws.String("http-header"),
			HttpHeaderConfig: c.HttpHeader.to(),
		}
	case c.HttpRequestMethod != nil:
		return elbv2types.RuleCondition{
			Field:                   aws.String("http-request-method"),
			HttpRequestMethodConfig: c.HttpRequestMethod.to(),
		}
	case c.PathPattern != nil:
		return elbv2types.RuleCondition{
			Field:             aws.String("path-pattern"),
			PathPatternConfig: c.PathPattern.to(),
		}
	case c.QueryString != nil:
		return elbv2types.RuleCondition{
			Field:             aws.String("query-string"),
			QueryStringConfig: c.QueryString.to(),
		}
	default:
		return elbv2types.RuleCondition{
			Field:          aws.String("source-ip"),
			SourceIpConfig: c.SourceIp.to(),
		}
	}
}

// ListenerRuleHostHeader matches the request host header. Values holds one or
// more host names, each up to 128 characters, matched case-insensitively, with
// the wildcards * and ?.
type ListenerRuleHostHeader struct {
	Values []string `ub:"values"`
}

func (h *ListenerRuleHostHeader) to() *elbv2types.HostHeaderConditionConfig {
	return &elbv2types.HostHeaderConditionConfig{Values: h.Values}
}

// ListenerRuleHttpHeader matches an arbitrary HTTP header. HttpHeaderName is
// required, the name of the header to match. Values holds one or more strings to
// compare against the header, each up to 128 characters, with the wildcards *
// and ?.
type ListenerRuleHttpHeader struct {
	HttpHeaderName string   `ub:"http-header-name"`
	Values         []string `ub:"values"`
}

func (h *ListenerRuleHttpHeader) to() *elbv2types.HttpHeaderConditionConfig {
	return &elbv2types.HttpHeaderConditionConfig{
		HttpHeaderName: aws.String(h.HttpHeaderName),
		Values:         h.Values,
	}
}

// ListenerRuleHttpRequestMethod matches the HTTP request method. Values is
// required and holds one or more method names, each 1..40 characters of A-Z,
// hyphen, and underscore, compared case-sensitively.
type ListenerRuleHttpRequestMethod struct {
	Values []string `ub:"values"`
}

func (h *ListenerRuleHttpRequestMethod) to() *elbv2types.HttpRequestMethodConditionConfig {
	return &elbv2types.HttpRequestMethodConditionConfig{Values: h.Values}
}

// ListenerRulePathPattern matches the request path. Values holds one or more
// path patterns, each up to 128 characters, matched case-sensitively, with the
// wildcards * and ?.
type ListenerRulePathPattern struct {
	Values []string `ub:"values"`
}

func (p *ListenerRulePathPattern) to() *elbv2types.PathPatternConditionConfig {
	return &elbv2types.PathPatternConditionConfig{Values: p.Values}
}

// ListenerRuleQueryString matches key/value pairs in the request query string.
// Values holds one or more pairs; within each, the value is required and the key
// is optional. Each string is up to 128 characters, matched case-insensitively,
// with the wildcards * and ?.
type ListenerRuleQueryString struct {
	Values []ListenerRuleQueryStringPair `ub:"values"`
}

func (q *ListenerRuleQueryString) to() *elbv2types.QueryStringConditionConfig {
	pairs := make([]elbv2types.QueryStringKeyValuePair, 0, len(q.Values))
	for i := range q.Values {
		pairs = append(pairs, q.Values[i].to())
	}
	return &elbv2types.QueryStringConditionConfig{Values: pairs}
}

// ListenerRuleQueryStringPair is one key/value matcher in a query-string
// condition. Value is required; Key is optional and omitted when matching on the
// value alone.
type ListenerRuleQueryStringPair struct {
	Key   *string `ub:"key"`
	Value *string `ub:"value"`
}

func (p *ListenerRuleQueryStringPair) to() elbv2types.QueryStringKeyValuePair {
	return elbv2types.QueryStringKeyValuePair{Key: p.Key, Value: p.Value}
}

// ListenerRuleSourceIp matches the source IP address of the request. Values is
// required and holds one or more CIDR blocks, IPv4 or IPv6; the condition uses
// the address that connects to the load balancer, not the X-Forwarded-For
// header.
type ListenerRuleSourceIp struct {
	Values []string `ub:"values"`
}

func (s *ListenerRuleSourceIp) to() *elbv2types.SourceIpConditionConfig {
	return &elbv2types.SourceIpConditionConfig{Values: s.Values}
}
