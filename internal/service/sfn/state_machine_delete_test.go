package sfn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateMachineDeletePropagatesCallError(t *testing.T) {
	client := &fakeStateMachineClient{
		deleteErrors: []error{&sfntypes.StateMachineDoesNotExist{}},
	}
	resource := baseStateMachine()

	err := resource.delete(context.Background(), client,
		&StateMachineResourceOutput{ARN: testStateMachineARN},
		stateMachineOptions(&fakeStateMachineClock{}))
	require.Error(t, err)
	assert.Empty(t, client.describeInputs)
	require.Len(t, client.deleteInputs, 1)
}

func TestStateMachineDeleteAlreadyAbsentAfterCall(t *testing.T) {
	client := &fakeStateMachineClient{
		describeErrors: []error{&sfntypes.StateMachineDoesNotExist{}},
	}
	resource := baseStateMachine()

	err := resource.delete(context.Background(), client,
		&StateMachineResourceOutput{ARN: testStateMachineARN},
		stateMachineOptions(&fakeStateMachineClock{}))
	require.NoError(t, err)
	assert.Equal(t, []string{"delete", "describe"}, client.calls)
	require.Len(t, client.describeInputs, 1)
	assert.Equal(t, sfntypes.IncludedDataMetadataOnly,
		client.describeInputs[0].IncludedData)
}

func TestStateMachineDeleteWaitsThroughAcceptedStatuses(t *testing.T) {
	active := stateMachineDescription(testDefinition, testRoleARN, "revision-1")
	deleting := stateMachineDescription(testDefinition, testRoleARN, "revision-1")
	deleting.Status = sfntypes.StateMachineStatusDeleting
	client := &fakeStateMachineClient{
		describeOutputs: []*sfn.DescribeStateMachineOutput{active, deleting},
		describeErrors:  []error{nil, nil, &sfntypes.StateMachineDoesNotExist{}},
	}
	clock := &fakeStateMachineClock{}
	resource := baseStateMachine()

	err := resource.delete(context.Background(), client,
		&StateMachineResourceOutput{ARN: testStateMachineARN}, stateMachineOptions(clock))
	require.NoError(t, err)
	assert.Equal(t, []string{"delete", "describe", "describe", "describe"}, client.calls)
	assert.Equal(t, []time.Duration{200 * time.Millisecond, 400 * time.Millisecond},
		clock.sleeps)
	for _, input := range client.describeInputs {
		assert.Equal(t, sfntypes.IncludedDataMetadataOnly, input.IncludedData)
	}
}

func TestStateMachineDeleteRejectsUnexpectedStatus(t *testing.T) {
	out := stateMachineDescription(testDefinition, testRoleARN, "revision-1")
	out.Status = sfntypes.StateMachineStatus("FAILED")
	client := &fakeStateMachineClient{
		describeOutputs: []*sfn.DescribeStateMachineOutput{out},
	}
	resource := baseStateMachine()

	err := resource.delete(context.Background(), client,
		&StateMachineResourceOutput{ARN: testStateMachineARN},
		stateMachineOptions(&fakeStateMachineClock{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestStateMachineDeleteStopsOnDescribeError(t *testing.T) {
	client := &fakeStateMachineClient{describeErrors: []error{errors.New("describe failed")}}
	resource := baseStateMachine()

	err := resource.delete(context.Background(), client,
		&StateMachineResourceOutput{ARN: testStateMachineARN},
		stateMachineOptions(&fakeStateMachineClock{}))
	require.Error(t, err)
	assert.ErrorContains(t, err, "describe failed")
	require.Len(t, client.describeInputs, 1)
}

func TestStateMachineDeleteTimeoutAndCancellation(t *testing.T) {
	resource := baseStateMachine()
	prior := &StateMachineResourceOutput{ARN: testStateMachineARN}

	t.Run("timeout", func(t *testing.T) {
		client := &fakeStateMachineClient{}
		clock := &fakeStateMachineClock{}
		options := stateMachineOptions(clock)
		options.deleteWindow = 3 * time.Second

		err := resource.delete(context.Background(), client, prior, options)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timed out")
		assert.Equal(t, []time.Duration{
			200 * time.Millisecond,
			400 * time.Millisecond,
			800 * time.Millisecond,
			1600 * time.Millisecond,
		}, clock.sleeps)
	})

	t.Run("cancellation", func(t *testing.T) {
		client := &fakeStateMachineClient{}
		clock := &fakeStateMachineClock{sleepErr: context.Canceled}

		err := resource.delete(context.Background(), client, prior,
			stateMachineOptions(clock))
		assert.ErrorIs(t, err, context.Canceled)
		require.Len(t, client.describeInputs, 1)
	})
}

func TestStateMachineDeleteNilDescribeResponseIsAbsent(t *testing.T) {
	client := &fakeStateMachineClient{describeNil: true}
	resource := baseStateMachine()

	err := resource.delete(context.Background(), client,
		&StateMachineResourceOutput{ARN: testStateMachineARN},
		stateMachineOptions(&fakeStateMachineClock{}))
	require.NoError(t, err)
	assert.Equal(t, []string{"delete", "describe"}, client.calls)
}
