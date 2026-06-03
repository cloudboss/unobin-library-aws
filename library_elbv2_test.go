package library_test

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	library "github.com/cloudboss/unobin-library-aws"
	"github.com/cloudboss/unobin-library-aws/internal/service/elbv2"
)

// TestLibraryRegistersElbv2 checks the runtime registration: the five
// load-balancing resources are present under Resources and dispatch to their
// output types.
func TestLibraryRegistersElbv2(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "elbv2-load-balancer")
	assert.Equal(t, reflect.TypeFor[*elbv2.LoadBalancerOutput](),
		lib.Resources["elbv2-load-balancer"].OutputType())
	require.Contains(t, lib.Resources, "elbv2-target-group")
	assert.Equal(t, reflect.TypeFor[*elbv2.TargetGroupOutput](),
		lib.Resources["elbv2-target-group"].OutputType())
	require.Contains(t, lib.Resources, "elbv2-listener")
	assert.Equal(t, reflect.TypeFor[*elbv2.ListenerOutput](),
		lib.Resources["elbv2-listener"].OutputType())
	require.Contains(t, lib.Resources, "elbv2-listener-rule")
	assert.Equal(t, reflect.TypeFor[*elbv2.ListenerRuleOutput](),
		lib.Resources["elbv2-listener-rule"].OutputType())
	require.Contains(t, lib.Resources, "elbv2-listener-certificate")
	assert.Equal(t, reflect.TypeFor[*elbv2.ListenerCertificateOutput](),
		lib.Resources["elbv2-listener-certificate"].OutputType())
}

