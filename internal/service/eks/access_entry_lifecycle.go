package eks

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"

	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type accessEntryClient interface {
	CreateAccessEntry(context.Context, *eks.CreateAccessEntryInput,
		...func(*eks.Options)) (*eks.CreateAccessEntryOutput, error)
	DescribeAccessEntry(context.Context, *eks.DescribeAccessEntryInput,
		...func(*eks.Options)) (*eks.DescribeAccessEntryOutput, error)
	UpdateAccessEntry(context.Context, *eks.UpdateAccessEntryInput,
		...func(*eks.Options)) (*eks.UpdateAccessEntryOutput, error)
	DeleteAccessEntry(context.Context, *eks.DeleteAccessEntryInput,
		...func(*eks.Options)) (*eks.DeleteAccessEntryOutput, error)
	ListTagsForResource(context.Context, *eks.ListTagsForResourceInput,
		...func(*eks.Options)) (*eks.ListTagsForResourceOutput, error)
	TagResource(context.Context, *eks.TagResourceInput,
		...func(*eks.Options)) (*eks.TagResourceOutput, error)
	UntagResource(context.Context, *eks.UntagResourceInput,
		...func(*eks.Options)) (*eks.UntagResourceOutput, error)
}

const accessEntryPropagationTimeout = 2 * time.Minute

type accessEntryKey struct {
	clusterName  string
	principalARN string
}

func (k accessEntryKey) String() string {
	return k.clusterName + "/" + k.principalARN
}

func storedAccessEntryKey(output *AccessEntryResourceOutput) (accessEntryKey, error) {
	switch {
	case output == nil:
		return accessEntryKey{}, errors.New("prior EKS access entry output is missing")
	case output.ClusterName == "":
		return accessEntryKey{}, errors.New("prior EKS access entry output has no cluster name")
	case output.PrincipalARN == "":
		return accessEntryKey{}, errors.New("prior EKS access entry output has no principal ARN")
	default:
		return accessEntryKey{output.ClusterName, output.PrincipalARN}, nil
	}
}

func (r AccessEntryResource) createAccessEntry(
	ctx context.Context, client accessEntryClient, clock clusterClock, token func() (string, error),
) (*AccessEntryResourceOutput, error) {
	key := accessEntryKey{r.ClusterName, r.PrincipalARN}
	request, err := r.prepareAccessEntryCreate()
	if err != nil {
		return nil, fmt.Errorf("create EKS access entry %s: %w", key, err)
	}
	id, err := token()
	if err != nil {
		return nil, fmt.Errorf("create EKS access entry %s: %w", key, err)
	}
	request.ClientRequestToken = aws.String(id)
	if err := retryAccessEntryCreate(ctx, client, clock, request); err != nil {
		return nil, fmt.Errorf("create EKS access entry %s: %w", key, err)
	}
	return requireAccessEntry(ctx, client, key)
}

func retryAccessEntryCreate(
	ctx context.Context, client accessEntryClient, clock clusterClock,
	request *eks.CreateAccessEntryInput,
) error {
	retryCtx, cancel := context.WithTimeout(ctx, accessEntryPropagationTimeout)
	defer cancel()
	deadline := clock.Now().Add(accessEntryPropagationTimeout)
	for attempt := 0; ; attempt++ {
		if err := context.Cause(retryCtx); err != nil {
			return err
		}
		_, err := client.CreateAccessEntry(retryCtx, request)
		var invalid *ekstypes.InvalidParameterException
		if !errors.As(err, &invalid) || !strings.Contains(
			invalid.ErrorMessage(), "The specified principalArn is invalid: invalid principal",
		) {
			return err
		}
		if !sleepClusterRetry(retryCtx, clock, deadline, clusterBackoff(attempt)) ||
			!clock.Now().Before(deadline) {
			if cause := context.Cause(retryCtx); cause != nil {
				return cause
			}
			return fmt.Errorf("principal propagation timed out after %s: %w",
				accessEntryPropagationTimeout, err)
		}
	}
}

func (r AccessEntryResource) readAccessEntry(
	ctx context.Context, client accessEntryClient, prior *AccessEntryResourceOutput,
) (*AccessEntryResourceOutput, error) {
	key := accessEntryKey{r.ClusterName, r.PrincipalARN}
	if prior != nil {
		stored, err := storedAccessEntryKey(prior)
		if err != nil {
			return nil, err
		}
		key = stored
	}
	output, err := describeAccessEntry(ctx, client, key)
	if accessEntryNotFound(err) || (err == nil && output == nil) {
		return nil, runtime.ErrNotFound
	}
	return output, err
}

func requireAccessEntry(
	ctx context.Context, client accessEntryClient, key accessEntryKey,
) (*AccessEntryResourceOutput, error) {
	output, err := describeAccessEntry(ctx, client, key)
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, fmt.Errorf("describe EKS access entry %s: response has no access entry", key)
	}
	return output, nil
}

