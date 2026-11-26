package applicationautoscaling

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPolicy() ScalingPolicyResource {
	return ScalingPolicyResource{Name: "policy", ResourceID: "table/a/b", ServiceNamespace: "dynamodb",
		ScalableDimension: "dynamodb:table:ReadCapacityUnits"}
}

func TestPreparePolicyDefaults(t *testing.T) {
	r := testPolicy()
	got, err := r.preparePolicy()
	require.NoError(t, err)
	assert.Equal(t, &sdk.PutScalingPolicyInput{PolicyName: aws.String(r.Name),
		ResourceId: aws.String(r.ResourceID), ServiceNamespace: types.ServiceNamespaceDynamodb,
		ScalableDimension: types.ScalableDimensionDynamoDBTableReadCapacityUnits,
		PolicyType:        types.PolicyTypeStepScaling}, got)
}

func TestPreparePolicyStep(t *testing.T) {
	for _, magnitude := range []int64{-2, 0, 3} {
		r := testPolicy()
		r.StepScaling = &PolicyStepScaling{MinAdjustmentMagnitude: aws.Int64(magnitude),
			StepAdjustment: &[]PolicyStepAdjustment{
				{ScalingAdjustment: -1, MetricIntervalUpperBound: aws.Float64(-0.25)},
				{ScalingAdjustment: 0, MetricIntervalLowerBound: aws.Float64(0),
					MetricIntervalUpperBound: aws.Float64(1.5)},
				{ScalingAdjustment: 2, MetricIntervalLowerBound: aws.Float64(1.5)},
			}}
		got, err := r.preparePolicy()
		require.NoError(t, err)
		var minimum *int32
		if magnitude > 0 {
			minimum = aws.Int32(int32(magnitude))
		}
		expected := &types.StepScalingPolicyConfiguration{Cooldown: aws.Int32(0),
			MinAdjustmentMagnitude: minimum, StepAdjustments: []types.StepAdjustment{
				{ScalingAdjustment: aws.Int32(-1), MetricIntervalUpperBound: aws.Float64(-0.25)},
				{ScalingAdjustment: aws.Int32(0), MetricIntervalLowerBound: aws.Float64(0),
					MetricIntervalUpperBound: aws.Float64(1.5)},
				{ScalingAdjustment: aws.Int32(2), MetricIntervalLowerBound: aws.Float64(1.5)},
			}}
		canonicalPolicySet(expected.StepAdjustments)
		assert.Equal(t, expected, got.StepScalingPolicyConfiguration)
		reversed := *r.StepScaling
		reversed.StepAdjustment = &[]PolicyStepAdjustment{(*r.StepScaling.StepAdjustment)[2],
			(*r.StepScaling.StepAdjustment)[0], (*r.StepScaling.StepAdjustment)[1]}
		assert.True(t, equalPolicyStep(r.StepScaling, &reversed))
		output, err := policyOutput(types.ScalingPolicy{StepScalingPolicyConfiguration: expected})
		require.NoError(t, err)
		expectedOutput := &PolicyStepScaling{Cooldown: aws.Int64(0),
			AdjustmentType: aws.String(""), MetricAggregationType: aws.String(""),
			StepAdjustment: r.StepScaling.StepAdjustment}
		if magnitude > 0 {
			expectedOutput.MinAdjustmentMagnitude = aws.Int64(magnitude)
		}
		canonicalPolicySet(*expectedOutput.StepAdjustment)
		assert.Equal(t, expectedOutput, output.StepScaling)
	}
}

