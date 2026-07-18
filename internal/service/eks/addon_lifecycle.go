package eks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

type addonTokenSource func() (string, error)

func (r AddonResource) createAddon(
	ctx context.Context,
	client addonClient,
	clock clusterClock,
	tokens addonTokenSource,
) (*AddonResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	token, err := tokens()
	if err != nil {
		return nil, err
	}
	input := r.addonCreateInput(token)
	if err := retryAddonCreate(ctx, clock, func(ctx context.Context) error {
		_, err := client.CreateAddon(ctx, input)
		return err
	}); err != nil {
		return nil, fmt.Errorf("create add-on %s/%s: %w", r.ClusterName, r.AddonName, err)
	}
	output, err := finishAddonCreate(ctx, client, r.ClusterName, r.AddonName, clock)
	if err == nil {
		return output, nil
	}
	rollbackErr := rollbackAddonCreate(ctx, client, r.ClusterName, r.AddonName, clock)
	if rollbackErr != nil {
		return nil, errors.Join(err, fmt.Errorf("roll back accepted add-on create: %w", rollbackErr))
	}
	return nil, err
}

func finishAddonCreate(
	ctx context.Context,
	client addonClient,
	clusterName string,
	addonName string,
	clock clusterClock,
) (*AddonResourceOutput, error) {
	if err := waitAddonCreated(ctx, client, clusterName, addonName, clock); err != nil {
		return nil, err
	}
	return readAddonByName(ctx, client, clusterName, addonName)
}

func rollbackAddonCreate(
	ctx context.Context,
	client addonClient,
	clusterName string,
	addonName string,
	clock clusterClock,
) error {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), addonDeleteWaitTimeout,
	)
	defer cancel()
	return deleteAddonByName(cleanupCtx, client, clusterName, addonName, true, clock)
}

func retryAddonCreate(
	ctx context.Context,
	clock clusterClock,
	create func(context.Context) error,
) error {
	deadline := clock.Now().Add(addonCreateRetryTimeout)
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		err := create(ctx)
		if err == nil || !isAddonCreateRetryable(err) {
			return err
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("create retry timed out after %s: %w",
				addonCreateRetryTimeout, err)
		}
	}
}

func isAddonCreateRetryable(err error) bool {
	var invalid *ekstypes.InvalidParameterException
	if !errors.As(err, &invalid) {
		return false
	}
	message := invalid.ErrorMessage()
	return strings.Contains(message, "CREATE_FAILED") ||
		strings.Contains(message, "does not exist")
}

func (r AddonResource) readAddon(
	ctx context.Context,
	client addonClient,
	prior *AddonResourceOutput,
) (*AddonResourceOutput, error) {
	if prior == nil {
		return readAddonByName(ctx, client, r.ClusterName, r.AddonName)
	}
	clusterName, addonName, err := addonNames(prior)
	if err != nil {
		return nil, err
	}
	return readAddonByName(ctx, client, clusterName, addonName)
}

func readAddonByName(
	ctx context.Context,
	client addonClient,
	clusterName string,
	addonName string,
) (*AddonResourceOutput, error) {
	addon, err := describeAddon(ctx, client, clusterName, addonName)
	if err != nil {
		return nil, err
	}
	return addonOutput(addon), nil
}

func (r AddonResource) deleteAddon(
	ctx context.Context,
	client addonClient,
	prior *AddonResourceOutput,
	clock clusterClock,
) error {
	clusterName, addonName, err := addonNames(prior)
	if err != nil {
		return err
	}
	return deleteAddonByName(ctx, client, clusterName, addonName, r.Preserve, clock)
}

func deleteAddonByName(
	ctx context.Context,
	client addonClient,
	clusterName string,
	addonName string,
	preserve bool,
	clock clusterClock,
) error {
	_, err := client.DeleteAddon(ctx, &eks.DeleteAddonInput{
		AddonName: aws.String(addonName), ClusterName: aws.String(clusterName),
		Preserve: preserve,
	})
	if err != nil {
		if isAddonNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete add-on %s/%s: %w", clusterName, addonName, err)
	}
	return waitAddonDeleted(ctx, client, clusterName, addonName, clock)
}

func addonNames(output *AddonResourceOutput) (string, string, error) {
	if output == nil {
		return "", "", errors.New("prior add-on output is missing")
	}
	if output.ClusterName == "" {
		return "", "", errors.New("prior add-on output has no cluster name")
	}
	if output.AddonName == "" {
		return "", "", errors.New("prior add-on output has no add-on name")
	}
	return output.ClusterName, output.AddonName, nil
}

func addonOutput(addon *ekstypes.Addon) *AddonResourceOutput {
	output := &AddonResourceOutput{
		AddonName:    aws.ToString(addon.AddonName),
		AddonVersion: aws.ToString(addon.AddonVersion),
		ARN:          aws.ToString(addon.AddonArn),
		ClusterName:  aws.ToString(addon.ClusterName),
		Status:       string(addon.Status),
	}
	if addon.NamespaceConfig != nil {
		output.Namespace = copyStringPointer(addon.NamespaceConfig.Namespace)
	}
	if addon.CreatedAt != nil {
		output.CreatedAt = addon.CreatedAt.UTC().Format(time.RFC3339)
	}
	if addon.ModifiedAt != nil {
		output.ModifiedAt = addon.ModifiedAt.UTC().Format(time.RFC3339)
	}
	return output
}
