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
	"github.com/cloudboss/unobin-library-aws/internal/service/cloudwatch"
)

// TestLibraryRegistersCloudwatch checks the runtime registration: the metric
// alarm resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersCloudwatch(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"cloudwatch-metric-alarm": reflect.TypeFor[*cloudwatch.MetricAlarmOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestCloudwatchSchemas asserts the whole derived TypeSchema for the metric
// alarm: input and output field types (including the metric-query metric-math
// array), the metric-form and enum constraints, and the optional defaults.
func TestCloudwatchSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"cloudwatch-metric-alarm": {
			Inputs: map[string]typecheck.Type{
				"actions-enabled":                      typecheck.TOptional(typecheck.TBoolean()),
				"alarm-actions":                        typecheck.TList(typecheck.TString()),
				"alarm-description":                    typecheck.TOptional(typecheck.TString()),
				"alarm-name":                           typecheck.TString(),
				"comparison-operator":                  typecheck.TOptional(typecheck.TString()),
				"datapoints-to-alarm":                  typecheck.TOptional(typecheck.TInteger()),
				"dimensions":                           typecheck.TMap(typecheck.TString()),
				"evaluate-low-sample-count-percentile": typecheck.TOptional(typecheck.TString()),
				"evaluation-periods":                   typecheck.TOptional(typecheck.TInteger()),
				"extended-statistic":                   typecheck.TOptional(typecheck.TString()),
				"insufficient-data-actions":            typecheck.TList(typecheck.TString()),
				"metric-name":                          typecheck.TOptional(typecheck.TString()),
				"metric-query": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "id", Type: typecheck.TString()},
					{Name: "account-id", Type: typecheck.TString(), Optional: true},
					{Name: "expression", Type: typecheck.TString(), Optional: true},
					{Name: "label", Type: typecheck.TString(), Optional: true},
					{Name: "metric", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "metric-name", Type: typecheck.TString(), Optional: true},
						{Name: "namespace", Type: typecheck.TString(), Optional: true},
						{Name: "dimensions", Type: typecheck.TMap(typecheck.TString()), Optional: true},
						{Name: "stat", Type: typecheck.TString(), Optional: true},
						{Name: "period", Type: typecheck.TInteger(), Optional: true},
						{Name: "unit", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
					{Name: "period", Type: typecheck.TInteger(), Optional: true},
					{Name: "return-data", Type: typecheck.TBoolean(), Optional: true},
				})),
				"namespace":           typecheck.TOptional(typecheck.TString()),
				"ok-actions":          typecheck.TList(typecheck.TString()),
				"period":              typecheck.TOptional(typecheck.TInteger()),
				"statistic":           typecheck.TOptional(typecheck.TString()),
				"tags":                typecheck.TMap(typecheck.TString()),
				"threshold":           typecheck.TOptional(typecheck.TNumber()),
				"threshold-metric-id": typecheck.TOptional(typecheck.TString()),
				"treat-missing-data":  typecheck.TOptional(typecheck.TString()),
				"unit":                typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"alarm-name":                           typecheck.TString(),
				"arn":                                  typecheck.TString(),
				"evaluate-low-sample-count-percentile": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "exactly-one-of",
					Fields: []string{"var.metric-name", "var.metric-query"},
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"var.metric-query", "var.namespace", "var.dimensions", "var.period",
						"var.unit", "var.statistic", "var.extended-statistic",
					},
				},
				{
					Kind:   "at-most-one-of",
					Fields: []string{"var.statistic", "var.extended-statistic"},
				},
				{
					Kind:   "exactly-one-of",
					Fields: []string{"var.threshold", "var.threshold-metric-id"},
				},
				{
					Kind: "predicate",
					When: "(var.metric-name != null)",
					Require: "(var.comparison-operator != null) && " +
						"(var.evaluation-periods != null)",
					Message: "comparison-operator and evaluation-periods are required " +
						"when metric-name is set",
				},
				{
					Kind: "predicate",
					When: "((var.metric-query != null) && " +
						"(@core.length(var.metric-query) >= 1))",
					Require: "(var.comparison-operator != null) && " +
						"(var.evaluation-periods != null)",
					Message: "comparison-operator and evaluation-periods are required " +
						"when metric-query is set",
				},
				{
					Kind: "predicate",
					When: "(var.metric-name != null)",
					Require: "(((var.statistic != null) && " +
						"(var.extended-statistic == null)) || " +
						"((var.statistic == null) && " +
						"(var.extended-statistic != null)))",
					Message: "exactly one of statistic or extended-statistic is required " +
						"when metric-name is set",
				},
				{
					Kind: "predicate",
					When: "(var.comparison-operator != null)",
					Require: "(var.comparison-operator == 'GreaterThanOrEqualToThreshold' || " +
						"var.comparison-operator == 'GreaterThanThreshold' || " +
						"var.comparison-operator == 'LessThanThreshold' || " +
						"var.comparison-operator == 'LessThanOrEqualToThreshold' || " +
						"var.comparison-operator == 'LessThanLowerOrGreaterThanUpperThreshold' || " +
						"var.comparison-operator == 'LessThanLowerThreshold' || " +
						"var.comparison-operator == 'GreaterThanUpperThreshold')",
					Message: "comparison-operator must be a valid CloudWatch comparison operator",
				},
				{
					Kind: "predicate",
					When: "(var.statistic != null)",
					Require: "(var.statistic == 'SampleCount' || " +
						"var.statistic == 'Average' || " +
						"var.statistic == 'Sum' || " +
						"var.statistic == 'Minimum' || " +
						"var.statistic == 'Maximum')",
					Message: "statistic must be SampleCount, Average, Sum, Minimum, or Maximum",
				},
				{
					Kind: "predicate",
					When: "(var.unit != null)",
					Require: "(var.unit == 'Seconds' || " +
						"var.unit == 'Microseconds' || " +
						"var.unit == 'Milliseconds' || " +
						"var.unit == 'Bytes' || " +
						"var.unit == 'Kilobytes' || " +
						"var.unit == 'Megabytes' || " +
						"var.unit == 'Gigabytes' || " +
						"var.unit == 'Terabytes' || " +
						"var.unit == 'Bits' || " +
						"var.unit == 'Kilobits' || " +
						"var.unit == 'Megabits' || " +
						"var.unit == 'Gigabits' || " +
						"var.unit == 'Terabits' || " +
						"var.unit == 'Percent' || " +
						"var.unit == 'Count' || " +
						"var.unit == 'Bytes/Second' || " +
						"var.unit == 'Kilobytes/Second' || " +
						"var.unit == 'Megabytes/Second' || " +
						"var.unit == 'Gigabytes/Second' || " +
						"var.unit == 'Terabytes/Second' || " +
						"var.unit == 'Bits/Second' || " +
						"var.unit == 'Kilobits/Second' || " +
						"var.unit == 'Megabits/Second' || " +
						"var.unit == 'Gigabits/Second' || " +
						"var.unit == 'Terabits/Second' || " +
						"var.unit == 'Count/Second' || " +
						"var.unit == 'None')",
					Message: "unit must be a valid CloudWatch standard unit",
				},
				{
					Kind: "predicate",
					When: "(var.treat-missing-data != null)",
					Require: "(var.treat-missing-data == 'breaching' || " +
						"var.treat-missing-data == 'notBreaching' || " +
						"var.treat-missing-data == 'ignore' || " +
						"var.treat-missing-data == 'missing')",
					Message: "treat-missing-data must be breaching, notBreaching, ignore, or missing",
				},
				{
					Kind: "predicate",
					When: "(var.evaluate-low-sample-count-percentile != null)",
					Require: "(var.evaluate-low-sample-count-percentile == 'evaluate' || " +
						"var.evaluate-low-sample-count-percentile == 'ignore')",
					Message: "evaluate-low-sample-count-percentile must be evaluate or ignore",
				},
				{
					Kind: "predicate",
					When: "(var.datapoints-to-alarm != null)",
					Require: "(var.datapoints-to-alarm == null || " +
						"var.datapoints-to-alarm >= 1)",
					Message: "datapoints-to-alarm must be at least 1",
				},
				{
					Kind: "predicate",
					When: "(var.evaluation-periods != null)",
					Require: "(var.evaluation-periods == null || " +
						"var.evaluation-periods >= 1)",
					Message: "evaluation-periods must be at least 1",
				},
				{
					Kind: "predicate",
					When: "((var.ok-actions != null) && " +
						"(@core.length(var.ok-actions) >= 1))",
					Require: "(var.ok-actions == null || " +
						"@core.length(var.ok-actions) <= 5)",
					Message: "ok-actions allows at most 5 actions",
				},
				{
					Kind: "predicate",
					When: "((var.insufficient-data-actions != null) && " +
						"(@core.length(var.insufficient-data-actions) >= 1))",
					Require: "(var.insufficient-data-actions == null || " +
						"@core.length(var.insufficient-data-actions) <= 5)",
					Message: "insufficient-data-actions allows at most 5 actions",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"var.metric-query[*].expression", "var.metric-query[*].metric",
					},
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.alarm-actions", Optional: true},
				{Field: "var.ok-actions", Optional: true},
				{Field: "var.insufficient-data-actions", Optional: true},
				{Field: "var.dimensions", Optional: true},
				{Field: "var.metric-query", Optional: true},
				{Field: "var.tags", Optional: true},
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
