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
	"github.com/cloudboss/unobin-library-aws/internal/service/acm"
)

// TestLibraryRegistersAcmResources checks the runtime registration:
// acm-certificate is present under Resources and dispatches to its output type.
func TestLibraryRegistersAcmResources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"acm-certificate":            reflect.TypeFor[*acm.CertificateOutput](),
		"acm-certificate-validation": reflect.TypeFor[*acm.CertificateValidationOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestLibraryRegistersAcmDataSources checks the runtime registration of the
// certificate lookup under DataSources.
func TestLibraryRegistersAcmDataSources(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.DataSources, "acm-certificate-data")
	assert.Equal(t, reflect.TypeFor[*acm.CertificateDataOutput](),
		lib.DataSources["acm-certificate-data"].OutputType())
}

// TestAcmSchemas asserts the derived TypeSchema for the ACM resources and data
// source: the certificate resource's request and import modes, the validation
// resource, and the certificate lookup's filters, outputs, constraints, and
// defaults.
func TestAcmSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

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
				"subject-alternative-names": typecheck.TOptional(typecheck.TList(typecheck.TString())),
				"tags":                      typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"validation-method":         typecheck.TOptional(typecheck.TString()),
				"validation-option": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "domain-name", Type: typecheck.TString()},
					{Name: "validation-domain", Type: typecheck.TString()},
				}))),
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
					Fields: []string{"input.domain-name", "input.private-key"},
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"input.private-key",
						"input.domain-name",
						"input.certificate-authority-arn",
						"input.key-algorithm",
						"input.validation-method",
						"input.options",
					},
				},
				{
					Kind: "predicate",
					When: "(input.private-key != null)",
					Require: "!((input.subject-alternative-names != null) && " +
						"(@core.length(input.subject-alternative-names) >= 1))",
					Message: "subject-alternative-names cannot be set with private-key",
				},
				{
					Kind: "predicate",
					When: "(input.private-key != null)",
					Require: "!((input.validation-option != null) && " +
						"(@core.length(input.validation-option) >= 1))",
					Message: "validation-option cannot be set with private-key",
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"input.domain-name",
						"input.certificate-body",
						"input.private-key",
						"input.certificate-chain",
					},
				},
				{
					Kind:   "required-with",
					Fields: []string{"input.certificate-body", "input.private-key"},
				},
				{
					Kind:   "forbidden-with",
					Fields: []string{"input.certificate-authority-arn", "input.validation-method"},
				},
				{
					Kind: "predicate",
					When: "(input.domain-name != null)",
					Require: "((input.certificate-authority-arn != null) || " +
						"(input.validation-method != null))",
					Message: "a domain-name request requires certificate-authority-arn or " +
						"validation-method",
				},
				{
					Kind: "predicate",
					When: "(input.validation-method != null)",
					Require: "(input.validation-method == 'DNS' || " +
						"input.validation-method == 'EMAIL')",
					Message: "validation-method must be DNS or EMAIL",
				},
				{
					Kind: "predicate",
					When: "(input.key-algorithm != null)",
					Require: "(input.key-algorithm == 'RSA_1024' || " +
						"input.key-algorithm == 'RSA_2048' || " +
						"input.key-algorithm == 'RSA_3072' || " +
						"input.key-algorithm == 'RSA_4096' || " +
						"input.key-algorithm == 'EC_prime256v1' || " +
						"input.key-algorithm == 'EC_secp384r1' || " +
						"input.key-algorithm == 'EC_secp521r1')",
					Message: "key-algorithm must be a valid ACM key algorithm",
				},
				{
					Kind: "predicate",
					When: "(input.options.certificate-transparency-logging-preference != null)",
					Require: "(input.options.certificate-transparency-logging-preference == " +
						"'ENABLED' || " +
						"input.options.certificate-transparency-logging-preference == " +
						"'DISABLED')",
					Message: "options certificate-transparency-logging-preference must be " +
						"ENABLED or DISABLED",
				},
				{
					Kind: "predicate",
					When: "(input.options.export != null)",
					Require: "(input.options.export == 'ENABLED' || " +
						"input.options.export == 'DISABLED')",
					Message: "options export must be ENABLED or DISABLED",
				},
			},
		},
		"acm-certificate-validation": {
			Inputs: map[string]typecheck.Type{
				"certificate-arn":         typecheck.TString(),
				"validation-record-fqdns": typecheck.TOptional(typecheck.TList(typecheck.TString())),
			},
			Outputs: map[string]typecheck.Type{
				"certificate-arn": typecheck.TString(),
			},
		},
	}

	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assertTypeSchemaEqual(t, want, schema.Resources[key])
		})
	}

	dataSources := map[string]*runtime.TypeSchema{
		"acm-certificate-data": {
			Inputs: map[string]typecheck.Type{
				"domain":      typecheck.TOptional(typecheck.TString()),
				"key-types":   typecheck.TOptional(typecheck.TList(typecheck.TString())),
				"most-recent": typecheck.TBoolean(),
				"statuses":    typecheck.TOptional(typecheck.TList(typecheck.TString())),
				"tags":        typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"types":       typecheck.TOptional(typecheck.TList(typecheck.TString())),
			},
			Outputs: map[string]typecheck.Type{
				"arn":               typecheck.TString(),
				"certificate":       typecheck.TOptional(typecheck.TString()),
				"certificate-chain": typecheck.TOptional(typecheck.TString()),
				"domain":            typecheck.TString(),
				"status":            typecheck.TString(),
				"tags":              typecheck.TMap(typecheck.TString()),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "true",
					Require: "((input.domain != null) || ((input.tags != null) && " +
						"(@core.length(input.tags) >= 1)))",
					Message: "a certificate lookup needs domain or tags",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value == 'RSA_1024' || " +
						"@each.value == 'RSA_2048' || " +
						"@each.value == 'RSA_3072' || " +
						"@each.value == 'RSA_4096' || " +
						"@each.value == 'EC_prime256v1' || " +
						"@each.value == 'EC_secp384r1' || " +
						"@each.value == 'EC_secp521r1')",
					Message: "key-types entries must be valid ACM key algorithms",
					ForEach: "input.key-types ?? []",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value == 'PENDING_VALIDATION' || " +
						"@each.value == 'ISSUED' || " +
						"@each.value == 'INACTIVE' || " +
						"@each.value == 'EXPIRED' || " +
						"@each.value == 'VALIDATION_TIMED_OUT' || " +
						"@each.value == 'REVOKED' || " +
						"@each.value == 'FAILED')",
					Message: "statuses entries must be valid ACM certificate statuses",
					ForEach: "input.statuses ?? []",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value == 'IMPORTED' || " +
						"@each.value == 'AMAZON_ISSUED' || " +
						"@each.value == 'PRIVATE')",
					Message: "types entries must be valid ACM certificate types",
					ForEach: "input.types ?? []",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "input.most-recent", Value: "false"},
			},
		},
	}

	for key, want := range dataSources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.DataSources, key)
			assertTypeSchemaEqual(t, want, schema.DataSources[key])
		})
	}
}
