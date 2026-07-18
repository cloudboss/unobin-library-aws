package kinesis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testStreamARN = "arn:aws:kinesis:us-east-1:123:stream/events"

func TestStreamCreateRunsFollowUpsInOrder(t *testing.T) {
	metrics := []string{"IncomingBytes", "ALL"}
	tags := map[string]string{"env": "test", "aws:owner": "system"}
	key := "alias/aws/kinesis"
	resource := validProvisionedStream(2)
	resource.RetentionPeriod = 48
	resource.ShardLevelMetrics = &metrics
	resource.Tags = &tags
	resource.EncryptionType = "KMS"
	resource.KMSKeyID = &key
	client := &fakeStreamClient{limits: streamLimitOutput(1, 10, 0, 10)}

	output, err := resource.create(
		context.Background(),
		client,
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, testStreamARN, output.ARN)
	assert.Equal(t, []string{
		"DescribeLimits",
		"CreateStream",
		"DescribeStreamSummary",
		"IncreaseStreamRetentionPeriod",
		"DescribeStreamSummary",
		"EnableEnhancedMonitoring",
		"DescribeStreamSummary",
		"StartStreamEncryption",
		"DescribeStreamSummary",
		"DescribeStreamSummary",
	}, client.calls)
	require.Len(t, client.createInputs, 1)
	assert.Equal(t, map[string]string{"env": "test"}, client.createInputs[0].Tags)
	require.Len(t, client.increaseInputs, 1)
	assert.Equal(t, int32(48), aws.ToInt32(client.increaseInputs[0].RetentionPeriodHours))
	require.Len(t, client.enableInputs, 1)
	assert.Equal(t, []awstypes.MetricsName{
		awstypes.MetricsNameAll,
		awstypes.MetricsNameIncomingBytes,
	}, client.enableInputs[0].ShardLevelMetrics)
	require.Len(t, client.startEncryptionInputs, 1)
	assert.Equal(t, key, aws.ToString(client.startEncryptionInputs[0].KeyId))
}

func TestStreamCreateCleansUpOnlyAfterAcceptedCreate(t *testing.T) {
	resource := validProvisionedStream(1)
	t.Run("follow-up failure", func(t *testing.T) {
		client := &fakeStreamClient{fail: map[string]error{
			"IncreaseStreamRetentionPeriod": errors.New("retention failed"),
		}}
		_, err := resource.create(
			context.Background(),
			client,
			testStreamOptions(&fakeStreamClock{}),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "retention failed")
		assert.Contains(t, client.calls, "DeleteStream")
		require.Len(t, client.deleteInputs, 1)
		assert.Equal(t, "events", aws.ToString(client.deleteInputs[0].StreamName))
	})

	t.Run("create API failure", func(t *testing.T) {
		client := &fakeStreamClient{fail: map[string]error{
			"CreateStream": errors.New("create failed"),
		}}
		_, err := resource.create(
			context.Background(),
			client,
			testStreamOptions(&fakeStreamClock{}),
		)
		require.Error(t, err)
		assert.NotContains(t, client.calls, "DeleteStream")
	})
}

func TestStreamCreateUsesCreateTimeoutForEveryWait(t *testing.T) {
	key := "key"
	metrics := []string{"IncomingBytes"}
	resource := validProvisionedStream(1)
	resource.EncryptionType = "KMS"
	resource.KMSKeyID = &key
	resource.ShardLevelMetrics = &metrics
	tests := []struct {
		name         string
		activeBefore int
	}{
		{name: "initial activation", activeBefore: 0},
		{name: "retention", activeBefore: 1},
		{name: "monitoring", activeBefore: 2},
		{name: "encryption", activeBefore: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			describeCall := 0
			client := &fakeStreamClient{describe: func(
				_ *awssdk.DescribeStreamSummaryInput,
			) (*awssdk.DescribeStreamSummaryOutput, error) {
				describeCall++
				if describeCall <= test.activeBefore {
					return activeStreamDescription(), nil
				}
				status := awstypes.StreamStatusUpdating
				if test.activeBefore == 0 {
					status = awstypes.StreamStatusCreating
				}
				return streamDescription(status), nil
			}}
			options := testStreamOptions(&fakeStreamClock{})
			options.createTimeout = 2500 * time.Millisecond
			options.updateTimeout = 20 * time.Second

			_, err := resource.create(context.Background(), client, options)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "timed out after 2.5s")
			assert.Contains(t, client.calls, "DeleteStream")
		})
	}
}

