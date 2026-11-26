package applicationautoscaling

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type policyStub struct {
	puts    []*sdk.PutScalingPolicyInput
	reads   []*sdk.DescribeScalingPoliciesInput
	deletes []*sdk.DeleteScalingPolicyInput
	calls   []string
	put     func(context.Context) error
	read    func(context.Context, *sdk.DescribeScalingPoliciesInput) (
		*sdk.DescribeScalingPoliciesOutput, error)
	delete func(context.Context) error
	policy types.ScalingPolicy
}

func (s *policyStub) PutScalingPolicy(ctx context.Context, in *sdk.PutScalingPolicyInput,
	_ ...func(*sdk.Options)) (*sdk.PutScalingPolicyOutput, error) {
	s.puts = append(s.puts, in)
	s.calls = append(s.calls, "put")
	if s.put != nil {
		return nil, s.put(ctx)
	}
	return &sdk.PutScalingPolicyOutput{PolicyARN: aws.String("ignored-put-arn")}, nil
}

func (s *policyStub) DescribeScalingPolicies(
	ctx context.Context, in *sdk.DescribeScalingPoliciesInput,
	_ ...func(*sdk.Options)) (*sdk.DescribeScalingPoliciesOutput, error) {
	copy := *in
	s.reads = append(s.reads, &copy)
	s.calls = append(s.calls, "describe")
	if s.read != nil {
		return s.read(ctx, in)
	}
	return &sdk.DescribeScalingPoliciesOutput{ScalingPolicies: []types.ScalingPolicy{s.policy}}, nil
}

func (s *policyStub) DeleteScalingPolicy(ctx context.Context, in *sdk.DeleteScalingPolicyInput,
	_ ...func(*sdk.Options)) (*sdk.DeleteScalingPolicyOutput, error) {
	s.deletes = append(s.deletes, in)
	s.calls = append(s.calls, "delete")
	if s.delete != nil {
		return nil, s.delete(ctx)
	}
	return &sdk.DeleteScalingPolicyOutput{}, nil
}

func remotePolicy() types.ScalingPolicy {
	r := testPolicy()
	return types.ScalingPolicy{PolicyARN: aws.String("observed-arn"), PolicyName: aws.String(r.Name),
		ResourceId:        aws.String(r.ResourceID),
		ServiceNamespace:  types.ServiceNamespace(r.ServiceNamespace),
		ScalableDimension: types.ScalableDimension(r.ScalableDimension),
		PolicyType:        types.PolicyTypeStepScaling}
}

func TestPolicyCreatePaginationAndCardinality(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		for _, created := range []bool{false, true} {
			t.Run(time.Duration(count).String()+"/"+map[bool]string{true: "create", false: "read"}[created],
				func(t *testing.T) {
					r := testPolicy()
					client := &policyStub{}
					client.read = func(_ context.Context, in *sdk.DescribeScalingPoliciesInput,
					) (*sdk.DescribeScalingPoliciesOutput, error) {
						misleading := remotePolicy()
						misleading.PolicyName = aws.String("other")
						wrongDimension := remotePolicy()
						wrongDimension.ScalableDimension = types.ScalableDimension("other")
						if in.NextToken == nil {
							return &sdk.DescribeScalingPoliciesOutput{
									ScalingPolicies: []types.ScalingPolicy{misleading,
										wrongDimension},
									NextToken: aws.String("next")},
								nil
						}
						matches := make([]types.ScalingPolicy, count)
						for i := range matches {
							matches[i] = remotePolicy()
						}
						return &sdk.DescribeScalingPoliciesOutput{ScalingPolicies: matches}, nil
					}
					var out *ScalingPolicyResourceOutput
					var err error
					if created {
						out, err = r.createPolicy(t.Context(), client, &fakeClock{})
					} else {
						out, err = readPolicy(t.Context(), client, r.policyKey(), &fakeClock{}, false)
					}
					if count == 1 {
						require.NoError(t, err)
						assert.Equal(t, "observed-arn", out.PolicyARN)
					} else {
						require.Error(t, err)
						if created {
							require.ErrorContains(t, err, "expected one result")
							assert.Equal(t, []string{"put", "describe", "describe", "delete"}, client.calls)
						} else {
							require.ErrorIs(t, err, runtime.ErrNotFound)
						}
					}
					expected := &sdk.DescribeScalingPoliciesInput{PolicyNames: []string{r.Name},
						ResourceId: aws.String(r.ResourceID), ServiceNamespace: types.ServiceNamespaceDynamodb,
						ScalableDimension: types.ScalableDimensionDynamoDBTableReadCapacityUnits}
					assert.Equal(t, expected, client.reads[0])
					expectedNext := *expected
					expectedNext.NextToken = aws.String("next")
					assert.Equal(t, &expectedNext, client.reads[1])
				})
		}
	}
}

