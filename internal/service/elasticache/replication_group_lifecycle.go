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

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	replicationGroupSettleTimeout = 60 * time.Minute
	replicationGroupDeleteTimeout = 45 * time.Minute
	replicationGroupWaitInterval  = 15 * time.Second
	replicationGroupTagTimeout    = 15 * time.Minute
)

type replicationGroupOperationOptions struct {
	waitOptions  []wait.Option
	retryOptions []retry.Option
}

var replicationGroupAvailablePending = map[string]bool{
	"creating":     true,
	"modifying":    true,
	"snapshotting": true,
}

func (r *ReplicationGroupResource) create(
	ctx context.Context,
	client replicationGroupClient,
	region string,
	options replicationGroupOperationOptions,
) (*ReplicationGroupResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	if r.TransitEncryptionEnabled && r.TransitEncryptionMode != nil &&
		*r.TransitEncryptionMode == "required" {
		return nil, errors.New(
			"creating with transit encryption requires transit-encryption-mode preferred")
	}
	in := r.createInput()
	hadTags := len(in.Tags) > 0
	out, err := client.CreateReplicationGroup(ctx, in)
	fallbackTags := false
	if err != nil {
		if !hadTags || !partition.UnsupportedOperation(region, err) {
			return nil, fmt.Errorf("create replication group: %w", err)
		}
		retryInput := *in
		retryInput.Tags = nil
		out, err = client.CreateReplicationGroup(ctx, &retryInput)
		if err != nil {
			return nil, fmt.Errorf("create replication group without tags: %w", err)
		}
		fallbackTags = true
	}
	group, err := waitReplicationGroupAvailable(ctx, client,
		strings.ToLower(r.ReplicationGroupID), true, options.waitOptions...)
	if err != nil {
		return nil, err
	}
	if r.GlobalReplicationGroupID != nil {
		if err := waitGlobalReplicationGroupAvailable(ctx, client,
			*r.GlobalReplicationGroupID, options.waitOptions...); err != nil {
			return nil, err
		}
	}
	if r.SnapshottingClusterID != nil {
		in := &elasticache.ModifyReplicationGroupInput{
			ReplicationGroupId:    group.ReplicationGroupId,
			ApplyImmediately:      aws.Bool(true),
			SnapshottingClusterId: r.SnapshottingClusterID,
		}
		if err := modifyReplicationGroup(ctx, client, in); err != nil {
			return nil, err
		}
		group, err = waitReplicationGroupAvailable(ctx, client,
			aws.ToString(group.ReplicationGroupId), false, options.waitOptions...)
		if err != nil {
			return nil, err
		}
	}
	if fallbackTags {
		arn := aws.ToString(group.ARN)
		if arn == "" && out != nil && out.ReplicationGroup != nil {
			arn = aws.ToString(out.ReplicationGroup.ARN)
		}
		if arn == "" {
			return nil, errors.New("create replication group without tags returned no ARN")
		}
		if err := addSubnetGroupTags(ctx, client, arn, valueOrEmptyMap(r.Tags),
			append([]retry.Option{retry.WithTimeout(replicationGroupTagTimeout)},
				options.retryOptions...)...); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, aws.ToString(group.ReplicationGroupId), options)
}

