package elasticache

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

var (
	replicationGroupIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
	nodeGroupIDPattern        = regexp.MustCompile(`^[0-9]{1,4}$`)
	snapshotWindowPattern     = regexp.MustCompile(
		`^(?:[01][0-9]|2[0-3]):[0-5][0-9]-(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)
	maintenanceWindowPattern = regexp.MustCompile(
		`^(?:sun|mon|tue|wed|thu|fri|sat):(?:[01][0-9]|2[0-3]):[0-5][0-9]-` +
			`(?:sun|mon|tue|wed|thu|fri|sat):(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)
)

type ReplicationGroupLogDeliveryConfiguration struct {
	Destination     string `ub:"destination"`
	DestinationType string `ub:"destination-type"`
	LogFormat       string `ub:"log-format"`
	LogType         string `ub:"log-type"`
}

type ReplicationGroupNodeGroupConfiguration struct {
	NodeGroupID              *string   `ub:"node-group-id"`
	PrimaryAvailabilityZone  *string   `ub:"primary-availability-zone"`
	PrimaryOutpostArn        *string   `ub:"primary-outpost-arn"`
	ReplicaAvailabilityZones *[]string `ub:"replica-availability-zones"`
	ReplicaCount             *int64    `ub:"replica-count"`
	ReplicaOutpostArns       *[]string `ub:"replica-outpost-arns"`
	Slots                    *string   `ub:"slots"`
}

type ReplicationGroupLogDeliveries = []ReplicationGroupLogDeliveryConfiguration
type ReplicationGroupNodeGroups = []ReplicationGroupNodeGroupConfiguration

type ReplicationGroupResource struct {
	ReplicationGroupID string `ub:"replication-group-id"`
	Description        string `ub:"description"`

	AtRestEncryptionEnabled *bool   `ub:"at-rest-encryption-enabled"`
	AuthToken               *string `ub:"auth-token,sensitive"`
	AuthTokenUpdateStrategy string  `ub:"auth-token-update-strategy"`

	AutoMinorVersionUpgrade  *bool `ub:"auto-minor-version-upgrade"`
	AutomaticFailoverEnabled bool  `ub:"automatic-failover-enabled"`
	MultiAZEnabled           *bool `ub:"multi-az-enabled"`

	ClusterMode        *string `ub:"cluster-mode"`
	DataTieringEnabled *bool   `ub:"data-tiering-enabled"`
	Durability         *string `ub:"durability"`
	Engine             string  `ub:"engine"`
	EngineVersion      *string `ub:"engine-version"`

	GlobalReplicationGroupID *string `ub:"global-replication-group-id"`
	PrimaryClusterID         *string `ub:"primary-cluster-id"`

	IPDiscovery *string `ub:"ip-discovery"`
	NetworkType *string `ub:"network-type"`
	NodeType    *string `ub:"node-type"`
	KMSKeyID    *string `ub:"kms-key-id"`
	Port        *int64  `ub:"port"`

	LogDeliveryConfiguration *ReplicationGroupLogDeliveries `ub:"log-delivery-configuration"`
	NodeGroupConfiguration   *ReplicationGroupNodeGroups    `ub:"node-group-configuration"`

	NumCacheClusters     *int64 `ub:"num-cache-clusters"`
	NumNodeGroups        *int64 `ub:"num-node-groups"`
	ReplicasPerNodeGroup *int64 `ub:"replicas-per-node-group"`

	MaintenanceWindow        *string   `ub:"maintenance-window"`
	NotificationTopicArn     *string   `ub:"notification-topic-arn"`
	ParameterGroupName       *string   `ub:"parameter-group-name"`
	PreferredCacheClusterAZs *[]string `ub:"preferred-cache-cluster-azs"`
	SecurityGroupIDs         *[]string `ub:"security-group-ids"`
	SecurityGroupNames       *[]string `ub:"security-group-names"`
	SubnetGroupName          *string   `ub:"subnet-group-name"`

	SnapshotArns           *[]string `ub:"snapshot-arns"`
	SnapshotName           *string   `ub:"snapshot-name"`
	SnapshotRetentionLimit *int64    `ub:"snapshot-retention-limit"`
	SnapshotWindow         *string   `ub:"snapshot-window"`
	SnapshottingClusterID  *string   `ub:"snapshotting-cluster-id"`

	TransitEncryptionEnabled bool      `ub:"transit-encryption-enabled"`
	TransitEncryptionMode    *string   `ub:"transit-encryption-mode"`
	UserGroupIDs             *[]string `ub:"user-group-ids"`

	FinalSnapshotIdentifier *string            `ub:"final-snapshot-identifier"`
	Tags                    *map[string]string `ub:"tags"`
}

type ReplicationGroupResourceOutput struct {
	Arn                          string   `ub:"arn"`
	ReplicationGroupID           string   `ub:"replication-group-id"`
	EngineVersion                string   `ub:"engine-version"`
	ClusterEnabled               bool     `ub:"cluster-enabled"`
	Port                         int64    `ub:"port"`
	ConfigurationEndpointAddress string   `ub:"configuration-endpoint-address"`
	PrimaryEndpointAddress       string   `ub:"primary-endpoint-address"`
	ReaderEndpointAddress        string   `ub:"reader-endpoint-address"`
	MemberClusters               []string `ub:"member-clusters"`
	NumCacheClusters             int64    `ub:"num-cache-clusters"`
	NumNodeGroups                int64    `ub:"num-node-groups"`
	ReplicasPerNodeGroup         int64    `ub:"replicas-per-node-group"`
	GlobalReplicationGroupID     string   `ub:"global-replication-group-id"`
	ParameterGroupName           string   `ub:"parameter-group-name"`
	SnapshottingClusterID        string   `ub:"snapshotting-cluster-id"`
}

func (r *ReplicationGroupResource) SchemaVersion() int { return 1 }

func (r *ReplicationGroupResource) ReplaceFields() []string {
	fields := []string{
		"at-rest-encryption-enabled",
		"data-tiering-enabled",
		"durability",
		"global-replication-group-id",
		"kms-key-id",
		"network-type",
		"node-group-configuration",
		"port",
		"preferred-cache-cluster-azs",
		"replication-group-id",
		"security-group-names",
		"snapshot-arns",
		"snapshot-name",
		"subnet-group-name",
	}
	if r.Engine == "redis" {
		fields = append(fields, "engine")
	}
	if r.NodeGroupConfiguration != nil {
		fields = append(fields, "replicas-per-node-group")
	}
	return fields
}

func (r ReplicationGroupResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.AuthTokenUpdateStrategy, "ROTATE"),
		defaults.Value(r.AutomaticFailoverEnabled, false),
		defaults.Value(r.Engine, "redis"),
		defaults.Value(r.TransitEncryptionEnabled, false),
	}
}

