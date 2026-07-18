package kinesis

import (
	"context"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
)

type fakeStreamClient struct {
	calls    []string
	fail     map[string]error
	limits   *awssdk.DescribeLimitsOutput
	describe func(*awssdk.DescribeStreamSummaryInput) (*awssdk.DescribeStreamSummaryOutput, error)
	listTags []*awssdk.ListTagsForStreamOutput
	deleted  bool
	region   string

	createInputs          []*awssdk.CreateStreamInput
	deleteInputs          []*awssdk.DeleteStreamInput
	listTagInputs         []*awssdk.ListTagsForStreamInput
	addTagInputs          []*awssdk.AddTagsToStreamInput
	removeTagInputs       []*awssdk.RemoveTagsFromStreamInput
	updateModeInputs      []*awssdk.UpdateStreamModeInput
	updateShardInputs     []*awssdk.UpdateShardCountInput
	increaseInputs        []*awssdk.IncreaseStreamRetentionPeriodInput
	decreaseInputs        []*awssdk.DecreaseStreamRetentionPeriodInput
	enableInputs          []*awssdk.EnableEnhancedMonitoringInput
	disableInputs         []*awssdk.DisableEnhancedMonitoringInput
	startEncryptionInputs []*awssdk.StartStreamEncryptionInput
	stopEncryptionInputs  []*awssdk.StopStreamEncryptionInput
	maxRecordInputs       []*awssdk.UpdateMaxRecordSizeInput
	warmInputs            []*awssdk.UpdateStreamWarmThroughputInput
}

func (f *fakeStreamClient) Options() awssdk.Options {
	return awssdk.Options{Region: f.region}
}

func (f *fakeStreamClient) record(name string) error {
	f.calls = append(f.calls, name)
	if f.fail == nil {
		return nil
	}
	return f.fail[name]
}

func (f *fakeStreamClient) AddTagsToStream(
	_ context.Context,
	input *awssdk.AddTagsToStreamInput,
	_ ...func(*awssdk.Options),
) (*awssdk.AddTagsToStreamOutput, error) {
	f.addTagInputs = append(f.addTagInputs, input)
	return &awssdk.AddTagsToStreamOutput{}, f.record("AddTagsToStream")
}

func (f *fakeStreamClient) CreateStream(
	_ context.Context,
	input *awssdk.CreateStreamInput,
	_ ...func(*awssdk.Options),
) (*awssdk.CreateStreamOutput, error) {
	f.createInputs = append(f.createInputs, input)
	return &awssdk.CreateStreamOutput{}, f.record("CreateStream")
}

func (f *fakeStreamClient) DecreaseStreamRetentionPeriod(
	_ context.Context,
	input *awssdk.DecreaseStreamRetentionPeriodInput,
	_ ...func(*awssdk.Options),
) (*awssdk.DecreaseStreamRetentionPeriodOutput, error) {
	f.decreaseInputs = append(f.decreaseInputs, input)
	return &awssdk.DecreaseStreamRetentionPeriodOutput{},
		f.record("DecreaseStreamRetentionPeriod")
}

func (f *fakeStreamClient) DeleteStream(
	_ context.Context,
	input *awssdk.DeleteStreamInput,
	_ ...func(*awssdk.Options),
) (*awssdk.DeleteStreamOutput, error) {
	f.deleteInputs = append(f.deleteInputs, input)
	err := f.record("DeleteStream")
	if err == nil {
		f.deleted = true
	}
	return &awssdk.DeleteStreamOutput{}, err
}

func (f *fakeStreamClient) DescribeLimits(
	_ context.Context,
	_ *awssdk.DescribeLimitsInput,
	_ ...func(*awssdk.Options),
) (*awssdk.DescribeLimitsOutput, error) {
	if err := f.record("DescribeLimits"); err != nil {
		return nil, err
	}
	return f.limits, nil
}

func (f *fakeStreamClient) DescribeStreamSummary(
	_ context.Context,
	input *awssdk.DescribeStreamSummaryInput,
	_ ...func(*awssdk.Options),
) (*awssdk.DescribeStreamSummaryOutput, error) {
	if err := f.record("DescribeStreamSummary"); err != nil {
		return nil, err
	}
	if f.deleted {
		return nil, streamNotFoundError()
	}
	if f.describe != nil {
		return f.describe(input)
	}
	return activeStreamDescription(), nil
}

func (f *fakeStreamClient) DisableEnhancedMonitoring(
	_ context.Context,
	input *awssdk.DisableEnhancedMonitoringInput,
	_ ...func(*awssdk.Options),
) (*awssdk.DisableEnhancedMonitoringOutput, error) {
	f.disableInputs = append(f.disableInputs, input)
	return &awssdk.DisableEnhancedMonitoringOutput{},
		f.record("DisableEnhancedMonitoring")
}

func (f *fakeStreamClient) EnableEnhancedMonitoring(
	_ context.Context,
	input *awssdk.EnableEnhancedMonitoringInput,
	_ ...func(*awssdk.Options),
) (*awssdk.EnableEnhancedMonitoringOutput, error) {
	f.enableInputs = append(f.enableInputs, input)
	return &awssdk.EnableEnhancedMonitoringOutput{},
		f.record("EnableEnhancedMonitoring")
}