func (r *ReplicationGroupResource) createInput() *elasticache.CreateReplicationGroupInput {
	in := &elasticache.CreateReplicationGroupInput{
		ReplicationGroupId:          aws.String(r.ReplicationGroupID),
		ReplicationGroupDescription: aws.String(r.Description),
		AutoMinorVersionUpgrade:     r.AutoMinorVersionUpgrade,
		CacheSecurityGroupNames:     valueOrZero(r.SecurityGroupNames),
		CacheSubnetGroupName:        r.SubnetGroupName,
		DataTieringEnabled:          r.DataTieringEnabled,
		GlobalReplicationGroupId:    r.GlobalReplicationGroupID,
		KmsKeyId:                    r.KMSKeyID,
		LogDeliveryConfigurations: replicationGroupLogRequests(
			valueOrZero(r.LogDeliveryConfiguration), true),
		MultiAZEnabled: r.MultiAZEnabled,
		NodeGroupConfiguration: replicationGroupNodeGroupRequests(
			valueOrZero(r.NodeGroupConfiguration)),
		NotificationTopicArn:       r.NotificationTopicArn,
		NumCacheClusters:           ptr.Int32(r.NumCacheClusters),
		NumNodeGroups:              ptr.Int32(r.NumNodeGroups),
		Port:                       ptr.Int32(r.Port),
		PreferredCacheClusterAZs:   valueOrZero(r.PreferredCacheClusterAZs),
		PreferredMaintenanceWindow: r.MaintenanceWindow,
		PrimaryClusterId:           r.PrimaryClusterID,
		ReplicasPerNodeGroup:       ptr.Int32(r.ReplicasPerNodeGroup),
		SecurityGroupIds:           valueOrZero(r.SecurityGroupIDs),
		SnapshotArns:               valueOrZero(r.SnapshotArns),
		SnapshotName:               r.SnapshotName,
		SnapshotRetentionLimit:     ptr.Int32(r.SnapshotRetentionLimit),
		SnapshotWindow:             r.SnapshotWindow,
		Tags:                       subnetGroupTags(valueOrEmptyMap(r.Tags)),
		UserGroupIds:               valueOrZero(r.UserGroupIDs),
	}
	if r.ClusterMode != nil {
		in.ClusterMode = elasticachetypes.ClusterMode(*r.ClusterMode)
	}
	if r.Durability != nil {
		in.Durability = elasticachetypes.Durability(*r.Durability)
	}
	if r.IPDiscovery != nil {
		in.IpDiscovery = elasticachetypes.IpDiscovery(*r.IPDiscovery)
	}
	if r.NetworkType != nil {
		in.NetworkType = elasticachetypes.NetworkType(*r.NetworkType)
	}
	if r.GlobalReplicationGroupID == nil {
		in.AtRestEncryptionEnabled = r.AtRestEncryptionEnabled
		in.AuthToken = r.AuthToken
		in.AutomaticFailoverEnabled = aws.Bool(r.AutomaticFailoverEnabled)
		in.CacheNodeType = r.NodeType
		in.CacheParameterGroupName = r.ParameterGroupName
		in.Engine = aws.String(r.Engine)
		in.EngineVersion = r.EngineVersion
		in.TransitEncryptionEnabled = aws.Bool(r.TransitEncryptionEnabled)
		if r.TransitEncryptionMode != nil {
			in.TransitEncryptionMode = elasticachetypes.TransitEncryptionMode(
				*r.TransitEncryptionMode)
		}
	}
	return in
}

func (r *ReplicationGroupResource) read(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	options replicationGroupOperationOptions,
) (*ReplicationGroupResourceOutput, error) {
	group, err := waitReplicationGroupAvailable(ctx, client, id, false,
		options.waitOptions...)
	if err != nil {
		return nil, err
	}
	var cluster *elasticachetypes.CacheCluster
	if len(group.MemberClusters) > 0 {
		cluster, err = findReplicationGroupCacheCluster(ctx, client, group.MemberClusters[0])
		if err != nil {
			return nil, err
		}
	}
	return replicationGroupOutput(group, cluster), nil
}

func replicationGroupOutput(
	group *elasticachetypes.ReplicationGroup,
	cluster *elasticachetypes.CacheCluster,
) *ReplicationGroupResourceOutput {
	out := &ReplicationGroupResourceOutput{
		Arn:                   aws.ToString(group.ARN),
		ReplicationGroupID:    aws.ToString(group.ReplicationGroupId),
		ClusterEnabled:        aws.ToBool(group.ClusterEnabled),
		MemberClusters:        append([]string(nil), group.MemberClusters...),
		NumCacheClusters:      int64(len(group.MemberClusters)),
		NumNodeGroups:         int64(len(group.NodeGroups)),
		SnapshottingClusterID: aws.ToString(group.SnapshottingClusterId),
	}
	if group.GlobalReplicationGroupInfo != nil {
		out.GlobalReplicationGroupID = aws.ToString(
			group.GlobalReplicationGroupInfo.GlobalReplicationGroupId)
	}
	if cluster != nil {
		out.EngineVersionActual = aws.ToString(cluster.EngineVersion)
		if cluster.CacheParameterGroup != nil {
			out.ParameterGroupName = aws.ToString(
				cluster.CacheParameterGroup.CacheParameterGroupName)
		}
	}
	if out.ClusterEnabled {
		if group.ConfigurationEndpoint != nil {
			out.ConfigurationEndpointAddress = aws.ToString(group.ConfigurationEndpoint.Address)
			out.Port = int64(aws.ToInt32(group.ConfigurationEndpoint.Port))
		}
	} else if len(group.NodeGroups) > 0 {
		nodeGroup := group.NodeGroups[0]
		if nodeGroup.PrimaryEndpoint != nil {
			out.PrimaryEndpointAddress = aws.ToString(nodeGroup.PrimaryEndpoint.Address)
			out.Port = int64(aws.ToInt32(nodeGroup.PrimaryEndpoint.Port))
		}
		if nodeGroup.ReaderEndpoint != nil {
			out.ReaderEndpointAddress = aws.ToString(nodeGroup.ReaderEndpoint.Address)
			if out.Port == 0 {
				out.Port = int64(aws.ToInt32(nodeGroup.ReaderEndpoint.Port))
			}
		}
	}
	if len(group.NodeGroups) > 0 {
		replicas := len(group.NodeGroups[0].NodeGroupMembers) - 1
		if replicas > 0 {
			out.ReplicasPerNodeGroup = int64(replicas)
		}
	}
	return out
}

