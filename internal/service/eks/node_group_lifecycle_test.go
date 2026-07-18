package eks

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeGroupCreateWaitsAndReadsFinalState(t *testing.T) {
	client := &fakeNodeGroupClient{
		createOutput: &eks.CreateNodegroupOutput{Nodegroup: &ekstypes.Nodegroup{
			ClusterName: aws.String("example"), NodegroupName: aws.String("workers"),
		}},
		describeResults: []nodeGroupDescribeResult{
			nodeGroupDescribe(sdkNodeGroup(ekstypes.NodegroupStatusCreating)),
			nodeGroupDescribe(sdkNodeGroup(ekstypes.NodegroupStatusActive)),
			nodeGroupDescribe(completeSDKNodeGroup()),
		},
	}

	output, err := validNodeGroupResource().createNodeGroup(
		t.Context(), client, newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"create", "describe", "describe", "describe"}, client.calls)
	require.Len(t, client.createInputs, 1)
	assert.NotEmpty(t, aws.ToString(client.createInputs[0].ClientRequestToken))
	assert.Equal(t, "prior-cluster", output.ClusterName)
	assert.Equal(t, "prior-workers", output.NodeGroupName)
	assert.Equal(t, "arn:node-group", output.ARN)
	assert.Equal(t, []string{"asg-a", "asg-b"}, output.AutoScalingGroupNames)
	assert.Equal(t, "sg-remote", aws.ToString(output.RemoteAccessSecurityGroupID))
	assert.Equal(t, "lt-123", aws.ToString(output.LaunchTemplateId))
	assert.True(t, aws.ToBool(output.NodeRepairConfig.Enabled))
	assert.True(t, aws.ToBool(output.WarmPoolConfig.Enabled))
}

func TestNodeGroupCreateRejectsMalformedResponses(t *testing.T) {
	tests := []struct {
		name   string
		output *eks.CreateNodegroupOutput
	}{
		{name: "nil output"},
		{name: "nil node group", output: &eks.CreateNodegroupOutput{}},
		{name: "missing cluster", output: &eks.CreateNodegroupOutput{
			Nodegroup: &ekstypes.Nodegroup{NodegroupName: aws.String("workers")},
		}},
		{name: "missing node group", output: &eks.CreateNodegroupOutput{
			Nodegroup: &ekstypes.Nodegroup{ClusterName: aws.String("cluster")},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeNodeGroupClient{createOutput: tt.output}
			_, err := validNodeGroupResource().createNodeGroup(
				t.Context(), client, newFakeClusterClock(),
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, "response")
		})
	}
}

func TestNodeGroupReadUsesPriorHandles(t *testing.T) {
	resource := validNodeGroupResource()
	resource.ClusterName = "replacement-cluster"
	resource.NodeGroupName = "replacement-workers"
	client := &fakeNodeGroupClient{describeResults: []nodeGroupDescribeResult{
		nodeGroupDescribe(completeSDKNodeGroup()),
	}}

	_, err := resource.readNodeGroup(t.Context(), client, &NodeGroupResourceOutput{
		ClusterName: "prior-cluster", NodeGroupName: "prior-workers",
	})

	require.NoError(t, err)
	require.Len(t, client.describeInputs, 1)
	assert.Equal(t, "prior-cluster", aws.ToString(client.describeInputs[0].ClusterName))
	assert.Equal(t, "prior-workers", aws.ToString(client.describeInputs[0].NodegroupName))
}

func TestNodeGroupReadWithoutPriorUsesCurrentNames(t *testing.T) {
	resource := validNodeGroupResource()
	client := &fakeNodeGroupClient{describeResults: []nodeGroupDescribeResult{
		nodeGroupDescribe(completeSDKNodeGroup()),
	}}

	_, err := resource.readNodeGroup(t.Context(), client, nil)

	require.NoError(t, err)
	require.Len(t, client.describeInputs, 1)
	assert.Equal(t, "example", aws.ToString(client.describeInputs[0].ClusterName))
	assert.Equal(t, "workers", aws.ToString(client.describeInputs[0].NodegroupName))
}

func TestNodeGroupReadRejectsIncompletePriorIdentity(t *testing.T) {
	resource := validNodeGroupResource()
	_, err := resource.readNodeGroup(
		t.Context(), &fakeNodeGroupClient{}, &NodeGroupResourceOutput{ClusterName: "cluster"},
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "prior node group output")
}

