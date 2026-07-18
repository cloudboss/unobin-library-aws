package eks

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeGroupValidateInputs(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*NodeGroupResource)
		match  string
	}{
		{name: "cluster name", modify: func(r *NodeGroupResource) { r.ClusterName = "" },
			match: "cluster-name"},
		{name: "node group name", modify: func(r *NodeGroupResource) { r.NodeGroupName = "" },
			match: "node-group-name"},
		{name: "long node group name", modify: func(r *NodeGroupResource) {
			r.NodeGroupName = strings.Repeat("a", 64)
		}, match: "node-group-name"},
		{name: "node role", modify: func(r *NodeGroupResource) { r.NodeRoleARN = "" },
			match: "node-role-arn"},
		{name: "subnets", modify: func(r *NodeGroupResource) { r.SubnetIDs = nil },
			match: "subnet-ids"},
		{name: "desired size", modify: func(r *NodeGroupResource) {
			r.ScalingConfig.DesiredSize = -1
		}, match: "desired-size"},
		{name: "minimum size", modify: func(r *NodeGroupResource) {
			r.ScalingConfig.MinSize = -1
		}, match: "min-size"},
		{name: "maximum size", modify: func(r *NodeGroupResource) {
			r.ScalingConfig.MaxSize = 0
		}, match: "max-size"},
		{name: "instance type count", modify: func(r *NodeGroupResource) {
			values := make([]string, 21)
			r.InstanceTypes = &values
		}, match: "instance-types"},
		{name: "launch identity missing", modify: func(r *NodeGroupResource) {
			r.LaunchTemplateVersion = stringPointer("1")
		}, match: "launch-template"},
		{name: "launch identities conflict", modify: func(r *NodeGroupResource) {
			r.LaunchTemplateID = stringPointer("lt-1")
			r.LaunchTemplateName = stringPointer("template")
		}, match: "launch-template"},
		{name: "launch version empty", modify: func(r *NodeGroupResource) {
			r.LaunchTemplateID = stringPointer("lt-1")
			r.LaunchTemplateVersion = stringPointer("")
		}, match: "launch-template-version"},
		{name: "taint count", modify: func(r *NodeGroupResource) {
			values := make([]NodeGroupTaint, 51)
			r.Taints = &values
		}, match: "taints"},
		{name: "taint key", modify: func(r *NodeGroupResource) {
			r.Taints = &[]NodeGroupTaint{{Effect: "NO_SCHEDULE"}}
		}, match: "taints.key"},
		{name: "taint value", modify: func(r *NodeGroupResource) {
			value := strings.Repeat("v", 64)
			r.Taints = &[]NodeGroupTaint{{Key: "key", Value: &value, Effect: "NO_SCHEDULE"}}
		}, match: "taints.value"},
		{name: "taint effect", modify: func(r *NodeGroupResource) {
			r.Taints = &[]NodeGroupTaint{{Key: "key", Effect: "NEVER"}}
		}, match: "taints.effect"},
		{name: "update quantities missing", modify: func(r *NodeGroupResource) {
			r.UpdateConfig = &NodeGroupUpdateConfig{}
		}, match: "update-config"},
		{name: "update quantities conflict", modify: func(r *NodeGroupResource) {
			r.UpdateConfig = &NodeGroupUpdateConfig{
				MaxUnavailable: int64Pointer(1), MaxUnavailablePercentage: int64Pointer(10),
			}
		}, match: "update-config"},
		{name: "update percentage", modify: func(r *NodeGroupResource) {
			r.UpdateConfig = &NodeGroupUpdateConfig{MaxUnavailablePercentage: int64Pointer(101)}
		}, match: "max-unavailable-percentage"},
		{name: "update strategy", modify: func(r *NodeGroupResource) {
			r.UpdateConfig = &NodeGroupUpdateConfig{
				MaxUnavailable: int64Pointer(1), Strategy: stringPointer("FAST"),
			}
		}, match: "strategy"},
		{name: "repair count conflict", modify: func(r *NodeGroupResource) {
			r.NodeRepairConfig = &NodeGroupNodeRepairConfig{
				ParallelCount: int64Pointer(1),
				ParallelPct:   int64Pointer(10),
			}
		}, match: "node-repair-config"},
		{name: "repair threshold percentage", modify: func(r *NodeGroupResource) {
			r.NodeRepairConfig = &NodeGroupNodeRepairConfig{
				UnhealthyPct: int64Pointer(0),
			}
		}, match: "max-unhealthy-node-threshold-percentage"},
		{name: "repair override", modify: func(r *NodeGroupResource) {
			r.NodeRepairConfig = &NodeGroupNodeRepairConfig{
				Overrides: &[]NodeGroupNodeRepairOverride{{RepairAction: "Replace"}},
			}
		}, match: "overrides"},
		{name: "warm maximum", modify: func(r *NodeGroupResource) {
			r.WarmPoolConfig = &NodeGroupWarmPoolConfig{
				MaxGroupPreparedCapacity: int64Pointer(-2),
			}
		}, match: "max-group-prepared-capacity"},
		{name: "warm minimum", modify: func(r *NodeGroupResource) {
			r.WarmPoolConfig = &NodeGroupWarmPoolConfig{MinSize: int64Pointer(-1)}
		}, match: "warm-pool-config.min-size"},
		{name: "warm state", modify: func(r *NodeGroupResource) {
			r.WarmPoolConfig = &NodeGroupWarmPoolConfig{PoolState: stringPointer("PAUSED")}
		}, match: "pool-state"},
		{name: "Bottlerocket hibernation", modify: func(r *NodeGroupResource) {
			r.AMIType = stringPointer("BOTTLEROCKET_x86_64")
			r.WarmPoolConfig = &NodeGroupWarmPoolConfig{PoolState: stringPointer("HIBERNATED")}
		}, match: "Bottlerocket"},
		{name: "Bottlerocket reuse", modify: func(r *NodeGroupResource) {
			r.AMIType = stringPointer("BOTTLEROCKET_ARM_64")
			r.WarmPoolConfig = &NodeGroupWarmPoolConfig{ReuseOnScaleIn: boolPointer(true)}
		}, match: "Bottlerocket"},
		{name: "remote access key", modify: func(r *NodeGroupResource) {
			r.RemoteAccess = &NodeGroupRemoteAccess{}
		}, match: "ec2-ssh-key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validNodeGroupResource()
			tt.modify(&resource)
			err := resource.ValidateInputs(t.Context(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.match)
		})
	}
}

