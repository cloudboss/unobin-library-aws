package ecs

import (
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// ClusterConfiguration is a cluster's configuration block: the execute
// command settings and the managed storage settings. Removing the whole block
// from an existing cluster clears both on the next apply, since the update
// sends the documented empty-configuration sentinel.
type ClusterConfiguration struct {
	ExecuteCommandConfiguration *ClusterExecuteCommandConfiguration `ub:"execute-command-configuration"`
	ManagedStorageConfiguration *ClusterManagedStorageConfiguration `ub:"managed-storage-configuration"`
}

// ClusterExecuteCommandConfiguration controls ECS Exec sessions on the
// cluster: the KMS key that encrypts the session and where session logs go.
// Logging accepts NONE, DEFAULT, or OVERRIDE; OVERRIDE requires the
// log-configuration block that names the destinations.
type ClusterExecuteCommandConfiguration struct {
	KmsKeyId         *string                                `ub:"kms-key-id"`
	Logging          *string                                `ub:"logging"`
	LogConfiguration *ClusterExecuteCommandLogConfiguration `ub:"log-configuration"`
}

// ClusterExecuteCommandLogConfiguration names where execute command session
// logs are sent: a CloudWatch log group, an S3 bucket, or both, each with an
// optional encryption toggle. The log group and bucket must already exist.
type ClusterExecuteCommandLogConfiguration struct {
	CloudWatchEncryptionEnabled *bool   `ub:"cloud-watch-encryption-enabled"`
	CloudWatchLogGroupName      *string `ub:"cloud-watch-log-group-name"`
	S3BucketName                *string `ub:"s3-bucket-name"`
	S3EncryptionEnabled         *bool   `ub:"s3-encryption-enabled"`
	S3KeyPrefix                 *string `ub:"s3-key-prefix"`
}

// ClusterManagedStorageConfiguration names the KMS keys that encrypt storage
// ECS manages for tasks in the cluster: kms-key-id for managed data volumes
// such as EBS, and fargate-ephemeral-storage-kms-key-id for Fargate ephemeral
// storage. Each key must be a single-Region key.
type ClusterManagedStorageConfiguration struct {
	KmsKeyId                        *string `ub:"kms-key-id"`
	FargateEphemeralStorageKmsKeyId *string `ub:"fargate-ephemeral-storage-kms-key-id"`
}

// ClusterServiceConnectDefaults sets the cluster's default Service Connect
// namespace. The namespace is a Cloud Map namespace name or ARN; a name that
// does not exist yet makes ECS create an HTTP namespace by that name.
// Removing the block from an existing cluster clears the default on the next
// apply, through the documented empty-string namespace; the Cloud Map
// namespace itself remains and must be deleted separately.
type ClusterServiceConnectDefaults struct {
	Namespace string `ub:"namespace"`
}

// ClusterSetting is one cluster setting. The only name ECS defines is
// containerInsights, whose value is enabled, disabled, or enhanced; the value
// set is enforced by the API, not validated here. Settings have no clear
// operation: removing an entry, or the whole list, leaves the value already
// on the cluster in place.
type ClusterSetting struct {
	Name  string `ub:"name"`
	Value string `ub:"value"`
}

// ClusterCapacityProviderStrategyItem is one entry of the cluster's default
// capacity provider strategy. The capacity provider is FARGATE, FARGATE_SPOT,
// or the name of a capacity provider attached to the cluster through the
// capacity-providers field; that membership, and the API rule that only one
// item may define a nonzero base, are enforced by ECS rather than validated
// here. An omitted base or weight rides as 0, the API default.
type ClusterCapacityProviderStrategyItem struct {
	CapacityProvider string `ub:"capacity-provider"`
	Base             *int64 `ub:"base"`
	Weight           *int64 `ub:"weight"`
}

// sdk converts the configuration block to its SDK type, returning nil for a
// nil block so an absent configuration stays out of the request.
func (c *ClusterConfiguration) sdk() *ecstypes.ClusterConfiguration {
	if c == nil {
		return nil
	}
	out := &ecstypes.ClusterConfiguration{}
	if ecc := c.ExecuteCommandConfiguration; ecc != nil {
		execute := &ecstypes.ExecuteCommandConfiguration{
			KmsKeyId: ecc.KmsKeyId,
		}
		if ecc.Logging != nil {
			execute.Logging = ecstypes.ExecuteCommandLogging(*ecc.Logging)
		}
		if lc := ecc.LogConfiguration; lc != nil {
			execute.LogConfiguration = &ecstypes.ExecuteCommandLogConfiguration{
				CloudWatchEncryptionEnabled: aws.ToBool(lc.CloudWatchEncryptionEnabled),
				CloudWatchLogGroupName:      lc.CloudWatchLogGroupName,
				S3BucketName:                lc.S3BucketName,
				S3EncryptionEnabled:         aws.ToBool(lc.S3EncryptionEnabled),
				S3KeyPrefix:                 lc.S3KeyPrefix,
			}
		}
		out.ExecuteCommandConfiguration = execute
	}
	if msc := c.ManagedStorageConfiguration; msc != nil {
		out.ManagedStorageConfiguration = &ecstypes.ManagedStorageConfiguration{
			KmsKeyId:                        msc.KmsKeyId,
			FargateEphemeralStorageKmsKeyId: msc.FargateEphemeralStorageKmsKeyId,
		}
	}
	return out
}

// sdk converts the service-connect-defaults block to its SDK request type,
// returning nil for a nil block so an absent default stays out of the request.
func (d *ClusterServiceConnectDefaults) sdk() *ecstypes.ClusterServiceConnectDefaultsRequest {
	if d == nil {
		return nil
	}
	return &ecstypes.ClusterServiceConnectDefaultsRequest{
		Namespace: aws.String(d.Namespace),
	}
}

// clusterSettingsSDK converts the settings list to its SDK type, returning
// nil for an empty list: in both CreateCluster and UpdateCluster a nil
// Settings member means the server-defaulted settings are left alone.
func clusterSettingsSDK(settings []ClusterSetting) []ecstypes.ClusterSetting {
	if len(settings) == 0 {
		return nil
	}
	out := make([]ecstypes.ClusterSetting, 0, len(settings))
	for _, s := range settings {
		out = append(out, ecstypes.ClusterSetting{
			Name:  ecstypes.ClusterSettingName(s.Name),
			Value: aws.String(s.Value),
		})
	}
	return out
}

// clusterStrategySDK converts the default capacity provider strategy to its
// SDK type. It always returns a non-nil slice, even for an empty input,
// because PutClusterCapacityProviders requires the member and an explicit
// empty strategy is how the call clears one.
func clusterStrategySDK(
	items []ClusterCapacityProviderStrategyItem,
) []ecstypes.CapacityProviderStrategyItem {
	out := make([]ecstypes.CapacityProviderStrategyItem, 0, len(items))
	for _, item := range items {
		out = append(out, ecstypes.CapacityProviderStrategyItem{
			CapacityProvider: aws.String(item.CapacityProvider),
			Base:             int32(aws.ToInt64(item.Base)),
			Weight:           int32(aws.ToInt64(item.Weight)),
		})
	}
	return out
}

// clusterTags converts a desired tag map into the ECS SDK tag list, ordered
// by key so the request is deterministic. It returns nil for an empty map so
// an untagged create sends no Tags member.
func clusterTags(tags map[string]string) []ecstypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ecstypes.Tag, 0, len(tags))
	for _, k := range keys {
		out = append(out, ecstypes.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return out
}
