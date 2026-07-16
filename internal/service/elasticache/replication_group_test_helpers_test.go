package elasticache

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	testReplicationGroupID  = "unobin-test-rg"
	testReplicationGroupArn = "arn:aws:elasticache:us-east-1:123456789012:" +
		"replicationgroup:unobin-test-rg"
	testCacheClusterID = "unobin-test-rg-001"
)

type fakeReplicationGroupClient struct {
	calls []string

	createInputs  []*elasticache.CreateReplicationGroupInput
	createOutputs []*elasticache.CreateReplicationGroupOutput
	createErrors  []error

	describeInputs  []*elasticache.DescribeReplicationGroupsInput
	describeOutputs []*elasticache.DescribeReplicationGroupsOutput
	describeErrors  []error

	describeClusterInputs  []*elasticache.DescribeCacheClustersInput
	describeClusterOutputs []*elasticache.DescribeCacheClustersOutput
	describeClusterErrors  []error

	modifyInputs []*elasticache.ModifyReplicationGroupInput
	modifyErrors []error

	shardInputs []*elasticache.ModifyReplicationGroupShardConfigurationInput
	shardErrors []error

	increaseInputs []*elasticache.IncreaseReplicaCountInput
	increaseErrors []error
	decreaseInputs []*elasticache.DecreaseReplicaCountInput
	decreaseErrors []error

	deleteInputs []*elasticache.DeleteReplicationGroupInput
	deleteErrors []error
	deleted      bool

	describeGlobalInputs  []*elasticache.DescribeGlobalReplicationGroupsInput
	describeGlobalOutputs []*elasticache.DescribeGlobalReplicationGroupsOutput
	describeGlobalErrors  []error

	disassociateInputs []*elasticache.DisassociateGlobalReplicationGroupInput
	disassociateErrors []error

	deleteParameterGroupInputs []*elasticache.DeleteCacheParameterGroupInput
	deleteParameterGroupErrors []error

	listTagInputs   []*elasticache.ListTagsForResourceInput
	listTagOutputs  []*elasticache.ListTagsForResourceOutput
	listTagErrors   []error
	addTagInputs    []*elasticache.AddTagsToResourceInput
	addTagErrors    []error
	removeTagInputs []*elasticache.RemoveTagsFromResourceInput
	removeTagErrors []error
}

func newFakeReplicationGroupClient() *fakeReplicationGroupClient {
	return &fakeReplicationGroupClient{}
}

func (c *fakeReplicationGroupClient) CreateReplicationGroup(
	_ context.Context,
	in *elasticache.CreateReplicationGroupInput,
	_ ...func(*elasticache.Options),
) (*elasticache.CreateReplicationGroupOutput, error) {
	c.calls = append(c.calls, "create")
	c.createInputs = append(c.createInputs, in)
	if err := popValue(&c.createErrors); err != nil {
		return nil, err
	}
	if out := popValue(&c.createOutputs); out != nil {
		return out, nil
	}
	return &elasticache.CreateReplicationGroupOutput{
		ReplicationGroup: testReplicationGroup(),
	}, nil
}

func (c *fakeReplicationGroupClient) DescribeReplicationGroups(
	_ context.Context,
	in *elasticache.DescribeReplicationGroupsInput,
	_ ...func(*elasticache.Options),
) (*elasticache.DescribeReplicationGroupsOutput, error) {
	c.calls = append(c.calls, "describe")
	c.describeInputs = append(c.describeInputs, in)
	if err := popValue(&c.describeErrors); err != nil {
		return nil, err
	}
	if out := popValue(&c.describeOutputs); out != nil {
		return out, nil
	}
	if c.deleted {
		return nil, &elasticachetypes.ReplicationGroupNotFoundFault{}
	}
	return &elasticache.DescribeReplicationGroupsOutput{
		ReplicationGroups: []elasticachetypes.ReplicationGroup{*testReplicationGroup()},
	}, nil
}

func (c *fakeReplicationGroupClient) DescribeCacheClusters(
	_ context.Context,
	in *elasticache.DescribeCacheClustersInput,
	_ ...func(*elasticache.Options),
) (*elasticache.DescribeCacheClustersOutput, error) {
	c.calls = append(c.calls, "describe-cluster")
	c.describeClusterInputs = append(c.describeClusterInputs, in)
	if err := popValue(&c.describeClusterErrors); err != nil {
		return nil, err
	}
	if out := popValue(&c.describeClusterOutputs); out != nil {
		return out, nil
	}
	return &elasticache.DescribeCacheClustersOutput{
		CacheClusters: []elasticachetypes.CacheCluster{*testReplicationGroupCacheCluster()},
	}, nil
}

