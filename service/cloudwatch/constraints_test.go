package cloudwatch

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/testutil"
)

func TestMetricAlarmQueryRequiresExpressionOrMetric(t *testing.T) {
	schema := readLibrarySchema(t).Resources["metric-alarm"]
	query := map[string]any{"id": "q1"}
	inputs := map[string]any{
		"alarm-name":          "request-count",
		"comparison-operator": "GreaterThanThreshold",
		"evaluation-periods":  int64(1),
		"threshold":           float64(5),
		"metric-query":        []any{query},
	}
	errs := testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "exactly one")
	query["expression"] = "m1 + m2"
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	query["metric"] = map[string]any{"stat": "Maximum"}
	errs = testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "exactly one")
}

func TestMetricAlarmPeriodAndStatisticConstraints(t *testing.T) {
	schema := readLibrarySchema(t).Resources["metric-alarm"]
	inputs := map[string]any{
		"alarm-name":          "request-count",
		"comparison-operator": "GreaterThanThreshold",
		"evaluation-periods":  int64(1),
		"threshold":           float64(5),
		"metric-name":         "RequestCount",
		"extended-statistic":  "p99.9",
		"period":              int64(120),
	}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["extended-statistic"] = "median"
	inputs["period"] = int64(5)
	errs := testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 2)
	require.Contains(t, errs[0], "period must be 10")
	require.Contains(t, errs[1], "extended-statistic must be a valid percentile")

	delete(inputs, "metric-name")
	delete(inputs, "extended-statistic")
	delete(inputs, "period")
	metric := map[string]any{"stat": "Maximum", "period": int64(1)}
	query := map[string]any{"id": "q1", "period": int64(5), "metric": metric}
	inputs["metric-query"] = []any{query}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	query["period"], metric["period"], metric["stat"] = int64(-60), int64(0), "median"
	errs = testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 3)
	require.Contains(t, errs[0], "metric-query period")
	require.Contains(t, errs[1], "metric-query metric period")
	require.Contains(t, errs[2], "metric-query metric stat")
}

func TestDashboardTagConstraints(t *testing.T) {
	schema := readLibrarySchema(t).Resources["dashboard"]
	inputs := map[string]any{"name": "dashboard", "body": "{}"}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["tags"] = map[string]any{"name": "", "owner": strings.Repeat("é", 256)}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["tags"] = map[string]any{"aws:owner": "team", "owner": strings.Repeat("é", 257)}
	errs := testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 2)
	require.Contains(t, errs[0], "tag key must not begin with aws:")
	require.Contains(t, errs[1], "tag value must be at most 256 characters")
	inputs["tags"] = map[string]any{"team!": "#"}
	errs = testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 2)
	require.Contains(t, errs[0], "tag key contains invalid characters")
	require.Contains(t, errs[1], "tag value contains invalid characters")
}
