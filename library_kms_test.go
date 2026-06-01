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
	"github.com/cloudboss/unobin-library-aws/library/actions"
	"github.com/cloudboss/unobin-library-aws/library/resources"
)

// TestLibraryRegistersKmsResources checks the runtime registration: every
// KMS resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersKmsResources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"kms-key":   reflect.TypeFor[*resources.KmsKeyOutput](),
		"kms-alias": reflect.TypeFor[*resources.KmsAliasOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestLibraryRegistersKmsActions checks the runtime registration: every KMS
// key operation is present under Actions and dispatches to the action output
// type.
func TestLibraryRegistersKmsActions(t *testing.T) {
	lib := library.Library()
	out := reflect.TypeFor[*actions.KmsKeyActionOutput]()
	for _, key := range []string{
		"kms-enable-key", "kms-disable-key",
		"kms-enable-key-rotation", "kms-disable-key-rotation",
	} {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Actions, key)
			assert.Equal(t, out, lib.Actions[key].OutputType())
		})
	}
}

// TestKmsSchemas checks what the dev CLI reads from this library's source for
// each KMS resource: the input and output field types, that nothing is marked
// sensitive, and the cross-field constraints derived from each Constraints
// method. The whole TypeSchema is asserted so a stray field or tag is caught.
func TestKmsSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	cases := map[string]*runtime.TypeSchema{
		"kms-key": {
			Inputs: map[string]typecheck.Type{
				"policy":                             typecheck.TOptional(typecheck.TString()),
				"bypass-policy-lockout-safety-check": typecheck.TOptional(typecheck.TBoolean()),
				"description":                        typecheck.TOptional(typecheck.TString()),
				"key-spec":                           typecheck.TOptional(typecheck.TString()),
				"key-usage":                          typecheck.TOptional(typecheck.TString()),
				"custom-key-store-id":                typecheck.TOptional(typecheck.TString()),
				"xks-key-id":                         typecheck.TOptional(typecheck.TString()),
				"multi-region":                       typecheck.TOptional(typecheck.TBoolean()),
				"tags":                               typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":    typecheck.TString(),
				"key-id": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "required-with",
					Fields: []string{"xks-key-id", "custom-key-store-id"},
				},
				{
					Kind: "predicate",
					When: "(var.key-spec != null)",
					Require: "(var.key-spec == 'SYMMETRIC_DEFAULT' || var.key-spec == 'RSA_2048' || " +
						"var.key-spec == 'RSA_3072' || var.key-spec == 'RSA_4096' || " +
						"var.key-spec == 'ECC_NIST_P256' || var.key-spec == 'ECC_NIST_P384' || " +
						"var.key-spec == 'ECC_NIST_P521' || var.key-spec == 'ECC_SECG_P256K1' || " +
						"var.key-spec == 'ECC_NIST_EDWARDS25519' || var.key-spec == 'HMAC_224' || " +
						"var.key-spec == 'HMAC_256' || var.key-spec == 'HMAC_384' || " +
						"var.key-spec == 'HMAC_512' || var.key-spec == 'ML_DSA_44' || " +
						"var.key-spec == 'ML_DSA_65' || var.key-spec == 'ML_DSA_87' || " +
						"var.key-spec == 'SM2')",
					Message: "key-spec must be a valid KMS key spec",
				},
				{
					Kind: "predicate",
					When: "(var.key-usage != null)",
					Require: "(var.key-usage == 'ENCRYPT_DECRYPT' || var.key-usage == 'SIGN_VERIFY' || " +
						"var.key-usage == 'GENERATE_VERIFY_MAC' || var.key-usage == 'KEY_AGREEMENT')",
					Message: "key-usage must be a valid KMS key usage",
				},
			},
		},
		"kms-alias": {
			Inputs: map[string]typecheck.Type{
				"alias-name":    typecheck.TString(),
				"target-key-id": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"arn":            typecheck.TString(),
				"target-key-arn": typecheck.TString(),
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

// TestKmsActionSchemas checks the input and output field types the dev CLI
// reads for each KMS key action, including the rotation period bound.
func TestKmsActionSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	keyIDOnly := map[string]typecheck.Type{"key-id": typecheck.TString()}
	noOutputs := map[string]typecheck.Type{}
	cases := map[string]*runtime.TypeSchema{
		"kms-enable-key": {
			Inputs:  keyIDOnly,
			Outputs: noOutputs,
		},
		"kms-disable-key": {
			Inputs:  keyIDOnly,
			Outputs: noOutputs,
		},
		"kms-disable-key-rotation": {
			Inputs:  keyIDOnly,
			Outputs: noOutputs,
		},
		"kms-enable-key-rotation": {
			Inputs: map[string]typecheck.Type{
				"key-id":                  typecheck.TString(),
				"rotation-period-in-days": typecheck.TOptional(typecheck.TInteger()),
			},
			Outputs: noOutputs,
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(var.rotation-period-in-days != null)",
					Require: "(var.rotation-period-in-days == null || " +
						"var.rotation-period-in-days >= 90) && " +
						"(var.rotation-period-in-days == null || var.rotation-period-in-days <= 2560)",
					Message: "rotation-period-in-days must be between 90 and 2560",
				},
			},
		},
	}

	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Actions, key)
			assert.Equal(t, want, schema.Actions[key])
		})
	}
}
