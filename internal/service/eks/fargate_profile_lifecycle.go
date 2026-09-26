package eks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

const fargateProfileCreateRetryTimeout = 2 * time.Minute

var fargateProfileClusterLocks sync.Map

type fargateProfileClient interface {
	CreateFargateProfile(context.Context, *eks.CreateFargateProfileInput,
		...func(*eks.Options)) (*eks.CreateFargateProfileOutput, error)
	DescribeFargateProfile(context.Context, *eks.DescribeFargateProfileInput,
		...func(*eks.Options)) (*eks.DescribeFargateProfileOutput, error)
	DeleteFargateProfile(context.Context, *eks.DeleteFargateProfileInput,
		...func(*eks.Options)) (*eks.DeleteFargateProfileOutput, error)
	ListTagsForResource(context.Context, *eks.ListTagsForResourceInput,
		...func(*eks.Options)) (*eks.ListTagsForResourceOutput, error)
	TagResource(context.Context, *eks.TagResourceInput,
		...func(*eks.Options)) (*eks.TagResourceOutput, error)
	UntagResource(context.Context, *eks.UntagResourceInput,
		...func(*eks.Options)) (*eks.UntagResourceOutput, error)
}

func (r FargateProfileResource) createFargateProfile(
	ctx context.Context,
	client fargateProfileClient,
	clock clusterClock,
) (*FargateProfileResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	token, err := eksClientRequestToken()
	if err != nil {
		return nil, err
	}
	lock := fargateProfileClusterLock(r.ClusterName)
	lock.Lock()
	defer lock.Unlock()
	input := r.fargateProfileCreateInput(token)
	if err := retryFargateProfileCreate(ctx, clock, func(ctx context.Context) error {
		_, err := client.CreateFargateProfile(ctx, input)
		if err != nil {
			return fmt.Errorf("create Fargate profile %s/%s: %w",
				r.ClusterName, r.FargateProfileName, err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	output, err := finishFargateProfileCreate(
		ctx, client, r.ClusterName, r.FargateProfileName, clock,
	)
	if err == nil {
		return output, nil
	}
	rollbackErr := rollbackFargateProfileCreate(
		ctx, client, r.ClusterName, r.FargateProfileName, clock,
	)
	if rollbackErr != nil {
		return nil, errors.Join(
			err,
			fmt.Errorf("roll back accepted Fargate profile create: %w", rollbackErr),
		)
	}
	return nil, err
}

func finishFargateProfileCreate(
	ctx context.Context,
	client fargateProfileClient,
	clusterName string,
	profileName string,
	clock clusterClock,
) (*FargateProfileResourceOutput, error) {
	if err := waitFargateProfileCreated(
		ctx, client, clusterName, profileName, clock,
	); err != nil {
		return nil, err
	}
	return readFargateProfileByName(ctx, client, clusterName, profileName)
}

func rollbackFargateProfileCreate(
	ctx context.Context,
	client fargateProfileClient,
	clusterName string,
	profileName string,
	clock clusterClock,
) error {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), fargateProfileWaitTimeout,
	)
	defer cancel()
	return deleteFargateProfileByName(cleanupCtx, client, clusterName, profileName, clock)
}

func retryFargateProfileCreate(
	ctx context.Context,
	clock clusterClock,
	create func(context.Context) error,
) error {
	deadline := clock.Now().Add(fargateProfileCreateRetryTimeout)
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		err := create(ctx)
		if err == nil || !isFargateProfileCreateRetryable(err) {
			return err
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("create Fargate profile retry timed out after %s: %w",
				fargateProfileCreateRetryTimeout, err)
		}
	}
}

func isFargateProfileCreateRetryable(err error) bool {
	var invalid *ekstypes.InvalidParameterException
	return errors.As(err, &invalid) &&
		strings.Contains(
			invalid.ErrorMessage(),
			"Misconfigured PodExecutionRole Trust Policy",
		)
}

func (r FargateProfileResource) readFargateProfile(
	ctx context.Context,
	client fargateProfileClient,
	prior *FargateProfileResourceOutput,
) (*FargateProfileResourceOutput, error) {
	if prior == nil {
		return readFargateProfileByName(ctx, client, r.ClusterName, r.FargateProfileName)
	}
	clusterName, profileName, err := fargateProfileNames(prior)
	if err != nil {
		return nil, err
	}
	return readFargateProfileByName(ctx, client, clusterName, profileName)
}

func readFargateProfileByName(
	ctx context.Context,
	client fargateProfileClient,
	clusterName string,
	profileName string,
) (*FargateProfileResourceOutput, error) {
	profile, err := describeFargateProfile(ctx, client, clusterName, profileName)
	if err != nil {
		return nil, err
	}
	return fargateProfileOutput(profile), nil
}

func (r FargateProfileResource) updateFargateProfile(
	ctx context.Context,
	client fargateProfileClient,
	prior runtime.Prior[FargateProfileResource, *FargateProfileResourceOutput],
) (*FargateProfileResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	clusterName, profileName, err := fargateProfileNames(prior.Outputs)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		arn, err := fargateProfileTagARN(prior)
		if err != nil {
			return nil, err
		}
		if err := syncFargateProfileTags(ctx, client, arn, valueMapOrNil(r.Tags)); err != nil {
			return nil, err
		}
	}
	return readFargateProfileByName(ctx, client, clusterName, profileName)
}

