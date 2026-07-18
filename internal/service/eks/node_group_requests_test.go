package eks

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeGroupCreateInput(t *testing.T) {
	resource := validNodeGroupResource()
	resource.AMIType = stringPointer("AL2023_x86_64_STANDARD")
	resource.CapacityType = stringPointer("SPOT")
	resource.DiskSize = int64Pointer(40)
	resource.InstanceTypes = &[]string{"m7i.large", "m7i.xlarge"}
	resource.Labels = &map[string]string{"role": "worker"}
	resource.LaunchTemplateID = stringPointer("lt-123")
	resource.LaunchTemplateVersion = stringPointer("7")
	resource.NodeRepairConfig = &NodeGroupNodeRepairConfig{
		Enabled:       boolPointer(true),
		ParallelCount: int64Pointer(2),
		Overrides: &[]NodeGroupNodeRepairOverride{{
			Condition: "Ready", Reason: "Unknown", MinRepairWaitMins: 5,
			RepairAction: "Reboot",
		}},
	}
	resource.ReleaseVersion = stringPointer("1.33.1-20260101")
	resource.RemoteAccess = &NodeGroupRemoteAccess{
		EC2SSHKey: "worker-key", SourceSecurityGroupIDs: &[]string{"sg-a", "sg-b"},
	}
	resource.Tags = &map[string]string{"env": "test", "aws:owned": "ignored"}
	resource.Taints = &[]NodeGroupTaint{{
		Key: "dedicated", Value: stringPointer("workers"), Effect: "NO_SCHEDULE",
	}}
	resource.UpdateConfig = &NodeGroupUpdateConfig{
		MaxUnavailablePercentage: int64Pointer(25), Strategy: stringPointer("MINIMAL"),
	}
	resource.Version = stringPointer("1.33")
	resource.WarmPoolConfig = &NodeGroupWarmPoolConfig{
		Enabled: boolPointer(true), MinSize: int64Pointer(1),
		PoolState: stringPointer("RUNNING"), ReuseOnScaleIn: boolPointer(true),
	}

	input := resource.nodeGroupCreateInput("request-token")

	assert.Equal(t, "example", aws.ToString(input.ClusterName))
	assert.Equal(t, "workers", aws.ToString(input.NodegroupName))
	assert.Equal(t, resource.NodeRoleARN, aws.ToString(input.NodeRole))
	assert.Equal(t, resource.SubnetIDs, input.Subnets)
	assert.Equal(t, "request-token", aws.ToString(input.ClientRequestToken))
	require.NotNil(t, input.ScalingConfig)
	assert.Equal(t, int32(0), aws.ToInt32(input.ScalingConfig.DesiredSize))
	assert.Equal(t, int32(0), aws.ToInt32(input.ScalingConfig.MinSize))
	assert.Equal(t, int32(1), aws.ToInt32(input.ScalingConfig.MaxSize))
	assert.Equal(t, ekstypes.AMITypes("AL2023_x86_64_STANDARD"), input.AmiType)
	assert.Equal(t, ekstypes.CapacityTypes("SPOT"), input.CapacityType)
	assert.Equal(t, int32(40), aws.ToInt32(input.DiskSize))
	assert.Equal(t, []string{"m7i.large", "m7i.xlarge"}, input.InstanceTypes)
	assert.Equal(t, map[string]string{"role": "worker"}, input.Labels)
	assert.Equal(t, map[string]string{"env": "test"}, input.Tags)
	require.NotNil(t, input.LaunchTemplate)
	assert.Equal(t, "lt-123", aws.ToString(input.LaunchTemplate.Id))
	assert.Equal(t, "7", aws.ToString(input.LaunchTemplate.Version))
	require.NotNil(t, input.RemoteAccess)
	assert.Equal(t, "worker-key", aws.ToString(input.RemoteAccess.Ec2SshKey))
	assert.Equal(t, []string{"sg-a", "sg-b"}, input.RemoteAccess.SourceSecurityGroups)
	require.Len(t, input.Taints, 1)
	assert.Equal(t, "dedicated", aws.ToString(input.Taints[0].Key))
	assert.Equal(t, "workers", aws.ToString(input.Taints[0].Value))
	assert.Equal(t, ekstypes.TaintEffectNoSchedule, input.Taints[0].Effect)
	require.NotNil(t, input.NodeRepairConfig)
	assert.True(t, aws.ToBool(input.NodeRepairConfig.Enabled))
	assert.Equal(t, int32(2),
		aws.ToInt32(input.NodeRepairConfig.MaxParallelNodesRepairedCount))
	require.Len(t, input.NodeRepairConfig.NodeRepairConfigOverrides, 1)
	require.NotNil(t, input.UpdateConfig)
	assert.Equal(t, int32(25), aws.ToInt32(input.UpdateConfig.MaxUnavailablePercentage))
	assert.Equal(t, ekstypes.NodegroupUpdateStrategiesMinimal,
		input.UpdateConfig.UpdateStrategy)
	assert.Equal(t, "1.33.1-20260101", aws.ToString(input.ReleaseVersion))
	assert.Equal(t, "1.33", aws.ToString(input.Version))
	require.NotNil(t, input.WarmPoolConfig)
	assert.True(t, aws.ToBool(input.WarmPoolConfig.Enabled))
	assert.Equal(t, int32(1), aws.ToInt32(input.WarmPoolConfig.MinSize))
	assert.Equal(t, ekstypes.WarmPoolStateRunning, input.WarmPoolConfig.PoolState)
}

