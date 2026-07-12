package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/smithy-go"
)

const (
	alarmName             = "unobin-it-alarm"
	dashboardName         = "unobin-it-dashboard"
	initialAlarmThreshold = 80.0
	updatedAlarmThreshold = 90.0
	initialDashboardBody  = `{
		"widgets": [{
			"type": "text",
			"x": 0,
			"y": 0,
			"width": 6,
			"height": 3,
			"properties": {"markdown": "initial"}
		}]
	}`
	updatedDashboardBody = `{
		"widgets": [{
			"type": "text",
			"x": 0,
			"y": 0,
			"width": 12,
			"height": 3,
			"properties": {"markdown": "updated"}
		}]
	}`
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := cloudwatch.NewFromConfig(cfg)
	phase := os.Getenv("VERIFY_PHASE")

	switch phase {
	case "applied":
		return verifyPresent(ctx, client, initialAlarmThreshold, initialDashboardBody,
			map[string]string{
				"empty-create": "",
				"empty-update": "set",
				"env":          "initial",
				"remove":       "yes",
			})
	case "updated":
		return verifyPresent(ctx, client, updatedAlarmThreshold, updatedDashboardBody,
			map[string]string{
				"empty-create": "",
				"empty-update": "",
				"env":          "updated",
			})
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf(
			"verification mode must be applied, updated, or destroyed, got %q", phase)
	}
}

func verifyPresent(
	ctx context.Context,
	client *cloudwatch.Client,
	wantThreshold float64,
	wantBody string,
	wantTags map[string]string,
) error {
	if err := verifyAlarm(ctx, client, wantThreshold); err != nil {
		return err
	}
	return verifyDashboard(ctx, client, wantBody, wantTags)
}

func verifyAlarm(
	ctx context.Context,
	client *cloudwatch.Client,
	wantThreshold float64,
) error {
	alarm, err := findAlarm(ctx, client)
	if err != nil {
		return err
	}
	if alarm == nil {
		return fmt.Errorf("metric alarm %s not found", alarmName)
	}
	if got := aws.ToFloat64(alarm.Threshold); got != wantThreshold {
		return fmt.Errorf("alarm threshold is %v, want %v", got, wantThreshold)
	}
	fmt.Printf("ok: metric alarm %s present with threshold %v\n", alarmName, wantThreshold)
	return nil
}

func verifyDashboard(
	ctx context.Context,
	client *cloudwatch.Client,
	wantBody string,
	wantTags map[string]string,
) error {
	resp, err := client.GetDashboard(ctx,
		&cloudwatch.GetDashboardInput{DashboardName: aws.String(dashboardName)})
	if err != nil {
		return fmt.Errorf("get dashboard: %w", err)
	}
	if resp == nil {
		return errors.New("get dashboard returned no result")
	}
	if err := compareJSON(aws.ToString(resp.DashboardBody), wantBody); err != nil {
		return err
	}
	if aws.ToString(resp.DashboardName) != dashboardName {
		return fmt.Errorf("dashboard name is %q, want %q",
			aws.ToString(resp.DashboardName), dashboardName)
	}
	tagResp, err := client.ListTagsForResource(ctx,
		&cloudwatch.ListTagsForResourceInput{ResourceARN: resp.DashboardArn})
	if err != nil {
		return fmt.Errorf("list dashboard tags: %w", err)
	}
	if tagResp == nil {
		return errors.New("list dashboard tags returned no result")
	}
	gotTags := map[string]string{}
	for _, tag := range tagResp.Tags {
		gotTags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	if !reflect.DeepEqual(gotTags, wantTags) {
		return fmt.Errorf("dashboard tags are %#v, want %#v", gotTags, wantTags)
	}
	fmt.Printf("ok: dashboard %s present\n", dashboardName)
	return nil
}

func verifyDestroyed(ctx context.Context, client *cloudwatch.Client) error {
	alarm, err := findAlarm(ctx, client)
	if err != nil {
		return err
	}
	if alarm != nil {
		return fmt.Errorf("metric alarm %s still exists", alarmName)
	}
	_, err = client.GetDashboard(ctx,
		&cloudwatch.GetDashboardInput{DashboardName: aws.String(dashboardName)})
	if err == nil {
		return fmt.Errorf("dashboard %s still exists", dashboardName)
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFound" {
		return fmt.Errorf("get deleted dashboard: %w", err)
	}
	fmt.Printf("ok: metric alarm %s and dashboard %s gone\n", alarmName, dashboardName)
	return nil
}

func findAlarm(
	ctx context.Context,
	client *cloudwatch.Client,
) (*cloudwatchtypes.MetricAlarm, error) {
	resp, err := client.DescribeAlarms(ctx, &cloudwatch.DescribeAlarmsInput{
		AlarmNames: []string{alarmName},
		AlarmTypes: []cloudwatchtypes.AlarmType{cloudwatchtypes.AlarmTypeMetricAlarm},
	})
	if err != nil {
		return nil, fmt.Errorf("describe alarms: %w", err)
	}
	if len(resp.MetricAlarms) == 0 {
		return nil, nil
	}
	return &resp.MetricAlarms[0], nil
}

func compareJSON(got, want string) error {
	var gotValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		return fmt.Errorf("decode dashboard body: %w", err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		return fmt.Errorf("decode expected body: %w", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		return fmt.Errorf("dashboard body is %s, want %s", got, want)
	}
	return nil
}
