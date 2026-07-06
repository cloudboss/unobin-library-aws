package cloudwatch

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/cloudwatch"
)

// TestLibraryRegistersCloudwatch checks the runtime registration: the metric
// alarm resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersCloudwatch(t *testing.T) {
	lib := Library()
	resources := map[string]reflect.Type{
		"metric-alarm": reflect.TypeFor[*svc.MetricAlarmResourceOutput](),
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
		"metric-alarm": {
			Inputs: map[string]typecheck.Type{
				"actions-enabled":                      typecheck.TOptional(typecheck.TBoolean()),
				"alarm-actions":                        typecheck.TOptional(typecheck.TList(typecheck.TString())),
				"alarm-description":                    typecheck.TOptional(typecheck.TString()),
				"alarm-name":                           typecheck.TString(),
				"comparison-operator":                  typecheck.TOptional(typecheck.TString()),
				"datapoints-to-alarm":                  typecheck.TOptional(typecheck.TInteger()),
				"dimensions":                           typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"evaluate-low-sample-count-percentile": typecheck.TOptional(typecheck.TString()),
				"evaluation-periods":                   typecheck.TOptional(typecheck.TInteger()),
				"extended-statistic":                   typecheck.TOptional(typecheck.TString()),
				"insufficient-data-actions":            typecheck.TOptional(typecheck.TList(typecheck.TString())),
				"metric-name":                          typecheck.TOptional(typecheck.TString()),
				"metric-query": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
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
				}))),
				"namespace":           typecheck.TOptional(typecheck.TString()),
				"ok-actions":          typecheck.TOptional(typecheck.TList(typecheck.TString())),
				"period":              typecheck.TOptional(typecheck.TInteger()),
				"statistic":           typecheck.TOptional(typecheck.TString()),
				"tags":                typecheck.TOptional(typecheck.TMap(typecheck.TString())),
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
					Kind: "predicate",
					When: "true",
					Require: "(((input.metric-name != null) && " +
						"!(@core.length(input.metric-query ?? []) >= 1)) || " +
						"((input.metric-name == null) && " +
						"(@core.length(input.metric-query ?? []) >= 1)))",
					Message: "exactly one of metric-name or metric-query is required",
				},
				{
					Kind: "predicate",
					When: "(@core.length(input.metric-query ?? []) >= 1)",
					Require: "(input.namespace == null) && " +
						"!((input.dimensions != null) && " +
						"(@core.length(input.dimensions) >= 1)) && " +
						"(input.period == null) && (input.unit == null) && " +
						"(input.statistic == null) && (input.extended-statistic == null)",
					Message: "metric-query cannot combine with single-metric fields",
				},
				{
					Kind:   "at-most-one-of",
					Fields: []string{"input.statistic", "input.extended-statistic"},
				},
				{
					Kind:   "exactly-one-of",
					Fields: []string{"input.threshold", "input.threshold-metric-id"},
				},
				{
					Kind: "predicate",
					When: "(input.metric-name != null)",
					Require: "(input.comparison-operator != null) && " +
						"(input.evaluation-periods != null)",
					Message: "comparison-operator and evaluation-periods are required " +
						"when metric-name is set",
				},
				{
					Kind: "predicate",
					When: "(@core.length(input.metric-query ?? []) >= 1)",
					Require: "(input.comparison-operator != null) && " +
						"(input.evaluation-periods != null)",
					Message: "comparison-operator and evaluation-periods are required " +
						"when metric-query is set",
				},
				{
					Kind: "predicate",
					When: "(input.metric-name != null)",
					Require: "(((input.statistic != null) && " +
						"(input.extended-statistic == null)) || " +
						"((input.statistic == null) && " +
						"(input.extended-statistic != null)))",
					Message: "exactly one of statistic or extended-statistic is required " +
						"when metric-name is set",
				},
				{
					Kind: "predicate",
					When: "(input.comparison-operator != null)",
					Require: "(input.comparison-operator == 'GreaterThanOrEqualToThreshold' || " +
						"input.comparison-operator == 'GreaterThanThreshold' || " +
						"input.comparison-operator == 'LessThanThreshold' || " +
						"input.comparison-operator == 'LessThanOrEqualToThreshold' || " +
						"input.comparison-operator == 'LessThanLowerOrGreaterThanUpperThreshold' || " +
						"input.comparison-operator == 'LessThanLowerThreshold' || " +
						"input.comparison-operator == 'GreaterThanUpperThreshold')",
					Message: "comparison-operator must be a valid CloudWatch comparison operator",
				},
				{
					Kind: "predicate",
					When: "(input.statistic != null)",
					Require: "(input.statistic == 'SampleCount' || " +
						"input.statistic == 'Average' || " +
						"input.statistic == 'Sum' || " +
						"input.statistic == 'Minimum' || " +
						"input.statistic == 'Maximum')",
					Message: "statistic must be SampleCount, Average, Sum, Minimum, or Maximum",
				},
				{
					Kind: "predicate",
					When: "(input.unit != null)",
					Require: "(input.unit == 'Seconds' || " +
						"input.unit == 'Microseconds' || " +
						"input.unit == 'Milliseconds' || " +
						"input.unit == 'Bytes' || " +
						"input.unit == 'Kilobytes' || " +
						"input.unit == 'Megabytes' || " +
						"input.unit == 'Gigabytes' || " +
						"input.unit == 'Terabytes' || " +
						"input.unit == 'Bits' || " +
						"input.unit == 'Kilobits' || " +
						"input.unit == 'Megabits' || " +
						"input.unit == 'Gigabits' || " +
						"input.unit == 'Terabits' || " +
						"input.unit == 'Percent' || " +
						"input.unit == 'Count' || " +
						"input.unit == 'Bytes/Second' || " +
						"input.unit == 'Kilobytes/Second' || " +
						"input.unit == 'Megabytes/Second' || " +
						"input.unit == 'Gigabytes/Second' || " +
						"input.unit == 'Terabytes/Second' || " +
						"input.unit == 'Bits/Second' || " +
						"input.unit == 'Kilobits/Second' || " +
						"input.unit == 'Megabits/Second' || " +
						"input.unit == 'Gigabits/Second' || " +
						"input.unit == 'Terabits/Second' || " +
						"input.unit == 'Count/Second' || " +
						"input.unit == 'None')",
					Message: "unit must be a valid CloudWatch standard unit",
				},
				{
					Kind: "predicate",
					When: "(input.treat-missing-data != null)",
					Require: "(input.treat-missing-data == 'breaching' || " +
						"input.treat-missing-data == 'notBreaching' || " +
						"input.treat-missing-data == 'ignore' || " +
						"input.treat-missing-data == 'missing')",
					Message: "treat-missing-data must be breaching, notBreaching, ignore, or missing",
				},
				{
					Kind: "predicate",
					When: "(input.evaluate-low-sample-count-percentile != null)",
					Require: "(input.evaluate-low-sample-count-percentile == 'evaluate' || " +
						"input.evaluate-low-sample-count-percentile == 'ignore')",
					Message: "evaluate-low-sample-count-percentile must be evaluate or ignore",
				},
				{
					Kind: "predicate",
					When: "(input.datapoints-to-alarm != null)",
					Require: "(input.datapoints-to-alarm == null || " +
						"input.datapoints-to-alarm >= 1)",
					Message: "datapoints-to-alarm must be at least 1",
				},
				{
					Kind: "predicate",
					When: "(input.evaluation-periods != null)",
					Require: "(input.evaluation-periods == null || " +
						"input.evaluation-periods >= 1)",
					Message: "evaluation-periods must be at least 1",
				},
				{
					Kind: "predicate",
					When: "((input.ok-actions != null) && " +
						"(@core.length(input.ok-actions) >= 1))",
					Require: "(input.ok-actions == null || " +
						"@core.length(input.ok-actions) <= 5)",
					Message: "ok-actions allows at most 5 actions",
				},
				{
					Kind: "predicate",
					When: "((input.insufficient-data-actions != null) && " +
						"(@core.length(input.insufficient-data-actions) >= 1))",
					Require: "(input.insufficient-data-actions == null || " +
						"@core.length(input.insufficient-data-actions) <= 5)",
					Message: "insufficient-data-actions allows at most 5 actions",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.metric-query[*].expression", "input.metric-query[*].metric",
					},
				},
			},
		},
	}
	for key, want := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assertTypeSchemaEqual(t, want, schema.Resources[key])
		})
	}
}