func TestPolicyTypedErrorsAndDelete(t *testing.T) {
	access := &types.FailedResourceAccessException{Message: aws.String("not ready")}
	missing := &types.ObjectNotFoundException{Message: aws.String("missing")}
	generic := &smithy.GenericAPIError{Code: "FailedResourceAccessException", Message: "generic"}
	for _, failure := range []error{missing, generic, errors.New("other")} {
		client := &policyStub{read: func(context.Context, *sdk.DescribeScalingPoliciesInput,
		) (*sdk.DescribeScalingPoliciesOutput, error) {
			return nil, failure
		}}
		_, err := readPolicy(t.Context(), client, testPolicy().policyKey(), &fakeClock{}, false)
		require.ErrorIs(t, err, failure)
		assert.Equal(t, []string{"describe"}, client.calls)
	}
	for _, failure := range []error{nil, missing, generic} {
		client := &policyStub{delete: func(context.Context) error { return failure }}
		err := deletePolicy(t.Context(), client, testPolicy().policyKey(), &fakeClock{})
		if failure == generic {
			require.ErrorIs(t, err, generic)
		} else {
			require.NoError(t, err)
		}
		assert.Equal(t, []string{"delete"}, client.calls)
		assert.Equal(t, []*sdk.DeleteScalingPolicyInput{{PolicyName: aws.String("policy"),
			ResourceId: aws.String("table/a/b"), ServiceNamespace: types.ServiceNamespaceDynamodb,
			ScalableDimension: types.ScalableDimensionDynamoDBTableReadCapacityUnits}}, client.deletes)
	}
	client := &policyStub{}
	client.delete = func(context.Context) error {
		if len(client.deletes) == 1 {
			return access
		}
		return nil
	}
	clock := &fakeClock{}
	require.NoError(t, deletePolicy(t.Context(), client, testPolicy().policyKey(), clock))
	assert.Equal(t, []string{"delete", "delete"}, client.calls)
	assert.Equal(t, []time.Duration{500 * time.Millisecond}, clock.delays)
}

func TestPolicyPropagationTiming(t *testing.T) {
	failure := &types.FailedResourceAccessException{Message: aws.String("retry")}
	for _, operation := range []string{"put", "describe", "delete"} {
		t.Run(operation, func(t *testing.T) {
			clock := &fakeClock{}
			client := &policyStub{
				put: func(ctx context.Context) error {
					_, deadline := ctx.Deadline()
					assert.False(t, deadline)
					return failure
				},
				read: func(context.Context, *sdk.DescribeScalingPoliciesInput,
				) (*sdk.DescribeScalingPoliciesOutput, error) {
					return nil, failure
				},
				delete: func(context.Context) error { return failure },
			}
			var err error
			r := testPolicy()
			switch operation {
			case "put":
				in, e := r.preparePolicy()
				require.NoError(t, e)
				err = putPolicy(t.Context(), client, in, clock)
			case "describe":
				_, err = readPolicy(t.Context(), client, r.policyKey(), clock, false)
			case "delete":
				err = deletePolicy(t.Context(), client, r.policyKey(), clock)
			}
			require.ErrorIs(t, err, failure)
			assert.Equal(t, 135500*time.Millisecond, clock.now.Sub(time.Time{}))
			expectedDelays := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second,
				4 * time.Second, 8 * time.Second}
			for range 12 {
				expectedDelays = append(expectedDelays, 10*time.Second)
			}
			assert.Equal(t, expectedDelays, clock.delays)
		})
	}
	for _, finalSuccess := range []bool{false, true} {
		clock := &fakeClock{}
		attempts := 0
		err := retryPolicy(t.Context(), clock, func(context.Context) error {
			attempts++
			if attempts == 18 && finalSuccess {
				return nil
			}
			return failure
		})
		if finalSuccess {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, failure)
		}
		assert.Equal(t, 18, attempts)
	}
	ctx, cancel := context.WithCancel(t.Context())
	clock := &fakeClock{cancel: cancel}
	attempts := 0
	err := retryPolicy(ctx, clock, func(context.Context) error { attempts++; return failure })
	require.ErrorIs(t, err, failure)
	assert.Equal(t, 1, attempts)
	assert.Equal(t, []time.Duration{500 * time.Millisecond}, clock.delays)
	ctx, cancel = context.WithCancel(t.Context())
	clock = &fakeClock{}
	attempts = 0
	err = retryPolicy(ctx, clock, func(context.Context) error {
		attempts++
		cancel()
		return failure
	})
	require.ErrorIs(t, err, failure)
	assert.Equal(t, 1, attempts)
	assert.Empty(t, clock.delays)
}

