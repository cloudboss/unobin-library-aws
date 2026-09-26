package elasticache

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

const (
	subnetGroupTagRetryTimeout = 15 * time.Minute
	subnetGroupDeleteTimeout   = 5 * time.Minute
)

var subnetGroupNamePattern = regexp.MustCompile(`^[0-9A-Za-z-]+$`)

type SubnetGroupResource struct {
	Name        string             `ub:"name"`
	Description string             `ub:"description"`
	SubnetIds   []string           `ub:"subnet-ids"`
	Tags        *map[string]string `ub:"tags"`
}

type SubnetGroupResourceOutput struct {
	Name  string `ub:"name"`
	Arn   string `ub:"arn"`
	VpcId string `ub:"vpc-id"`
}

func (r *SubnetGroupResource) SchemaVersion() int { return 1 }

func (r *SubnetGroupResource) ReplaceFields() []string { return []string{"name"} }

func (r SubnetGroupResource) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Value(r.Description, "")}
}

func (r SubnetGroupResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.SubnetIds)).
			Message("subnet-ids must not be empty"),
		constraint.Must(constraint.MaxItems(r.Tags, 50)).
			Message("tags holds at most 50 entries"),
	}
}

func (r *SubnetGroupResource) ValidateInputs(context.Context, *awsCfg) error {
	if len(r.Name) < 1 || len(r.Name) > 255 {
		return errors.New("name must be 1 to 255 ASCII characters")
	}
	if !subnetGroupNamePattern.MatchString(r.Name) {
		return errors.New("name must contain only ASCII letters, digits, or hyphen")
	}
	if len(r.SubnetIds) == 0 {
		return errors.New("subnet-ids must not be empty")
	}
	tags := ptr.Value(r.Tags)
	if len(tags) > 50 {
		return errors.New("tags must have at most 50 entries")
	}
	for key, value := range tags {
		if n := utf8.RuneCountInString(key); n < 1 || n > 128 {
			return errors.New("tag key must be 1 to 128 characters")
		}
		if strings.HasPrefix(key, "aws:") {
			return errors.New("tag key must not begin with aws:")
		}
		if utf8.RuneCountInString(value) > 256 {
			return errors.New("tag value must be at most 256 characters")
		}
	}
	return nil
}

func (r *SubnetGroupResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*SubnetGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, client.Options().Region,
		retry.WithTimeout(subnetGroupTagRetryTimeout))
}

func (r *SubnetGroupResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg],
) (*SubnetGroupResourceOutput, error) {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, subnetGroupIdentity(r.Name, prior))
}

func (r *SubnetGroupResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg],
) (*SubnetGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior, retry.WithTimeout(subnetGroupTagRetryTimeout))
}

func (r *SubnetGroupResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior, retry.WithTimeout(subnetGroupDeleteTimeout))
}

type subnetGroupClient interface {
	elasticacheTagClient

	CreateCacheSubnetGroup(
		context.Context,
		*elasticache.CreateCacheSubnetGroupInput,
		...func(*elasticache.Options),
	) (*elasticache.CreateCacheSubnetGroupOutput, error)
	DescribeCacheSubnetGroups(
		context.Context,
		*elasticache.DescribeCacheSubnetGroupsInput,
		...func(*elasticache.Options),
	) (*elasticache.DescribeCacheSubnetGroupsOutput, error)
	ModifyCacheSubnetGroup(
		context.Context,
		*elasticache.ModifyCacheSubnetGroupInput,
		...func(*elasticache.Options),
	) (*elasticache.ModifyCacheSubnetGroupOutput, error)
	DeleteCacheSubnetGroup(
		context.Context,
		*elasticache.DeleteCacheSubnetGroupInput,
		...func(*elasticache.Options),
	) (*elasticache.DeleteCacheSubnetGroupOutput, error)
}

