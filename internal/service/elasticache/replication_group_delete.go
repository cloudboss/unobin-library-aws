package elasticache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	replicationGroupDeleteRetryTimeout   = 10 * time.Minute
	globalReplicationGroupDetachTimeout  = 45 * time.Minute
	globalReplicationGroupDetachInterval = 5 * time.Second
	globalMemberAbsentWaitTimeout        = 45 * time.Minute
	parameterGroupDeleteRetryTimeout     = 3 * time.Minute
)

func (r *ReplicationGroupResource) delete(
	ctx context.Context,
	client replicationGroupClient,
	region string,
	prior *ReplicationGroupResourceOutput,
	options replicationGroupOperationOptions,
) error {
	id := replicationGroupIdentity(r.ReplicationGroupID, prior)
	globalID := ""
	parameterGroupName := ""
	if prior != nil {
		globalID = prior.GlobalReplicationGroupID
		parameterGroupName = prior.ParameterGroupName
	}
	if globalID != "" {
		if err := disassociateReplicationGroupFromGlobal(ctx, client,
			globalID, id, region, options.retryOptions...); err != nil {
			return err
		}
		if err := waitGlobalReplicationGroupMemberAbsent(ctx, client,
			globalID, id, options.waitOptions...); err != nil {
			return err
		}
	}
	retryOptions := append([]retry.Option{
		retry.WithTimeout(replicationGroupDeleteRetryTimeout),
	}, options.retryOptions...)
	err := retry.OnError(ctx, invalidReplicationGroupState,
		func(ctx context.Context) error {
			_, err := client.DeleteReplicationGroup(ctx,
				&elasticache.DeleteReplicationGroupInput{
					ReplicationGroupId:      aws.String(id),
					FinalSnapshotIdentifier: r.FinalSnapshotIdentifier,
				})
			return err
		}, retryOptions...)
	if err != nil && !replicationGroupNotFound(err) {
		return fmt.Errorf("delete replication group: %w", err)
	}
	if err == nil {
		if err := waitReplicationGroupDeleted(ctx, client, id,
			options.waitOptions...); err != nil {
			return err
		}
	}
	if globalID != "" && parameterGroupName != "" {
		if err := deleteGeneratedParameterGroup(ctx, client, parameterGroupName,
			options.retryOptions...); err != nil {
			return err
		}
	}
	return nil
}

func disassociateReplicationGroupFromGlobal(
	ctx context.Context,
	client replicationGroupClient,
	globalID string,
	replicationGroupID string,
	region string,
	options ...retry.Option,
) error {
	retryOptions := append([]retry.Option{
		retry.WithTimeout(globalReplicationGroupDetachTimeout),
		retry.WithInterval(globalReplicationGroupDetachInterval),
	}, options...)
	err := retry.OnError(ctx, invalidGlobalReplicationGroupState,
		func(ctx context.Context) error {
			_, err := client.DisassociateGlobalReplicationGroup(ctx,
				&elasticache.DisassociateGlobalReplicationGroupInput{
					GlobalReplicationGroupId: aws.String(globalID),
					ReplicationGroupId:       aws.String(replicationGroupID),
					ReplicationGroupRegion:   aws.String(region),
				})
			return err
		}, retryOptions...)
	if err != nil {
		if globalReplicationGroupNotFound(err) || replicationGroupNotFound(err) ||
			globalMemberAlreadyAbsent(err) {
			return nil
		}
		return fmt.Errorf("disassociate global replication group: %w", err)
	}
	return nil
}

func waitGlobalReplicationGroupMemberAbsent(
	ctx context.Context,
	client replicationGroupClient,
	globalID string,
	replicationGroupID string,
	options ...wait.Option,
) error {
	waitOptions := append([]wait.Option{
		wait.WithTimeout(globalMemberAbsentWaitTimeout),
		wait.WithInterval(replicationGroupWaitInterval),
	}, options...)
	what := fmt.Sprintf("replication group %s to leave global group %s",
		replicationGroupID, globalID)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		globalGroup, err := findGlobalReplicationGroup(ctx, client, globalID)
		if err != nil {
			if errors.Is(err, runtime.ErrNotFound) {
				return true, nil
			}
			return false, err
		}
		for _, member := range globalGroup.Members {
			if strings.EqualFold(aws.ToString(member.ReplicationGroupId),
				replicationGroupID) {
				return false, nil
			}
		}
		return true, nil
	}, waitOptions...)
}

func waitReplicationGroupDeleted(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	options ...wait.Option,
) error {
	waitOptions := append([]wait.Option{
		wait.WithTimeout(replicationGroupDeleteTimeout),
		wait.WithInterval(replicationGroupWaitInterval),
	}, options...)
	what := fmt.Sprintf("replication group %s to be deleted", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		group, err := findReplicationGroup(ctx, client, id)
		if err != nil {
			if errors.Is(err, runtime.ErrNotFound) {
				return true, nil
			}
			return false, err
		}
		status := aws.ToString(group.Status)
		if status == "available" || status == "deleting" || status == "modifying" ||
			status == "snapshotting" {
			return false, nil
		}
		return false, fmt.Errorf("replication group %s entered unexpected status %q",
			id, status)
	}, waitOptions...)
}

func deleteGeneratedParameterGroup(
	ctx context.Context,
	client replicationGroupClient,
	name string,
	options ...retry.Option,
) error {
	retryOptions := append([]retry.Option{
		retry.WithTimeout(parameterGroupDeleteRetryTimeout),
	}, options...)
	err := retry.OnError(ctx, invalidCacheParameterGroupState,
		func(ctx context.Context) error {
			_, err := client.DeleteCacheParameterGroup(ctx,
				&elasticache.DeleteCacheParameterGroupInput{
					CacheParameterGroupName: aws.String(name),
				})
			return err
		}, retryOptions...)
	if err != nil {
		var notFound *elasticachetypes.CacheParameterGroupNotFoundFault
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("delete generated cache parameter group: %w", err)
	}
	return nil
}

func invalidReplicationGroupState(err error) bool {
	var invalid *elasticachetypes.InvalidReplicationGroupStateFault
	return errors.As(err, &invalid)
}

func invalidGlobalReplicationGroupState(err error) bool {
	var invalid *elasticachetypes.InvalidGlobalReplicationGroupStateFault
	return errors.As(err, &invalid)
}

func invalidCacheParameterGroupState(err error) bool {
	var invalid *elasticachetypes.InvalidCacheParameterGroupStateFault
	return errors.As(err, &invalid)
}

func globalMemberAlreadyAbsent(err error) bool {
	var invalid *elasticachetypes.InvalidParameterValueException
	if !errors.As(err, &invalid) {
		return false
	}
	message := strings.ToLower(invalid.ErrorMessage())
	return strings.Contains(message, "not found") ||
		strings.Contains(message, "not part") ||
		strings.Contains(message, "not a member")
}
