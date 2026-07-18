package kinesis

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

const streamTagBatchSize = 10

func userStreamTags(tags *map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	result := map[string]string{}
	for key, value := range *tags {
		if !strings.HasPrefix(key, "aws:") {
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func reconcileStreamTags(
	ctx context.Context,
	client streamClient,
	arn string,
	desired *map[string]string,
) error {
	current, err := listStreamTags(ctx, client, arn)
	if err != nil {
		return reconcileStreamTagError(client, err)
	}
	wanted := userStreamTags(desired)
	remove := make([]string, 0, len(current))
	for key := range current {
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		if _, ok := wanted[key]; !ok {
			remove = append(remove, key)
		}
	}
	slices.Sort(remove)
	for start := 0; start < len(remove); start += streamTagBatchSize {
		end := min(start+streamTagBatchSize, len(remove))
		_, err := client.RemoveTagsFromStream(ctx, &awssdk.RemoveTagsFromStreamInput{
			StreamARN: aws.String(arn),
			TagKeys:   remove[start:end],
		})
		if err != nil {
			return reconcileStreamTagError(
				client,
				fmt.Errorf("remove tags from stream %s: %w", arn, err),
			)
		}
	}
	upsertKeys := make([]string, 0, len(wanted))
	for key, value := range wanted {
		currentValue, exists := current[key]
		if !exists || currentValue != value {
			upsertKeys = append(upsertKeys, key)
		}
	}
	slices.Sort(upsertKeys)
	for start := 0; start < len(upsertKeys); start += streamTagBatchSize {
		end := min(start+streamTagBatchSize, len(upsertKeys))
		batch := make(map[string]string, end-start)
		for _, key := range upsertKeys[start:end] {
			batch[key] = wanted[key]
		}
		_, err := client.AddTagsToStream(ctx, &awssdk.AddTagsToStreamInput{
			StreamARN: aws.String(arn),
			Tags:      batch,
		})
		if err != nil {
			return reconcileStreamTagError(
				client,
				fmt.Errorf("add tags to stream %s: %w", arn, err),
			)
		}
	}
	return nil
}

func reconcileStreamTagError(client streamClient, err error) error {
	if partition.UnsupportedOperation(client.Options().Region, err) {
		return nil
	}
	return err
}

func listStreamTags(
	ctx context.Context,
	client streamClient,
	arn string,
) (map[string]string, error) {
	result := map[string]string{}
	var start *string
	for {
		output, err := client.ListTagsForStream(ctx, &awssdk.ListTagsForStreamInput{
			StreamARN:            aws.String(arn),
			ExclusiveStartTagKey: start,
		})
		if err != nil {
			return nil, fmt.Errorf("list tags for stream %s: %w", arn, err)
		}
		if output == nil {
			return nil, fmt.Errorf("list tags for stream %s: empty response", arn)
		}
		for _, tag := range output.Tags {
			result[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
		if !aws.ToBool(output.HasMoreTags) {
			return result, nil
		}
		if len(output.Tags) == 0 || output.Tags[len(output.Tags)-1].Key == nil {
			return nil, fmt.Errorf("list tags for stream %s: page has no continuation key", arn)
		}
		start = output.Tags[len(output.Tags)-1].Key
	}
}
