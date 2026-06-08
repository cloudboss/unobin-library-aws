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
	"github.com/cloudboss/unobin-library-aws/internal/service/acm"
)

// TestLibraryRegistersAcmResources checks the runtime registration:
// acm-certificate is present under Resources and dispatches to its output type.
func TestLibraryRegistersAcmResources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"acm-certificate": reflect.TypeFor[*acm.CertificateOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestAcmSchemas asserts the whole derived TypeSchema for the acm-certificate
// resource: the request-mode and import-mode inputs, the computed outputs
// including the domain-validation-options downstream validation reads, the
// mutually-exclusive and enum constraints, and the optional defaults.
func TestAcmSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	cases := map[string]*runtime.TypeSchema{
		"acm-certificate": {
			Inputs: map[string]typecheck.Type{
				"certificate-authority-arn": typecheck.TOptional(typecheck.TString()),
				"certificate-body":          typecheck.TOptional(typecheck.TString()),
				"certificate-chain":         typecheck.TOptional(typecheck.TString()),
				"domain-name":               typecheck.TOptional(typecheck.TString()),
				"key-algorithm":             typecheck.TOptional(typecheck.TString()),
				"options": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "certificate-transparency-logging-preference",
						Type: typecheck.TString(), Optional: true},
					{Name: "export", Type: typecheck.TString(), Optional: true},
				})),
				"private-key":               typecheck.TOptional(typecheck.TString()),
				"subject-alternative-names": typecheck.TList(typecheck.TString()),
				"tags":                      typecheck.TMap(typecheck.TString()),
				"validation-method":         typecheck.TOptional(typecheck.TString()),
				"validation-option": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "domain-name", Type: typecheck.TString()},
					{Name: "validation-domain", Type: typecheck.TString()},
				})),
			},
			Outputs: map[string]typecheck.Type{
				"arn":         typecheck.TString(),
				"domain-name": typecheck.TString(),
				"domain-validation-options": typecheck.TList(typecheck.TObject(
					[]typecheck.ObjectField{
						{Name: "domain-name", Type: typecheck.TString()},
						{Name: "resource-record-name", Type: typecheck.TString()},
						{Name: "resource-record-type", Type: typecheck.TString()},
						{Name: "resource-record-value", Type: typecheck.TString()},
					})),
				"not-after":           typecheck.TString(),
				"not-before":          typecheck.TString(),
				"renewal-eligibility": typecheck.TString(),
				"status":              typecheck.TString(),
				"type":                typecheck.TString(),
				"validation-emails":   typecheck.TList(typecheck.TString()),
			},
			SensitiveInputs: []string{"private-key"},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "exactly-one-of",
					Fields: []string{"var.domain-name", "var.private-key"},
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"var.private-key",
						"var.domain-name",
						"var.certificate-authority-arn",
						"var.key-algorithm",
						"var.subject-alternative-names",
						"var.validation-method",
						"var.validation-option",
						"var.options",
					},
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"var.domain-name",
						"var.certificate-body",
						"var.private-key",
						"var.certificate-chain",
					},
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.certificate-body", "var.private-key"},
				},
				{
					Kind:   "forbidden-with",
					Fields: []string{"var.certificate-authority-arn", "var.validation-method"},
				},
				{
					Kind: "predicate",
					When: "(var.domain-name != null)",
					Require: "((var.certificate-authority-arn != null) || " +
						"(var.validation-method != null))",
					Message: "a domain-name request requires certificate-authority-arn or " +
						"validation-method",
				},
				{
					Kind: "predicate",
					When: "(var.validation-method != null)",
					Require: "(var.validation-method == 'DNS' || " +
						"var.validation-method == 'EMAIL')",
					Message: "validation-method must be DNS or EMAIL",
				},
				{
					Kind: "predicate",
					When: "(var.key-algorithm != null)",
					Require: "(var.key-algorithm == 'RSA_1024' || " +
						"var.key-algorithm == 'RSA_2048' || " +
						"var.key-algorithm == 'RSA_3072' || " +
						"var.key-algorithm == 'RSA_4096' || " +
						"var.key-algorithm == 'EC_prime256v1' || " +
						"var.key-algorithm == 'EC_secp384r1' || " +
						"var.key-algorithm == 'EC_secp521r1')",
					Message: "key-algorithm must be a valid ACM key algorithm",
				},
				{
					Kind: "predicate",
					When: "(var.options.certificate-transparency-logging-preference != null)",
					Require: "(var.options.certificate-transparency-logging-preference == " +
						"'ENABLED' || " +
						"var.options.certificate-transparency-logging-preference == " +
						"'DISABLED')",
					Message: "options certificate-transparency-logging-preference must be " +
						"ENABLED or DISABLED",
				},
				{
					Kind: "predicate",
					When: "(var.options.export != null)",
					Require: "(var.options.export == 'ENABLED' || " +
						"var.options.export == 'DISABLED')",
					Message: "options export must be ENABLED or DISABLED",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.subject-alternative-names", Optional: true},
				{Field: "var.validation-option", Optional: true},
				{Field: "var.tags", Optional: true},
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
