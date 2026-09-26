package elasticache

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

func (r *ReplicationGroupResource) update(
	ctx context.Context,
	client replicationGroupClient,
	prior runtime.Prior[ReplicationGroupResource, *ReplicationGroupResourceOutput, *awsCfg],
	options replicationGroupOperationOptions,
) (*ReplicationGroupResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	observed := prior.Observed
	if observed == nil {
		observed = prior.Outputs
	}
	if err := r.validateUpdateTransitions(prior, observed); err != nil {
		return nil, err
	}
	id := replicationGroupIdentity(r.ReplicationGroupID, prior.Outputs)
	if runtime.Changed(valueOrEmptyMap(prior.Inputs.Tags), valueOrEmptyMap(r.Tags)) {
		if prior.Outputs == nil || prior.Outputs.Arn == "" {
			return nil, errors.New("update replication group: prior ARN is missing")
		}
		retryOptions := append([]retry.Option{retry.WithTimeout(replicationGroupTagTimeout)},
			options.retryOptions...)
		if err := syncSubnetGroupTags(ctx, client, prior.Outputs.Arn,
			valueOrEmptyMap(r.Tags), retryOptions...); err != nil {
			return nil, err
		}
	}
	if observed == nil {
		return nil, errors.New("update replication group: observed outputs are missing")
	}
	if r.NumNodeGroups != nil && *r.NumNodeGroups != observed.NumNodeGroups {
		if err := r.updateShardCount(ctx, client, id, *r.NumNodeGroups, options); err != nil {
			return nil, err
		}
	}
	if r.ReplicasPerNodeGroup != nil &&
		*r.ReplicasPerNodeGroup != observed.ReplicasPerNodeGroup {
		if err := r.updateReplicasPerShard(ctx, client, id,
			*r.ReplicasPerNodeGroup, observed.ReplicasPerNodeGroup, options); err != nil {
			return nil, err
		}
	}
	if r.NumCacheClusters != nil && *r.NumCacheClusters > observed.NumCacheClusters {
		if err := r.updateNonclusteredReplicaCount(ctx, client, id,
			*r.NumCacheClusters, true, options); err != nil {
			return nil, err
		}
	}
	preupgraded := r.needsSlowLogEnginePreupgrade(prior, observed)
	if preupgraded {
		if err := r.preupgradeEngineForLogs(ctx, client, id, options); err != nil {
			return nil, err
		}
	}
	promoting := r.PrimaryClusterID != nil &&
		runtime.Changed(prior.Inputs.PrimaryClusterID, r.PrimaryClusterID)
	if err := r.applyStandardModify(ctx, client, id, prior, preupgraded, promoting,
		options); err != nil {
		return nil, err
	}
	if promoting {
		if err := r.promotePrimaryCluster(ctx, client, id, options); err != nil {
			return nil, err
		}
	}
	if r.authUpdateNeeded(prior.Inputs) {
		if err := r.applyAuthUpdate(ctx, client, id, prior.Inputs, options); err != nil {
			return nil, err
		}
	}
	if r.NumCacheClusters != nil && *r.NumCacheClusters < observed.NumCacheClusters {
		if err := r.updateNonclusteredReplicaCount(ctx, client, id,
			*r.NumCacheClusters, false, options); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, id, options)
}