func TestPolicyRemovingLastOptionalItemDoesNotClear(t *testing.T) {
	for _, family := range []string{"step", "inline", "query", "queries", "predictive"} {
		t.Run(family, func(t *testing.T) {
			r := testPolicy()
			switch family {
			case "step":
				r.StepScaling = &PolicyStepScaling{
					StepAdjustment: &[]PolicyStepAdjustment{{ScalingAdjustment: 1}}}
			case "inline":
				r.TargetTracking = &PolicyTargetTracking{CustomizedMetricSpecification: &PolicyCustomizedMetric{
					Dimensions: &[]PolicyDimension{{Name: "n", Value: "v"}}}}
			case "query", "queries":
				r.TargetTracking = &PolicyTargetTracking{CustomizedMetricSpecification: &PolicyCustomizedMetric{
					Metrics: &[]PolicyTargetQuery{{ID: "q", MetricStat: &PolicyTargetMetricStat{
						Stat: "Sum", Metric: PolicyTargetMetric{MetricName: "n", Namespace: "ns",
							Dimensions: &[]PolicyDimension{{Name: "n", Value: "v"}}}}}}}}
			case "predictive":
				r.PredictiveScaling = &PolicyPredictiveScaling{MetricSpecifications: []PolicyPredictiveMetric{{
					CustomizedLoadMetricSpecification: &PolicyCustom{MetricDataQuery: []PolicyPredictiveQuery{{
						ID: "q", MetricStat: &PredictiveStat{Stat: "Sum", Metric: PolicyPredictiveQueryMetric{
							Dimension: &[]PolicyDimension{{Name: "n", Value: "v"}}}}}}}}}}
			}
			request, err := r.preparePolicy()
			require.NoError(t, err)
			remote := remotePolicy()
			remote.StepScalingPolicyConfiguration = request.StepScalingPolicyConfiguration
			remote.TargetTrackingScalingPolicyConfiguration =
				request.TargetTrackingScalingPolicyConfiguration
			remote.PredictiveScalingPolicyConfiguration = request.PredictiveScalingPolicyConfiguration
			observed, err := policyOutput(remote)
			require.NoError(t, err)
			for _, absent := range []bool{false, true} {
				var dimensions *[]PolicyDimension
				if !absent {
					dimensions = &[]PolicyDimension{}
				}
				switch family {
				case "step":
					r.StepScaling.StepAdjustment = nil
					if !absent {
						r.StepScaling.StepAdjustment = &[]PolicyStepAdjustment{}
					}
				case "inline":
					r.TargetTracking.CustomizedMetricSpecification.Dimensions = dimensions
				case "query":
					queries := r.TargetTracking.CustomizedMetricSpecification.Metrics
					(*queries)[0].MetricStat.Metric.Dimensions = dimensions
				case "queries":
					r.TargetTracking.CustomizedMetricSpecification.Metrics = nil
					if !absent {
						r.TargetTracking.CustomizedMetricSpecification.Metrics = &[]PolicyTargetQuery{}
					}
				case "predictive":
					custom := r.PredictiveScaling.MetricSpecifications[0].CustomizedLoadMetricSpecification
					custom.MetricDataQuery[0].MetricStat.Metric.Dimension = dimensions
				}
				client := &policyStub{policy: remote}
				_, err := r.updatePolicy(t.Context(), client, policyPrior{Outputs: observed}, &fakeClock{})
				require.NoError(t, err)
				assert.Equal(t, []string{"describe"}, client.calls)
				assert.Empty(t, client.puts)
			}
		})
	}
}

