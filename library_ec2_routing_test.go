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
	"github.com/cloudboss/unobin-library-aws/internal/service/ec2"
)

// TestLibraryRegistersVpcRouting checks the runtime registration: the seven
// VPC-routing resources are present under Resources and dispatch to their
// output types.
func TestLibraryRegistersVpcRouting(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "ec2-internet-gateway")
	assert.Equal(t, reflect.TypeFor[*ec2.InternetGatewayOutput](),
		lib.Resources["ec2-internet-gateway"].OutputType())
	require.Contains(t, lib.Resources, "ec2-route-table")
	assert.Equal(t, reflect.TypeFor[*ec2.RouteTableOutput](),
		lib.Resources["ec2-route-table"].OutputType())
	require.Contains(t, lib.Resources, "ec2-route")
	assert.Equal(t, reflect.TypeFor[*ec2.RouteOutput](),
		lib.Resources["ec2-route"].OutputType())
	require.Contains(t, lib.Resources, "ec2-route-table-association")
	assert.Equal(t, reflect.TypeFor[*ec2.RouteTableAssociationOutput](),
		lib.Resources["ec2-route-table-association"].OutputType())
	require.Contains(t, lib.Resources, "ec2-eip")
	assert.Equal(t, reflect.TypeFor[*ec2.EipOutput](),
		lib.Resources["ec2-eip"].OutputType())
	require.Contains(t, lib.Resources, "ec2-nat-gateway")
	assert.Equal(t, reflect.TypeFor[*ec2.NatGatewayOutput](),
		lib.Resources["ec2-nat-gateway"].OutputType())
	require.Contains(t, lib.Resources, "ec2-vpc-endpoint")
	assert.Equal(t, reflect.TypeFor[*ec2.VpcEndpointOutput](),
		lib.Resources["ec2-vpc-endpoint"].OutputType())
}

