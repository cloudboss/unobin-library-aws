package applicationautoscaling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type policyClient interface {
	PutScalingPolicy(context.Context, *sdk.PutScalingPolicyInput,
		...func(*sdk.Options)) (*sdk.PutScalingPolicyOutput, error)
	DescribeScalingPolicies(context.Context, *sdk.DescribeScalingPoliciesInput,
		...func(*sdk.Options)) (*sdk.DescribeScalingPoliciesOutput, error)
	DeleteScalingPolicy(context.Context, *sdk.DeleteScalingPolicyInput,
		...func(*sdk.Options)) (*sdk.DeleteScalingPolicyOutput, error)
}

func retryPolicy(
	ctx context.Context, clock targetClock, operation func(context.Context) error,
) error {
	return retryTarget(ctx, clock, 2*time.Minute, operation, func(err error) bool {
		var retry *types.FailedResourceAccessException
		return errors.As(err, &retry)
	})
}

func (r ScalingPolicyResource) policyKey() *ScalingPolicyResourceOutput {
	return &ScalingPolicyResourceOutput{Name: r.Name, ResourceID: r.ResourceID,
		ServiceNamespace: r.ServiceNamespace, ScalableDimension: r.ScalableDimension}
}

func policyIdentity(prior policyPrior) *ScalingPolicyResourceOutput {
	if prior.Outputs != nil {
		return prior.Outputs
	}
	return prior.Inputs.policyKey()
}

