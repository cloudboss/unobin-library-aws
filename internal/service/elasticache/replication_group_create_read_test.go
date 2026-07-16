package elasticache

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplicationGroupCreateRequest(t *testing.T) {
	r := testReplicationGroupResource()
	r.NumNodeGroups = int64Ptr(1)
	r.ReplicasPerNodeGroup = int64Ptr(0)
	r.Tags = &map[string]string{"team": "platform"}
	client := newFakeReplicationGroupClient()

	out, err := r.create(context.Background(), client, "us-east-1",
		fastReplicationGroupOptions())

	require.NoError(t, err)
	assert.Equal(t, testReplicationGroupID, out.ReplicationGroupID)
	require.Len(t, client.createInputs, 1)
	in := client.createInputs[0]
	assert.Equal(t, testReplicationGroupID, aws.ToString(in.ReplicationGroupId))
	assert.Equal(t, "test replication group",
		aws.ToString(in.ReplicationGroupDescription))
	assert.Equal(t, "redis", aws.ToString(in.Engine))
	require.NotNil(t, in.AutomaticFailoverEnabled)
	assert.False(t, *in.AutomaticFailoverEnabled)
	require.NotNil(t, in.TransitEncryptionEnabled)
	assert.False(t, *in.TransitEncryptionEnabled)
	require.NotNil(t, in.ReplicasPerNodeGroup)
	assert.Zero(t, *in.ReplicasPerNodeGroup)
	assert.Equal(t, map[string]string{"team": "platform"}, tagMap(in.Tags))
}

func TestReplicationGroupCreateGlobalMemberOmitsInheritedInputs(t *testing.T) {
	r := testReplicationGroupResource()
	r.NodeType = nil
	r.GlobalReplicationGroupID = new("global-test")
	r.AutomaticFailoverEnabled = true
	client := newFakeReplicationGroupClient()

	_, err := r.create(context.Background(), client, "us-east-1",
		fastReplicationGroupOptions())

	require.NoError(t, err)
	in := client.createInputs[0]
	assert.Nil(t, in.AtRestEncryptionEnabled)
	assert.Nil(t, in.AuthToken)
	assert.Nil(t, in.AutomaticFailoverEnabled)
	assert.Nil(t, in.CacheNodeType)
	assert.Nil(t, in.CacheParameterGroupName)
	assert.Nil(t, in.Engine)
	assert.Nil(t, in.EngineVersion)
	assert.Nil(t, in.TransitEncryptionEnabled)
	assert.Empty(t, in.TransitEncryptionMode)
	assert.Contains(t, client.calls, "describe-global")
}

func TestReplicationGroupCreateTaggedFallback(t *testing.T) {
	r := testReplicationGroupResource()
	r.Tags = &map[string]string{"team": "platform"}
	client := newFakeReplicationGroupClient()
	client.createErrors = []error{
		&smithy.GenericAPIError{Code: "UnsupportedOperation", Message: "unavailable"},
		nil,
	}

	_, err := r.create(context.Background(), client, "us-iso-east-1",
		fastReplicationGroupOptions())

	require.NoError(t, err)
	require.Len(t, client.createInputs, 2)
	assert.NotEmpty(t, client.createInputs[0].Tags)
	assert.Nil(t, client.createInputs[1].Tags)
	require.Len(t, client.addTagInputs, 1)
	assert.Equal(t, testReplicationGroupArn,
		aws.ToString(client.addTagInputs[0].ResourceName))
	assert.Equal(t, map[string]string{"team": "platform"},
		tagMap(client.addTagInputs[0].Tags))
	assert.Less(t, callIndex(client.calls, "describe"), callIndex(client.calls, "add-tags"))
}

func TestReplicationGroupCreateDoesNotFallbackInStandardPartition(t *testing.T) {
	r := testReplicationGroupResource()
	r.Tags = &map[string]string{"team": "platform"}
	client := newFakeReplicationGroupClient()
	sentinel := &smithy.GenericAPIError{Code: "UnsupportedOperation", Message: "failed"}
	client.createErrors = []error{sentinel}

	out, err := r.create(context.Background(), client, "us-east-1",
		fastReplicationGroupOptions())

	assert.Nil(t, out)
	assert.ErrorIs(t, err, sentinel)
	assert.Len(t, client.createInputs, 1)
}

