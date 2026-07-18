package kinesis

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

type streamClient interface {
	Options() awssdk.Options
	AddTagsToStream(context.Context, *awssdk.AddTagsToStreamInput,
		...func(*awssdk.Options)) (*awssdk.AddTagsToStreamOutput, error)
	CreateStream(context.Context, *awssdk.CreateStreamInput,
		...func(*awssdk.Options)) (*awssdk.CreateStreamOutput, error)
	DecreaseStreamRetentionPeriod(context.Context, *awssdk.DecreaseStreamRetentionPeriodInput,
		...func(*awssdk.Options)) (*awssdk.DecreaseStreamRetentionPeriodOutput, error)
	DeleteStream(context.Context, *awssdk.DeleteStreamInput,
		...func(*awssdk.Options)) (*awssdk.DeleteStreamOutput, error)
	DescribeLimits(context.Context, *awssdk.DescribeLimitsInput,
		...func(*awssdk.Options)) (*awssdk.DescribeLimitsOutput, error)
	DescribeStreamSummary(context.Context, *awssdk.DescribeStreamSummaryInput,
		...func(*awssdk.Options)) (*awssdk.DescribeStreamSummaryOutput, error)
	DisableEnhancedMonitoring(context.Context, *awssdk.DisableEnhancedMonitoringInput,
		...func(*awssdk.Options)) (*awssdk.DisableEnhancedMonitoringOutput, error)
	EnableEnhancedMonitoring(context.Context, *awssdk.EnableEnhancedMonitoringInput,
		...func(*awssdk.Options)) (*awssdk.EnableEnhancedMonitoringOutput, error)
	IncreaseStreamRetentionPeriod(context.Context, *awssdk.IncreaseStreamRetentionPeriodInput,
		...func(*awssdk.Options)) (*awssdk.IncreaseStreamRetentionPeriodOutput, error)
	ListTagsForStream(context.Context, *awssdk.ListTagsForStreamInput,
		...func(*awssdk.Options)) (*awssdk.ListTagsForStreamOutput, error)
	RemoveTagsFromStream(context.Context, *awssdk.RemoveTagsFromStreamInput,
		...func(*awssdk.Options)) (*awssdk.RemoveTagsFromStreamOutput, error)
	StartStreamEncryption(context.Context, *awssdk.StartStreamEncryptionInput,
		...func(*awssdk.Options)) (*awssdk.StartStreamEncryptionOutput, error)
	StopStreamEncryption(context.Context, *awssdk.StopStreamEncryptionInput,
		...func(*awssdk.Options)) (*awssdk.StopStreamEncryptionOutput, error)
	UpdateMaxRecordSize(context.Context, *awssdk.UpdateMaxRecordSizeInput,
		...func(*awssdk.Options)) (*awssdk.UpdateMaxRecordSizeOutput, error)
	UpdateShardCount(context.Context, *awssdk.UpdateShardCountInput,
		...func(*awssdk.Options)) (*awssdk.UpdateShardCountOutput, error)
	UpdateStreamMode(context.Context, *awssdk.UpdateStreamModeInput,
		...func(*awssdk.Options)) (*awssdk.UpdateStreamModeOutput, error)
	UpdateStreamWarmThroughput(context.Context, *awssdk.UpdateStreamWarmThroughputInput,
		...func(*awssdk.Options)) (*awssdk.UpdateStreamWarmThroughputOutput, error)
}

func newClient(ctx context.Context, cfg *awsCfg) (*awssdk.Client, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return awssdk.NewFromConfig(loaded, func(options *awssdk.Options) {
		options.Retryer = streamRetryer{Retryer: options.Retryer}
	}), nil
}

type streamRetryer struct {
	aws.Retryer
}

func (r streamRetryer) IsErrorRetryable(err error) bool {
	if isStreamTransientLimit(err) {
		return true
	}
	return r.Retryer.IsErrorRetryable(err)
}

func (r streamRetryer) GetAttemptToken(
	ctx context.Context,
) (func(error) error, error) {
	if retryer, ok := r.Retryer.(aws.RetryerV2); ok {
		return retryer.GetAttemptToken(ctx)
	}
	return r.GetInitialToken(), nil
}

func isStreamTransientLimit(err error) bool {
	var limit *awstypes.LimitExceededException
	if !errors.As(err, &limit) {
		return false
	}
	message := limit.ErrorMessage()
	return strings.Contains(message, "simultaneously be in CREATING or DELETING") ||
		strings.Contains(message, "Rate exceeded for stream")
}