// TestVpcRoutingSchemas asserts the whole derived TypeSchema -- input and
// output field types, the cross-field constraints, and the declared optional
// defaults -- for each VPC-routing resource. normalizeSchema sorts nested
// object fields so the comparison is stable despite goschema varying their
// order.
func TestVpcRoutingSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	tests := []struct {
		key  string
		want *runtime.TypeSchema
	}{
		{
			key: "ec2-internet-gateway",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"tags":   typecheck.TMap(typecheck.TString()),
					"vpc-id": typecheck.TOptional(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"internet-gateway-id": typecheck.TString(),
					"owner-id":            typecheck.TString(),
					"vpc-id":              typecheck.TString(),
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "ec2-route-table",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"tags":   typecheck.TMap(typecheck.TString()),
					"vpc-id": typecheck.TString(),
				},
				Outputs: map[string]typecheck.Type{
					"owner-id":       typecheck.TString(),
					"route-table-id": typecheck.TString(),
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "ec2-route",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"carrier-gateway-id":          typecheck.TOptional(typecheck.TString()),
					"core-network-arn":            typecheck.TOptional(typecheck.TString()),
					"destination-cidr-block":      typecheck.TOptional(typecheck.TString()),
					"destination-ipv6-cidr-block": typecheck.TOptional(typecheck.TString()),
					"destination-prefix-list-id":  typecheck.TOptional(typecheck.TString()),
					"egress-only-gateway-id":      typecheck.TOptional(typecheck.TString()),
					"gateway-id":                  typecheck.TOptional(typecheck.TString()),
					"local-gateway-id":            typecheck.TOptional(typecheck.TString()),
					"nat-gateway-id":              typecheck.TOptional(typecheck.TString()),
					"network-interface-id":        typecheck.TOptional(typecheck.TString()),
					"route-table-id":              typecheck.TString(),
					"transit-gateway-id":          typecheck.TOptional(typecheck.TString()),
					"vpc-endpoint-id":             typecheck.TOptional(typecheck.TString()),
					"vpc-peering-connection-id":   typecheck.TOptional(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"state": typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "exactly-one-of",
						Fields: []string{
							"var.destination-cidr-block",
							"var.destination-ipv6-cidr-block",
							"var.destination-prefix-list-id",
						},
					},
					{
						Kind: "exactly-one-of",
						Fields: []string{
							"var.carrier-gateway-id",
							"var.core-network-arn",
							"var.egress-only-gateway-id",
							"var.gateway-id",
							"var.local-gateway-id",
							"var.nat-gateway-id",
							"var.network-interface-id",
							"var.transit-gateway-id",
							"var.vpc-endpoint-id",
							"var.vpc-peering-connection-id",
						},
					},
					{
						Kind: "forbidden-with",
						Fields: []string{
							"var.carrier-gateway-id",
							"var.destination-ipv6-cidr-block",
						},
					},
					{
						Kind: "forbidden-with",
						Fields: []string{
							"var.egress-only-gateway-id",
							"var.destination-cidr-block",
						},
					},
					{
						Kind: "forbidden-with",
						Fields: []string{
							"var.vpc-endpoint-id",
							"var.destination-prefix-list-id",
						},
					},
					{
						Kind:    "predicate",
						When:    "(var.gateway-id != null)",
						Require: "(var.gateway-id != 'local')",
						Message: "gateway-id cannot be local; the local route is not managed here",
					},
				},
			},
		},
		{
			key: "ec2-route-table-association",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"gateway-id":     typecheck.TOptional(typecheck.TString()),
					"route-table-id": typecheck.TString(),
					"subnet-id":      typecheck.TOptional(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"route-table-association-id": typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "exactly-one-of",
						Fields: []string{
							"var.subnet-id",
							"var.gateway-id",
						},
					},
				},
			},
		},
		{
			key: "ec2-eip",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"address":                  typecheck.TOptional(typecheck.TString()),
					"customer-owned-ipv4-pool": typecheck.TOptional(typecheck.TString()),
					"domain":                   typecheck.TOptional(typecheck.TString()),
					"ipam-pool-id":             typecheck.TOptional(typecheck.TString()),
					"network-border-group":     typecheck.TOptional(typecheck.TString()),
					"public-ipv4-pool":         typecheck.TOptional(typecheck.TString()),
					"tags":                     typecheck.TMap(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"allocation-id":  typecheck.TString(),
					"association-id": typecheck.TString(),
					"private-ip":     typecheck.TString(),
					"public-ip":      typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind:    "predicate",
						When:    "(var.domain != null)",
						Require: "(var.domain == 'vpc' || var.domain == 'standard')",
						Message: "domain must be vpc or standard",
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "ec2-nat-gateway",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"allocation-id":     typecheck.TOptional(typecheck.TString()),
					"connectivity-type": typecheck.TOptional(typecheck.TString()),
					"private-ip":        typecheck.TOptional(typecheck.TString()),
					"secondary-allocation-ids": typecheck.TList(
						typecheck.TString()),
					"secondary-private-ip-address-count": typecheck.TOptional(
						typecheck.TInteger()),
					"secondary-private-ip-addresses": typecheck.TList(
						typecheck.TString()),
					"subnet-id": typecheck.TString(),
					"tags":      typecheck.TMap(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"nat-gateway-id":       typecheck.TString(),
					"network-interface-id": typecheck.TString(),
					"private-ip":           typecheck.TString(),
					"public-ip":            typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(var.connectivity-type != null)",
						Require: "(var.connectivity-type == 'public' || " +
							"var.connectivity-type == 'private')",
						Message: "connectivity-type must be public or private",
					},
					{
						Kind: "predicate",
						When: "((var.connectivity-type == 'public') || " +
							"(var.connectivity-type == null))",
						Require: "(var.allocation-id != null)",
						Message: "allocation-id is required for a public NAT gateway",
					},
					{
						Kind:    "predicate",
						When:    "(var.connectivity-type == 'private')",
						Require: "(var.allocation-id == null)",
						Message: "allocation-id is not supported with connectivity-type private",
					},
					{
						Kind:    "predicate",
						When:    "(var.connectivity-type == 'private')",
						Require: "(var.secondary-allocation-ids == null)",
						Message: "secondary-allocation-ids is not supported with " +
							"connectivity-type private",
					},
					{
						Kind:    "predicate",
						When:    "(var.secondary-private-ip-address-count != null)",
						Require: "(var.connectivity-type == 'private')",
						Message: "secondary-private-ip-address-count is supported only with " +
							"connectivity-type private",
					},
					{
						Kind: "at-most-one-of",
						Fields: []string{
							"var.secondary-private-ip-address-count",
							"var.secondary-private-ip-addresses",
						},
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.secondary-allocation-ids", Optional: true},
					{Field: "var.secondary-private-ip-addresses", Optional: true},
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "ec2-vpc-endpoint",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"dns-options": typecheck.TOptional(typecheck.TObject(
						[]typecheck.ObjectField{
							{
								Name:     "dns-record-ip-type",
								Type:     typecheck.TString(),
								Optional: true,
							},
							{
								Name:     "private-dns-only-for-inbound-resolver-endpoint",
								Type:     typecheck.TBoolean(),
								Optional: true,
							},
						})),
					"ip-address-type":     typecheck.TOptional(typecheck.TString()),
					"policy":              typecheck.TOptional(typecheck.TString()),
					"private-dns-enabled": typecheck.TOptional(typecheck.TBoolean()),
					"route-table-ids":     typecheck.TList(typecheck.TString()),
					"security-group-ids":  typecheck.TList(typecheck.TString()),
					"service-name":        typecheck.TString(),
					"subnet-ids":          typecheck.TList(typecheck.TString()),
					"tags":                typecheck.TMap(typecheck.TString()),
					"vpc-endpoint-type":   typecheck.TOptional(typecheck.TString()),
					"vpc-id":              typecheck.TString(),
				},
				Outputs: map[string]typecheck.Type{
					"cidr-blocks": typecheck.TList(typecheck.TString()),
					"dns-entries": typecheck.TList(typecheck.TObject(
						[]typecheck.ObjectField{
							{Name: "dns-name", Type: typecheck.TString()},
							{Name: "hosted-zone-id", Type: typecheck.TString()},
						})),
					"network-interface-ids": typecheck.TList(typecheck.TString()),
					"owner-id":              typecheck.TString(),
					"policy":                typecheck.TString(),
					"prefix-list-id":        typecheck.TString(),
					"state":                 typecheck.TString(),
					"vpc-endpoint-id":       typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(var.vpc-endpoint-type != null)",
						Require: "(var.vpc-endpoint-type == 'Gateway' || " +
							"var.vpc-endpoint-type == 'Interface' || " +
							"var.vpc-endpoint-type == 'GatewayLoadBalancer')",
						Message: "vpc-endpoint-type must be Gateway, Interface, or " +
							"GatewayLoadBalancer",
					},
					{
						Kind: "predicate",
						When: "(var.ip-address-type != null)",
						Require: "(var.ip-address-type == 'ipv4' || " +
							"var.ip-address-type == 'dualstack' || " +
							"var.ip-address-type == 'ipv6')",
						Message: "ip-address-type must be ipv4, dualstack, or ipv6",
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.route-table-ids", Optional: true},
					{Field: "var.security-group-ids", Optional: true},
					{Field: "var.subnet-ids", Optional: true},
					{Field: "var.tags", Optional: true},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			require.Contains(t, schema.Resources, tt.key)
			assert.Equal(t, normalizeSchema(tt.want), normalizeSchema(schema.Resources[tt.key]),
				tt.key)
		})
	}
}
