package rds

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/cloudboss/unobin/pkg/awscfg"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for RDS, configured from cfg.
// It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*rds.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return rds.NewFromConfig(awsCfg), nil
}

// tagList converts a desired tag map into the RDS SDK tag list. RDS shares one
// Tag type across its taggable resources, so every resource in this package
// builds its create-time tags through it.
func tagList(tags map[string]string) []rdstypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]rdstypes.Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, rdstypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return out
}

// syncTags reconciles a resource's tags with the desired set, reading the live
// tags through ListTagsForResource and writing changes with AddTagsToResource
// and RemoveTagsFromResource. RDS addresses every taggable resource by its ARN
// through one shared tag API, so the whole package reconciles tags through
// this helper.
func syncTags(
	ctx context.Context, client *rds.Client, arn string, desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx,
				&rds.ListTagsForResourceInput{ResourceName: aws.String(arn)})
			if err != nil {
				return nil, fmt.Errorf("list tags: %w", err)
			}
			current := map[string]string{}
			for _, t := range resp.TagList {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.AddTagsToResource(ctx, &rds.AddTagsToResourceInput{
				ResourceName: aws.String(arn),
				Tags:         tagList(upsert),
			}); err != nil {
				return fmt.Errorf("add tags: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.RemoveTagsFromResource(ctx, &rds.RemoveTagsFromResourceInput{
				ResourceName: aws.String(arn),
				TagKeys:      remove,
			}); err != nil {
				return fmt.Errorf("remove tags: %w", err)
			}
			return nil
		},
	)
}