func TestPreparePolicyTargetMetrics(t *testing.T) {
	r := testPolicy()
	r.TargetTracking = &PolicyTargetTracking{TargetValue: 0,
		CustomizedMetricSpecification: &PolicyCustomizedMetric{
			MetricName: aws.String("inline"), Namespace: aws.String("NS"), Statistic: aws.String("Sum"),
			Unit: aws.String(""), Dimensions: &[]PolicyDimension{{Name: "z", Value: "v"},
				{Name: "a", Value: ""}},
			Metrics: &[]PolicyTargetQuery{
				{ID: "expression", Expression: aws.String("SUM(m)"), Label: aws.String("label")},
				{ID: "m", ReturnData: aws.Bool(false), MetricStat: &PolicyTargetMetricStat{
					Stat: "Average", Unit: aws.String("Count"), Metric: PolicyTargetMetric{
						MetricName: "requests", Namespace: "NS", Dimensions: &[]PolicyDimension{
							{Name: "name", Value: "value"}},
					}}},
			}},
		PredefinedMetricSpecification: &PolicyPreset{PredefinedMetricType: "any-type",
			ResourceLabel: aws.String("")},
	}
	got, err := r.preparePolicy()
	require.NoError(t, err)
	expected := &types.TargetTrackingScalingPolicyConfiguration{TargetValue: aws.Float64(0),
		DisableScaleIn: aws.Bool(false), ScaleInCooldown: aws.Int32(0), ScaleOutCooldown: aws.Int32(0),
		PredefinedMetricSpecification: &types.PredefinedMetricSpecification{
			PredefinedMetricType: types.MetricType("any-type")},
		CustomizedMetricSpecification: &types.CustomizedMetricSpecification{
			Metrics: []types.TargetTrackingMetricDataQuery{
				{Id: aws.String("expression"), Expression: aws.String("SUM(m)"), Label: aws.String("label"),
					ReturnData: aws.Bool(true)},
				{Id: aws.String("m"), ReturnData: aws.Bool(false), MetricStat: &types.TargetTrackingMetricStat{
					Stat: aws.String("Average"), Unit: aws.String("Count"), Metric: &types.TargetTrackingMetric{
						MetricName: aws.String("requests"), Namespace: aws.String("NS"),
						Dimensions: []types.TargetTrackingMetricDimension{
							{Name: aws.String("name"), Value: aws.String("value")}},
					}}},
			}},
	}
	canonicalPolicySet(expected.CustomizedMetricSpecification.Metrics)
	assert.Equal(t, expected, got.TargetTrackingScalingPolicyConfiguration)
	// Request conversion does not impose policy-type gating or metric conflicts.
	assert.Equal(t, types.PolicyTypeStepScaling, got.PolicyType)
	out, err := policyOutput(types.ScalingPolicy{TargetTrackingScalingPolicyConfiguration: expected})
	require.NoError(t, err)
	want := *r.TargetTracking
	want.DisableScaleIn, want.ScaleInCooldown = aws.Bool(false), aws.Int64(0)
	want.ScaleOutCooldown = aws.Int64(0)
	predefined := *want.PredefinedMetricSpecification
	predefined.ResourceLabel = nil
	want.PredefinedMetricSpecification = &predefined
	custom := *want.CustomizedMetricSpecification
	custom.Unit = nil
	custom.MetricName, custom.Namespace, custom.Statistic, custom.Dimensions = nil, nil, nil, nil
	queries := append([]PolicyTargetQuery(nil), *custom.Metrics...)
	queries[0].ReturnData = aws.Bool(true)
	canonicalPolicySet(queries)
	custom.Metrics = &queries
	want.CustomizedMetricSpecification = &custom
	assert.Equal(t, &want, out.TargetTracking)
}

