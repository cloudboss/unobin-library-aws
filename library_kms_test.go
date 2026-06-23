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
	"github.com/cloudboss/unobin-library-aws/internal/service/kms"
)

// TestLibraryRegistersKmsResources checks the runtime registration: every
// KMS resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersKmsResources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"kms-key":   reflect.TypeFor[*kms.KeyOutput](),
		"kms-alias": reflect.TypeFor[*kms.AliasOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestKmsSchemas checks what the dev CLI reads from this library's source for
// each KMS resource: the input and output field types, that nothing is marked
// sensitive, and the cross-field constraints derived from each Constraints
// method. The whole TypeSchema is asserted so a stray field or tag is caught.
func TestKmsSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

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
				"enable-key":                         typecheck.TOptional(typecheck.TBoolean()),
				"enable-key-rotation":                typecheck.TOptional(typecheck.TBoolean()),
				"rotation-period-in-days":            typecheck.TOptional(typecheck.TInteger()),
				"tags":                               typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":    typecheck.TString(),
				"key-id": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "required-with",
					Fields: []string{"input.xks-key-id", "input.custom-key-store-id"},
				},
				{
					Kind: "predicate",
					When: "(input.key-spec != null)",
					Require: "(input.key-spec == 'SYMMETRIC_DEFAULT' || input.key-spec == 'RSA_2048' || " +
						"input.key-spec == 'RSA_3072' || input.key-spec == 'RSA_4096' || " +
						"input.key-spec == 'ECC_NIST_P256' || input.key-spec == 'ECC_NIST_P384' || " +
						"input.key-spec == 'ECC_NIST_P521' || input.key-spec == 'ECC_SECG_P256K1' || " +
						"input.key-spec == 'ECC_NIST_EDWARDS25519' || input.key-spec == 'HMAC_224' || " +
						"input.key-spec == 'HMAC_256' || input.key-spec == 'HMAC_384' || " +
						"input.key-spec == 'HMAC_512' || input.key-spec == 'ML_DSA_44' || " +
						"input.key-spec == 'ML_DSA_65' || input.key-spec == 'ML_DSA_87' || " +
						"input.key-spec == 'SM2')",
					Message: "key-spec must be a valid KMS key spec",
				},
				{
					Kind: "predicate",
					When: "(input.key-usage != null)",
					Require: "(input.key-usage == 'ENCRYPT_DECRYPT' || input.key-usage == 'SIGN_VERIFY' || " +
						"input.key-usage == 'GENERATE_VERIFY_MAC' || input.key-usage == 'KEY_AGREEMENT')",
					Message: "key-usage must be a valid KMS key usage",
				},
				{
					Kind:   "required-with",
					Fields: []string{"input.rotation-period-in-days", "input.enable-key-rotation"},
				},
				{
					Kind: "predicate",
					When: "(input.rotation-period-in-days != null)",
					Require: "(input.rotation-period-in-days == null || " +
						"input.rotation-period-in-days >= 90) && " +
						"(input.rotation-period-in-days == null || input.rotation-period-in-days <= 2560)",
					Message: "rotation-period-in-days must be between 90 and 2560",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "input.tags", Optional: true},
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