func (r *ReplicationGroupResource) validateUpdateTransitions(
	prior runtime.Prior[ReplicationGroupResource, *ReplicationGroupResourceOutput, *awsCfg],
	observed *ReplicationGroupResourceOutput,
) error {
	priorEngine := prior.Inputs.Engine
	if priorEngine == "" {
		priorEngine = "redis"
	}
	if priorEngine != r.Engine {
		if priorEngine != "redis" || r.Engine != "valkey" {
			return fmt.Errorf("engine change from %s to %s requires replacement",
				priorEngine, r.Engine)
		}
		if r.EngineVersion == nil ||
			!runtime.Changed(prior.Inputs.EngineVersion, r.EngineVersion) {
			return errors.New("redis to valkey requires an explicit changed engine-version")
		}
	}
	if runtime.Changed(prior.Inputs.EngineVersion, r.EngineVersion) &&
		r.EngineVersion != nil && observed != nil && observed.EngineVersion != "" &&
		compareEngineVersions(*r.EngineVersion, observed.EngineVersion) < 0 {
		return fmt.Errorf("engine-version downgrade from %s to %s requires replacement",
			observed.EngineVersion, *r.EngineVersion)
	}
	if runtime.Changed(prior.Inputs.ClusterMode, r.ClusterMode) && r.ClusterMode != nil {
		priorMode := "disabled"
		if prior.Inputs.ClusterMode != nil {
			priorMode = *prior.Inputs.ClusterMode
		}
		allowed := priorMode == "disabled" && *r.ClusterMode == "compatible" ||
			priorMode == "compatible" && *r.ClusterMode == "enabled"
		if !allowed {
			return fmt.Errorf("cluster-mode transition from %s to %s is not supported",
				priorMode, *r.ClusterMode)
		}
	}
	if runtime.Changed(prior.Inputs.TransitEncryptionEnabled,
		r.TransitEncryptionEnabled) {
		if observed != nil && observed.EngineVersion != "" &&
			compareEngineVersions(observed.EngineVersion, "7.0.5") < 0 {
			return fmt.Errorf("changing transit-encryption-enabled on engine version %s "+
				"requires replacement", observed.EngineVersion)
		}
		if r.TransitEncryptionEnabled &&
			(r.TransitEncryptionMode == nil || *r.TransitEncryptionMode != "preferred") {
			return errors.New(
				"enabling transit encryption requires transit-encryption-mode preferred")
		}
	}
	if r.authUpdateNeeded(prior.Inputs) {
		added, _ := stringSetDiff(valueOrZero(prior.Inputs.UserGroupIDs),
			valueOrZero(r.UserGroupIDs))
		strategy := normalizedAuthTokenUpdateStrategy(r.AuthTokenUpdateStrategy)
		switch strategy {
		case "SET", "ROTATE":
			if r.AuthToken == nil {
				return fmt.Errorf("auth-token-update-strategy %s requires auth-token",
					strategy)
			}
		case "DELETE":
			if r.AuthToken != nil {
				return errors.New("auth-token-update-strategy DELETE forbids auth-token")
			}
			if len(added) == 0 {
				return errors.New(
					"auth-token-update-strategy DELETE requires a replacement user group")
			}
		}
	}
	return nil
}

func (r *ReplicationGroupResource) updateShardCount(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	desired int64,
	options replicationGroupOperationOptions,
) error {
	return runReplicationGroupOperation(ctx, client, id, options,
		func(ctx context.Context, group *elasticachetypes.ReplicationGroup) error {
			in := &elasticache.ModifyReplicationGroupShardConfigurationInput{
				ApplyImmediately:   aws.Bool(true),
				NodeGroupCount:     aws.Int32(int32(desired)),
				ReplicationGroupId: aws.String(id),
			}
			if desired < int64(len(group.NodeGroups)) {
				ids := make([]string, 0, len(group.NodeGroups))
				for _, nodeGroup := range group.NodeGroups {
					id := aws.ToString(nodeGroup.NodeGroupId)
					if id == "" {
						return errors.New("scale down replication group: node group ID is missing")
					}
					ids = append(ids, id)
				}
				slices.Sort(ids)
				removeCount := len(ids) - int(desired)
				in.NodeGroupsToRemove = append([]string(nil), ids[len(ids)-removeCount:]...)
			}
			_, err := client.ModifyReplicationGroupShardConfiguration(ctx, in)
			if err != nil {
				return fmt.Errorf("modify replication group shard configuration: %w", err)
			}
			return nil
		})
}

func (r *ReplicationGroupResource) updateReplicasPerShard(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	desired int64,
	observed int64,
	options replicationGroupOperationOptions,
) error {
	return runReplicationGroupReplicaOperation(ctx, client, id, options,
		func(ctx context.Context, _ *elasticachetypes.ReplicationGroup) error {
			if desired > observed {
				_, err := client.IncreaseReplicaCount(ctx, &elasticache.IncreaseReplicaCountInput{
					ApplyImmediately:   aws.Bool(true),
					NewReplicaCount:    aws.Int32(int32(desired)),
					ReplicationGroupId: aws.String(id),
				})
				if err != nil {
					return fmt.Errorf("increase replication group replicas: %w", err)
				}
				return nil
			}
			_, err := client.DecreaseReplicaCount(ctx, &elasticache.DecreaseReplicaCountInput{
				ApplyImmediately:   aws.Bool(true),
				NewReplicaCount:    aws.Int32(int32(desired)),
				ReplicationGroupId: aws.String(id),
			})
			if err != nil {
				return fmt.Errorf("decrease replication group replicas: %w", err)
			}
			return nil
		})
}

