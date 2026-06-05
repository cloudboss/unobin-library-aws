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
	"github.com/cloudboss/unobin-library-aws/internal/service/iam"
)

// TestLibraryRegistersIamResources checks the runtime registration: every
// IAM resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersIamResources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"iam-role":                    reflect.TypeFor[*iam.RoleOutput](),
		"iam-policy":                  reflect.TypeFor[*iam.PolicyOutput](),
		"iam-instance-profile":        reflect.TypeFor[*iam.InstanceProfileOutput](),
		"iam-openid-connect-provider": reflect.TypeFor[*iam.OpenIDConnectProviderOutput](),
		"iam-role-policy-attachment":  reflect.TypeFor[*iam.RolePolicyAttachmentOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestIamSchemas checks what the dev CLI reads from this library's source for
// each IAM resource: the input and output field types, that nothing is marked
// sensitive, and the cross-field constraints derived from each Constraints
// method. The whole TypeSchema is asserted so a stray field or tag is caught.
func TestIamSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	cases := map[string]*runtime.TypeSchema{
		"iam-role": {
			Inputs: map[string]typecheck.Type{
				"role-name":                   typecheck.TString(),
				"assume-role-policy-document": typecheck.TString(),
				"path":                        typecheck.TOptional(typecheck.TString()),
				"description":                 typecheck.TOptional(typecheck.TString()),
				"max-session-duration":        typecheck.TOptional(typecheck.TInteger()),
				"permissions-boundary":        typecheck.TOptional(typecheck.TString()),
				"tags":                        typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":         typecheck.TString(),
				"role-id":     typecheck.TString(),
				"create-date": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(var.max-session-duration != null)",
					Require: "(var.max-session-duration == null || var.max-session-duration >= 3600) && " +
						"(var.max-session-duration == null || var.max-session-duration <= 43200)",
					Message: "max-session-duration must be between 3600 and 43200 seconds",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.tags", Optional: true},
			},
		},
		"iam-policy": {
			Inputs: map[string]typecheck.Type{
				"policy-name":     typecheck.TString(),
				"policy-document": typecheck.TString(),
				"path":            typecheck.TOptional(typecheck.TString()),
				"description":     typecheck.TOptional(typecheck.TString()),
				"tags":            typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                typecheck.TString(),
				"policy-id":          typecheck.TString(),
				"default-version-id": typecheck.TString(),
				"attachment-count":   typecheck.TInteger(),
				"create-date":        typecheck.TString(),
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.tags", Optional: true},
			},
		},
		"iam-instance-profile": {
			Inputs: map[string]typecheck.Type{
				"instance-profile-name": typecheck.TString(),
				"path":                  typecheck.TOptional(typecheck.TString()),
				"role":                  typecheck.TOptional(typecheck.TString()),
				"tags":                  typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                 typecheck.TString(),
				"instance-profile-id": typecheck.TString(),
				"create-date":         typecheck.TString(),
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.tags", Optional: true},
			},
		},
		"iam-openid-connect-provider": {
			Inputs: map[string]typecheck.Type{
				"url":             typecheck.TString(),
				"client-id-list":  typecheck.TList(typecheck.TString()),
				"thumbprint-list": typecheck.TList(typecheck.TString()),
				"tags":            typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":             typecheck.TString(),
				"create-date":     typecheck.TString(),
				"thumbprint-list": typecheck.TList(typecheck.TString()),
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.client-id-list", Optional: true},
				{Field: "var.thumbprint-list", Optional: true},
				{Field: "var.tags", Optional: true},
			},
		},
		"iam-role-policy-attachment": {
			Inputs: map[string]typecheck.Type{
				"role-name":  typecheck.TString(),
				"policy-arn": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{},
		},
	}

	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, want, schema.Resources[key])
		})
	}
}
