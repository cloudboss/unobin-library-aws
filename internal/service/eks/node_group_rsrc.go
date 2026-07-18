package eks

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *NodeGroupResource) SchemaVersion() int { return 1 }

func (r *NodeGroupResource) ReplaceFields() []string {
	return []string{
		"cluster-name",
		"node-group-name",
		"node-role-arn",
		"subnet-ids",
		"ami-type",
		"capacity-type",
		"disk-size",
		"instance-types",
		"remote-access",
		"launch-template-id",
		"launch-template-name",
	}
}

func (r NodeGroupResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.ForceUpdateVersion, false),
		defaults.NullableValue(r.NodeRepairConfig.Enabled, false),
	}
}

func (r NodeGroupResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.SubnetIDs)).
			Message("subnet-ids must not be empty"),
		constraint.Must(
			constraint.AtLeast(r.ScalingConfig.DesiredSize, 0),
			constraint.AtLeast(r.ScalingConfig.MinSize, 0),
			constraint.AtLeast(r.ScalingConfig.MaxSize, 1),
		),
		constraint.When(constraint.Present(r.InstanceTypes)).
			Require(constraint.MaxItems(r.InstanceTypes, 20)),
		constraint.AtMostOneOf(r.LaunchTemplateID, r.LaunchTemplateName),
		constraint.When(constraint.Present(r.LaunchTemplateVersion)).Require(
			constraint.Any(
				constraint.Present(r.LaunchTemplateID),
				constraint.Present(r.LaunchTemplateName),
			),
		),
		constraint.When(constraint.Present(r.Taints)).
			Require(constraint.MaxItems(r.Taints, 50)),
		constraint.ForEach(r.Taints, func(taint NodeGroupTaint) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(taint.Effect,
					"NO_SCHEDULE", "NO_EXECUTE", "PREFER_NO_SCHEDULE")),
			}
		}),
		constraint.AtMostOneOf(
			r.UpdateConfig.MaxUnavailable,
			r.UpdateConfig.MaxUnavailablePercentage,
		),
		constraint.When(constraint.Present(r.UpdateConfig)).Require(
			constraint.Any(
				constraint.Present(r.UpdateConfig.MaxUnavailable),
				constraint.Present(r.UpdateConfig.MaxUnavailablePercentage),
			),
		),
		constraint.When(constraint.Present(r.UpdateConfig.MaxUnavailable)).Require(
			constraint.AtLeast(r.UpdateConfig.MaxUnavailable, 1),
			constraint.AtMost(r.UpdateConfig.MaxUnavailable, 100),
		),
		constraint.When(
			constraint.Present(r.UpdateConfig.MaxUnavailablePercentage),
		).Require(
			constraint.AtLeast(r.UpdateConfig.MaxUnavailablePercentage, 1),
			constraint.AtMost(r.UpdateConfig.MaxUnavailablePercentage, 100),
		),
		constraint.When(constraint.Present(r.UpdateConfig.Strategy)).Require(
			constraint.OneOf(r.UpdateConfig.Strategy, "DEFAULT", "MINIMAL"),
		),
		constraint.AtMostOneOf(
			r.NodeRepairConfig.ParallelCount,
			r.NodeRepairConfig.ParallelPct,
		),
		constraint.AtMostOneOf(
			r.NodeRepairConfig.UnhealthyCount,
			r.NodeRepairConfig.UnhealthyPct,
		),
		constraint.ForEach(
			r.NodeRepairConfig.Overrides,
			func(override NodeGroupNodeRepairOverride) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.AtLeast(override.MinRepairWaitMins, 1)),
					constraint.Must(constraint.OneOf(
						override.RepairAction, "Replace", "Reboot", "NoAction",
					)),
				}
			},
		),
		constraint.When(
			constraint.Present(r.WarmPoolConfig.MaxGroupPreparedCapacity),
		).Require(constraint.AtLeast(r.WarmPoolConfig.MaxGroupPreparedCapacity, -1)),
		constraint.When(constraint.Present(r.WarmPoolConfig.MinSize)).Require(
			constraint.AtLeast(r.WarmPoolConfig.MinSize, 0),
		),
		constraint.When(constraint.Present(r.WarmPoolConfig.PoolState)).Require(
			constraint.OneOf(r.WarmPoolConfig.PoolState,
				"STOPPED", "RUNNING", "HIBERNATED"),
		),
	}
}

func (r *NodeGroupResource) EquivalentInput(
	field string,
	prior NodeGroupResource,
	current NodeGroupResource,
) bool {
	return equivalentNodeGroupInput(field, prior, current)
}

func (r *NodeGroupResource) ValidateInputs(ctx context.Context, cfg *awsCfg) error {
	return r.validateNodeGroupInputs(ctx, cfg)
}

func (r *NodeGroupResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*NodeGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createNodeGroup(ctx, client, systemClusterClock{})
}

func (r *NodeGroupResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *NodeGroupResourceOutput,
) (*NodeGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.readNodeGroup(ctx, client, prior)
}

func (r *NodeGroupResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput],
) (*NodeGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.updateNodeGroup(ctx, client, prior, systemClusterClock{})
}

func (r *NodeGroupResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *NodeGroupResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.deleteNodeGroup(ctx, client, prior, systemClusterClock{})
}
