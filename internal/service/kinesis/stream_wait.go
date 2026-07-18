package kinesis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	streamCreateTimeout  = 5 * time.Minute
	streamUpdateTimeout  = 120 * time.Minute
	streamDeleteTimeout  = 120 * time.Minute
	streamInitialDelay   = 10 * time.Second
	streamPollInterval   = 3 * time.Second
	streamPollMaxDelay   = 10 * time.Second
	streamNotFoundChecks = 20
)

type streamClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type systemStreamClock struct{}

func (systemStreamClock) Now() time.Time { return time.Now() }

func (systemStreamClock) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

type streamOperationOptions struct {
	clock          streamClock
	createTimeout  time.Duration
	updateTimeout  time.Duration
	deleteTimeout  time.Duration
	initialDelay   time.Duration
	pollInterval   time.Duration
	notFoundChecks int
}

func defaultStreamOperationOptions() streamOperationOptions {
	return streamOperationOptions{
		clock:          systemStreamClock{},
		createTimeout:  streamCreateTimeout,
		updateTimeout:  streamUpdateTimeout,
		deleteTimeout:  streamDeleteTimeout,
		initialDelay:   streamInitialDelay,
		pollInterval:   streamPollInterval,
		notFoundChecks: streamNotFoundChecks,
	}
}

func (options streamOperationOptions) withDefaults() streamOperationOptions {
	defaults := defaultStreamOperationOptions()
	if options.clock == nil {
		options.clock = defaults.clock
	}
	if options.createTimeout == 0 {
		options.createTimeout = defaults.createTimeout
	}
	if options.updateTimeout == 0 {
		options.updateTimeout = defaults.updateTimeout
	}
	if options.deleteTimeout == 0 {
		options.deleteTimeout = defaults.deleteTimeout
	}
	if options.initialDelay == 0 {
		options.initialDelay = defaults.initialDelay
	}
	if options.pollInterval == 0 {
		options.pollInterval = defaults.pollInterval
	}
	if options.notFoundChecks == 0 {
		options.notFoundChecks = defaults.notFoundChecks
	}
	return options
}

type streamAddress struct {
	name string
	arn  string
}

func (address streamAddress) describeInput() *awssdk.DescribeStreamSummaryInput {
	return &awssdk.DescribeStreamSummaryInput{
		StreamName: stringPointer(address.name),
		StreamARN:  stringPointer(address.arn),
	}
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return aws.String(value)
}

func describeStream(
	ctx context.Context,
	client streamClient,
	address streamAddress,
) (*awstypes.StreamDescriptionSummary, error) {
	output, err := client.DescribeStreamSummary(ctx, address.describeInput())
	if isStreamNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe stream %s: %w", address.label(), err)
	}
	if output == nil || output.StreamDescriptionSummary == nil {
		return nil, runtime.ErrNotFound
	}
	return output.StreamDescriptionSummary, nil
}

func (address streamAddress) label() string {
	if address.name != "" {
		return address.name
	}
	return address.arn
}

