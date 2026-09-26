package elasticache

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplicationGroupUpdateOrdersClusteredOperations(t *testing.T) {
	priorInputs := testReplicationGroupResource()
	priorInputs.ClusterMode = new("enabled")
	priorInputs.NumNodeGroups = int64Ptr(2)
	priorInputs.ReplicasPerNodeGroup = int64Ptr(2)
	priorInputs.PrimaryClusterID = new("old-primary")
	priorInputs.TransitEncryptionEnabled = true
	priorInputs.TransitEncryptionMode = new("preferred")
	priorInputs.AuthToken = new("1234567890123456")
	current := *priorInputs
	current.Description = "updated"
	current.PrimaryClusterID = new("new-primary")
	current.AuthToken = new("abcdefghijklmnop")
	prior := testReplicationGroupPrior(*priorInputs)
	prior.Observed.NumNodeGroups = 1
	prior.Observed.ReplicasPerNodeGroup = 0
	client := newFakeReplicationGroupClient()

	_, err := current.update(context.Background(), client, prior,
		fastReplicationGroupOptions())

	require.NoError(t, err)
	assert.Equal(t, []string{"shards", "increase", "modify", "modify", "modify"},
		mutationCalls(client.calls))
	require.Len(t, client.modifyInputs, 3)
	assert.Equal(t, "updated",
		aws.ToString(client.modifyInputs[0].ReplicationGroupDescription))
	assert.Equal(t, "new-primary", aws.ToString(client.modifyInputs[1].PrimaryClusterId))
	assert.Equal(t, "abcdefghijklmnop", aws.ToString(client.modifyInputs[2].AuthToken))
	assert.Equal(t, elasticachetypes.AuthTokenUpdateStrategyTypeRotate,
		client.modifyInputs[2].AuthTokenUpdateStrategy)
}

func TestReplicationGroupUpdateRepairsTopologyDrift(t *testing.T) {
	t.Run("nonclustered increase precedes standard modify", func(t *testing.T) {
		priorInputs := testReplicationGroupResource()
		priorInputs.NumCacheClusters = int64Ptr(3)
		current := *priorInputs
		current.Description = "updated"
		prior := testReplicationGroupPrior(*priorInputs)
		prior.Observed.NumCacheClusters = 1
		client := newFakeReplicationGroupClient()

		_, err := current.update(context.Background(), client, prior,
			fastReplicationGroupOptions())

		require.NoError(t, err)
		assert.Equal(t, []string{"increase", "modify"}, mutationCalls(client.calls))
		require.Len(t, client.increaseInputs, 1)
		assert.Equal(t, int32(2), aws.ToInt32(client.increaseInputs[0].NewReplicaCount))
	})

	t.Run("nonclustered decrease follows standard modify", func(t *testing.T) {
		priorInputs := testReplicationGroupResource()
		priorInputs.NumCacheClusters = int64Ptr(1)
		current := *priorInputs
		current.Description = "updated"
		prior := testReplicationGroupPrior(*priorInputs)
		prior.Observed.NumCacheClusters = 3
		client := newFakeReplicationGroupClient()

		_, err := current.update(context.Background(), client, prior,
			fastReplicationGroupOptions())

		require.NoError(t, err)
		assert.Equal(t, []string{"modify", "decrease"}, mutationCalls(client.calls))
		require.Len(t, client.decreaseInputs, 1)
		assert.Zero(t, aws.ToInt32(client.decreaseInputs[0].NewReplicaCount))
	})
}

