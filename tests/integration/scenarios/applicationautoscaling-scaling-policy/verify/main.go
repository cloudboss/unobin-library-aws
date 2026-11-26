package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	sdk "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamotypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"log"
	"os"
)

const policyName = "unobin-it-scaling-policy"

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
func run(ctx context.Context) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := sdk.NewFromConfig(cfg)
	result, err := client.DescribeScalingPolicies(ctx, &sdk.DescribeScalingPoliciesInput{
		PolicyNames: []string{policyName}, ResourceId: aws.String("table/" + policyName),
		ServiceNamespace:  types.ServiceNamespaceDynamodb,
		ScalableDimension: types.ScalableDimensionDynamoDBTableReadCapacityUnits,
	})
	if err != nil {
		return err
	}
	phase := os.Getenv("VERIFY_PHASE")
	if phase == "destroyed" {
		if len(result.ScalingPolicies) != 0 {
			return fmt.Errorf("policy still exists: %+v", result)
		}
		targets, err := client.DescribeScalableTargets(ctx, &sdk.DescribeScalableTargetsInput{
			ResourceIds: []string{"table/" + policyName}, ServiceNamespace: types.ServiceNamespaceDynamodb,
			ScalableDimension: types.ScalableDimensionDynamoDBTableReadCapacityUnits,
		})
		if err != nil {
			return err
		}
		if len(targets.ScalableTargets) != 0 {
			return fmt.Errorf("managed target still exists: %+v", targets)
		}
		_, err = dynamodb.NewFromConfig(cfg).DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(policyName),
		})
		var missing *dynamotypes.ResourceNotFoundException
		if errors.As(err, &missing) {
			return nil
		}
		if err == nil {
			return errors.New("managed table still exists")
		}
		return err
	}
	if phase != "applied" && phase != "updated" {
		return fmt.Errorf("unexpected phase %q", phase)
	}
	if len(result.ScalingPolicies) != 1 {
		return fmt.Errorf("unexpected policies: %+v", result)
	}
	policy := result.ScalingPolicies[0]
	if aws.ToString(policy.PolicyName) != policyName ||
		aws.ToString(policy.ResourceId) != "table/"+policyName ||
		policy.ServiceNamespace != types.ServiceNamespaceDynamodb ||
		policy.ScalableDimension != types.ScalableDimensionDynamoDBTableReadCapacityUnits ||
		policy.PolicyType != types.PolicyTypeTargetTrackingScaling ||
		aws.ToString(policy.PolicyARN) == "" {
		return fmt.Errorf("unexpected policy identity: %+v", policy)
	}
	target, in, out := float64(50), int32(30), int32(15)
	updated := phase == "updated"
	if updated {
		target, in, out = 65, 60, 45
	}
	tracking := policy.TargetTrackingScalingPolicyConfiguration
	if tracking == nil || aws.ToFloat64(tracking.TargetValue) != target ||
		aws.ToBool(tracking.DisableScaleIn) != updated || aws.ToInt32(tracking.ScaleInCooldown) != in ||
		aws.ToInt32(tracking.ScaleOutCooldown) != out || tracking.PredefinedMetricSpecification == nil ||
		tracking.PredefinedMetricSpecification.PredefinedMetricType !=
			types.MetricTypeDynamoDBReadCapacityUtilization {
		return fmt.Errorf("unexpected target tracking: %+v", tracking)
	}
	return nil
}