func (r FargateProfileResource) deleteFargateProfile(
	ctx context.Context,
	client fargateProfileClient,
	prior *FargateProfileResourceOutput,
	clock clusterClock,
) error {
	clusterName, profileName, err := fargateProfileNames(prior)
	if err != nil {
		return err
	}
	lock := fargateProfileClusterLock(clusterName)
	lock.Lock()
	defer lock.Unlock()
	return deleteFargateProfileByName(ctx, client, clusterName, profileName, clock)
}

func deleteFargateProfileByName(
	ctx context.Context,
	client fargateProfileClient,
	clusterName string,
	profileName string,
	clock clusterClock,
) error {
	_, err := client.DeleteFargateProfile(ctx, &eks.DeleteFargateProfileInput{
		ClusterName:        aws.String(clusterName),
		FargateProfileName: aws.String(profileName),
	})
	if err != nil {
		if isFargateProfileNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete Fargate profile %s/%s: %w", clusterName, profileName, err)
	}
	return waitFargateProfileDeleted(ctx, client, clusterName, profileName, clock)
}

func fargateProfileNames(output *FargateProfileResourceOutput) (string, string, error) {
	if output == nil {
		return "", "", errors.New("prior Fargate profile output is missing")
	}
	if output.ClusterName == "" {
		return "", "", errors.New("prior Fargate profile output has no cluster name")
	}
	if output.FargateProfileName == "" {
		return "", "", errors.New("prior Fargate profile output has no profile name")
	}
	return output.ClusterName, output.FargateProfileName, nil
}

func fargateProfileOutput(
	profile *ekstypes.FargateProfile,
) *FargateProfileResourceOutput {
	return &FargateProfileResourceOutput{
		ClusterName:         aws.ToString(profile.ClusterName),
		FargateProfileName:  aws.ToString(profile.FargateProfileName),
		ARN:                 aws.ToString(profile.FargateProfileArn),
		PodExecutionRoleARN: aws.ToString(profile.PodExecutionRoleArn),
		Selector:            fargateProfileSelectorOutput(profile.Selectors),
		Status:              string(profile.Status),
		SubnetIDs:           slicesClone(profile.Subnets),
	}
}

func syncFargateProfileTags(
	ctx context.Context,
	client fargateProfileClient,
	arn string,
	desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			output, err := client.ListTagsForResource(ctx, &eks.ListTagsForResourceInput{
				ResourceArn: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("list Fargate profile tags: %w", err)
			}
			if output == nil {
				return map[string]string{}, nil
			}
			return output.Tags, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &eks.TagResourceInput{
				ResourceArn: aws.String(arn),
				Tags:        upsert,
			})
			if err != nil {
				return fmt.Errorf("tag Fargate profile: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &eks.UntagResourceInput{
				ResourceArn: aws.String(arn),
				TagKeys:     remove,
			})
			if err != nil {
				return fmt.Errorf("untag Fargate profile: %w", err)
			}
			return nil
		},
	)
}

func fargateProfileTagARN(
	prior runtime.Prior[FargateProfileResource, *FargateProfileResourceOutput],
) (string, error) {
	if prior.Observed != nil && prior.Observed.ARN != "" {
		return prior.Observed.ARN, nil
	}
	if prior.Outputs != nil && prior.Outputs.ARN != "" {
		return prior.Outputs.ARN, nil
	}
	return "", errors.New("fargate profile output has no ARN for tag update")
}

func fargateProfileClusterLock(clusterName string) *sync.Mutex {
	actual, _ := fargateProfileClusterLocks.LoadOrStore(clusterName, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

func slicesClone[T any](values []T) []T {
	if values == nil {
		return nil
	}
	cloned := make([]T, len(values))
	copy(cloned, values)
	return cloned
}