func TestStreamUpdateOrdersIndependentMutations(t *testing.T) {
	oldMetrics := []string{"IncomingBytes"}
	newMetrics := []string{"OutgoingBytes"}
	oldTags := map[string]string{"change": "old", "remove": "yes"}
	newTags := map[string]string{"add": "yes", "change": "new"}
	oldKey := "key-old"
	newKey := "key-new"
	oldRecord := int64(1024)
	newRecord := int64(2048)
	priorInput := validProvisionedStream(1)
	priorInput.RetentionPeriod = 24
	priorInput.ShardLevelMetrics = &oldMetrics
	priorInput.Tags = &oldTags
	priorInput.EncryptionType = "KMS"
	priorInput.KMSKeyID = &oldKey
	priorInput.MaxRecordSizeInKiB = &oldRecord
	resource := validProvisionedStream(2)
	resource.RetentionPeriod = 48
	resource.ShardLevelMetrics = &newMetrics
	resource.Tags = &newTags
	resource.EncryptionType = "KMS"
	resource.KMSKeyID = &newKey
	resource.MaxRecordSizeInKiB = &newRecord
	priorOutput := &StreamResourceOutput{
		ARN:               testStreamARN,
		Name:              "events",
		OpenShardCount:    1,
		StreamModeDetails: &StreamModeDetails{StreamMode: "PROVISIONED"},
	}
	client := &fakeStreamClient{
		limits: streamLimitOutput(1, 10, 0, 10),
		listTags: []*awssdk.ListTagsForStreamOutput{{
			Tags: []awstypes.Tag{
				{Key: aws.String("change"), Value: aws.String("old")},
				{Key: aws.String("remove"), Value: aws.String("yes")},
			},
		}},
	}

	output, err := resource.update(
		context.Background(),
		client,
		runtime.Prior[StreamResource, *StreamResourceOutput]{
			Inputs: priorInput, Outputs: priorOutput, Observed: priorOutput,
		},
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	assert.Equal(t, testStreamARN, output.ARN)
	assert.Equal(t, []string{
		"DescribeLimits",
		"ListTagsForStream",
		"RemoveTagsFromStream",
		"AddTagsToStream",
		"UpdateShardCount",
		"DescribeStreamSummary",
		"IncreaseStreamRetentionPeriod",
		"DescribeStreamSummary",
		"DisableEnhancedMonitoring",
		"DescribeStreamSummary",
		"EnableEnhancedMonitoring",
		"DescribeStreamSummary",
		"StartStreamEncryption",
		"DescribeStreamSummary",
		"UpdateMaxRecordSize",
		"DescribeStreamSummary",
		"DescribeStreamSummary",
	}, client.calls)
	require.Len(t, client.updateShardInputs, 1)
	assert.Equal(t, int32(2), aws.ToInt32(client.updateShardInputs[0].TargetShardCount))
	assert.Equal(t, awstypes.ScalingTypeUniformScaling,
		client.updateShardInputs[0].ScalingType)
	assert.Equal(t, []awstypes.MetricsName{awstypes.MetricsNameIncomingBytes},
		client.disableInputs[0].ShardLevelMetrics)
	assert.Equal(t, []awstypes.MetricsName{awstypes.MetricsNameOutgoingBytes},
		client.enableInputs[0].ShardLevelMetrics)
	assert.Equal(t, newKey, aws.ToString(client.startEncryptionInputs[0].KeyId))
	assert.Equal(t, int32(2048), aws.ToInt32(client.maxRecordInputs[0].MaxRecordSizeInKiB))
}

func TestStreamUpdateModeAndWarmThroughput(t *testing.T) {
	priorInput := validProvisionedStream(1)
	warm := int64(20)
	resource := validOnDemandStream()
	resource.WarmThroughputMiBps = &warm
	priorOutput := &StreamResourceOutput{
		ARN:               testStreamARN,
		Name:              "events",
		OpenShardCount:    1,
		StreamModeDetails: &StreamModeDetails{StreamMode: "PROVISIONED"},
	}
	client := &fakeStreamClient{}

	_, err := resource.update(
		context.Background(),
		client,
		runtime.Prior[StreamResource, *StreamResourceOutput]{
			Inputs: priorInput, Outputs: priorOutput, Observed: priorOutput,
		},
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"UpdateStreamMode",
		"DescribeStreamSummary",
		"UpdateStreamWarmThroughput",
		"DescribeStreamSummary",
		"DescribeStreamSummary",
	}, client.calls)
	require.Len(t, client.updateModeInputs, 1)
	assert.Equal(t, awstypes.StreamModeOnDemand,
		client.updateModeInputs[0].StreamModeDetails.StreamMode)
	assert.Empty(t, client.updateShardInputs)
	assert.Equal(t, int32(20), aws.ToInt32(client.warmInputs[0].WarmThroughputMiBps))
}