func TestReplicationGroupUpdateWaitsForMembersBeforeStandardModify(t *testing.T) {
	const newClusterID = "unobin-test-rg-002"
	priorInputs := testReplicationGroupResource()
	priorInputs.NumCacheClusters = new(int64(2))
	current := *priorInputs
	current.Description = "updated"
	prior := testReplicationGroupPrior(*priorInputs)
	prior.Observed.NumCacheClusters = 1
	group := testReplicationGroup()
	group.MemberClusters = []string{testCacheClusterID, newClusterID}
	groupOutput := &elasticache.DescribeReplicationGroupsOutput{
		ReplicationGroups: []elasticachetypes.ReplicationGroup{*group},
	}
	primaryCluster := testReplicationGroupCacheCluster()
	primaryCluster.CacheClusterStatus = aws.String("available")
	creatingCluster := testReplicationGroupCacheCluster()
	creatingCluster.CacheClusterId = aws.String(newClusterID)
	creatingCluster.CacheClusterStatus = aws.String("creating")
	availableCluster := testReplicationGroupCacheCluster()
	availableCluster.CacheClusterId = aws.String(newClusterID)
	availableCluster.CacheClusterStatus = aws.String("available")
	client := newFakeReplicationGroupClient()
	client.describeOutputs = []*elasticache.DescribeReplicationGroupsOutput{
		groupOutput,
		groupOutput,
		groupOutput,
		groupOutput,
		groupOutput,
	}
	client.describeClusterOutputs = []*elasticache.DescribeCacheClustersOutput{
		{CacheClusters: []elasticachetypes.CacheCluster{*primaryCluster}},
		{CacheClusters: []elasticachetypes.CacheCluster{*creatingCluster}},
		{CacheClusters: []elasticachetypes.CacheCluster{*availableCluster}},
	}

	_, err := current.update(context.Background(), client, prior,
		fastReplicationGroupOptions())

	require.NoError(t, err)
	modifyIndex := callIndex(client.calls, "modify")
	require.Greater(t, modifyIndex, 0)
	require.GreaterOrEqual(t, len(client.describeClusterInputs), 3)
	assert.Equal(t, testCacheClusterID,
		aws.ToString(client.describeClusterInputs[0].CacheClusterId))
	assert.Equal(t, newClusterID,
		aws.ToString(client.describeClusterInputs[1].CacheClusterId))
	assert.Equal(t, newClusterID,
		aws.ToString(client.describeClusterInputs[2].CacheClusterId))
	memberDescribeCount := 0
	for i, call := range client.calls {
		if call == "describe-cluster" && memberDescribeCount < 3 {
			assert.Less(t, i, modifyIndex)
			memberDescribeCount++
		}
	}
	assert.Equal(t, 3, memberDescribeCount)
}

func TestWaitReplicationGroupMemberAvailableWaitsForPendingStatuses(t *testing.T) {
	for _, status := range []string{"creating", "deleting", "modifying", "snapshotting"} {
		t.Run(status, func(t *testing.T) {
			pending := testReplicationGroupCacheCluster()
			pending.CacheClusterStatus = aws.String(status)
			available := testReplicationGroupCacheCluster()
			available.CacheClusterStatus = aws.String("available")
			client := newFakeReplicationGroupClient()
			client.describeClusterOutputs = []*elasticache.DescribeCacheClustersOutput{
				{CacheClusters: []elasticachetypes.CacheCluster{*pending}},
				{CacheClusters: []elasticachetypes.CacheCluster{*available}},
			}

			err := waitReplicationGroupMemberAvailable(context.Background(), client,
				testCacheClusterID, fastReplicationGroupOptions().waitOptions...)

			require.NoError(t, err)
			require.Len(t, client.describeClusterInputs, 2)
		})
	}
}

func TestReplicationGroupShardScaleDownChoosesLargestIDs(t *testing.T) {
	group := testReplicationGroup()
	group.NodeGroups = []elasticachetypes.NodeGroup{
		{NodeGroupId: aws.String("0003")},
		{NodeGroupId: aws.String("0001")},
		{NodeGroupId: aws.String("0002")},
	}
	client := newFakeReplicationGroupClient()
	client.describeOutputs = []*elasticache.DescribeReplicationGroupsOutput{
		{ReplicationGroups: []elasticachetypes.ReplicationGroup{*group}},
	}

	err := testReplicationGroupResource().updateShardCount(context.Background(), client,
		testReplicationGroupID, 1, fastReplicationGroupOptions())

	require.NoError(t, err)
	require.Len(t, client.shardInputs, 1)
	assert.Equal(t, []string{"0002", "0003"}, client.shardInputs[0].NodeGroupsToRemove)
	assert.Equal(t, int32(1), aws.ToInt32(client.shardInputs[0].NodeGroupCount))
	mutationIndex := callIndex(client.calls, "shards")
	assert.Greater(t, mutationIndex, callIndex(client.calls, "describe"))
	assert.Contains(t, client.calls[mutationIndex+1:], "describe")
}

