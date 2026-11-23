package wafv2

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type wafTagClient interface {
	ListTagsForResource(context.Context, *awssvc.ListTagsForResourceInput,
		...func(*awssvc.Options)) (*awssvc.ListTagsForResourceOutput, error)
	TagResource(context.Context, *awssvc.TagResourceInput,
		...func(*awssvc.Options)) (*awssvc.TagResourceOutput, error)
	UntagResource(context.Context, *awssvc.UntagResourceInput,
		...func(*awssvc.Options)) (*awssvc.UntagResourceOutput, error)
}

func wafUserTags(tags map[string]string) map[string]string {
	filtered := make(map[string]string, len(tags))
	for key, value := range tags {
		if !strings.HasPrefix(key, "aws:") {
			filtered[key] = value
		}
	}
	return filtered
}

func listWAFTags(
	ctx context.Context, client wafTagClient, arn, region, kind string,
) (map[string]string, error) {
	result, err := client.ListTagsForResource(ctx, &awssvc.ListTagsForResourceInput{
		ResourceARN: aws.String(arn),
	})
	if err != nil && !partition.UnsupportedOperation(region, err) {
		return nil, fmt.Errorf("list %s tags %s: %w", kind, arn, err)
	}
	tags := map[string]string{}
	if err == nil && result != nil && result.TagInfoForResource != nil {
		for _, tag := range result.TagInfoForResource.TagList {
			tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}
	return wafUserTags(tags), nil
}

func syncWAFTags(
	ctx context.Context, client wafTagClient, arn, region, kind string,
	current, desired map[string]string,
) error {
	upserts, removals := tagsync.Diff(current, desired)
	if len(removals) > 0 {
		_, err := client.UntagResource(ctx, &awssvc.UntagResourceInput{
			ResourceARN: aws.String(arn), TagKeys: removals,
		})
		if err != nil {
			if partition.UnsupportedOperation(region, err) {
				return nil
			}
			return fmt.Errorf("untag %s %s: %w", kind, arn, err)
		}
	}
	if len(upserts) > 0 {
		_, err := client.TagResource(ctx, &awssvc.TagResourceInput{
			ResourceARN: aws.String(arn), Tags: expandTags(upserts),
		})
		if err != nil && !partition.UnsupportedOperation(region, err) {
			return fmt.Errorf("tag %s %s: %w", kind, arn, err)
		}
	}
	return nil
}
