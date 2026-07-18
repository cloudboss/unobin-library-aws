package kinesis

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamUpdateOnDemandToProvisionedWaitsBeforeScaling(t *testing.T) {
	priorInput := validOnDemandStream()
	resource := validProvisionedStream(2)
	priorOutput := &StreamResourceOutput{
		ARN:               testStreamARN,
		Name:              "events",
		OpenShardCount:    1,
		StreamModeDetails: &StreamModeDetails{StreamMode: "ON_DEMAND"},
	}
	client := &fakeStreamClient{}

	_, err := resource.update(
		context.Background(),
		client,
		streamPrior(priorInput, priorOutput),
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"UpdateStreamMode",
		"DescribeStreamSummary",
		"UpdateShardCount",
		"DescribeStreamSummary",
		"DescribeStreamSummary",
	}, client.calls)
	require.Len(t, client.updateModeInputs, 1)
	assert.Equal(t, testStreamARN, aws.ToString(client.updateModeInputs[0].StreamARN))
	assert.Equal(t, awstypes.StreamModeProvisioned,
		client.updateModeInputs[0].StreamModeDetails.StreamMode)
	require.Len(t, client.updateShardInputs, 1)
	assert.Equal(t, testStreamARN, aws.ToString(client.updateShardInputs[0].StreamARN))
	assert.Equal(t, int32(2), aws.ToInt32(client.updateShardInputs[0].TargetShardCount))
	assert.Equal(t, awstypes.ScalingTypeUniformScaling,
		client.updateShardInputs[0].ScalingType)
}

func TestStreamUpdateTreatsOmittedAndExplicitProvisionedAsEquivalent(t *testing.T) {
	priorInput := validProvisionedStream(1)
	resource := validProvisionedStream(1)
	resource.StreamModeDetails = &StreamModeDetails{StreamMode: "PROVISIONED"}
	priorOutput := provisionedStreamOutput()
	client := &fakeStreamClient{}

	_, err := resource.update(
		context.Background(),
		client,
		streamPrior(priorInput, priorOutput),
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"DescribeLimits", "DescribeStreamSummary"}, client.calls)
	assert.Empty(t, client.updateModeInputs)
	assert.Empty(t, client.updateShardInputs)
}

func TestStreamUpdateTagOnlyAndNoOpRequests(t *testing.T) {
	t.Run("tag only", func(t *testing.T) {
		priorTags := map[string]string{"old": "value"}
		newTags := map[string]string{"new": "value"}
		priorInput := validProvisionedStream(1)
		priorInput.Tags = &priorTags
		resource := validProvisionedStream(1)
		resource.Tags = &newTags
		client := &fakeStreamClient{listTags: []*awssdk.ListTagsForStreamOutput{{
			Tags: []awstypes.Tag{{Key: aws.String("old"), Value: aws.String("value")}},
		}}}

		_, err := resource.update(
			context.Background(),
			client,
			streamPrior(priorInput, provisionedStreamOutput()),
			testStreamOptions(&fakeStreamClock{}),
		)
		require.NoError(t, err)
		assert.Equal(t, []string{
			"DescribeLimits",
			"ListTagsForStream",
			"RemoveTagsFromStream",
			"AddTagsToStream",
			"DescribeStreamSummary",
		}, client.calls)
		require.Len(t, client.listTagInputs, 1)
		assert.Equal(t, testStreamARN, aws.ToString(client.listTagInputs[0].StreamARN))
		require.Len(t, client.removeTagInputs, 1)
		assert.Equal(t, testStreamARN, aws.ToString(client.removeTagInputs[0].StreamARN))
		assert.Equal(t, []string{"old"}, client.removeTagInputs[0].TagKeys)
		require.Len(t, client.addTagInputs, 1)
		assert.Equal(t, testStreamARN, aws.ToString(client.addTagInputs[0].StreamARN))
		assert.Equal(t, map[string]string{"new": "value"}, client.addTagInputs[0].Tags)
		assert.Empty(t, client.updateModeInputs)
		assert.Empty(t, client.updateShardInputs)
	})

	t.Run("no-op", func(t *testing.T) {
		resource := validProvisionedStream(1)
		client := &fakeStreamClient{}

		_, err := resource.update(
			context.Background(),
			client,
			streamPrior(validProvisionedStream(1), provisionedStreamOutput()),
			testStreamOptions(&fakeStreamClock{}),
		)
		require.NoError(t, err)
		assert.Equal(t, []string{"DescribeLimits", "DescribeStreamSummary"}, client.calls)
		assert.Empty(t, client.updateModeInputs)
		assert.Empty(t, client.updateShardInputs)
	})
}

func TestStreamUpdateRetentionDecreaseAndUnchanged(t *testing.T) {
	tests := []struct {
		name       string
		priorHours int64
		hours      int64
		wantCalls  []string
	}{
		{
			name:       "decrease",
			priorHours: 48,
			hours:      24,
			wantCalls: []string{
				"DescribeLimits",
				"DecreaseStreamRetentionPeriod",
				"DescribeStreamSummary",
				"DescribeStreamSummary",
			},
		},
		{
			name:       "unchanged",
			priorHours: 24,
			hours:      24,
			wantCalls:  []string{"DescribeLimits", "DescribeStreamSummary"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			priorInput := validProvisionedStream(1)
			priorInput.RetentionPeriod = test.priorHours
			resource := validProvisionedStream(1)
			resource.RetentionPeriod = test.hours
			client := &fakeStreamClient{}

			_, err := resource.update(
				context.Background(),
				client,
				streamPrior(priorInput, provisionedStreamOutput()),
				testStreamOptions(&fakeStreamClock{}),
			)
			require.NoError(t, err)
			assert.Equal(t, test.wantCalls, client.calls)
			if test.priorHours == test.hours {
				assert.Empty(t, client.decreaseInputs)
				return
			}
			require.Len(t, client.decreaseInputs, 1)
			assert.Equal(t, testStreamARN,
				aws.ToString(client.decreaseInputs[0].StreamARN))
			assert.Equal(t, int32(test.hours),
				aws.ToInt32(client.decreaseInputs[0].RetentionPeriodHours))
		})
	}
}