func TestNodeGroupCreateInputOmitsOptionals(t *testing.T) {
	input := validNodeGroupResource().nodeGroupCreateInput("token")

	assert.Empty(t, input.AmiType)
	assert.Empty(t, input.CapacityType)
	assert.Nil(t, input.DiskSize)
	assert.Nil(t, input.InstanceTypes)
	assert.Nil(t, input.Labels)
	assert.Nil(t, input.LaunchTemplate)
	assert.Nil(t, input.NodeRepairConfig)
	assert.Nil(t, input.ReleaseVersion)
	assert.Nil(t, input.RemoteAccess)
	assert.Nil(t, input.Tags)
	assert.Nil(t, input.Taints)
	assert.Nil(t, input.UpdateConfig)
	assert.Nil(t, input.Version)
	assert.Nil(t, input.WarmPoolConfig)
}

func TestNodeGroupVersionInput(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*NodeGroupResource, *NodeGroupResource)
		needed bool
		check  func(*testing.T, *eks.UpdateNodegroupVersionInput)
	}{
		{
			name: "Kubernetes version",
			modify: func(prior, current *NodeGroupResource) {
				prior.Version = stringPointer("1.32")
				current.Version = stringPointer("1.33")
			},
			needed: true,
			check: func(t *testing.T, input *eks.UpdateNodegroupVersionInput) {
				assert.Equal(t, "1.33", aws.ToString(input.Version))
				assert.Nil(t, input.ReleaseVersion)
				assert.Nil(t, input.LaunchTemplate)
			},
		},
		{
			name: "release removal",
			modify: func(prior, current *NodeGroupResource) {
				prior.ReleaseVersion = stringPointer("old")
				current.ForceUpdateVersion = true
			},
			needed: true,
			check: func(t *testing.T, input *eks.UpdateNodegroupVersionInput) {
				assert.Nil(t, input.ReleaseVersion)
				assert.Nil(t, input.Version)
				assert.True(t, input.Force)
			},
		},
		{
			name: "Kubernetes version removal",
			modify: func(prior, _ *NodeGroupResource) {
				prior.Version = stringPointer("1.32")
			},
			needed: true,
			check: func(t *testing.T, input *eks.UpdateNodegroupVersionInput) {
				assert.Nil(t, input.Version)
				assert.Nil(t, input.ReleaseVersion)
				assert.Nil(t, input.LaunchTemplate)
			},
		},
		{
			name: "launch template version",
			modify: func(prior, current *NodeGroupResource) {
				prior.LaunchTemplateID = stringPointer("lt-123")
				current.LaunchTemplateID = stringPointer("lt-123")
				prior.LaunchTemplateVersion = stringPointer("1")
				current.LaunchTemplateVersion = stringPointer("2")
			},
			needed: true,
			check: func(t *testing.T, input *eks.UpdateNodegroupVersionInput) {
				require.NotNil(t, input.LaunchTemplate)
				assert.Equal(t, "lt-123", aws.ToString(input.LaunchTemplate.Id))
				assert.Equal(t, "2", aws.ToString(input.LaunchTemplate.Version))
			},
		},
		{
			name: "launch template version removal",
			modify: func(prior, current *NodeGroupResource) {
				prior.LaunchTemplateName = stringPointer("workers")
				current.LaunchTemplateName = stringPointer("workers")
				prior.LaunchTemplateVersion = stringPointer("4")
			},
			needed: true,
			check: func(t *testing.T, input *eks.UpdateNodegroupVersionInput) {
				require.NotNil(t, input.LaunchTemplate)
				assert.Equal(t, "workers", aws.ToString(input.LaunchTemplate.Name))
				assert.Nil(t, input.LaunchTemplate.Version)
			},
		},
		{
			name: "force alone",
			modify: func(_, current *NodeGroupResource) {
				current.ForceUpdateVersion = true
			},
			needed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prior := validNodeGroupResource()
			current := cloneNodeGroupResource(t, prior)
			tt.modify(&prior, &current)

			input, needed := current.nodeGroupVersionInput(
				prior, "prior-cluster", "prior-workers", "token",
			)

			assert.Equal(t, tt.needed, needed)
			if !tt.needed {
				assert.Nil(t, input)
				return
			}
			require.NotNil(t, input)
			assert.Equal(t, "prior-cluster", aws.ToString(input.ClusterName))
			assert.Equal(t, "prior-workers", aws.ToString(input.NodegroupName))
			assert.Equal(t, "token", aws.ToString(input.ClientRequestToken))
			tt.check(t, input)
		})
	}
}