func describeAccessEntry(
	ctx context.Context, client accessEntryClient, key accessEntryKey,
) (*AccessEntryResourceOutput, error) {
	response, err := client.DescribeAccessEntry(ctx, &eks.DescribeAccessEntryInput{
		ClusterName: aws.String(key.clusterName), PrincipalArn: aws.String(key.principalARN),
	})
	if err != nil {
		return nil, fmt.Errorf("describe EKS access entry %s: %w", key, err)
	}
	if response == nil || response.AccessEntry == nil {
		return nil, nil
	}
	entry := response.AccessEntry
	tags := clusterTags(entry.Tags)
	if tags == nil {
		tags = map[string]string{}
	}
	return &AccessEntryResourceOutput{
		ARN:              aws.ToString(entry.AccessEntryArn),
		ClusterName:      aws.ToString(entry.ClusterName),
		PrincipalARN:     aws.ToString(entry.PrincipalArn),
		Type:             aws.ToString(entry.Type),
		UserName:         aws.ToString(entry.Username),
		KubernetesGroups: accessEntryObservedGroups(entry.KubernetesGroups),
		CreatedAt:        aws.ToTime(entry.CreatedAt).Format(time.RFC3339),
		ModifiedAt:       aws.ToTime(entry.ModifiedAt).Format(time.RFC3339),
		Tags:             tags,
	}, nil
}

func (r AccessEntryResource) updateAccessEntry(
	ctx context.Context, client accessEntryClient, prior accessEntryPrior,
) (*AccessEntryResourceOutput, error) {
	key, err := storedAccessEntryKey(prior.Outputs)
	if err != nil {
		return nil, err
	}
	observed := prior.Observed
	if observed == nil {
		observed = prior.Outputs
	}

	tags := clusterTags(valueMapOrNil(r.Tags))
	tagsChanged := !maps.Equal(tags, clusterTags(valueMapOrNil(prior.Inputs.Tags)))
	tagsDrifted := prior.Observed != nil && !maps.Equal(tags, clusterTags(prior.Observed.Tags))
	if tagsChanged || tagsDrifted {
		arn := observed.ARN
		if arn == "" {
			arn = prior.Outputs.ARN
		}
		if arn == "" {
			return nil, fmt.Errorf("EKS access entry %s has no ARN for tag update", key)
		}
		if err := syncAccessEntryTags(ctx, client, arn, tags); err != nil {
			return nil, fmt.Errorf("update EKS access entry %s: %w", key, err)
		}
	}

	groups, username := observed.KubernetesGroups, observed.UserName
	if r.KubernetesGroups != nil {
		groups = accessEntryRequestGroups(*r.KubernetesGroups)
	}
	if r.UserName != nil {
		username = *r.UserName
	}
	if !slices.Equal(groups, observed.KubernetesGroups) || username != observed.UserName {
		_, err := client.UpdateAccessEntry(ctx, &eks.UpdateAccessEntryInput{
			ClusterName:      aws.String(key.clusterName),
			PrincipalArn:     aws.String(key.principalARN),
			KubernetesGroups: accessEntryObservedGroups(groups),
			Username:         aws.String(username),
		})
		if err != nil {
			return nil, fmt.Errorf("update EKS access entry %s: %w", key, err)
		}
	}
	return requireAccessEntry(ctx, client, key)
}

func deleteAccessEntry(
	ctx context.Context, client accessEntryClient, prior *AccessEntryResourceOutput,
) error {
	key, err := storedAccessEntryKey(prior)
	if err != nil {
		return err
	}
	_, err = client.DeleteAccessEntry(ctx, &eks.DeleteAccessEntryInput{
		ClusterName: aws.String(key.clusterName), PrincipalArn: aws.String(key.principalARN),
	})
	if err != nil && !accessEntryNotFound(err) {
		return fmt.Errorf("delete EKS access entry %s: %w", key, err)
	}
	return nil
}

func accessEntryNotFound(err error) bool {
	var missing *ekstypes.ResourceNotFoundException
	return errors.As(err, &missing)
}

func syncAccessEntryTags(
	ctx context.Context, client accessEntryClient, arn string, desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			output, err := client.ListTagsForResource(ctx, &eks.ListTagsForResourceInput{
				ResourceArn: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("list access entry tags: %w", err)
			}
			if output == nil {
				return nil, nil
			}
			return output.Tags, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &eks.TagResourceInput{
				ResourceArn: aws.String(arn), Tags: upsert,
			})
			if err != nil {
				return fmt.Errorf("tag access entry: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &eks.UntagResourceInput{
				ResourceArn: aws.String(arn), TagKeys: remove,
			})
			if err != nil {
				return fmt.Errorf("untag access entry: %w", err)
			}
			return nil
		},
	)
}