func TestNodeGroupUpdateOrdersTagsVersionAndConfig(t *testing.T) {
	priorInputs := validNodeGroupResource()
	priorInputs.Tags = &map[string]string{"keep": "old", "drop": "value"}
	priorInputs.Version = stringPointer("1.32")
	priorInputs.Labels = &map[string]string{"old": "value"}
	resource := cloneNodeGroupResource(t, priorInputs)
	resource.Tags = &map[string]string{"keep": "new", "add": "value", "aws:x": "skip"}
	resource.Version = stringPointer("1.33")
	resource.Labels = &map[string]string{"new": "value"}
	client := &fakeNodeGroupClient{
		listTags: map[string]string{
			"keep": "old", "drop": "value", "aws:managed": "yes",
		},
		versionOutputs: []*eks.UpdateNodegroupVersionOutput{{
			Update: &ekstypes.Update{Id: aws.String("version-id")},
		}},
		configOutputs: []*eks.UpdateNodegroupConfigOutput{{
			Update: &ekstypes.Update{Id: aws.String("config-id")},
		}},
		updateResults: []nodeGroupUpdateResult{
			{output: successfulNodeGroupUpdate("version-id")},
			{output: successfulNodeGroupUpdate("config-id")},
		},
		describeResults: []nodeGroupDescribeResult{
			nodeGroupDescribe(completeSDKNodeGroup()),
		},
	}
	prior := runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput]{
		Inputs: priorInputs,
		Outputs: &NodeGroupResourceOutput{
			ClusterName: "prior-cluster", NodeGroupName: "prior-workers",
			ARN: "output-arn",
		},
		Observed: &NodeGroupResourceOutput{ARN: "observed-arn"},
	}

	_, err := resource.updateNodeGroup(
		t.Context(), client, prior, newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"list-tags", "untag", "tag", "version", "wait:version-id",
		"config", "wait:config-id", "describe",
	}, client.calls)
	assert.Equal(t, "observed-arn",
		aws.ToString(client.listTagsInputs[0].ResourceArn))
	assert.Equal(t, []string{"drop"}, client.untagInputs[0].TagKeys)
	assert.Equal(t, map[string]string{"add": "value", "keep": "new"},
		client.tagInputs[0].Tags)
	assert.NotEmpty(t, aws.ToString(client.versionInputs[0].ClientRequestToken))
	assert.NotEmpty(t, aws.ToString(client.configInputs[0].ClientRequestToken))
	assert.NotEqual(t,
		aws.ToString(client.versionInputs[0].ClientRequestToken),
		aws.ToString(client.configInputs[0].ClientRequestToken),
	)
}

func TestNodeGroupUpdateForceOnlyDoesNotMutate(t *testing.T) {
	priorInputs := validNodeGroupResource()
	resource := cloneNodeGroupResource(t, priorInputs)
	resource.ForceUpdateVersion = true
	client := &fakeNodeGroupClient{describeResults: []nodeGroupDescribeResult{
		nodeGroupDescribe(completeSDKNodeGroup()),
	}}

	_, err := resource.updateNodeGroup(t.Context(), client,
		runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput]{
			Inputs: priorInputs,
			Outputs: &NodeGroupResourceOutput{
				ClusterName: "prior-cluster", NodeGroupName: "prior-workers",
			},
		},
		newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"describe"}, client.calls)
}

func TestNodeGroupUpdateIgnoresAutoscalerDesiredSizeDrift(t *testing.T) {
	priorInputs := validNodeGroupResource()
	resource := cloneNodeGroupResource(t, priorInputs)
	observed := completeSDKNodeGroup()
	observed.ScalingConfig = &ekstypes.NodegroupScalingConfig{
		DesiredSize: aws.Int32(1),
		MinSize:     aws.Int32(0),
		MaxSize:     aws.Int32(1),
	}
	client := &fakeNodeGroupClient{describeResults: []nodeGroupDescribeResult{
		nodeGroupDescribe(observed),
	}}

	_, err := resource.updateNodeGroup(t.Context(), client,
		runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput]{
			Inputs: priorInputs,
			Outputs: &NodeGroupResourceOutput{
				ClusterName: "prior-cluster", NodeGroupName: "prior-workers",
			},
			Observed: &NodeGroupResourceOutput{
				ClusterName: "prior-cluster", NodeGroupName: "prior-workers",
			},
		},
		newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"describe"}, client.calls)
	assert.Empty(t, client.configInputs)
}

