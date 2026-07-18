package eks

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

func (r NodeGroupResource) createNodeGroup(
	ctx context.Context,
	client nodeGroupClient,
	clock clusterClock,
) (*NodeGroupResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	token, err := nodeGroupClientRequestToken()
	if err != nil {
		return nil, err
	}
	response, err := client.CreateNodegroup(ctx, r.nodeGroupCreateInput(token))
	if err != nil {
		return nil, fmt.Errorf("create node group %s/%s: %w",
			r.ClusterName, r.NodeGroupName, err)
	}
	if response == nil || response.Nodegroup == nil {
		return nil, fmt.Errorf("create node group %s/%s: response has no node group",
			r.ClusterName, r.NodeGroupName)
	}
	clusterName := aws.ToString(response.Nodegroup.ClusterName)
	nodeGroupName := aws.ToString(response.Nodegroup.NodegroupName)
	if clusterName == "" {
		return nil, fmt.Errorf("create node group %s/%s: response has no cluster name",
			r.ClusterName, r.NodeGroupName)
	}
	if nodeGroupName == "" {
		return nil, fmt.Errorf("create node group %s/%s: response has no node group name",
			r.ClusterName, r.NodeGroupName)
	}
	if err := waitNodeGroupCreated(
		ctx, client, clusterName, nodeGroupName, clock,
	); err != nil {
		return nil, err
	}
	return readNodeGroupByName(ctx, client, clusterName, nodeGroupName)
}

func (r NodeGroupResource) readNodeGroup(
	ctx context.Context,
	client nodeGroupClient,
	prior *NodeGroupResourceOutput,
) (*NodeGroupResourceOutput, error) {
	if prior == nil {
		return readNodeGroupByName(ctx, client, r.ClusterName, r.NodeGroupName)
	}
	clusterName, nodeGroupName, err := nodeGroupNames(prior)
	if err != nil {
		return nil, err
	}
	return readNodeGroupByName(ctx, client, clusterName, nodeGroupName)
}

func readNodeGroupByName(
	ctx context.Context,
	client nodeGroupClient,
	clusterName string,
	nodeGroupName string,
) (*NodeGroupResourceOutput, error) {
	group, err := describeNodeGroup(ctx, client, clusterName, nodeGroupName)
	if err != nil {
		return nil, err
	}
	return nodeGroupOutput(group), nil
}

func (r NodeGroupResource) deleteNodeGroup(
	ctx context.Context,
	client nodeGroupClient,
	prior *NodeGroupResourceOutput,
	clock clusterClock,
) error {
	clusterName, nodeGroupName, err := nodeGroupNames(prior)
	if err != nil {
		return err
	}
	_, err = client.DeleteNodegroup(ctx, &eks.DeleteNodegroupInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(nodeGroupName),
	})
	if err != nil {
		if isNodeGroupNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete node group %s/%s: %w", clusterName, nodeGroupName, err)
	}
	return waitNodeGroupDeleted(ctx, client, clusterName, nodeGroupName, clock)
}

func nodeGroupNames(output *NodeGroupResourceOutput) (string, string, error) {
	if output == nil {
		return "", "", errors.New("prior node group output is missing")
	}
	if output.ClusterName == "" {
		return "", "", errors.New("prior node group output has no cluster name")
	}
	if output.NodeGroupName == "" {
		return "", "", errors.New("prior node group output has no node group name")
	}
	return output.ClusterName, output.NodeGroupName, nil
}

