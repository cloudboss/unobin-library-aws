package ecs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for ECS, configured from cfg.
// It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*ecs.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return ecs.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is an AWS API error whose service code is
// one of codes. ECS models a missing resource as its own exception per
// construct -- ClusterNotFoundException, ServiceNotFoundException, and so on
// -- each reporting its type name as the error code, so a resource passes the
// codes that mean its own resource is gone and maps a match to
// runtime.ErrNotFound.
func isNotFound(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return slices.Contains(codes, apiErr.ErrorCode())
	}
	return false
}

// region returns the region the client is configured for. A resource reads
// it to decide partition-specific behavior, such as whether a create that
// sends tags must retry without them on a partition that cannot tag a
// resource at create time.
func region(client *ecs.Client) string {
	return client.Options().Region
}

// tagsSDK converts a desired tag map into the ECS SDK tag list, ordered by
// key so requests are deterministic. AWS system tags with the aws: prefix are
// ignored because callers cannot create, update, or delete them.
func tagsSDK(tags map[string]string) []ecstypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]ecstypes.Tag, 0, len(tags))
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		if strings.HasPrefix(k, "aws:") {
			continue
		}
		out = append(out, ecstypes.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// syncResourceTags reconciles the tags on the ECS resource identified by arn
// with the desired set, reading the live tags with ListTagsForResource and
// applying the difference with TagResource and UntagResource. All ECS
// resources share these three tag calls, keyed by ARN.
func syncResourceTags(
	ctx context.Context, client *ecs.Client, arn string, desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
				ResourceArn: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("list tags for resource: %w", err)
			}
			current := map[string]string{}
			for _, t := range resp.Tags {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &ecs.TagResourceInput{
				ResourceArn: aws.String(arn),
				Tags:        tagsSDK(upsert),
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &ecs.UntagResourceInput{
				ResourceArn: aws.String(arn),
				TagKeys:     remove,
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}

// derefStrings returns the slice behind an optional nested list input, or
// nil when the input is absent so the request member stays unset.
func derefStrings(s *[]string) []string {
	if s == nil {
		return nil
	}
	return *s
}

// derefStringMap returns the map behind an optional nested map input, or nil
// when the input is absent so the request member stays unset.
func derefStringMap(m *map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	return *m
}