func (r *ReplicationGroupResource) updateNonclusteredReplicaCount(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	desiredTotal int64,
	increase bool,
	options replicationGroupOperationOptions,
) error {
	return runReplicationGroupReplicaOperation(ctx, client, id, options,
		func(ctx context.Context, _ *elasticachetypes.ReplicationGroup) error {
			desiredReplicas := int32(desiredTotal - 1)
			if increase {
				_, err := client.IncreaseReplicaCount(ctx, &elasticache.IncreaseReplicaCountInput{
					ApplyImmediately:   aws.Bool(true),
					NewReplicaCount:    aws.Int32(desiredReplicas),
					ReplicationGroupId: aws.String(id),
				})
				if err != nil {
					return fmt.Errorf("increase replication group replicas: %w", err)
				}
				return nil
			}
			_, err := client.DecreaseReplicaCount(ctx, &elasticache.DecreaseReplicaCountInput{
				ApplyImmediately:   aws.Bool(true),
				NewReplicaCount:    aws.Int32(desiredReplicas),
				ReplicationGroupId: aws.String(id),
			})
			if err != nil {
				return fmt.Errorf("decrease replication group replicas: %w", err)
			}
			return nil
		})
}

func runReplicationGroupOperation(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	options replicationGroupOperationOptions,
	operation func(context.Context, *elasticachetypes.ReplicationGroup) error,
) error {
	_, err := runReplicationGroupOperationResult(ctx, client, id, options, operation)
	return err
}

func runReplicationGroupReplicaOperation(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	options replicationGroupOperationOptions,
	operation func(context.Context, *elasticachetypes.ReplicationGroup) error,
) error {
	group, err := runReplicationGroupOperationResult(ctx, client, id, options, operation)
	if err != nil {
		return err
	}
	return waitReplicationGroupMembersAvailable(ctx, client, group,
		options.waitOptions...)
}

func runReplicationGroupOperationResult(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	options replicationGroupOperationOptions,
	operation func(context.Context, *elasticachetypes.ReplicationGroup) error,
) (*elasticachetypes.ReplicationGroup, error) {
	group, err := waitReplicationGroupAvailable(ctx, client, id, false,
		options.waitOptions...)
	if err != nil {
		return nil, err
	}
	if err := operation(ctx, group); err != nil {
		return nil, err
	}
	return waitReplicationGroupAvailable(ctx, client, id, false,
		options.waitOptions...)
}

func waitReplicationGroupMembersAvailable(
	ctx context.Context,
	client replicationGroupClient,
	group *elasticachetypes.ReplicationGroup,
	options ...wait.Option,
) error {
	for _, id := range group.MemberClusters {
		if err := waitReplicationGroupMemberAvailable(ctx, client, id, options...); err != nil {
			return err
		}
	}
	return nil
}

func waitReplicationGroupMemberAvailable(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	options ...wait.Option,
) error {
	waitOptions := append([]wait.Option{
		wait.WithTimeout(replicationGroupSettleTimeout),
		wait.WithInterval(replicationGroupWaitInterval),
	}, options...)
	what := fmt.Sprintf("replication group member cache cluster %s to be available", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		cluster, err := findReplicationGroupCacheCluster(ctx, client, id)
		if err != nil {
			return false, err
		}
		status := aws.ToString(cluster.CacheClusterStatus)
		switch status {
		case "available":
			return true, nil
		case "creating", "deleting", "modifying", "snapshotting":
			return false, nil
		default:
			return false, fmt.Errorf("cache cluster %s entered unexpected status %q",
				id, status)
		}
	}, waitOptions...)
}

