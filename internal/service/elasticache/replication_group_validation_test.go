package elasticache

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplicationGroupValidateInputs(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ReplicationGroupResource)
		wantErr string
	}{
		{name: "valid", mutate: func(*ReplicationGroupResource) {}},
		{
			name: "40 character id",
			mutate: func(r *ReplicationGroupResource) {
				r.ReplicationGroupID = "a" + strings.Repeat("1", 39)
			},
		},
		{
			name: "empty id",
			mutate: func(r *ReplicationGroupResource) {
				r.ReplicationGroupID = ""
			},
			wantErr: "replication-group-id must be 1 to 40 letters, digits, or single " +
				"hyphens, begin with a letter, and not end with a hyphen",
		},
		{
			name: "id starts with digit",
			mutate: func(r *ReplicationGroupResource) {
				r.ReplicationGroupID = "1bad"
			},
			wantErr: "replication-group-id must be 1 to 40 letters, digits, or single " +
				"hyphens, begin with a letter, and not end with a hyphen",
		},
		{
			name: "id ends with hyphen",
			mutate: func(r *ReplicationGroupResource) {
				r.ReplicationGroupID = "bad-"
			},
			wantErr: "replication-group-id must be 1 to 40 letters, digits, or single " +
				"hyphens, begin with a letter, and not end with a hyphen",
		},
		{
			name: "id has consecutive hyphens",
			mutate: func(r *ReplicationGroupResource) {
				r.ReplicationGroupID = "bad--id"
			},
			wantErr: "replication-group-id must be 1 to 40 letters, digits, or single " +
				"hyphens, begin with a letter, and not end with a hyphen",
		},
		{
			name: "empty description",
			mutate: func(r *ReplicationGroupResource) {
				r.Description = ""
			},
			wantErr: "description must not be empty",
		},
		{
			name: "invalid engine",
			mutate: func(r *ReplicationGroupResource) {
				r.Engine = "memcached"
			},
			wantErr: "engine must be redis or valkey",
		},
		{
			name: "missing node type",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeType = nil
			},
			wantErr: "node-type is required unless joining a global group or adopting a cluster",
		},
		{
			name: "global member needs no node type",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeType = nil
				r.GlobalReplicationGroupID = new("global-test")
			},
		},
		{
			name: "primary adoption needs no node type",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeType = nil
				r.PrimaryClusterID = new("cache-primary")
			},
		},
		{
			name: "invalid replica count",
			mutate: func(r *ReplicationGroupResource) {
				r.ReplicasPerNodeGroup = int64Ptr(6)
			},
			wantErr: "replicas-per-node-group must be between 0 and 5",
		},
		{
			name: "invalid total cluster count",
			mutate: func(r *ReplicationGroupResource) {
				r.NumCacheClusters = int64Ptr(0)
			},
			wantErr: "num-cache-clusters must be between 1 and 6",
		},
		{
			name: "invalid retention",
			mutate: func(r *ReplicationGroupResource) {
				r.SnapshotRetentionLimit = int64Ptr(36)
			},
			wantErr: "snapshot-retention-limit must be between 0 and 35",
		},
		{
			name: "invalid durability",
			mutate: func(r *ReplicationGroupResource) {
				r.Durability = new("disabled")
			},
			wantErr: "durability must be one of sync, async",
		},
		{
			name: "invalid maintenance window",
			mutate: func(r *ReplicationGroupResource) {
				r.MaintenanceWindow = new("monday:01:00-monday:02:00")
			},
			wantErr: "maintenance-window must use ddd:hh:mm-ddd:hh:mm",
		},
		{
			name: "invalid snapshot window",
			mutate: func(r *ReplicationGroupResource) {
				r.SnapshotWindow = new("24:00-25:00")
			},
			wantErr: "snapshot-window must use hh:mm-hh:mm",
		},
		{
			name: "short auth token",
			mutate: func(r *ReplicationGroupResource) {
				r.AuthToken = new("too-short")
				r.TransitEncryptionEnabled = true
			},
			wantErr: "auth-token must be 16 to 128 characters",
		},
		{
			name: "auth token forbidden character",
			mutate: func(r *ReplicationGroupResource) {
				r.AuthToken = new("123456789012345@")
				r.TransitEncryptionEnabled = true
			},
			wantErr: `auth-token must not contain @, ", or /`,
		},
		{
			name: "auth requires transit encryption",
			mutate: func(r *ReplicationGroupResource) {
				r.AuthToken = new("1234567890123456")
			},
			wantErr: "auth-token requires transit-encryption-enabled",
		},
		{
			name: "auth conflicts with user groups",
			mutate: func(r *ReplicationGroupResource) {
				r.AuthToken = new("1234567890123456")
				r.TransitEncryptionEnabled = true
				r.UserGroupIDs = stringSlicePtr("users")
			},
			wantErr: "auth-token conflicts with user-group-ids",
		},
		{
			name: "multi AZ requires failover",
			mutate: func(r *ReplicationGroupResource) {
				r.MultiAZEnabled = new(true)
			},
			wantErr: "multi-az-enabled requires automatic-failover-enabled",
		},
		{
			name: "nonclustered and clustered counts conflict",
			mutate: func(r *ReplicationGroupResource) {
				r.NumCacheClusters = int64Ptr(1)
				r.NumNodeGroups = new(int64(1))
			},
			wantErr: "num-cache-clusters conflicts with clustered topology inputs",
		},
		{
			name: "node groups conflict with preferred AZs",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeGroupConfiguration = &[]ReplicationGroupNodeGroupConfiguration{{}}
				r.PreferredCacheClusterAZs = stringSlicePtr("us-east-1a")
			},
			wantErr: "node-group-configuration conflicts with count and preferred AZ inputs",
		},
		{
			name: "preferred AZ count mismatch",
			mutate: func(r *ReplicationGroupResource) {
				r.NumCacheClusters = int64Ptr(2)
				r.PreferredCacheClusterAZs = stringSlicePtr("us-east-1a")
			},
			wantErr: "preferred-cache-cluster-azs must match num-cache-clusters",
		},
		{
			name: "data tiering requires r6gd",
			mutate: func(r *ReplicationGroupResource) {
				r.DataTieringEnabled = new(true)
			},
			wantErr: "data-tiering-enabled requires a cache.r6gd node type",
		},
		{
			name: "r6gd requires data tiering",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeType = new("cache.r6gd.large")
			},
			wantErr: "cache.r6gd node types require data-tiering-enabled",
		},
		{
			name: "valid data tiering",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeType = new("cache.r6gd.large")
				r.DataTieringEnabled = new(true)
			},
		},
		{
			name: "global member conflicts with local version",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeType = nil
				r.GlobalReplicationGroupID = new("global-test")
				r.EngineVersion = new("7.1")
			},
			wantErr: "global-replication-group-id conflicts with locally inherited inputs",
		},
		{
			name: "global member conflicts with node group count",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeType = nil
				r.GlobalReplicationGroupID = new("global-test")
				r.NumNodeGroups = int64Ptr(1)
			},
			wantErr: "global-replication-group-id conflicts with locally inherited inputs",
		},
		{
			name: "global member conflicts with security groups",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeType = nil
				r.GlobalReplicationGroupID = new("global-test")
				r.SecurityGroupNames = new([]string{"default"})
			},
			wantErr: "global-replication-group-id conflicts with locally inherited inputs",
		},
		{
			name: "global member conflicts with snapshot ARNs",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeType = nil
				r.GlobalReplicationGroupID = new("global-test")
				r.SnapshotArns = new([]string{
					"arn:aws:elasticache:us-east-1:123456789012:snapshot:test",
				})
			},
			wantErr: "global-replication-group-id conflicts with locally inherited inputs",
		},
		{
			name: "global member conflicts with snapshot name",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeType = nil
				r.GlobalReplicationGroupID = new("global-test")
				r.SnapshotName = new("snapshot-test")
			},
			wantErr: "global-replication-group-id conflicts with locally inherited inputs",
		},
		{
			name: "snapshot source forbidden for clustered mode",
			mutate: func(r *ReplicationGroupResource) {
				r.ClusterMode = new("enabled")
				r.SnapshottingClusterID = new("cache-primary")
			},
			wantErr: "snapshotting-cluster-id is forbidden when cluster-mode is enabled",
		},
		{
			name: "invalid node group id",
			mutate: func(r *ReplicationGroupResource) {
				r.NodeGroupConfiguration = &[]ReplicationGroupNodeGroupConfiguration{
					{NodeGroupID: new("abc")},
				}
			},
			wantErr: "node-group-configuration[0].node-group-id must be 1 to 4 digits",
		},
		{
			name: "invalid snapshot ARN",
			mutate: func(r *ReplicationGroupResource) {
				r.SnapshotArns = stringSlicePtr("not-an-arn")
			},
			wantErr: "snapshot-arns[0] must be a valid ARN:",
		},
		{
			name: "snapshot name comma",
			mutate: func(r *ReplicationGroupResource) {
				r.SnapshotName = new("one,two")
			},
			wantErr: "snapshot-name must not contain a comma",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReplicationGroupResource()
			tt.mutate(r)
			err := r.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestReplicationGroupAutomaticFailoverTopologyValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ReplicationGroupResource)
		wantErr bool
	}{
		{name: "no topology", mutate: func(*ReplicationGroupResource) {}, wantErr: true},
		{name: "two nonclustered nodes", mutate: func(r *ReplicationGroupResource) {
			r.NumCacheClusters = new(int64(2))
		}},
		{name: "disabled cluster mode", mutate: func(r *ReplicationGroupResource) {
			r.ClusterMode = new("disabled")
		}, wantErr: true},
		{name: "compatible cluster mode", mutate: func(r *ReplicationGroupResource) {
			r.ClusterMode = new("compatible")
		}},
		{name: "enabled cluster mode", mutate: func(r *ReplicationGroupResource) {
			r.ClusterMode = new("enabled")
		}},
		{name: "node group count", mutate: func(r *ReplicationGroupResource) {
			r.NumNodeGroups = new(int64(1))
		}},
		{name: "replicas per node group", mutate: func(r *ReplicationGroupResource) {
			r.ReplicasPerNodeGroup = new(int64(0))
		}},
		{name: "node group configuration", mutate: func(r *ReplicationGroupResource) {
			r.NodeGroupConfiguration = &[]ReplicationGroupNodeGroupConfiguration{{}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReplicationGroupResource()
			r.AutomaticFailoverEnabled = true
			tt.mutate(r)

			err := r.ValidateInputs(context.Background(), nil)

			if tt.wantErr {
				require.EqualError(t, err,
					"automatic-failover-enabled requires at least two cache clusters")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestReplicationGroupTagValidation(t *testing.T) {
	tests := []struct {
		name    string
		tags    map[string]string
		wantErr string
	}{
		{name: "50 tags", tags: numberedReplicationGroupTags(50)},
		{name: "51 tags", tags: numberedReplicationGroupTags(51),
			wantErr: "tags must have at most 50 entries"},
		{name: "empty key", tags: map[string]string{"": "value"},
			wantErr: "tag key must be 1 to 128 characters"},
		{name: "128 multibyte characters",
			tags: map[string]string{strings.Repeat("é", 128): strings.Repeat("界", 256)}},
		{name: "129 multibyte characters",
			tags:    map[string]string{strings.Repeat("é", 129): "value"},
			wantErr: "tag key must be 1 to 128 characters"},
		{name: "257 multibyte value", tags: map[string]string{
			"key": strings.Repeat("界", 257),
		}, wantErr: "tag value must be at most 256 characters"},
		{name: "empty value", tags: map[string]string{"key": ""}},
		{name: "lowercase reserved prefix", tags: map[string]string{"aws:key": "value"},
			wantErr: "tag key must not begin with aws:"},
		{name: "uppercase prefix accepted", tags: map[string]string{"AWS:key": "value"}},
		{name: "reserved text allowed in value", tags: map[string]string{"key": "aws:value"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReplicationGroupResource()
			r.Tags = &tt.tags
			err := r.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestReplicationGroupReplacementAndIdentity(t *testing.T) {
	r := testReplicationGroupResource()
	assert.ElementsMatch(t, []string{
		"at-rest-encryption-enabled",
		"data-tiering-enabled",
		"durability",
		"engine",
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
	}, r.ReplaceFields())

	r.Engine = "valkey"
	r.NodeGroupConfiguration = &[]ReplicationGroupNodeGroupConfiguration{{}}
	fields := r.ReplaceFields()
	assert.NotContains(t, fields, "engine")
	assert.Contains(t, fields, "replicas-per-node-group")

	prior := *testReplicationGroupResource()
	current := prior
	current.ReplicationGroupID = strings.ToUpper(prior.ReplicationGroupID)
	assert.True(t, r.EquivalentInput("replication-group-id", prior, current))
	current.ReplicationGroupID = "different"
	assert.False(t, r.EquivalentInput("replication-group-id", prior, current))
	assert.False(t, r.EquivalentInput("description", prior, prior))

	assert.Equal(t, "prior-id", replicationGroupIdentity("NEW-ID",
		&ReplicationGroupResourceOutput{ReplicationGroupID: "prior-id"}))
	assert.Equal(t, "new-id", replicationGroupIdentity("NEW-ID", nil))
}

func numberedReplicationGroupTags(count int) map[string]string {
	tags := make(map[string]string, count)
	for i := range count {
		tags[strings.Repeat("k", i/10+1)+string(rune('a'+i%10))] = "value"
	}
	return tags
}

//go:fix inline
func stringPtr(value string) *string { return new(value) }

//go:fix inline
func int64Ptr(value int64) *int64 { return new(value) }

//go:fix inline
func boolPtr(value bool) *bool { return new(value) }

//go:fix inline
func stringSlicePtr(values ...string) *[]string { return new(values) }