func (f *fakeStreamClient) IncreaseStreamRetentionPeriod(
	_ context.Context,
	input *awssdk.IncreaseStreamRetentionPeriodInput,
	_ ...func(*awssdk.Options),
) (*awssdk.IncreaseStreamRetentionPeriodOutput, error) {
	f.increaseInputs = append(f.increaseInputs, input)
	return &awssdk.IncreaseStreamRetentionPeriodOutput{},
		f.record("IncreaseStreamRetentionPeriod")
}

func (f *fakeStreamClient) ListTagsForStream(
	_ context.Context,
	input *awssdk.ListTagsForStreamInput,
	_ ...func(*awssdk.Options),
) (*awssdk.ListTagsForStreamOutput, error) {
	f.listTagInputs = append(f.listTagInputs, input)
	if err := f.record("ListTagsForStream"); err != nil {
		return nil, err
	}
	if len(f.listTags) == 0 {
		return &awssdk.ListTagsForStreamOutput{}, nil
	}
	result := f.listTags[0]
	f.listTags = f.listTags[1:]
	return result, nil
}

func (f *fakeStreamClient) RemoveTagsFromStream(
	_ context.Context,
	input *awssdk.RemoveTagsFromStreamInput,
	_ ...func(*awssdk.Options),
) (*awssdk.RemoveTagsFromStreamOutput, error) {
	f.removeTagInputs = append(f.removeTagInputs, input)
	return &awssdk.RemoveTagsFromStreamOutput{}, f.record("RemoveTagsFromStream")
}

func (f *fakeStreamClient) StartStreamEncryption(
	_ context.Context,
	input *awssdk.StartStreamEncryptionInput,
	_ ...func(*awssdk.Options),
) (*awssdk.StartStreamEncryptionOutput, error) {
	f.startEncryptionInputs = append(f.startEncryptionInputs, input)
	return &awssdk.StartStreamEncryptionOutput{}, f.record("StartStreamEncryption")
}

func (f *fakeStreamClient) StopStreamEncryption(
	_ context.Context,
	input *awssdk.StopStreamEncryptionInput,
	_ ...func(*awssdk.Options),
) (*awssdk.StopStreamEncryptionOutput, error) {
	f.stopEncryptionInputs = append(f.stopEncryptionInputs, input)
	return &awssdk.StopStreamEncryptionOutput{}, f.record("StopStreamEncryption")
}

func (f *fakeStreamClient) UpdateMaxRecordSize(
	_ context.Context,
	input *awssdk.UpdateMaxRecordSizeInput,
	_ ...func(*awssdk.Options),
) (*awssdk.UpdateMaxRecordSizeOutput, error) {
	f.maxRecordInputs = append(f.maxRecordInputs, input)
	return &awssdk.UpdateMaxRecordSizeOutput{}, f.record("UpdateMaxRecordSize")
}

func (f *fakeStreamClient) UpdateShardCount(
	_ context.Context,
	input *awssdk.UpdateShardCountInput,
	_ ...func(*awssdk.Options),
) (*awssdk.UpdateShardCountOutput, error) {
	f.updateShardInputs = append(f.updateShardInputs, input)
	return &awssdk.UpdateShardCountOutput{}, f.record("UpdateShardCount")
}

func (f *fakeStreamClient) UpdateStreamMode(
	_ context.Context,
	input *awssdk.UpdateStreamModeInput,
	_ ...func(*awssdk.Options),
) (*awssdk.UpdateStreamModeOutput, error) {
	f.updateModeInputs = append(f.updateModeInputs, input)
	return &awssdk.UpdateStreamModeOutput{}, f.record("UpdateStreamMode")
}

func (f *fakeStreamClient) UpdateStreamWarmThroughput(
	_ context.Context,
	input *awssdk.UpdateStreamWarmThroughputInput,
	_ ...func(*awssdk.Options),
) (*awssdk.UpdateStreamWarmThroughputOutput, error) {
	f.warmInputs = append(f.warmInputs, input)
	return &awssdk.UpdateStreamWarmThroughputOutput{},
		f.record("UpdateStreamWarmThroughput")
}

type fakeStreamClock struct {
	now    time.Time
	sleeps []time.Duration
	err    error
}

func (f *fakeStreamClock) Now() time.Time { return f.now }

func (f *fakeStreamClock) Sleep(_ context.Context, duration time.Duration) error {
	f.sleeps = append(f.sleeps, duration)
	if f.err != nil {
		return f.err
	}
	f.now = f.now.Add(duration)
	return nil
}

func testStreamOptions(clock streamClock) streamOperationOptions {
	return streamOperationOptions{
		clock:          clock,
		createTimeout:  time.Minute,
		updateTimeout:  time.Minute,
		deleteTimeout:  time.Minute,
		initialDelay:   time.Second,
		pollInterval:   time.Second,
		notFoundChecks: 20,
	}
}
