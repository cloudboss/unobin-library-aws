package kinesis

import (
	"context"
	"errors"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamCreateCleansUpEveryPostAcceptanceFailure(t *testing.T) {
	tests := []struct {
		name            string
		failedOperation string
		failedDescribe  int
	}{
		{name: "initial waiter", failedDescribe: 1},
		{name: "retention call", failedOperation: "IncreaseStreamRetentionPeriod"},
		{name: "retention waiter", failedDescribe: 2},
		{name: "monitoring call", failedOperation: "EnableEnhancedMonitoring"},
		{name: "monitoring waiter", failedDescribe: 3},
		{name: "encryption call", failedOperation: "StartStreamEncryption"},
		{name: "encryption waiter", failedDescribe: 4},
		{name: "final read", failedDescribe: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalErr := errors.New("injected create failure")
			metrics := []string{"IncomingBytes"}
			key := "key"
			resource := validProvisionedStream(1)
			resource.ShardLevelMetrics = &metrics
			resource.EncryptionType = "KMS"
			resource.KMSKeyID = &key
			describeCall := 0
			client := &fakeStreamClient{describe: func(
				_ *awssdk.DescribeStreamSummaryInput,
			) (*awssdk.DescribeStreamSummaryOutput, error) {
				describeCall++
				if describeCall == test.failedDescribe {
					return nil, originalErr
				}
				return activeStreamDescription(), nil
			}}
			if test.failedOperation != "" {
				client.fail = map[string]error{test.failedOperation: originalErr}
			}

			_, err := resource.create(
				context.Background(),
				client,
				testStreamOptions(&fakeStreamClock{}),
			)
			require.Error(t, err)
			assert.ErrorIs(t, err, originalErr)
			require.GreaterOrEqual(t, len(client.calls), 2)
			assert.Equal(t, []string{"DeleteStream", "DescribeStreamSummary"},
				client.calls[len(client.calls)-2:])
		})
	}
}

func TestStreamCreateCleansUpActiveStreamWithoutARN(t *testing.T) {
	client := &fakeStreamClient{describe: func(
		_ *awssdk.DescribeStreamSummaryInput,
	) (*awssdk.DescribeStreamSummaryOutput, error) {
		output := activeStreamDescription()
		output.StreamDescriptionSummary.StreamARN = nil
		return output, nil
	}}

	_, err := validProvisionedStream(1).create(
		context.Background(),
		client,
		testStreamOptions(&fakeStreamClock{}),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "active stream has no ARN")
	require.GreaterOrEqual(t, len(client.calls), 2)
	assert.Equal(t, []string{"DeleteStream", "DescribeStreamSummary"},
		client.calls[len(client.calls)-2:])
}

func TestStreamCreateJoinsOriginalAndCleanupErrors(t *testing.T) {
	originalErr := errors.New("retention failed")
	cleanupErr := errors.New("cleanup failed")
	client := &fakeStreamClient{fail: map[string]error{
		"IncreaseStreamRetentionPeriod": originalErr,
		"DeleteStream":                  cleanupErr,
	}}

	_, err := validProvisionedStream(1).create(
		context.Background(),
		client,
		testStreamOptions(&fakeStreamClock{}),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, originalErr)
	assert.ErrorIs(t, err, cleanupErr)
	assert.Equal(t, 1, countCall(client.calls, "DeleteStream"))
}
