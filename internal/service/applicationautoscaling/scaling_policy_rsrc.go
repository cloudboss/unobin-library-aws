package applicationautoscaling

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type ScalingPolicyResource struct {
	Name              string                   `ub:"name" json:"PolicyName"`
	ResourceID        string                   `ub:"resource-id" json:"ResourceId"`
	ServiceNamespace  string                   `ub:"service-namespace"`
	ScalableDimension string                   `ub:"scalable-dimension"`
	PolicyType        *string                  `ub:"policy-type"`
	StepScaling       *PolicyStepScaling       `ub:"step-scaling-policy-configuration"`
	TargetTracking    *PolicyTargetTracking    `ub:"target-tracking-scaling-policy-configuration"`
	PredictiveScaling *PolicyPredictiveScaling `ub:"predictive-scaling-policy-configuration"`
}

type PolicyStepScaling struct {
	AdjustmentType         *string                 `ub:"adjustment-type"`
	Cooldown               *int64                  `ub:"cooldown"`
	MetricAggregationType  *string                 `ub:"metric-aggregation-type"`
	MinAdjustmentMagnitude *int64                  `ub:"min-adjustment-magnitude"`
	StepAdjustment         *[]PolicyStepAdjustment `ub:"step-adjustment" json:"StepAdjustments"`
}

type PolicyStepAdjustment struct {
	ScalingAdjustment        int64    `ub:"scaling-adjustment"`
	MetricIntervalLowerBound *float64 `ub:"metric-interval-lower-bound"`
	MetricIntervalUpperBound *float64 `ub:"metric-interval-upper-bound"`
}

type PolicyTargetTracking struct {
	TargetValue                   float64                 `ub:"target-value"`
	DisableScaleIn                *bool                   `ub:"disable-scale-in"`
	ScaleInCooldown               *int64                  `ub:"scale-in-cooldown"`
	ScaleOutCooldown              *int64                  `ub:"scale-out-cooldown"`
	PredefinedMetricSpecification *PolicyPreset           `ub:"predefined-metric-specification"`
	CustomizedMetricSpecification *PolicyCustomizedMetric `ub:"customized-metric-specification"`
}

type PolicyPreset struct {
	PredefinedMetricType string  `ub:"predefined-metric-type"`
	ResourceLabel        *string `ub:"resource-label"`
}

type PolicyDimension struct {
	Name  string `ub:"name"`
	Value string `ub:"value"`
}

type PolicyCustomizedMetric struct {
	Dimensions *[]PolicyDimension   `ub:"dimensions"`
	MetricName *string              `ub:"metric-name"`
	Namespace  *string              `ub:"namespace"`
	Statistic  *string              `ub:"statistic"`
	Unit       *string              `ub:"unit"`
	Metrics    *[]PolicyTargetQuery `ub:"metrics"`
}

type PolicyTargetQuery struct {
	ID         string                  `ub:"id" json:"Id"`
	Expression *string                 `ub:"expression"`
	Label      *string                 `ub:"label"`
	ReturnData *bool                   `ub:"return-data"`
	MetricStat *PolicyTargetMetricStat `ub:"metric-stat"`
}

type PolicyTargetMetricStat struct {
	Stat   string             `ub:"stat"`
	Unit   *string            `ub:"unit"`
	Metric PolicyTargetMetric `ub:"metric"`
}

type PolicyTargetMetric struct {
	MetricName string             `ub:"metric-name"`
	Namespace  string             `ub:"namespace"`
	Dimensions *[]PolicyDimension `ub:"dimensions"`
}

type PolicyPredictiveScaling struct {
	MaxCapacityBreachBehavior *string                  `ub:"max-capacity-breach-behavior"`
	MaxCapacityBuffer         *int64                   `ub:"max-capacity-buffer"`
	Mode                      *string                  `ub:"mode"`
	SchedulingBufferTime      *int64                   `ub:"scheduling-buffer-time"`
	MetricSpecifications      []PolicyPredictiveMetric `ub:"metric-specification"`
}

