package sts

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/sts"
)

// TestLibraryRegistersStsCallerIdentity checks the runtime registration:
// sts-caller-identity is present under DataSources and dispatches to the
// CallerIdentityOutput type.
func TestLibraryRegistersStsCallerIdentity(t *testing.T) {
	lib := Library()
	require.Contains(t, lib.DataSources, "caller-identity")
	assert.Equal(t, reflect.TypeFor[*svc.CallerIdentityOutput](),
		lib.DataSources["caller-identity"].OutputType())
}

// TestStsCallerIdentitySchema asserts the whole derived TypeSchema for the
// sts-caller-identity data source: no inputs, and the three identity outputs.
func TestStsCallerIdentitySchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Contains(t, schema.DataSources, "caller-identity")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{},
		Outputs: map[string]typecheck.Type{
			"account": typecheck.TString(),
			"arn":     typecheck.TString(),
			"user-id": typecheck.TString(),
		},
	}
	assertTypeSchemaEqual(t, want, schema.DataSources["caller-identity"])
}