func TestPreparePolicyPredictiveMetrics(t *testing.T) {
	custom := &PolicyCustom{MetricDataQuery: []PolicyPredictiveQuery{
		{ID: "z", Expression: aws.String("SUM(m)"), Label: aws.String("label")},
		{ID: "m", ReturnData: aws.Bool(true), MetricStat: &PredictiveStat{
			Stat: "Sum", Unit: aws.String("Count"), Metric: PolicyPredictiveQueryMetric{
				MetricName: aws.String("requests"), Namespace: aws.String("NS"),
				Dimension: &[]PolicyDimension{{Name: "z", Value: "v"}, {Name: "a", Value: ""}},
			}}},
	}}
	predefined := &PolicyPreset{PredefinedMetricType: "ECSServiceCPUUtilization",
		ResourceLabel: aws.String("resource")}
	metric := PolicyPredictiveMetric{
		CustomizedCapacityMetricSpecification: custom, CustomizedLoadMetricSpecification: custom,
		CustomizedScalingMetricSpecification: custom, PredefinedLoadMetricSpecification: predefined,
		PredefinedMetricPairSpecification: predefined, PredefinedScalingMetricSpecification: predefined,
	}
	r := testPolicy()
	r.PolicyType = aws.String("PredictiveScaling")
	r.PredictiveScaling = &PolicyPredictiveScaling{MaxCapacityBuffer: aws.Int64(0),
		SchedulingBufferTime: aws.Int64(0),
		MetricSpecifications: []PolicyPredictiveMetric{metric,
			metric}}
	r.PredictiveScaling.MetricSpecifications[1].TargetValue = aws.Float64(0)
	got, err := r.preparePolicy()
	require.NoError(t, err)
	expectedCustom := &types.PredictiveScalingCustomizedMetricSpecification{
		MetricDataQueries: []types.PredictiveScalingMetricDataQuery{
			{Id: aws.String("z"), Expression: aws.String("SUM(m)"), Label: aws.String("label"),
				ReturnData: aws.Bool(false)},
			{Id: aws.String("m"), ReturnData: aws.Bool(true), MetricStat: &types.PredictiveScalingMetricStat{
				Stat: aws.String("Sum"), Unit: aws.String("Count"), Metric: &types.PredictiveScalingMetric{
					MetricName: aws.String("requests"), Namespace: aws.String("NS"),
					Dimensions: []types.PredictiveScalingMetricDimension{
						{Name: aws.String("a")},
						{Name: aws.String("z"), Value: aws.String("v")}},
				}}},
		}}
	scalingPreset := &types.PredictiveScalingPredefinedScalingMetricSpecification{
		PredefinedMetricType: aws.String(predefined.PredefinedMetricType),
		ResourceLabel:        predefined.ResourceLabel,
	}
	expectedMetric := types.PredictiveScalingMetricSpecification{
		CustomizedCapacityMetricSpecification: expectedCustom,
		CustomizedLoadMetricSpecification:     expectedCustom,
		CustomizedScalingMetricSpecification:  expectedCustom,
		PredefinedLoadMetricSpecification: &types.PredictiveScalingPredefinedLoadMetricSpecification{
			PredefinedMetricType: aws.String(predefined.PredefinedMetricType),
			ResourceLabel:        predefined.ResourceLabel},
		PredefinedMetricPairSpecification: &types.PredictiveScalingPredefinedMetricPairSpecification{
			PredefinedMetricType: aws.String(predefined.PredefinedMetricType),
			ResourceLabel:        predefined.ResourceLabel},
		PredefinedScalingMetricSpecification: scalingPreset,
	}
	other := expectedMetric
	other.TargetValue = aws.Float64(0)
	expected := &types.PredictiveScalingPolicyConfiguration{
		MetricSpecifications: []types.PredictiveScalingMetricSpecification{expectedMetric, other}}
	assert.Equal(t, expected, got.PredictiveScalingPolicyConfiguration)
	expected.Mode = types.PredictiveScalingModeForecastOnly
	expected.SchedulingBufferTime = aws.Int32(300)
	out, err := policyOutput(types.ScalingPolicy{PredictiveScalingPolicyConfiguration: expected})
	require.NoError(t, err)
	want := *r.PredictiveScaling
	want.MaxCapacityBuffer = nil
	want.MaxCapacityBreachBehavior = aws.String("")
	want.SchedulingBufferTime, want.Mode = aws.Int64(300), aws.String("ForecastOnly")
	normalized := *custom
	normalized.MetricDataQuery = append([]PolicyPredictiveQuery(nil), custom.MetricDataQuery...)
	normalized.MetricDataQuery[0].ReturnData = aws.Bool(false)
	stat := *normalized.MetricDataQuery[1].MetricStat
	stat.Metric.Dimension = &[]PolicyDimension{{Name: "a", Value: ""}, {Name: "z", Value: "v"}}
	normalized.MetricDataQuery[1].MetricStat = &stat
	for i := range want.MetricSpecifications {
		want.MetricSpecifications[i].CustomizedCapacityMetricSpecification = &normalized
		want.MetricSpecifications[i].CustomizedLoadMetricSpecification = &normalized
		want.MetricSpecifications[i].CustomizedScalingMetricSpecification = &normalized
	}
	assert.Equal(t, &want, out.PredictiveScaling)
	reversed := *out.PredictiveScaling
	reversed.MetricSpecifications = []PolicyPredictiveMetric{
		reversed.MetricSpecifications[1], reversed.MetricSpecifications[0]}
	assert.False(t, equalPolicyPredictive(out.PredictiveScaling, &reversed))
}

