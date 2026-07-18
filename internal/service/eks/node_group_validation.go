package eks

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func (r *NodeGroupResource) validateNodeGroupInputs(context.Context, *awsCfg) error {
	if len(r.ClusterName) < 1 || len(r.ClusterName) > 100 ||
		!clusterNamePattern.MatchString(r.ClusterName) {
		return fmt.Errorf("cluster-name must match %s and contain 1 to 100 characters",
			clusterNamePattern.String())
	}
	if len(r.NodeGroupName) < 1 || len(r.NodeGroupName) > 63 {
		return errors.New("node-group-name must contain 1 to 63 characters")
	}
	if r.NodeRoleARN == "" {
		return errors.New("node-role-arn must not be empty")
	}
	if len(r.SubnetIDs) == 0 {
		return errors.New("subnet-ids must not be empty")
	}
	if r.ScalingConfig.DesiredSize < 0 {
		return errors.New("scaling-config.desired-size must be at least zero")
	}
	if r.ScalingConfig.MinSize < 0 {
		return errors.New("scaling-config.min-size must be at least zero")
	}
	if r.ScalingConfig.MaxSize < 1 {
		return errors.New("scaling-config.max-size must be at least one")
	}
	if r.InstanceTypes != nil && len(*r.InstanceTypes) > 20 {
		return errors.New("instance-types must contain at most 20 values")
	}
	if err := validateNodeGroupLaunchTemplate(r); err != nil {
		return err
	}
	if err := validateNodeGroupTaints(r.Taints); err != nil {
		return err
	}
	if err := validateNodeGroupUpdateConfig(r.UpdateConfig); err != nil {
		return err
	}
	if err := validateNodeRepairConfig(r.NodeRepairConfig); err != nil {
		return err
	}
	if err := validateNodeGroupWarmPool(r.AMIType, r.WarmPoolConfig); err != nil {
		return err
	}
	if r.RemoteAccess != nil && r.RemoteAccess.EC2SSHKey == "" {
		return errors.New("remote-access.ec2-ssh-key must not be empty")
	}
	return nil
}

func validateNodeGroupLaunchTemplate(r *NodeGroupResource) error {
	idSet := r.LaunchTemplateID != nil
	nameSet := r.LaunchTemplateName != nil
	configured := idSet || nameSet || r.LaunchTemplateVersion != nil
	if configured && idSet == nameSet {
		return errors.New("launch-template requires exactly one of id or name")
	}
	if idSet && *r.LaunchTemplateID == "" {
		return errors.New("launch-template-id must not be empty")
	}
	if nameSet && *r.LaunchTemplateName == "" {
		return errors.New("launch-template-name must not be empty")
	}
	if r.LaunchTemplateVersion != nil {
		length := len(*r.LaunchTemplateVersion)
		if length < 1 || length > 255 {
			return errors.New("launch-template-version must contain 1 to 255 characters")
		}
	}
	return nil
}

func validateNodeGroupTaints(taints *[]NodeGroupTaint) error {
	if taints == nil {
		return nil
	}
	if len(*taints) > 50 {
		return errors.New("taints must contain at most 50 entries")
	}
	for _, taint := range *taints {
		if len(taint.Key) < 1 || len(taint.Key) > 63 {
			return errors.New("taints.key must contain 1 to 63 characters")
		}
		if taint.Value != nil && len(*taint.Value) > 63 {
			return errors.New("taints.value must contain 0 to 63 characters")
		}
		if !slices.Contains([]string{
			"NO_SCHEDULE", "NO_EXECUTE", "PREFER_NO_SCHEDULE",
		}, taint.Effect) {
			return fmt.Errorf("taints.effect contains invalid value %q", taint.Effect)
		}
	}
	return nil
}

func validateNodeGroupUpdateConfig(config *NodeGroupUpdateConfig) error {
	if config == nil {
		return nil
	}
	if (config.MaxUnavailable == nil) == (config.MaxUnavailablePercentage == nil) {
		return errors.New("update-config requires exactly one maximum unavailable value")
	}
	if err := validateRange(
		"update-config.max-unavailable", config.MaxUnavailable, 1, 100,
	); err != nil {
		return err
	}
	if err := validateRange(
		"update-config.max-unavailable-percentage",
		config.MaxUnavailablePercentage,
		1,
		100,
	); err != nil {
		return err
	}
	return validateOptionalEnum("update-config.strategy", config.Strategy, "DEFAULT", "MINIMAL")
}