func (r ReplicationGroupResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.Description)).
			Message("description must not be empty"),
		constraint.Must(constraint.MaxItems(r.Tags, 50)).
			Message("tags holds at most 50 entries"),
		constraint.When(constraint.Present(r.AuthToken)).
			Require(constraint.IsTrue(r.TransitEncryptionEnabled)).
			Message("auth-token requires transit-encryption-enabled"),
		constraint.ForbiddenWith(r.AuthToken, r.UserGroupIDs),
		constraint.When(constraint.IsTrue(r.MultiAZEnabled)).
			Require(constraint.IsTrue(r.AutomaticFailoverEnabled)).
			Message("multi-az-enabled requires automatic-failover-enabled"),
		constraint.Must(constraint.OneOf(r.AuthTokenUpdateStrategy, "SET", "ROTATE", "DELETE")).
			Message("auth-token-update-strategy must be SET, ROTATE, or DELETE"),
		constraint.When(constraint.Present(r.ClusterMode)).
			Require(constraint.OneOf(r.ClusterMode, "disabled", "compatible", "enabled")).
			Message("cluster-mode must be disabled, compatible, or enabled"),
		constraint.When(constraint.Present(r.Durability)).
			Require(constraint.OneOf(r.Durability, "sync", "async")).
			Message("durability must be sync or async"),
		constraint.Must(constraint.OneOf(r.Engine, "redis", "valkey")).
			Message("engine must be redis or valkey"),
		constraint.When(constraint.Present(r.IPDiscovery)).
			Require(constraint.OneOf(r.IPDiscovery, "ipv4", "ipv6")).
			Message("ip-discovery must be ipv4 or ipv6"),
		constraint.When(constraint.Present(r.NetworkType)).
			Require(constraint.OneOf(r.NetworkType, "ipv4", "ipv6", "dual_stack")).
			Message("network-type must be ipv4, ipv6, or dual_stack"),
		constraint.When(constraint.Present(r.TransitEncryptionMode)).
			Require(constraint.OneOf(r.TransitEncryptionMode, "preferred", "required")).
			Message("transit-encryption-mode must be preferred or required"),
		constraint.When(constraint.Present(r.NumCacheClusters)).
			Require(constraint.AtLeast(r.NumCacheClusters, 1),
				constraint.AtMost(r.NumCacheClusters, 6)).
			Message("num-cache-clusters must be between 1 and 6"),
		constraint.When(constraint.Present(r.NumNodeGroups)).
			Require(constraint.AtLeast(r.NumNodeGroups, 1),
				constraint.AtMost(r.NumNodeGroups, 500)).
			Message("num-node-groups must be between 1 and 500"),
		constraint.When(constraint.Present(r.ReplicasPerNodeGroup)).
			Require(constraint.AtLeast(r.ReplicasPerNodeGroup, 0),
				constraint.AtMost(r.ReplicasPerNodeGroup, 5)).
			Message("replicas-per-node-group must be between 0 and 5"),
		constraint.When(constraint.Present(r.SnapshotRetentionLimit)).
			Require(constraint.AtLeast(r.SnapshotRetentionLimit, 0),
				constraint.AtMost(r.SnapshotRetentionLimit, 35)).
			Message("snapshot-retention-limit must be between 0 and 35"),
		constraint.When(constraint.Present(r.Port)).
			Require(constraint.AtLeast(r.Port, 1), constraint.AtMost(r.Port, 65535)).
			Message("port must be between 1 and 65535"),
		constraint.When(constraint.All(
			constraint.Absent(r.GlobalReplicationGroupID),
			constraint.Absent(r.PrimaryClusterID),
		)).Require(constraint.Present(r.NodeType)).
			Message("node-type is required unless joining a global group or adopting a cluster"),
		constraint.ForbiddenWith(r.NumCacheClusters, r.NumNodeGroups,
			r.ReplicasPerNodeGroup, r.NodeGroupConfiguration),
		constraint.ForbiddenWith(r.NodeGroupConfiguration, r.NumNodeGroups,
			r.ReplicasPerNodeGroup, r.PreferredCacheClusterAZs),
		constraint.ForbiddenWith(r.GlobalReplicationGroupID, r.PrimaryClusterID,
			r.AtRestEncryptionEnabled, r.AuthToken, r.DataTieringEnabled,
			r.EngineVersion, r.KMSKeyID, r.NodeType, r.NumNodeGroups,
			r.ParameterGroupName, r.SecurityGroupNames, r.SnapshotArns,
			r.SnapshotName, r.TransitEncryptionMode),
		constraint.When(constraint.Equals(r.ClusterMode, "enabled")).
			Require(constraint.Absent(r.SnapshottingClusterID)).
			Message("snapshotting-cluster-id is forbidden when cluster-mode is enabled"),
		constraint.Must(constraint.MaxItems(r.LogDeliveryConfiguration, 2)).
			Message("log-delivery-configuration holds at most two entries"),
		constraint.ForEach(r.LogDeliveryConfiguration,
			func(v ReplicationGroupLogDeliveryConfiguration) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(v.DestinationType,
						"cloudwatch-logs", "kinesis-firehose")).
						Message("destination-type must be cloudwatch-logs or kinesis-firehose"),
					constraint.Must(constraint.OneOf(v.LogFormat, "text", "json")).
						Message("log-format must be text or json"),
					constraint.Must(constraint.OneOf(v.LogType, "slow-log", "engine-log")).
						Message("log-type must be slow-log or engine-log"),
				}
			}),
		constraint.ForEach(r.NodeGroupConfiguration,
			func(v ReplicationGroupNodeGroupConfiguration) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(v.ReplicaCount)).
						Require(constraint.AtLeast(v.ReplicaCount, 0),
							constraint.AtMost(v.ReplicaCount, 5)).
						Message("replica-count must be between 0 and 5"),
				}
			}),
	}
}