type elasticacheTagClient interface {
	ListTagsForResource(
		context.Context,
		*elasticache.ListTagsForResourceInput,
		...func(*elasticache.Options),
	) (*elasticache.ListTagsForResourceOutput, error)
	AddTagsToResource(
		context.Context,
		*elasticache.AddTagsToResourceInput,
		...func(*elasticache.Options),
	) (*elasticache.AddTagsToResourceOutput, error)
	RemoveTagsFromResource(
		context.Context,
		*elasticache.RemoveTagsFromResourceInput,
		...func(*elasticache.Options),
	) (*elasticache.RemoveTagsFromResourceOutput, error)
}

func (r *SubnetGroupResource) create(
	ctx context.Context,
	client subnetGroupClient,
	region string,
	retryOptions ...retry.Option,
) (*SubnetGroupResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	desiredTags := ptr.Value(r.Tags)
	createTags := subnetGroupTags(desiredTags)
	in := &elasticache.CreateCacheSubnetGroupInput{
		CacheSubnetGroupName:        aws.String(r.Name),
		CacheSubnetGroupDescription: aws.String(r.Description),
		SubnetIds:                   r.SubnetIds,
		Tags:                        createTags,
	}
	out, err := client.CreateCacheSubnetGroup(ctx, in)
	if err != nil {
		if len(createTags) == 0 || !partition.UnsupportedOperation(region, err) {
			return nil, fmt.Errorf("create cache subnet group: %w", err)
		}
		retryInput := *in
		retryInput.Tags = nil
		out, err = client.CreateCacheSubnetGroup(ctx, &retryInput)
		if err != nil {
			return nil, fmt.Errorf("create cache subnet group without tags: %w", err)
		}
		if out == nil || out.CacheSubnetGroup == nil ||
			aws.ToString(out.CacheSubnetGroup.ARN) == "" {
			return nil, errors.New("create cache subnet group without tags returned no ARN")
		}
		if err := addSubnetGroupTags(ctx, client,
			aws.ToString(out.CacheSubnetGroup.ARN), desiredTags, retryOptions...); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, strings.ToLower(r.Name))
}

func (r *SubnetGroupResource) read(
	ctx context.Context,
	client subnetGroupClient,
	name string,
) (*SubnetGroupResourceOutput, error) {
	paginator := elasticache.NewDescribeCacheSubnetGroupsPaginator(client,
		&elasticache.DescribeCacheSubnetGroupsInput{
			CacheSubnetGroupName: aws.String(name),
		})
	groups := make([]elasticachetypes.CacheSubnetGroup, 0, 1)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if subnetGroupNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe cache subnet groups: %w", err)
		}
		groups = append(groups, page.CacheSubnetGroups...)
		if len(groups) > 1 {
			return nil, fmt.Errorf("describe cache subnet groups: %d results for name %s",
				len(groups), name)
		}
	}
	if len(groups) == 0 {
		return nil, runtime.ErrNotFound
	}
	group := groups[0]
	return &SubnetGroupResourceOutput{
		Name:  aws.ToString(group.CacheSubnetGroupName),
		Arn:   aws.ToString(group.ARN),
		VpcId: aws.ToString(group.VpcId),
	}, nil
}