func TestReplicationGroupPrimaryPromotionDisablesAndRestoresFailover(t *testing.T) {
	current := testReplicationGroupResource()
	current.PrimaryClusterID = new("new-primary")
	current.AutomaticFailoverEnabled = true
	current.MultiAZEnabled = new(true)
	group := testReplicationGroup()
	group.AutomaticFailover = elasticachetypes.AutomaticFailoverStatusEnabled
	group.MultiAZ = elasticachetypes.MultiAZStatusEnabled
	client := newFakeReplicationGroupClient()
	client.describeOutputs = []*elasticache.DescribeReplicationGroupsOutput{
		{ReplicationGroups: []elasticachetypes.ReplicationGroup{*group}},
	}

	err := current.promotePrimaryCluster(context.Background(), client,
		testReplicationGroupID, fastReplicationGroupOptions())

	require.NoError(t, err)
	require.Len(t, client.modifyInputs, 3)
	assert.False(t, aws.ToBool(client.modifyInputs[0].AutomaticFailoverEnabled))
	assert.False(t, aws.ToBool(client.modifyInputs[0].MultiAZEnabled))
	assert.Equal(t, "new-primary", aws.ToString(client.modifyInputs[1].PrimaryClusterId))
	assert.True(t, aws.ToBool(client.modifyInputs[2].AutomaticFailoverEnabled))
	assert.True(t, aws.ToBool(client.modifyInputs[2].MultiAZEnabled))
}

func TestReplicationGroupAuthDeleteAddsReplacementGroup(t *testing.T) {
	prior := *testReplicationGroupResource()
	prior.UserGroupIDs = stringSlicePtr("old")
	current := testReplicationGroupResource()
	current.AuthTokenUpdateStrategy = "DELETE"
	current.UserGroupIDs = stringSlicePtr("replacement")
	client := newFakeReplicationGroupClient()

	err := current.applyAuthUpdate(context.Background(), client, testReplicationGroupID,
		prior, fastReplicationGroupOptions())

	require.NoError(t, err)
	require.Len(t, client.modifyInputs, 1)
	in := client.modifyInputs[0]
	assert.Nil(t, in.AuthToken)
	assert.Equal(t, elasticachetypes.AuthTokenUpdateStrategyTypeDelete,
		in.AuthTokenUpdateStrategy)
	assert.Equal(t, []string{"replacement"}, in.UserGroupIdsToAdd)
	assert.Equal(t, []string{"old"}, in.UserGroupIdsToRemove)
}

func TestReplicationGroupSlowLogUpgradeRunsFirst(t *testing.T) {
	priorInputs := testReplicationGroupResource()
	priorInputs.EngineVersion = new("5.0.6")
	current := *priorInputs
	current.EngineVersion = new("6.2")
	current.LogDeliveryConfiguration = &[]ReplicationGroupLogDeliveryConfiguration{
		{
			Destination: "logs", DestinationType: "cloudwatch-logs",
			LogFormat: "json", LogType: "slow-log",
		},
	}
	prior := testReplicationGroupPrior(*priorInputs)
	prior.Observed.EngineVersion = "5.0.6"
	client := newFakeReplicationGroupClient()
	client.modifyErrors = []error{
		&elasticachetypes.InvalidParameterCombinationException{
			Message: aws.String("No modifications were requested"),
		},
		nil,
	}

	_, err := current.update(context.Background(), client, prior,
		fastReplicationGroupOptions())

	require.NoError(t, err)
	require.Len(t, client.modifyInputs, 2)
	assert.Equal(t, "6.2", aws.ToString(client.modifyInputs[0].EngineVersion))
	assert.Empty(t, client.modifyInputs[0].LogDeliveryConfigurations)
	assert.Nil(t, client.modifyInputs[1].EngineVersion)
	require.Len(t, client.modifyInputs[1].LogDeliveryConfigurations, 1)
	assert.Equal(t, elasticachetypes.LogTypeSlowLog,
		client.modifyInputs[1].LogDeliveryConfigurations[0].LogType)
}

