package route53

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/route53"
)

// TestLibraryRegistersRoute53Resources checks the runtime registration: every
// Route 53 resource is present under Resources and dispatches to its output
// type.
func TestLibraryRegistersRoute53Resources(t *testing.T) {
	lib := Library()
	cases := map[string]reflect.Type{
		"hosted-zone": reflect.TypeFor[*svc.HostedZoneOutput](),
		"record-set":  reflect.TypeFor[*svc.RecordSetOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestLibraryRegistersRoute53DataSources checks the runtime registration of the
// Route 53 data sources under DataSources.
func TestLibraryRegistersRoute53DataSources(t *testing.T) {
	lib := Library()
	require.Contains(t, lib.DataSources, "zone")
	assert.Equal(t, reflect.TypeFor[*svc.ZoneDataOutput](),
		lib.DataSources["zone"].OutputType())
}

// TestRoute53Schemas asserts the whole derived TypeSchema for each Route 53
// resource: input and output field types, the cross-field and enum constraints
// each Constraints method declares, and the optional defaults.
func TestRoute53Schemas(t *testing.T) {
	schema := readLibrarySchema(t)

	cases := map[string]*runtime.TypeSchema{
		"hosted-zone": {
			Inputs: map[string]typecheck.Type{
				"comment":           typecheck.TOptional(typecheck.TString()),
				"delegation-set-id": typecheck.TOptional(typecheck.TString()),
				"force-destroy":     typecheck.TOptional(typecheck.TBoolean()),
				"name":              typecheck.TString(),
				"tags":              typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"vpcs": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "vpc-id", Type: typecheck.TString()},
					{Name: "vpc-region", Type: typecheck.TString(), Optional: true},
				}))),
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
					Kind: "predicate",
					When: "true",
					Require: "((input.delegation-set-id == null) || " +
						"!(@core.length(input.vpcs ?? []) >= 1))",
					Message: "delegation-set-id and vpcs are mutually exclusive",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@core.length(@each.value.vpc-id) >= 1)",
					Message: "a vpc association requires a vpc-id",
					ForEach: "input.vpcs ?? []",
				},
			},
		},
		"record-set": {
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
				"records":                          typecheck.TOptional(typecheck.TList(typecheck.TString())),
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
					Require: "(@core.length(input.zone-id) >= 1)",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(((input.alias != null) && " +
						"!((input.records != null) && " +
						"(@core.length(input.records) >= 1))) || " +
						"((input.alias == null) && " +
						"((input.records != null) && " +
						"(@core.length(input.records) >= 1))))",
					Message: "exactly one of alias or records is required",
				},
				{
					Kind:   "forbidden-with",
					Fields: []string{"input.ttl", "input.alias"},
				},
				{
					Kind:    "predicate",
					When:    "(input.ttl != null)",
					Require: "((input.records != null) && (@core.length(input.records) >= 1))",
					Message: "ttl requires records",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.weighted-routing-policy",
						"input.latency-routing-policy",
						"input.failover-routing-policy",
						"input.geolocation-routing-policy",
						"input.multivalue-answer-routing-policy",
					},
				},
				{
					Kind:   "required-with",
					Fields: []string{"input.weighted-routing-policy", "input.set-identifier"},
				},
				{
					Kind:   "required-with",
					Fields: []string{"input.latency-routing-policy", "input.set-identifier"},
				},
				{
					Kind:   "required-with",
					Fields: []string{"input.failover-routing-policy", "input.set-identifier"},
				},
				{
					Kind:   "required-with",
					Fields: []string{"input.geolocation-routing-policy", "input.set-identifier"},
				},
				{
					Kind: "required-with",
					Fields: []string{
						"input.multivalue-answer-routing-policy",
						"input.set-identifier",
					},
				},
				{
					Kind: "predicate",
					When: "(input.failover-routing-policy != null)",
					Require: "(input.failover-routing-policy.type == 'PRIMARY' || " +
						"input.failover-routing-policy.type == 'SECONDARY')",
					Message: "failover-routing-policy type must be PRIMARY or SECONDARY",
				},
				{
					Kind: "predicate",
					When: "(input.latency-routing-policy != null)",
					Require: "(input.latency-routing-policy.region == 'us-east-1' || " +
						"input.latency-routing-policy.region == 'us-east-2' || " +
						"input.latency-routing-policy.region == 'us-west-1' || " +
						"input.latency-routing-policy.region == 'us-west-2' || " +
						"input.latency-routing-policy.region == 'ca-central-1' || " +
						"input.latency-routing-policy.region == 'ca-west-1' || " +
						"input.latency-routing-policy.region == 'eu-west-1' || " +
						"input.latency-routing-policy.region == 'eu-west-2' || " +
						"input.latency-routing-policy.region == 'eu-west-3' || " +
						"input.latency-routing-policy.region == 'eu-central-1' || " +
						"input.latency-routing-policy.region == 'eu-central-2' || " +
						"input.latency-routing-policy.region == 'eu-north-1' || " +
						"input.latency-routing-policy.region == 'eu-south-1' || " +
						"input.latency-routing-policy.region == 'eu-south-2' || " +
						"input.latency-routing-policy.region == 'ap-east-1' || " +
						"input.latency-routing-policy.region == 'ap-east-2' || " +
						"input.latency-routing-policy.region == 'ap-south-1' || " +
						"input.latency-routing-policy.region == 'ap-south-2' || " +
						"input.latency-routing-policy.region == 'ap-southeast-1' || " +
						"input.latency-routing-policy.region == 'ap-southeast-2' || " +
						"input.latency-routing-policy.region == 'ap-southeast-3' || " +
						"input.latency-routing-policy.region == 'ap-southeast-4' || " +
						"input.latency-routing-policy.region == 'ap-southeast-5' || " +
						"input.latency-routing-policy.region == 'ap-southeast-6' || " +
						"input.latency-routing-policy.region == 'ap-southeast-7' || " +
						"input.latency-routing-policy.region == 'ap-northeast-1' || " +
						"input.latency-routing-policy.region == 'ap-northeast-2' || " +
						"input.latency-routing-policy.region == 'ap-northeast-3' || " +
						"input.latency-routing-policy.region == 'sa-east-1' || " +
						"input.latency-routing-policy.region == 'me-south-1' || " +
						"input.latency-routing-policy.region == 'me-central-1' || " +
						"input.latency-routing-policy.region == 'af-south-1' || " +
						"input.latency-routing-policy.region == 'il-central-1' || " +
						"input.latency-routing-policy.region == 'mx-central-1' || " +
						"input.latency-routing-policy.region == 'cn-north-1' || " +
						"input.latency-routing-policy.region == 'cn-northwest-1' || " +
						"input.latency-routing-policy.region == 'us-gov-east-1' || " +
						"input.latency-routing-policy.region == 'us-gov-west-1' || " +
						"input.latency-routing-policy.region == 'eusc-de-east-1')",
					Message: "latency-routing-policy region must be a valid AWS region",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(input.type == 'A' || input.type == 'AAAA' || " +
						"input.type == 'CAA' || input.type == 'CNAME' || " +
						"input.type == 'DS' || input.type == 'MX' || " +
						"input.type == 'NAPTR' || input.type == 'NS' || " +
						"input.type == 'PTR' || input.type == 'SOA' || " +
						"input.type == 'SPF' || input.type == 'SRV' || " +
						"input.type == 'TXT' || input.type == 'TLSA' || " +
						"input.type == 'SSHFP' || input.type == 'SVCB' || " +
						"input.type == 'HTTPS')",
				},
			},
		},
	}

	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assertTypeSchemaEqual(t, want, schema.Resources[key])
		})
	}
}

