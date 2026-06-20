package ecr

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for ECR, configured from cfg.
// It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*ecr.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return ecr.NewFromConfig(awsCfg), nil
}

// region returns the region the client is configured for, used to decide
// whether a create that sends tags must retry without them on a partition
// that cannot tag a repository at create time.
func region(client *ecr.Client) string {
	return client.Options().Region
}

// isNotFound reports whether err is ECR's RepositoryNotFoundException, the
// typed exception a call raises when the repository itself is gone. A
// resource Read maps it to runtime.ErrNotFound, and a delete swallows it as
// success.
func isNotFound(err error) bool {
	var notFound *ecrtypes.RepositoryNotFoundException
	return errors.As(err, &notFound)
}

// isNotEmpty reports whether err is ECR's RepositoryNotEmptyException, raised
// by a delete without force on a repository that still holds images.
func isNotEmpty(err error) bool {
	var notEmpty *ecrtypes.RepositoryNotEmptyException
	return errors.As(err, &notEmpty)
}

// isLifecyclePolicyNotFound reports whether err is ECR's
// LifecyclePolicyNotFoundException, which means the repository exists but has
// no lifecycle policy. That is a normal state for the folded lifecycle-policy
// field, not a sign the repository is gone, so it never maps to
// runtime.ErrNotFound.
func isLifecyclePolicyNotFound(err error) bool {
	var notFound *ecrtypes.LifecyclePolicyNotFoundException
	return errors.As(err, &notFound)
}

// isRepositoryPolicyNotFound reports whether err is ECR's
// RepositoryPolicyNotFoundException, which means the repository exists but
// has no repository policy, the same field-absent semantics as its lifecycle
// counterpart.
func isRepositoryPolicyNotFound(err error) bool {
	var notFound *ecrtypes.RepositoryPolicyNotFoundException
	return errors.As(err, &notFound)
}

// isPrincipalNotFound reports whether err is the InvalidParameterException
// ECR returns when a repository policy names an IAM principal it cannot
// resolve yet. A principal created moments before the policy is set has not
// propagated to ECR, a window that clears on its own, so a policy put retries
// through it.
func isPrincipalNotFound(err error) bool {
	var invalid *ecrtypes.InvalidParameterException
	return errors.As(err, &invalid) &&
		strings.Contains(invalid.ErrorMessage(), "Principal not found")
}

// repositoryTags converts a desired tag map into the ECR SDK tag list,
// ordered by key so the request is deterministic.
func repositoryTags(tags map[string]string) []ecrtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]ecrtypes.Tag, 0, len(tags))
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		out = append(out, ecrtypes.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return out
}
