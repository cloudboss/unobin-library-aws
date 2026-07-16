package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	schedulerapi "github.com/aws/aws-sdk-go-v2/service/scheduler"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testScheduleARN = "arn:aws:scheduler:us-east-1:123456789012:" +
	"schedule/default/unobin-it-schedule"

func TestVerifyAppliedRecordsScheduleIdentity(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	creationDate := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	client := &fakeVerifierClient{
		output: scheduleOutput(
			creationDate,
			initialDescription,
			initialExpression,
			schedulertypes.ScheduleStateEnabled,
			initialPayload,
			3,
		),
	}

	err := verifyApplied(context.Background(), client)

	require.NoError(t, err)
	value, err := os.ReadFile(scheduleARNPath())
	require.NoError(t, err)
	assert.Equal(t, testScheduleARN, string(value))
	value, err = os.ReadFile(scheduleCreationDatePath())
	require.NoError(t, err)
	assert.Equal(t, creationDate.Format(time.RFC3339Nano), string(value))
}

func TestVerifyUpdatedRejectsReplacement(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	creationDate := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(
		scheduleARNPath(), []byte(testScheduleARN), 0o600))
	require.NoError(t, os.WriteFile(
		scheduleCreationDatePath(),
		[]byte(creationDate.Format(time.RFC3339Nano)),
		0o600,
	))
	output := scheduleOutput(
		creationDate,
		"",
		updatedExpression,
		schedulertypes.ScheduleStateDisabled,
		updatedPayload,
		0,
	)
	output.Arn = aws.String(
		"arn:aws:scheduler:us-east-1:123456789012:" +
			"schedule/default/unobin-it-schedule-replacement")
	client := &fakeVerifierClient{output: output}

	err := verifyUpdated(context.Background(), client)

	require.Error(t, err)
	assert.ErrorContains(t, err, "schedule ARN")
}

func TestVerifyUpdatedRejectsDifferentCreationDate(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	creationDate := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(
		scheduleARNPath(), []byte(testScheduleARN), 0o600))
	require.NoError(t, os.WriteFile(
		scheduleCreationDatePath(),
		[]byte(creationDate.Format(time.RFC3339Nano)),
		0o600,
	))
	client := &fakeVerifierClient{
		output: scheduleOutput(
			creationDate.Add(time.Minute),
			"",
			updatedExpression,
			schedulertypes.ScheduleStateDisabled,
			updatedPayload,
			0,
		),
	}

	err := verifyUpdated(context.Background(), client)

	require.Error(t, err)
	assert.ErrorContains(t, err, "creation date")
}

type fakeVerifierClient struct {
	output *schedulerapi.GetScheduleOutput
	err    error
}

func (c *fakeVerifierClient) GetSchedule(
	context.Context,
	*schedulerapi.GetScheduleInput,
	...func(*schedulerapi.Options),
) (*schedulerapi.GetScheduleOutput, error) {
	return c.output, c.err
}

func scheduleOutput(
	creationDate time.Time,
	description string,
	expression string,
	state schedulertypes.ScheduleState,
	payload string,
	retryAttempts int32,
) *schedulerapi.GetScheduleOutput {
	return &schedulerapi.GetScheduleOutput{
		ActionAfterCompletion: schedulertypes.ActionAfterCompletionNone,
		Arn:                   aws.String(testScheduleARN),
		CreationDate:          aws.Time(creationDate),
		Description:           aws.String(description),
		FlexibleTimeWindow: &schedulertypes.FlexibleTimeWindow{
			Mode: schedulertypes.FlexibleTimeWindowModeOff,
		},
		ScheduleExpression:         aws.String(expression),
		ScheduleExpressionTimezone: aws.String("UTC"),
		State:                      state,
		Target: &schedulertypes.Target{
			Arn: aws.String(
				"arn:aws:sqs:us-east-1:123456789012:unobin-it-scheduler-queue"),
			RoleArn: aws.String(
				"arn:aws:iam::123456789012:role/unobin-it-scheduler-role"),
			Input: aws.String(payload),
			RetryPolicy: &schedulertypes.RetryPolicy{
				MaximumEventAgeInSeconds: aws.Int32(3600),
				MaximumRetryAttempts:     aws.Int32(retryAttempts),
			},
		},
	}
}