func TestReplicationGroupNoModificationsExceptionIsNarrow(t *testing.T) {
	client := newFakeReplicationGroupClient()
	client.modifyErrors = []error{
		&elasticachetypes.InvalidParameterCombinationException{
			Message: aws.String("No modifications were requested"),
		},
	}
	err := modifyReplicationGroupAcceptedNoop(context.Background(), client,
		&elasticache.ModifyReplicationGroupInput{})
	require.NoError(t, err)

	client = newFakeReplicationGroupClient()
	sentinel := errors.New("failed")
	client.modifyErrors = []error{sentinel}
	err = modifyReplicationGroupAcceptedNoop(context.Background(), client,
		&elasticache.ModifyReplicationGroupInput{})
	assert.ErrorIs(t, err, sentinel)
}

func TestReplicationGroupTagUpdateUsesPriorARN(t *testing.T) {
	priorInputs := testReplicationGroupResource()
	priorInputs.Tags = &map[string]string{
		"change": "old",
		"remove": "yes",
	}
	current := *priorInputs
	current.Tags = &map[string]string{
		"add":    "yes",
		"change": "new",
	}
	prior := testReplicationGroupPrior(*priorInputs)
	prior.Outputs.Arn = "prior-arn"
	client := newFakeReplicationGroupClient()
	client.listTagOutputs = []*elasticache.ListTagsForResourceOutput{
		{TagList: []elasticachetypes.Tag{
			{Key: aws.String("change"), Value: aws.String("old")},
			{Key: aws.String("remove"), Value: aws.String("yes")},
			{Key: aws.String("aws:managed"), Value: aws.String("yes")},
		}},
	}

	_, err := current.update(context.Background(), client, prior,
		fastReplicationGroupOptions())

	require.NoError(t, err)
	require.Len(t, client.listTagInputs, 1)
	assert.Equal(t, "prior-arn", aws.ToString(client.listTagInputs[0].ResourceName))
	require.Len(t, client.removeTagInputs, 1)
	assert.Equal(t, []string{"remove"}, client.removeTagInputs[0].TagKeys)
	require.Len(t, client.addTagInputs, 1)
	assert.Equal(t, map[string]string{"add": "yes", "change": "new"},
		tagMap(client.addTagInputs[0].Tags))
	assert.NotContains(t, client.removeTagInputs[0].TagKeys, "aws:managed")
}

func TestReplicationGroupUnchangedTagsMakeNoTagCalls(t *testing.T) {
	priorInputs := testReplicationGroupResource()
	priorInputs.Tags = &map[string]string{"keep": "yes"}
	current := *priorInputs
	prior := testReplicationGroupPrior(*priorInputs)
	client := newFakeReplicationGroupClient()

	_, err := current.update(context.Background(), client, prior,
		fastReplicationGroupOptions())

	require.NoError(t, err)
	assert.Empty(t, client.listTagInputs)
	assert.Empty(t, client.addTagInputs)
	assert.Empty(t, client.removeTagInputs)
}

func testReplicationGroupPrior(
	inputs ReplicationGroupResource,
) runtime.Prior[ReplicationGroupResource, *ReplicationGroupResourceOutput, *awsCfg] {
	outputs := &ReplicationGroupResourceOutput{
		Arn:                  testReplicationGroupArn,
		ReplicationGroupID:   testReplicationGroupID,
		EngineVersion:        "7.1.0",
		NumCacheClusters:     1,
		NumNodeGroups:        1,
		ReplicasPerNodeGroup: 0,
	}
	observed := *outputs
	return runtime.Prior[ReplicationGroupResource, *ReplicationGroupResourceOutput, *awsCfg]{
		Inputs:   inputs,
		Outputs:  outputs,
		Observed: &observed,
	}
}

func mutationCalls(calls []string) []string {
	mutations := make([]string, 0)
	for _, call := range calls {
		switch call {
		case "create", "modify", "shards", "increase", "decrease", "delete",
			"disassociate", "delete-parameter-group", "add-tags", "remove-tags":
			mutations = append(mutations, call)
		}
	}
	return mutations
}