func replicationGroupIdentity(
	desired string,
	prior *ReplicationGroupResourceOutput,
) string {
	if prior != nil && prior.ReplicationGroupID != "" {
		return prior.ReplicationGroupID
	}
	return strings.ToLower(desired)
}

func waitReplicationGroupAvailable(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	tolerateMissing bool,
	options ...wait.Option,
) (*elasticachetypes.ReplicationGroup, error) {
	var observed *elasticachetypes.ReplicationGroup
	what := fmt.Sprintf("replication group %s to be available", id)
	waitOptions := append([]wait.Option{
		wait.WithTimeout(replicationGroupSettleTimeout),
		wait.WithInterval(replicationGroupWaitInterval),
	}, options...)
	err := wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		group, err := findReplicationGroup(ctx, client, id)
		if err != nil {
			if errors.Is(err, runtime.ErrNotFound) && tolerateMissing {
				return false, nil
			}
			return false, err
		}
		status := aws.ToString(group.Status)
		switch {
		case status == "available":
			observed = group
			return true, nil
		case status == "deleting":
			return false, runtime.ErrNotFound
		case replicationGroupAvailablePending[status]:
			return false, nil
		default:
			return false, fmt.Errorf("replication group %s entered unexpected status %q",
				id, status)
		}
	}, waitOptions...)
	if err != nil {
		return nil, err
	}
	return observed, nil
}

func findReplicationGroup(
	ctx context.Context,
	client replicationGroupClient,
	id string,
) (*elasticachetypes.ReplicationGroup, error) {
	paginator := elasticache.NewDescribeReplicationGroupsPaginator(client,
		&elasticache.DescribeReplicationGroupsInput{ReplicationGroupId: aws.String(id)})
	groups := make([]elasticachetypes.ReplicationGroup, 0, 1)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if replicationGroupNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe replication groups: %w", err)
		}
		groups = append(groups, page.ReplicationGroups...)
		if len(groups) > 1 {
			return nil, fmt.Errorf("describe replication groups: %d results for id %s",
				len(groups), id)
		}
	}
	if len(groups) == 0 ||
		!strings.EqualFold(aws.ToString(groups[0].ReplicationGroupId), id) {
		return nil, runtime.ErrNotFound
	}
	return &groups[0], nil
}

func findReplicationGroupCacheCluster(
	ctx context.Context,
	client replicationGroupClient,
	id string,
) (*elasticachetypes.CacheCluster, error) {
	paginator := elasticache.NewDescribeCacheClustersPaginator(client,
		&elasticache.DescribeCacheClustersInput{
			CacheClusterId:    aws.String(id),
			ShowCacheNodeInfo: aws.Bool(true),
		})
	clusters := make([]elasticachetypes.CacheCluster, 0, 1)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe cache clusters: %w", err)
		}
		clusters = append(clusters, page.CacheClusters...)
		if len(clusters) > 1 {
			return nil, fmt.Errorf("describe cache clusters: %d results for id %s",
				len(clusters), id)
		}
	}
	if len(clusters) == 0 {
		return nil, fmt.Errorf("describe cache clusters: no result for id %s", id)
	}
	return &clusters[0], nil
}

