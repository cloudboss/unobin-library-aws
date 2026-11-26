package applicationautoscaling

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
)

func (r ScalingPolicyResource) preparePolicy() (*sdk.PutScalingPolicyInput, error) {
	in := &sdk.PutScalingPolicyInput{
		PolicyName: aws.String(r.Name), ResourceId: aws.String(r.ResourceID),
		ServiceNamespace:  types.ServiceNamespace(r.ServiceNamespace),
		ScalableDimension: types.ScalableDimension(r.ScalableDimension),
		PolicyType:        types.PolicyTypeStepScaling,
	}
	if r.PolicyType != nil {
		in.PolicyType = types.PolicyType(*r.PolicyType)
	}
	if r.StepScaling != nil {
		step := *r.StepScaling
		step.Cooldown = aws.Int64(int64(int32(aws.ToInt64(step.Cooldown))))
		if aws.ToInt64(step.MinAdjustmentMagnitude) <= 0 {
			step.MinAdjustmentMagnitude = nil
		} else {
			step.MinAdjustmentMagnitude = aws.Int64(int64(int32(*step.MinAdjustmentMagnitude)))
		}
		if step.StepAdjustment != nil {
			adjustments := slices.Clone(*step.StepAdjustment)
			for i := range adjustments {
				adjustments[i].ScalingAdjustment = int64(int32(adjustments[i].ScalingAdjustment))
			}
			step.StepAdjustment = &adjustments
		}
		in.StepScalingPolicyConfiguration = &types.StepScalingPolicyConfiguration{}
		if err := convertPolicy(step, in.StepScalingPolicyConfiguration); err != nil {
			return nil, err
		}
		stepRequest := in.StepScalingPolicyConfiguration
		stepRequest.StepAdjustments = canonicalPolicySet(stepRequest.StepAdjustments)
	}
	if r.TargetTracking != nil {
		in.TargetTrackingScalingPolicyConfiguration = &types.TargetTrackingScalingPolicyConfiguration{}
		tracking := *r.TargetTracking
		tracking.ScaleInCooldown = aws.Int64(int64(int32(aws.ToInt64(tracking.ScaleInCooldown))))
		tracking.ScaleOutCooldown = aws.Int64(int64(int32(aws.ToInt64(tracking.ScaleOutCooldown))))
		tracking.DisableScaleIn = aws.Bool(aws.ToBool(tracking.DisableScaleIn))
		if err := convertPolicy(tracking, in.TargetTrackingScalingPolicyConfiguration); err != nil {
			return nil, err
		}
		target := in.TargetTrackingScalingPolicyConfiguration
		if target.PredefinedMetricSpecification != nil {
			target.PredefinedMetricSpecification.ResourceLabel = policyOptional(
				target.PredefinedMetricSpecification.ResourceLabel)
		}
		if custom := target.CustomizedMetricSpecification; custom != nil {
			if len(custom.Metrics) > 0 {
				custom.MetricName, custom.Namespace, custom.Dimensions, custom.Unit = nil, nil, nil, nil
				custom.Statistic = ""
			} else {
				custom.MetricName = aws.String(aws.ToString(custom.MetricName))
				custom.Namespace = aws.String(aws.ToString(custom.Namespace))
			}
			custom.Unit = policyOptional(custom.Unit)
			custom.Dimensions = canonicalPolicySet(custom.Dimensions)
			for i := range custom.Metrics {
				query := &custom.Metrics[i]
				query.Expression, query.Label = policyOptional(query.Expression), policyOptional(query.Label)
				if query.ReturnData == nil {
					query.ReturnData = aws.Bool(true)
				}
				if stat := query.MetricStat; stat != nil {
					stat.Unit = policyOptional(stat.Unit)
					stat.Metric.Dimensions = canonicalPolicySet(stat.Metric.Dimensions)
				}
			}
			custom.Metrics = canonicalPolicySet(custom.Metrics)
		}
	}
	if r.PredictiveScaling != nil {
		in.PredictiveScalingPolicyConfiguration = &types.PredictiveScalingPolicyConfiguration{}
		predictive := *r.PredictiveScaling
		if aws.ToInt64(predictive.MaxCapacityBuffer) == 0 {
			predictive.MaxCapacityBuffer = nil
		}
		if aws.ToInt64(predictive.SchedulingBufferTime) == 0 {
			predictive.SchedulingBufferTime = nil
		}
		if err := convertPolicy(predictive, in.PredictiveScalingPolicyConfiguration); err != nil {
			return nil, err
		}
		for i := range in.PredictiveScalingPolicyConfiguration.MetricSpecifications {
			metric := &in.PredictiveScalingPolicyConfiguration.MetricSpecifications[i]
			if preset := metric.PredefinedLoadMetricSpecification; preset != nil {
				preset.ResourceLabel = policyOptional(preset.ResourceLabel)
			}
			if preset := metric.PredefinedMetricPairSpecification; preset != nil {
				preset.ResourceLabel = policyOptional(preset.ResourceLabel)
			}
			if preset := metric.PredefinedScalingMetricSpecification; preset != nil {
				preset.ResourceLabel = policyOptional(preset.ResourceLabel)
			}
			for _, custom := range []*types.PredictiveScalingCustomizedMetricSpecification{
				metric.CustomizedCapacityMetricSpecification, metric.CustomizedLoadMetricSpecification,
				metric.CustomizedScalingMetricSpecification,
			} {
				if custom == nil {
					continue
				}
				for j := range custom.MetricDataQueries {
					query := &custom.MetricDataQueries[j]
					query.Id = policyOptional(query.Id)
					query.Expression, query.Label = policyOptional(query.Expression), policyOptional(query.Label)
					if query.ReturnData == nil {
						query.ReturnData = aws.Bool(false)
					}
					if stat := query.MetricStat; stat != nil {
						stat.Stat = policyOptional(stat.Stat)
						stat.Unit = policyOptional(stat.Unit)
						stat.Metric.MetricName = policyOptional(stat.Metric.MetricName)
						stat.Metric.Namespace = policyOptional(stat.Metric.Namespace)
						for k := range stat.Metric.Dimensions {
							dimension := &stat.Metric.Dimensions[k]
							dimension.Name = policyOptional(dimension.Name)
							dimension.Value = policyOptional(dimension.Value)
						}
						stat.Metric.Dimensions = canonicalPolicySet(stat.Metric.Dimensions)
					}
				}
			}
		}
	}
	return in, nil
}