func TestNodeGroupConfigInputDiffsCollections(t *testing.T) {
	prior := validNodeGroupResource()
	prior.Labels = &map[string]string{
		"keep": "same", "change": "old", "remove": "gone",
	}
	prior.Taints = &[]NodeGroupTaint{
		{Key: "value", Value: stringPointer("old"), Effect: "NO_SCHEDULE"},
		{Key: "removed", Effect: "NO_EXECUTE"},
		{Key: "effect", Value: stringPointer("same"), Effect: "NO_SCHEDULE"},
	}
	prior.WarmPoolConfig = &NodeGroupWarmPoolConfig{Enabled: boolPointer(true)}
	current := cloneNodeGroupResource(t, prior)
	current.Labels = &map[string]string{
		"keep": "same", "change": "new", "add": "value",
	}
	current.Taints = &[]NodeGroupTaint{
		{Key: "value", Value: stringPointer("new"), Effect: "NO_SCHEDULE"},
		{Key: "effect", Value: stringPointer("same"), Effect: "NO_EXECUTE"},
		{Key: "added", Effect: "PREFER_NO_SCHEDULE"},
	}
	current.NodeRepairConfig = &NodeGroupNodeRepairConfig{Enabled: boolPointer(false)}
	current.UpdateConfig = &NodeGroupUpdateConfig{MaxUnavailable: int64Pointer(1)}
	current.WarmPoolConfig = nil

	input, needed := current.nodeGroupConfigInput(
		prior, "prior-cluster", "prior-workers", "token",
	)

	require.True(t, needed)
	require.NotNil(t, input.Labels)
	assert.Equal(t, map[string]string{"change": "new", "add": "value"},
		input.Labels.AddOrUpdateLabels)
	assert.Equal(t, []string{"remove"}, input.Labels.RemoveLabels)
	require.NotNil(t, input.Taints)
	assert.ElementsMatch(t, []ekstypes.Taint{
		{Key: stringPointer("value"), Value: stringPointer("new"),
			Effect: ekstypes.TaintEffectNoSchedule},
		{Key: stringPointer("effect"), Value: stringPointer("same"),
			Effect: ekstypes.TaintEffectNoExecute},
		{Key: stringPointer("added"), Effect: ekstypes.TaintEffectPreferNoSchedule},
	}, input.Taints.AddOrUpdateTaints)
	assert.ElementsMatch(t, []ekstypes.Taint{
		{Key: stringPointer("removed"), Effect: ekstypes.TaintEffectNoExecute},
		{Key: stringPointer("effect"), Effect: ekstypes.TaintEffectNoSchedule},
	}, input.Taints.RemoveTaints)
	require.NotNil(t, input.NodeRepairConfig)
	assert.False(t, aws.ToBool(input.NodeRepairConfig.Enabled))
	require.NotNil(t, input.UpdateConfig)
	assert.Equal(t, int32(1), aws.ToInt32(input.UpdateConfig.MaxUnavailable))
	require.NotNil(t, input.WarmPoolConfig)
	assert.False(t, aws.ToBool(input.WarmPoolConfig.Enabled))
	assert.Nil(t, input.ScalingConfig)
}

