package eks

import (
	"maps"
	"reflect"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r NodeGroupResource) nodeGroupCreateInput(token string) *eks.CreateNodegroupInput {
	input := &eks.CreateNodegroupInput{
		ClientRequestToken: aws.String(token),
		ClusterName:        aws.String(r.ClusterName),
		Labels:             maps.Clone(valueMapOrNil(r.Labels)),
		NodegroupName:      aws.String(r.NodeGroupName),
		NodeRepairConfig:   nodeGroupRepairInput(r.NodeRepairConfig),
		NodeRole:           aws.String(r.NodeRoleARN),
		ScalingConfig:      nodeGroupScalingInput(r.ScalingConfig),
		Subnets:            slices.Clone(r.SubnetIDs),
		Tags:               clusterTags(valueMapOrNil(r.Tags)),
		Taints:             nodeGroupTaintInputs(valueOrNil(r.Taints)),
		UpdateConfig:       nodeGroupUpdateConfigInput(r.UpdateConfig),
		WarmPoolConfig:     nodeGroupWarmPoolInput(r.WarmPoolConfig),
	}
	if r.AMIType != nil {
		input.AmiType = ekstypes.AMITypes(*r.AMIType)
	}
	if r.CapacityType != nil {
		input.CapacityType = ekstypes.CapacityTypes(*r.CapacityType)
	}
	input.DiskSize = nodeGroupInt32Pointer(r.DiskSize)
	if r.InstanceTypes != nil {
		input.InstanceTypes = slices.Clone(*r.InstanceTypes)
	}
	input.LaunchTemplate = nodeGroupLaunchTemplateInput(r)
	input.ReleaseVersion = copyStringPointer(r.ReleaseVersion)
	input.RemoteAccess = nodeGroupRemoteAccessInput(r.RemoteAccess)
	input.Version = copyStringPointer(r.Version)
	return input
}

func (r NodeGroupResource) nodeGroupVersionInput(
	prior NodeGroupResource,
	clusterName string,
	nodeGroupName string,
	token string,
) (*eks.UpdateNodegroupVersionInput, bool) {
	launchChanged := runtime.Changed(
		prior.LaunchTemplateVersion,
		r.LaunchTemplateVersion,
	)
	releaseChanged := runtime.Changed(prior.ReleaseVersion, r.ReleaseVersion)
	versionChanged := runtime.Changed(prior.Version, r.Version)
	if !launchChanged && !releaseChanged && !versionChanged {
		return nil, false
	}
	input := &eks.UpdateNodegroupVersionInput{
		ClientRequestToken: aws.String(token),
		ClusterName:        aws.String(clusterName),
		Force:              r.ForceUpdateVersion,
		NodegroupName:      aws.String(nodeGroupName),
	}
	if launchChanged {
		input.LaunchTemplate = nodeGroupLaunchTemplateInput(r)
	}
	if releaseChanged {
		input.ReleaseVersion = copyStringPointer(r.ReleaseVersion)
	}
	if versionChanged {
		input.Version = copyStringPointer(r.Version)
	}
	return input, true
}

