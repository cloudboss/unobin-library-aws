package elbv2

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/elbv2"
)

// TestLibraryRegistersElbv2 checks the runtime registration: the load-balancing
// resources are present under Resources and dispatch to their output types.
func TestLibraryRegistersElbv2(t *testing.T) {
	lib := Library()
	require.Contains(t, lib.Resources, "load-balancer")
	assert.Equal(t, reflect.TypeFor[*svc.LoadBalancerOutput](),
		lib.Resources["load-balancer"].OutputType())
	require.Contains(t, lib.Resources, "target-group")
	assert.Equal(t, reflect.TypeFor[*svc.TargetGroupOutput](),
		lib.Resources["target-group"].OutputType())
	require.Contains(t, lib.Resources, "target-group-attachment")
	assert.Equal(t, reflect.TypeFor[*svc.TargetGroupAttachmentOutput](),
		lib.Resources["target-group-attachment"].OutputType())
	require.Contains(t, lib.Resources, "listener")
	assert.Equal(t, reflect.TypeFor[*svc.ListenerOutput](),
		lib.Resources["listener"].OutputType())
	require.Contains(t, lib.Resources, "listener-rule")
	assert.Equal(t, reflect.TypeFor[*svc.ListenerRuleOutput](),
		lib.Resources["listener-rule"].OutputType())
	require.Contains(t, lib.Resources, "listener-certificate")
	assert.Equal(t, reflect.TypeFor[*svc.ListenerCertificateOutput](),
		lib.Resources["listener-certificate"].OutputType())
}

