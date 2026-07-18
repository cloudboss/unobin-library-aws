package eks

import (
	"context"
	"errors"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *ClusterResource) SchemaVersion() int { return 1 }

func (r *ClusterResource) ReplaceFields() []string {
	return []string{"name", "role-arn", "bootstrap-self-managed-addons", "outpost-config"}
}

func (r ClusterResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.BootstrapSelfManagedAddons, true),
		defaults.Value(r.VPCConfig.EndpointPrivateAccess, false),
		defaults.Value(r.VPCConfig.EndpointPublicAccess, true),
	}
}

func (r ClusterResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForbiddenWith(r.EncryptionConfig, r.OutpostConfig).
			Message("encryption-config conflicts with outpost-config"),
		constraint.ForbiddenWith(r.KubernetesNetworkConfig, r.OutpostConfig).
			Message("kubernetes-network-config conflicts with outpost-config"),
		constraint.When(constraint.IsTrue(r.ComputeConfig.Enabled)).Require(
			constraint.IsTrue(r.KubernetesNetworkConfig.ElasticLoadBalancing.Enabled),
			constraint.IsTrue(r.StorageConfig.BlockStorage.Enabled),
			constraint.IsFalse(r.BootstrapSelfManagedAddons),
		).Message("Auto Mode requires all capabilities enabled and bootstrap add-ons disabled"),
		constraint.When(constraint.IsTrue(
			r.KubernetesNetworkConfig.ElasticLoadBalancing.Enabled,
		)).Require(
			constraint.IsTrue(r.ComputeConfig.Enabled),
			constraint.IsTrue(r.StorageConfig.BlockStorage.Enabled),
			constraint.IsFalse(r.BootstrapSelfManagedAddons),
		).Message("Auto Mode requires all capabilities enabled and bootstrap add-ons disabled"),
		constraint.When(constraint.IsTrue(r.StorageConfig.BlockStorage.Enabled)).Require(
			constraint.IsTrue(r.ComputeConfig.Enabled),
			constraint.IsTrue(r.KubernetesNetworkConfig.ElasticLoadBalancing.Enabled),
			constraint.IsFalse(r.BootstrapSelfManagedAddons),
		).Message("Auto Mode requires all capabilities enabled and bootstrap add-ons disabled"),
		constraint.When(constraint.Present(r.AccessConfig.AuthenticationMode)).
			Require(constraint.OneOf(r.AccessConfig.AuthenticationMode,
				"CONFIG_MAP", "API", "API_AND_CONFIG_MAP")),
		constraint.ForEach(r.ComputeConfig.NodePools,
			func(pool string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(pool, "general-purpose", "system")),
				}
			}),
		constraint.When(constraint.Present(r.ControlPlaneScalingConfig.Tier)).
			Require(constraint.OneOf(r.ControlPlaneScalingConfig.Tier,
				"standard", "tier-xl", "tier-2xl", "tier-4xl", "tier-8xl", "tier-ultra")),
		constraint.ForEach(r.EnabledClusterLogTypes,
			func(logType string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(logType,
						"api", "audit", "authenticator", "controllerManager", "scheduler")),
				}
			}),
		constraint.ForEach(r.EncryptionConfig.Resources,
			func(resource string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(resource, "secrets")),
				}
			}),
		constraint.When(constraint.Present(r.KubernetesNetworkConfig.IPFamily)).
			Require(constraint.OneOf(r.KubernetesNetworkConfig.IPFamily, "ipv4", "ipv6")),
		constraint.When(constraint.Present(r.UpgradePolicy.SupportType)).
			Require(constraint.OneOf(r.UpgradePolicy.SupportType, "STANDARD", "EXTENDED")),
		constraint.When(constraint.Present(r.VPCConfig.ControlPlaneEgressMode)).
			Require(constraint.OneOf(r.VPCConfig.ControlPlaneEgressMode,
				"AWS_MANAGED", "CUSTOMER_ROUTED")),
		constraint.When(constraint.Present(r.VPCConfig.PublicAccessCIDRs)).
			Require(constraint.NotEmpty(r.VPCConfig.PublicAccessCIDRs)),
		constraint.When(constraint.Present(r.RemoteNetworkConfig.RemoteNodeNetworks)).
			Require(constraint.NotEmpty(r.RemoteNetworkConfig.RemoteNodeNetworks)),
		constraint.When(constraint.Present(r.RemoteNetworkConfig.RemotePodNetworks)).
			Require(constraint.NotEmpty(r.RemoteNetworkConfig.RemotePodNetworks)),
	}
}

func (r *ClusterResource) ValidateInputs(ctx context.Context, cfg *awsCfg) error {
	return r.validateInputs(ctx, cfg)
}

func (r *ClusterResource) ModifyResourcePlan(
	req runtime.ResourcePlanRequest[ClusterResource, *ClusterResourceOutput, *awsCfg],
	resp *runtime.ResourcePlanResponse,
) error {
	if !req.HasPriorState || staticClusterReplacement(req.PriorInputs, req.CurrentInputs) {
		return nil
	}
	return conditionalClusterReplacement(req.PriorInputs, req.CurrentInputs)
}

func (r *ClusterResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*ClusterResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createWithClient(ctx, client, systemClusterClock{})
}

func (r *ClusterResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *ClusterResourceOutput,
) (*ClusterResourceOutput, error) {
	name, err := clusterReadName(r.Name, prior)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.readWithClient(ctx, client, name)
}

func (r *ClusterResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[ClusterResource, *ClusterResourceOutput],
) (*ClusterResourceOutput, error) {
	if err := conditionalClusterReplacement(prior.Inputs, *r); err != nil {
		return nil, err
	}
	if err := r.ValidateInputs(ctx, cfg); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.updateWithClient(ctx, client, prior, systemClusterClock{})
}

func (r *ClusterResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *ClusterResourceOutput,
) error {
	if prior == nil || prior.Name == "" {
		return errors.New("prior cluster output has no name")
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.deleteWithClient(ctx, client, prior, systemClusterClock{})
}

func clusterReadName(current string, prior *ClusterResourceOutput) (string, error) {
	if prior == nil {
		if current == "" {
			return "", errors.New("cluster input has no name")
		}
		return current, nil
	}
	if prior.Name == "" {
		return "", errors.New("prior cluster output has no name")
	}
	return prior.Name, nil
}
