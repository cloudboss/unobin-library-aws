// verify checks the CloudWatch metric alarm the scenario applied against the
// phase named in the VERIFY_PHASE environment variable. The alarm has a stable
// name, so applied requires it present with the threshold the first apply set,
// and destroyed requires it gone. It only reads cloud state; tearing the alarm
// down is the destroy plan's job.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

const (
	alarmName     = "unobin-it-alarm"
	wantThreshold = 80.0 // the threshold the first apply set
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := cloudwatch.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *cloudwatch.Client) error {
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

func verifyDestroyed(ctx context.Context, client *cloudwatch.Client) error {
	alarm, err := findAlarm(ctx, client)
	if err != nil {
		return err
	}
	if alarm != nil {
		return fmt.Errorf("metric alarm %s still exists", alarmName)
	}
	fmt.Printf("ok: metric alarm %s gone\n", alarmName)
	return nil
}

// findAlarm returns the scenario's metric alarm, or nil when DescribeAlarms
// reports no alarm by that name (the not-found signal CloudWatch gives, an empty
// result rather than an error).
func findAlarm(
	ctx context.Context, client *cloudwatch.Client,
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
