package library_test

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	library "github.com/cloudboss/unobin-library-aws"
	"github.com/cloudboss/unobin-library-aws/internal/service/dynamodb"
)

// TestLibraryRegistersDynamodb checks the runtime registration: the DynamoDB
// table resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersDynamodb(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"dynamodb-table": reflect.TypeFor[*dynamodb.TableOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestDynamodbSchemas asserts the whole derived TypeSchema for the DynamoDB
// table: input and output field types (including the nested attribute, index,
// throughput, encryption, ttl, and recovery blocks), the cross-field and enum
// constraints the Constraints method declares, and the optional defaults.
func TestDynamodbSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"dynamodb-table": {
			Inputs: map[string]typecheck.Type{
				"attribute": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "name", Type: typecheck.TString()},
					{Name: "type", Type: typecheck.TString()},
				}))),
				"billing-mode":                typecheck.TOptional(typecheck.TString()),
				"deletion-protection-enabled": typecheck.TOptional(typecheck.TBoolean()),
				"global-secondary-index": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "name", Type: typecheck.TString()},
					{Name: "hash-key", Type: typecheck.TString()},
					{Name: "range-key", Type: typecheck.TString(), Optional: true},
					{Name: "projection-type", Type: typecheck.TString()},
					{Name: "non-key-attributes", Type: typecheck.TList(typecheck.TString())},
					{Name: "read-capacity", Type: typecheck.TInteger(), Optional: true},
					{Name: "write-capacity", Type: typecheck.TInteger(), Optional: true},
					{Name: "on-demand-throughput", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "max-read-request-units", Type: typecheck.TInteger(), Optional: true},
						{Name: "max-write-request-units", Type: typecheck.TInteger(), Optional: true},
					}), Optional: true},
					{Name: "warm-throughput", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "read-units-per-second", Type: typecheck.TInteger(), Optional: true},
						{Name: "write-units-per-second", Type: typecheck.TInteger(), Optional: true},
					}), Optional: true},
				}))),
				"hash-key": typecheck.TString(),
				"local-secondary-index": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "name", Type: typecheck.TString()},
					{Name: "range-key", Type: typecheck.TString()},
					{Name: "projection-type", Type: typecheck.TString()},
					{Name: "non-key-attributes", Type: typecheck.TList(typecheck.TString())},
				}))),
				"name": typecheck.TString(),
				"on-demand-throughput": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "max-read-request-units", Type: typecheck.TInteger(), Optional: true},
					{Name: "max-write-request-units", Type: typecheck.TInteger(), Optional: true},
				})),
				"point-in-time-recovery": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
					{Name: "recovery-period-in-days", Type: typecheck.TInteger(), Optional: true},
				})),
				"range-key":     typecheck.TOptional(typecheck.TString()),
				"read-capacity": typecheck.TOptional(typecheck.TInteger()),
				"server-side-encryption": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
					{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
				})),
				"stream-enabled":   typecheck.TOptional(typecheck.TBoolean()),
				"stream-view-type": typecheck.TOptional(typecheck.TString()),
				"table-class":      typecheck.TOptional(typecheck.TString()),
				"tags":             typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"ttl": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
					{Name: "attribute-name", Type: typecheck.TString(), Optional: true},
				})),
				"warm-throughput": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "read-units-per-second", Type: typecheck.TInteger(), Optional: true},
					{Name: "write-units-per-second", Type: typecheck.TInteger(), Optional: true},
				})),
				"write-capacity": typecheck.TOptional(typecheck.TInteger()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":          typecheck.TString(),
				"stream-arn":   typecheck.TString(),
				"stream-label": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "at-most-one-of",
					Fields: []string{"input.read-capacity", "input.on-demand-throughput"},
				},
				{
					Kind:   "at-most-one-of",
					Fields: []string{"input.write-capacity", "input.on-demand-throughput"},
				},
				{
					Kind: "predicate",
					When: "(input.billing-mode == 'PROVISIONED')",
					Require: "(input.read-capacity != null) && " +
						"(input.read-capacity == null || " +
						"input.read-capacity >= 1) && " +
						"(input.write-capacity != null) && " +
						"(input.write-capacity == null || " +
						"input.write-capacity >= 1)",
					Message: "read-capacity and write-capacity are required and at least 1 " +
						"when billing-mode is PROVISIONED",
				},
				{
					Kind:    "predicate",
					When:    "(input.billing-mode == 'PAY_PER_REQUEST')",
					Require: "(input.read-capacity == null) && (input.write-capacity == null)",
					Message: "read-capacity and write-capacity must be unset " +
						"when billing-mode is PAY_PER_REQUEST",
				},
				{
					Kind: "predicate",
					When: "(input.billing-mode != null)",
					Require: "(input.billing-mode == 'PROVISIONED' || " +
						"input.billing-mode == 'PAY_PER_REQUEST')",
					Message: "billing-mode must be PROVISIONED or PAY_PER_REQUEST",
				},
				{
					Kind: "predicate",
					When: "(input.table-class != null)",
					Require: "(input.table-class == 'STANDARD' || " +
						"input.table-class == 'STANDARD_INFREQUENT_ACCESS')",
					Message: "table-class must be STANDARD or STANDARD_INFREQUENT_ACCESS",
				},
				{
					Kind:   "required-with",
					Fields: []string{"input.stream-view-type", "input.stream-enabled"},
				},
				{
					Kind:    "predicate",
					When:    "(input.stream-enabled == true)",
					Require: "(input.stream-view-type != null)",
					Message: "stream-view-type is required when stream-enabled is true",
				},
				{
					Kind:    "predicate",
					When:    "(input.stream-enabled == false)",
					Require: "(input.stream-view-type == null)",
					Message: "stream-view-type must be unset when stream-enabled is false",
				},
				{
					Kind: "predicate",
					When: "(input.stream-view-type != null)",
					Require: "(input.stream-view-type == 'NEW_IMAGE' || " +
						"input.stream-view-type == 'OLD_IMAGE' || " +
						"input.stream-view-type == 'NEW_AND_OLD_IMAGES' || " +
						"input.stream-view-type == 'KEYS_ONLY')",
					Message: "stream-view-type must be a valid DynamoDB stream view type",
				},
				{
					Kind: "predicate",
					When: "(input.ttl.enabled == true)",
					Require: "((input.ttl.attribute-name != null) && " +
						"(@core.length(input.ttl.attribute-name) >= 1))",
					Message: "ttl attribute-name is required when ttl is enabled",
				},
				{
					Kind: "predicate",
					When: "(input.point-in-time-recovery.recovery-period-in-days != null)",
					Require: "(input.point-in-time-recovery.recovery-period-in-days == null || " +
						"input.point-in-time-recovery.recovery-period-in-days >= 1) && " +
						"(input.point-in-time-recovery.recovery-period-in-days == null || " +
						"input.point-in-time-recovery.recovery-period-in-days <= 35)",
					Message: "point-in-time-recovery recovery-period-in-days must be between 1 and 35",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.type == 'S' || " +
						"@each.value.type == 'N' || " +
						"@each.value.type == 'B')",
					Message: "attribute type must be S, N, or B",
					ForEach: "input.attribute ?? []",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.projection-type == 'ALL' || " +
						"@each.value.projection-type == 'INCLUDE' || " +
						"@each.value.projection-type == 'KEYS_ONLY')",
					Message: "local-secondary-index projection-type must be ALL, " +
						"INCLUDE, or KEYS_ONLY",
					ForEach: "input.local-secondary-index ?? []",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.projection-type == 'ALL' || " +
						"@each.value.projection-type == 'INCLUDE' || " +
						"@each.value.projection-type == 'KEYS_ONLY')",
					Message: "global-secondary-index projection-type must be ALL, " +
						"INCLUDE, or KEYS_ONLY",
					ForEach: "input.global-secondary-index ?? []",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.global-secondary-index[*].read-capacity",
						"input.global-secondary-index[*].on-demand-throughput",
					},
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.global-secondary-index[*].write-capacity",
						"input.global-secondary-index[*].on-demand-throughput",
					},
				},
				{
					Kind: "predicate",
					When: "(input.billing-mode == 'PROVISIONED')",
					Require: "(@each.value.read-capacity != null) && " +
						"(@each.value.read-capacity == null || " +
						"@each.value.read-capacity >= 1) && " +
						"(@each.value.write-capacity != null) && " +
						"(@each.value.write-capacity == null || " +
						"@each.value.write-capacity >= 1)",
					Message: "global-secondary-index read-capacity and write-capacity " +
						"are required and at least 1 when billing-mode is PROVISIONED",
					ForEach: "input.global-secondary-index ?? []",
				},
				{
					Kind: "predicate",
					When: "(input.billing-mode == 'PAY_PER_REQUEST')",
					Require: "(@each.value.read-capacity == null) && " +
						"(@each.value.write-capacity == null)",
					Message: "global-secondary-index read-capacity and write-capacity " +
						"must be unset when billing-mode is PAY_PER_REQUEST",
					ForEach: "input.global-secondary-index ?? []",
				},
			},
		},
	}
	for key, want := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assertTypeSchemaEqual(t, want, schema.Resources[key])
		})
	}
}
