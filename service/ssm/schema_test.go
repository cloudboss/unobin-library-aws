package ssm

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/ssm"
)

// TestLibraryRegistersSsm checks the runtime registration: the SSM parameter
// resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersSsm(t *testing.T) {
	lib := Library()
	resources := map[string]reflect.Type{
		"parameter": reflect.TypeFor[*svc.ParameterOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestSsmSchemas asserts the whole derived TypeSchema for the SSM parameter:
// input and output field types, the value cross-field and enum constraints the
// Constraints method declares, and the optional defaults.
func TestSsmSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"parameter": {
			Inputs: map[string]typecheck.Type{
				"allowed-pattern": typecheck.TOptional(typecheck.TString()),
				"data-type":       typecheck.TOptional(typecheck.TString()),
				"description":     typecheck.TOptional(typecheck.TString()),
				"insecure-value":  typecheck.TOptional(typecheck.TString()),
				"key-id":          typecheck.TOptional(typecheck.TString()),
				"name":            typecheck.TString(),
				"tags":            typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"tier":            typecheck.TOptional(typecheck.TString()),
				"type":            typecheck.TString(),
				"value":           typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":     typecheck.TString(),
				"name":    typecheck.TString(),
				"version": typecheck.TInteger(),
			},
			SensitiveInputs: []string{"value"},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "exactly-one-of",
					Fields: []string{"input.value", "input.insecure-value"},
				},
				{
					Kind:    "predicate",
					When:    "(input.type == 'SecureString')",
					Require: "(input.insecure-value == null)",
					Message: "insecure-value cannot be set when type is SecureString",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(input.type == 'String' || " +
						"input.type == 'StringList' || " +
						"input.type == 'SecureString')",
				},
				{
					Kind: "predicate",
					When: "(input.tier != null)",
					Require: "(input.tier == 'Standard' || " +
						"input.tier == 'Advanced' || " +
						"input.tier == 'Intelligent-Tiering')",
					Message: "tier must be Standard, Advanced, or Intelligent-Tiering",
				},
				{
					Kind: "predicate",
					When: "(input.data-type != null)",
					Require: "(input.data-type == 'text' || " +
						"input.data-type == 'aws:ec2:image' || " +
						"input.data-type == 'aws:ssm:integration')",
					Message: "data-type must be text, aws:ec2:image, or aws:ssm:integration",
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
