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
		"cloudwatchlogs-resource-policy":     reflect.TypeFor[*cloudwatchlogs.ResourcePolicyOutput](),
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
					When: "(input.retention-in-days != null)",
					Require: "(input.retention-in-days == 0 || " +
						"input.retention-in-days == 1 || " +
						"input.retention-in-days == 3 || " +
						"input.retention-in-days == 5 || " +
						"input.retention-in-days == 7 || " +
						"input.retention-in-days == 14 || " +
						"input.retention-in-days == 30 || " +
						"input.retention-in-days == 60 || " +
						"input.retention-in-days == 90 || " +
						"input.retention-in-days == 120 || " +
						"input.retention-in-days == 150 || " +
						"input.retention-in-days == 180 || " +
						"input.retention-in-days == 365 || " +
						"input.retention-in-days == 400 || " +
						"input.retention-in-days == 545 || " +
						"input.retention-in-days == 731 || " +
						"input.retention-in-days == 1096 || " +
						"input.retention-in-days == 1827 || " +
						"input.retention-in-days == 2192 || " +
						"input.retention-in-days == 2557 || " +
						"input.retention-in-days == 2922 || " +
						"input.retention-in-days == 3288 || " +
						"input.retention-in-days == 3653)",
					Message: "retention-in-days must be a valid CloudWatch Logs retention value",
				},
				{
					Kind: "predicate",
					When: "(input.log-group-class != null)",
					Require: "(input.log-group-class == 'STANDARD' || " +
						"input.log-group-class == 'INFREQUENT_ACCESS' || " +
						"input.log-group-class == 'DELIVERY')",
					Message: "log-group-class must be STANDARD, INFREQUENT_ACCESS, or DELIVERY",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "input.tags", Optional: true},
			},
		},
		"cloudwatchlogs-resource-policy": {
			Inputs: map[string]typecheck.Type{
				"policy-document": typecheck.TString(),
				"policy-name":     typecheck.TOptional(typecheck.TString()),
				"resource-arn":    typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"policy-document": typecheck.TString(),
				"policy-name":     typecheck.TOptional(typecheck.TString()),
				"policy-scope":    typecheck.TString(),
				"resource-arn":    typecheck.TOptional(typecheck.TString()),
				"revision-id":     typecheck.TOptional(typecheck.TString()),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "exactly-one-of",
					Fields: []string{"input.policy-name", "input.resource-arn"},
				},
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
					When:    "(input.distribution != null)",
					Require: "(input.distribution == 'ByLogStream' || input.distribution == 'Random')",
					Message: "distribution must be ByLogStream or Random",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value == '@aws.account' || " +
						"@each.value == '@aws.region')",
					Message: "emit-system-fields entries must be @aws.account or @aws.region",
					ForEach: "input.emit-system-fields",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "input.distribution", Value: "'ByLogStream'"},
				{Field: "input.emit-system-fields", Optional: true},
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
					When: "(input.metric-transformation.unit != null)",
					Require: "(input.metric-transformation.unit == 'Seconds' || " +
						"input.metric-transformation.unit == 'Microseconds' || " +
						"input.metric-transformation.unit == 'Milliseconds' || " +
						"input.metric-transformation.unit == 'Bytes' || " +
						"input.metric-transformation.unit == 'Kilobytes' || " +
						"input.metric-transformation.unit == 'Megabytes' || " +
						"input.metric-transformation.unit == 'Gigabytes' || " +
						"input.metric-transformation.unit == 'Terabytes' || " +
						"input.metric-transformation.unit == 'Bits' || " +
						"input.metric-transformation.unit == 'Kilobits' || " +
						"input.metric-transformation.unit == 'Megabits' || " +
						"input.metric-transformation.unit == 'Gigabits' || " +
						"input.metric-transformation.unit == 'Terabits' || " +
						"input.metric-transformation.unit == 'Percent' || " +
						"input.metric-transformation.unit == 'Count' || " +
						"input.metric-transformation.unit == 'Bytes/Second' || " +
						"input.metric-transformation.unit == 'Kilobytes/Second' || " +
						"input.metric-transformation.unit == 'Megabytes/Second' || " +
						"input.metric-transformation.unit == 'Gigabytes/Second' || " +
						"input.metric-transformation.unit == 'Terabytes/Second' || " +
						"input.metric-transformation.unit == 'Bits/Second' || " +
						"input.metric-transformation.unit == 'Kilobits/Second' || " +
						"input.metric-transformation.unit == 'Megabits/Second' || " +
						"input.metric-transformation.unit == 'Gigabits/Second' || " +
						"input.metric-transformation.unit == 'Terabits/Second' || " +
						"input.metric-transformation.unit == 'Count/Second' || " +
						"input.metric-transformation.unit == 'None')",
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
