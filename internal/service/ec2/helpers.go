package ec2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	smithy "github.com/aws/smithy-go"

	"github.com/cloudboss/unobin-library-aws/internal/config"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

// newClient returns the AWS SDK Go v2 client for ec2,
// configured from cfg. cfg is the *config.Configuration the runtime
// hands every lifecycle method; the helper unwraps it and builds an
// aws.Config via config.LoadAWSConfig.
func newClient(ctx context.Context, cfg any) (*ec2.Client, error) {
	c, ok := cfg.(*config.Configuration)
	if !ok {
		return nil, fmt.Errorf("ec2client: unexpected configuration type %T", cfg)
	}
	awsCfg, err := config.LoadAWSConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	return ec2.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is an AWS API error whose service code
// is one of codes. EC2 reports a missing resource with a service code
// such as InvalidVpcID.NotFound carried on an HTTP 400, not a 404, so a
// resource Read matches the code to turn a describe of a gone resource
// into runtime.ErrNotFound.
func isNotFound(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return slices.Contains(codes, apiErr.ErrorCode())
	}
	return false
}

// region returns the region the client is configured for. A resource that
// composes an ARN needs it alongside the partition and account id.
func region(client *ec2.Client) string {
	return client.Options().Region
}

// mapToTags converts a desired tag map into the EC2 SDK tag list.
func mapToTags(tags map[string]string) []ec2types.Tag {
	out := make([]ec2types.Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, ec2types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return out
}

// tagSpecifications wraps a desired tag map in the create-time
// TagSpecification list for resourceType. It returns nil for an empty
// map: EC2 rejects a TagSpecification whose tag list is empty.
func tagSpecifications(
	resourceType ec2types.ResourceType,
	tags map[string]string,
) []ec2types.TagSpecification {
	if len(tags) == 0 {
		return nil
	}
	return []ec2types.TagSpecification{{
		ResourceType: resourceType,
		Tags:         mapToTags(tags),
	}}
}

// tagsToMap converts a tag list read back from AWS into a map, skipping
// the aws: reserved tags AWS attaches itself so they never read as drift.
func tagsToMap(tags []ec2types.Tag) map[string]string {
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

// syncTags reconciles the tags on resourceID with desired. The comparison and
// ordering are delegated to tagsync.Sync; the closures supply EC2's own tag
// calls. EC2 manages tags for every resource type through this one trio
// (DescribeTags/CreateTags/DeleteTags), addressed by resource id.
func syncTags(
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
				Tags:      mapToTags(upsert),
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
