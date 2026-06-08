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
	"github.com/cloudboss/unobin-library-aws/internal/service/route53"
)

// TestLibraryRegistersRoute53Resources checks the runtime registration: every
// Route 53 resource is present under Resources and dispatches to its output
// type.
func TestLibraryRegistersRoute53Resources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"route53-hosted-zone": reflect.TypeFor[*route53.HostedZoneOutput](),
		"route53-record-set":  reflect.TypeFor[*route53.RecordSetOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestRoute53Schemas asserts the whole derived TypeSchema for each Route 53
// resource: input and output field types, the cross-field and enum constraints
// each Constraints method declares, and the optional defaults.
func TestRoute53Schemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	cases := map[string]*runtime.TypeSchema{
		"route53-hosted-zone": {
			Inputs: map[string]typecheck.Type{
				"comment":           typecheck.TOptional(typecheck.TString()),
				"delegation-set-id": typecheck.TOptional(typecheck.TString()),
				"force-destroy":     typecheck.TOptional(typecheck.TBoolean()),
				"name":              typecheck.TString(),
				"tags":              typecheck.TMap(typecheck.TString()),
				"vpcs": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "vpc-id", Type: typecheck.TString()},
					{Name: "vpc-region", Type: typecheck.TString(), Optional: true},
				})),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                 typecheck.TString(),
				"name":                typecheck.TString(),
				"name-servers":        typecheck.TList(typecheck.TString()),
				"primary-name-server": typecheck.TString(),
				"zone-id":             typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "at-most-one-of",
					Fields: []string{"var.delegation-set-id", "var.vpcs"},
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "((@each.value.vpc-id != null) && " +
						"(@core.length(@each.value.vpc-id) >= 1))",
					Message: "a vpc association requires a vpc-id",
					ForEach: "var.vpcs",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.vpcs", Optional: true},
				{Field: "var.tags", Optional: true},
			},
		},
		"route53-record-set": {
			Inputs: map[string]typecheck.Type{
				"alias": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "name", Type: typecheck.TString()},
					{Name: "zone-id", Type: typecheck.TString()},
					{Name: "evaluate-target-health", Type: typecheck.TBoolean()},
				})),
				"failover-routing-policy": typecheck.TOptional(
					typecheck.TObject([]typecheck.ObjectField{
						{Name: "type", Type: typecheck.TString()},
					})),
				"geolocation-routing-policy": typecheck.TOptional(
					typecheck.TObject([]typecheck.ObjectField{
						{Name: "continent-code", Type: typecheck.TString(), Optional: true},
						{Name: "country-code", Type: typecheck.TString(), Optional: true},
						{Name: "subdivision-code", Type: typecheck.TString(), Optional: true},
					})),
				"health-check-id": typecheck.TOptional(typecheck.TString()),
				"latency-routing-policy": typecheck.TOptional(
					typecheck.TObject([]typecheck.ObjectField{
						{Name: "region", Type: typecheck.TString()},
					})),
				"multivalue-answer-routing-policy": typecheck.TOptional(typecheck.TBoolean()),
				"name":                             typecheck.TString(),
				"records":                          typecheck.TList(typecheck.TString()),
				"set-identifier":                   typecheck.TOptional(typecheck.TString()),
				"ttl":                              typecheck.TOptional(typecheck.TInteger()),
				"type":                             typecheck.TString(),
				"weighted-routing-policy": typecheck.TOptional(
					typecheck.TObject([]typecheck.ObjectField{
						{Name: "weight", Type: typecheck.TInteger()},
					})),
				"zone-id": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"fqdn":           typecheck.TString(),
				"name":           typecheck.TString(),
				"set-identifier": typecheck.TString(),
				"type":           typecheck.TString(),
				"zone-id":        typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "true",
					Require: "((var.zone-id != null) && (@core.length(var.zone-id) >= 1))",
				},
				{
					Kind:   "exactly-one-of",
					Fields: []string{"var.alias", "var.records"},
				},
				{
					Kind:   "forbidden-with",
					Fields: []string{"var.ttl", "var.alias"},
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.ttl", "var.records"},
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"var.weighted-routing-policy",
						"var.latency-routing-policy",
						"var.failover-routing-policy",
						"var.geolocation-routing-policy",
						"var.multivalue-answer-routing-policy",
					},
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.weighted-routing-policy", "var.set-identifier"},
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.latency-routing-policy", "var.set-identifier"},
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.failover-routing-policy", "var.set-identifier"},
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.geolocation-routing-policy", "var.set-identifier"},
				},
				{
					Kind: "required-with",
					Fields: []string{
						"var.multivalue-answer-routing-policy",
						"var.set-identifier",
					},
				},
				{
					Kind: "predicate",
					When: "(var.failover-routing-policy != null)",
					Require: "(var.failover-routing-policy.type == 'PRIMARY' || " +
						"var.failover-routing-policy.type == 'SECONDARY')",
					Message: "failover-routing-policy type must be PRIMARY or SECONDARY",
				},
				{
					Kind: "predicate",
					When: "(var.latency-routing-policy != null)",
					Require: "(var.latency-routing-policy.region == 'us-east-1' || " +
						"var.latency-routing-policy.region == 'us-east-2' || " +
						"var.latency-routing-policy.region == 'us-west-1' || " +
						"var.latency-routing-policy.region == 'us-west-2' || " +
						"var.latency-routing-policy.region == 'ca-central-1' || " +
						"var.latency-routing-policy.region == 'ca-west-1' || " +
						"var.latency-routing-policy.region == 'eu-west-1' || " +
						"var.latency-routing-policy.region == 'eu-west-2' || " +
						"var.latency-routing-policy.region == 'eu-west-3' || " +
						"var.latency-routing-policy.region == 'eu-central-1' || " +
						"var.latency-routing-policy.region == 'eu-central-2' || " +
						"var.latency-routing-policy.region == 'eu-north-1' || " +
						"var.latency-routing-policy.region == 'eu-south-1' || " +
						"var.latency-routing-policy.region == 'eu-south-2' || " +
						"var.latency-routing-policy.region == 'ap-east-1' || " +
						"var.latency-routing-policy.region == 'ap-east-2' || " +
						"var.latency-routing-policy.region == 'ap-south-1' || " +
						"var.latency-routing-policy.region == 'ap-south-2' || " +
						"var.latency-routing-policy.region == 'ap-southeast-1' || " +
						"var.latency-routing-policy.region == 'ap-southeast-2' || " +
						"var.latency-routing-policy.region == 'ap-southeast-3' || " +
						"var.latency-routing-policy.region == 'ap-southeast-4' || " +
						"var.latency-routing-policy.region == 'ap-southeast-5' || " +
						"var.latency-routing-policy.region == 'ap-southeast-6' || " +
						"var.latency-routing-policy.region == 'ap-southeast-7' || " +
						"var.latency-routing-policy.region == 'ap-northeast-1' || " +
						"var.latency-routing-policy.region == 'ap-northeast-2' || " +
						"var.latency-routing-policy.region == 'ap-northeast-3' || " +
						"var.latency-routing-policy.region == 'sa-east-1' || " +
						"var.latency-routing-policy.region == 'me-south-1' || " +
						"var.latency-routing-policy.region == 'me-central-1' || " +
						"var.latency-routing-policy.region == 'af-south-1' || " +
						"var.latency-routing-policy.region == 'il-central-1' || " +
						"var.latency-routing-policy.region == 'mx-central-1' || " +
						"var.latency-routing-policy.region == 'cn-north-1' || " +
						"var.latency-routing-policy.region == 'cn-northwest-1' || " +
						"var.latency-routing-policy.region == 'us-gov-east-1' || " +
						"var.latency-routing-policy.region == 'us-gov-west-1' || " +
						"var.latency-routing-policy.region == 'eusc-de-east-1')",
					Message: "latency-routing-policy region must be a valid AWS region",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.type == 'A' || var.type == 'AAAA' || " +
						"var.type == 'CAA' || var.type == 'CNAME' || " +
						"var.type == 'DS' || var.type == 'MX' || " +
						"var.type == 'NAPTR' || var.type == 'NS' || " +
						"var.type == 'PTR' || var.type == 'SOA' || " +
						"var.type == 'SPF' || var.type == 'SRV' || " +
						"var.type == 'TXT' || var.type == 'TLSA' || " +
						"var.type == 'SSHFP' || var.type == 'SVCB' || " +
						"var.type == 'HTTPS')",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.records", Optional: true},
			},
		},
	}

	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, want, schema.Resources[key])
		})
	}
}
