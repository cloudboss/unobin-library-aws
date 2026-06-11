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
	"github.com/cloudboss/unobin-library-aws/internal/service/ssm"
)

// TestLibraryRegistersSsm checks the runtime registration: the SSM parameter
// resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersSsm(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"ssm-parameter": reflect.TypeFor[*ssm.ParameterOutput](),
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
		"ssm-parameter": {
			Inputs: map[string]typecheck.Type{
				"allowed-pattern": typecheck.TOptional(typecheck.TString()),
				"data-type":       typecheck.TOptional(typecheck.TString()),
				"description":     typecheck.TOptional(typecheck.TString()),
				"insecure-value":  typecheck.TOptional(typecheck.TString()),
				"key-id":          typecheck.TOptional(typecheck.TString()),
				"name":            typecheck.TString(),
				"tags":            typecheck.TMap(typecheck.TString()),
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
					Fields: []string{"var.value", "var.insecure-value"},
				},
				{
					Kind:    "predicate",
					When:    "(var.type == 'SecureString')",
					Require: "(var.insecure-value == null)",
					Message: "insecure-value cannot be set when type is SecureString",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.type == 'String' || " +
						"var.type == 'StringList' || " +
						"var.type == 'SecureString')",
				},
				{
					Kind: "predicate",
					When: "(var.tier != null)",
					Require: "(var.tier == 'Standard' || " +
						"var.tier == 'Advanced' || " +
						"var.tier == 'Intelligent-Tiering')",
					Message: "tier must be Standard, Advanced, or Intelligent-Tiering",
				},
				{
					Kind: "predicate",
					When: "(var.data-type != null)",
					Require: "(var.data-type == 'text' || " +
						"var.data-type == 'aws:ec2:image' || " +
						"var.data-type == 'aws:ssm:integration')",
					Message: "data-type must be text, aws:ec2:image, or aws:ssm:integration",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.tags", Optional: true},
			},
		},
	}
	for key, want := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, want, schema.Resources[key])
		})
	}
}
