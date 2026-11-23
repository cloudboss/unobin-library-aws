package main

import (
	"context"
	"fmt"
	"log"
	"maps"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

const alarmName = "unobin-it-composite"

func main() {
	if err := verify(context.Background(), os.Getenv("VERIFY_PHASE")); err != nil {
		log.Fatal(err)
	}
}

func verify(ctx context.Context, phase string) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	client := cloudwatch.NewFromConfig(cfg)
	result, err := client.DescribeAlarms(ctx, &cloudwatch.DescribeAlarmsInput{
		AlarmNames: []string{alarmName},
		AlarmTypes: []types.AlarmType{types.AlarmTypeCompositeAlarm},
	})
	if err != nil {
		return fmt.Errorf("describe composite alarm: %w", err)
	}

	switch phase {
	case "applied":
		return verifyAlarm(ctx, client, result, "TRUE", true,
			map[string]string{"env": "initial", "remove": "yes"})
	case "updated":
		return verifyAlarm(ctx, client, result, "FALSE", false,
			map[string]string{"env": "updated", "empty": ""})
	case "destroyed":
		if len(result.CompositeAlarms) != 0 {
			return fmt.Errorf("composite alarm remains after destroy: %v", result.CompositeAlarms)
		}
		return nil
	default:
		return fmt.Errorf("unexpected verification phase %q", phase)
	}
}

func verifyAlarm(
	ctx context.Context, client *cloudwatch.Client, result *cloudwatch.DescribeAlarmsOutput,
	rule string, enabled bool, expectedTags map[string]string,
) error {
	if len(result.CompositeAlarms) != 1 {
		return fmt.Errorf("expected exactly one composite alarm, got %v", result.CompositeAlarms)
	}
	alarm := result.CompositeAlarms[0]
	if aws.ToString(alarm.AlarmName) != alarmName || aws.ToString(alarm.AlarmRule) != rule ||
		alarm.ActionsEnabled == nil || *alarm.ActionsEnabled != enabled ||
		aws.ToString(alarm.AlarmArn) == "" {
		return fmt.Errorf("unexpected composite alarm: %+v", alarm)
	}
	tags, err := client.ListTagsForResource(ctx,
		&cloudwatch.ListTagsForResourceInput{ResourceARN: alarm.AlarmArn})
	if err != nil {
		return fmt.Errorf("list tags for %q: %w", aws.ToString(alarm.AlarmArn), err)
	}
	actualTags := map[string]string{}
	for _, tag := range tags.Tags {
		actualTags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	if !maps.Equal(expectedTags, actualTags) {
		return fmt.Errorf("expected tags %v, got %v", expectedTags, actualTags)
	}
	return nil
}
