package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	initialStateMachineARN = "arn:aws:states:us-east-1:123456789012:" +
		"stateMachine:unobin-it-state-machine"
	replacementStateMachineARN = "arn:aws:states:us-east-1:123456789012:" +
		"stateMachine:unobin-it-state-machine-replacement"
)

func TestVerifyAppliedRecordsStateMachineARN(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	creationDate := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	client := &fakeVerifierClient{
		stateMachineARN: initialStateMachineARN,
		creationDate:    creationDate,
		definition:      initialDefinition,
		tracingEnabled:  true,
		tagValue:        "initial",
	}

	err := verifyApplied(context.Background(), client)
	require.NoError(t, err)
	data, err := os.ReadFile(stateMachineARNPath())
	require.NoError(t, err)
	assert.Equal(t, initialStateMachineARN, string(data))
	data, err = os.ReadFile(stateMachineCreationDatePath())
	require.NoError(t, err)
	assert.Equal(t, creationDate.Format(time.RFC3339Nano), string(data))
}

func TestVerifyUpdatedRejectsReplacementWithoutOverwritingARN(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	creationDate := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(
		stateMachineARNPath(), []byte(initialStateMachineARN), 0o600))
	require.NoError(t, os.WriteFile(
		stateMachineCreationDatePath(),
		[]byte(creationDate.Format(time.RFC3339Nano)),
		0o600,
	))
	client := &fakeVerifierClient{stateMachineARN: replacementStateMachineARN}

	err := verifyUpdated(context.Background(), client)
	require.Error(t, err)
	assert.ErrorContains(t, err, replacementStateMachineARN)
	assert.Zero(t, client.describeCalls)
	assert.Zero(t, client.listTagsCalls)
	data, readErr := os.ReadFile(stateMachineARNPath())
	require.NoError(t, readErr)
	assert.Equal(t, initialStateMachineARN, string(data))
}

func TestVerifyUpdatedRejectsSameARNWithDifferentCreationDate(t *testing.T) {
	buildDir := t.TempDir()
	t.Setenv("VERIFY_BUILD_DIR", buildDir)
	appliedCreationDate := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(
		stateMachineARNPath(), []byte(initialStateMachineARN), 0o600))
	require.NoError(t, os.WriteFile(
		stateMachineCreationDatePath(),
		[]byte(appliedCreationDate.Format(time.RFC3339Nano)),
		0o600,
	))
	client := &fakeVerifierClient{
		stateMachineARN: initialStateMachineARN,
		creationDate:    appliedCreationDate.Add(time.Minute),
		definition:      updatedDefinition,
		tagValue:        "updated",
	}

	err := verifyUpdated(context.Background(), client)
	require.Error(t, err)
	assert.ErrorContains(t, err, "creation date")
	assert.Equal(t, 1, client.describeCalls)
	assert.Zero(t, client.listTagsCalls)
}

type fakeVerifierClient struct {
	stateMachineARN string
	creationDate    time.Time
	definition      string
	tracingEnabled  bool
	tagValue        string
	describeCalls   int
	listTagsCalls   int
}

func (c *fakeVerifierClient) ListStateMachines(
	context.Context,
	*sfn.ListStateMachinesInput,
	...func(*sfn.Options),
) (*sfn.ListStateMachinesOutput, error) {
	return &sfn.ListStateMachinesOutput{
		StateMachines: []sfntypes.StateMachineListItem{{
			Name:            aws.String(stateMachineName),
			StateMachineArn: aws.String(c.stateMachineARN),
		}},
	}, nil
}

func (c *fakeVerifierClient) DescribeStateMachine(
	context.Context,
	*sfn.DescribeStateMachineInput,
	...func(*sfn.Options),
) (*sfn.DescribeStateMachineOutput, error) {
	c.describeCalls++
	return &sfn.DescribeStateMachineOutput{
		CreationDate: aws.Time(c.creationDate),
		Definition:   aws.String(c.definition),
		RoleArn: aws.String(
			"arn:aws:iam::123456789012:role/unobin-it-sfn-role"),
		TracingConfiguration: &sfntypes.TracingConfiguration{
			Enabled: c.tracingEnabled,
		},
		Type: sfntypes.StateMachineTypeStandard,
	}, nil
}

func (c *fakeVerifierClient) ListTagsForResource(
	context.Context,
	*sfn.ListTagsForResourceInput,
	...func(*sfn.Options),
) (*sfn.ListTagsForResourceOutput, error) {
	c.listTagsCalls++
	return &sfn.ListTagsForResourceOutput{
		Tags: []sfntypes.Tag{{
			Key:   aws.String("unobin"),
			Value: aws.String(c.tagValue),
		}},
	}, nil
}