func TestNodeGroupUpdateTagOnlySkipsEmptyTagCalls(t *testing.T) {
	priorInputs := validNodeGroupResource()
	resource := cloneNodeGroupResource(t, priorInputs)
	resource.Tags = &map[string]string{"keep": "same", "aws:ignored": "value"}
	client := &fakeNodeGroupClient{
		listTags: map[string]string{"keep": "same", "aws:managed": "value"},
		describeResults: []nodeGroupDescribeResult{
			nodeGroupDescribe(completeSDKNodeGroup()),
		},
	}

	_, err := resource.updateNodeGroup(t.Context(), client,
		runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput]{
			Inputs: priorInputs,
			Outputs: &NodeGroupResourceOutput{
				ClusterName: "cluster", NodeGroupName: "workers", ARN: "output-arn",
			},
		},
		newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"list-tags", "describe"}, client.calls)
	assert.Equal(t, "output-arn", aws.ToString(client.listTagsInputs[0].ResourceArn))
}

func TestNodeGroupUpdateRejectsMalformedUpdateResults(t *testing.T) {
	tests := []struct {
		name   string
		output *eks.UpdateNodegroupVersionOutput
	}{
		{name: "nil output"},
		{name: "nil update", output: &eks.UpdateNodegroupVersionOutput{}},
		{name: "missing ID", output: &eks.UpdateNodegroupVersionOutput{
			Update: &ekstypes.Update{},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priorInputs := validNodeGroupResource()
			priorInputs.Version = stringPointer("1.32")
			resource := cloneNodeGroupResource(t, priorInputs)
			resource.Version = stringPointer("1.33")
			client := &fakeNodeGroupClient{
				versionOutputs: []*eks.UpdateNodegroupVersionOutput{tt.output},
			}
			_, err := resource.updateNodeGroup(t.Context(), client,
				runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput]{
					Inputs: priorInputs,
					Outputs: &NodeGroupResourceOutput{
						ClusterName: "cluster", NodeGroupName: "workers",
					},
				},
				newFakeClusterClock(),
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, "update ID")
		})
	}
}

func TestNodeGroupUpdateRejectsMalformedConfigResults(t *testing.T) {
	tests := []struct {
		name   string
		output *eks.UpdateNodegroupConfigOutput
	}{
		{name: "nil output"},
		{name: "nil update", output: &eks.UpdateNodegroupConfigOutput{}},
		{name: "missing ID", output: &eks.UpdateNodegroupConfigOutput{
			Update: &ekstypes.Update{},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priorInputs := validNodeGroupResource()
			resource := cloneNodeGroupResource(t, priorInputs)
			resource.Labels = &map[string]string{"new": "value"}
			client := &fakeNodeGroupClient{
				configOutputs: []*eks.UpdateNodegroupConfigOutput{tt.output},
			}
			_, err := resource.updateNodeGroup(t.Context(), client,
				runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput]{
					Inputs: priorInputs,
					Outputs: &NodeGroupResourceOutput{
						ClusterName: "cluster", NodeGroupName: "workers",
					},
				},
				newFakeClusterClock(),
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, "update ID")
		})
	}
}

func TestNodeGroupOutputExposesEffectiveCloudValues(t *testing.T) {
	output := nodeGroupOutput(completeSDKNodeGroup())

	assert.Equal(t, "AL2023_x86_64_STANDARD", output.AmiType)
	assert.Equal(t, "ON_DEMAND", output.CapacityType)
	assert.Equal(t, int64(40), *output.DiskSize)
	assert.Equal(t, []string{"m7i.large"}, output.InstanceTypes)
	assert.Equal(t, "lt-123", aws.ToString(output.LaunchTemplateId))
	assert.Equal(t, "workers", aws.ToString(output.LaunchTemplateName))
	assert.Equal(t, "7", aws.ToString(output.LaunchTemplateVersion))
	assert.Equal(t, int64(2), *output.NodeRepairConfig.ParallelCount)
	assert.Equal(t, "1.33.1-20260101", output.ReleaseVersion)
	assert.Equal(t, int64(1), *output.UpdateConfig.MaxUnavailable)
	assert.Equal(t, "MINIMAL", aws.ToString(output.UpdateConfig.Strategy))
	assert.Equal(t, "1.33", output.Version)
	assert.Equal(t, int64(1), *output.WarmPoolConfig.MinSize)
	assert.Equal(t, "RUNNING", aws.ToString(output.WarmPoolConfig.PoolState))
}

