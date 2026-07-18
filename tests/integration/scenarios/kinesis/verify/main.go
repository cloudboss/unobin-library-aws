package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	kinesis "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
)

const streamName = "unobin-it-kinesis-stream"

type verifierClient interface {
	DescribeStreamSummary(context.Context, *kinesis.DescribeStreamSummaryInput,
		...func(*kinesis.Options)) (*kinesis.DescribeStreamSummaryOutput, error)
	ListTagsForStream(context.Context, *kinesis.ListTagsForStreamInput,
		...func(*kinesis.Options)) (*kinesis.ListTagsForStreamOutput, error)
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	configuration, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	client := kinesis.NewFromConfig(configuration)
	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
		return verifyPresent(ctx, client, 1, 48, map[string]string{
			"change": "old", "keep": "1", "remove": "yes",
		})
	case "updated":
		return verifyPresent(ctx, client, 2, 72, map[string]string{
			"add": "yes", "change": "new", "keep": "1",
		})
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf(
			"VERIFY_PHASE must be applied, updated, or destroyed, got %q",
			phase,
		)
	}
}

func verifyPresent(
	ctx context.Context,
	client verifierClient,
	wantShards int32,
	wantRetention int32,
	wantTags map[string]string,
) error {
	output, err := client.DescribeStreamSummary(ctx, &kinesis.DescribeStreamSummaryInput{
		StreamName: aws.String(streamName),
	})
	if err != nil {
		return fmt.Errorf("describe stream: %w", err)
	}
	if output == nil || output.StreamDescriptionSummary == nil {
		return errors.New("describe stream returned no summary")
	}
	summary := output.StreamDescriptionSummary
	if summary.StreamStatus != awstypes.StreamStatusActive {
		return fmt.Errorf("stream status is %s, want ACTIVE", summary.StreamStatus)
	}
	if got := aws.ToInt32(summary.OpenShardCount); got != wantShards {
		return fmt.Errorf("open shard count is %d, want %d", got, wantShards)
	}
	if got := aws.ToInt32(summary.RetentionPeriodHours); got != wantRetention {
		return fmt.Errorf("retention period is %d, want %d", got, wantRetention)
	}
	if aws.ToString(summary.StreamARN) == "" {
		return errors.New("stream ARN is empty")
	}
	tags, err := listTags(ctx, client, aws.ToString(summary.StreamARN))
	if err != nil {
		return err
	}
	if !equalTags(tags, wantTags) {
		return fmt.Errorf("stream tags are %#v, want %#v", tags, wantTags)
	}
	return nil
}

func verifyDestroyed(ctx context.Context, client verifierClient) error {
	output, err := client.DescribeStreamSummary(ctx, &kinesis.DescribeStreamSummaryInput{
		StreamName: aws.String(streamName),
	})
	var notFound *awstypes.ResourceNotFoundException
	if errors.As(err, &notFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("describe destroyed stream: %w", err)
	}
	if output != nil && output.StreamDescriptionSummary != nil {
		return fmt.Errorf("stream %s still exists", streamName)
	}
	return nil
}

func listTags(
	ctx context.Context,
	client verifierClient,
	arn string,
) (map[string]string, error) {
	result := map[string]string{}
	var start *string
	for {
		output, err := client.ListTagsForStream(ctx, &kinesis.ListTagsForStreamInput{
			StreamARN:            aws.String(arn),
			ExclusiveStartTagKey: start,
		})
		if err != nil {
			return nil, fmt.Errorf("list stream tags: %w", err)
		}
		if output == nil {
			return nil, errors.New("list stream tags returned no output")
		}
		for _, tag := range output.Tags {
			result[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
		if !aws.ToBool(output.HasMoreTags) {
			return result, nil
		}
		if len(output.Tags) == 0 {
			return nil, errors.New("stream tag page has no continuation key")
		}
		start = output.Tags[len(output.Tags)-1].Key
	}
}

func equalTags(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