func TestPolicyOutputIdentityAndAbsentTrees(t *testing.T) {
	r := testPolicy()
	out, err := policyOutput(types.ScalingPolicy{PolicyARN: aws.String("arn:policy"),
		PolicyName: aws.String(r.Name), ResourceId: aws.String(r.ResourceID),
		ServiceNamespace:  types.ServiceNamespace(r.ServiceNamespace),
		ScalableDimension: types.ScalableDimension(r.ScalableDimension),
		PolicyType:        types.PolicyTypePredictiveScaling,
		Alarms:            []types.Alarm{{AlarmARN: aws.String("b")}, {AlarmARN: aws.String("a")}},
	})
	require.NoError(t, err)
	assert.Equal(t,
		&ScalingPolicyResourceOutput{PolicyARN: "arn:policy",
			AlarmARNs: []string{"b",
				"a"},
			Name: r.Name, ResourceID: r.ResourceID, ServiceNamespace: r.ServiceNamespace,
			ScalableDimension: r.ScalableDimension, PolicyType: "PredictiveScaling"}, out)
	definition := r.ResourceDefinition()
	identity, err := definition.StableID(r, out)
	require.NoError(t, err)
	assert.Equal(t, `"dynamodb"/"table/a/b"/"dynamodb:table:ReadCapacityUnits"/"policy"`, identity)
	_, err = definition.StableID(r, nil)
	require.Error(t, err)
}

func TestPolicySourceSetDuplicates(t *testing.T) {
	one := &PolicyStepScaling{StepAdjustment: &[]PolicyStepAdjustment{{ScalingAdjustment: 1}}}
	duplicate := &PolicyStepScaling{StepAdjustment: &[]PolicyStepAdjustment{
		{ScalingAdjustment: 1}, {ScalingAdjustment: 1}}}
	assert.True(t, equalPolicyStep(one, duplicate))
	a := &PolicyTargetTracking{TargetValue: 1, CustomizedMetricSpecification: &PolicyCustomizedMetric{
		Dimensions: &[]PolicyDimension{{Name: "n", Value: "v"}}}}
	b := &PolicyTargetTracking{TargetValue: 1, CustomizedMetricSpecification: &PolicyCustomizedMetric{
		Dimensions: &[]PolicyDimension{{Name: "n", Value: "v"}, {Name: "n", Value: "v"}}}}
	assert.True(t, equalPolicyTracking(a, b))
}