func TestNodeGroupValidateInputsAcceptsCompleteOptionalBlocks(t *testing.T) {
	resource := validNodeGroupResource()
	resource.LaunchTemplateID = stringPointer("lt-123")
	resource.LaunchTemplateVersion = stringPointer("7")
	resource.UpdateConfig = &NodeGroupUpdateConfig{
		MaxUnavailable: int64Pointer(1), Strategy: stringPointer("MINIMAL"),
	}
	resource.NodeRepairConfig = &NodeGroupNodeRepairConfig{
		ParallelCount:  int64Pointer(1),
		UnhealthyCount: int64Pointer(2),
		Overrides: &[]NodeGroupNodeRepairOverride{{
			Condition: "Ready", Reason: "Unknown", MinRepairWaitMins: 5,
			RepairAction: "Reboot",
		}},
	}
	resource.WarmPoolConfig = &NodeGroupWarmPoolConfig{
		Enabled: boolPointer(true), MaxGroupPreparedCapacity: int64Pointer(-1),
		MinSize: int64Pointer(0), PoolState: stringPointer("RUNNING"),
		ReuseOnScaleIn: boolPointer(true),
	}

	require.NoError(t, resource.ValidateInputs(t.Context(), nil))
}

func TestNodeGroupEquivalentInputUsesUnorderedCollections(t *testing.T) {
	resource := &NodeGroupResource{}
	prior := validNodeGroupResource()
	reordered := cloneNodeGroupResource(t, prior)
	reordered.SubnetIDs = []string{"subnet-b", "subnet-a"}
	prior.RemoteAccess = &NodeGroupRemoteAccess{
		EC2SSHKey: "key", SourceSecurityGroupIDs: &[]string{"sg-a", "sg-b"},
	}
	reordered.RemoteAccess = &NodeGroupRemoteAccess{
		EC2SSHKey: "key", SourceSecurityGroupIDs: &[]string{"sg-b", "sg-a"},
	}
	prior.Taints = &[]NodeGroupTaint{
		{Key: "one", Effect: "NO_SCHEDULE"},
		{Key: "two", Value: stringPointer("value"), Effect: "NO_EXECUTE"},
	}
	reordered.Taints = &[]NodeGroupTaint{(*prior.Taints)[1], (*prior.Taints)[0]}

	assert.True(t, resource.EquivalentInput("subnet-ids", prior, reordered))
	assert.True(t, resource.EquivalentInput("remote-access", prior, reordered))
	assert.True(t, resource.EquivalentInput("taints", prior, reordered))
	assert.False(t, resource.EquivalentInput("instance-types", prior, reordered))

	changed := cloneNodeGroupResource(t, reordered)
	changed.SubnetIDs = []string{"subnet-a", "subnet-c"}
	assert.False(t, resource.EquivalentInput("subnet-ids", prior, changed))
}