func (r *ReplicationGroupResource) needsSlowLogEnginePreupgrade(
	prior runtime.Prior[ReplicationGroupResource, *ReplicationGroupResourceOutput, *awsCfg],
	observed *ReplicationGroupResourceOutput,
) bool {
	if r.EngineVersion == nil || observed == nil || observed.EngineVersion == "" ||
		!runtime.Changed(prior.Inputs.EngineVersion, r.EngineVersion) ||
		!runtime.Changed(prior.Inputs.LogDeliveryConfiguration,
			r.LogDeliveryConfiguration) {
		return false
	}
	priorSlow := hasReplicationGroupLogType(
		valueOrZero(prior.Inputs.LogDeliveryConfiguration), "slow-log")
	desiredSlow := hasReplicationGroupLogType(
		valueOrZero(r.LogDeliveryConfiguration), "slow-log")
	if priorSlow || !desiredSlow {
		return false
	}
	return !engineSupportsSlowLog(prior.Inputs.Engine, observed.EngineVersion) &&
		engineSupportsSlowLog(r.Engine, *r.EngineVersion)
}

func (r *ReplicationGroupResource) preupgradeEngineForLogs(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	options replicationGroupOperationOptions,
) error {
	return runReplicationGroupOperation(ctx, client, id, options,
		func(ctx context.Context, _ *elasticachetypes.ReplicationGroup) error {
			in := &elasticache.ModifyReplicationGroupInput{
				ReplicationGroupId: aws.String(id),
				ApplyImmediately:   aws.Bool(true),
				EngineVersion:      r.EngineVersion,
			}
			if r.Engine == "valkey" {
				in.Engine = aws.String(r.Engine)
			}
			return modifyReplicationGroupAcceptedNoop(ctx, client, in)
		})
}

func (r *ReplicationGroupResource) applyStandardModify(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	prior runtime.Prior[ReplicationGroupResource, *ReplicationGroupResourceOutput, *awsCfg],
	preupgraded bool,
	promoting bool,
	options replicationGroupOperationOptions,
) error {
	_, needed := r.standardModifyInput(id, prior.Inputs, nil, preupgraded, promoting)
	if !needed {
		return nil
	}
	return runReplicationGroupOperation(ctx, client, id, options,
		func(ctx context.Context, group *elasticachetypes.ReplicationGroup) error {
			in, needed := r.standardModifyInput(
				id, prior.Inputs, group, preupgraded, promoting)
			if !needed {
				return nil
			}
			if r.SnapshotRetentionLimit != nil && *r.SnapshotRetentionLimit > 0 &&
				r.SnapshottingClusterID == nil && !replicationGroupClustered(group) &&
				in.SnapshottingClusterId == nil {
				return errors.New(
					"enable snapshot retention: replication group has no primary member")
			}
			return modifyReplicationGroupAcceptedNoop(ctx, client, in)
		})
}

