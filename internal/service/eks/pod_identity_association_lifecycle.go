package eks

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

const podIdentityPropagationTimeout = 2 * time.Minute

type podIdentityKey struct {
	clusterName   string
	associationID string
}

func (k podIdentityKey) String() string {
	return fmt.Sprintf("%q/%q", k.clusterName, k.associationID)
}

func storedPodIdentityKey(output *PodIdentityAssociationResourceOutput) (podIdentityKey, error) {
	switch {
	case output == nil:
		return podIdentityKey{}, errors.New("prior EKS Pod Identity association output is missing")
	case output.ClusterName == "":
		return podIdentityKey{}, errors.New("prior EKS Pod Identity association has no cluster name")
	case output.AssociationID == "":
		return podIdentityKey{}, errors.New("prior EKS Pod Identity association has no association ID")
	}
	return podIdentityKey{clusterName: output.ClusterName, associationID: output.AssociationID}, nil
}

func (r PodIdentityAssociationResource) createPodIdentity(
	ctx context.Context, client *eks.Client, clock clusterClock, token func() (string, error),
) (*PodIdentityAssociationResourceOutput, error) {
	id, err := token()
	if err != nil {
		return nil, fmt.Errorf("create EKS Pod Identity association in %q: %w", r.ClusterName, err)
	}
	request := &eks.CreatePodIdentityAssociationInput{
		ClusterName:        aws.String(r.ClusterName),
		Namespace:          aws.String(r.Namespace),
		ServiceAccount:     aws.String(r.ServiceAccount),
		RoleArn:            aws.String(r.RoleARN),
		DisableSessionTags: aws.Bool(aws.ToBool(r.DisableSessionTags)),
		TargetRoleArn:      r.TargetRoleARN,
		Tags:               clusterTags(valueMapOrNil(r.Tags)),
		ClientRequestToken: aws.String(id),
	}
	result, err := retryPodIdentityOperation(ctx, clock,
		func(ctx context.Context) (*eks.CreatePodIdentityAssociationOutput, error) {
			return client.CreatePodIdentityAssociation(ctx, request)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create EKS Pod Identity association in %q: %w", r.ClusterName, err)
	}
	if result == nil || result.Association == nil {
		return nil, fmt.Errorf("create EKS Pod Identity association in %q: missing association",
			r.ClusterName)
	}
	output := podIdentityOutput(result.Association)
	if output.AssociationID == "" || output.AssociationARN == "" ||
		output.ClusterName != r.ClusterName {
		return nil, fmt.Errorf("create EKS Pod Identity association in %q: invalid association identity",
			r.ClusterName)
	}
	return output, nil
}

func (r PodIdentityAssociationResource) updatePodIdentity(
	ctx context.Context, client *eks.Client, prior podIdentityPrior,
	clock clusterClock, token func() (string, error),
) (*PodIdentityAssociationResourceOutput, error) {
	key, err := storedPodIdentityKey(prior.Outputs)
	if err != nil {
		return nil, err
	}
	observed := prior.Observed
	if observed == nil {
		observed = prior.Outputs
	}
	output := *observed
	output.ClusterName, output.AssociationID = key.clusterName, key.associationID
	if output.AssociationARN == "" {
		output.AssociationARN = prior.Outputs.AssociationARN
	}
	output.Tags = clusterTags(output.Tags)
	tags := clusterTags(valueMapOrNil(r.Tags))
	if !maps.Equal(tags, output.Tags) {
		if output.AssociationARN == "" {
			return nil, fmt.Errorf("update EKS Pod Identity association %s: missing ARN for tags", key)
		}
		if err := syncPodIdentityTags(ctx, client, output.AssociationARN, tags); err != nil {
			return nil, fmt.Errorf("update EKS Pod Identity association %s tags: %w", key, err)
		}
		output.Tags = tags
	}
	if output.Tags == nil {
		output.Tags = map[string]string{}
	}

	disableSessionTags := aws.ToBool(r.DisableSessionTags)
	if disableSessionTags == observed.DisableSessionTags && r.RoleARN == observed.RoleARN &&
		!runtime.Changed(r.TargetRoleARN, observed.TargetRoleARN) {
		return &output, nil
	}

	id, err := token()
	if err != nil {
		return nil, fmt.Errorf("update EKS Pod Identity association %s: %w", key, err)
	}
	request := &eks.UpdatePodIdentityAssociationInput{
		ClusterName:        aws.String(key.clusterName),
		AssociationId:      aws.String(key.associationID),
		RoleArn:            aws.String(r.RoleARN),
		DisableSessionTags: aws.Bool(disableSessionTags),
		TargetRoleArn:      r.TargetRoleARN,
		ClientRequestToken: aws.String(id),
	}
	result, err := retryPodIdentityOperation(ctx, clock,
		func(ctx context.Context) (*eks.UpdatePodIdentityAssociationOutput, error) {
			return client.UpdatePodIdentityAssociation(ctx, request)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("update EKS Pod Identity association %s: %w", key, err)
	}
	if result == nil || result.Association == nil {
		return nil, fmt.Errorf("update EKS Pod Identity association %s: missing association", key)
	}
	output.DisableSessionTags = aws.ToBool(result.Association.DisableSessionTags)
	output.RoleARN = aws.ToString(result.Association.RoleArn)
	output.TargetRoleARN = result.Association.TargetRoleArn
	output.ExternalID = result.Association.ExternalId
	return &output, nil
}

func readPodIdentity(
	ctx context.Context, client *eks.Client, key podIdentityKey,
) (*PodIdentityAssociationResourceOutput, error) {
	result, err := client.DescribePodIdentityAssociation(ctx, &eks.DescribePodIdentityAssociationInput{
		ClusterName: aws.String(key.clusterName), AssociationId: aws.String(key.associationID),
	})
	var missing *ekstypes.ResourceNotFoundException
	switch {
	case errors.As(err, &missing):
		return nil, runtime.ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("describe EKS Pod Identity association %s: %w", key, err)
	case result == nil || result.Association == nil:
		return nil, runtime.ErrNotFound
	}
	return podIdentityOutput(result.Association), nil
}

func deletePodIdentity(ctx context.Context, client *eks.Client, key podIdentityKey) error {
	_, err := client.DeletePodIdentityAssociation(ctx, &eks.DeletePodIdentityAssociationInput{
		ClusterName: aws.String(key.clusterName), AssociationId: aws.String(key.associationID),
	})
	var missing *ekstypes.ResourceNotFoundException
	if err == nil || errors.As(err, &missing) {
		return nil
	}
	return fmt.Errorf("delete EKS Pod Identity association %s: %w", key, err)
}

func retryPodIdentityOperation[T any](
	ctx context.Context, clock clusterClock, operation func(context.Context) (T, error),
) (T, error) {
	retryCtx, cancel := context.WithTimeout(ctx, podIdentityPropagationTimeout)
	defer cancel()
	deadline := clock.Now().Add(podIdentityPropagationTimeout)
	var lastRoleError error
	var zero T
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return zero, err
		}
		if context.Cause(retryCtx) != nil || !clock.Now().Before(deadline) {
			break
		}
		result, err := operation(retryCtx)
		if err == nil {
			return result, nil
		}
		if cause := context.Cause(ctx); cause != nil {
			return zero, cause
		}
		if lastRoleError != nil && context.Cause(retryCtx) != nil &&
			errors.Is(err, context.DeadlineExceeded) {
			break
		}
		var invalid *ekstypes.InvalidParameterException
		if !errors.As(err, &invalid) || !strings.Contains(
			invalid.ErrorMessage(), "Role provided in the request does not exist",
		) {
			return result, err
		}
		lastRoleError = err
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			break
		}
		if err := clock.Sleep(retryCtx, min(remaining, clusterBackoff(attempt))); err != nil {
			if cause := context.Cause(ctx); cause != nil {
				return zero, cause
			}
			break
		}
	}
	if lastRoleError == nil {
		return zero, context.DeadlineExceeded
	}
	return zero, fmt.Errorf("role propagation timed out after %s: %w",
		podIdentityPropagationTimeout, lastRoleError)
}