func isStreamNotFound(err error) bool {
	var notFound *awstypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func waitStreamCreated(
	ctx context.Context,
	client streamClient,
	name string,
	options streamOperationOptions,
) (*awstypes.StreamDescriptionSummary, error) {
	return waitStreamActive(
		ctx,
		client,
		streamAddress{name: name},
		awstypes.StreamStatusCreating,
		"creation",
		options.createTimeout,
		options,
	)
}

func waitStreamUpdated(
	ctx context.Context,
	client streamClient,
	arn string,
	options streamOperationOptions,
) (*awstypes.StreamDescriptionSummary, error) {
	return waitStreamActive(
		ctx,
		client,
		streamAddress{arn: arn},
		awstypes.StreamStatusUpdating,
		"update",
		options.updateTimeout,
		options,
	)
}

func waitStreamCreateUpdated(
	ctx context.Context,
	client streamClient,
	arn string,
	options streamOperationOptions,
) (*awstypes.StreamDescriptionSummary, error) {
	return waitStreamActive(
		ctx,
		client,
		streamAddress{arn: arn},
		awstypes.StreamStatusUpdating,
		"update",
		options.createTimeout,
		options,
	)
}

func waitStreamActive(
	ctx context.Context,
	client streamClient,
	address streamAddress,
	pending awstypes.StreamStatus,
	operation string,
	timeout time.Duration,
	options streamOperationOptions,
) (*awstypes.StreamDescriptionSummary, error) {
	options = options.withDefaults()
	deadline := options.clock.Now().Add(timeout)
	if err := sleepStream(ctx, options.clock, deadline, options.initialDelay); err != nil {
		return nil, waitStreamError(operation, timeout, err)
	}
	notFoundCount := 0
	pollDelay := min(options.pollInterval, streamPollMaxDelay)
	for {
		summary, err := describeStream(ctx, client, address)
		if errors.Is(err, runtime.ErrNotFound) {
			notFoundCount++
			if notFoundCount > options.notFoundChecks {
				return nil, fmt.Errorf(
					"waiting for stream %s %s: not found after %d checks",
					address.label(), operation, options.notFoundChecks,
				)
			}
		} else if err != nil {
			return nil, fmt.Errorf(
				"waiting for stream %s %s: %w",
				address.label(), operation, err,
			)
		} else {
			notFoundCount = 0
			switch summary.StreamStatus {
			case awstypes.StreamStatusActive:
				return summary, nil
			case pending:
			default:
				return nil, fmt.Errorf(
					"waiting for stream %s %s: unexpected status %s",
					address.label(), operation, summary.StreamStatus,
				)
			}
		}
		if err := sleepStream(
			ctx,
			options.clock,
			deadline,
			pollDelay,
		); err != nil {
			return nil, waitStreamError(operation, timeout, err)
		}
		pollDelay = nextStreamPollDelay(pollDelay)
	}
}

func waitStreamDeleted(
	ctx context.Context,
	client streamClient,
	name string,
	options streamOperationOptions,
) error {
	options = options.withDefaults()
	deadline := options.clock.Now().Add(options.deleteTimeout)
	if err := sleepStream(ctx, options.clock, deadline, options.initialDelay); err != nil {
		return waitStreamError("deletion", options.deleteTimeout, err)
	}
	pollDelay := min(options.pollInterval, streamPollMaxDelay)
	for {
		summary, err := describeStream(ctx, client, streamAddress{name: name})
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("waiting for stream %s deletion: %w", name, err)
		}
		if summary.StreamStatus != awstypes.StreamStatusDeleting {
			return fmt.Errorf(
				"waiting for stream %s deletion: unexpected status %s",
				name, summary.StreamStatus,
			)
		}
		if err := sleepStream(
			ctx,
			options.clock,
			deadline,
			pollDelay,
		); err != nil {
			return waitStreamError("deletion", options.deleteTimeout, err)
		}
		pollDelay = nextStreamPollDelay(pollDelay)
	}
}

func nextStreamPollDelay(current time.Duration) time.Duration {
	if current >= streamPollMaxDelay/2 {
		return streamPollMaxDelay
	}
	return current * 2
}

var errStreamWaitTimeout = errors.New("stream wait timeout")

func sleepStream(
	ctx context.Context,
	clock streamClock,
	deadline time.Time,
	delay time.Duration,
) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	remaining := deadline.Sub(clock.Now())
	if remaining <= 0 {
		return errStreamWaitTimeout
	}
	if delay > remaining {
		delay = remaining
	}
	if err := clock.Sleep(ctx, delay); err != nil {
		return err
	}
	if !clock.Now().Before(deadline) {
		return errStreamWaitTimeout
	}
	return nil
}

func waitStreamError(operation string, timeout time.Duration, err error) error {
	if errors.Is(err, errStreamWaitTimeout) {
		return fmt.Errorf("waiting for stream %s timed out after %s", operation, timeout)
	}
	return fmt.Errorf("waiting for stream %s: %w", operation, err)
}