// TestRoute53DataSourceSchemas asserts the whole derived TypeSchema for each
// Route 53 data source.
func TestRoute53DataSourceSchemas(t *testing.T) {
	schema := readLibrarySchema(t)
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"zone-id":      typecheck.TOptional(typecheck.TString()),
			"name":         typecheck.TOptional(typecheck.TString()),
			"private-zone": typecheck.TOptional(typecheck.TBoolean()),
			"vpc-id":       typecheck.TOptional(typecheck.TString()),
			"tags":         typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		},
		Outputs: map[string]typecheck.Type{
			"zone-id":                     typecheck.TString(),
			"arn":                         typecheck.TString(),
			"name":                        typecheck.TString(),
			"name-servers":                typecheck.TList(typecheck.TString()),
			"primary-name-server":         typecheck.TString(),
			"caller-reference":            typecheck.TString(),
			"comment":                     typecheck.TString(),
			"private-zone":                typecheck.TBoolean(),
			"resource-record-set-count":   typecheck.TInteger(),
			"enable-accelerated-recovery": typecheck.TBoolean(),
			"linked-service-description":  typecheck.TString(),
			"linked-service-principal":    typecheck.TString(),
			"tags":                        typecheck.TMap(typecheck.TString()),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind:   "at-most-one-of",
				Fields: []string{"input.zone-id", "input.name"},
			},
		},
	}
	require.Contains(t, schema.DataSources, "zone")
	assertTypeSchemaEqual(t, want, schema.DataSources["zone"])
}
