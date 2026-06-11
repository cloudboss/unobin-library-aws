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
	"github.com/cloudboss/unobin-library-aws/internal/service/cloudwatchlogs"
)

// TestLibraryRegistersCloudwatchlogsResources checks the runtime registration:
// cloudwatchlogs-log-group is present under Resources and dispatches to its
// output type.
func TestLibraryRegistersCloudwatchlogsResources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"cloudwatchlogs-log-group": reflect.TypeFor[*cloudwatchlogs.LogGroupOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestCloudwatchlogsSchemas asserts the whole derived TypeSchema for the
// cloudwatchlogs-log-group resource: its input and output field types, the
// retention and class enum constraints, and the optional tag default.
func TestCloudwatchlogsSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	cases := map[string]*runtime.TypeSchema{
		"cloudwatchlogs-log-group": {
			Inputs: map[string]typecheck.Type{
				"deletion-protection-enabled": typecheck.TOptional(typecheck.TBoolean()),
				"kms-key-id":                  typecheck.TOptional(typecheck.TString()),
				"log-group-class":             typecheck.TOptional(typecheck.TString()),
				"name":                        typecheck.TString(),
				"retention-in-days":           typecheck.TOptional(typecheck.TInteger()),
				"tags":                        typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(var.retention-in-days != null)",
					Require: "(var.retention-in-days == 0 || " +
						"var.retention-in-days == 1 || " +
						"var.retention-in-days == 3 || " +
						"var.retention-in-days == 5 || " +
						"var.retention-in-days == 7 || " +
						"var.retention-in-days == 14 || " +
						"var.retention-in-days == 30 || " +
						"var.retention-in-days == 60 || " +
						"var.retention-in-days == 90 || " +
						"var.retention-in-days == 120 || " +
						"var.retention-in-days == 150 || " +
						"var.retention-in-days == 180 || " +
						"var.retention-in-days == 365 || " +
						"var.retention-in-days == 400 || " +
						"var.retention-in-days == 545 || " +
						"var.retention-in-days == 731 || " +
						"var.retention-in-days == 1096 || " +
						"var.retention-in-days == 1827 || " +
						"var.retention-in-days == 2192 || " +
						"var.retention-in-days == 2557 || " +
						"var.retention-in-days == 2922 || " +
						"var.retention-in-days == 3288 || " +
						"var.retention-in-days == 3653)",
					Message: "retention-in-days must be a valid CloudWatch Logs retention value",
				},
				{
					Kind: "predicate",
					When: "(var.log-group-class != null)",
					Require: "(var.log-group-class == 'STANDARD' || " +
						"var.log-group-class == 'INFREQUENT_ACCESS' || " +
						"var.log-group-class == 'DELIVERY')",
					Message: "log-group-class must be STANDARD, INFREQUENT_ACCESS, or DELIVERY",
				},
			},
			Defaults: []lang.DefaultSpec{
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