func validateNodeRepairConfig(config *NodeGroupNodeRepairConfig) error {
	if config == nil {
		return nil
	}
	if config.ParallelCount != nil && config.ParallelPct != nil {
		return errors.New("node-repair-config parallel repair limits conflict")
	}
	if config.UnhealthyCount != nil && config.UnhealthyPct != nil {
		return errors.New("node-repair-config unhealthy thresholds conflict")
	}
	for _, check := range []struct {
		field string
		value *int64
		max   int64
	}{
		{"node-repair-config.max-parallel-nodes-repaired-count",
			config.ParallelCount, 0},
		{"node-repair-config.max-parallel-nodes-repaired-percentage",
			config.ParallelPct, 100},
		{"node-repair-config.max-unhealthy-node-threshold-count",
			config.UnhealthyCount, 0},
		{"node-repair-config.max-unhealthy-node-threshold-percentage",
			config.UnhealthyPct, 100},
	} {
		if err := validateRange(check.field, check.value, 1, check.max); err != nil {
			return err
		}
	}
	if config.Overrides == nil {
		return nil
	}
	for _, override := range *config.Overrides {
		if override.Condition == "" || override.Reason == "" ||
			override.MinRepairWaitMins < 1 ||
			!slices.Contains([]string{"Replace", "Reboot", "NoAction"},
				override.RepairAction) {
			return errors.New(
				"node-repair-config.overrides require condition, reason, wait, and action",
			)
		}
	}
	return nil
}

func validateNodeGroupWarmPool(amiType *string, config *NodeGroupWarmPoolConfig) error {
	if config == nil {
		return nil
	}
	if config.MaxGroupPreparedCapacity != nil && *config.MaxGroupPreparedCapacity < -1 {
		return errors.New("warm-pool-config.max-group-prepared-capacity must be at least -1")
	}
	if config.MinSize != nil && *config.MinSize < 0 {
		return errors.New("warm-pool-config.min-size must be at least zero")
	}
	if err := validateOptionalEnum(
		"warm-pool-config.pool-state", config.PoolState,
		"STOPPED", "RUNNING", "HIBERNATED",
	); err != nil {
		return err
	}
	bottlerocket := amiType != nil && strings.HasPrefix(*amiType, "BOTTLEROCKET_")
	if !bottlerocket {
		return nil
	}
	if config.PoolState != nil && *config.PoolState == "HIBERNATED" {
		return errors.New("Bottlerocket AMIs do not support HIBERNATED warm pools")
	}
	if config.ReuseOnScaleIn != nil && *config.ReuseOnScaleIn {
		return errors.New("Bottlerocket AMIs do not support warm-pool reuse-on-scale-in")
	}
	return nil
}

func validateRange(field string, value *int64, minimum, maximum int64) error {
	if value == nil {
		return nil
	}
	if *value < minimum || (maximum > 0 && *value > maximum) {
		if maximum > 0 {
			return fmt.Errorf("%s must be between %d and %d", field, minimum, maximum)
		}
		return fmt.Errorf("%s must be at least %d", field, minimum)
	}
	return nil
}

func equivalentNodeGroupInput(
	field string,
	prior NodeGroupResource,
	current NodeGroupResource,
) bool {
	switch field {
	case "subnet-ids":
		return unorderedNodeGroupSliceEqual(prior.SubnetIDs, current.SubnetIDs)
	case "remote-access":
		return equivalentNodeGroupRemoteAccess(prior.RemoteAccess, current.RemoteAccess)
	case "taints":
		return optionalUnorderedNodeGroupSliceEqual(prior.Taints, current.Taints)
	default:
		return false
	}
}

func equivalentNodeGroupRemoteAccess(prior, current *NodeGroupRemoteAccess) bool {
	if prior == nil || current == nil {
		return prior == current
	}
	return prior.EC2SSHKey == current.EC2SSHKey && optionalUnorderedNodeGroupSliceEqual(
		prior.SourceSecurityGroupIDs,
		current.SourceSecurityGroupIDs,
	)
}

func optionalUnorderedNodeGroupSliceEqual[T any](prior, current *[]T) bool {
	if prior == nil || current == nil {
		return prior == nil && current == nil
	}
	return unorderedNodeGroupSliceEqual(*prior, *current)
}

func unorderedNodeGroupSliceEqual[T any](prior, current []T) bool {
	if len(prior) != len(current) {
		return false
	}
	matched := make([]bool, len(current))
	for _, oldValue := range prior {
		found := false
		for index, newValue := range current {
			if matched[index] || !reflect.DeepEqual(oldValue, newValue) {
				continue
			}
			matched[index] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}
