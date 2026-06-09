package autoscaling

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// PolicyTargetTrackingConfiguration is the target-tracking scaling policy
// block: a metric and a target value the group is held to. The metric is given
// either as a predefined metric or as a customized one, never both. DisableScaleIn
// keeps the policy from removing instances; the API treats an omitted value as
// false.
type PolicyTargetTrackingConfiguration struct {
	TargetValue                   float64                              `ub:"target-value"`
	DisableScaleIn                *bool                                `ub:"disable-scale-in"`
	PredefinedMetricSpecification *PolicyPredefinedMetricSpecification `ub:"predefined-metric-specification"`
	CustomizedMetricSpecification *PolicyCustomizedMetricSpecification `ub:"customized-metric-specification"`
}

// PolicyPredefinedMetricSpecification names one of the predefined metrics for a
// target-tracking policy: ASGAverageCPUUtilization, ASGAverageNetworkIn,
// ASGAverageNetworkOut, or ALBRequestCountPerTarget. The resource label is
// required only for ALBRequestCountPerTarget, identifying the target group whose
// request count is tracked.
type PolicyPredefinedMetricSpecification struct {
	PredefinedMetricType string  `ub:"predefined-metric-type"`
	ResourceLabel        *string `ub:"resource-label"`
}

// PolicyCustomizedMetricSpecification is a customized CloudWatch metric for a
// target-tracking policy. A single metric is given inline by name, namespace,
// statistic, unit, and dimensions; alternatively the metrics field gives a
// metric-math expression, in which case the inline fields are not used. The API
// rejects mixing the two forms.
type PolicyCustomizedMetricSpecification struct {
	MetricName string                                `ub:"metric-name"`
	Namespace  string                                `ub:"namespace"`
	Statistic  string                                `ub:"statistic"`
	Unit       *string                               `ub:"unit"`
	Period     *int64                                `ub:"period"`
	Dimensions []PolicyMetricDimension               `ub:"dimensions"`
	Metrics    []PolicyTargetTrackingMetricDataQuery `ub:"metrics"`
}

// PolicyMetricDimension is one name/value dimension of a customized metric. Both
// halves are required.
type PolicyMetricDimension struct {
	Name  string `ub:"name"`
	Value string `ub:"value"`
}

// PolicyTargetTrackingMetricDataQuery is one entry in a metric-math
// specification: either a raw metric (via metric-stat) or an expression over
// other entries' ids, never both. Id labels the entry; return-data marks the
// single entry whose value the policy tracks and defaults to true.
type PolicyTargetTrackingMetricDataQuery struct {
	Id         string                          `ub:"id"`
	Expression *string                         `ub:"expression"`
	Label      *string                         `ub:"label"`
	Period     *int64                          `ub:"period"`
	ReturnData *bool                           `ub:"return-data"`
	MetricStat *PolicyTargetTrackingMetricStat `ub:"metric-stat"`
}

// PolicyTargetTrackingMetricStat pairs a metric with the statistic to apply. The
// metric and statistic are required; the period and unit are optional.
type PolicyTargetTrackingMetricStat struct {
	Metric PolicyMetric `ub:"metric"`
	Stat   string       `ub:"stat"`
	Period *int64       `ub:"period"`
	Unit   *string      `ub:"unit"`
}

// PolicyMetric identifies a CloudWatch metric by name and namespace, with
// optional dimensions to narrow it.
type PolicyMetric struct {
	MetricName string                  `ub:"metric-name"`
	Namespace  string                  `ub:"namespace"`
	Dimensions []PolicyMetricDimension `ub:"dimensions"`
}

// expandTargetTracking converts the target-tracking block into the SDK
// configuration. Exactly one of the predefined or customized metric forms is
// present, matching the API's requirement; the value-conditional constraints on
// the resource keep an invalid combination from reaching here.
func expandTargetTracking(
	c PolicyTargetTrackingConfiguration,
) *autoscalingtypes.TargetTrackingConfiguration {
	out := &autoscalingtypes.TargetTrackingConfiguration{
		TargetValue:    aws.Float64(c.TargetValue),
		DisableScaleIn: c.DisableScaleIn,
	}
	if c.PredefinedMetricSpecification != nil {
		out.PredefinedMetricSpecification = expandPredefinedMetric(*c.PredefinedMetricSpecification)
	}
	if c.CustomizedMetricSpecification != nil {
		out.CustomizedMetricSpecification = expandCustomizedMetric(*c.CustomizedMetricSpecification)
	}
	return out
}