func TestNodeGroupConfigInputRemovalAndDesiredScalingRules(t *testing.T) {
	prior := validNodeGroupResource()
	prior.NodeRepairConfig = &NodeGroupNodeRepairConfig{Enabled: boolPointer(true)}
	prior.UpdateConfig = &NodeGroupUpdateConfig{MaxUnavailable: int64Pointer(1)}
	current := cloneNodeGroupResource(t, prior)
	current.NodeRepairConfig = nil
	current.UpdateConfig = nil

	input, needed := current.nodeGroupConfigInput(prior, "cluster", "workers", "token")
	assert.False(t, needed)
	assert.Nil(t, input)

	current.ScalingConfig.DesiredSize = 1
	input, needed = current.nodeGroupConfigInput(prior, "cluster", "workers", "token")
	require.True(t, needed)
	require.NotNil(t, input.ScalingConfig)
	assert.Equal(t, int32(1), aws.ToInt32(input.ScalingConfig.DesiredSize))
	assert.Nil(t, input.NodeRepairConfig)
	assert.Nil(t, input.UpdateConfig)
}

func TestNodeGroupConfigInputOmitsReorderedTaintsDuringLabelUpdate(t *testing.T) {
	prior := validNodeGroupResource()
	prior.Taints = &[]NodeGroupTaint{
		{Key: "one", Effect: "NO_SCHEDULE"},
		{Key: "two", Effect: "NO_EXECUTE"},
	}
	prior.Labels = &map[string]string{"old": "value"}
	current := cloneNodeGroupResource(t, prior)
	current.Taints = &[]NodeGroupTaint{(*current.Taints)[1], (*current.Taints)[0]}
	current.Labels = &map[string]string{"new": "value"}

	input, needed := current.nodeGroupConfigInput(prior, "cluster", "workers", "token")

	require.True(t, needed)
	require.NotNil(t, input.Labels)
	assert.Nil(t, input.Taints)
}

func TestNodeGroupConfigInputRemovesAllLabelsAndTaints(t *testing.T) {
	prior := validNodeGroupResource()
	prior.Labels = &map[string]string{"one": "1", "two": "2"}
	prior.Taints = &[]NodeGroupTaint{
		{Key: "one", Value: stringPointer("1"), Effect: "NO_SCHEDULE"},
		{Key: "two", Effect: "NO_EXECUTE"},
	}
	current := cloneNodeGroupResource(t, prior)
	current.Labels = &map[string]string{}
	current.Taints = &[]NodeGroupTaint{}

	input, needed := current.nodeGroupConfigInput(prior, "cluster", "workers", "token")

	require.True(t, needed)
	assert.Empty(t, input.Labels.AddOrUpdateLabels)
	assert.ElementsMatch(t, []string{"one", "two"}, input.Labels.RemoveLabels)
	assert.Empty(t, input.Taints.AddOrUpdateTaints)
	assert.ElementsMatch(t, []ekstypes.Taint{
		{Key: stringPointer("one"), Effect: ekstypes.TaintEffectNoSchedule},
		{Key: stringPointer("two"), Effect: ekstypes.TaintEffectNoExecute},
	}, input.Taints.RemoveTaints)
}

func TestNodeGroupWarmPoolInputPreservesOmittedMembers(t *testing.T) {
	input := nodeGroupWarmPoolInput(&NodeGroupWarmPoolConfig{MinSize: int64Pointer(2)})

	require.NotNil(t, input)
	assert.Nil(t, input.Enabled)
	assert.Equal(t, int32(2), aws.ToInt32(input.MinSize))
	assert.Nil(t, input.MaxGroupPreparedCapacity)
	assert.Empty(t, input.PoolState)
	assert.Nil(t, input.ReuseOnScaleIn)
}

func TestNodeGroupWarmPoolInputSendsExplicitFalse(t *testing.T) {
	input := nodeGroupWarmPoolInput(&NodeGroupWarmPoolConfig{Enabled: boolPointer(false)})

	require.NotNil(t, input)
	require.NotNil(t, input.Enabled)
	assert.False(t, aws.ToBool(input.Enabled))
}
