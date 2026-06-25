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
	"github.com/cloudboss/unobin-library-aws/internal/service/iam"
)

// TestLibraryRegistersIamResources checks the runtime registration: every
// IAM resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersIamResources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"iam-role":                    reflect.TypeFor[*iam.RoleOutput](),
		"iam-group":                   reflect.TypeFor[*iam.GroupOutput](),
		"iam-user":                    reflect.TypeFor[*iam.UserOutput](),
		"iam-access-key":              reflect.TypeFor[*iam.AccessKeyOutput](),
		"iam-policy":                  reflect.TypeFor[*iam.PolicyOutput](),
		"iam-instance-profile":        reflect.TypeFor[*iam.InstanceProfileOutput](),
		"iam-openid-connect-provider": reflect.TypeFor[*iam.OpenIDConnectProviderOutput](),
		"iam-role-policy-attachment":  reflect.TypeFor[*iam.RolePolicyAttachmentOutput](),
		"iam-group-policy-attachment": reflect.TypeFor[*iam.GroupPolicyAttachmentOutput](),
		"iam-user-policy-attachment":  reflect.TypeFor[*iam.UserPolicyAttachmentOutput](),
		"iam-role-policy":             reflect.TypeFor[*iam.RolePolicyOutput](),
		"iam-group-policy":            reflect.TypeFor[*iam.GroupPolicyOutput](),
		"iam-user-policy":             reflect.TypeFor[*iam.UserPolicyOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestIamSchemas checks what the dev CLI reads from this library's source for
// each IAM resource: the input and output field types, sensitive fields, and
// the cross-field constraints derived from each Constraints method. The whole
// TypeSchema is asserted so a stray field or tag is caught.
func TestIamSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

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
					When: "(input.max-session-duration != null)",
					Require: "(input.max-session-duration == null || input.max-session-duration >= 3600) && " +
						"(input.max-session-duration == null || input.max-session-duration <= 43200)",
					Message: "max-session-duration must be between 3600 and 43200 seconds",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "input.tags", Optional: true},
			},
		},
		"iam-group": {
			Inputs: map[string]typecheck.Type{
				"name": typecheck.TString(),
				"path": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"arn":       typecheck.TString(),
				"unique-id": typecheck.TString(),
				"name":      typecheck.TString(),
			},
			Defaults: []lang.DefaultSpec{
				{Field: "input.path", Value: "'/'"},
			},
		},
		"iam-user": {
			Inputs: map[string]typecheck.Type{
				"name":                 typecheck.TString(),
				"path":                 typecheck.TString(),
				"permissions-boundary": typecheck.TOptional(typecheck.TString()),
				"force-destroy":        typecheck.TBoolean(),
				"tags":                 typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                  typecheck.TString(),
				"unique-id":            typecheck.TString(),
				"name":                 typecheck.TString(),
				"path":                 typecheck.TString(),
				"permissions-boundary": typecheck.TOptional(typecheck.TString()),
				"tags":                 typecheck.TMap(typecheck.TString()),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(input.permissions-boundary != null)",
					Require: "(input.permissions-boundary == null || " +
						"@core.length(input.permissions-boundary) <= 2048)",
					Message: "permissions-boundary must be at most 2048 characters",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "input.path", Value: "'/'"},
				{Field: "input.force-destroy", Value: "false"},
				{Field: "input.tags", Optional: true},
			},
		},
		"iam-access-key": {
			Inputs: map[string]typecheck.Type{
				"user-name": typecheck.TString(),
				"pgp-key":   typecheck.TString(),
				"status":    typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"access-key-id":                  typecheck.TString(),
				"user-name":                      typecheck.TString(),
				"create-date":                    typecheck.TString(),
				"status":                         typecheck.TString(),
				"secret":                         typecheck.TOptional(typecheck.TString()),
				"ses-smtp-password-v4":           typecheck.TOptional(typecheck.TString()),
				"encrypted-secret":               typecheck.TOptional(typecheck.TString()),
				"encrypted-ses-smtp-password-v4": typecheck.TOptional(typecheck.TString()),
				"key-fingerprint":                typecheck.TOptional(typecheck.TString()),
			},
			SensitiveOutputs: []string{"secret", "ses-smtp-password-v4"},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(input.status == 'Active' || input.status == 'Inactive')",
					Message: "status must be Active or Inactive",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "input.pgp-key", Value: "''"},
				{Field: "input.status", Value: "'Active'"},
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
				{Field: "input.tags", Optional: true},
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
				{Field: "input.tags", Optional: true},
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
				{Field: "input.client-id-list", Optional: true},
				{Field: "input.thumbprint-list", Optional: true},
				{Field: "input.tags", Optional: true},
			},
		},
		"iam-role-policy-attachment": {
			Inputs: map[string]typecheck.Type{
				"role-name":  typecheck.TString(),
				"policy-arn": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{},
		},
		"iam-group-policy-attachment": {
			Inputs: map[string]typecheck.Type{
				"group-name": typecheck.TString(),
				"policy-arn": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"group-name": typecheck.TString(),
				"policy-arn": typecheck.TString(),
			},
		},
		"iam-user-policy-attachment": {
			Inputs: map[string]typecheck.Type{
				"policy-arn": typecheck.TString(),
				"user":       typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"policy-arn": typecheck.TString(),
				"user":       typecheck.TString(),
			},
		},
		"iam-role-policy": {
			Inputs: map[string]typecheck.Type{
				"policy-document": typecheck.TString(),
				"policy-name":     typecheck.TString(),
				"role-name":       typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"policy-document": typecheck.TString(),
				"policy-name":     typecheck.TString(),
				"role-name":       typecheck.TString(),
			},
		},
		"iam-group-policy": {
			Inputs: map[string]typecheck.Type{
				"group-name":      typecheck.TString(),
				"policy-document": typecheck.TString(),
				"policy-name":     typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"group-name":      typecheck.TString(),
				"policy-document": typecheck.TString(),
				"policy-name":     typecheck.TString(),
			},
		},
		"iam-user-policy": {
			Inputs: map[string]typecheck.Type{
				"policy-document": typecheck.TString(),
				"policy-name":     typecheck.TString(),
				"user-name":       typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"policy-document": typecheck.TString(),
				"policy-name":     typecheck.TString(),
				"user-name":       typecheck.TString(),
			},
		},
	}

	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, want, schema.Resources[key])
		})
	}
}