func TestPolicyOptionalEmptyCollections(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "omitted", true: "empty"}[empty], func(t *testing.T) {
			r := testPolicy()
			r.StepScaling = &PolicyStepScaling{}
			r.TargetTracking = &PolicyTargetTracking{
				CustomizedMetricSpecification: &PolicyCustomizedMetric{}}
			metric := PolicyPredictiveQueryMetric{}
			if empty {
				r.StepScaling.StepAdjustment = &[]PolicyStepAdjustment{}
				r.TargetTracking.CustomizedMetricSpecification.Dimensions = &[]PolicyDimension{}
				r.TargetTracking.CustomizedMetricSpecification.Metrics = &[]PolicyTargetQuery{}
				metric.Dimension = &[]PolicyDimension{}
			}
			custom := &PolicyCustom{MetricDataQuery: []PolicyPredictiveQuery{{ID: "id",
				MetricStat: &PredictiveStat{Stat: "Sum", Metric: metric}}}}
			r.PredictiveScaling = &PolicyPredictiveScaling{MetricSpecifications: []PolicyPredictiveMetric{{
				CustomizedCapacityMetricSpecification: custom, CustomizedLoadMetricSpecification: custom,
				CustomizedScalingMetricSpecification: custom}}}
			got, err := r.preparePolicy()
			require.NoError(t, err)
			assert.Equal(t, &types.StepScalingPolicyConfiguration{Cooldown: aws.Int32(0)},
				got.StepScalingPolicyConfiguration)
			assert.Equal(t, &types.TargetTrackingScalingPolicyConfiguration{TargetValue: aws.Float64(0),
				DisableScaleIn: aws.Bool(false), ScaleInCooldown: aws.Int32(0), ScaleOutCooldown: aws.Int32(0),
				CustomizedMetricSpecification: &types.CustomizedMetricSpecification{
					MetricName: aws.String(""), Namespace: aws.String("")}},
				got.TargetTrackingScalingPolicyConfiguration)
			sdkCustom := &types.PredictiveScalingCustomizedMetricSpecification{
				MetricDataQueries: []types.PredictiveScalingMetricDataQuery{{Id: aws.String("id"),
					ReturnData: aws.Bool(false), MetricStat: &types.PredictiveScalingMetricStat{
						Stat: aws.String("Sum"), Metric: &types.PredictiveScalingMetric{}}}}}
			assert.Equal(t, &types.PredictiveScalingPolicyConfiguration{
				MetricSpecifications: []types.PredictiveScalingMetricSpecification{{
					CustomizedCapacityMetricSpecification: sdkCustom,
					CustomizedLoadMetricSpecification:     sdkCustom,
					CustomizedScalingMetricSpecification:  sdkCustom}}},
				got.PredictiveScalingPolicyConfiguration)
		})
	}
	assert.True(t, equalPolicyStep(&PolicyStepScaling{},
		&PolicyStepScaling{StepAdjustment: &[]PolicyStepAdjustment{}}))
	assert.True(t, equalPolicyTracking(&PolicyTargetTracking{
		CustomizedMetricSpecification: &PolicyCustomizedMetric{}}, &PolicyTargetTracking{
		CustomizedMetricSpecification: &PolicyCustomizedMetric{Dimensions: &[]PolicyDimension{},
			Metrics: &[]PolicyTargetQuery{}}}))
}

func TestPolicyTargetInlineDefaultsAndQueryDimensions(t *testing.T) {
	for _, value := range []*string{nil, aws.String(""), aws.String("value")} {
		r := testPolicy()
		r.TargetTracking = &PolicyTargetTracking{CustomizedMetricSpecification: &PolicyCustomizedMetric{
			MetricName: value, Namespace: value}}
		got, err := r.preparePolicy()
		require.NoError(t, err)
		assert.Equal(t, &types.CustomizedMetricSpecification{MetricName: aws.String(aws.ToString(value)),
			Namespace: aws.String(aws.ToString(value))},
			got.TargetTrackingScalingPolicyConfiguration.CustomizedMetricSpecification)
	}
	for _, empty := range []bool{false, true} {
		metric := PolicyTargetMetric{MetricName: "name", Namespace: "namespace"}
		if empty {
			metric.Dimensions = &[]PolicyDimension{}
		}
		r := testPolicy()
		r.TargetTracking = &PolicyTargetTracking{CustomizedMetricSpecification: &PolicyCustomizedMetric{
			Metrics: &[]PolicyTargetQuery{{ID: "query", MetricStat: &PolicyTargetMetricStat{
				Stat: "Sum", Metric: metric}}}}}
		got, err := r.preparePolicy()
		require.NoError(t, err)
		assert.Equal(t, &types.CustomizedMetricSpecification{
			Metrics: []types.TargetTrackingMetricDataQuery{{Id: aws.String("query"),
				ReturnData: aws.Bool(true), MetricStat: &types.TargetTrackingMetricStat{
					Stat: aws.String("Sum"), Metric: &types.TargetTrackingMetric{
						MetricName: aws.String("name"), Namespace: aws.String("namespace")}}}}},
			got.TargetTrackingScalingPolicyConfiguration.CustomizedMetricSpecification)
	}
}