func (c *fakeReplicationGroupClient) ModifyReplicationGroup(
	_ context.Context,
	in *elasticache.ModifyReplicationGroupInput,
	_ ...func(*elasticache.Options),
) (*elasticache.ModifyReplicationGroupOutput, error) {
	c.calls = append(c.calls, "modify")
	c.modifyInputs = append(c.modifyInputs, in)
	if err := popValue(&c.modifyErrors); err != nil {
		return nil, err
	}
	return &elasticache.ModifyReplicationGroupOutput{
		ReplicationGroup: testReplicationGroup(),
	}, nil
}

func (c *fakeReplicationGroupClient) ModifyReplicationGroupShardConfiguration(
	_ context.Context,
	in *elasticache.ModifyReplicationGroupShardConfigurationInput,
	_ ...func(*elasticache.Options),
) (*elasticache.ModifyReplicationGroupShardConfigurationOutput, error) {
	c.calls = append(c.calls, "shards")
	c.shardInputs = append(c.shardInputs, in)
	if err := popValue(&c.shardErrors); err != nil {
		return nil, err
	}
	return &elasticache.ModifyReplicationGroupShardConfigurationOutput{
		ReplicationGroup: testReplicationGroup(),
	}, nil
}

func (c *fakeReplicationGroupClient) IncreaseReplicaCount(
	_ context.Context,
	in *elasticache.IncreaseReplicaCountInput,
	_ ...func(*elasticache.Options),
) (*elasticache.IncreaseReplicaCountOutput, error) {
	c.calls = append(c.calls, "increase")
	c.increaseInputs = append(c.increaseInputs, in)
	if err := popValue(&c.increaseErrors); err != nil {
		return nil, err
	}
	return &elasticache.IncreaseReplicaCountOutput{
		ReplicationGroup: testReplicationGroup(),
	}, nil
}

func (c *fakeReplicationGroupClient) DecreaseReplicaCount(
	_ context.Context,
	in *elasticache.DecreaseReplicaCountInput,
	_ ...func(*elasticache.Options),
) (*elasticache.DecreaseReplicaCountOutput, error) {
	c.calls = append(c.calls, "decrease")
	c.decreaseInputs = append(c.decreaseInputs, in)
	if err := popValue(&c.decreaseErrors); err != nil {
		return nil, err
	}
	return &elasticache.DecreaseReplicaCountOutput{
		ReplicationGroup: testReplicationGroup(),
	}, nil
}

func (c *fakeReplicationGroupClient) DeleteReplicationGroup(
	_ context.Context,
	in *elasticache.DeleteReplicationGroupInput,
	_ ...func(*elasticache.Options),
) (*elasticache.DeleteReplicationGroupOutput, error) {
	c.calls = append(c.calls, "delete")
	c.deleteInputs = append(c.deleteInputs, in)
	if err := popValue(&c.deleteErrors); err != nil {
		return nil, err
	}
	c.deleted = true
	return &elasticache.DeleteReplicationGroupOutput{
		ReplicationGroup: testReplicationGroup(),
	}, nil
}

func (c *fakeReplicationGroupClient) DescribeGlobalReplicationGroups(
	_ context.Context,
	in *elasticache.DescribeGlobalReplicationGroupsInput,
	_ ...func(*elasticache.Options),
) (*elasticache.DescribeGlobalReplicationGroupsOutput, error) {
	c.calls = append(c.calls, "describe-global")
	c.describeGlobalInputs = append(c.describeGlobalInputs, in)
	if err := popValue(&c.describeGlobalErrors); err != nil {
		return nil, err
	}
	if out := popValue(&c.describeGlobalOutputs); out != nil {
		return out, nil
	}
	return &elasticache.DescribeGlobalReplicationGroupsOutput{
		GlobalReplicationGroups: []elasticachetypes.GlobalReplicationGroup{
			{
				GlobalReplicationGroupId: aws.String("global-test"),
				Status:                   aws.String("available"),
			},
		},
	}, nil
}

func (c *fakeReplicationGroupClient) DisassociateGlobalReplicationGroup(
	_ context.Context,
	in *elasticache.DisassociateGlobalReplicationGroupInput,
	_ ...func(*elasticache.Options),
) (*elasticache.DisassociateGlobalReplicationGroupOutput, error) {
	c.calls = append(c.calls, "disassociate")
	c.disassociateInputs = append(c.disassociateInputs, in)
	if err := popValue(&c.disassociateErrors); err != nil {
		return nil, err
	}
	return &elasticache.DisassociateGlobalReplicationGroupOutput{}, nil
}