func (r *ReplicationGroupResource) EquivalentInput(
	field string,
	prior ReplicationGroupResource,
	current ReplicationGroupResource,
) bool {
	return field == "replication-group-id" &&
		strings.EqualFold(prior.ReplicationGroupID, current.ReplicationGroupID)
}

func (r *ReplicationGroupResource) ValidateInputs(context.Context, *awsCfg) error {
	if err := validateReplicationGroupID(r.ReplicationGroupID); err != nil {
		return err
	}
	if r.Description == "" {
		return errors.New("description must not be empty")
	}
	if r.Engine != "redis" && r.Engine != "valkey" {
		return errors.New("engine must be redis or valkey")
	}
	if r.AuthTokenUpdateStrategy != "" &&
		!oneOf(r.AuthTokenUpdateStrategy, "SET", "ROTATE", "DELETE") {
		return errors.New("auth-token-update-strategy must be SET, ROTATE, or DELETE")
	}
	if err := validateOptionalEnum("cluster-mode", r.ClusterMode,
		"disabled", "compatible", "enabled"); err != nil {
		return err
	}
	if err := validateOptionalEnum("durability", r.Durability, "sync", "async"); err != nil {
		return err
	}
	if err := validateOptionalEnum("ip-discovery", r.IPDiscovery, "ipv4", "ipv6"); err != nil {
		return err
	}
	if err := validateOptionalEnum("network-type", r.NetworkType,
		"ipv4", "ipv6", "dual_stack"); err != nil {
		return err
	}
	if err := validateOptionalEnum("transit-encryption-mode", r.TransitEncryptionMode,
		"preferred", "required"); err != nil {
		return err
	}
	if err := validateRange("num-cache-clusters", r.NumCacheClusters, 1, 6); err != nil {
		return err
	}
	if err := validateRange("num-node-groups", r.NumNodeGroups, 1, 500); err != nil {
		return err
	}
	if err := validateRange("replicas-per-node-group", r.ReplicasPerNodeGroup, 0, 5); err != nil {
		return err
	}
	if err := validateRange("snapshot-retention-limit", r.SnapshotRetentionLimit, 0, 35); err != nil {
		return err
	}
	if err := validateRange("port", r.Port, 1, 65535); err != nil {
		return err
	}
	if r.MaintenanceWindow != nil &&
		!maintenanceWindowPattern.MatchString(*r.MaintenanceWindow) {
		return errors.New("maintenance-window must use ddd:hh:mm-ddd:hh:mm")
	}
	if r.SnapshotWindow != nil && !snapshotWindowPattern.MatchString(*r.SnapshotWindow) {
		return errors.New("snapshot-window must use hh:mm-hh:mm")
	}
	if err := validateAuthToken(r.AuthToken); err != nil {
		return err
	}
	if err := validateReplicationGroupTags(r.Tags); err != nil {
		return err
	}
	if err := r.validateNestedInputs(); err != nil {
		return err
	}
	return r.validateInputCombinations()
}

