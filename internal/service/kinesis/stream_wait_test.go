package kinesis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitStreamCreatedToleratesInitialLag(t *testing.T) {
	responses := []struct {
		status awstypes.StreamStatus
		err    error
	}{
		{err: streamNotFoundError()},
		{err: streamNotFoundError()},
		{status: awstypes.StreamStatusCreating},
		{status: awstypes.StreamStatusActive},
	}
	client := &fakeStreamClient{}
	client.describe = func(
		_ *awssdk.DescribeStreamSummaryInput,
	) (*awssdk.DescribeStreamSummaryOutput, error) {
		response := responses[0]
		responses = responses[1:]
		if response.err != nil {
			return nil, response.err
		}
		return streamDescription(response.status), nil
	}
	clock := &fakeStreamClock{}

	summary, err := waitStreamCreated(
		context.Background(),
		client,
		"events",
		testStreamOptions(clock),
	)
	require.NoError(t, err)
	assert.Equal(t, awstypes.StreamStatusActive, summary.StreamStatus)
	assert.Equal(t, []time.Duration{
		time.Second,
		time.Second,
		2 * time.Second,
		4 * time.Second,
	}, clock.sleeps)
}

func TestWaitStreamCreatedResetsConsecutiveNotFoundCount(t *testing.T) {
	statuses := make([]awstypes.StreamStatus, 0, 42)
	missing := make(map[int]bool, 40)
	for index := range 20 {
		missing[index] = true
	}
	statuses = append(statuses, make([]awstypes.StreamStatus, 20)...)
	statuses = append(statuses, awstypes.StreamStatusCreating)
	for index := 21; index < 41; index++ {
		missing[index] = true
	}
	statuses = append(statuses, make([]awstypes.StreamStatus, 20)...)
	statuses = append(statuses, awstypes.StreamStatusActive)
	call := 0
	client := &fakeStreamClient{describe: func(
		_ *awssdk.DescribeStreamSummaryInput,
	) (*awssdk.DescribeStreamSummaryOutput, error) {
		index := call
		call++
		if missing[index] {
			return nil, streamNotFoundError()
		}
		return streamDescription(statuses[index]), nil
	}}
	options := testStreamOptions(&fakeStreamClock{})
	options.createTimeout = 10 * time.Minute

	_, err := waitStreamCreated(context.Background(), client, "events", options)
	require.NoError(t, err)
	assert.Equal(t, 42, call)
}

func TestWaitStreamCreatedRejectsTwentyFirstConsecutiveAbsence(t *testing.T) {
	for _, afterVisible := range []bool{false, true} {
		t.Run(fmt.Sprintf("after-visible-%t", afterVisible), func(t *testing.T) {
			call := 0
			client := &fakeStreamClient{describe: func(
				_ *awssdk.DescribeStreamSummaryInput,
			) (*awssdk.DescribeStreamSummaryOutput, error) {
				call++
				if afterVisible && call == 1 {
					return streamDescription(awstypes.StreamStatusCreating), nil
				}
				return nil, streamNotFoundError()
			}}
			options := testStreamOptions(&fakeStreamClock{})
			options.createTimeout = 10 * time.Minute

			_, err := waitStreamCreated(context.Background(), client, "events", options)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not found after 20 checks")
			if afterVisible {
				assert.Equal(t, 22, call)
			} else {
				assert.Equal(t, 21, call)
			}
		})
	}
}

func TestWaitStreamCreatedUsesProductionBackoff(t *testing.T) {
	call := 0
	client := &fakeStreamClient{describe: func(
		_ *awssdk.DescribeStreamSummaryInput,
	) (*awssdk.DescribeStreamSummaryOutput, error) {
		call++
		if call < 4 {
			return streamDescription(awstypes.StreamStatusCreating), nil
		}
		return streamDescription(awstypes.StreamStatusActive), nil
	}}
	clock := &fakeStreamClock{}
	options := defaultStreamOperationOptions()
	options.clock = clock

	_, err := waitStreamCreated(context.Background(), client, "events", options)
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{
		10 * time.Second,
		3 * time.Second,
		6 * time.Second,
		10 * time.Second,
	}, clock.sleeps)
}

