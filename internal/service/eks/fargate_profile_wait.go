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

const fargateProfileWaitTimeout = 10 * time.Minute

func describeFargateProfile(
	ctx context.Context,
	client fargateProfileClient,
	clusterName string,
	profileName string,
) (*ekstypes.FargateProfile, error) {
	output, err := client.DescribeFargateProfile(ctx, &eks.DescribeFargateProfileInput{
		ClusterName:        aws.String(clusterName),
		FargateProfileName: aws.String(profileName),
	})
	if err != nil {
		if isFargateProfileNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe Fargate profile %s/%s: %w",
			clusterName, profileName, err)
	}
	if output == nil || output.FargateProfile == nil {
		return nil, runtime.ErrNotFound
	}
	return output.FargateProfile, nil
}

func waitFargateProfileCreated(
	ctx context.Context,
	client fargateProfileClient,
	clusterName string,
	profileName string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(fargateProfileWaitTimeout)
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		profile, err := describeFargateProfile(ctx, client, clusterName, profileName)
		if errors.Is(err, runtime.ErrNotFound) {
		} else if err != nil {
			return err
		} else {
			switch profile.Status {
			case ekstypes.FargateProfileStatusActive:
				return nil
			case ekstypes.FargateProfileStatusCreating:
			default:
				return fargateProfileStatusError(clusterName, profileName, profile)
			}
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("waiting for Fargate profile %s/%s ACTIVE timed out after %s",
				clusterName, profileName, fargateProfileWaitTimeout)
		}
	}
}

func waitFargateProfileDeleted(
	ctx context.Context,
	client fargateProfileClient,
	clusterName string,
	profileName string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(fargateProfileWaitTimeout)
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		profile, err := describeFargateProfile(ctx, client, clusterName, profileName)
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		switch profile.Status {
		case ekstypes.FargateProfileStatusActive, ekstypes.FargateProfileStatusDeleting:
		default:
			return fargateProfileStatusError(clusterName, profileName, profile)
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("waiting for Fargate profile %s/%s deletion timed out after %s",
				clusterName, profileName, fargateProfileWaitTimeout)
		}
	}
}

func isFargateProfileNotFound(err error) bool {
	var notFound *ekstypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func fargateProfileStatusError(
	clusterName string,
	profileName string,
	profile *ekstypes.FargateProfile,
) error {
	return fmt.Errorf("fargate profile %s/%s reached unexpected status %s: %s",
		clusterName, profileName, profile.Status, formatFargateProfileIssues(profile.Health))
}

func formatFargateProfileIssues(health *ekstypes.FargateProfileHealth) string {
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
