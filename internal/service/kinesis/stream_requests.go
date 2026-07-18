package kinesis

import (
	"errors"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
)

func (r StreamResource) createInput() (*awssdk.CreateStreamInput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	input := &awssdk.CreateStreamInput{
		StreamName:          aws.String(r.Name),
		Tags:                userStreamTags(r.Tags),
		MaxRecordSizeInKiB:  streamInt32Pointer(r.MaxRecordSizeInKiB),
		WarmThroughputMiBps: streamInt32Pointer(r.WarmThroughputMiBps),
	}
	if r.StreamModeDetails != nil {
		input.StreamModeDetails = &awstypes.StreamModeDetails{
			StreamMode: awstypes.StreamMode(r.StreamModeDetails.StreamMode),
		}
	}
	if effectiveStreamMode(r.StreamModeDetails) == streamModeProvisioned {
		input.ShardCount = streamInt32Pointer(r.ShardCount)
	}
	return input, nil
}

func metricsForRequest(values *[]string) []awstypes.MetricsName {
	if metricCount(values) == 0 {
		return nil
	}
	metrics := make([]awstypes.MetricsName, 0, metricCount(values))
	for _, value := range stringSlice(values) {
		metrics = append(metrics, awstypes.MetricsName(value))
	}
	slices.Sort(metrics)
	return metrics
}

func metricDifference(left, right *[]string) []awstypes.MetricsName {
	rightSet := make(map[string]struct{}, metricCount(right))
	for _, value := range stringSlice(right) {
		rightSet[value] = struct{}{}
	}
	result := make([]awstypes.MetricsName, 0, metricCount(left))
	for _, value := range stringSlice(left) {
		if _, ok := rightSet[value]; !ok {
			result = append(result, awstypes.MetricsName(value))
		}
	}
	slices.Sort(result)
	return result
}

func streamInt32Pointer(value *int64) *int32 {
	if value == nil {
		return nil
	}
	converted := int32(*value)
	return &converted
}

func streamInt64Pointer(value *int32) *int64 {
	if value == nil {
		return nil
	}
	converted := int64(*value)
	return &converted
}

func streamOutput(summary *awstypes.StreamDescriptionSummary) *StreamResourceOutput {
	if summary == nil {
		return nil
	}
	output := &StreamResourceOutput{
		ARN:                     aws.ToString(summary.StreamARN),
		Name:                    aws.ToString(summary.StreamName),
		OpenShardCount:          int64(aws.ToInt32(summary.OpenShardCount)),
		StreamCreationTimestamp: streamTimestamp(summary.StreamCreationTimestamp),
		StreamStatus:            string(summary.StreamStatus),
		ConsumerCount:           streamInt64Pointer(summary.ConsumerCount),
		MaxRecordSizeInKiB:      streamInt64Pointer(summary.MaxRecordSizeInKiB),
	}
	if summary.StreamModeDetails != nil {
		output.StreamModeDetails = &StreamModeDetails{
			StreamMode: string(summary.StreamModeDetails.StreamMode),
		}
	}
	if summary.WarmThroughput != nil {
		output.WarmThroughput = &WarmThroughput{
			CurrentMiBps: streamInt64Pointer(summary.WarmThroughput.CurrentMiBps),
			TargetMiBps:  streamInt64Pointer(summary.WarmThroughput.TargetMiBps),
		}
	}
	return output
}

func streamTimestamp(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func streamPriorName(desired string, prior *StreamResourceOutput) string {
	if prior != nil && prior.Name != "" {
		return prior.Name
	}
	return desired
}

func streamPriorARN(priorOutputs, observed *StreamResourceOutput) (string, error) {
	if priorOutputs != nil && priorOutputs.ARN != "" {
		return priorOutputs.ARN, nil
	}
	if observed != nil && observed.ARN != "" {
		return observed.ARN, nil
	}
	return "", errors.New("prior stream ARN is empty")
}

func streamObserved(
	priorOutputs,
	observed *StreamResourceOutput,
) *StreamResourceOutput {
	if observed != nil {
		return observed
	}
	return priorOutputs
}