// TestElbv2Schemas asserts the whole derived TypeSchema -- input and output
// field types, sensitivity, and the cross-field constraints -- for each
// load-balancing resource. The comparison is direct: nested object fields
// follow goschema's declaration order.
func TestElbv2Schemas(t *testing.T) {
	schema := readLibrarySchema(t)
	tests := []struct {
		key  string
		want *runtime.TypeSchema
	}{
		{
			key: "load-balancer",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"access-logs": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
						{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
						{Name: "bucket", Type: typecheck.TString(), Optional: true},
						{Name: "prefix", Type: typecheck.TString(), Optional: true},
					})),
					"client-keep-alive": typecheck.TOptional(typecheck.TInteger()),
					"connection-logs": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
						{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
						{Name: "bucket", Type: typecheck.TString(), Optional: true},
						{Name: "prefix", Type: typecheck.TString(), Optional: true},
					})),
					"customer-owned-ipv4-pool":                    typecheck.TOptional(typecheck.TString()),
					"desync-mitigation-mode":                      typecheck.TOptional(typecheck.TString()),
					"dns-record-client-routing-policy":            typecheck.TOptional(typecheck.TString()),
					"drop-invalid-header-fields":                  typecheck.TOptional(typecheck.TBoolean()),
					"enable-cross-zone-load-balancing":            typecheck.TOptional(typecheck.TBoolean()),
					"enable-deletion-protection":                  typecheck.TOptional(typecheck.TBoolean()),
					"enable-http2":                                typecheck.TOptional(typecheck.TBoolean()),
					"enable-tls-version-and-cipher-suite-headers": typecheck.TOptional(typecheck.TBoolean()),
					"enable-xff-client-port":                      typecheck.TOptional(typecheck.TBoolean()),
					"idle-timeout":                                typecheck.TOptional(typecheck.TInteger()),
					"internal":                                    typecheck.TOptional(typecheck.TBoolean()),
					"ip-address-type":                             typecheck.TOptional(typecheck.TString()),
					"load-balancer-type":                          typecheck.TOptional(typecheck.TString()),
					"name":                                        typecheck.TString(),
					"preserve-host-header":                        typecheck.TOptional(typecheck.TBoolean()),
					"security-groups": typecheck.TOptional(typecheck.TList(
						typecheck.TString())),
					"subnet-mappings": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "subnet-id", Type: typecheck.TString(), Optional: false},
						{Name: "allocation-id", Type: typecheck.TString(), Optional: true},
						{Name: "private-ipv4-address", Type: typecheck.TString(), Optional: true},
						{Name: "ipv6-address", Type: typecheck.TString(), Optional: true},
						{Name: "source-nat-ipv6-prefix", Type: typecheck.TString(), Optional: true},
					}))),
					"subnets":                    typecheck.TOptional(typecheck.TList(typecheck.TString())),
					"tags":                       typecheck.TOptional(typecheck.TMap(typecheck.TString())),
					"xff-header-processing-mode": typecheck.TOptional(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"arn":                      typecheck.TString(),
					"arn-suffix":               typecheck.TString(),
					"canonical-hosted-zone-id": typecheck.TString(),
					"dns-name":                 typecheck.TString(),
					"ip-address-type":          typecheck.TString(),
					"name":                     typecheck.TString(),
					"scheme":                   typecheck.TString(),
					"vpc-id":                   typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "true",
						Require: "((((input.subnets != null) && " +
							"(@core.length(input.subnets) >= 1)) && " +
							"!((input.subnet-mappings != null) && " +
							"(@core.length(input.subnet-mappings) >= 1))) || " +
							"(!((input.subnets != null) && " +
							"(@core.length(input.subnets) >= 1)) && " +
							"((input.subnet-mappings != null) && " +
							"(@core.length(input.subnet-mappings) >= 1))))",
						Message: "exactly one of subnets or subnet-mappings is required",
					},
					{
						Kind: "predicate",
						When: "(input.load-balancer-type != null)",
						Require: "(input.load-balancer-type == 'application' || " +
							"input.load-balancer-type == 'network' || input.load-balancer-type == 'gateway')",
						Message: "load-balancer-type must be application, network, or gateway",
					},
					{
						Kind: "predicate",
						When: "(input.ip-address-type != null)",
						Require: "(input.ip-address-type == 'ipv4' || input.ip-address-type == 'dualstack' || " +
							"input.ip-address-type == 'dualstack-without-public-ipv4')",
						Message: "ip-address-type must be ipv4, dualstack, or dualstack-without-public-ipv4",
					},
					{
						Kind: "predicate",
						When: "(input.desync-mitigation-mode != null)",
						Require: "(input.desync-mitigation-mode == 'monitor' || " +
							"input.desync-mitigation-mode == 'defensive' || input.desync-mitigation-mode == 'strictest')",
						Message: "desync-mitigation-mode must be monitor, defensive, or strictest",
					},
					{
						Kind: "predicate",
						When: "(input.xff-header-processing-mode != null)",
						Require: "(input.xff-header-processing-mode == 'append' || " +
							"input.xff-header-processing-mode == 'preserve' || " +
							"input.xff-header-processing-mode == 'remove')",
						Message: "xff-header-processing-mode must be append, preserve, or remove",
					},
					{
						Kind: "predicate",
						When: "(input.dns-record-client-routing-policy != null)",
						Require: "(input.dns-record-client-routing-policy == 'availability_zone_affinity' || " +
							"input.dns-record-client-routing-policy == 'partial_availability_zone_affinity' || " +
							"input.dns-record-client-routing-policy == 'any_availability_zone')",
						Message: "dns-record-client-routing-policy must be a valid routing policy",
					},
					{
						Kind:    "predicate",
						When:    "(input.access-logs.enabled == true)",
						Require: "(input.access-logs.bucket != null)",
						Message: "enabled access-logs require a bucket",
					},
					{
						Kind:    "predicate",
						When:    "(input.connection-logs.enabled == true)",
						Require: "(input.connection-logs.bucket != null)",
						Message: "enabled connection-logs require a bucket",
					},
				},
			},
		},
		{
			key: "target-group",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"connection-termination":             typecheck.TOptional(typecheck.TBoolean()),
					"deregistration-delay":               typecheck.TOptional(typecheck.TInteger()),
					"health-check-enabled":               typecheck.TOptional(typecheck.TBoolean()),
					"health-check-interval-seconds":      typecheck.TOptional(typecheck.TInteger()),
					"health-check-path":                  typecheck.TOptional(typecheck.TString()),
					"health-check-port":                  typecheck.TOptional(typecheck.TString()),
					"health-check-protocol":              typecheck.TOptional(typecheck.TString()),
					"health-check-timeout-seconds":       typecheck.TOptional(typecheck.TInteger()),
					"healthy-threshold-count":            typecheck.TOptional(typecheck.TInteger()),
					"ip-address-type":                    typecheck.TOptional(typecheck.TString()),
					"lambda-multi-value-headers-enabled": typecheck.TOptional(typecheck.TBoolean()),
					"load-balancing-algorithm-type":      typecheck.TOptional(typecheck.TString()),
					"load-balancing-cross-zone-enabled":  typecheck.TOptional(typecheck.TString()),
					"matcher": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
						{Name: "http-code", Type: typecheck.TString(), Optional: true},
						{Name: "grpc-code", Type: typecheck.TString(), Optional: true},
					})),
					"name":               typecheck.TString(),
					"port":               typecheck.TOptional(typecheck.TInteger()),
					"preserve-client-ip": typecheck.TOptional(typecheck.TBoolean()),
					"protocol":           typecheck.TOptional(typecheck.TString()),
					"protocol-version":   typecheck.TOptional(typecheck.TString()),
					"proxy-protocol-v2":  typecheck.TOptional(typecheck.TBoolean()),
					"slow-start":         typecheck.TOptional(typecheck.TInteger()),
					"stickiness": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
						{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
						{Name: "type", Type: typecheck.TString(), Optional: true},
						{Name: "cookie-duration", Type: typecheck.TInteger(), Optional: true},
						{Name: "cookie-name", Type: typecheck.TString(), Optional: true},
					})),
					"tags":                      typecheck.TOptional(typecheck.TMap(typecheck.TString())),
					"target-control-port":       typecheck.TOptional(typecheck.TInteger()),
					"target-type":               typecheck.TOptional(typecheck.TString()),
					"unhealthy-threshold-count": typecheck.TOptional(typecheck.TInteger()),
					"vpc-id":                    typecheck.TOptional(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"arn":                typecheck.TString(),
					"arn-suffix":         typecheck.TString(),
					"load-balancer-arns": typecheck.TList(typecheck.TString()),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(input.target-type == 'lambda')",
						Require: "(input.port == null) && (input.protocol == null) && " +
							"(input.protocol-version == null) && (input.vpc-id == null)",
						Message: "a lambda target group takes no port, protocol, protocol-version, or vpc-id",
					},
					{
						Kind:    "predicate",
						When:    "(input.target-type != 'lambda')",
						Require: "(input.port != null) && (input.protocol != null) && (input.vpc-id != null)",
						Message: "a non-lambda target group requires port, protocol, and vpc-id",
					},
					{
						Kind:    "predicate",
						When:    "(input.protocol-version != null)",
						Require: "(input.protocol == 'HTTP' || input.protocol == 'HTTPS')",
						Message: "protocol-version applies only when protocol is HTTP or HTTPS",
					},
					{
						Kind: "predicate",
						When: "(input.target-type != null)",
						Require: "(input.target-type == 'instance' || input.target-type == 'ip' || " +
							"input.target-type == 'lambda' || input.target-type == 'alb')",
						Message: "target-type must be instance, ip, lambda, or alb",
					},
					{
						Kind:    "predicate",
						When:    "(input.ip-address-type != null)",
						Require: "(input.ip-address-type == 'ipv4' || input.ip-address-type == 'ipv6')",
						Message: "ip-address-type must be ipv4 or ipv6",
					},
					{
						Kind: "predicate",
						When: "(input.load-balancing-algorithm-type != null)",
						Require: "(input.load-balancing-algorithm-type == 'round_robin' || " +
							"input.load-balancing-algorithm-type == 'least_outstanding_requests' || " +
							"input.load-balancing-algorithm-type == 'weighted_random')",
						Message: "algorithm type must be round_robin, least_outstanding_requests, or " +
							"weighted_random",
					},
					{
						Kind: "predicate",
						When: "(input.load-balancing-cross-zone-enabled != null)",
						Require: "(input.load-balancing-cross-zone-enabled == 'true' || " +
							"input.load-balancing-cross-zone-enabled == 'false' || " +
							"input.load-balancing-cross-zone-enabled == 'use_load_balancer_configuration')",
						Message: "cross-zone-enabled must be true, false, or use_load_balancer_configuration",
					},
					{
						Kind:    "predicate",
						When:    "(input.port != null)",
						Require: "(input.port == null || input.port >= 1) && (input.port == null || input.port <= 65535)",
						Message: "port must be between 1 and 65535",
					},
					{
						Kind: "predicate",
						When: "(input.target-control-port != null)",
						Require: "(input.target-control-port == null || " +
							"input.target-control-port >= 1) && (input.target-control-port == null || " +
							"input.target-control-port <= 65535)",
						Message: "target-control-port must be between 1 and 65535",
					},
					{
						Kind: "predicate",
						When: "(input.deregistration-delay != null)",
						Require: "(input.deregistration-delay == null || " +
							"input.deregistration-delay >= 0) && (input.deregistration-delay == null || " +
							"input.deregistration-delay <= 3600)",
						Message: "deregistration-delay must be between 0 and 3600",
					},
					{
						Kind: "predicate",
						When: "(input.slow-start != null)",
						Require: "(input.slow-start == null || input.slow-start >= 0) && (input.slow-start == null || " +
							"input.slow-start <= 900)",
						Message: "slow-start must be 0 or between 30 and 900",
					},
					{
						Kind: "predicate",
						When: "(input.health-check-protocol != null)",
						Require: "(input.health-check-protocol == 'HTTP' || " +
							"input.health-check-protocol == 'HTTPS' || input.health-check-protocol == 'TCP')",
						Message: "health-check-protocol must be HTTP, HTTPS, or TCP",
					},
					{
						Kind:    "predicate",
						When:    "(input.health-check-protocol == 'TCP')",
						Require: "(input.health-check-path == null) && (input.matcher == null)",
						Message: "a TCP health check takes no path or matcher",
					},
					{
						Kind: "predicate",
						When: "(input.health-check-protocol == 'TCP')",
						Require: "!(input.protocol == 'HTTP' || " +
							"input.protocol == 'HTTPS') && (input.target-type != 'lambda')",
						Message: "a TCP health check is not valid for an HTTP/HTTPS or lambda group",
					},
					{
						Kind: "predicate",
						When: "(input.health-check-interval-seconds != null)",
						Require: "(input.health-check-interval-seconds == null || " +
							"input.health-check-interval-seconds >= 5) && " +
							"(input.health-check-interval-seconds == null || input.health-check-interval-seconds <= 300)",
						Message: "health-check-interval-seconds must be between 5 and 300",
					},
					{
						Kind: "predicate",
						When: "(input.health-check-timeout-seconds != null)",
						Require: "(input.health-check-timeout-seconds == null || " +
							"input.health-check-timeout-seconds >= 2) && (input.health-check-timeout-seconds == null || " +
							"input.health-check-timeout-seconds <= 120)",
						Message: "health-check-timeout-seconds must be between 2 and 120",
					},
					{
						Kind: "predicate",
						When: "(input.healthy-threshold-count != null)",
						Require: "(input.healthy-threshold-count == null || " +
							"input.healthy-threshold-count >= 2) && (input.healthy-threshold-count == null || " +
							"input.healthy-threshold-count <= 10)",
						Message: "healthy-threshold-count must be between 2 and 10",
					},
					{
						Kind: "predicate",
						When: "(input.unhealthy-threshold-count != null)",
						Require: "(input.unhealthy-threshold-count == null || " +
							"input.unhealthy-threshold-count >= 2) && (input.unhealthy-threshold-count == null || " +
							"input.unhealthy-threshold-count <= 10)",
						Message: "unhealthy-threshold-count must be between 2 and 10",
					},
					{
						Kind:   "at-most-one-of",
						Fields: []string{"input.matcher.http-code", "input.matcher.grpc-code"},
					},
					{
						Kind:    "predicate",
						When:    "(input.matcher != null)",
						Require: "((input.matcher.http-code != null) || (input.matcher.grpc-code != null))",
						Message: "matcher requires http-code or grpc-code",
					},
					{
						Kind:    "predicate",
						When:    "(input.matcher.grpc-code != null)",
						Require: "(input.protocol-version == 'GRPC')",
						Message: "grpc-code applies only when protocol-version is GRPC",
					},
					{
						Kind:    "predicate",
						When:    "(input.stickiness != null)",
						Require: "(input.stickiness.type != null)",
						Message: "stickiness requires a type",
					},
					{
						Kind: "predicate",
						When: "(input.stickiness.type != null)",
						Require: "(input.stickiness.type == 'lb_cookie' || input.stickiness.type == 'app_cookie' || " +
							"input.stickiness.type == 'source_ip' || input.stickiness.type == 'source_ip_dest_ip' || " +
							"input.stickiness.type == 'source_ip_dest_ip_proto')",
						Message: "stickiness type must be one of the ELBv2 stickiness types",
					},
					{
						Kind: "predicate",
						When: "(input.stickiness.cookie-duration != null)",
						Require: "(input.stickiness.cookie-duration == null || " +
							"input.stickiness.cookie-duration >= 0) && (input.stickiness.cookie-duration == null || " +
							"input.stickiness.cookie-duration <= 604800)",
						Message: "stickiness cookie-duration must be between 0 and 604800",
					},
					{
						Kind:    "predicate",
						When:    "(input.stickiness.cookie-name != null)",
						Require: "(input.stickiness.type == 'app_cookie')",
						Message: "cookie-name applies only to app_cookie stickiness",
					},
				},
			},
		},
		{
			key: "target-group-attachment",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"availability-zone": typecheck.TOptional(typecheck.TString()),
					"port":              typecheck.TOptional(typecheck.TInteger()),
					"quic-server-id":    typecheck.TOptional(typecheck.TString()),
					"target-group-arn":  typecheck.TString(),
					"target-id":         typecheck.TString(),
				},
				Outputs: map[string]typecheck.Type{
					"availability-zone": typecheck.TOptional(typecheck.TString()),
					"port":              typecheck.TOptional(typecheck.TInteger()),
					"quic-server-id":    typecheck.TOptional(typecheck.TString()),
					"target-group-arn":  typecheck.TString(),
					"target-id":         typecheck.TString(),
				},
			},
		},
		{
			key: "listener",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"alpn-policy":     typecheck.TOptional(typecheck.TString()),
					"certificate-arn": typecheck.TOptional(typecheck.TString()),
					"default-action": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "type", Type: typecheck.TString(), Optional: false},
						{Name: "order", Type: typecheck.TInteger(), Optional: true},
						{Name: "target-group-arn", Type: typecheck.TString(), Optional: true},
						{Name: "forward", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "target-groups", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
								{Name: "arn", Type: typecheck.TString(), Optional: false},
								{Name: "weight", Type: typecheck.TInteger(), Optional: true},
							})), Optional: false},
							{Name: "stickiness", Type: typecheck.TObject([]typecheck.ObjectField{
								{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
								{Name: "duration-seconds", Type: typecheck.TInteger(), Optional: true},
							}), Optional: true},
						}), Optional: true},
						{Name: "redirect", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "host", Type: typecheck.TString(), Optional: true},
							{Name: "path", Type: typecheck.TString(), Optional: true},
							{Name: "port", Type: typecheck.TString(), Optional: true},
							{Name: "protocol", Type: typecheck.TString(), Optional: true},
							{Name: "query", Type: typecheck.TString(), Optional: true},
							{Name: "status-code", Type: typecheck.TString(), Optional: false},
						}), Optional: true},
						{Name: "fixed-response", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "content-type", Type: typecheck.TString(), Optional: true},
							{Name: "message-body", Type: typecheck.TString(), Optional: true},
							{Name: "status-code", Type: typecheck.TString(), Optional: false},
						}), Optional: true},
					})),
					"load-balancer-arn": typecheck.TString(),
					"port":              typecheck.TOptional(typecheck.TInteger()),
					"protocol":          typecheck.TOptional(typecheck.TString()),
					"ssl-policy":        typecheck.TOptional(typecheck.TString()),
					"tags":              typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				},
				Outputs: map[string]typecheck.Type{
					"arn":        typecheck.TString(),
					"protocol":   typecheck.TString(),
					"ssl-policy": typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind:    "predicate",
						When:    "(input.protocol == 'HTTPS' || input.protocol == 'TLS')",
						Require: "(input.ssl-policy != null) && (input.certificate-arn != null)",
						Message: "an HTTPS or TLS listener requires ssl-policy and certificate-arn",
					},
					{
						Kind: "predicate",
						When: "(input.protocol == 'HTTP' || input.protocol == 'TCP' || input.protocol == 'UDP' || " +
							"input.protocol == 'TCP_UDP' || input.protocol == 'GENEVE' || input.protocol == 'QUIC' || " +
							"input.protocol == 'TCP_QUIC')",
						Require: "(input.ssl-policy == null) && (input.certificate-arn == null) && " +
							"(input.alpn-policy == null)",
						Message: "only an HTTPS or TLS listener accepts ssl-policy, certificate-arn, or alpn-policy",
					},
					{
						Kind: "predicate",
						When: "(input.alpn-policy != null)",
						Require: "(input.alpn-policy == 'HTTP1Only' || input.alpn-policy == 'HTTP2Only' || " +
							"input.alpn-policy == 'HTTP2Optional' || input.alpn-policy == 'HTTP2Preferred' || " +
							"input.alpn-policy == 'None')",
						Message: "alpn-policy must be HTTP1Only, HTTP2Only, HTTP2Optional, HTTP2Preferred, or None",
					},
					{
						Kind:    "predicate",
						When:    "true",
						Require: "(@core.length(input.default-action) >= 1)",
						Message: "default-action must list at least one action",
					},
					{
						Kind: "predicate",
						When: "true",
						Require: "(@each.value.type == 'forward' || @each.value.type == 'redirect' || " +
							"@each.value.type == 'fixed-response')",
						Message: "an action type must be forward, redirect, or fixed-response",
						ForEach: "input.default-action",
					},
					{
						Kind: "predicate",
						When: "(@each.value.type == 'forward')",
						Require: "((@each.value.target-group-arn != null) || " +
							"(@each.value.forward != null)) && (@each.value.redirect == null) && " +
							"(@each.value.fixed-response == null)",
						Message: "a forward action takes target-group-arn or a forward block only",
						ForEach: "input.default-action",
					},
					{
						Kind: "predicate",
						When: "(@each.value.type == 'redirect')",
						Require: "(@each.value.redirect != null) && (@each.value.target-group-arn == null) && " +
							"(@each.value.forward == null) && (@each.value.fixed-response == null)",
						Message: "a redirect action takes a redirect block only",
						ForEach: "input.default-action",
					},
					{
						Kind: "predicate",
						When: "(@each.value.type == 'fixed-response')",
						Require: "(@each.value.fixed-response != null) && " +
							"(@each.value.target-group-arn == null) && (@each.value.forward == null) && " +
							"(@each.value.redirect == null)",
						Message: "a fixed-response action takes a fixed-response block only",
						ForEach: "input.default-action",
					},
					{
						Kind: "predicate",
						When: "(@each.value.redirect.status-code != null)",
						Require: "(@each.value.redirect.status-code == 'HTTP_301' || " +
							"@each.value.redirect.status-code == 'HTTP_302')",
						Message: "a redirect status-code must be HTTP_301 or HTTP_302",
						ForEach: "input.default-action",
					},
					{
						Kind: "predicate",
						When: "(@each.value.redirect.protocol != null)",
						Require: "(@each.value.redirect.protocol == '#{protocol}' || " +
							"@each.value.redirect.protocol == 'HTTP' || @each.value.redirect.protocol == 'HTTPS')",
						Message: "a redirect protocol must be HTTP, HTTPS, or #{protocol}",
						ForEach: "input.default-action",
					},
					{
						Kind: "predicate",
						When: "(@each.value.fixed-response.content-type != null)",
						Require: "(@each.value.fixed-response.content-type == 'text/plain' || " +
							"@each.value.fixed-response.content-type == 'text/css' || " +
							"@each.value.fixed-response.content-type == 'text/html' || " +
							"@each.value.fixed-response.content-type == 'application/javascript' || " +
							"@each.value.fixed-response.content-type == 'application/json')",
						Message: "a fixed-response content-type must be one of the accepted types",
						ForEach: "input.default-action",
					},
					{
						Kind: "predicate",
						When: "(@each.value.forward != null)",
						Require: "((@each.value.forward.target-groups != null) && " +
							"(@core.length(@each.value.forward.target-groups) >= 1)) && " +
							"(@each.value.forward.target-groups == null || " +
							"@core.length(@each.value.forward.target-groups) <= 5)",
						Message: "a forward block takes one to five target-groups",
						ForEach: "input.default-action",
					},
					{
						Kind: "predicate",
						When: "((@each.value.target-group-arn != null) && " +
							"(@each.value.forward != null))",
						Require: "(@each.value.forward.target-groups == null || " +
							"@core.length(@each.value.forward.target-groups) <= 1)",
						Message: "with target-group-arn set, the forward block must name " +
							"exactly one target group",
						ForEach: "input.default-action",
					},
					{
						Kind: "predicate",
						When: "(@g.value.weight != null)",
						Require: "(@g.value.weight == null || @g.value.weight >= 0) && " +
							"(@g.value.weight == null || @g.value.weight <= 999)",
						Message: "a target group weight must be between 0 and 999",
						ForEachLevels: []lang.ForEachSpecLevel{
							{Name: "@a", In: "input.default-action"},
							{Name: "@g", In: "@a.value.forward.target-groups"},
						},
					},
					{
						Kind:    "predicate",
						When:    "(@a.value.target-group-arn != null)",
						Require: "(@g.value.arn == @a.value.target-group-arn)",
						Message: "target-group-arn must match the forward block's target group",
						ForEachLevels: []lang.ForEachSpecLevel{
							{Name: "@a", In: "input.default-action"},
							{Name: "@g", In: "@a.value.forward.target-groups"},
						},
					},
					{
						Kind:    "predicate",
						When:    "(@each.value.forward.stickiness.enabled == true)",
						Require: "(@each.value.forward.stickiness.duration-seconds != null)",
						Message: "enabled forward stickiness requires duration-seconds",
						ForEach: "input.default-action",
					},
					{
						Kind: "predicate",
						When: "(@each.value.forward.stickiness.duration-seconds != null)",
						Require: "(@each.value.forward.stickiness.duration-seconds == null || " +
							"@each.value.forward.stickiness.duration-seconds >= 1) && " +
							"(@each.value.forward.stickiness.duration-seconds == null || " +
							"@each.value.forward.stickiness.duration-seconds <= 604800)",
						Message: "stickiness duration-seconds must be between 1 and 604800",
						ForEach: "input.default-action",
					},
				},
			},
		},
		{
			key: "listener-rule",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"actions": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "type", Type: typecheck.TString(), Optional: false},
						{Name: "order", Type: typecheck.TInteger(), Optional: true},
						{Name: "target-group-arn", Type: typecheck.TString(), Optional: true},
						{Name: "forward", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "target-groups", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
								{Name: "arn", Type: typecheck.TString(), Optional: true},
								{Name: "weight", Type: typecheck.TInteger(), Optional: true},
							})), Optional: false},
							{Name: "stickiness", Type: typecheck.TObject([]typecheck.ObjectField{
								{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
								{Name: "duration-seconds", Type: typecheck.TInteger(), Optional: true},
							}), Optional: true},
						}), Optional: true},
						{Name: "redirect", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "status-code", Type: typecheck.TString(), Optional: false},
							{Name: "host", Type: typecheck.TString(), Optional: true},
							{Name: "path", Type: typecheck.TString(), Optional: true},
							{Name: "port", Type: typecheck.TString(), Optional: true},
							{Name: "protocol", Type: typecheck.TString(), Optional: true},
							{Name: "query", Type: typecheck.TString(), Optional: true},
						}), Optional: true},
						{Name: "fixed-response", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "content-type", Type: typecheck.TString(), Optional: false},
							{Name: "status-code", Type: typecheck.TString(), Optional: true},
							{Name: "message-body", Type: typecheck.TString(), Optional: true},
						}), Optional: true},
					})),
					"conditions": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "host-header", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "values", Type: typecheck.TList(typecheck.TString()), Optional: false},
						}), Optional: true},
						{Name: "http-header", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "http-header-name", Type: typecheck.TString(), Optional: false},
							{Name: "values", Type: typecheck.TList(typecheck.TString()), Optional: false},
						}), Optional: true},
						{Name: "http-request-method", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "values", Type: typecheck.TList(typecheck.TString()), Optional: false},
						}), Optional: true},
						{Name: "path-pattern", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "values", Type: typecheck.TList(typecheck.TString()), Optional: false},
						}), Optional: true},
						{Name: "query-string", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "values", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
								{Name: "key", Type: typecheck.TString(), Optional: true},
								{Name: "value", Type: typecheck.TString(), Optional: true},
							})), Optional: false},
						}), Optional: true},
						{Name: "source-ip", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "values", Type: typecheck.TList(typecheck.TString()), Optional: false},
						}), Optional: true},
					})),
					"listener-arn": typecheck.TString(),
					"priority":     typecheck.TOptional(typecheck.TInteger()),
					"tags":         typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				},
				Outputs: map[string]typecheck.Type{
					"arn": typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(input.priority != null)",
						Require: "(input.priority == null || input.priority >= 1) && (input.priority == null || " +
							"input.priority <= 50000)",
						Message: "priority must be between 1 and 50000",
					},
					{
						Kind:    "predicate",
						When:    "true",
						Require: "(@core.length(input.actions) >= 1)",
						Message: "a rule requires at least one action",
					},
					{
						Kind:    "predicate",
						When:    "true",
						Require: "(@core.length(input.conditions) >= 1)",
						Message: "a rule requires at least one condition",
					},
					{
						Kind: "predicate",
						When: "true",
						Require: "(@each.value.type == 'forward' || @each.value.type == 'redirect' || " +
							"@each.value.type == 'fixed-response')",
						Message: "an action type must be forward, redirect, or fixed-response",
						ForEach: "input.actions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.type == 'forward')",
						Require: "(((@each.value.target-group-arn != null) && (@each.value.forward == null)) || " +
							"((@each.value.target-group-arn == null) && (@each.value.forward != null))) && " +
							"(@each.value.redirect == null) && (@each.value.fixed-response == null)",
						Message: "a forward action takes exactly one of target-group-arn or forward",
						ForEach: "input.actions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.type == 'redirect')",
						Require: "(@each.value.redirect != null) && (@each.value.target-group-arn == null) && " +
							"(@each.value.forward == null) && (@each.value.fixed-response == null)",
						Message: "a redirect action takes a redirect block only",
						ForEach: "input.actions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.type == 'fixed-response')",
						Require: "(@each.value.fixed-response != null) && " +
							"(@each.value.target-group-arn == null) && (@each.value.forward == null) && " +
							"(@each.value.redirect == null)",
						Message: "a fixed-response action takes a fixed-response block only",
						ForEach: "input.actions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.order != null)",
						Require: "(@each.value.order == null || " +
							"@each.value.order >= 1) && (@each.value.order == null || @each.value.order <= 50000)",
						Message: "an action order must be between 1 and 50000",
						ForEach: "input.actions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.redirect.status-code != null)",
						Require: "(@each.value.redirect.status-code == 'HTTP_301' || " +
							"@each.value.redirect.status-code == 'HTTP_302')",
						Message: "a redirect status-code must be HTTP_301 or HTTP_302",
						ForEach: "input.actions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.redirect.protocol != null)",
						Require: "(@each.value.redirect.protocol == '#{protocol}' || " +
							"@each.value.redirect.protocol == 'HTTP' || @each.value.redirect.protocol == 'HTTPS')",
						Message: "a redirect protocol must be HTTP, HTTPS, or #{protocol}",
						ForEach: "input.actions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.fixed-response.content-type != null)",
						Require: "(@each.value.fixed-response.content-type == 'text/plain' || " +
							"@each.value.fixed-response.content-type == 'text/css' || " +
							"@each.value.fixed-response.content-type == 'text/html' || " +
							"@each.value.fixed-response.content-type == 'application/javascript' || " +
							"@each.value.fixed-response.content-type == 'application/json')",
						Message: "a fixed-response content-type must be one of the accepted types",
						ForEach: "input.actions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.forward != null)",
						Require: "((@each.value.forward.target-groups != null) && " +
							"(@core.length(@each.value.forward.target-groups) >= 1)) && " +
							"(@each.value.forward.target-groups == null || " +
							"@core.length(@each.value.forward.target-groups) <= 5)",
						Message: "a forward block takes one to five target-groups",
						ForEach: "input.actions",
					},
					{
						Kind:    "predicate",
						When:    "true",
						Require: "(@g.value.arn != null)",
						Message: "a forward target-group requires an arn",
						ForEachLevels: []lang.ForEachSpecLevel{
							{Name: "@a", In: "input.actions"},
							{Name: "@g", In: "@a.value.forward.target-groups"},
						},
					},
					{
						Kind: "predicate",
						When: "(@g.value.weight != null)",
						Require: "(@g.value.weight == null || @g.value.weight >= 0) && " +
							"(@g.value.weight == null || @g.value.weight <= 999)",
						Message: "a target group weight must be between 0 and 999",
						ForEachLevels: []lang.ForEachSpecLevel{
							{Name: "@a", In: "input.actions"},
							{Name: "@g", In: "@a.value.forward.target-groups"},
						},
					},
					{
						Kind:    "predicate",
						When:    "(@each.value.forward.stickiness.enabled == true)",
						Require: "(@each.value.forward.stickiness.duration-seconds != null)",
						Message: "enabled forward stickiness requires duration-seconds",
						ForEach: "input.actions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.forward.stickiness.duration-seconds != null)",
						Require: "(@each.value.forward.stickiness.duration-seconds == null || " +
							"@each.value.forward.stickiness.duration-seconds >= 1) && " +
							"(@each.value.forward.stickiness.duration-seconds == null || " +
							"@each.value.forward.stickiness.duration-seconds <= 604800)",
						Message: "stickiness duration-seconds must be between 1 and 604800",
						ForEach: "input.actions",
					},
					{
						Kind: "at-most-one-of",
						Fields: []string{
							"input.conditions[*].host-header", "input.conditions[*].http-header",
							"input.conditions[*].http-request-method", "input.conditions[*].path-pattern",
							"input.conditions[*].query-string", "input.conditions[*].source-ip",
						},
					},
					{
						Kind: "predicate",
						When: "true",
						Require: "((@each.value.host-header != null) || (@each.value.http-header != null) || " +
							"(@each.value.http-request-method != null) || (@each.value.path-pattern != null) || " +
							"(@each.value.query-string != null) || (@each.value.source-ip != null))",
						Message: "a condition requires exactly one matcher",
						ForEach: "input.conditions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.host-header != null)",
						Require: "((@each.value.host-header.values != null) && " +
							"(@core.length(@each.value.host-header.values) >= 1))",
						Message: "host-header requires values",
						ForEach: "input.conditions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.http-header != null)",
						Require: "((@each.value.http-header.values != null) && " +
							"(@core.length(@each.value.http-header.values) >= 1))",
						Message: "http-header requires values",
						ForEach: "input.conditions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.http-request-method != null)",
						Require: "((@each.value.http-request-method.values != null) && " +
							"(@core.length(@each.value.http-request-method.values) >= 1))",
						Message: "http-request-method requires values",
						ForEach: "input.conditions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.path-pattern != null)",
						Require: "((@each.value.path-pattern.values != null) && " +
							"(@core.length(@each.value.path-pattern.values) >= 1))",
						Message: "path-pattern requires values",
						ForEach: "input.conditions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.query-string != null)",
						Require: "((@each.value.query-string.values != null) && " +
							"(@core.length(@each.value.query-string.values) >= 1))",
						Message: "query-string requires values",
						ForEach: "input.conditions",
					},
					{
						Kind: "predicate",
						When: "(@each.value.source-ip != null)",
						Require: "((@each.value.source-ip.values != null) && " +
							"(@core.length(@each.value.source-ip.values) >= 1))",
						Message: "source-ip requires values",
						ForEach: "input.conditions",
					},
					{
						Kind:    "predicate",
						When:    "true",
						Require: "(@p.value.value != null)",
						Message: "a query-string pair requires a value",
						ForEachLevels: []lang.ForEachSpecLevel{
							{Name: "@c", In: "input.conditions"},
							{Name: "@p", In: "@c.value.query-string.values"},
						},
					},
				},
			},
		},
		{
			key: "listener-certificate",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"certificate-arn": typecheck.TString(),
					"listener-arn":    typecheck.TString(),
				},
				Outputs: map[string]typecheck.Type{},
			},
		},
	}
	for _, tt := range tests {
		require.Contains(t, schema.Resources, tt.key)
		assertTypeSchemaEqual(t, tt.want, schema.Resources[tt.key],
			"schema mismatch for %s", tt.key)
	}
}