func (r NodeGroupResource) nodeGroupConfigInput(
	prior NodeGroupResource,
	clusterName string,
	nodeGroupName string,
	token string,
) (*eks.UpdateNodegroupConfigInput, bool) {
	input := &eks.UpdateNodegroupConfigInput{
		ClientRequestToken: aws.String(token),
		ClusterName:        aws.String(clusterName),
		NodegroupName:      aws.String(nodeGroupName),
	}
	needed := false
	if runtime.Changed(prior.Labels, r.Labels) {
		input.Labels = nodeGroupLabelChanges(prior.Labels, r.Labels)
		needed = input.Labels != nil
	}
	if r.NodeRepairConfig != nil &&
		runtime.Changed(prior.NodeRepairConfig, r.NodeRepairConfig) {
		input.NodeRepairConfig = nodeGroupRepairInput(r.NodeRepairConfig)
		needed = true
	}
	if runtime.Changed(prior.ScalingConfig, r.ScalingConfig) {
		input.ScalingConfig = nodeGroupScalingInput(r.ScalingConfig)
		needed = true
	}
	if !optionalUnorderedNodeGroupSliceEqual(prior.Taints, r.Taints) {
		input.Taints = nodeGroupTaintChanges(prior.Taints, r.Taints)
		needed = needed || input.Taints != nil
	}
	if r.UpdateConfig != nil && runtime.Changed(prior.UpdateConfig, r.UpdateConfig) {
		input.UpdateConfig = nodeGroupUpdateConfigInput(r.UpdateConfig)
		needed = true
	}
	if runtime.Changed(prior.WarmPoolConfig, r.WarmPoolConfig) {
		if r.WarmPoolConfig == nil {
			input.WarmPoolConfig = &ekstypes.WarmPoolConfig{Enabled: aws.Bool(false)}
		} else {
			input.WarmPoolConfig = nodeGroupWarmPoolInput(r.WarmPoolConfig)
		}
		needed = true
	}
	if !needed {
		return nil, false
	}
	return input, true
}

func nodeGroupScalingInput(config NodeGroupScalingConfig) *ekstypes.NodegroupScalingConfig {
	return &ekstypes.NodegroupScalingConfig{
		DesiredSize: aws.Int32(int32(config.DesiredSize)),
		MaxSize:     aws.Int32(int32(config.MaxSize)),
		MinSize:     aws.Int32(int32(config.MinSize)),
	}
}

func nodeGroupLaunchTemplateInput(
	resource NodeGroupResource,
) *ekstypes.LaunchTemplateSpecification {
	if resource.LaunchTemplateID == nil && resource.LaunchTemplateName == nil {
		return nil
	}
	return &ekstypes.LaunchTemplateSpecification{
		Id:      copyStringPointer(resource.LaunchTemplateID),
		Name:    copyStringPointer(resource.LaunchTemplateName),
		Version: copyStringPointer(resource.LaunchTemplateVersion),
	}
}

func nodeGroupRemoteAccessInput(config *NodeGroupRemoteAccess) *ekstypes.RemoteAccessConfig {
	if config == nil {
		return nil
	}
	input := &ekstypes.RemoteAccessConfig{Ec2SshKey: aws.String(config.EC2SSHKey)}
	if config.SourceSecurityGroupIDs != nil {
		input.SourceSecurityGroups = slices.Clone(*config.SourceSecurityGroupIDs)
	}
	return input
}

func nodeGroupTaintInputs(taints []NodeGroupTaint) []ekstypes.Taint {
	if taints == nil {
		return nil
	}
	inputs := make([]ekstypes.Taint, 0, len(taints))
	for _, taint := range taints {
		inputs = append(inputs, nodeGroupTaintInput(taint, true))
	}
	return inputs
}

func nodeGroupTaintInput(taint NodeGroupTaint, includeValue bool) ekstypes.Taint {
	input := ekstypes.Taint{
		Effect: ekstypes.TaintEffect(taint.Effect),
		Key:    aws.String(taint.Key),
	}
	if includeValue {
		input.Value = copyStringPointer(taint.Value)
	}
	return input
}

func nodeGroupRepairInput(config *NodeGroupNodeRepairConfig) *ekstypes.NodeRepairConfig {
	if config == nil {
		return nil
	}
	input := &ekstypes.NodeRepairConfig{
		Enabled:                             copyBoolPointer(config.Enabled),
		MaxParallelNodesRepairedCount:       nodeGroupInt32Pointer(config.ParallelCount),
		MaxParallelNodesRepairedPercentage:  nodeGroupInt32Pointer(config.ParallelPct),
		MaxUnhealthyNodeThresholdCount:      nodeGroupInt32Pointer(config.UnhealthyCount),
		MaxUnhealthyNodeThresholdPercentage: nodeGroupInt32Pointer(config.UnhealthyPct),
		NodeRepairConfigOverrides:           nodeGroupRepairOverrides(config.Overrides),
	}
	return input
}