type PolicyPredictiveMetric struct {
	TargetValue                           *float64      `ub:"target-value"`
	CustomizedCapacityMetricSpecification *PolicyCustom `ub:"customized-capacity-metric-specification"`
	CustomizedLoadMetricSpecification     *PolicyCustom `ub:"customized-load-metric-specification"`
	CustomizedScalingMetricSpecification  *PolicyCustom `ub:"customized-scaling-metric-specification"`
	PredefinedLoadMetricSpecification     *PolicyPreset `ub:"predefined-load-metric-specification"`
	PredefinedMetricPairSpecification     *PolicyPreset `ub:"predefined-metric-pair-specification"`
	PredefinedScalingMetricSpecification  *PolicyPreset `ub:"predefined-scaling-metric-specification"`
}

type PolicyCustom struct {
	MetricDataQuery []PolicyPredictiveQuery `ub:"metric-data-query" json:"MetricDataQueries"`
}

type PolicyPredictiveQuery struct {
	ID         string          `ub:"id" json:"Id"`
	Expression *string         `ub:"expression"`
	Label      *string         `ub:"label"`
	ReturnData *bool           `ub:"return-data"`
	MetricStat *PredictiveStat `ub:"metric-stat"`
}

type PredictiveStat struct {
	Stat   string                      `ub:"stat"`
	Unit   *string                     `ub:"unit"`
	Metric PolicyPredictiveQueryMetric `ub:"metric"`
}

type PolicyPredictiveQueryMetric struct {
	MetricName *string            `ub:"metric-name"`
	Namespace  *string            `ub:"namespace"`
	Dimension  *[]PolicyDimension `ub:"dimension" json:"Dimensions"`
}

type ScalingPolicyResourceOutput struct {
	PolicyARN         string                   `ub:"policy-arn"`
	AlarmARNs         []string                 `ub:"alarm-arns"`
	Name              string                   `ub:"name" json:"PolicyName"`
	ResourceID        string                   `ub:"resource-id" json:"ResourceId"`
	ServiceNamespace  string                   `ub:"service-namespace"`
	ScalableDimension string                   `ub:"scalable-dimension"`
	PolicyType        string                   `ub:"policy-type"`
	StepScaling       *PolicyStepScaling       `ub:"step-scaling-policy-configuration"`
	TargetTracking    *PolicyTargetTracking    `ub:"target-tracking-scaling-policy-configuration"`
	PredictiveScaling *PolicyPredictiveScaling `ub:"predictive-scaling-policy-configuration"`
}

type policyPrior = runtime.Prior[ScalingPolicyResource, *ScalingPolicyResourceOutput, *awsCfg]
type policyDefinition = runtime.ResourceDefinition[
	ScalingPolicyResource, *ScalingPolicyResourceOutput, *awsCfg,
]

func (r ScalingPolicyResource) Defaults() []defaults.Default {
	return []defaults.Default{defaults.NullableValue(r.PolicyType, "StepScaling")}
}