func (r *ReplicationGroupResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*ReplicationGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, client.Options().Region, replicationGroupOperationOptions{})
}

func (r *ReplicationGroupResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *ReplicationGroupResourceOutput,
) (*ReplicationGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, replicationGroupIdentity(r.ReplicationGroupID, prior),
		replicationGroupOperationOptions{})
}

func (r *ReplicationGroupResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[ReplicationGroupResource, *ReplicationGroupResourceOutput],
) (*ReplicationGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior, replicationGroupOperationOptions{})
}

func (r *ReplicationGroupResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *ReplicationGroupResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, client.Options().Region, prior,
		replicationGroupOperationOptions{})
}

type replicationGroupClient interface {
	elasticacheTagClient

	CreateReplicationGroup(
		context.Context,
		*elasticache.CreateReplicationGroupInput,
		...func(*elasticache.Options),
	) (*elasticache.CreateReplicationGroupOutput, error)
	DescribeReplicationGroups(
		context.Context,
		*elasticache.DescribeReplicationGroupsInput,
		...func(*elasticache.Options),
	) (*elasticache.DescribeReplicationGroupsOutput, error)
	DescribeCacheClusters(
		context.Context,
		*elasticache.DescribeCacheClustersInput,
		...func(*elasticache.Options),
	) (*elasticache.DescribeCacheClustersOutput, error)
	ModifyReplicationGroup(
		context.Context,
		*elasticache.ModifyReplicationGroupInput,
		...func(*elasticache.Options),
	) (*elasticache.ModifyReplicationGroupOutput, error)
	ModifyReplicationGroupShardConfiguration(
		context.Context,
		*elasticache.ModifyReplicationGroupShardConfigurationInput,
		...func(*elasticache.Options),
	) (*elasticache.ModifyReplicationGroupShardConfigurationOutput, error)
	IncreaseReplicaCount(
		context.Context,
		*elasticache.IncreaseReplicaCountInput,
		...func(*elasticache.Options),
	) (*elasticache.IncreaseReplicaCountOutput, error)
	DecreaseReplicaCount(
		context.Context,
		*elasticache.DecreaseReplicaCountInput,
		...func(*elasticache.Options),
	) (*elasticache.DecreaseReplicaCountOutput, error)
	DeleteReplicationGroup(
		context.Context,
		*elasticache.DeleteReplicationGroupInput,
		...func(*elasticache.Options),
	) (*elasticache.DeleteReplicationGroupOutput, error)
	DescribeGlobalReplicationGroups(
		context.Context,
		*elasticache.DescribeGlobalReplicationGroupsInput,
		...func(*elasticache.Options),
	) (*elasticache.DescribeGlobalReplicationGroupsOutput, error)
	DisassociateGlobalReplicationGroup(
		context.Context,
		*elasticache.DisassociateGlobalReplicationGroupInput,
		...func(*elasticache.Options),
	) (*elasticache.DisassociateGlobalReplicationGroupOutput, error)
	DeleteCacheParameterGroup(
		context.Context,
		*elasticache.DeleteCacheParameterGroupInput,
		...func(*elasticache.Options),
	) (*elasticache.DeleteCacheParameterGroupOutput, error)
}

