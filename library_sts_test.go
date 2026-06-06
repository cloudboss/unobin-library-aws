package library_test

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	library "github.com/cloudboss/unobin-library-aws"
	"github.com/cloudboss/unobin-library-aws/internal/service/sts"
)

// TestLibraryRegistersStsCallerIdentity checks the runtime registration:
// sts-caller-identity is present under DataSources and dispatches to the
// CallerIdentityOutput type.
func TestLibraryRegistersStsCallerIdentity(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.DataSources, "sts-caller-identity")
	assert.Equal(t, reflect.TypeFor[*sts.CallerIdentityOutput](),
		lib.DataSources["sts-caller-identity"].OutputType())
}

// TestStsCallerIdentitySchema asserts the whole derived TypeSchema for the
// sts-caller-identity data source: no inputs, and the three identity outputs.
func TestStsCallerIdentitySchema(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.DataSources, "sts-caller-identity")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{},
		Outputs: map[string]typecheck.Type{
			"account": typecheck.TString(),
			"arn":     typecheck.TString(),
			"user-id": typecheck.TString(),
		},
	}
	assert.Equal(t, want, schema.DataSources["sts-caller-identity"])
}