func (r ScalingPolicyResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.Name, nil)).Message("Name must be non-null"),
		constraint.Must(constraint.NotEquals(r.ResourceID,
			nil)).
			Message("ResourceID must be non-null"),
		constraint.Must(constraint.NotEquals(r.ServiceNamespace,
			nil)).
			Message("ServiceNamespace must be non-null"),
		constraint.Must(constraint.NotEquals(r.ScalableDimension,
			nil)).
			Message("ScalableDimension must be non-null"),
		constraint.Must(constraint.AtMost(constraint.ByteLength(r.Name),
			255)).
			Message("name must be at most 255 bytes"),
		constraint.When(constraint.NotEquals(r.PolicyType,
			nil)).Require(constraint.OneOf(r.PolicyType,
			"StepScaling",
			"TargetTrackingScaling",
			"PredictiveScaling")).
			Message("PolicyType is invalid"),
		constraint.When(constraint.NotEquals(r.StepScaling.AdjustmentType,
			nil)).Require(constraint.OneOf(r.StepScaling.AdjustmentType,
			"ChangeInCapacity",
			"PercentChangeInCapacity",
			"ExactCapacity")).
			Message("AdjustmentType is invalid"),
		constraint.When(constraint.NotEquals(r.StepScaling.MetricAggregationType,
			nil)).Require(constraint.OneOf(r.StepScaling.MetricAggregationType,
			"Average",
			"Minimum",
			"Maximum")).
			Message("MetricAggregationType is invalid"),
		constraint.ForEach(r.StepScaling.StepAdjustment,
			func(s PolicyStepAdjustment) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.NotEquals(s.ScalingAdjustment,
						nil)).
						Message("scaling-adjustment must be non-null"),
				}
			}),
		constraint.When(constraint.NotEquals(r.TargetTracking,
			nil)).Require(
			constraint.NotEquals(r.TargetTracking.TargetValue,
				nil)).
			Message("target-value must be non-null"),
		constraint.AtMostOneOf(r.TargetTracking.CustomizedMetricSpecification,
			r.TargetTracking.PredefinedMetricSpecification),
		constraint.ForbiddenWith(r.TargetTracking.CustomizedMetricSpecification.Metrics,
			r.TargetTracking.CustomizedMetricSpecification.Dimensions,
			r.TargetTracking.CustomizedMetricSpecification.MetricName,
			r.TargetTracking.CustomizedMetricSpecification.Namespace,
			r.TargetTracking.CustomizedMetricSpecification.Statistic,
			r.TargetTracking.CustomizedMetricSpecification.Unit),
		constraint.When(constraint.NotEquals(r.TargetTracking.CustomizedMetricSpecification.Statistic,
			nil)).Require(constraint.OneOf(r.TargetTracking.CustomizedMetricSpecification.Statistic,
			"Average",
			"Minimum",
			"Maximum",
			"SampleCount",
			"Sum")).
			Message("Statistic is invalid"),
		constraint.ForEach(r.TargetTracking.CustomizedMetricSpecification.Dimensions,
			func(d PolicyDimension) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.NotEquals(d.Name, nil)).
						Message("dimension name must be non-null"),
					constraint.Must(constraint.NotEquals(d.Value,
						nil)).
						Message("dimension value must be non-null"),
				}
			}),
		constraint.When(constraint.NotEquals(r.TargetTracking.PredefinedMetricSpecification,
			nil)).Require(
			constraint.NotEquals(r.TargetTracking.PredefinedMetricSpecification.PredefinedMetricType,
				nil)).
			Message("predefined-metric-type must be non-null"),
		constraint.Must(constraint.Expr(
			"input.value == null || (@core.byte-length(input.value) >= 0 && "+
				"@core.byte-length(input.value) <= 1023)",
			constraint.Bind("value", r.TargetTracking.PredefinedMetricSpecification.ResourceLabel),
		)).Message("ResourceLabel byte length is invalid"),
		constraint.ForEach(r.TargetTracking.CustomizedMetricSpecification.Metrics,
			func(q PolicyTargetQuery) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.NotEquals(q.ID, nil)).Message("query id must be non-null"),
					constraint.Must(constraint.Expr(
						"input.value == null || (@core.byte-length(input.value) >= 1 && "+
							"@core.byte-length(input.value) <= 255)",
						constraint.Bind("value", q.ID),
					)).Message("ID byte length is invalid"),
					constraint.Must(constraint.Expr(
						"input.value == null || (@core.byte-length(input.value) >= 1 && "+
							"@core.byte-length(input.value) <= 2047)",
						constraint.Bind("value", q.Expression),
					)).Message("Expression byte length is invalid"),
					constraint.Must(constraint.Expr(
						"input.value == null || (@core.byte-length(input.value) >= 1 && "+
							"@core.byte-length(input.value) <= 2047)",
						constraint.Bind("value", q.Label),
					)).Message("Label byte length is invalid"),
					constraint.When(constraint.NotEquals(q.MetricStat,
						nil)).Require(
						constraint.NotEquals(q.MetricStat.Stat,
							nil),
						constraint.NotEquals(q.MetricStat.Metric,
							nil),
						constraint.NotEquals(q.MetricStat.Metric.MetricName,
							nil),
						constraint.NotEquals(q.MetricStat.Metric.Namespace,
							nil)).
						Message("metric-stat requires stat and metric name and namespace"),
					constraint.Must(constraint.Expr(
						"input.value == null || (@core.byte-length(input.value) >= 1 && "+
							"@core.byte-length(input.value) <= 100)",
						constraint.Bind("value", q.MetricStat.Stat),
					)).Message("Stat byte length is invalid"),
					constraint.ForEach(q.MetricStat.Metric.Dimensions,
						func(d PolicyDimension) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.NotEquals(d.Name, nil)).
									Message("dimension name must be non-null"),
								constraint.Must(constraint.NotEquals(d.Value,
									nil)).
									Message("dimension value must be non-null"),
							}
						}),
				}
			}),
		constraint.When(constraint.NotEquals(r.PredictiveScaling.MaxCapacityBreachBehavior,
			nil)).Require(constraint.OneOf(r.PredictiveScaling.MaxCapacityBreachBehavior,
			"HonorMaxCapacity",
			"IncreaseMaxCapacity")).
			Message("MaxCapacityBreachBehavior is invalid"),
		constraint.When(constraint.NotEquals(r.PredictiveScaling.Mode,
			nil)).Require(constraint.OneOf(r.PredictiveScaling.Mode,
			"ForecastOnly",
			"ForecastAndScale")).
			Message("Mode is invalid"),
		constraint.When(constraint.NotEquals(r.PredictiveScaling.MaxCapacityBuffer,
			nil)).Require(constraint.AtLeast(r.PredictiveScaling.MaxCapacityBuffer,
			0),
			constraint.AtMost(r.PredictiveScaling.MaxCapacityBuffer,
				100)).
			Message("MaxCapacityBuffer is outside its permitted range"),
		constraint.When(constraint.NotEquals(r.PredictiveScaling.SchedulingBufferTime,
			nil)).Require(constraint.AtLeast(r.PredictiveScaling.SchedulingBufferTime,
			0),
			constraint.AtMost(r.PredictiveScaling.SchedulingBufferTime,
				3600)).
			Message("SchedulingBufferTime is outside its permitted range"),
		constraint.When(constraint.NotEquals(r.PredictiveScaling,
			nil)).Require(constraint.NotEmpty(r.PredictiveScaling.MetricSpecifications)).
			Message("metric-specification must be nonempty"),
		constraint.Must(constraint.Expr(
			"@core.all([ for value in (input.items ?? []) : value != null ])",
			constraint.Bind("items", r.PredictiveScaling.MetricSpecifications),
		)).Message("metric-specification entries must be non-null"),
		constraint.ForEach(r.PredictiveScaling.MetricSpecifications,
			func(m PolicyPredictiveMetric) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.NotEquals(m.PredefinedLoadMetricSpecification,
						nil)).Require(
						constraint.NotEquals(m.PredefinedLoadMetricSpecification.PredefinedMetricType,
							nil)).
						Message("predefined-metric-type must be non-null"),
					constraint.Must(constraint.Expr(
						"input.value == null || (@core.byte-length(input.value) >= 1 && "+
							"@core.byte-length(input.value) <= 128)",
						constraint.Bind("value", m.PredefinedLoadMetricSpecification.PredefinedMetricType),
					)).Message("PredefinedMetricType byte length is invalid"),
					constraint.Must(constraint.Expr(
						"input.value == null || (@core.byte-length(input.value) >= 1 && "+
							"@core.byte-length(input.value) <= 1023)",
						constraint.Bind("value", m.PredefinedLoadMetricSpecification.ResourceLabel),
					)).Message("ResourceLabel byte length is invalid"),
					constraint.When(constraint.NotEquals(m.PredefinedMetricPairSpecification,
						nil)).Require(
						constraint.NotEquals(m.PredefinedMetricPairSpecification.PredefinedMetricType,
							nil)).
						Message("predefined-metric-type must be non-null"),
					constraint.Must(constraint.Expr(
						"input.value == null || (@core.byte-length(input.value) >= 1 && "+
							"@core.byte-length(input.value) <= 128)",
						constraint.Bind("value", m.PredefinedMetricPairSpecification.PredefinedMetricType),
					)).Message("PredefinedMetricType byte length is invalid"),
					constraint.Must(constraint.Expr(
						"input.value == null || (@core.byte-length(input.value) >= 1 && "+
							"@core.byte-length(input.value) <= 1023)",
						constraint.Bind("value", m.PredefinedMetricPairSpecification.ResourceLabel),
					)).Message("ResourceLabel byte length is invalid"),
					constraint.When(constraint.NotEquals(m.PredefinedScalingMetricSpecification,
						nil)).Require(
						constraint.NotEquals(m.PredefinedScalingMetricSpecification.PredefinedMetricType,
							nil)).
						Message("predefined-metric-type must be non-null"),
					constraint.Must(constraint.Expr(
						"input.value == null || (@core.byte-length(input.value) >= 1 && "+
							"@core.byte-length(input.value) <= 128)",
						constraint.Bind("value", m.PredefinedScalingMetricSpecification.PredefinedMetricType),
					)).Message("PredefinedMetricType byte length is invalid"),
					constraint.Must(constraint.Expr(
						"input.value == null || (@core.byte-length(input.value) >= 1 && "+
							"@core.byte-length(input.value) <= 1023)",
						constraint.Bind("value", m.PredefinedScalingMetricSpecification.ResourceLabel),
					)).Message("ResourceLabel byte length is invalid"),
					constraint.When(constraint.NotEquals(m.CustomizedCapacityMetricSpecification,
						nil)).Require(constraint.NotEmpty(m.CustomizedCapacityMetricSpecification.MetricDataQuery)).
						Message("metric-data-query must be nonempty"),
					constraint.ForEach(m.CustomizedCapacityMetricSpecification.MetricDataQuery,
						func(q PolicyPredictiveQuery) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.NotEquals(q.ID, nil)).Message("query id must be non-null"),
								constraint.Must(constraint.Expr(
									"input.value == null || (@core.byte-length(input.value) >= 1 && "+
										"@core.byte-length(input.value) <= 2048)",
									constraint.Bind("value", q.Expression),
								)).Message("Expression byte length is invalid"),
								constraint.When(constraint.NotEquals(q.MetricStat,
									nil)).Require(
									constraint.NotEquals(q.MetricStat.Stat,
										nil),
									constraint.NotEquals(q.MetricStat.Metric,
										nil)).
									Message("predictive metric-stat requires stat and metric"),
								constraint.ForEach(q.MetricStat.Metric.Dimension,
									func(d PolicyDimension) []constraint.Constraint {
										return []constraint.Constraint{
											constraint.Must(constraint.NotEquals(d.Name, nil)).
												Message("dimension name must be non-null"),
											constraint.Must(constraint.NotEquals(d.Value,
												nil)).
												Message("dimension value must be non-null"),
										}
									}),
							}
						}),
					constraint.When(constraint.NotEquals(m.CustomizedLoadMetricSpecification,
						nil)).Require(constraint.NotEmpty(m.CustomizedLoadMetricSpecification.MetricDataQuery)).
						Message("metric-data-query must be nonempty"),
					constraint.ForEach(m.CustomizedLoadMetricSpecification.MetricDataQuery,
						func(q PolicyPredictiveQuery) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.NotEquals(q.ID, nil)).Message("query id must be non-null"),
								constraint.Must(constraint.Expr(
									"input.value == null || (@core.byte-length(input.value) >= 1 && "+
										"@core.byte-length(input.value) <= 2048)",
									constraint.Bind("value", q.Expression),
								)).Message("Expression byte length is invalid"),
								constraint.When(constraint.NotEquals(q.MetricStat,
									nil)).Require(
									constraint.NotEquals(q.MetricStat.Stat,
										nil),
									constraint.NotEquals(q.MetricStat.Metric,
										nil)).
									Message("predictive metric-stat requires stat and metric"),
								constraint.ForEach(q.MetricStat.Metric.Dimension,
									func(d PolicyDimension) []constraint.Constraint {
										return []constraint.Constraint{
											constraint.Must(constraint.NotEquals(d.Name, nil)).
												Message("dimension name must be non-null"),
											constraint.Must(constraint.NotEquals(d.Value,
												nil)).
												Message("dimension value must be non-null"),
										}
									}),
							}
						}),
					constraint.When(constraint.NotEquals(m.CustomizedScalingMetricSpecification,
						nil)).Require(constraint.NotEmpty(m.CustomizedScalingMetricSpecification.MetricDataQuery)).
						Message("metric-data-query must be nonempty"),
					constraint.ForEach(m.CustomizedScalingMetricSpecification.MetricDataQuery,
						func(q PolicyPredictiveQuery) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.NotEquals(q.ID, nil)).Message("query id must be non-null"),
								constraint.Must(constraint.Expr(
									"input.value == null || (@core.byte-length(input.value) >= 1 && "+
										"@core.byte-length(input.value) <= 2048)",
									constraint.Bind("value", q.Expression),
								)).Message("Expression byte length is invalid"),
								constraint.When(constraint.NotEquals(q.MetricStat,
									nil)).Require(
									constraint.NotEquals(q.MetricStat.Stat,
										nil),
									constraint.NotEquals(q.MetricStat.Metric,
										nil)).
									Message("predictive metric-stat requires stat and metric"),
								constraint.ForEach(q.MetricStat.Metric.Dimension,
									func(d PolicyDimension) []constraint.Constraint {
										return []constraint.Constraint{
											constraint.Must(constraint.NotEquals(d.Name, nil)).
												Message("dimension name must be non-null"),
											constraint.Must(constraint.NotEquals(d.Value,
												nil)).
												Message("dimension value must be non-null"),
										}
									}),
							}
						}),
				}
			}),
	}
}

