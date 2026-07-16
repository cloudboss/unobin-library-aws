package sfn_test

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/sfn"
)

func TestStateMachineSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 1)
	require.Contains(t, schema.Resources, "state-machine")
	got := schema.Resources["state-machine"]

	encryption := typecheck.TObject([]typecheck.ObjectField{
		{Name: "type", Type: typecheck.TString()},
		{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
		{
			Name:     "kms-data-key-reuse-period-seconds",
			Type:     typecheck.TInteger(),
			Optional: true,
		},
	})
	logging := typecheck.TObject([]typecheck.ObjectField{
		{Name: "include-execution-data", Type: typecheck.TBoolean(), Optional: true},
		{Name: "level", Type: typecheck.TString(), Optional: true},
		{Name: "log-destination", Type: typecheck.TString(), Optional: true},
	})
	tracing := typecheck.TObject([]typecheck.ObjectField{
		{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
	})
	assert.Equal(t, map[string]typecheck.Type{
		"name":                     typecheck.TOptional(typecheck.TString()),
		"definition":               typecheck.TString(),
		"role-arn":                 typecheck.TString(),
		"type":                     typecheck.TString(),
		"encryption-configuration": typecheck.TOptional(encryption),
		"logging-configuration":    typecheck.TOptional(logging),
		"tracing-configuration":    typecheck.TOptional(tracing),
		"tags":                     typecheck.TOptional(typecheck.TMap(typecheck.TString())),
	}, got.Inputs)
	assert.Equal(t, map[string]typecheck.Type{
		"arn":         typecheck.TString(),
		"revision-id": typecheck.TString(),
	}, got.Outputs)
	assert.Equal(t, []string{"definition"}, got.SensitiveInputs)
	assert.Empty(t, got.SensitiveOutputs)
	assert.Equal(t, []lang.DefaultSpec{
		{Field: "input.type", Value: "'STANDARD'"},
	}, got.Defaults)
	assert.Contains(t, got.Constraints, lang.ConstraintSpec{
		Kind:    "predicate",
		When:    "true",
		Require: "(@core.length(input.tags ?? {}) <= 50)",
		Message: "tags holds at most 50 entries",
	})
	assert.Contains(t, got.Constraints, lang.ConstraintSpec{
		Kind:    "predicate",
		When:    "true",
		Require: "(input.type == 'STANDARD' || input.type == 'EXPRESS')",
		Message: "type must be STANDARD or EXPRESS",
	})
}

func TestStateMachineReplacementFieldsFromRegistration(t *testing.T) {
	resource := &svc.StateMachineResource{}
	assert.Equal(t, []string{"name", "type"}, resource.ReplaceFields())
}
