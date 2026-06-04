package elbv2

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// TargetGroupMatcher is the health-check success-code matcher, the Matcher
// member of CreateTargetGroup. Exactly one of HttpCode or GrpcCode is set:
// HttpCode for an HTTP or HTTPS health check (such as 200 or 200-299), and
// GrpcCode, which applies only when the group's protocol version is GRPC.
type TargetGroupMatcher struct {
	HttpCode *string `ub:"http-code"`
	GrpcCode *string `ub:"grpc-code"`
}

func (m *TargetGroupMatcher) to() *elbv2types.Matcher {
	if m == nil {
		return nil
	}
	return &elbv2types.Matcher{HttpCode: m.HttpCode, GrpcCode: m.GrpcCode}
}

// TargetGroupStickiness configures whether a client is consistently routed to
// the same target. ELBv2 stores it among the target group attributes, so the
// block is reconciled by ModifyTargetGroupAttributes rather than at create. A
// nil block leaves stickiness disabled, ELBv2's default. Type is required when
// the block is present: lb_cookie, app_cookie, source_ip, source_ip_dest_ip,
// or source_ip_dest_ip_proto. The cookie duration is 0-604800 seconds. The
// cookie-duration and cookie-name fields apply only to an HTTP or HTTPS group,
// where lb_cookie reads the duration and app_cookie reads the name and
// duration.
type TargetGroupStickiness struct {
	Enabled        *bool   `ub:"enabled"`
	Type           *string `ub:"type"`
	CookieDuration *int64  `ub:"cookie-duration"`
	CookieName     *string `ub:"cookie-name"`
}

// attributes returns the target group attributes that express the stickiness
// block for a group of the given protocol. The cookie duration and app-cookie
// name apply only to an HTTP or HTTPS group; for other protocols stickiness is
// just the enabled flag and the type. A nil block enables nothing.
func (s *TargetGroupStickiness) attributes(httpProtocol bool) []elbv2types.TargetGroupAttribute {
	if s == nil {
		return nil
	}
	attrs := []elbv2types.TargetGroupAttribute{
		attribute("stickiness.enabled", boolValue(s.Enabled)),
	}
	if s.Type != nil {
		attrs = append(attrs, attribute("stickiness.type", *s.Type))
	}
	if !httpProtocol {
		return attrs
	}
	cookieType := ""
	if s.Type != nil {
		cookieType = *s.Type
	}
	if s.CookieDuration != nil {
		switch cookieType {
		case "app_cookie":
			attrs = append(attrs, attribute("stickiness.app_cookie.duration_seconds",
				int64String(s.CookieDuration)))
		default:
			attrs = append(attrs, attribute("stickiness.lb_cookie.duration_seconds",
				int64String(s.CookieDuration)))
		}
	}
	if cookieType == "app_cookie" && s.CookieName != nil {
		attrs = append(attrs, attribute("stickiness.app_cookie.cookie_name", *s.CookieName))
	}
	return attrs
}

// attribute builds one target group attribute from a key and a string value.
func attribute(key, value string) elbv2types.TargetGroupAttribute {
	return elbv2types.TargetGroupAttribute{Key: aws.String(key), Value: aws.String(value)}
}
