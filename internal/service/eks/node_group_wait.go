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

const nodeGroupWaitTimeout = 60 * time.Minute

func describeNodeGroup(
	ctx context.Context,
	client interface {
		DescribeNodegroup(context.Context, *eks.DescribeNodegroupInput,
			...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error)
	},
	clusterName string,
	nodeGroupName string,
) (*ekstypes.Nodegroup, error) {
	output, err := client.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(nodeGroupName),
	})
	if err != nil {
		if isNodeGroupNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe node group %s/%s: %w",
			clusterName, nodeGroupName, err)
	}
	if output == nil || output.Nodegroup == nil {
		return nil, runtime.ErrNotFound
	}
	return output.Nodegroup, nil
}

func waitNodeGroupCreated(
	ctx context.Context,
	client nodeGroupClient,
	clusterName string,
	nodeGroupName string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(nodeGroupWaitTimeout)
	misses := 0
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		group, err := describeNodeGroup(ctx, client, clusterName, nodeGroupName)
		if errors.Is(err, runtime.ErrNotFound) {
			misses++
			if misses > 20 {
				return fmt.Errorf("node group %s/%s not found after 20 checks",
					clusterName, nodeGroupName)
			}
		} else if err != nil {
			return err
		} else {
			misses = 0
			switch group.Status {
			case ekstypes.NodegroupStatusActive:
				return nil
			case ekstypes.NodegroupStatusCreating:
			default:
				return nodeGroupStatusError(clusterName, nodeGroupName, group)
			}
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("waiting for node group %s/%s ACTIVE timed out after %s",
				clusterName, nodeGroupName, nodeGroupWaitTimeout)
		}
	}
}

func waitNodeGroupUpdate(
	ctx context.Context,
	client nodeGroupClient,
	clusterName string,
	nodeGroupName string,
	updateID string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(nodeGroupWaitTimeout)
	misses := 0
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		update, err := describeNodeGroupUpdate(
			ctx, client, clusterName, nodeGroupName, updateID,
		)
		if errors.Is(err, runtime.ErrNotFound) {
			misses++
			if misses > 20 {
				return fmt.Errorf("node group update %s not found after 20 checks", updateID)
			}
		} else if err != nil {
			return err
		} else {
			misses = 0
			switch update.Status {
			case ekstypes.UpdateStatusSuccessful:
				return nil
			case ekstypes.UpdateStatusInProgress:
			case ekstypes.UpdateStatusFailed, ekstypes.UpdateStatusCancelled:
				return fmt.Errorf("node group update %s is %s: %s",
					updateID, update.Status, formatUpdateErrors(update.Errors))
			default:
				return fmt.Errorf("node group update %s reached unexpected status %s",
					updateID, update.Status)
			}
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("waiting for node group update %s timed out after %s",
				updateID, nodeGroupWaitTimeout)
		}
	}
}

func describeNodeGroupUpdate(
	ctx context.Context,
	client interface {
		DescribeUpdate(context.Context, *eks.DescribeUpdateInput,
			...func(*eks.Options)) (*eks.DescribeUpdateOutput, error)
	},
	clusterName string,
	nodeGroupName string,
	updateID string,
) (*ekstypes.Update, error) {
	output, err := client.DescribeUpdate(ctx, &eks.DescribeUpdateInput{
		Name:          aws.String(clusterName),
		NodegroupName: aws.String(nodeGroupName),
		UpdateId:      aws.String(updateID),
	})
	if err != nil {
		if isNodeGroupNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe node group update %s: %w", updateID, err)
	}
	if output == nil || output.Update == nil {
		return nil, runtime.ErrNotFound
	}
	return output.Update, nil
}

func waitNodeGroupDeleted(
	ctx context.Context,
	client nodeGroupClient,
	clusterName string,
	nodeGroupName string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(nodeGroupWaitTimeout)
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		group, err := describeNodeGroup(ctx, client, clusterName, nodeGroupName)
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		switch group.Status {
		case ekstypes.NodegroupStatusActive, ekstypes.NodegroupStatusDeleting:
		default:
			return nodeGroupStatusError(clusterName, nodeGroupName, group)
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("waiting for node group %s/%s deletion timed out after %s",
				clusterName, nodeGroupName, nodeGroupWaitTimeout)
		}
	}
}

func isNodeGroupNotFound(err error) bool {
	var notFound *ekstypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func nodeGroupStatusError(
	clusterName string,
	nodeGroupName string,
	group *ekstypes.Nodegroup,
) error {
	return fmt.Errorf("node group %s/%s reached unexpected status %s: %s",
		clusterName, nodeGroupName, group.Status, formatNodeGroupIssues(group.Health))
}

func formatNodeGroupIssues(health *ekstypes.NodegroupHealth) string {
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
