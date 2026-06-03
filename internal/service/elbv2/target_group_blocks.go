package elbv2

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// healthCheckProtocolTCP is the one health-check protocol that forbids a path
// and a matcher, so the create and update omit those fields when it is in use.
const healthCheckProtocolTCP = "TCP"

// TargetGroupHealthCheck describes how the load balancer probes a target's
// health. The fields ride CreateTargetGroup at create and are reconciled by
// ModifyTargetGroup on update. A nil block leaves ELBv2's per-protocol health
// check defaults in place.
//
// These bounds and enums are enforced in the resource code and documented here
// rather than as constraints, since a nested block's fields do not derive
// unobin constraints: protocol is one of HTTP, HTTPS, or TCP (a TCP check is
// rejected for an HTTP/HTTPS group, for a lambda target, and forbids path and
// matcher); port is the literal traffic-port or an integer from 1 to 65535;
// interval is 5-300 seconds; timeout is 2-120 seconds; the healthy and
// unhealthy thresholds are each 2-10.
type TargetGroupHealthCheck struct {
	Enabled            *bool   `ub:"enabled"`
	Protocol           *string `ub:"protocol"`
	Port               *string `ub:"port"`
	Path               *string `ub:"path"`
	IntervalSeconds    *int64  `ub:"interval-seconds"`
	TimeoutSeconds     *int64  `ub:"timeout-seconds"`
	HealthyThreshold   *int64  `ub:"healthy-threshold"`
	UnhealthyThreshold *int64  `ub:"unhealthy-threshold"`
	Matcher            *string `ub:"matcher"`
	GrpcMatcher        *string `ub:"grpc-matcher"`
}

// usesTCP reports whether the health check runs over TCP, the protocol that
// has no path or matcher. The comparison is case-insensitive because ELBv2
// accepts a lower-case protocol and normalizes it to upper-case.
func (h *TargetGroupHealthCheck) usesTCP() bool {
	return h != nil && h.Protocol != nil && upperProtocol(*h.Protocol) == healthCheckProtocolTCP
}

// matcher builds the health-check success-code matcher. The GRPC code goes in
// its own field when the protocol version is gRPC; otherwise the value is an
// HTTP code. A TCP health check has no matcher, so this returns nil there.
func (h *TargetGroupHealthCheck) matcher(grpc bool) *elbv2types.Matcher {
	if h == nil || h.usesTCP() {
		return nil
	}
	if grpc && h.GrpcMatcher != nil {
		return &elbv2types.Matcher{GrpcCode: h.GrpcMatcher}
	}
	if h.Matcher != nil {
		return &elbv2types.Matcher{HttpCode: h.Matcher}
	}
	return nil
}

// applyToCreate fills the health-check fields on a CreateTargetGroup request.
// The path and matcher are omitted for a TCP check, which rejects them. grpc
// reports whether the group's protocol version is gRPC, which decides whether
// the matcher holds a gRPC code or an HTTP code.
func (h *TargetGroupHealthCheck) applyToCreate(in *elbv2.CreateTargetGroupInput, grpc bool) {
	if h == nil {
		return
	}
	in.HealthCheckEnabled = h.Enabled
	in.HealthCheckPort = h.Port
	in.HealthCheckIntervalSeconds = ptr.Int32(h.IntervalSeconds)
	in.HealthCheckTimeoutSeconds = ptr.Int32(h.TimeoutSeconds)
	in.HealthyThresholdCount = ptr.Int32(h.HealthyThreshold)
	in.UnhealthyThresholdCount = ptr.Int32(h.UnhealthyThreshold)
	if h.Protocol != nil {
		in.HealthCheckProtocol = elbv2types.ProtocolEnum(upperProtocol(*h.Protocol))
	}
	if !h.usesTCP() {
		in.HealthCheckPath = h.Path
	}
	in.Matcher = h.matcher(grpc)
}

// applyToModify fills the health-check fields on a ModifyTargetGroup request,
// the call an update makes when a health-check field changed. As with create,
// a TCP check omits the path and matcher.
func (h *TargetGroupHealthCheck) applyToModify(in *elbv2.ModifyTargetGroupInput, grpc bool) {
	if h == nil {
		return
	}
	in.HealthCheckEnabled = h.Enabled
	in.HealthCheckPort = h.Port
	in.HealthCheckIntervalSeconds = ptr.Int32(h.IntervalSeconds)
	in.HealthCheckTimeoutSeconds = ptr.Int32(h.TimeoutSeconds)
	in.HealthyThresholdCount = ptr.Int32(h.HealthyThreshold)
	in.UnhealthyThresholdCount = ptr.Int32(h.UnhealthyThreshold)
	if h.Protocol != nil {
		in.HealthCheckProtocol = elbv2types.ProtocolEnum(upperProtocol(*h.Protocol))
	}
	if !h.usesTCP() {
		in.HealthCheckPath = h.Path
	}
	in.Matcher = h.matcher(grpc)
}

// TargetGroupStickiness configures whether a client is consistently routed to
// the same target. ELBv2 stores it among the target group attributes, so the
// block is reconciled by ModifyTargetGroupAttributes rather than at create. A
// nil block leaves stickiness disabled, ELBv2's default.
//
// Type is required when the block is present and is enforced in the resource
// code, since a nested block's fields do not derive unobin constraints; it is
// one of lb_cookie, app_cookie, source_ip, source_ip_dest_ip, or
// source_ip_dest_ip_proto. The cookie duration is 0-604800 seconds. The
// cookie-duration and app-cookie-name fields apply only to an HTTP or HTTPS
// group, where lb_cookie reads the duration and app_cookie reads the name and
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