func TestPolicyAcceptedCreateCleanup(t *testing.T) {
	initial := errors.New("describe failed")
	cleanupFailure := errors.New("cleanup failed")
	for _, cleanupErr := range []error{nil, cleanupFailure} {
		ctx, cancel := context.WithCancel(t.Context())
		client := &policyStub{read: func(context.Context, *sdk.DescribeScalingPoliciesInput,
		) (*sdk.DescribeScalingPoliciesOutput, error) {
			cancel()
			return nil, initial
		}}
		client.delete = func(ctx context.Context) error {
			require.NoError(t, context.Cause(ctx))
			_, bounded := ctx.Deadline()
			assert.True(t, bounded)
			return cleanupErr
		}
		out, err := testPolicy().createPolicy(ctx, client, &fakeClock{})
		assert.Nil(t, out)
		require.ErrorIs(t, err, initial)
		if cleanupErr != nil {
			require.ErrorIs(t, err, cleanupFailure)
		}
		assert.Equal(t, []string{"put", "describe", "delete"}, client.calls)
	}
}

func TestPolicyObservedDriftAndSavedFallback(t *testing.T) {
	for _, useObserved := range []bool{false, true} {
		r := testPolicy()
		r.TargetTracking = &PolicyTargetTracking{TargetValue: 50}
		r.PolicyType = aws.String("TargetTrackingScaling")
		remote := remotePolicy()
		remote.PolicyType = types.PolicyTypeTargetTrackingScaling
		remote.TargetTrackingScalingPolicyConfiguration = &types.TargetTrackingScalingPolicyConfiguration{
			TargetValue: aws.Float64(20)}
		current, err := policyOutput(remote)
		require.NoError(t, err)
		prior := policyPrior{Inputs: r, Outputs: current}
		if useObserved {
			saved := *current
			saved.TargetTracking = &PolicyTargetTracking{TargetValue: 50}
			prior.Outputs, prior.Observed = &saved, current
		}
		client := &policyStub{policy: remote}
		_, err = r.updatePolicy(t.Context(), client, prior, &fakeClock{})
		require.NoError(t, err)
		assert.Equal(t, []string{"put", "describe"}, client.calls)
		expected, err := r.preparePolicy()
		require.NoError(t, err)
		assert.Equal(t, []*sdk.PutScalingPolicyInput{expected}, client.puts)
		r.TargetTracking = nil
		client.calls, client.puts = nil, nil
		_, err = r.updatePolicy(t.Context(), client, prior, &fakeClock{})
		require.NoError(t, err)
		assert.Equal(t, []string{"describe"}, client.calls)
	}
}

func TestPolicyUnmanagedObservationsDoNotReset(t *testing.T) {
	for _, family := range []string{"step", "predictive"} {
		r := testPolicy()
		remote := remotePolicy()
		if family == "step" {
			r.StepScaling = &PolicyStepScaling{}
			remote.StepScalingPolicyConfiguration = &types.StepScalingPolicyConfiguration{
				AdjustmentType: types.AdjustmentTypeChangeInCapacity, Cooldown: aws.Int32(0),
				MetricAggregationType: types.MetricAggregationTypeAverage, MinAdjustmentMagnitude: aws.Int32(3),
				StepAdjustments: []types.StepAdjustment{{ScalingAdjustment: aws.Int32(1)}},
			}
		} else {
			r.PolicyType = aws.String("PredictiveScaling")
			r.PredictiveScaling = &PolicyPredictiveScaling{
				MetricSpecifications: []PolicyPredictiveMetric{{}}}
			remote.PolicyType = types.PolicyTypePredictiveScaling
			remote.PredictiveScalingPolicyConfiguration = &types.PredictiveScalingPolicyConfiguration{
				Mode: types.PredictiveScalingModeForecastOnly, SchedulingBufferTime: aws.Int32(300),
				MaxCapacityBreachBehavior: types.PredictiveScalingMaxCapacityBreachBehaviorHonorMaxCapacity,
				MaxCapacityBuffer:         aws.Int32(5),
				MetricSpecifications: []types.PredictiveScalingMetricSpecification{
					{TargetValue: aws.Float64(50)},
				},
			}
		}
		observed, err := policyOutput(remote)
		require.NoError(t, err)
		client := &policyStub{policy: remote}
		_,
			err = r.updatePolicy(t.Context(),
			client,
			policyPrior{Inputs: r,
				Outputs: observed},
			&fakeClock{})
		require.NoError(t, err)
		assert.Equal(t, []string{"describe"}, client.calls)
	}
}