func TestNodeGroupUpdateRequiresPriorIdentityAndTagARN(t *testing.T) {
	resource := validNodeGroupResource()
	_, err := resource.updateNodeGroup(t.Context(), &fakeNodeGroupClient{},
		runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput]{Inputs: resource},
		newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "prior node group output")

	priorInputs := validNodeGroupResource()
	resource = cloneNodeGroupResource(t, priorInputs)
	resource.Tags = &map[string]string{"new": "value"}
	_, err = resource.updateNodeGroup(t.Context(), &fakeNodeGroupClient{},
		runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput]{
			Inputs: priorInputs,
			Outputs: &NodeGroupResourceOutput{
				ClusterName: "cluster", NodeGroupName: "workers",
			},
		},
		newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "ARN")
}

func TestNodeGroupDeleteUsesPriorHandlesAndAcceptsNotFound(t *testing.T) {
	resource := validNodeGroupResource()
	resource.ClusterName = "replacement-cluster"
	resource.NodeGroupName = "replacement-workers"
	client := &fakeNodeGroupClient{deleteErr: nodeGroupNotFoundError()}

	err := resource.deleteNodeGroup(t.Context(), client, &NodeGroupResourceOutput{
		ClusterName: "prior-cluster", NodeGroupName: "prior-workers",
	}, newFakeClusterClock())

	require.NoError(t, err)
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, "prior-cluster", aws.ToString(client.deleteInputs[0].ClusterName))
	assert.Equal(t, "prior-workers", aws.ToString(client.deleteInputs[0].NodegroupName))
	assert.Equal(t, []string{"delete"}, client.calls)
}

func TestNodeGroupDeleteWaitsForAbsence(t *testing.T) {
	client := &fakeNodeGroupClient{
		deleteOutput: &eks.DeleteNodegroupOutput{},
		describeResults: []nodeGroupDescribeResult{
			nodeGroupDescribe(sdkNodeGroup(ekstypes.NodegroupStatusDeleting)),
			{err: nodeGroupNotFoundError()},
		},
	}
	err := validNodeGroupResource().deleteNodeGroup(
		t.Context(), client,
		&NodeGroupResourceOutput{
			ClusterName: "prior-cluster", NodeGroupName: "prior-workers",
		},
		newFakeClusterClock(),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"delete", "describe", "describe"}, client.calls)
}

func successfulNodeGroupUpdate(id string) *eks.DescribeUpdateOutput {
	return &eks.DescribeUpdateOutput{Update: &ekstypes.Update{
		Id: aws.String(id), Status: ekstypes.UpdateStatusSuccessful,
	}}
}

func completeSDKNodeGroup() *ekstypes.Nodegroup {
	return &ekstypes.Nodegroup{
		AmiType:       ekstypes.AMITypesAl2023X8664Standard,
		CapacityType:  ekstypes.CapacityTypesOnDemand,
		ClusterName:   aws.String("prior-cluster"),
		DiskSize:      aws.Int32(40),
		InstanceTypes: []string{"m7i.large"},
		LaunchTemplate: &ekstypes.LaunchTemplateSpecification{
			Id: aws.String("lt-123"), Name: aws.String("workers"),
			Version: aws.String("7"),
		},
		NodeRepairConfig: &ekstypes.NodeRepairConfig{
			Enabled: aws.Bool(true), MaxParallelNodesRepairedCount: aws.Int32(2),
		},
		NodegroupArn:   aws.String("arn:node-group"),
		NodegroupName:  aws.String("prior-workers"),
		ReleaseVersion: aws.String("1.33.1-20260101"),
		Resources: &ekstypes.NodegroupResources{
			AutoScalingGroups: []ekstypes.AutoScalingGroup{
				{Name: aws.String("asg-a")}, {Name: aws.String("asg-b")},
			},
			RemoteAccessSecurityGroup: aws.String("sg-remote"),
		},
		Status: ekstypes.NodegroupStatusActive,
		UpdateConfig: &ekstypes.NodegroupUpdateConfig{
			MaxUnavailable: aws.Int32(1),
			UpdateStrategy: ekstypes.NodegroupUpdateStrategiesMinimal,
		},
		Version: aws.String("1.33"),
		WarmPoolConfig: &ekstypes.WarmPoolConfig{
			Enabled: aws.Bool(true), MinSize: aws.Int32(1),
			PoolState: ekstypes.WarmPoolStateRunning,
		},
	}
}
