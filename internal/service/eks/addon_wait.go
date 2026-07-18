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
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	addonCreateRetryTimeout = 2 * time.Minute
	addonCreateWaitTimeout  = 20 * time.Minute
	addonUpdateWaitTimeout  = 20 * time.Minute
	addonDeleteWaitTimeout  = 40 * time.Minute
)

func describeAddon(
	ctx context.Context,
	client addonClient,
	clusterName string,
	addonName string,
) (*ekstypes.Addon, error) {
	output, err := client.DescribeAddon(ctx, &eks.DescribeAddonInput{
		AddonName: aws.String(addonName), ClusterName: aws.String(clusterName),
	})
	if err != nil {
		if isAddonNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe add-on %s/%s: %w", clusterName, addonName, err)
	}
	if output == nil || output.Addon == nil {
		return nil, runtime.ErrNotFound
	}
	return output.Addon, nil
}

func waitAddonCreated(
	ctx context.Context,
	client addonClient,
	clusterName string,
	addonName string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(addonCreateWaitTimeout)
	misses := 0
	var last *ekstypes.Addon
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		addon, err := describeAddon(ctx, client, clusterName, addonName)
		if errors.Is(err, runtime.ErrNotFound) {
			misses++
			if misses > 20 {
				return fmt.Errorf("add-on %s/%s not found after 20 checks",
					clusterName, addonName)
			}
		} else if err != nil {
			return err
		} else {
			misses = 0
			last = addon
			switch addon.Status {
			case ekstypes.AddonStatusActive:
				return nil
			case ekstypes.AddonStatusCreating, ekstypes.AddonStatusDegraded:
			default:
				return addonStatusError(clusterName, addonName, addon)
			}
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			if last != nil && last.Status == ekstypes.AddonStatusDegraded {
				return fmt.Errorf("waiting for add-on %s/%s ACTIVE timed out after %s: %s",
					clusterName, addonName, addonCreateWaitTimeout,
					formatAddonIssues(last.Health))
			}
			return fmt.Errorf("waiting for add-on %s/%s ACTIVE timed out after %s",
				clusterName, addonName, addonCreateWaitTimeout)
		}
	}
}

func waitAddonUpdate(
	ctx context.Context,
	client addonClient,
	clusterName string,
	addonName string,
	updateID string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(addonUpdateWaitTimeout)
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		update, err := describeAddonUpdate(ctx, client, clusterName, addonName, updateID)
		if err != nil {
			return err
		}
		switch update.Status {
		case ekstypes.UpdateStatusSuccessful:
			return nil
		case ekstypes.UpdateStatusInProgress:
		case ekstypes.UpdateStatusCancelled, ekstypes.UpdateStatusFailed:
			return fmt.Errorf("add-on update %s is %s: %s",
				updateID, update.Status, formatUpdateErrors(update.Errors))
		default:
			return fmt.Errorf("add-on update %s reached unexpected status %s",
				updateID, update.Status)
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("waiting for add-on update %s timed out after %s",
				updateID, addonUpdateWaitTimeout)
		}
	}
}

func describeAddonUpdate(
	ctx context.Context,
	client addonClient,
	clusterName string,
	addonName string,
	updateID string,
) (*ekstypes.Update, error) {
	output, err := client.DescribeUpdate(ctx, &eks.DescribeUpdateInput{
		AddonName: aws.String(addonName),
		Name:      aws.String(clusterName),
		UpdateId:  aws.String(updateID),
	})
	if err != nil {
		if isAddonNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe add-on update %s: %w", updateID, err)
	}
	if output == nil || output.Update == nil {
		return nil, runtime.ErrNotFound
	}
	return output.Update, nil
}

func waitAddonDeleted(
	ctx context.Context,
	client addonClient,
	clusterName string,
	addonName string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(addonDeleteWaitTimeout)
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		addon, err := describeAddon(ctx, client, clusterName, addonName)
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		switch addon.Status {
		case ekstypes.AddonStatusActive, ekstypes.AddonStatusDeleting:
		default:
			return addonStatusError(clusterName, addonName, addon)
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("waiting for add-on %s/%s deletion timed out after %s",
				clusterName, addonName, addonDeleteWaitTimeout)
		}
	}
}

func isAddonNotFound(err error) bool {
	var notFound *ekstypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func addonStatusError(clusterName string, addonName string, addon *ekstypes.Addon) error {
	return fmt.Errorf("add-on %s/%s reached unexpected status %s: %s",
		clusterName, addonName, addon.Status, formatAddonIssues(addon.Health))
}

func formatAddonIssues(health *ekstypes.AddonHealth) string {
	if health == nil || len(health.Issues) == 0 {
		return "no health issues"
	}
	formatted := make([]string, 0, len(health.Issues))
	for _, issue := range health.Issues {
		prefix := ""
		if len(issue.ResourceIds) > 0 {
			prefix = strings.Join(issue.ResourceIds, ", ") + ": "
		}
		formatted = append(formatted, fmt.Sprintf("%s%s: %s",
			prefix, issue.Code, aws.ToString(issue.Message)))
	}
	return strings.Join(formatted, "; ")
}
