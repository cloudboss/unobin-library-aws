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
	"github.com/cloudboss/unobin-library-aws/internal/service/ecr"
)

// TestLibraryRegistersEcr checks the runtime registration: the repository
// resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersEcr(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"ecr-repository": reflect.TypeFor[*ecr.RepositoryOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestEcrSchemas asserts the whole derived TypeSchema for the repository:
// input and output field types, the mutability and encryption enums, the
// exclusion-filter rules, and the optional defaults.
func TestEcrSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"ecr-repository": {
			Inputs: map[string]typecheck.Type{
				"encryption-configuration": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "encryption-type", Type: typecheck.TString()},
					{Name: "kms-key", Type: typecheck.TString(), Optional: true},
				})),
				"force-delete":         typecheck.TOptional(typecheck.TBoolean()),
				"image-tag-mutability": typecheck.TOptional(typecheck.TString()),
				"image-tag-mutability-exclusion-filters": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "filter", Type: typecheck.TString()},
					{Name: "filter-type", Type: typecheck.TString()},
				})),
				"lifecycle-policy":  typecheck.TOptional(typecheck.TString()),
				"name":              typecheck.TString(),
				"repository-policy": typecheck.TOptional(typecheck.TString()),
				"scan-on-push":      typecheck.TOptional(typecheck.TBoolean()),
				"tags":              typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":            typecheck.TString(),
				"name":           typecheck.TString(),
				"registry-id":    typecheck.TString(),
				"repository-uri": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(input.image-tag-mutability != null)",
					Require: "(input.image-tag-mutability == 'MUTABLE' || " +
						"input.image-tag-mutability == 'IMMUTABLE' || " +
						"input.image-tag-mutability == 'MUTABLE_WITH_EXCLUSION' || " +
						"input.image-tag-mutability == 'IMMUTABLE_WITH_EXCLUSION')",
					Message: "image-tag-mutability must be a valid tag mutability setting",
				},
				{
					Kind: "predicate",
					When: "((input.image-tag-mutability-exclusion-filters != null) && " +
						"(@core.length(input.image-tag-mutability-exclusion-filters) >= 1))",
					Require: "(input.image-tag-mutability == 'MUTABLE_WITH_EXCLUSION' || " +
						"input.image-tag-mutability == 'IMMUTABLE_WITH_EXCLUSION')",
					Message: "exclusion filters require a WITH_EXCLUSION image-tag-mutability",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(input.image-tag-mutability-exclusion-filters == null || " +
						"@core.length(input.image-tag-mutability-exclusion-filters) <= 5)",
					Message: "image-tag-mutability-exclusion-filters holds at most 5 filters",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@each.value.filter-type == 'WILDCARD')",
					Message: "a filter type must be WILDCARD",
					ForEach: "input.image-tag-mutability-exclusion-filters",
				},
				{
					Kind: "predicate",
					When: "(input.encryption-configuration.encryption-type != null)",
					Require: "(input.encryption-configuration.encryption-type == 'AES256' || " +
						"input.encryption-configuration.encryption-type == 'KMS' || " +
						"input.encryption-configuration.encryption-type == 'KMS_DSSE')",
					Message: "encryption-type must be AES256, KMS, or KMS_DSSE",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "input.image-tag-mutability-exclusion-filters", Optional: true},
				{Field: "input.tags", Optional: true},
			},
		},
	}
	for key, want := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, want, schema.Resources[key])
		})
	}
}