func TestStreamUpdateClearsWarmThroughputWithZero(t *testing.T) {
	oldWarm := int64(20)
	priorInput := validOnDemandStream()
	priorInput.WarmThroughputMiBps = &oldWarm
	resource := validOnDemandStream()
	priorOutput := &StreamResourceOutput{
		ARN:               testStreamARN,
		Name:              "events",
		StreamModeDetails: &StreamModeDetails{StreamMode: "ON_DEMAND"},
	}
	client := &fakeStreamClient{}

	_, err := resource.update(
		context.Background(),
		client,
		runtime.Prior[StreamResource, *StreamResourceOutput]{
			Inputs: priorInput, Outputs: priorOutput, Observed: priorOutput,
		},
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	require.Len(t, client.warmInputs, 1)
	assert.Equal(t, int32(0), aws.ToInt32(client.warmInputs[0].WarmThroughputMiBps))
}

func TestStreamUpdateStopsObservedEncryption(t *testing.T) {
	oldKey := "key-old"
	priorInput := validProvisionedStream(1)
	priorInput.EncryptionType = "KMS"
	priorInput.KMSKeyID = &oldKey
	resource := validProvisionedStream(1)
	priorOutput := &StreamResourceOutput{
		ARN:               testStreamARN,
		Name:              "events",
		OpenShardCount:    1,
		StreamModeDetails: &StreamModeDetails{StreamMode: "PROVISIONED"},
	}
	client := &fakeStreamClient{limits: streamLimitOutput(1, 10, 0, 10)}
	describeCalls := 0
	client.describe = func(
		_ *awssdk.DescribeStreamSummaryInput,
	) (*awssdk.DescribeStreamSummaryOutput, error) {
		describeCalls++
		result := activeStreamDescription()
		if describeCalls == 1 {
			result.StreamDescriptionSummary.EncryptionType = awstypes.EncryptionTypeKms
			result.StreamDescriptionSummary.KeyId = aws.String("observed-key")
		}
		return result, nil
	}

	_, err := resource.update(
		context.Background(),
		client,
		runtime.Prior[StreamResource, *StreamResourceOutput]{
			Inputs: priorInput, Outputs: priorOutput, Observed: priorOutput,
		},
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	require.Len(t, client.stopEncryptionInputs, 1)
	assert.Equal(t, awstypes.EncryptionTypeKms,
		client.stopEncryptionInputs[0].EncryptionType)
	assert.Equal(t, "observed-key", aws.ToString(client.stopEncryptionInputs[0].KeyId))
}

func TestStreamDeleteUsesPriorNameAndConsumerFlag(t *testing.T) {
	resource := validProvisionedStream(1)
	resource.Name = "new-name"
	resource.EnforceConsumerDeletion = true
	client := &fakeStreamClient{}

	err := resource.delete(
		context.Background(),
		client,
		&StreamResourceOutput{Name: "old-name"},
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, "old-name", aws.ToString(client.deleteInputs[0].StreamName))
	assert.True(t, aws.ToBool(client.deleteInputs[0].EnforceConsumerDeletion))
}

func TestStreamDeleteAcceptsAlreadyGone(t *testing.T) {
	resource := validProvisionedStream(1)
	client := &fakeStreamClient{fail: map[string]error{
		"DeleteStream": streamNotFoundError(),
	}}

	err := resource.delete(
		context.Background(),
		client,
		&StreamResourceOutput{Name: "events"},
		testStreamOptions(&fakeStreamClock{}),
	)
	require.NoError(t, err)
	assert.NotContains(t, client.calls, "DescribeStreamSummary")
}

func TestStreamReadUsesPriorName(t *testing.T) {
	resource := validProvisionedStream(1)
	resource.Name = "new-name"
	client := &fakeStreamClient{}
	client.describe = func(
		input *awssdk.DescribeStreamSummaryInput,
	) (*awssdk.DescribeStreamSummaryOutput, error) {
		assert.Equal(t, "old-name", aws.ToString(input.StreamName))
		assert.Nil(t, input.StreamARN)
		return activeStreamDescription(), nil
	}

	_, err := resource.read(
		context.Background(),
		client,
		&StreamResourceOutput{Name: "old-name"},
	)
	require.NoError(t, err)
}