// TestElbv2Schemas asserts the whole derived TypeSchema -- input and output
// field types, sensitivity, and the cross-field constraints -- for each
// load-balancing resource. normalizeSchema sorts nested object fields so the
// comparison is stable despite goschema varying their order.
func TestElbv2Schemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	tests := []struct {
		key  string
		want *runtime.TypeSchema
	}{
		{
			key: "elbv2-load-balancer",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"access-logs": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
						{Name: "bucket", Type: typecheck.TString(), Optional: true},
						{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
						{Name: "prefix", Type: typecheck.TString(), Optional: true},
					})),
					"client-keep-alive": typecheck.TOptional(typecheck.TInteger()),
					"connection-logs": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
						{Name: "bucket", Type: typecheck.TString(), Optional: true},
						{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
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
					"security-groups":                             typecheck.TList(typecheck.TString()),
					"subnet-mappings": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "allocation-id", Type: typecheck.TString(), Optional: true},
						{Name: "ipv6-address", Type: typecheck.TString(), Optional: true},
						{Name: "private-ipv4-address", Type: typecheck.TString(), Optional: true},
						{Name: "source-nat-ipv6-prefix", Type: typecheck.TString(), Optional: true},
						{Name: "subnet-id", Type: typecheck.TString(), Optional: false},
					})),
					"subnets":                    typecheck.TList(typecheck.TString()),
					"tags":                       typecheck.TMap(typecheck.TString()),
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
						Kind:   "exactly-one-of",
						Fields: []string{"subnets", "subnet-mappings"},
					},
					{
						Kind: "predicate",
						When: "(var.load-balancer-type != null)",
						Require: "(var.load-balancer-type == 'application' || " +
							"var.load-balancer-type == 'network' || " +
							"var.load-balancer-type == 'gateway')",
						Message: "load-balancer-type must be application, network, or gateway",
					},
					{
						Kind: "predicate",
						When: "(var.ip-address-type != null)",
						Require: "(var.ip-address-type == 'ipv4' || " +
							"var.ip-address-type == 'dualstack' || " +
							"var.ip-address-type == 'dualstack-without-public-ipv4')",
						Message: "ip-address-type must be ipv4, dualstack, or " +
							"dualstack-without-public-ipv4",
					},
					{
						Kind: "predicate",
						When: "(var.desync-mitigation-mode != null)",
						Require: "(var.desync-mitigation-mode == 'monitor' || " +
							"var.desync-mitigation-mode == 'defensive' || " +
							"var.desync-mitigation-mode == 'strictest')",
						Message: "desync-mitigation-mode must be monitor, defensive, or strictest",
					},
					{
						Kind: "predicate",
						When: "(var.xff-header-processing-mode != null)",
						Require: "(var.xff-header-processing-mode == 'append' || " +
							"var.xff-header-processing-mode == 'preserve' || " +
							"var.xff-header-processing-mode == 'remove')",
						Message: "xff-header-processing-mode must be append, preserve, or remove",
					},
					{
						Kind: "predicate",
						When: "(var.dns-record-client-routing-policy != null)",
						Require: "(var.dns-record-client-routing-policy == " +
							"'availability_zone_affinity' || " +
							"var.dns-record-client-routing-policy == " +
							"'partial_availability_zone_affinity' || " +
							"var.dns-record-client-routing-policy == 'any_availability_zone')",
						Message: "dns-record-client-routing-policy must be a valid routing policy",
					},
				},
			},
		},
		{
			key: "elbv2-target-group",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"connection-termination": typecheck.TOptional(typecheck.TBoolean()),
					"deregistration-delay":   typecheck.TOptional(typecheck.TInteger()),
					"health-check": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
						{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
						{Name: "grpc-matcher", Type: typecheck.TString(), Optional: true},
						{Name: "healthy-threshold", Type: typecheck.TInteger(), Optional: true},
						{Name: "interval-seconds", Type: typecheck.TInteger(), Optional: true},
						{Name: "matcher", Type: typecheck.TString(), Optional: true},
						{Name: "path", Type: typecheck.TString(), Optional: true},
						{Name: "port", Type: typecheck.TString(), Optional: true},
						{Name: "protocol", Type: typecheck.TString(), Optional: true},
						{Name: "timeout-seconds", Type: typecheck.TInteger(), Optional: true},
						{Name: "unhealthy-threshold", Type: typecheck.TInteger(), Optional: true},
					})),
					"ip-address-type":                    typecheck.TOptional(typecheck.TString()),
					"lambda-multi-value-headers-enabled": typecheck.TOptional(typecheck.TBoolean()),
					"load-balancing-algorithm-type":      typecheck.TOptional(typecheck.TString()),
					"load-balancing-cross-zone-enabled":  typecheck.TOptional(typecheck.TString()),
					"name":                               typecheck.TString(),
					"port":                               typecheck.TOptional(typecheck.TInteger()),
					"preserve-client-ip":                 typecheck.TOptional(typecheck.TBoolean()),
					"protocol":                           typecheck.TOptional(typecheck.TString()),
					"protocol-version":                   typecheck.TOptional(typecheck.TString()),
					"proxy-protocol-v2":                  typecheck.TOptional(typecheck.TBoolean()),
					"slow-start":                         typecheck.TOptional(typecheck.TInteger()),
					"stickiness": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
						{Name: "cookie-duration", Type: typecheck.TInteger(), Optional: true},
						{Name: "cookie-name", Type: typecheck.TString(), Optional: true},
						{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
						{Name: "type", Type: typecheck.TString(), Optional: true},
					})),
					"tags":                typecheck.TMap(typecheck.TString()),
					"target-control-port": typecheck.TOptional(typecheck.TInteger()),
					"target-type":         typecheck.TOptional(typecheck.TString()),
					"vpc-id":              typecheck.TOptional(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"arn":                typecheck.TString(),
					"arn-suffix":         typecheck.TString(),
					"load-balancer-arns": typecheck.TList(typecheck.TString()),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(var.target-type == 'lambda')",
						Require: "(var.port == null) && (var.protocol == null) && " +
							"(var.protocol-version == null) && (var.vpc-id == null)",
						Message: "a lambda target group takes no port, protocol, " +
							"protocol-version, or vpc-id",
					},
					{
						Kind: "predicate",
						When: "(var.target-type != 'lambda')",
						Require: "(var.port != null) && (var.protocol != null) && " +
							"(var.vpc-id != null)",
						Message: "a non-lambda target group requires port, protocol, and vpc-id",
					},
					{
						Kind:    "predicate",
						When:    "(var.protocol-version != null)",
						Require: "(var.protocol == 'HTTP' || var.protocol == 'HTTPS')",
						Message: "protocol-version applies only when protocol is HTTP or HTTPS",
					},
					{
						Kind: "predicate",
						When: "(var.target-type != null)",
						Require: "(var.target-type == 'instance' || var.target-type == 'ip' || " +
							"var.target-type == 'lambda' || var.target-type == 'alb')",
						Message: "target-type must be instance, ip, lambda, or alb",
					},
					{
						Kind:    "predicate",
						When:    "(var.ip-address-type != null)",
						Require: "(var.ip-address-type == 'ipv4' || var.ip-address-type == 'ipv6')",
						Message: "ip-address-type must be ipv4 or ipv6",
					},
					{
						Kind: "predicate",
						When: "(var.load-balancing-algorithm-type != null)",
						Require: "(var.load-balancing-algorithm-type == 'round_robin' || " +
							"var.load-balancing-algorithm-type == " +
							"'least_outstanding_requests' || " +
							"var.load-balancing-algorithm-type == 'weighted_random')",
					},
					{
						Kind: "predicate",
						When: "(var.load-balancing-cross-zone-enabled != null)",
						Require: "(var.load-balancing-cross-zone-enabled == 'true' || " +
							"var.load-balancing-cross-zone-enabled == 'false' || " +
							"var.load-balancing-cross-zone-enabled == " +
							"'use_load_balancer_configuration')",
					},
					{
						Kind: "predicate",
						When: "(var.port != null)",
						Require: "(var.port == null || var.port >= 1) && (var.port == null || " +
							"var.port <= 65535)",
						Message: "port must be between 1 and 65535",
					},
					{
						Kind: "predicate",
						When: "(var.target-control-port != null)",
						Require: "(var.target-control-port == null || " +
							"var.target-control-port >= 1) && " +
							"(var.target-control-port == null || " +
							"var.target-control-port <= 65535)",
						Message: "target-control-port must be between 1 and 65535",
					},
					{
						Kind: "predicate",
						When: "(var.deregistration-delay != null)",
						Require: "(var.deregistration-delay == null || " +
							"var.deregistration-delay >= 0) && " +
							"(var.deregistration-delay == null || " +
							"var.deregistration-delay <= 3600)",
						Message: "deregistration-delay must be between 0 and 3600",
					},
					{
						Kind: "predicate",
						When: "(var.slow-start != null)",
						Require: "(var.slow-start == null || var.slow-start >= 0) && " +
							"(var.slow-start == null || var.slow-start <= 900)",
						Message: "slow-start must be 0 or between 30 and 900",
					},
				},
			},
		},
		{
			key: "elbv2-listener",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"alpn-policy":     typecheck.TOptional(typecheck.TString()),
					"certificate-arn": typecheck.TOptional(typecheck.TString()),
					"default-action": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "fixed-response", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "content-type", Type: typecheck.TString(), Optional: true},
							{Name: "message-body", Type: typecheck.TString(), Optional: true},
							{Name: "status-code", Type: typecheck.TString(), Optional: false},
						}), Optional: true},
						{Name: "forward", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "stickiness", Type: typecheck.TObject([]typecheck.ObjectField{
								{Name: "duration-seconds", Type: typecheck.TInteger(), Optional: true},
								{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
							}), Optional: true},
							{Name: "target-groups", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
								{Name: "arn", Type: typecheck.TString(), Optional: false},
								{Name: "weight", Type: typecheck.TInteger(), Optional: true},
							})), Optional: false},
						}), Optional: true},
						{Name: "order", Type: typecheck.TInteger(), Optional: true},
						{Name: "redirect", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "host", Type: typecheck.TString(), Optional: true},
							{Name: "path", Type: typecheck.TString(), Optional: true},
							{Name: "port", Type: typecheck.TString(), Optional: true},
							{Name: "protocol", Type: typecheck.TString(), Optional: true},
							{Name: "query", Type: typecheck.TString(), Optional: true},
							{Name: "status-code", Type: typecheck.TString(), Optional: false},
						}), Optional: true},
						{Name: "target-group-arn", Type: typecheck.TString(), Optional: true},
						{Name: "type", Type: typecheck.TString(), Optional: false},
					})),
					"load-balancer-arn": typecheck.TString(),
					"port":              typecheck.TOptional(typecheck.TInteger()),
					"protocol":          typecheck.TOptional(typecheck.TString()),
					"ssl-policy":        typecheck.TOptional(typecheck.TString()),
					"tags":              typecheck.TMap(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"arn":        typecheck.TString(),
					"protocol":   typecheck.TString(),
					"ssl-policy": typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind:    "predicate",
						When:    "(var.protocol == 'HTTPS' || var.protocol == 'TLS')",
						Require: "(var.ssl-policy != null) && (var.certificate-arn != null)",
						Message: "an HTTPS or TLS listener requires ssl-policy and certificate-arn",
					},
					{
						Kind: "predicate",
						When: "(var.protocol == 'HTTP' || var.protocol == 'TCP' || " +
							"var.protocol == 'UDP' || var.protocol == 'TCP_UDP' || " +
							"var.protocol == 'GENEVE' || var.protocol == 'QUIC' || " +
							"var.protocol == 'TCP_QUIC')",
						Require: "(var.ssl-policy == null) && (var.certificate-arn == null) && " +
							"(var.alpn-policy == null)",
						Message: "only an HTTPS or TLS listener accepts ssl-policy, " +
							"certificate-arn, or alpn-policy",
					},
					{
						Kind: "predicate",
						When: "(var.alpn-policy != null)",
						Require: "(var.alpn-policy == 'HTTP1Only' || " +
							"var.alpn-policy == 'HTTP2Only' || " +
							"var.alpn-policy == 'HTTP2Optional' || " +
							"var.alpn-policy == 'HTTP2Preferred' || " +
							"var.alpn-policy == 'None')",
						Message: "alpn-policy must be HTTP1Only, HTTP2Only, HTTP2Optional, " +
							"HTTP2Preferred, or None",
					},
				},
			},
		},
		{
			key: "elbv2-listener-rule",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"actions": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "fixed-response", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "content-type", Type: typecheck.TString(), Optional: false},
							{Name: "message-body", Type: typecheck.TString(), Optional: true},
							{Name: "status-code", Type: typecheck.TString(), Optional: true},
						}), Optional: true},
						{Name: "forward", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "stickiness", Type: typecheck.TObject([]typecheck.ObjectField{
								{Name: "duration-seconds", Type: typecheck.TInteger(), Optional: true},
								{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
							}), Optional: true},
							{Name: "target-groups", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
								{Name: "arn", Type: typecheck.TString(), Optional: true},
								{Name: "weight", Type: typecheck.TInteger(), Optional: true},
							})), Optional: false},
						}), Optional: true},
						{Name: "order", Type: typecheck.TInteger(), Optional: true},
						{Name: "redirect", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "host", Type: typecheck.TString(), Optional: true},
							{Name: "path", Type: typecheck.TString(), Optional: true},
							{Name: "port", Type: typecheck.TString(), Optional: true},
							{Name: "protocol", Type: typecheck.TString(), Optional: true},
							{Name: "query", Type: typecheck.TString(), Optional: true},
							{Name: "status-code", Type: typecheck.TString(), Optional: false},
						}), Optional: true},
						{Name: "target-group-arn", Type: typecheck.TString(), Optional: true},
						{Name: "type", Type: typecheck.TString(), Optional: false},
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
					"tags":         typecheck.TMap(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"arn": typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(var.priority != null)",
						Require: "(var.priority == null || var.priority >= 1) && " +
							"(var.priority == null || var.priority <= 50000)",
						Message: "priority must be between 1 and 50000",
					},
				},
			},
		},
		{
			key: "elbv2-listener-certificate",
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
		assert.Equal(t, normalizeSchema(tt.want), normalizeSchema(schema.Resources[tt.key]),
			"schema mismatch for %s", tt.key)
	}
}
