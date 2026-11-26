package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	sdk "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamotypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const tableName = "unobin-it-scalable-target"

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
	result, err := client.DescribeScalableTargets(ctx, &sdk.DescribeScalableTargetsInput{
		ResourceIds: []string{"table/" + tableName}, ServiceNamespace: types.ServiceNamespaceDynamodb,
		ScalableDimension: types.ScalableDimensionDynamoDBTableReadCapacityUnits,
	})
	if err != nil {
		return err
	}
	phase := os.Getenv("VERIFY_PHASE")
	if phase == "destroyed" {
		if len(result.ScalableTargets) != 0 {
			return fmt.Errorf("target still exists: %+v", result)
		}
		_, err := dynamodb.NewFromConfig(cfg).DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(tableName),
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
	if len(result.ScalableTargets) != 1 {
		return fmt.Errorf("unexpected targets: %+v", result)
	}
	target := result.ScalableTargets[0]
	minimum, maximum := int32(1), int32(10)
	expected := map[string]string{"keep": "", "remove": "yes"}
	updated := phase == "updated"
	if updated {
		minimum, maximum = 2, 20
		expected = map[string]string{"keep": "", "stage": "updated"}
	}
	if aws.ToInt32(target.MinCapacity) != minimum || aws.ToInt32(target.MaxCapacity) != maximum {
		return fmt.Errorf("unexpected capacities: %+v", target)
	}
	if aws.ToString(target.ResourceId) != "table/"+tableName ||
		target.ServiceNamespace != types.ServiceNamespaceDynamodb ||
		target.ScalableDimension != types.ScalableDimensionDynamoDBTableReadCapacityUnits ||
		aws.ToString(target.ScalableTargetARN) == "" {
		return fmt.Errorf("unexpected target identity: %+v", target)
	}
	state := target.SuspendedState
	if state == nil || aws.ToBool(state.DynamicScalingInSuspended) != updated ||
		aws.ToBool(state.DynamicScalingOutSuspended) != updated ||
		aws.ToBool(state.ScheduledScalingSuspended) != updated {
		return fmt.Errorf("unexpected suspension: %+v", state)
	}
	tags, err := client.ListTagsForResource(ctx, &sdk.ListTagsForResourceInput{
		ResourceARN: target.ScalableTargetARN,
	})
	if err != nil {
		return err
	}
	if !maps.Equal(tags.Tags, expected) {
		return fmt.Errorf("unexpected tags: %+v", tags.Tags)
	}
	return nil
}