func validateReplicationGroupID(id string) error {
	if len(id) < 1 || len(id) > 40 || !replicationGroupIDPattern.MatchString(id) ||
		strings.HasSuffix(id, "-") || strings.Contains(id, "--") {
		return errors.New("replication-group-id must be 1 to 40 letters, digits, or " +
			"single hyphens, begin with a letter, and not end with a hyphen")
	}
	return nil
}

func validateOptionalEnum(name string, value *string, allowed ...string) error {
	if value == nil || oneOf(*value, allowed...) {
		return nil
	}
	return fmt.Errorf("%s must be one of %s", name, strings.Join(allowed, ", "))
}

func validateRange(name string, value *int64, minValue, maxValue int64) error {
	if value == nil || (*value >= minValue && *value <= maxValue) {
		return nil
	}
	return fmt.Errorf("%s must be between %d and %d", name, minValue, maxValue)
}

func oneOf(value string, allowed ...string) bool {
	return slices.Contains(allowed, value)
}

func validateAuthToken(token *string) error {
	if token == nil {
		return nil
	}
	length := utf8.RuneCountInString(*token)
	if length < 16 || length > 128 {
		return errors.New("auth-token must be 16 to 128 characters")
	}
	if strings.ContainsAny(*token, `@"/`) {
		return errors.New(`auth-token must not contain @, ", or /`)
	}
	return nil
}