func podIdentityOutput(
	value *ekstypes.PodIdentityAssociation,
) *PodIdentityAssociationResourceOutput {
	tags := clusterTags(value.Tags)
	if tags == nil {
		tags = map[string]string{}
	}
	return &PodIdentityAssociationResourceOutput{
		AssociationID:      aws.ToString(value.AssociationId),
		AssociationARN:     aws.ToString(value.AssociationArn),
		ClusterName:        aws.ToString(value.ClusterName),
		Namespace:          aws.ToString(value.Namespace),
		ServiceAccount:     aws.ToString(value.ServiceAccount),
		RoleARN:            aws.ToString(value.RoleArn),
		DisableSessionTags: aws.ToBool(value.DisableSessionTags),
		TargetRoleARN:      value.TargetRoleArn,
		ExternalID:         value.ExternalId,
		Tags:               tags,
	}
}

func syncPodIdentityTags(
	ctx context.Context, client *eks.Client, arn string, desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			result, err := client.ListTagsForResource(ctx, &eks.ListTagsForResourceInput{
				ResourceArn: aws.String(arn),
			})
			if err != nil || result == nil {
				return nil, err
			}
			return result.Tags, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &eks.TagResourceInput{
				ResourceArn: aws.String(arn), Tags: upsert,
			})
			return err
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &eks.UntagResourceInput{
				ResourceArn: aws.String(arn), TagKeys: remove,
			})
			return err
		},
	)
}