// expandPredefinedMetric converts the predefined-metric block into the SDK
// specification.
func expandPredefinedMetric(
	m PolicyPredefinedMetricSpecification,
) *autoscalingtypes.PredefinedMetricSpecification {
	return &autoscalingtypes.PredefinedMetricSpecification{
		PredefinedMetricType: autoscalingtypes.MetricType(m.PredefinedMetricType),
		ResourceLabel:        m.ResourceLabel,
	}
}

// expandCustomizedMetric converts the customized-metric block into the SDK
// specification. The inline single-metric fields are sent as given; the
// metric-math metrics, when present, replace them, and the API rejects sending
// both, so an empty inline field stays absent rather than arriving as a zero.
func expandCustomizedMetric(
	m PolicyCustomizedMetricSpecification,
) *autoscalingtypes.CustomizedMetricSpecification {
	out := &autoscalingtypes.CustomizedMetricSpecification{
		Period: ptr.Int32(m.Period),
	}
	if m.MetricName != "" {
		out.MetricName = aws.String(m.MetricName)
	}
	if m.Namespace != "" {
		out.Namespace = aws.String(m.Namespace)
	}
	if m.Statistic != "" {
		out.Statistic = autoscalingtypes.MetricStatistic(m.Statistic)
	}
	if m.Unit != nil {
		out.Unit = m.Unit
	}
	out.Dimensions = expandMetricDimensions(m.Dimensions)
	out.Metrics = expandMetricDataQueries(m.Metrics)
	return out
}

// expandMetricDimensions converts the dimension blocks into the SDK dimensions.
func expandMetricDimensions(dims []PolicyMetricDimension) []autoscalingtypes.MetricDimension {
	if len(dims) == 0 {
		return nil
	}
	out := make([]autoscalingtypes.MetricDimension, 0, len(dims))
	for _, d := range dims {
		out = append(out, autoscalingtypes.MetricDimension{
			Name:  aws.String(d.Name),
			Value: aws.String(d.Value),
		})
	}
	return out
}

// expandMetricDataQueries converts the metric-math entries into the SDK
// queries. Each entry is a raw metric (via metric-stat) or an expression, and
// return-data is passed through so the API's default of true applies only to an
// omitted value.
func expandMetricDataQueries(
	queries []PolicyTargetTrackingMetricDataQuery,
) []autoscalingtypes.TargetTrackingMetricDataQuery {
	if len(queries) == 0 {
		return nil
	}
	out := make([]autoscalingtypes.TargetTrackingMetricDataQuery, 0, len(queries))
	for _, q := range queries {
		entry := autoscalingtypes.TargetTrackingMetricDataQuery{
			Id:         aws.String(q.Id),
			Expression: q.Expression,
			Label:      q.Label,
			Period:     ptr.Int32(q.Period),
			ReturnData: q.ReturnData,
		}
		if q.MetricStat != nil {
			entry.MetricStat = expandMetricStat(*q.MetricStat)
		}
		out = append(out, entry)
	}
	return out
}

// expandMetricStat converts the metric-stat block into the SDK stat.
func expandMetricStat(
	s PolicyTargetTrackingMetricStat,
) *autoscalingtypes.TargetTrackingMetricStat {
	return &autoscalingtypes.TargetTrackingMetricStat{
		Metric: expandMetric(s.Metric),
		Stat:   aws.String(s.Stat),
		Period: ptr.Int32(s.Period),
		Unit:   s.Unit,
	}
}

// expandMetric converts the metric block into the SDK metric.
func expandMetric(m PolicyMetric) *autoscalingtypes.Metric {
	return &autoscalingtypes.Metric{
		MetricName: aws.String(m.MetricName),
		Namespace:  aws.String(m.Namespace),
		Dimensions: expandMetricDimensions(m.Dimensions),
	}
}
