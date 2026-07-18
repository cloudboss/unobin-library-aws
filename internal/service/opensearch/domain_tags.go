package opensearch

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

func syncDomainTags(
	ctx context.Context,
	client domainClient,
	arn string,
	desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			output, err := client.ListTags(ctx, &awssdk.ListTagsInput{ARN: aws.String(arn)})
			if err != nil {
				return nil, fmt.Errorf("list domain tags: %w", err)
			}
			current := map[string]string{}
			if output != nil {
				for _, tag := range output.TagList {
					current[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.AddTags(ctx, &awssdk.AddTagsInput{
				ARN: aws.String(arn), TagList: domainTags(&upsert),
			})
			if err != nil {
				return fmt.Errorf("add domain tags: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.RemoveTags(ctx, &awssdk.RemoveTagsInput{
				ARN: aws.String(arn), TagKeys: remove,
			})
			if err != nil {
				return fmt.Errorf("remove domain tags: %w", err)
			}
			return nil
		},
	)
}