func (r *SubnetGroupResource) update(
	ctx context.Context,
	client subnetGroupClient,
	prior runtime.Prior[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg],
	retryOptions ...retry.Option,
) (*SubnetGroupResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	name := subnetGroupIdentity(r.Name, prior.Outputs)
	if runtime.Changed(prior.Inputs.Description, r.Description) ||
		runtime.Changed(prior.Inputs.SubnetIds, r.SubnetIds) {
		_, err := client.ModifyCacheSubnetGroup(ctx, &elasticache.ModifyCacheSubnetGroupInput{
			CacheSubnetGroupName:        aws.String(name),
			CacheSubnetGroupDescription: aws.String(r.Description),
			SubnetIds:                   r.SubnetIds,
		})
		if err != nil {
			return nil, fmt.Errorf("modify cache subnet group: %w", err)
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if prior.Outputs == nil || prior.Outputs.Arn == "" {
			return nil, errors.New("update cache subnet group: prior ARN is missing")
		}
		if err := syncSubnetGroupTags(ctx, client, prior.Outputs.Arn,
			ptr.Value(r.Tags), retryOptions...); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, name)
}

func (r *SubnetGroupResource) delete(
	ctx context.Context,
	client subnetGroupClient,
	prior *SubnetGroupResourceOutput,
	retryOptions ...retry.Option,
) error {
	name := subnetGroupIdentity(r.Name, prior)
	err := retry.OnError(ctx, subnetGroupDeleteRetryable, func(ctx context.Context) error {
		_, err := client.DeleteCacheSubnetGroup(ctx, &elasticache.DeleteCacheSubnetGroupInput{
			CacheSubnetGroupName: aws.String(name),
		})
		return err
	}, retryOptions...)
	if err != nil {
		if subnetGroupNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete cache subnet group: %w", err)
	}
	return nil
}

func subnetGroupIdentity(name string, prior *SubnetGroupResourceOutput) string {
	if prior != nil && prior.Name != "" {
		return prior.Name
	}
	return strings.ToLower(name)
}

func syncSubnetGroupTags(
	ctx context.Context,
	client elasticacheTagClient,
	arn string,
	desired map[string]string,
	retryOptions ...retry.Option,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			var out *elasticache.ListTagsForResourceOutput
			err := retry.OnError(ctx, subnetGroupTagRetryable,
				func(ctx context.Context) error {
					var err error
					out, err = client.ListTagsForResource(ctx,
						&elasticache.ListTagsForResourceInput{
							ResourceName: aws.String(arn),
						})
					return err
				}, retryOptions...)
			if err != nil {
				return nil, fmt.Errorf("list tags for resource: %w", err)
			}
			current := make(map[string]string, len(out.TagList))
			for _, tag := range out.TagList {
				current[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			return addSubnetGroupTags(ctx, client, arn, upsert, retryOptions...)
		},
		func(ctx context.Context, remove []string) error {
			err := retry.OnError(ctx, subnetGroupTagRetryable,
				func(ctx context.Context) error {
					_, err := client.RemoveTagsFromResource(ctx,
						&elasticache.RemoveTagsFromResourceInput{
							ResourceName: aws.String(arn),
							TagKeys:      remove,
						})
					return err
				}, retryOptions...)
			if err != nil {
				return fmt.Errorf("remove tags from resource: %w", err)
			}
			return nil
		},
	)
}

func addSubnetGroupTags(
	ctx context.Context,
	client elasticacheTagClient,
	arn string,
	tags map[string]string,
	retryOptions ...retry.Option,
) error {
	tagList := subnetGroupTags(tags)
	if len(tagList) == 0 {
		return nil
	}
	err := retry.OnError(ctx, subnetGroupTagRetryable, func(ctx context.Context) error {
		_, err := client.AddTagsToResource(ctx, &elasticache.AddTagsToResourceInput{
			ResourceName: aws.String(arn),
			Tags:         tagList,
		})
		return err
	}, retryOptions...)
	if err != nil {
		return fmt.Errorf("add tags to resource: %w", err)
	}
	return nil
}

func subnetGroupTags(tags map[string]string) []elasticachetypes.Tag {
	upsert, _ := tagsync.Diff(nil, tags)
	if len(upsert) == 0 {
		return nil
	}
	out := make([]elasticachetypes.Tag, 0, len(upsert))
	for _, key := range slices.Sorted(maps.Keys(upsert)) {
		out = append(out, elasticachetypes.Tag{
			Key: aws.String(key), Value: aws.String(upsert[key]),
		})
	}
	return out
}

func subnetGroupTagRetryable(err error) bool {
	var stateFault *elasticachetypes.InvalidReplicationGroupStateFault
	return errors.As(err, &stateFault) &&
		strings.Contains(stateFault.ErrorMessage(), "not in available state")
}

func subnetGroupNotFound(err error) bool {
	var notFound *elasticachetypes.CacheSubnetGroupNotFoundFault
	return errors.As(err, &notFound)
}

func subnetGroupDeleteRetryable(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "DependencyViolation"
}
