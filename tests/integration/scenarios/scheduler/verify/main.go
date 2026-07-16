package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	schedulerapi "github.com/aws/aws-sdk-go-v2/service/scheduler"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
)

const (
	scheduleName       = "unobin-it-schedule"
	initialDescription = "initial scheduler integration schedule"
	initialExpression  = "rate(1 day)"
	updatedExpression  = "rate(2 days)"
	initialPayload     = `{"version":"initial"}`
	updatedPayload     = `{"version":"updated"}`
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	configuration, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := schedulerapi.NewFromConfig(configuration)
	switch os.Getenv("VERIFY_PHASE") {
	case "applied":
		return verifyApplied(ctx, client)
	case "updated":
		return verifyUpdated(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return errors.New("VERIFY_PHASE must be applied, updated, or destroyed")
	}
}

type verifierClient interface {
	GetSchedule(
		context.Context,
		*schedulerapi.GetScheduleInput,
		...func(*schedulerapi.Options),
	) (*schedulerapi.GetScheduleOutput, error)
}

func verifyApplied(ctx context.Context, client verifierClient) error {
	output, err := verifyPresent(
		ctx,
		client,
		initialDescription,
		initialExpression,
		schedulertypes.ScheduleStateEnabled,
		initialPayload,
		3,
		nil,
	)
	if err != nil {
		return err
	}
	if err := os.WriteFile(scheduleARNPath(), []byte(aws.ToString(output.Arn)), 0o600); err != nil {
		return fmt.Errorf("record schedule ARN: %w", err)
	}
	created := []byte(output.CreationDate.UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(scheduleCreationDatePath(), created, 0o600); err != nil {
		return fmt.Errorf("record schedule creation date: %w", err)
	}
	return nil
}

func verifyUpdated(ctx context.Context, client verifierClient) error {
	arn, err := os.ReadFile(scheduleARNPath())
	if err != nil {
		return fmt.Errorf("read schedule ARN: %w", err)
	}
	creationDate, err := readScheduleCreationDate()
	if err != nil {
		return err
	}
	output, err := verifyPresent(
		ctx,
		client,
		"",
		updatedExpression,
		schedulertypes.ScheduleStateDisabled,
		updatedPayload,
		0,
		&creationDate,
	)
	if err != nil {
		return err
	}
	if aws.ToString(output.Arn) != string(arn) {
		return fmt.Errorf(
			"schedule ARN is %q, want %q", aws.ToString(output.Arn), string(arn))
	}
	return nil
}

func verifyPresent(
	ctx context.Context,
	client verifierClient,
	description string,
	expression string,
	state schedulertypes.ScheduleState,
	payload string,
	retryAttempts int32,
	expectedCreationDate *time.Time,
) (*schedulerapi.GetScheduleOutput, error) {
	output, err := client.GetSchedule(ctx, &schedulerapi.GetScheduleInput{
		GroupName: aws.String("default"),
		Name:      aws.String(scheduleName),
	})
	if err != nil {
		return nil, fmt.Errorf("get schedule %s: %w", scheduleName, err)
	}
	if output == nil || output.Arn == nil || output.CreationDate == nil {
		return nil, fmt.Errorf("schedule %s returned an incomplete result", scheduleName)
	}
	if expectedCreationDate != nil && !output.CreationDate.Equal(*expectedCreationDate) {
		return nil, fmt.Errorf(
			"schedule creation date is %s, want %s",
			output.CreationDate.UTC().Format(time.RFC3339Nano),
			expectedCreationDate.UTC().Format(time.RFC3339Nano),
		)
	}
	if !strings.HasSuffix(
		aws.ToString(output.Arn), ":schedule/default/"+scheduleName) {
		return nil, fmt.Errorf("schedule ARN is %q", aws.ToString(output.Arn))
	}
	if aws.ToString(output.Description) != description {
		return nil, fmt.Errorf(
			"schedule description is %q, want %q",
			aws.ToString(output.Description), description)
	}
	if aws.ToString(output.ScheduleExpression) != expression {
		return nil, fmt.Errorf(
			"schedule expression is %q, want %q",
			aws.ToString(output.ScheduleExpression), expression)
	}
	if aws.ToString(output.ScheduleExpressionTimezone) != "UTC" {
		return nil, fmt.Errorf(
			"schedule timezone is %q, want UTC",
			aws.ToString(output.ScheduleExpressionTimezone))
	}
	if output.State != state {
		return nil, fmt.Errorf("schedule state is %q, want %q", output.State, state)
	}
	if output.ActionAfterCompletion != schedulertypes.ActionAfterCompletionNone {
		return nil, fmt.Errorf(
			"schedule action after completion is %q, want NONE",
			output.ActionAfterCompletion)
	}
	if output.FlexibleTimeWindow == nil ||
		output.FlexibleTimeWindow.Mode != schedulertypes.FlexibleTimeWindowModeOff {
		return nil, errors.New("schedule flexible time window is not OFF")
	}
	if output.Target == nil {
		return nil, errors.New("schedule target is missing")
	}
	if !strings.HasSuffix(
		aws.ToString(output.Target.Arn), ":unobin-it-scheduler-queue") {
		return nil, fmt.Errorf("schedule target ARN is %q", aws.ToString(output.Target.Arn))
	}
	if !strings.HasSuffix(
		aws.ToString(output.Target.RoleArn), ":role/unobin-it-scheduler-role") {
		return nil, fmt.Errorf(
			"schedule target role ARN is %q", aws.ToString(output.Target.RoleArn))
	}
	if aws.ToString(output.Target.Input) != payload {
		return nil, fmt.Errorf(
			"schedule target input is %q, want %q",
			aws.ToString(output.Target.Input), payload)
	}
	if output.Target.RetryPolicy == nil ||
		aws.ToInt32(output.Target.RetryPolicy.MaximumEventAgeInSeconds) != 3600 ||
		aws.ToInt32(output.Target.RetryPolicy.MaximumRetryAttempts) != retryAttempts {
		return nil, errors.New("schedule target retry policy does not match")
	}
	fmt.Printf("ok: schedule %s matches %s\n", scheduleName, expression)
	return output, nil
}

func verifyDestroyed(ctx context.Context, client verifierClient) error {
	_, err := client.GetSchedule(ctx, &schedulerapi.GetScheduleInput{
		GroupName: aws.String("default"),
		Name:      aws.String(scheduleName),
	})
	if err == nil {
		return fmt.Errorf("schedule %s still exists", scheduleName)
	}
	var notFound *schedulertypes.ResourceNotFoundException
	if !errors.As(err, &notFound) {
		return err
	}
	fmt.Printf("ok: schedule %s is gone\n", scheduleName)
	return nil
}

func scheduleARNPath() string {
	return filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "schedule-arn")
}

func scheduleCreationDatePath() string {
	return filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "schedule-creation-date")
}

func readScheduleCreationDate() (time.Time, error) {
	value, err := os.ReadFile(scheduleCreationDatePath())
	if err != nil {
		return time.Time{}, fmt.Errorf("read schedule creation date: %w", err)
	}
	created, err := time.Parse(time.RFC3339Nano, string(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse schedule creation date: %w", err)
	}
	return created, nil
}