func validateReplicationGroupTags(tags *map[string]string) error {
	desired := map[string]string{}
	if tags != nil {
		desired = *tags
	}
	if len(desired) > 50 {
		return errors.New("tags must have at most 50 entries")
	}
	for key, value := range desired {
		if n := utf8.RuneCountInString(key); n < 1 || n > 128 {
			return errors.New("tag key must be 1 to 128 characters")
		}
		if strings.HasPrefix(key, "aws:") {
			return errors.New("tag key must not begin with aws:")
		}
		if utf8.RuneCountInString(value) > 256 {
			return errors.New("tag value must be at most 256 characters")
		}
	}
	return nil
}

func (r *ReplicationGroupResource) validateNestedInputs() error {
	logs := valueOrZero(r.LogDeliveryConfiguration)
	if len(logs) > 2 {
		return errors.New("log-delivery-configuration must have at most two entries")
	}
	seenLogTypes := make(map[string]bool, len(logs))
	for i, logConfig := range logs {
		if logConfig.Destination == "" {
			return fmt.Errorf("log-delivery-configuration[%d].destination must not be empty", i)
		}
		if !oneOf(logConfig.DestinationType, "cloudwatch-logs", "kinesis-firehose") {
			return fmt.Errorf("log-delivery-configuration[%d].destination-type is invalid", i)
		}
		if !oneOf(logConfig.LogFormat, "text", "json") {
			return fmt.Errorf("log-delivery-configuration[%d].log-format is invalid", i)
		}
		if !oneOf(logConfig.LogType, "slow-log", "engine-log") {
			return fmt.Errorf("log-delivery-configuration[%d].log-type is invalid", i)
		}
		if seenLogTypes[logConfig.LogType] {
			return fmt.Errorf("log-delivery-configuration repeats log type %q", logConfig.LogType)
		}
		seenLogTypes[logConfig.LogType] = true
	}
	for i, nodeGroup := range valueOrZero(r.NodeGroupConfiguration) {
		if nodeGroup.NodeGroupID != nil && !nodeGroupIDPattern.MatchString(*nodeGroup.NodeGroupID) {
			return fmt.Errorf("node-group-configuration[%d].node-group-id must be 1 to 4 digits", i)
		}
		if err := validateRange(
			fmt.Sprintf("node-group-configuration[%d].replica-count", i),
			nodeGroup.ReplicaCount, 0, 5,
		); err != nil {
			return err
		}
	}
	for i, snapshotArn := range valueOrZero(r.SnapshotArns) {
		if strings.Contains(snapshotArn, ",") {
			return fmt.Errorf("snapshot-arns[%d] must not contain a comma", i)
		}
		if _, err := arn.Parse(snapshotArn); err != nil {
			return fmt.Errorf("snapshot-arns[%d] must be a valid ARN: %w", i, err)
		}
	}
	if r.SnapshotName != nil && strings.Contains(*r.SnapshotName, ",") {
		return errors.New("snapshot-name must not contain a comma")
	}
	return nil
}

