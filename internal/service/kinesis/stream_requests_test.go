package kinesis

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamCreateInputUsesEffectiveModeAndOptionalMembers(t *testing.T) {
	metrics := []string{"IncomingBytes", "ALL"}
	tags := map[string]string{"env": "test", "aws:owner": "system"}
	recordSize := int64(2048)
	resource := StreamResource{
		Name:               "events",
		EncryptionType:     "NONE",
		RetentionPeriod:    24,
		ShardCount:         new(int64(2)),
		ShardLevelMetrics:  &metrics,
		Tags:               &tags,
		MaxRecordSizeInKiB: &recordSize,
	}

	input, err := resource.createInput()
	require.NoError(t, err)
	assert.Equal(t, "events", aws.ToString(input.StreamName))
	assert.Equal(t, int32(2), aws.ToInt32(input.ShardCount))
	assert.Nil(t, input.StreamModeDetails)
	assert.Equal(t, int32(2048), aws.ToInt32(input.MaxRecordSizeInKiB))
	assert.Nil(t, input.WarmThroughputMiBps)
	assert.Equal(t, map[string]string{"env": "test"}, input.Tags)
	assert.Equal(t, []awstypes.MetricsName(nil), metricsForRequest(nil))
}

func TestStreamCreateInputOmitsProvisionedMembersForOnDemand(t *testing.T) {
	warm := int64(20)
	resource := StreamResource{
		Name:                "events",
		EncryptionType:      "NONE",
		RetentionPeriod:     24,
		StreamModeDetails:   &StreamModeDetails{StreamMode: "ON_DEMAND"},
		WarmThroughputMiBps: &warm,
	}

	input, err := resource.createInput()
	require.NoError(t, err)
	assert.Nil(t, input.ShardCount)
	require.NotNil(t, input.StreamModeDetails)
	assert.Equal(t, awstypes.StreamModeOnDemand, input.StreamModeDetails.StreamMode)
	assert.Equal(t, int32(20), aws.ToInt32(input.WarmThroughputMiBps))
	assert.Nil(t, input.MaxRecordSizeInKiB)
	assert.Nil(t, input.Tags)
}

func TestStreamOutputPreservesCloudValues(t *testing.T) {
	created := time.Date(2026, time.July, 18, 12, 30, 0, 123, time.FixedZone("EDT", -4*60*60))
	summary := &awstypes.StreamDescriptionSummary{
		StreamARN:               aws.String("arn:aws:kinesis:us-east-1:123:stream/events"),
		StreamName:              aws.String("events"),
		OpenShardCount:          aws.Int32(2),
		StreamCreationTimestamp: &created,
		StreamStatus:            awstypes.StreamStatusActive,
		ConsumerCount:           aws.Int32(3),
		MaxRecordSizeInKiB:      aws.Int32(2048),
		StreamModeDetails: &awstypes.StreamModeDetails{
			StreamMode: awstypes.StreamModeOnDemand,
		},
		WarmThroughput: &awstypes.WarmThroughputObject{
			CurrentMiBps: aws.Int32(10),
			TargetMiBps:  aws.Int32(20),
		},
	}

	output := streamOutput(summary)
	require.NotNil(t, output)
	assert.Equal(t, "arn:aws:kinesis:us-east-1:123:stream/events", output.ARN)
	assert.Equal(t, "events", output.Name)
	assert.Equal(t, int64(2), output.OpenShardCount)
	assert.Equal(t, "2026-07-18T16:30:00.000000123Z", output.StreamCreationTimestamp)
	assert.Equal(t, "ACTIVE", output.StreamStatus)
	assert.Equal(t, int64(3), *output.ConsumerCount)
	assert.Equal(t, int64(2048), *output.MaxRecordSizeInKiB)
	assert.Equal(t, "ON_DEMAND", output.StreamModeDetails.StreamMode)
	assert.Equal(t, int64(10), *output.WarmThroughput.CurrentMiBps)
	assert.Equal(t, int64(20), *output.WarmThroughput.TargetMiBps)
}

func TestStreamOutputPreservesAbsentOptionalMembers(t *testing.T) {
	tests := []struct {
		name string
		mode *awstypes.StreamModeDetails
	}{
		{name: "all optional members absent"},
		{
			name: "provisioned mode only",
			mode: &awstypes.StreamModeDetails{StreamMode: awstypes.StreamModeProvisioned},
		},
		{
			name: "on-demand mode only",
			mode: &awstypes.StreamModeDetails{StreamMode: awstypes.StreamModeOnDemand},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := streamOutput(&awstypes.StreamDescriptionSummary{
				StreamARN:         aws.String(testStreamARN),
				StreamName:        aws.String("events"),
				OpenShardCount:    aws.Int32(0),
				StreamStatus:      awstypes.StreamStatusActive,
				StreamModeDetails: test.mode,
			})

			require.NotNil(t, output)
			assert.Nil(t, output.ConsumerCount)
			assert.Nil(t, output.MaxRecordSizeInKiB)
			assert.Nil(t, output.WarmThroughput)
			if test.mode == nil {
				assert.Nil(t, output.StreamModeDetails)
				return
			}
			require.NotNil(t, output.StreamModeDetails)
			assert.Equal(t, string(test.mode.StreamMode),
				output.StreamModeDetails.StreamMode)
		})
	}
}