func (c *fakeReplicationGroupClient) DeleteCacheParameterGroup(
	_ context.Context,
	in *elasticache.DeleteCacheParameterGroupInput,
	_ ...func(*elasticache.Options),
) (*elasticache.DeleteCacheParameterGroupOutput, error) {
	c.calls = append(c.calls, "delete-parameter-group")
	c.deleteParameterGroupInputs = append(c.deleteParameterGroupInputs, in)
	if err := popValue(&c.deleteParameterGroupErrors); err != nil {
		return nil, err
	}
	return &elasticache.DeleteCacheParameterGroupOutput{}, nil
}

func (c *fakeReplicationGroupClient) ListTagsForResource(
	_ context.Context,
	in *elasticache.ListTagsForResourceInput,
	_ ...func(*elasticache.Options),
) (*elasticache.ListTagsForResourceOutput, error) {
	c.calls = append(c.calls, "list-tags")
	c.listTagInputs = append(c.listTagInputs, in)
	if err := popValue(&c.listTagErrors); err != nil {
		return nil, err
	}
	if out := popValue(&c.listTagOutputs); out != nil {
		return out, nil
	}
	return &elasticache.ListTagsForResourceOutput{}, nil
}

func (c *fakeReplicationGroupClient) AddTagsToResource(
	_ context.Context,
	in *elasticache.AddTagsToResourceInput,
	_ ...func(*elasticache.Options),
) (*elasticache.AddTagsToResourceOutput, error) {
	c.calls = append(c.calls, "add-tags")
	c.addTagInputs = append(c.addTagInputs, in)
	if err := popValue(&c.addTagErrors); err != nil {
		return nil, err
	}
	return &elasticache.AddTagsToResourceOutput{}, nil
}

func (c *fakeReplicationGroupClient) RemoveTagsFromResource(
	_ context.Context,
	in *elasticache.RemoveTagsFromResourceInput,
	_ ...func(*elasticache.Options),
) (*elasticache.RemoveTagsFromResourceOutput, error) {
	c.calls = append(c.calls, "remove-tags")
	c.removeTagInputs = append(c.removeTagInputs, in)
	if err := popValue(&c.removeTagErrors); err != nil {
		return nil, err
	}
	return &elasticache.RemoveTagsFromResourceOutput{}, nil
}

func testReplicationGroup() *elasticachetypes.ReplicationGroup {
	return &elasticachetypes.ReplicationGroup{
		ARN:                aws.String(testReplicationGroupArn),
		ReplicationGroupId: aws.String(testReplicationGroupID),
		Status:             aws.String("available"),
		ClusterEnabled:     aws.Bool(false),
		AutomaticFailover:  elasticachetypes.AutomaticFailoverStatusDisabled,
		MultiAZ:            elasticachetypes.MultiAZStatusDisabled,
		MemberClusters:     []string{testCacheClusterID},
		NodeGroups: []elasticachetypes.NodeGroup{
			{
				NodeGroupId: aws.String("0001"),
				PrimaryEndpoint: &elasticachetypes.Endpoint{
					Address: aws.String("primary.example.test"),
					Port:    aws.Int32(6379),
				},
				ReaderEndpoint: &elasticachetypes.Endpoint{
					Address: aws.String("reader.example.test"),
					Port:    aws.Int32(6379),
				},
				NodeGroupMembers: []elasticachetypes.NodeGroupMember{
					{
						CacheClusterId: aws.String(testCacheClusterID),
						CurrentRole:    aws.String("primary"),
					},
				},
			},
		},
	}
}

func testReplicationGroupCacheCluster() *elasticachetypes.CacheCluster {
	return &elasticachetypes.CacheCluster{
		CacheClusterId:     aws.String(testCacheClusterID),
		CacheClusterStatus: aws.String("available"),
		EngineVersion:      aws.String("7.1.0"),
		CacheParameterGroup: &elasticachetypes.CacheParameterGroupStatus{
			CacheParameterGroupName: aws.String("default.redis7"),
		},
	}
}

func testReplicationGroupResource() *ReplicationGroupResource {
	nodeType := "cache.t3.micro"
	return &ReplicationGroupResource{
		ReplicationGroupID:      testReplicationGroupID,
		Description:             "test replication group",
		AuthTokenUpdateStrategy: "ROTATE",
		Engine:                  "redis",
		NodeType:                &nodeType,
	}
}

func fastReplicationGroupOptions() replicationGroupOperationOptions {
	return replicationGroupOperationOptions{
		waitOptions: []wait.Option{
			wait.WithTimeout(time.Second),
			wait.WithInterval(0),
		},
		retryOptions: []retry.Option{
			retry.WithTimeout(time.Second),
			retry.WithInterval(0),
		},
	}
}

func popValue[T any](values *[]T) T {
	if len(*values) == 0 {
		var zero T
		return zero
	}
	value := (*values)[0]
	*values = (*values)[1:]
	return value
}