func TestPolicyTargetOutputMetricBranch(t *testing.T) {
	for _, queries := range [][]types.TargetTrackingMetricDataQuery{nil, {}} {
		out, err := policyOutput(types.ScalingPolicy{
			TargetTrackingScalingPolicyConfiguration: &types.TargetTrackingScalingPolicyConfiguration{
				CustomizedMetricSpecification: &types.CustomizedMetricSpecification{
					MetricName: aws.String("name"), Namespace: aws.String("namespace"),
					Statistic: types.MetricStatisticSum, Unit: aws.String("Count"),
					Dimensions: []types.MetricDimension{{Name: aws.String("n"), Value: aws.String("v")}},
					Metrics:    queries}}})
		require.NoError(t, err)
		want := &PolicyCustomizedMetric{MetricName: aws.String("name"),
			Namespace: aws.String("namespace"),
			Statistic: aws.String("Sum"), Unit: aws.String("Count"),
			Dimensions: &[]PolicyDimension{{Name: "n", Value: "v"}}}
		if queries != nil {
			want = &PolicyCustomizedMetric{Metrics: &[]PolicyTargetQuery{}}
		}
		assert.Equal(t, want, out.TargetTracking.CustomizedMetricSpecification)
	}
}

func TestPolicyIndependentFamiliesAndOptionalValues(t *testing.T) {
	r := testPolicy()
	r.StepScaling = &PolicyStepScaling{AdjustmentType: aws.String("ExactCapacity"),
		Cooldown: aws.Int64(-7), MetricAggregationType: aws.String("Minimum")}
	r.TargetTracking = &PolicyTargetTracking{TargetValue: -0.25, DisableScaleIn: aws.Bool(true),
		ScaleInCooldown: aws.Int64(-5), ScaleOutCooldown: aws.Int64(12)}
	r.PredictiveScaling = &PolicyPredictiveScaling{
		MaxCapacityBreachBehavior: aws.String("IncreaseMaxCapacity"), MaxCapacityBuffer: aws.Int64(1),
		Mode: aws.String("ForecastAndScale"), SchedulingBufferTime: aws.Int64(3600),
		MetricSpecifications: []PolicyPredictiveMetric{{TargetValue: aws.Float64(-0.5)}}}
	got, err := r.preparePolicy()
	require.NoError(t, err)
	assert.Equal(t, &sdk.PutScalingPolicyInput{PolicyName: aws.String(r.Name),
		ResourceId: aws.String(r.ResourceID), ServiceNamespace: types.ServiceNamespaceDynamodb,
		ScalableDimension: types.ScalableDimensionDynamoDBTableReadCapacityUnits,
		PolicyType:        types.PolicyTypeStepScaling,
		StepScalingPolicyConfiguration: &types.StepScalingPolicyConfiguration{
			AdjustmentType: types.AdjustmentTypeExactCapacity, Cooldown: aws.Int32(-7),
			MetricAggregationType: types.MetricAggregationTypeMinimum},
		TargetTrackingScalingPolicyConfiguration: &types.TargetTrackingScalingPolicyConfiguration{
			TargetValue: aws.Float64(-0.25), DisableScaleIn: aws.Bool(true),
			ScaleInCooldown: aws.Int32(-5), ScaleOutCooldown: aws.Int32(12)},
		PredictiveScalingPolicyConfiguration: &types.PredictiveScalingPolicyConfiguration{
			MaxCapacityBreachBehavior: types.PredictiveScalingMaxCapacityBreachBehaviorIncreaseMaxCapacity,
			MaxCapacityBuffer:         aws.Int32(1), Mode: types.PredictiveScalingModeForecastAndScale,
			SchedulingBufferTime: aws.Int32(3600),
			MetricSpecifications: []types.PredictiveScalingMetricSpecification{
				{TargetValue: aws.Float64(-0.5)}},
		}}, got)
}