func (r *ReplicationGroupResource) standardModifyInput(
	id string,
	prior ReplicationGroupResource,
	group *elasticachetypes.ReplicationGroup,
	preupgraded bool,
	promoting bool,
) (*elasticache.ModifyReplicationGroupInput, bool) {
	in := &elasticache.ModifyReplicationGroupInput{
		ReplicationGroupId: aws.String(id),
		ApplyImmediately:   aws.Bool(true),
	}
	changed := false
	if runtime.Changed(prior.Description, r.Description) {
		in.ReplicationGroupDescription = aws.String(r.Description)
		changed = true
	}
	if r.AutoMinorVersionUpgrade != nil &&
		runtime.Changed(prior.AutoMinorVersionUpgrade, r.AutoMinorVersionUpgrade) {
		in.AutoMinorVersionUpgrade = r.AutoMinorVersionUpgrade
		changed = true
	}
	globalMember := r.GlobalReplicationGroupID != nil
	if !globalMember && !promoting &&
		runtime.Changed(prior.AutomaticFailoverEnabled, r.AutomaticFailoverEnabled) {
		in.AutomaticFailoverEnabled = aws.Bool(r.AutomaticFailoverEnabled)
		changed = true
	}
	if !globalMember && !promoting && r.MultiAZEnabled != nil &&
		runtime.Changed(prior.MultiAZEnabled, r.MultiAZEnabled) {
		in.MultiAZEnabled = r.MultiAZEnabled
		changed = true
	}
	if r.ClusterMode != nil && runtime.Changed(prior.ClusterMode, r.ClusterMode) {
		in.ClusterMode = elasticachetypes.ClusterMode(*r.ClusterMode)
		changed = true
	}
	if r.Durability != nil && runtime.Changed(prior.Durability, r.Durability) {
		in.Durability = elasticachetypes.Durability(*r.Durability)
		changed = true
	}
	if !globalMember && !preupgraded && runtime.Changed(prior.Engine, r.Engine) {
		in.Engine = aws.String(r.Engine)
		changed = true
	}
	if !globalMember && !preupgraded && r.EngineVersion != nil &&
		runtime.Changed(prior.EngineVersion, r.EngineVersion) {
		in.EngineVersion = r.EngineVersion
		changed = true
	}
	if r.IPDiscovery != nil && runtime.Changed(prior.IPDiscovery, r.IPDiscovery) {
		in.IpDiscovery = elasticachetypes.IpDiscovery(*r.IPDiscovery)
		changed = true
	}
	if !globalMember && r.NodeType != nil && runtime.Changed(prior.NodeType, r.NodeType) {
		in.CacheNodeType = r.NodeType
		changed = true
	}
	if runtime.Changed(prior.NotificationTopicArn, r.NotificationTopicArn) {
		if r.NotificationTopicArn == nil {
			in.NotificationTopicArn = aws.String("")
		} else {
			in.NotificationTopicArn = r.NotificationTopicArn
		}
		changed = true
	}
	if !globalMember && r.ParameterGroupName != nil &&
		runtime.Changed(prior.ParameterGroupName, r.ParameterGroupName) {
		in.CacheParameterGroupName = r.ParameterGroupName
		changed = true
	}
	if r.SecurityGroupNames != nil &&
		runtime.Changed(prior.SecurityGroupNames, r.SecurityGroupNames) {
		in.CacheSecurityGroupNames = *r.SecurityGroupNames
		changed = true
	}
	if r.SecurityGroupIDs != nil && runtime.Changed(prior.SecurityGroupIDs, r.SecurityGroupIDs) {
		in.SecurityGroupIds = *r.SecurityGroupIDs
		changed = true
	}
	if r.MaintenanceWindow != nil &&
		runtime.Changed(prior.MaintenanceWindow, r.MaintenanceWindow) {
		in.PreferredMaintenanceWindow = r.MaintenanceWindow
		changed = true
	}
	if runtime.Changed(prior.SnapshotRetentionLimit, r.SnapshotRetentionLimit) {
		if r.SnapshotRetentionLimit == nil {
			in.SnapshotRetentionLimit = aws.Int32(0)
		} else {
			in.SnapshotRetentionLimit = ptr.Int32(r.SnapshotRetentionLimit)
		}
		changed = true
		if r.SnapshotRetentionLimit != nil && *r.SnapshotRetentionLimit > 0 &&
			r.SnapshottingClusterID == nil && !replicationGroupClustered(group) {
			in.SnapshottingClusterId = replicationGroupPrimaryMember(group)
		}
	}
	if r.SnapshotWindow != nil && runtime.Changed(prior.SnapshotWindow, r.SnapshotWindow) {
		in.SnapshotWindow = r.SnapshotWindow
		changed = true
	}
	if r.SnapshottingClusterID != nil &&
		runtime.Changed(prior.SnapshottingClusterID, r.SnapshottingClusterID) {
		in.SnapshottingClusterId = r.SnapshottingClusterID
		changed = true
	}
	if !globalMember && runtime.Changed(prior.TransitEncryptionEnabled,
		r.TransitEncryptionEnabled) {
		in.TransitEncryptionEnabled = aws.Bool(r.TransitEncryptionEnabled)
		changed = true
	}
	if !globalMember && r.TransitEncryptionMode != nil &&
		runtime.Changed(prior.TransitEncryptionMode, r.TransitEncryptionMode) {
		in.TransitEncryptionMode = elasticachetypes.TransitEncryptionMode(
			*r.TransitEncryptionMode)
		changed = true
	}
	if runtime.Changed(
		prior.LogDeliveryConfiguration, r.LogDeliveryConfiguration) {
		in.LogDeliveryConfigurations = replicationGroupLogDiff(
			valueOrZero(prior.LogDeliveryConfiguration),
			valueOrZero(r.LogDeliveryConfiguration))
		changed = true
	}
	if normalizedAuthTokenUpdateStrategy(r.AuthTokenUpdateStrategy) != "DELETE" &&
		runtime.Changed(prior.UserGroupIDs, r.UserGroupIDs) {
		in.UserGroupIdsToAdd, in.UserGroupIdsToRemove = stringSetDiff(
			valueOrZero(prior.UserGroupIDs), valueOrZero(r.UserGroupIDs))
		changed = true
	}
	return in, changed
}