// TestLibraryRegistersIamOpenIDConnectProviderData checks the runtime
// registration of the OIDC provider data source under DataSources (the resource
// of the same key is registered separately under Resources).
func TestLibraryRegistersIamOpenIDConnectProviderData(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.DataSources, "iam-openid-connect-provider")
	assert.Equal(t, reflect.TypeFor[*iam.OpenIDConnectProviderDataOutput](),
		lib.DataSources["iam-openid-connect-provider"].OutputType())
}

// TestIamOpenIDConnectProviderDataSchema asserts the whole derived TypeSchema for
// the OIDC provider data source: the arn/url lookup keys (exactly one of them),
// and the resolved provider outputs.
func TestIamOpenIDConnectProviderDataSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Contains(t, schema.DataSources, "iam-openid-connect-provider")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"arn": typecheck.TOptional(typecheck.TString()),
			"url": typecheck.TOptional(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"arn":             typecheck.TString(),
			"url":             typecheck.TString(),
			"client-id-list":  typecheck.TList(typecheck.TString()),
			"thumbprint-list": typecheck.TList(typecheck.TString()),
			"tags":            typecheck.TMap(typecheck.TString()),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind:   "exactly-one-of",
				Fields: []string{"input.arn", "input.url"},
			},
		},
	}
	assert.Equal(t, want, schema.DataSources["iam-openid-connect-provider"])
}