func TestPredictiveStringOmissionsAndReturnedEmptyStrings(t *testing.T) {
	for _, value := range []*string{nil, aws.String(""), aws.String("value")} {
		custom := &PolicyCustom{MetricDataQuery: []PolicyPredictiveQuery{{ID: "", Expression: value,
			Label: value,
			MetricStat: &PredictiveStat{Stat: "",
				Unit: value,
				Metric: PolicyPredictiveQueryMetric{
					MetricName: value, Namespace: value,
					Dimension: &[]PolicyDimension{{Name: aws.ToString(value),
						Value: aws.ToString(value)}}}}}}}
		preset := &PolicyPreset{PredefinedMetricType: "type", ResourceLabel: value}
		r := testPolicy()
		r.PredictiveScaling = &PolicyPredictiveScaling{MetricSpecifications: []PolicyPredictiveMetric{{
			CustomizedCapacityMetricSpecification: custom, CustomizedLoadMetricSpecification: custom,
			CustomizedScalingMetricSpecification: custom, PredefinedLoadMetricSpecification: preset,
			PredefinedMetricPairSpecification: preset, PredefinedScalingMetricSpecification: preset,
		}}}
		got, err := r.preparePolicy()
		require.NoError(t, err)
		var optional *string
		if value != nil && *value != "" {
			optional = value
		}
		sdkCustom := &types.PredictiveScalingCustomizedMetricSpecification{
			MetricDataQueries: []types.PredictiveScalingMetricDataQuery{{
				Expression: optional, Label: optional, ReturnData: aws.Bool(false),
				MetricStat: &types.PredictiveScalingMetricStat{Unit: optional,
					Metric: &types.PredictiveScalingMetric{MetricName: optional, Namespace: optional,
						Dimensions: []types.PredictiveScalingMetricDimension{
							{Name: optional, Value: optional}}}}}}}
		sdkPreset := &types.PredictiveScalingPredefinedScalingMetricSpecification{
			PredefinedMetricType: aws.String("type"), ResourceLabel: optional}
		expected := &types.PredictiveScalingPolicyConfiguration{
			MetricSpecifications: []types.PredictiveScalingMetricSpecification{{
				CustomizedCapacityMetricSpecification: sdkCustom, CustomizedLoadMetricSpecification: sdkCustom,
				CustomizedScalingMetricSpecification: sdkCustom,
				PredefinedLoadMetricSpecification: &types.PredictiveScalingPredefinedLoadMetricSpecification{
					PredefinedMetricType: aws.String("type"), ResourceLabel: optional},
				PredefinedMetricPairSpecification: &types.PredictiveScalingPredefinedMetricPairSpecification{
					PredefinedMetricType: aws.String("type"), ResourceLabel: optional},
				PredefinedScalingMetricSpecification: sdkPreset,
			}}}
		assert.Equal(t, expected, got.PredictiveScalingPolicyConfiguration)
		// Describe preserves pointers to empty strings as returned values.
		sdkCustom.MetricDataQueries[0].Id = aws.String("")
		sdkCustom.MetricDataQueries[0].Expression = value
		sdkCustom.MetricDataQueries[0].Label = value
		sdkCustom.MetricDataQueries[0].MetricStat.Stat = aws.String("")
		sdkCustom.MetricDataQueries[0].MetricStat.Unit = value
		sdkCustom.MetricDataQueries[0].MetricStat.Metric.MetricName = value
		sdkCustom.MetricDataQueries[0].MetricStat.Metric.Namespace = value
		sdkCustom.MetricDataQueries[0].MetricStat.Metric.Dimensions[0].Name =
			aws.String(aws.ToString(value))
		sdkCustom.MetricDataQueries[0].MetricStat.Metric.Dimensions[0].Value =
			aws.String(aws.ToString(value))
		expected.MetricSpecifications[0].PredefinedLoadMetricSpecification.ResourceLabel = value
		expected.MetricSpecifications[0].PredefinedMetricPairSpecification.ResourceLabel = value
		expected.MetricSpecifications[0].PredefinedScalingMetricSpecification.ResourceLabel = value
		out, err := policyOutput(types.ScalingPolicy{PredictiveScalingPolicyConfiguration: expected})
		require.NoError(t, err)
		want := *r.PredictiveScaling
		want.Mode = aws.String("")
		want.MaxCapacityBreachBehavior = aws.String("")
		custom.MetricDataQuery[0].ReturnData = aws.Bool(false)
		assert.Equal(t, &want, out.PredictiveScaling)
	}
}