func convertPolicy(from, to any) error {
	data, err := json.Marshal(from)
	if err != nil {
		return fmt.Errorf("encode scaling policy: %w", err)
	}
	if err := json.Unmarshal(data, to); err != nil {
		return fmt.Errorf("decode scaling policy: %w", err)
	}
	return nil
}

func policyOptional(value *string) *string {
	if aws.ToString(value) == "" {
		return nil
	}
	return value
}

func canonicalPolicySet[T any](items []T) []T {
	if len(items) == 0 {
		return nil
	}
	slices.SortFunc(items, func(a, b T) int {
		left, _ := json.Marshal(a)
		right, _ := json.Marshal(b)
		return slices.Compare(left, right)
	})
	return slices.CompactFunc(items, func(a, b T) bool { return reflect.DeepEqual(a, b) })
}

func policyOutput(policy types.ScalingPolicy) (*ScalingPolicyResourceOutput, error) {
	out := &ScalingPolicyResourceOutput{
		PolicyARN: aws.ToString(policy.PolicyARN), Name: aws.ToString(policy.PolicyName),
		ResourceID: aws.ToString(policy.ResourceId), ServiceNamespace: string(policy.ServiceNamespace),
		ScalableDimension: string(policy.ScalableDimension), PolicyType: string(policy.PolicyType),
		AlarmARNs: make([]string, 0, len(policy.Alarms)),
	}
	for _, alarm := range policy.Alarms {
		out.AlarmARNs = append(out.AlarmARNs, aws.ToString(alarm.AlarmARN))
	}
	if policy.StepScalingPolicyConfiguration != nil {
		out.StepScaling = &PolicyStepScaling{}
		if err := convertPolicy(policy.StepScalingPolicyConfiguration, out.StepScaling); err != nil {
			return nil, err
		}
		if out.StepScaling.StepAdjustment != nil {
			*out.StepScaling.StepAdjustment = canonicalPolicySet(*out.StepScaling.StepAdjustment)
		}
	}
	if policy.TargetTrackingScalingPolicyConfiguration != nil {
		out.TargetTracking = &PolicyTargetTracking{}
		if err := convertPolicy(policy.TargetTrackingScalingPolicyConfiguration,
			out.TargetTracking); err != nil {
			return nil, err
		}
		custom := policy.TargetTrackingScalingPolicyConfiguration.CustomizedMetricSpecification
		if custom != nil && custom.Metrics != nil {
			queries := out.TargetTracking.CustomizedMetricSpecification.Metrics
			out.TargetTracking.CustomizedMetricSpecification = &PolicyCustomizedMetric{Metrics: queries}
		}
	}
	if policy.PredictiveScalingPolicyConfiguration != nil {
		out.PredictiveScaling = &PolicyPredictiveScaling{}
		if err := convertPolicy(policy.PredictiveScalingPolicyConfiguration,
			out.PredictiveScaling); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func equalPolicyStep(a, b *PolicyStepScaling) bool {
	left, le := (ScalingPolicyResource{StepScaling: a}).preparePolicy()
	right, re := (ScalingPolicyResource{StepScaling: b}).preparePolicy()
	return le == nil && re == nil && reflect.DeepEqual(left, right)
}

func equalPolicyTracking(a, b *PolicyTargetTracking) bool {
	left, le := (ScalingPolicyResource{TargetTracking: a}).preparePolicy()
	right, re := (ScalingPolicyResource{TargetTracking: b}).preparePolicy()
	return le == nil && re == nil && reflect.DeepEqual(left, right)
}

func equalPolicyPredictive(a, b *PolicyPredictiveScaling) bool {
	left, le := (ScalingPolicyResource{PredictiveScaling: a}).preparePolicy()
	right, re := (ScalingPolicyResource{PredictiveScaling: b}).preparePolicy()
	return le == nil && re == nil && reflect.DeepEqual(left, right)
}