func replicationGroupClustered(group *elasticachetypes.ReplicationGroup) bool {
	return group != nil && aws.ToBool(group.ClusterEnabled)
}

func replicationGroupPrimaryMember(group *elasticachetypes.ReplicationGroup) *string {
	if group == nil || len(group.NodeGroups) == 0 {
		return nil
	}
	for _, member := range group.NodeGroups[0].NodeGroupMembers {
		if member.CurrentRole == nil || *member.CurrentRole != "primary" {
			continue
		}
		return member.CacheClusterId
	}
	return nil
}

func (r *ReplicationGroupResource) promotePrimaryCluster(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	options replicationGroupOperationOptions,
) error {
	group, err := waitReplicationGroupAvailable(ctx, client, id, false,
		options.waitOptions...)
	if err != nil {
		return err
	}
	wasFailoverEnabled := group.AutomaticFailover ==
		elasticachetypes.AutomaticFailoverStatusEnabled
	wasMultiAZEnabled := group.MultiAZ == elasticachetypes.MultiAZStatusEnabled
	if wasFailoverEnabled {
		if err := strictReplicationGroupModify(ctx, client,
			&elasticache.ModifyReplicationGroupInput{
				ReplicationGroupId:       aws.String(id),
				ApplyImmediately:         aws.Bool(true),
				AutomaticFailoverEnabled: aws.Bool(false),
				MultiAZEnabled:           aws.Bool(false),
			}); err != nil {
			return err
		}
		if _, err := waitReplicationGroupAvailable(ctx, client, id, false,
			options.waitOptions...); err != nil {
			return err
		}
	}
	if _, err := waitReplicationGroupAvailable(ctx, client, id, false,
		options.waitOptions...); err != nil {
		return err
	}
	if err := strictReplicationGroupModify(ctx, client,
		&elasticache.ModifyReplicationGroupInput{
			ReplicationGroupId: aws.String(id),
			ApplyImmediately:   aws.Bool(true),
			PrimaryClusterId:   r.PrimaryClusterID,
		}); err != nil {
		return err
	}
	if _, err := waitReplicationGroupAvailable(ctx, client, id, false,
		options.waitOptions...); err != nil {
		return err
	}
	desiredMultiAZ := wasMultiAZEnabled
	if r.MultiAZEnabled != nil {
		desiredMultiAZ = *r.MultiAZEnabled
	}
	if r.AutomaticFailoverEnabled || desiredMultiAZ {
		if _, err := waitReplicationGroupAvailable(ctx, client, id, false,
			options.waitOptions...); err != nil {
			return err
		}
		if err := strictReplicationGroupModify(ctx, client,
			&elasticache.ModifyReplicationGroupInput{
				ReplicationGroupId:       aws.String(id),
				ApplyImmediately:         aws.Bool(true),
				AutomaticFailoverEnabled: aws.Bool(r.AutomaticFailoverEnabled),
				MultiAZEnabled:           aws.Bool(desiredMultiAZ),
			}); err != nil {
			return err
		}
		_, err = waitReplicationGroupAvailable(ctx, client, id, false,
			options.waitOptions...)
	}
	return err
}

func strictReplicationGroupModify(
	ctx context.Context,
	client replicationGroupClient,
	in *elasticache.ModifyReplicationGroupInput,
) error {
	if _, err := client.ModifyReplicationGroup(ctx, in); err != nil {
		return fmt.Errorf("modify replication group: %w", err)
	}
	return nil
}

func (r *ReplicationGroupResource) authUpdateNeeded(prior ReplicationGroupResource) bool {
	strategy := normalizedAuthTokenUpdateStrategy(r.AuthTokenUpdateStrategy)
	priorStrategy := normalizedAuthTokenUpdateStrategy(prior.AuthTokenUpdateStrategy)
	if strategy == "DELETE" {
		return runtime.Changed(priorStrategy, strategy) ||
			runtime.Changed(prior.AuthToken, r.AuthToken) ||
			runtime.Changed(prior.UserGroupIDs, r.UserGroupIDs)
	}
	return runtime.Changed(priorStrategy, strategy) ||
		runtime.Changed(prior.AuthToken, r.AuthToken)
}

