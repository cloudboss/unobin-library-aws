package cloudwatch

import (
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// MetricAlarmMetricQuery is one element of the metric-math array, an SDK
// MetricDataQuery. Each element either retrieves a metric (the metric block)
// or computes a math expression over other elements (expression); within a
// single element exactly one of the two is set. Id ties the element to its
// result and lets an expression refer to it. ReturnData marks the one element
// whose value the alarm watches. AccountId names the account a cross-account
// element reads from. Period is the granularity of the returned data points.
type MetricAlarmMetricQuery struct {
	Id         string                 `ub:"id"`
	AccountId  *string                `ub:"account-id"`
	Expression *string                `ub:"expression"`
	Label      *string                `ub:"label"`
	Metric     *MetricAlarmMetricStat `ub:"metric"`
	Period     *int64                 `ub:"period"`
	ReturnData *bool                  `ub:"return-data"`
}

// MetricAlarmMetricStat is the metric block of a metric-query element, the SDK
// MetricStat flattened together with its Metric. MetricName, Namespace, and
// Dimensions identify the metric to retrieve; Stat is the statistic to apply,
// either a standard statistic or a percentile; Period is the granularity; Unit
// the unit of measure. The SDK marks Stat, Period, and Metric required within a
// MetricStat, but the whole block is optional within an element (an element may
// instead hold an expression), so each is a pointer here. Dimensions is a
// pointer map so it can be omitted within the block, since a nested field
// cannot take a defaults marker.
type MetricAlarmMetricStat struct {
	MetricName *string            `ub:"metric-name"`
	Namespace  *string            `ub:"namespace"`
	Dimensions *map[string]string `ub:"dimensions"`
	Stat       *string            `ub:"stat"`
	Period     *int64             `ub:"period"`
	Unit       *string            `ub:"unit"`
}

// expandMetricQuery builds the SDK MetricDataQuery array from the metric-query
// elements, or nil when there are none so a simple-metric alarm sends no
// Metrics. ReturnData is sent as given; CloudWatch requires exactly one element
// with true, which is the caller's responsibility.
func expandMetricQuery(in []MetricAlarmMetricQuery) []cloudwatchtypes.MetricDataQuery {
	if len(in) == 0 {
		return nil
	}
	out := make([]cloudwatchtypes.MetricDataQuery, 0, len(in))
	for _, q := range in {
		query := cloudwatchtypes.MetricDataQuery{
			Id:         aws.String(q.Id),
			AccountId:  q.AccountId,
			Expression: q.Expression,
			Label:      q.Label,
			Period:     ptr.Int32(q.Period),
			ReturnData: q.ReturnData,
			MetricStat: expandMetricStat(q.Metric),
		}
		out = append(out, query)
	}
	return out
}

// expandMetricStat builds the SDK MetricStat from the metric block, or nil when
// the block is absent so the element holds only its expression.
func expandMetricStat(in *MetricAlarmMetricStat) *cloudwatchtypes.MetricStat {
	if in == nil {
		return nil
	}
	stat := &cloudwatchtypes.MetricStat{
		Metric: &cloudwatchtypes.Metric{
			MetricName: in.MetricName,
			Namespace:  in.Namespace,
			Dimensions: expandDimensions(metricStatDimensions(in)),
		},
		Period: ptr.Int32(in.Period),
		Stat:   in.Stat,
	}
	if in.Unit != nil {
		stat.Unit = cloudwatchtypes.StandardUnit(*in.Unit)
	}
	return stat
}

// metricStatDimensions returns the dimensions of a metric block, or nil when
// the block omits them. The block holds dimensions behind a pointer so the
// field can be left out, since a nested field cannot take a defaults marker.
func metricStatDimensions(in *MetricAlarmMetricStat) map[string]string {
	if in.Dimensions == nil {
		return nil
	}
	return *in.Dimensions
}

// expandDimensions converts a dimension map into the SDK Dimension list,
// ordered by name so the request is deterministic, or nil when the map is
// empty.
func expandDimensions(in map[string]string) []cloudwatchtypes.Dimension {
	if len(in) == 0 {
		return nil
	}
	names := make([]string, 0, len(in))
	for name := range in {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]cloudwatchtypes.Dimension, 0, len(in))
	for _, name := range names {
		out = append(out, cloudwatchtypes.Dimension{
			Name:  aws.String(name),
			Value: aws.String(in[name]),
		})
	}
	return out
}