func (r *ReplicationGroupResource) validateInputCombinations() error {
	if r.NodeType == nil && r.GlobalReplicationGroupID == nil && r.PrimaryClusterID == nil {
		return errors.New("node-type is required unless joining a global group or adopting a cluster")
	}
	if r.MultiAZEnabled != nil && *r.MultiAZEnabled && !r.AutomaticFailoverEnabled {
		return errors.New("multi-az-enabled requires automatic-failover-enabled")
	}
	if r.AuthToken != nil && len(valueOrZero(r.UserGroupIDs)) > 0 {
		return errors.New("auth-token conflicts with user-group-ids")
	}
	if r.AuthToken != nil && !r.TransitEncryptionEnabled {
		return errors.New("auth-token requires transit-encryption-enabled")
	}
	if r.ClusterMode != nil && *r.ClusterMode == "enabled" && r.SnapshottingClusterID != nil {
		return errors.New("snapshotting-cluster-id is forbidden when cluster-mode is enabled")
	}
	if r.NumCacheClusters != nil &&
		(r.NumNodeGroups != nil || r.ReplicasPerNodeGroup != nil ||
			r.NodeGroupConfiguration != nil) {
		return errors.New("num-cache-clusters conflicts with clustered topology inputs")
	}
	if r.NodeGroupConfiguration != nil &&
		(r.NumNodeGroups != nil || r.ReplicasPerNodeGroup != nil ||
			r.PreferredCacheClusterAZs != nil) {
		return errors.New("node-group-configuration conflicts with count and preferred AZ inputs")
	}
	if r.PreferredCacheClusterAZs != nil && r.NumCacheClusters != nil &&
		len(*r.PreferredCacheClusterAZs) != int(*r.NumCacheClusters) {
		return errors.New("preferred-cache-cluster-azs must match num-cache-clusters")
	}
	clustered := r.ClusterMode != nil && *r.ClusterMode != "disabled" ||
		r.NumNodeGroups != nil || r.ReplicasPerNodeGroup != nil ||
		r.NodeGroupConfiguration != nil
	if r.GlobalReplicationGroupID == nil && r.AutomaticFailoverEnabled && !clustered &&
		(r.NumCacheClusters == nil || *r.NumCacheClusters < 2) {
		return errors.New("automatic-failover-enabled requires at least two cache clusters")
	}
	if r.DataTieringEnabled != nil && *r.DataTieringEnabled {
		if r.NodeType == nil || !strings.HasPrefix(*r.NodeType, "cache.r6gd.") {
			return errors.New("data-tiering-enabled requires a cache.r6gd node type")
		}
	}
	if r.NodeType != nil && strings.HasPrefix(*r.NodeType, "cache.r6gd.") &&
		(r.DataTieringEnabled == nil || !*r.DataTieringEnabled) {
		return errors.New("cache.r6gd node types require data-tiering-enabled")
	}
	if r.GlobalReplicationGroupID != nil {
		if r.PrimaryClusterID != nil || r.AtRestEncryptionEnabled != nil ||
			r.AuthToken != nil || r.DataTieringEnabled != nil || r.EngineVersion != nil ||
			r.KMSKeyID != nil || r.NodeType != nil || r.NumNodeGroups != nil ||
			r.ParameterGroupName != nil || r.SecurityGroupNames != nil ||
			r.SnapshotArns != nil || r.SnapshotName != nil ||
			r.TransitEncryptionMode != nil || r.TransitEncryptionEnabled {
			return errors.New("global-replication-group-id conflicts with locally inherited inputs")
		}
	}
	return nil
}

func valueOrZero[T any](value *[]T) []T {
	if value == nil {
		return nil
	}
	return *value
}

func replicationGroupNotFound(err error) bool {
	var notFound *elasticachetypes.ReplicationGroupNotFoundFault
	return errors.As(err, &notFound)
}
