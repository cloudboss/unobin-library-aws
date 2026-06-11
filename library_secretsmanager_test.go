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
	"github.com/cloudboss/unobin-library-aws/internal/service/secretsmanager"
)

// TestLibraryRegistersSecretsmanager checks the runtime registration: the
// Secrets Manager secret resource is present under Resources and dispatches to
// its output type.
func TestLibraryRegistersSecretsmanager(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"secretsmanager-secret": reflect.TypeFor[*secretsmanager.SecretOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestSecretsmanagerSchemas asserts the whole derived TypeSchema for the Secrets
// Manager secret: input and output field types (including the replica blocks and
// the folded sensitive value), the value and recovery-window constraints the
// Constraints method declares, and the optional defaults.
func TestSecretsmanagerSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"secretsmanager-secret": {
			Inputs: map[string]typecheck.Type{
				"description":                    typecheck.TOptional(typecheck.TString()),
				"force-overwrite-replica-secret": typecheck.TOptional(typecheck.TBoolean()),
				"kms-key-id":                     typecheck.TOptional(typecheck.TString()),
				"name":                           typecheck.TString(),
				"recovery-window-in-days":        typecheck.TOptional(typecheck.TInteger()),
				"replica": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "region", Type: typecheck.TString()},
					{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
				})),
				"secret-binary": typecheck.TOptional(typecheck.TString()),
				"secret-string": typecheck.TOptional(typecheck.TString()),
				"tags":          typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn": typecheck.TString(),
				"replica-status": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "region", Type: typecheck.TString()},
					{Name: "status", Type: typecheck.TString()},
					{Name: "status-message", Type: typecheck.TString()},
					{Name: "last-accessed-date", Type: typecheck.TString()},
				})),
				"version-id": typecheck.TString(),
			},
			SensitiveInputs: []string{"secret-binary", "secret-string"},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "at-most-one-of",
					Fields: []string{"var.secret-string", "var.secret-binary"},
				},
				{
					Kind: "predicate",
					When: "(var.recovery-window-in-days != null)",
					Require: "((var.recovery-window-in-days == 0) || " +
						"((var.recovery-window-in-days == null || " +
						"var.recovery-window-in-days >= 7) && " +
						"(var.recovery-window-in-days == null || " +
						"var.recovery-window-in-days <= 30)))",
					Message: "recovery-window-in-days must be 0 or between 7 and 30",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "((@each.value.region != null) && " +
						"(@core.length(@each.value.region) >= 1))",
					Message: "a replica requires a region",
					ForEach: "var.replica",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.replica", Optional: true},
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