func waitGlobalReplicationGroupAvailable(
	ctx context.Context,
	client replicationGroupClient,
	id string,
	options ...wait.Option,
) error {
	waitOptions := append([]wait.Option{
		wait.WithTimeout(replicationGroupSettleTimeout),
		wait.WithInterval(replicationGroupWaitInterval),
	}, options...)
	what := fmt.Sprintf("global replication group %s to be available", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		group, err := findGlobalReplicationGroup(ctx, client, id)
		if err != nil {
			if errors.Is(err, runtime.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		status := aws.ToString(group.Status)
		if status == "available" || status == "primary-only" {
			return true, nil
		}
		if status == "creating" || status == "modifying" {
			return false, nil
		}
		return false, fmt.Errorf("global replication group %s entered unexpected status %q",
			id, status)
	}, waitOptions...)
}

func findGlobalReplicationGroup(
	ctx context.Context,
	client replicationGroupClient,
	id string,
) (*elasticachetypes.GlobalReplicationGroup, error) {
	paginator := elasticache.NewDescribeGlobalReplicationGroupsPaginator(client,
		&elasticache.DescribeGlobalReplicationGroupsInput{
			GlobalReplicationGroupId: aws.String(id),
			ShowMemberInfo:           aws.Bool(true),
		})
	groups := make([]elasticachetypes.GlobalReplicationGroup, 0, 1)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if globalReplicationGroupNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe global replication groups: %w", err)
		}
		groups = append(groups, page.GlobalReplicationGroups...)
		if len(groups) > 1 {
			return nil, fmt.Errorf(
				"describe global replication groups: %d results for id %s", len(groups), id)
		}
	}
	if len(groups) == 0 {
		return nil, runtime.ErrNotFound
	}
	return &groups[0], nil
}

func globalReplicationGroupNotFound(err error) bool {
	var notFound *elasticachetypes.GlobalReplicationGroupNotFoundFault
	return errors.As(err, &notFound)
}

func modifyReplicationGroupAcceptedNoop(
	ctx context.Context,
	client replicationGroupClient,
	in *elasticache.ModifyReplicationGroupInput,
) error {
	err := modifyReplicationGroup(ctx, client, in)
	if err != nil && !replicationGroupNoModifications(err) {
		return err
	}
	return nil
}

func modifyReplicationGroup(
	ctx context.Context,
	client replicationGroupClient,
	in *elasticache.ModifyReplicationGroupInput,
) error {
	_, err := client.ModifyReplicationGroup(ctx, in)
	if err != nil {
		return fmt.Errorf("modify replication group: %w", err)
	}
	return nil
}

func replicationGroupNoModifications(err error) bool {
	var invalid *elasticachetypes.InvalidParameterCombinationException
	return errors.As(err, &invalid) &&
		strings.Contains(invalid.ErrorMessage(), "No modifications were requested")
}

func valueOrEmptyMap(value *map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	return *value
}

func replicationGroupLogRequests(
	configs []ReplicationGroupLogDeliveryConfiguration,
	enabled bool,
) []elasticachetypes.LogDeliveryConfigurationRequest {
	if len(configs) == 0 {
		return nil
	}
	requests := make([]elasticachetypes.LogDeliveryConfigurationRequest, 0, len(configs))
	for _, config := range configs {
		request := elasticachetypes.LogDeliveryConfigurationRequest{
			DestinationType: elasticachetypes.DestinationType(config.DestinationType),
			Enabled:         aws.Bool(enabled),
			LogFormat:       elasticachetypes.LogFormat(config.LogFormat),
			LogType:         elasticachetypes.LogType(config.LogType),
		}
		if enabled {
			request.DestinationDetails = &elasticachetypes.DestinationDetails{}
			if config.DestinationType == "cloudwatch-logs" {
				request.DestinationDetails.CloudWatchLogsDetails =
					&elasticachetypes.CloudWatchLogsDestinationDetails{
						LogGroup: aws.String(config.Destination),
					}
			} else {
				request.DestinationDetails.KinesisFirehoseDetails =
					&elasticachetypes.KinesisFirehoseDestinationDetails{
						DeliveryStream: aws.String(config.Destination),
					}
			}
		}
		requests = append(requests, request)
	}
	return requests
}

func replicationGroupNodeGroupRequests(
	configs []ReplicationGroupNodeGroupConfiguration,
) []elasticachetypes.NodeGroupConfiguration {
	if len(configs) == 0 {
		return nil
	}
	requests := make([]elasticachetypes.NodeGroupConfiguration, 0, len(configs))
	for _, config := range configs {
		requests = append(requests, elasticachetypes.NodeGroupConfiguration{
			NodeGroupId:              config.NodeGroupID,
			PrimaryAvailabilityZone:  config.PrimaryAvailabilityZone,
			PrimaryOutpostArn:        config.PrimaryOutpostArn,
			ReplicaAvailabilityZones: valueOrZero(config.ReplicaAvailabilityZones),
			ReplicaCount:             ptr.Int32(config.ReplicaCount),
			ReplicaOutpostArns:       valueOrZero(config.ReplicaOutpostArns),
			Slots:                    config.Slots,
		})
	}
	return requests
}