func (r *ReplicationGroupResource) applyAuthUpdate(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	prior ReplicationGroupResource,
	options replicationGroupOperationOptions,
) error {
	return runReplicationGroupOperation(ctx, client, id, options,
		func(ctx context.Context, _ *elasticachetypes.ReplicationGroup) error {
			strategy := normalizedAuthTokenUpdateStrategy(r.AuthTokenUpdateStrategy)
			in := &elasticache.ModifyReplicationGroupInput{
				ReplicationGroupId: aws.String(id),
				ApplyImmediately:   aws.Bool(true),
				AuthTokenUpdateStrategy: elasticachetypes.AuthTokenUpdateStrategyType(
					strategy),
			}
			if strategy == "DELETE" {
				in.UserGroupIdsToAdd, in.UserGroupIdsToRemove = stringSetDiff(
					valueOrZero(prior.UserGroupIDs), valueOrZero(r.UserGroupIDs))
			} else {
				in.AuthToken = r.AuthToken
			}
			return modifyReplicationGroupAcceptedNoop(ctx, client, in)
		})
}

func normalizedAuthTokenUpdateStrategy(value string) string {
	if value == "" {
		return "ROTATE"
	}
	return value
}

func replicationGroupLogDiff(
	prior []ReplicationGroupLogDeliveryConfiguration,
	desired []ReplicationGroupLogDeliveryConfiguration,
) []elasticachetypes.LogDeliveryConfigurationRequest {
	requests := replicationGroupLogRequests(desired, true)
	desiredTypes := make(map[string]bool, len(desired))
	for _, config := range desired {
		desiredTypes[config.LogType] = true
	}
	removed := make([]string, 0)
	for _, config := range prior {
		if !desiredTypes[config.LogType] {
			removed = append(removed, config.LogType)
		}
	}
	slices.Sort(removed)
	for _, logType := range removed {
		requests = append(requests, elasticachetypes.LogDeliveryConfigurationRequest{
			Enabled: aws.Bool(false),
			LogType: elasticachetypes.LogType(logType),
		})
	}
	return requests
}

func hasReplicationGroupLogType(
	configs []ReplicationGroupLogDeliveryConfiguration,
	logType string,
) bool {
	for _, config := range configs {
		if config.LogType == logType {
			return true
		}
	}
	return false
}

func engineSupportsSlowLog(engine, version string) bool {
	if engine == "" {
		engine = "redis"
	}
	minimum := "6.0"
	if engine == "valkey" {
		minimum = "7.0"
	}
	return compareEngineVersions(version, minimum) >= 0
}

func compareEngineVersions(left, right string) int {
	leftParts := numericVersionParts(left)
	rightParts := numericVersionParts(right)
	count := max(len(leftParts), len(rightParts))
	for i := range count {
		var leftPart, rightPart int
		if i < len(leftParts) {
			leftPart = leftParts[i]
		}
		if i < len(rightParts) {
			rightPart = rightParts[i]
		}
		if leftPart < rightPart {
			return -1
		}
		if leftPart > rightPart {
			return 1
		}
	}
	return 0
}

func numericVersionParts(version string) []int {
	parts := strings.Split(version, ".")
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		digits := strings.TrimLeftFunc(part, func(r rune) bool {
			return r < '0' || r > '9'
		})
		end := 0
		for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
			end++
		}
		if end == 0 {
			result = append(result, 0)
			continue
		}
		value, _ := strconv.Atoi(digits[:end])
		result = append(result, value)
	}
	return result
}

func stringSetDiff(prior, desired []string) (add, remove []string) {
	priorSet := make(map[string]bool, len(prior))
	for _, value := range prior {
		priorSet[value] = true
	}
	desiredSet := make(map[string]bool, len(desired))
	for _, value := range desired {
		desiredSet[value] = true
		if !priorSet[value] {
			add = append(add, value)
		}
	}
	for _, value := range prior {
		if !desiredSet[value] {
			remove = append(remove, value)
		}
	}
	slices.Sort(add)
	slices.Sort(remove)
	return add, remove
}
