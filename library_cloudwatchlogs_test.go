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
// the CloudWatch Logs resources are present under Resources and dispatch to
// their output types.
func TestLibraryRegistersCloudwatchlogsResources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"cloudwatchlogs-log-group":           reflect.TypeFor[*cloudwatchlogs.LogGroupOutput](),
		"cloudwatchlogs-metric-filter":       reflect.TypeFor[*cloudwatchlogs.MetricFilterOutput](),
		"cloudwatchlogs-subscription-filter": reflect.TypeFor[*cloudwatchlogs.SubscriptionFilterOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestCloudwatchlogsSchemas asserts the whole derived TypeSchema for the
// CloudWatch Logs resources: their input and output field types, constraints,
// and defaults.
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
		"cloudwatchlogs-subscription-filter": {
			Inputs: map[string]typecheck.Type{
				"apply-on-transformed-logs": typecheck.TOptional(typecheck.TBoolean()),
				"destination-arn":           typecheck.TString(),
				"distribution":              typecheck.TString(),
				"emit-system-fields":        typecheck.TList(typecheck.TString()),
				"field-selection-criteria":  typecheck.TOptional(typecheck.TString()),
				"filter-pattern":            typecheck.TString(),
				"name":                      typecheck.TString(),
				"log-group-name":            typecheck.TString(),
				"role-arn":                  typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"apply-on-transformed-logs": typecheck.TBoolean(),
				"log-group-name":            typecheck.TString(),
				"name":                      typecheck.TString(),
				"role-arn":                  typecheck.TOptional(typecheck.TString()),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "(var.distribution != null)",
					Require: "(var.distribution == 'ByLogStream' || var.distribution == 'Random')",
					Message: "distribution must be ByLogStream or Random",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value == '@aws.account' || " +
						"@each.value == '@aws.region')",
					Message: "emit-system-fields entries must be @aws.account or @aws.region",
					ForEach: "var.emit-system-fields",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.distribution", Value: "'ByLogStream'"},
				{Field: "var.emit-system-fields", Optional: true},
			},
		},
		"cloudwatchlogs-metric-filter": {
			Inputs: map[string]typecheck.Type{
				"apply-on-transformed-logs": typecheck.TOptional(typecheck.TBoolean()),
				"filter-name":               typecheck.TString(),
				"filter-pattern":            typecheck.TString(),
				"log-group-name":            typecheck.TString(),
				"metric-transformation": typecheck.TObject([]typecheck.ObjectField{
					{Name: "default-value", Type: typecheck.TNumber(), Optional: true},
					{Name: "dimensions", Type: typecheck.TMap(typecheck.TString()), Optional: true},
					{Name: "metric-name", Type: typecheck.TString()},
					{Name: "metric-namespace", Type: typecheck.TString()},
					{Name: "metric-value", Type: typecheck.TString()},
					{Name: "unit", Type: typecheck.TString(), Optional: true},
				}),
			},
			Outputs: map[string]typecheck.Type{
				"apply-on-transformed-logs": typecheck.TBoolean(),
				"filter-name":               typecheck.TString(),
				"filter-pattern":            typecheck.TString(),
				"log-group-name":            typecheck.TString(),
				"metric-transformation": typecheck.TObject([]typecheck.ObjectField{
					{Name: "default-value", Type: typecheck.TNumber(), Optional: true},
					{Name: "dimensions", Type: typecheck.TMap(typecheck.TString()), Optional: true},
					{Name: "metric-name", Type: typecheck.TString()},
					{Name: "metric-namespace", Type: typecheck.TString()},
					{Name: "metric-value", Type: typecheck.TString()},
					{Name: "unit", Type: typecheck.TString()},
				}),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(var.metric-transformation.unit != null)",
					Require: "(var.metric-transformation.unit == 'Seconds' || " +
						"var.metric-transformation.unit == 'Microseconds' || " +
						"var.metric-transformation.unit == 'Milliseconds' || " +
						"var.metric-transformation.unit == 'Bytes' || " +
						"var.metric-transformation.unit == 'Kilobytes' || " +
						"var.metric-transformation.unit == 'Megabytes' || " +
						"var.metric-transformation.unit == 'Gigabytes' || " +
						"var.metric-transformation.unit == 'Terabytes' || " +
						"var.metric-transformation.unit == 'Bits' || " +
						"var.metric-transformation.unit == 'Kilobits' || " +
						"var.metric-transformation.unit == 'Megabits' || " +
						"var.metric-transformation.unit == 'Gigabits' || " +
						"var.metric-transformation.unit == 'Terabits' || " +
						"var.metric-transformation.unit == 'Percent' || " +
						"var.metric-transformation.unit == 'Count' || " +
						"var.metric-transformation.unit == 'Bytes/Second' || " +
						"var.metric-transformation.unit == 'Kilobytes/Second' || " +
						"var.metric-transformation.unit == 'Megabytes/Second' || " +
						"var.metric-transformation.unit == 'Gigabytes/Second' || " +
						"var.metric-transformation.unit == 'Terabytes/Second' || " +
						"var.metric-transformation.unit == 'Bits/Second' || " +
						"var.metric-transformation.unit == 'Kilobits/Second' || " +
						"var.metric-transformation.unit == 'Megabits/Second' || " +
						"var.metric-transformation.unit == 'Gigabits/Second' || " +
						"var.metric-transformation.unit == 'Terabits/Second' || " +
						"var.metric-transformation.unit == 'Count/Second' || " +
						"var.metric-transformation.unit == 'None')",
					Message: "metric-transformation unit must be a valid CloudWatch unit",
				},
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