func nodeGroupOutput(group *ekstypes.Nodegroup) *NodeGroupResourceOutput {
	output := &NodeGroupResourceOutput{
		AMITypeActual:          string(group.AmiType),
		ARN:                    aws.ToString(group.NodegroupArn),
		CapacityTypeActual:     string(group.CapacityType),
		ClusterName:            aws.ToString(group.ClusterName),
		DiskSizeActual:         nodeGroupInt64Pointer(group.DiskSize),
		InstanceTypesActual:    slices.Clone(group.InstanceTypes),
		NodeGroupName:          aws.ToString(group.NodegroupName),
		NodeRepairConfigActual: nodeGroupRepairOutput(group.NodeRepairConfig),
		ReleaseVersionActual:   aws.ToString(group.ReleaseVersion),
		Status:                 string(group.Status),
		UpdateConfigActual:     nodeGroupUpdateConfigOutput(group.UpdateConfig),
		VersionActual:          aws.ToString(group.Version),
		WarmPoolConfigActual:   nodeGroupWarmPoolOutput(group.WarmPoolConfig),
	}
	if group.LaunchTemplate != nil {
		output.LaunchTemplateIDActual = copyStringPointer(group.LaunchTemplate.Id)
		output.LaunchTemplateNameActual = copyStringPointer(group.LaunchTemplate.Name)
		output.LaunchTemplateVersionActual = copyStringPointer(group.LaunchTemplate.Version)
	}
	if group.Resources != nil {
		output.AutoScalingGroupNames = make(
			[]string, 0, len(group.Resources.AutoScalingGroups),
		)
		for _, autoScalingGroup := range group.Resources.AutoScalingGroups {
			output.AutoScalingGroupNames = append(
				output.AutoScalingGroupNames,
				aws.ToString(autoScalingGroup.Name),
			)
		}
		output.RemoteAccessSecurityGroupID = copyStringPointer(
			group.Resources.RemoteAccessSecurityGroup,
		)
	}
	return output
}

func nodeGroupRepairOutput(config *ekstypes.NodeRepairConfig) *NodeGroupNodeRepairConfig {
	if config == nil {
		return nil
	}
	return &NodeGroupNodeRepairConfig{
		Enabled:        copyBoolPointer(config.Enabled),
		ParallelCount:  nodeGroupInt64Pointer(config.MaxParallelNodesRepairedCount),
		ParallelPct:    nodeGroupInt64Pointer(config.MaxParallelNodesRepairedPercentage),
		UnhealthyCount: nodeGroupInt64Pointer(config.MaxUnhealthyNodeThresholdCount),
		UnhealthyPct:   nodeGroupInt64Pointer(config.MaxUnhealthyNodeThresholdPercentage),
		Overrides:      nodeGroupRepairOverrideOutput(config.NodeRepairConfigOverrides),
	}
}

func nodeGroupRepairOverrideOutput(
	overrides []ekstypes.NodeRepairConfigOverrides,
) *[]NodeGroupNodeRepairOverride {
	if overrides == nil {
		return nil
	}
	output := make([]NodeGroupNodeRepairOverride, 0, len(overrides))
	for _, override := range overrides {
		output = append(output, NodeGroupNodeRepairOverride{
			Condition:         aws.ToString(override.NodeMonitoringCondition),
			MinRepairWaitMins: int64(aws.ToInt32(override.MinRepairWaitTimeMins)),
			Reason:            aws.ToString(override.NodeUnhealthyReason),
			RepairAction:      string(override.RepairAction),
		})
	}
	converted := make([]NodeGroupNodeRepairOverride, len(output))
	copy(converted, output)
	return &converted
}

func nodeGroupUpdateConfigOutput(
	config *ekstypes.NodegroupUpdateConfig,
) *NodeGroupUpdateConfig {
	if config == nil {
		return nil
	}
	output := &NodeGroupUpdateConfig{
		MaxUnavailable: nodeGroupInt64Pointer(config.MaxUnavailable),
		MaxUnavailablePercentage: nodeGroupInt64Pointer(
			config.MaxUnavailablePercentage,
		),
	}
	if config.UpdateStrategy != "" {
		strategy := string(config.UpdateStrategy)
		output.Strategy = &strategy
	}
	return output
}

func nodeGroupWarmPoolOutput(config *ekstypes.WarmPoolConfig) *NodeGroupWarmPoolConfig {
	if config == nil {
		return nil
	}
	output := &NodeGroupWarmPoolConfig{
		Enabled:                  copyBoolPointer(config.Enabled),
		MaxGroupPreparedCapacity: nodeGroupInt64Pointer(config.MaxGroupPreparedCapacity),
		MinSize:                  nodeGroupInt64Pointer(config.MinSize),
		ReuseOnScaleIn:           copyBoolPointer(config.ReuseOnScaleIn),
	}
	if config.PoolState != "" {
		state := string(config.PoolState)
		output.PoolState = &state
	}
	return output
}

func nodeGroupInt64Pointer(value *int32) *int64 {
	if value == nil {
		return nil
	}
	converted := int64(*value)
	return &converted
}