func TestStreamUpdateClearsAllMetricsBeforeLaterMutation(t *testing.T) {
	priorMetrics := []string{"IncomingBytes", "ALL"}
	tests := []struct {
		name    string
		metrics *[]string
	}{
		{name: "nil"},
		{name: "empty", metrics: &[]string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			priorRecordSize := int64(1024)
			newRecordSize := int64(2048)
			priorInput := validProvisionedStream(1)
			priorInput.ShardLevelMetrics = &priorMetrics
			priorInput.MaxRecordSizeInKiB = &priorRecordSize
			resource := validProvisionedStream(1)
			resource.ShardLevelMetrics = test.metrics
			resource.MaxRecordSizeInKiB = &newRecordSize
			client := &fakeStreamClient{}

			_, err := resource.update(
				context.Background(),
				client,
				streamPrior(priorInput, provisionedStreamOutput()),
				testStreamOptions(&fakeStreamClock{}),
			)
			require.NoError(t, err)
			assert.Equal(t, []string{
				"DescribeLimits",
				"DisableEnhancedMonitoring",
				"DescribeStreamSummary",
				"UpdateMaxRecordSize",
				"DescribeStreamSummary",
				"DescribeStreamSummary",
			}, client.calls)
			require.Len(t, client.disableInputs, 1)
			assert.Equal(t, []awstypes.MetricsName{
				awstypes.MetricsNameAll,
				awstypes.MetricsNameIncomingBytes,
			}, client.disableInputs[0].ShardLevelMetrics)
			assert.Equal(t, testStreamARN,
				aws.ToString(client.disableInputs[0].StreamARN))
			assert.Empty(t, client.enableInputs)
		})
	}
}

func TestStreamUpdateOmittedMaxRecordSizeIsUnmanaged(t *testing.T) {
	configured := int64(2048)
	priorInput := validProvisionedStream(1)
	priorInput.MaxRecordSizeInKiB = &configured
	resource := validProvisionedStream(1)
	client := &fakeStreamClient{}

	_, err := resource.update(
		context.Background(),
		client,
		streamPrior(priorInput, provisionedStreamOutput()),
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"DescribeLimits", "DescribeStreamSummary"}, client.calls)
	assert.Empty(t, client.maxRecordInputs)
}

func TestStreamDeleteSendsConsumerDeletionFlagAndWaits(t *testing.T) {
	for _, enforce := range []bool{false, true} {
		t.Run(boolName(enforce), func(t *testing.T) {
			resource := validProvisionedStream(1)
			resource.EnforceConsumerDeletion = enforce
			client := &fakeStreamClient{}

			err := resource.delete(
				context.Background(),
				client,
				&StreamResourceOutput{Name: "events"},
				testStreamOptions(&fakeStreamClock{}),
			)
			require.NoError(t, err)
			assert.Equal(t, []string{"DeleteStream", "DescribeStreamSummary"}, client.calls)
			require.Len(t, client.deleteInputs, 1)
			assert.Equal(t, enforce,
				aws.ToBool(client.deleteInputs[0].EnforceConsumerDeletion))
		})
	}
}

func TestStreamDeletePropagatesServiceErrors(t *testing.T) {
	resourceInUse := &awstypes.ResourceInUseException{Message: aws.String("consumers remain")}
	generic := errors.New("service unavailable")
	for _, serviceErr := range []error{resourceInUse, generic} {
		t.Run(serviceErr.Error(), func(t *testing.T) {
			client := &fakeStreamClient{
				fail: map[string]error{"DeleteStream": serviceErr},
			}

			err := validProvisionedStream(1).delete(
				context.Background(),
				client,
				&StreamResourceOutput{Name: "events"},
				testStreamOptions(&fakeStreamClock{}),
			)
			require.Error(t, err)
			assert.ErrorIs(t, err, serviceErr)
			assert.Equal(t, []string{"DeleteStream"}, client.calls)
		})
	}
}

func TestStreamUpdateConsumerDeletionFlagDoesNotMutateStream(t *testing.T) {
	priorInput := validProvisionedStream(1)
	resource := validProvisionedStream(1)
	resource.EnforceConsumerDeletion = true
	client := &fakeStreamClient{}

	_, err := resource.update(
		context.Background(),
		client,
		streamPrior(priorInput, provisionedStreamOutput()),
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"DescribeLimits", "DescribeStreamSummary"}, client.calls)
}

func streamPrior(
	input StreamResource,
	output *StreamResourceOutput,
) runtime.Prior[StreamResource, *StreamResourceOutput] {
	return runtime.Prior[StreamResource, *StreamResourceOutput]{
		Inputs: input, Outputs: output, Observed: output,
	}
}

func provisionedStreamOutput() *StreamResourceOutput {
	return &StreamResourceOutput{
		ARN:               testStreamARN,
		Name:              "events",
		OpenShardCount:    1,
		StreamModeDetails: &StreamModeDetails{StreamMode: "PROVISIONED"},
	}
}

func boolName(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