func TestReplicationGroupCreateValidatesBeforeMutation(t *testing.T) {
	r := testReplicationGroupResource()
	tags := numberedReplicationGroupTags(51)
	r.Tags = &tags
	client := newFakeReplicationGroupClient()

	out, err := r.create(context.Background(), client, "us-east-1",
		fastReplicationGroupOptions())

	assert.Nil(t, out)
	assert.EqualError(t, err, "tags must have at most 50 entries")
	assert.Empty(t, client.calls)
}

func TestReplicationGroupCreateAppliesSnapshotSourceAfterSettling(t *testing.T) {
	r := testReplicationGroupResource()
	r.SnapshottingClusterID = new(testCacheClusterID)
	client := newFakeReplicationGroupClient()

	_, err := r.create(context.Background(), client, "us-east-1",
		fastReplicationGroupOptions())

	require.NoError(t, err)
	require.Len(t, client.modifyInputs, 1)
	assert.Equal(t, testCacheClusterID,
		aws.ToString(client.modifyInputs[0].SnapshottingClusterId))
	assert.Less(t, callIndex(client.calls, "describe"), callIndex(client.calls, "modify"))
}

func TestReplicationGroupCreatePropagatesSnapshotSourceNoModifications(t *testing.T) {
	r := testReplicationGroupResource()
	r.SnapshottingClusterID = new(testCacheClusterID)
	client := newFakeReplicationGroupClient()
	noModifications := &elasticachetypes.InvalidParameterCombinationException{
		Message: aws.String("No modifications were requested"),
	}
	client.modifyErrors = []error{noModifications}

	out, err := r.create(context.Background(), client, "us-east-1",
		fastReplicationGroupOptions())

	assert.Nil(t, out)
	assert.ErrorIs(t, err, noModifications)
	assert.Len(t, client.modifyInputs, 1)
}

func TestReplicationGroupCreateRequiresPreferredTransitMode(t *testing.T) {
	r := testReplicationGroupResource()
	r.TransitEncryptionEnabled = true
	r.TransitEncryptionMode = new("required")
	client := newFakeReplicationGroupClient()

	out, err := r.create(context.Background(), client, "us-east-1",
		fastReplicationGroupOptions())

	assert.Nil(t, out)
	assert.EqualError(t, err,
		"creating with transit encryption requires transit-encryption-mode preferred")
	assert.Empty(t, client.calls)
}

func TestReplicationGroupReadUsesFinalAvailableObservation(t *testing.T) {
	pending := testReplicationGroup()
	pending.Status = aws.String("modifying")
	available := testReplicationGroup()
	available.NodeGroups[0].PrimaryEndpoint.Address = aws.String("final.example.test")
	client := newFakeReplicationGroupClient()
	client.describeOutputs = []*elasticache.DescribeReplicationGroupsOutput{
		{ReplicationGroups: []elasticachetypes.ReplicationGroup{*pending}},
		{ReplicationGroups: []elasticachetypes.ReplicationGroup{*available}},
	}

	out, err := testReplicationGroupResource().read(context.Background(), client,
		testReplicationGroupID, fastReplicationGroupOptions())

	require.NoError(t, err)
	assert.Equal(t, "final.example.test", out.PrimaryEndpointAddress)
	assert.Equal(t, "reader.example.test", out.ReaderEndpointAddress)
	assert.Equal(t, int64(6379), out.Port)
	assert.Equal(t, "7.1.0", out.EngineVersionActual)
	assert.Equal(t, "default.redis7", out.ParameterGroupName)
	require.Len(t, client.describeClusterInputs, 1)
	assert.True(t, aws.ToBool(client.describeClusterInputs[0].ShowCacheNodeInfo))
}

func TestReplicationGroupReadClusteredEndpoint(t *testing.T) {
	group := testReplicationGroup()
	group.ClusterEnabled = aws.Bool(true)
	group.ConfigurationEndpoint = &elasticachetypes.Endpoint{
		Address: aws.String("config.example.test"),
		Port:    aws.Int32(6380),
	}
	client := newFakeReplicationGroupClient()
	client.describeOutputs = []*elasticache.DescribeReplicationGroupsOutput{
		{ReplicationGroups: []elasticachetypes.ReplicationGroup{*group}},
	}

	out, err := testReplicationGroupResource().read(context.Background(), client,
		testReplicationGroupID, fastReplicationGroupOptions())

	require.NoError(t, err)
	assert.True(t, out.ClusterEnabled)
	assert.Equal(t, "config.example.test", out.ConfigurationEndpointAddress)
	assert.Equal(t, int64(6380), out.Port)
	assert.Empty(t, out.PrimaryEndpointAddress)
	assert.Empty(t, out.ReaderEndpointAddress)
}