func (r *ScalingPolicyResource) ResourceDefinition() policyDefinition {
	type input = ScalingPolicyResource
	return policyDefinition{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[input]{
			runtime.EqualBy(runtime.InputField(func(v *input) **PolicyStepScaling {
				return &v.StepScaling
			}), equalPolicyStep),
			runtime.EqualBy(runtime.InputField(func(v *input) **PolicyTargetTracking {
				return &v.TargetTracking
			}), equalPolicyTracking),
			runtime.EqualBy(runtime.InputField(func(v *input) **PolicyPredictiveScaling {
				return &v.PredictiveScaling
			}), equalPolicyPredictive),
		},
		StableID: func(_ input, out *ScalingPolicyResourceOutput) (string, error) {
			if out == nil {
				return "", fmt.Errorf("scaling policy prior output is missing")
			}
			return fmt.Sprintf("%q/%q/%q/%q", out.ServiceNamespace, out.ResourceID,
				out.ScalableDimension, out.Name), nil
		},
		Replace: runtime.Replacement[input, *ScalingPolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[input]{
				runtime.InputField(func(v *input) *string { return &v.Name }),
				runtime.InputField(func(v *input) *string { return &v.ResourceID }),
				runtime.InputField(func(v *input) *string { return &v.ServiceNamespace }),
				runtime.InputField(func(v *input) *string { return &v.ScalableDimension }),
			}},
	}
}

func (r *ScalingPolicyResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*ScalingPolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createPolicy(ctx, client, systemClock{})
}

func (r *ScalingPolicyResource) Read(
	ctx context.Context, cfg *awsCfg, prior policyPrior,
) (*ScalingPolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return readPolicy(ctx, client, policyIdentity(prior), systemClock{}, false)
}

func (r *ScalingPolicyResource) Update(
	ctx context.Context, cfg *awsCfg, prior policyPrior,
) (*ScalingPolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.updatePolicy(ctx, client, prior, systemClock{})
}

func (r *ScalingPolicyResource) Delete(
	ctx context.Context, cfg *awsCfg, prior policyPrior,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return deletePolicy(ctx, client, policyIdentity(prior), systemClock{})
}