func TestWaitStreamCreatedRejectsUnexpectedStatus(t *testing.T) {
	client := &fakeStreamClient{describe: func(
		_ *awssdk.DescribeStreamSummaryInput,
	) (*awssdk.DescribeStreamSummaryOutput, error) {
		return streamDescription(awstypes.StreamStatusUpdating), nil
	}}

	_, err := waitStreamCreated(
		context.Background(),
		client,
		"events",
		testStreamOptions(&fakeStreamClock{}),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status UPDATING")
}

func TestWaitStreamUpdatedTimesOut(t *testing.T) {
	client := &fakeStreamClient{describe: func(
		_ *awssdk.DescribeStreamSummaryInput,
	) (*awssdk.DescribeStreamSummaryOutput, error) {
		return streamDescription(awstypes.StreamStatusUpdating), nil
	}}
	clock := &fakeStreamClock{}
	options := testStreamOptions(clock)
	options.createTimeout = 1500 * time.Millisecond
	options.updateTimeout = 2500 * time.Millisecond

	_, err := waitStreamUpdated(context.Background(), client, "arn:stream", options)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out after 2.5s")
}

func TestWaitStreamDeletedFinishesAtAbsence(t *testing.T) {
	calls := 0
	client := &fakeStreamClient{describe: func(
		_ *awssdk.DescribeStreamSummaryInput,
	) (*awssdk.DescribeStreamSummaryOutput, error) {
		calls++
		if calls == 1 {
			return streamDescription(awstypes.StreamStatusDeleting), nil
		}
		return nil, streamNotFoundError()
	}}

	err := waitStreamDeleted(
		context.Background(),
		client,
		"events",
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
}

func TestWaitStreamDeletedUsesProductionBackoff(t *testing.T) {
	call := 0
	client := &fakeStreamClient{describe: func(
		_ *awssdk.DescribeStreamSummaryInput,
	) (*awssdk.DescribeStreamSummaryOutput, error) {
		call++
		if call < 4 {
			return streamDescription(awstypes.StreamStatusDeleting), nil
		}
		return nil, streamNotFoundError()
	}}
	clock := &fakeStreamClock{}
	options := defaultStreamOperationOptions()
	options.clock = clock

	err := waitStreamDeleted(context.Background(), client, "events", options)
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{
		10 * time.Second,
		3 * time.Second,
		6 * time.Second,
		10 * time.Second,
	}, clock.sleeps)
}

func TestWaitStreamHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitStreamCreated(
		ctx,
		&fakeStreamClient{},
		"events",
		testStreamOptions(&fakeStreamClock{}),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestDescribeStreamMapsMissingResponses(t *testing.T) {
	for _, client := range []*fakeStreamClient{
		{describe: func(
			_ *awssdk.DescribeStreamSummaryInput,
		) (*awssdk.DescribeStreamSummaryOutput, error) {
			return nil, streamNotFoundError()
		}},
		{describe: func(
			_ *awssdk.DescribeStreamSummaryInput,
		) (*awssdk.DescribeStreamSummaryOutput, error) {
			return &awssdk.DescribeStreamSummaryOutput{}, nil
		}},
	} {
		_, err := describeStream(
			context.Background(),
			client,
			streamAddress{name: "events"},
		)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	}
}

func activeStreamDescription() *awssdk.DescribeStreamSummaryOutput {
	return streamDescription(awstypes.StreamStatusActive)
}

func streamDescription(
	status awstypes.StreamStatus,
) *awssdk.DescribeStreamSummaryOutput {
	created := time.Date(2026, time.July, 18, 12, 0, 0, 0, time.UTC)
	return &awssdk.DescribeStreamSummaryOutput{
		StreamDescriptionSummary: &awstypes.StreamDescriptionSummary{
			StreamARN:               aws.String("arn:aws:kinesis:us-east-1:123:stream/events"),
			StreamName:              aws.String("events"),
			OpenShardCount:          aws.Int32(1),
			StreamCreationTimestamp: &created,
			StreamStatus:            status,
			EncryptionType:          awstypes.EncryptionTypeNone,
			StreamModeDetails: &awstypes.StreamModeDetails{
				StreamMode: awstypes.StreamModeProvisioned,
			},
		},
	}
}

func streamNotFoundError() error {
	return &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
}