func nodeGroupRepairOverrides(
	overrides *[]NodeGroupNodeRepairOverride,
) []ekstypes.NodeRepairConfigOverrides {
	if overrides == nil {
		return nil
	}
	inputs := make([]ekstypes.NodeRepairConfigOverrides, 0, len(*overrides))
	for _, override := range *overrides {
		inputs = append(inputs, ekstypes.NodeRepairConfigOverrides{
			MinRepairWaitTimeMins:   aws.Int32(int32(override.MinRepairWaitMins)),
			NodeMonitoringCondition: aws.String(override.Condition),
			NodeUnhealthyReason:     aws.String(override.Reason),
			RepairAction:            ekstypes.RepairAction(override.RepairAction),
		})
	}
	return inputs
}

func nodeGroupUpdateConfigInput(
	config *NodeGroupUpdateConfig,
) *ekstypes.NodegroupUpdateConfig {
	if config == nil {
		return nil
	}
	input := &ekstypes.NodegroupUpdateConfig{
		MaxUnavailable:           nodeGroupInt32Pointer(config.MaxUnavailable),
		MaxUnavailablePercentage: nodeGroupInt32Pointer(config.MaxUnavailablePercentage),
	}
	if config.Strategy != nil {
		input.UpdateStrategy = ekstypes.NodegroupUpdateStrategies(*config.Strategy)
	}
	return input
}

func nodeGroupWarmPoolInput(config *NodeGroupWarmPoolConfig) *ekstypes.WarmPoolConfig {
	if config == nil {
		return nil
	}
	input := &ekstypes.WarmPoolConfig{
		Enabled:                  copyBoolPointer(config.Enabled),
		MaxGroupPreparedCapacity: nodeGroupInt32Pointer(config.MaxGroupPreparedCapacity),
		MinSize:                  nodeGroupInt32Pointer(config.MinSize),
		ReuseOnScaleIn:           copyBoolPointer(config.ReuseOnScaleIn),
	}
	if config.PoolState != nil {
		input.PoolState = ekstypes.WarmPoolState(*config.PoolState)
	}
	return input
}

func nodeGroupLabelChanges(
	prior *map[string]string,
	current *map[string]string,
) *ekstypes.UpdateLabelsPayload {
	oldLabels := valueMapOrNil(prior)
	newLabels := valueMapOrNil(current)
	upsert := make(map[string]string)
	for key, value := range newLabels {
		if oldValue, exists := oldLabels[key]; !exists || oldValue != value {
			upsert[key] = value
		}
	}
	var remove []string
	for key := range oldLabels {
		if _, exists := newLabels[key]; !exists {
			remove = append(remove, key)
		}
	}
	slices.Sort(remove)
	if len(upsert) == 0 && len(remove) == 0 {
		return nil
	}
	return &ekstypes.UpdateLabelsPayload{
		AddOrUpdateLabels: upsert,
		RemoveLabels:      remove,
	}
}

func nodeGroupTaintChanges(
	prior *[]NodeGroupTaint,
	current *[]NodeGroupTaint,
) *ekstypes.UpdateTaintsPayload {
	oldTaints := valueOrNil(prior)
	newTaints := valueOrNil(current)
	var add []ekstypes.Taint
	for _, taint := range newTaints {
		if !slices.ContainsFunc(oldTaints, func(old NodeGroupTaint) bool {
			return reflect.DeepEqual(old, taint)
		}) {
			add = append(add, nodeGroupTaintInput(taint, true))
		}
	}
	var remove []ekstypes.Taint
	for _, taint := range oldTaints {
		if !slices.ContainsFunc(newTaints, func(current NodeGroupTaint) bool {
			return current.Key == taint.Key && current.Effect == taint.Effect
		}) {
			remove = append(remove, nodeGroupTaintInput(taint, false))
		}
	}
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	return &ekstypes.UpdateTaintsPayload{
		AddOrUpdateTaints: add,
		RemoveTaints:      remove,
	}
}

func nodeGroupInt32Pointer(value *int64) *int32 {
	if value == nil {
		return nil
	}
	return aws.Int32(int32(*value))
}

func copyBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	return aws.Bool(*value)
}
