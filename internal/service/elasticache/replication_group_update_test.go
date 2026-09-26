package elasticache

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplicationGroupValidateUpdateTransitions(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ReplicationGroupResource, *ReplicationGroupResource)
		actual  string
		wantErr string
	}{
		{
			name: "redis to valkey with version",
			mutate: func(prior, current *ReplicationGroupResource) {
				prior.EngineVersion = new("7.1")
				current.Engine = "valkey"
				current.EngineVersion = new("7.2")
			},
			actual: "7.1",
		},
		{
			name: "redis to valkey requires version",
			mutate: func(_, current *ReplicationGroupResource) {
				current.Engine = "valkey"
			},
			actual:  "7.1",
			wantErr: "redis to valkey requires an explicit changed engine-version",
		},
		{
			name: "valkey to redis replaces",
			mutate: func(prior, current *ReplicationGroupResource) {
				prior.Engine = "valkey"
				prior.EngineVersion = new("7.2")
				current.Engine = "redis"
				current.EngineVersion = new("7.1")
			},
			actual:  "7.2",
			wantErr: "engine change from valkey to redis requires replacement",
		},
		{
			name: "upgrade",
			mutate: func(prior, current *ReplicationGroupResource) {
				prior.EngineVersion = new("6.2")
				current.EngineVersion = new("7.1")
			},
			actual: "6.2",
		},
		{
			name: "downgrade replaces",
			mutate: func(prior, current *ReplicationGroupResource) {
				prior.EngineVersion = new("7.1")
				current.EngineVersion = new("6.2")
			},
			actual:  "7.1",
			wantErr: "engine-version downgrade from 7.1 to 6.2 requires replacement",
		},
		{
			name: "disabled to compatible",
			mutate: func(prior, current *ReplicationGroupResource) {
				prior.ClusterMode = new("disabled")
				current.ClusterMode = new("compatible")
			},
			actual: "7.1",
		},
		{
			name: "compatible to enabled",
			mutate: func(prior, current *ReplicationGroupResource) {
				prior.ClusterMode = new("compatible")
				current.ClusterMode = new("enabled")
			},
			actual: "7.1",
		},
		{
			name: "disabled to enabled rejected",
			mutate: func(prior, current *ReplicationGroupResource) {
				prior.ClusterMode = new("disabled")
				current.ClusterMode = new("enabled")
			},
			actual:  "7.1",
			wantErr: "cluster-mode transition from disabled to enabled is not supported",
		},
		{
			name: "old version transit toggle replaces",
			mutate: func(prior, current *ReplicationGroupResource) {
				prior.TransitEncryptionEnabled = false
				current.TransitEncryptionEnabled = true
				current.TransitEncryptionMode = new("preferred")
			},
			actual: "7.0.4",
			wantErr: "changing transit-encryption-enabled on engine version 7.0.4 " +
				"requires replacement",
		},
		{
			name: "transit toggle at minimum version",
			mutate: func(prior, current *ReplicationGroupResource) {
				prior.TransitEncryptionEnabled = false
				current.TransitEncryptionEnabled = true
				current.TransitEncryptionMode = new("preferred")
			},
			actual: "7.0.5",
		},
		{
			name: "transit toggle requires preferred",
			mutate: func(prior, current *ReplicationGroupResource) {
				prior.TransitEncryptionEnabled = false
				current.TransitEncryptionEnabled = true
				current.TransitEncryptionMode = new("required")
			},
			actual:  "7.1",
			wantErr: "enabling transit encryption requires transit-encryption-mode preferred",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priorInputs := testReplicationGroupResource()
			current := testReplicationGroupResource()
			tt.mutate(priorInputs, current)
			prior := runtime.Prior[ReplicationGroupResource, *ReplicationGroupResourceOutput, *awsCfg]{
				Inputs: *priorInputs,
			}
			observed := &ReplicationGroupResourceOutput{EngineVersion: tt.actual}

			err := current.validateUpdateTransitions(prior, observed)

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestReplicationGroupStandardModifyChangedFields(t *testing.T) {
	t.Run("description only", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		current := testReplicationGroupResource()
		current.Description = "updated"

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, testReplicationGroup(), false, false)

		assert.True(t, changed)
		assert.Equal(t, "updated", aws.ToString(in.ReplicationGroupDescription))
		assert.Nil(t, in.CacheNodeType)
		assert.Nil(t, in.EngineVersion)
		assert.Nil(t, in.NotificationTopicArn)
	})

	t.Run("notification clear sends empty string", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		prior.NotificationTopicArn = new("arn:aws:sns:us-east-1:123:topic")
		current := testReplicationGroupResource()

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, testReplicationGroup(), false, false)

		assert.True(t, changed)
		require.NotNil(t, in.NotificationTopicArn)
		assert.Empty(t, *in.NotificationTopicArn)
	})

	t.Run("optional removal without clear stops management", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		prior.NodeType = new("cache.t3.small")
		prior.ParameterGroupName = new("custom")
		prior.MaintenanceWindow = new("sun:01:00-sun:02:00")
		prior.SnapshotWindow = new("01:00-02:00")
		current := testReplicationGroupResource()
		current.NodeType = nil

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, testReplicationGroup(), false, false)

		assert.False(t, changed)
		assert.Nil(t, in.CacheNodeType)
		assert.Nil(t, in.CacheParameterGroupName)
		assert.Nil(t, in.PreferredMaintenanceWindow)
		assert.Nil(t, in.SnapshotWindow)
	})

	t.Run("retention removal sends zero", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		prior.SnapshotRetentionLimit = int64Ptr(7)
		current := testReplicationGroupResource()

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, testReplicationGroup(), false, false)

		assert.True(t, changed)
		require.NotNil(t, in.SnapshotRetentionLimit)
		assert.Zero(t, *in.SnapshotRetentionLimit)
	})

	t.Run("retention enable selects live primary", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		prior.SnapshotRetentionLimit = int64Ptr(0)
		current := testReplicationGroupResource()
		current.SnapshotRetentionLimit = int64Ptr(7)
		group := testReplicationGroup()
		group.NodeGroups[0].NodeGroupMembers = []elasticachetypes.NodeGroupMember{
			{CacheClusterId: aws.String("replica"), CurrentRole: aws.String("replica")},
			{CacheClusterId: aws.String("new-primary"), CurrentRole: aws.String("primary")},
		}

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, group, false, false)

		assert.True(t, changed)
		assert.Equal(t, "new-primary", aws.ToString(in.SnapshottingClusterId))
	})

	t.Run("clustered retention omits source", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		prior.SnapshotRetentionLimit = int64Ptr(0)
		current := testReplicationGroupResource()
		current.SnapshotRetentionLimit = int64Ptr(7)
		group := testReplicationGroup()
		group.ClusterEnabled = aws.Bool(true)

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, group, false, false)

		assert.True(t, changed)
		assert.Nil(t, in.SnapshottingClusterId)
	})

	t.Run("log remove and update", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		prior.LogDeliveryConfiguration = &[]ReplicationGroupLogDeliveryConfiguration{
			{
				Destination: "old", DestinationType: "cloudwatch-logs",
				LogFormat: "text", LogType: "slow-log",
			},
			{
				Destination: "engine", DestinationType: "cloudwatch-logs",
				LogFormat: "text", LogType: "engine-log",
			},
		}
		current := testReplicationGroupResource()
		current.LogDeliveryConfiguration = &[]ReplicationGroupLogDeliveryConfiguration{
			{
				Destination: "new", DestinationType: "kinesis-firehose",
				LogFormat: "json", LogType: "slow-log",
			},
		}

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, testReplicationGroup(), false, false)

		assert.True(t, changed)
		require.Len(t, in.LogDeliveryConfigurations, 2)
		assert.True(t, aws.ToBool(in.LogDeliveryConfigurations[0].Enabled))
		assert.Equal(t, elasticachetypes.LogTypeSlowLog,
			in.LogDeliveryConfigurations[0].LogType)
		assert.False(t, aws.ToBool(in.LogDeliveryConfigurations[1].Enabled))
		assert.Equal(t, elasticachetypes.LogTypeEngineLog,
			in.LogDeliveryConfigurations[1].LogType)
	})

	t.Run("omitted logs disable prior entries", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		prior.LogDeliveryConfiguration = &[]ReplicationGroupLogDeliveryConfiguration{
			{
				Destination: "logs", DestinationType: "cloudwatch-logs",
				LogFormat: "text", LogType: "engine-log",
			},
		}
		current := testReplicationGroupResource()

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, testReplicationGroup(), false, false)

		assert.True(t, changed)
		require.Len(t, in.LogDeliveryConfigurations, 1)
		assert.False(t, aws.ToBool(in.LogDeliveryConfigurations[0].Enabled))
	})

	t.Run("user group differences", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		prior.UserGroupIDs = stringSlicePtr("keep", "remove")
		current := testReplicationGroupResource()
		current.UserGroupIDs = stringSlicePtr("add", "keep")

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, testReplicationGroup(), false, false)

		assert.True(t, changed)
		assert.Equal(t, []string{"add"}, in.UserGroupIdsToAdd)
		assert.Equal(t, []string{"remove"}, in.UserGroupIdsToRemove)
	})

	t.Run("omitted user groups remove prior entries", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		prior.UserGroupIDs = stringSlicePtr("remove")
		current := testReplicationGroupResource()

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, testReplicationGroup(), false, false)

		assert.True(t, changed)
		assert.Equal(t, []string{"remove"}, in.UserGroupIdsToRemove)
	})

	t.Run("global member ignores inherited changes", func(t *testing.T) {
		prior := *testReplicationGroupResource()
		prior.NodeType = nil
		prior.GlobalReplicationGroupID = new("global-test")
		current := testReplicationGroupResource()
		current.NodeType = nil
		current.GlobalReplicationGroupID = new("global-test")
		current.AutomaticFailoverEnabled = true
		current.EngineVersion = new("7.1")
		current.TransitEncryptionEnabled = true

		in, changed := current.standardModifyInput(
			testReplicationGroupID, prior, testReplicationGroup(), false, false)

		assert.False(t, changed)
		assert.Nil(t, in.AutomaticFailoverEnabled)
		assert.Nil(t, in.EngineVersion)
		assert.Nil(t, in.TransitEncryptionEnabled)
	})
}

func TestReplicationGroupLogAndVersionHelpers(t *testing.T) {
	assert.Equal(t, -1, compareEngineVersions("6.2", "7.0"))
	assert.Equal(t, 0, compareEngineVersions("7", "7.0.0"))
	assert.Equal(t, 1, compareEngineVersions("7.1.2", "7.1"))
	assert.False(t, engineSupportsSlowLog("redis", "5.0.6"))
	assert.True(t, engineSupportsSlowLog("redis", "6.0"))
	assert.True(t, engineSupportsSlowLog("valkey", "7.2"))
}
