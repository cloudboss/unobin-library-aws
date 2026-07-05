package secretsmanager

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/secretsmanager"
)

// TestLibraryRegistersSecretsmanager checks the runtime registration: the
// Secrets Manager resources and data sources dispatch to their output types.
func TestLibraryRegistersSecretsmanager(t *testing.T) {
	lib := Library()
	resources := map[string]reflect.Type{
		"secret":         reflect.TypeFor[*svc.SecretOutput](),
		"secret-version": reflect.TypeFor[*svc.SecretVersionOutput](),
	}
	for key, outputType := range resources {
		t.Run("resource/"+key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
	dataSources := map[string]reflect.Type{
		"secret-version": reflect.TypeFor[*svc.SecretVersionDataOutput](),
	}
	for key, outputType := range dataSources {
		t.Run("data-source/"+key, func(t *testing.T) {
			require.Contains(t, lib.DataSources, key)
			assert.Equal(t, outputType, lib.DataSources[key].OutputType())
		})
	}
}

// TestSecretsmanagerSchemas asserts the whole derived TypeSchema for the
// Secrets Manager resources and data sources: input and output field types,
// sensitive payloads, constraints, and optional defaults.
func TestSecretsmanagerSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"secret-version": {
			Inputs: map[string]typecheck.Type{
				"secret-binary-content": typecheck.TOptional(typecheck.TString()),
				"secret-id":             typecheck.TString(),
				"secret-string":         typecheck.TOptional(typecheck.TString()),
				"version-stages":        typecheck.TOptional(typecheck.TList(typecheck.TString())),
			},
			Outputs: map[string]typecheck.Type{
				"arn":            typecheck.TString(),
				"secret-id":      typecheck.TString(),
				"version-id":     typecheck.TString(),
				"version-stages": typecheck.TList(typecheck.TString()),
			},
			SensitiveInputs: []string{"secret-binary-content", "secret-string"},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.secret-binary-content",
						"input.secret-string",
					},
				},
			},
		},
		"secret": {
			Inputs: map[string]typecheck.Type{
				"description":                    typecheck.TOptional(typecheck.TString()),
				"force-overwrite-replica-secret": typecheck.TOptional(typecheck.TBoolean()),
				"kms-key-id":                     typecheck.TOptional(typecheck.TString()),
				"name":                           typecheck.TString(),
				"recovery-window-in-days":        typecheck.TOptional(typecheck.TInteger()),
				"replica": typecheck.TOptional(typecheck.TList(typecheck.TObject(
					[]typecheck.ObjectField{
						{Name: "region", Type: typecheck.TString()},
						{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
					}))),
				"secret-binary": typecheck.TOptional(typecheck.TString()),
				"secret-string": typecheck.TOptional(typecheck.TString()),
				"tags":          typecheck.TOptional(typecheck.TMap(typecheck.TString())),
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
					Fields: []string{"input.secret-string", "input.secret-binary"},
				},
				{
					Kind: "predicate",
					When: "(input.recovery-window-in-days != null)",
					Require: "((input.recovery-window-in-days == 0) || " +
						"((input.recovery-window-in-days == null || " +
						"input.recovery-window-in-days >= 7) && " +
						"(input.recovery-window-in-days == null || " +
						"input.recovery-window-in-days <= 30)))",
					Message: "recovery-window-in-days must be 0 or between 7 and 30",
				},
			},
		},
	}
	for key, want := range resources {
		t.Run("resource/"+key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assertTypeSchemaEqual(t, want, schema.Resources[key])
		})
	}

	dataSources := map[string]*runtime.TypeSchema{
		"secret-version": {
			Inputs: map[string]typecheck.Type{
				"secret-id":     typecheck.TString(),
				"version-id":    typecheck.TOptional(typecheck.TString()),
				"version-stage": typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":            typecheck.TString(),
				"created-date":   typecheck.TString(),
				"name":           typecheck.TString(),
				"secret-binary":  typecheck.TString(),
				"secret-string":  typecheck.TString(),
				"version-id":     typecheck.TString(),
				"version-stages": typecheck.TList(typecheck.TString()),
			},
			SensitiveOutputs: []string{"secret-binary", "secret-string"},
			Defaults: []lang.DefaultSpec{
				{Field: "input.version-stage", Value: "'AWSCURRENT'"},
			},
		},
	}
	for key, want := range dataSources {
		t.Run("data-source/"+key, func(t *testing.T) {
			require.Contains(t, schema.DataSources, key)
			assertTypeSchemaEqual(t, want, schema.DataSources[key])
		})
	}
}