func TestReplicationGroupReadWithoutMembers(t *testing.T) {
	group := testReplicationGroup()
	group.MemberClusters = nil
	group.NodeGroups = nil
	client := newFakeReplicationGroupClient()
	client.describeOutputs = []*elasticache.DescribeReplicationGroupsOutput{
		{ReplicationGroups: []elasticachetypes.ReplicationGroup{*group}},
	}

	out, err := testReplicationGroupResource().read(context.Background(), client,
		testReplicationGroupID, fastReplicationGroupOptions())

	require.NoError(t, err)
	assert.Zero(t, out.NumCacheClusters)
	assert.Zero(t, out.NumNodeGroups)
	assert.Empty(t, out.PrimaryEndpointAddress)
	assert.Empty(t, client.describeClusterInputs)
}

func TestReplicationGroupReadAbsenceAndUnexpectedStates(t *testing.T) {
	t.Run("typed not found", func(t *testing.T) {
		client := newFakeReplicationGroupClient()
		client.describeErrors = []error{
			&elasticachetypes.ReplicationGroupNotFoundFault{},
		}

		out, err := testReplicationGroupResource().read(context.Background(), client,
			testReplicationGroupID, fastReplicationGroupOptions())

		assert.Nil(t, out)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	})

	t.Run("empty result", func(t *testing.T) {
		client := newFakeReplicationGroupClient()
		client.describeOutputs = []*elasticache.DescribeReplicationGroupsOutput{{}}

		out, err := testReplicationGroupResource().read(context.Background(), client,
			testReplicationGroupID, fastReplicationGroupOptions())

		assert.Nil(t, out)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	})

	t.Run("deleting", func(t *testing.T) {
		group := testReplicationGroup()
		group.Status = aws.String("deleting")
		client := newFakeReplicationGroupClient()
		client.describeOutputs = []*elasticache.DescribeReplicationGroupsOutput{
			{ReplicationGroups: []elasticachetypes.ReplicationGroup{*group}},
		}

		out, err := testReplicationGroupResource().read(context.Background(), client,
			testReplicationGroupID, fastReplicationGroupOptions())

		assert.Nil(t, out)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	})

	t.Run("unexpected", func(t *testing.T) {
		group := testReplicationGroup()
		group.Status = aws.String("create-failed")
		client := newFakeReplicationGroupClient()
		client.describeOutputs = []*elasticache.DescribeReplicationGroupsOutput{
			{ReplicationGroups: []elasticachetypes.ReplicationGroup{*group}},
		}

		out, err := testReplicationGroupResource().read(context.Background(), client,
			testReplicationGroupID, fastReplicationGroupOptions())

		assert.Nil(t, out)
		assert.EqualError(t, err,
			`replication group unobin-test-rg entered unexpected status "create-failed"`)
	})

	t.Run("empty member cluster result", func(t *testing.T) {
		client := newFakeReplicationGroupClient()
		client.describeClusterOutputs = []*elasticache.DescribeCacheClustersOutput{{}}

		out, err := testReplicationGroupResource().read(context.Background(), client,
			testReplicationGroupID, fastReplicationGroupOptions())

		assert.Nil(t, out)
		assert.EqualError(t, err,
			"describe cache clusters: no result for id "+testCacheClusterID)
	})

	t.Run("other describe error", func(t *testing.T) {
		client := newFakeReplicationGroupClient()
		sentinel := errors.New("network failed")
		client.describeErrors = []error{sentinel}

		_, err := testReplicationGroupResource().read(context.Background(), client,
			testReplicationGroupID, fastReplicationGroupOptions())

		assert.ErrorIs(t, err, sentinel)
	})
}

func callIndex(calls []string, want string) int {
	for i, call := range calls {
		if call == want {
			return i
		}
	}
	return -1
}