func (r ScalingPolicyResource) createPolicy(
	ctx context.Context, client policyClient, clock targetClock,
) (*ScalingPolicyResourceOutput, error) {
	request, err := r.preparePolicy()
	if err != nil {
		return nil, err
	}
	if err := putPolicy(ctx, client, request, clock); err != nil {
		return nil, err
	}
	output, err := readPolicy(ctx, client, r.policyKey(), clock, true)
	if err == nil {
		return output, nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	if cleanupErr := deletePolicy(cleanup, client, r.policyKey(), clock); cleanupErr != nil {
		err = errors.Join(err, fmt.Errorf("clean up scaling policy after failed create: %w", cleanupErr))
	}
	return nil, err
}

func putPolicy(
	ctx context.Context, client policyClient, request *sdk.PutScalingPolicyInput, clock targetClock,
) error {
	err := retryPolicy(ctx, clock, func(ctx context.Context) error {
		_, err := client.PutScalingPolicy(ctx, request)
		return err
	})
	if err != nil {
		return fmt.Errorf("put scaling policy: %w", err)
	}
	return nil
}

func readPolicy(
	ctx context.Context, client policyClient, key *ScalingPolicyResourceOutput,
	clock targetClock, created bool,
) (*ScalingPolicyResourceOutput, error) {
	if key == nil {
		return nil, fmt.Errorf("scaling policy prior output is missing")
	}
	var found []types.ScalingPolicy
	err := retryPolicy(ctx, clock, func(ctx context.Context) error {
		found = nil
		request := &sdk.DescribeScalingPoliciesInput{
			PolicyNames: []string{key.Name}, ResourceId: aws.String(key.ResourceID),
			ServiceNamespace:  types.ServiceNamespace(key.ServiceNamespace),
			ScalableDimension: types.ScalableDimension(key.ScalableDimension),
		}
		for {
			page, err := client.DescribeScalingPolicies(ctx, request)
			if err != nil {
				return err
			}
			for _, policy := range page.ScalingPolicies {
				if aws.ToString(policy.PolicyName) == key.Name &&
					string(policy.ScalableDimension) == key.ScalableDimension {
					found = append(found, policy)
				}
			}
			if aws.ToString(page.NextToken) == "" {
				return nil
			}
			request.NextToken = page.NextToken
		}
	})
	if err != nil {
		return nil, fmt.Errorf("describe scaling policies: %w", err)
	}
	if len(found) != 1 {
		if !created {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe created scaling policy: expected one result, got %d", len(found))
	}
	return policyOutput(found[0])
}

func deletePolicy(
	ctx context.Context, client policyClient, key *ScalingPolicyResourceOutput, clock targetClock,
) error {
	if key == nil {
		return fmt.Errorf("scaling policy prior output is missing")
	}
	err := retryPolicy(ctx, clock, func(ctx context.Context) error {
		_, err := client.DeleteScalingPolicy(ctx, &sdk.DeleteScalingPolicyInput{
			PolicyName: aws.String(key.Name), ResourceId: aws.String(key.ResourceID),
			ServiceNamespace:  types.ServiceNamespace(key.ServiceNamespace),
			ScalableDimension: types.ScalableDimension(key.ScalableDimension),
		})
		return err
	})
	var missing *types.ObjectNotFoundException
	if errors.As(err, &missing) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete scaling policy: %w", err)
	}
	return nil
}

func (r ScalingPolicyResource) updatePolicy(
	ctx context.Context, client policyClient, prior policyPrior, clock targetClock,
) (*ScalingPolicyResourceOutput, error) {
	observed := prior.Observed
	if observed == nil {
		observed = prior.Outputs
	}
	if observed == nil {
		return nil, fmt.Errorf("scaling policy prior output is missing")
	}
	request, err := r.preparePolicy()
	if err != nil {
		return nil, err
	}
	remote := ScalingPolicyResource{
		Name: r.Name, ResourceID: r.ResourceID, ServiceNamespace: r.ServiceNamespace,
		ScalableDimension: r.ScalableDimension, PolicyType: aws.String(observed.PolicyType),
	}
	remote.StepScaling = observed.StepScaling
	remote.TargetTracking = observed.TargetTracking
	remote.PredictiveScaling = observed.PredictiveScaling
	var desired, currentObservation ScalingPolicyResource
	if err := convertPolicy(r, &desired); err != nil {
		return nil, err
	}
	if err := convertPolicy(remote, &currentObservation); err != nil {
		return nil, err
	}
	projectPolicyObservation(reflect.ValueOf(desired), reflect.ValueOf(&currentObservation).Elem(), "")
	remote = currentObservation
	current, err := remote.preparePolicy()
	if err != nil {
		return nil, err
	}
	key := policyIdentity(prior)
	request.PolicyName, request.ResourceId = aws.String(key.Name), aws.String(key.ResourceID)
	request.ServiceNamespace = types.ServiceNamespace(key.ServiceNamespace)
	request.ScalableDimension = types.ScalableDimension(key.ScalableDimension)
	current.PolicyName, current.ResourceId = request.PolicyName, request.ResourceId
	current.ServiceNamespace = request.ServiceNamespace
	current.ScalableDimension = request.ScalableDimension
	if !reflect.DeepEqual(request, current) {
		if err := putPolicy(ctx, client, request, clock); err != nil {
			return nil, err
		}
	}
	return readPolicy(ctx, client, key, clock, false)
}

func projectPolicyObservation(desired, observed reflect.Value, field string) {
	if desired.Kind() == reflect.Pointer {
		switch field {
		case "PolicyType", "Cooldown", "DisableScaleIn", "ScaleInCooldown", "ScaleOutCooldown",
			"ReturnData", "MetricIntervalLowerBound", "MetricIntervalUpperBound":
		default:
			if desired.IsNil() {
				observed.Set(desired)
				return
			}
		}
		if desired.IsNil() || observed.IsNil() {
			return
		}
		switch field {
		case "MinAdjustmentMagnitude", "MaxCapacityBuffer", "SchedulingBufferTime":
			if desired.Elem().Int() <= 0 {
				observed.Set(desired)
				return
			}
		}
		projectPolicyObservation(desired.Elem(), observed.Elem(), field)
		return
	}
	switch desired.Kind() {
	case reflect.Struct:
		for i := range desired.NumField() {
			projectPolicyObservation(desired.Field(i), observed.Field(i), desired.Type().Field(i).Name)
		}
	case reflect.Slice:
		if desired.Len() == 0 {
			observed.Set(desired)
			return
		}
		if desired.Len() != observed.Len() {
			return
		}
		switch desired.Type().Elem().Name() {
		case "PolicyStepAdjustment", "PolicyDimension", "PolicyTargetQuery":
			sortPolicyObservation(desired)
			sortPolicyObservation(observed)
		}
		for i := range desired.Len() {
			projectPolicyObservation(desired.Index(i), observed.Index(i), "")
		}
	}
}

func sortPolicyObservation(values reflect.Value) {
	type item struct {
		value reflect.Value
		key   string
	}
	items := make([]item, values.Len())
	for i := range items {
		value := reflect.New(values.Type().Elem()).Elem()
		value.Set(values.Index(i))
		data, _ := json.Marshal(value.Interface())
		items[i] = item{value, string(data)}
	}
	slices.SortFunc(items, func(a, b item) int {
		return strings.Compare(a.key, b.key)
	})
	for i := range items {
		values.Index(i).Set(items[i].value)
	}
}
