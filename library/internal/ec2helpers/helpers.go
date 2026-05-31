package ec2helpers

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin-library-aws/library/internal/tagsync"
)

// Tags converts a desired tag map into the EC2 SDK tag list.
func Tags(tags map[string]string) []ec2types.Tag {
	out := make([]ec2types.Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, ec2types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return out
}

// TagSpecifications wraps a desired tag map in the create-time
// TagSpecification list for resourceType. It returns nil for an empty
// map: EC2 rejects a TagSpecification whose tag list is empty.
func TagSpecifications(
	resourceType ec2types.ResourceType,
	tags map[string]string,
) []ec2types.TagSpecification {
	if len(tags) == 0 {
		return nil
	}
	return []ec2types.TagSpecification{{
		ResourceType: resourceType,
		Tags:         Tags(tags),
	}}
}

// TagsToMap converts a tag list read back from AWS into a map, skipping
// the aws: reserved tags AWS attaches itself so they never read as drift.
func TagsToMap(tags []ec2types.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, t := range tags {
		key := aws.ToString(t.Key)
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = aws.ToString(t.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SyncTags reconciles the tags on resourceID with desired. The comparison and
// ordering are delegated to tagsync.Sync; the closures supply EC2's own tag
// calls. EC2 manages tags for every resource type through this one trio
// (DescribeTags/CreateTags/DeleteTags), addressed by resource id.
func SyncTags(
	ctx context.Context,
	client *ec2.Client,
	resourceID string,
	desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.DescribeTags(ctx, &ec2.DescribeTagsInput{
				Filters: []ec2types.Filter{{
					Name:   aws.String("resource-id"),
					Values: []string{resourceID},
				}},
			})
			if err != nil {
				return nil, fmt.Errorf("describe tags: %w", err)
			}
			current := make(map[string]string, len(resp.Tags))
			for _, t := range resp.Tags {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.CreateTags(ctx, &ec2.CreateTagsInput{
				Resources: []string{resourceID},
				Tags:      Tags(upsert),
			}); err != nil {
				return fmt.Errorf("create tags: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			tags := make([]ec2types.Tag, 0, len(remove))
			for _, k := range remove {
				tags = append(tags, ec2types.Tag{Key: aws.String(k)})
			}
			if _, err := client.DeleteTags(ctx, &ec2.DeleteTagsInput{
				Resources: []string{resourceID},
				Tags:      tags,
			}); err != nil {
				return fmt.Errorf("delete tags: %w", err)
			}
			return nil
		},
	)
}
