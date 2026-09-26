package eks

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *AddonResource) ResourceDefinition() runtime.ResourceDefinition[AddonResource, *AddonResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[AddonResource, *AddonResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input AddonResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Equality: []runtime.InputEqualityRule[AddonResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *AddonResource) **[]AddonPodIdentityAssociation { return &input.PodIdentityAssociation }),
				func(prior, desired *[]AddonPodIdentityAssociation) bool {
					return r.EquivalentInput("pod-identity-association", AddonResource{PodIdentityAssociation: prior}, AddonResource{PodIdentityAssociation: desired})
				},
			),
		},
		Replace: runtime.Replacement[AddonResource, *AddonResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[AddonResource]{
				runtime.InputField(func(input *AddonResource) *string { return &input.ClusterName }),
				runtime.InputField(func(input *AddonResource) *string { return &input.AddonName }),
				runtime.InputField(func(input *AddonResource) **AddonNamespaceConfig { return &input.NamespaceConfig }),
			},
		},
	}
}

func (r *ClusterResource) ResourceDefinition() runtime.ResourceDefinition[ClusterResource, *ClusterResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ClusterResource, *ClusterResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input ClusterResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[ClusterResource, *ClusterResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ClusterResource]{
				runtime.InputField(func(input *ClusterResource) *string { return &input.Name }),
				runtime.InputField(func(input *ClusterResource) *string { return &input.RoleArn }),
				runtime.InputField(func(input *ClusterResource) *bool { return &input.BootstrapSelfManagedAddons }),
				runtime.InputField(func(input *ClusterResource) **ClusterOutpostConfig { return &input.OutpostConfig }),
			},
			Rules: []runtime.ReplacementRule[ClusterResource]{
				runtime.ReplaceWhen(
					runtime.InputField(func(input *ClusterResource) **ClusterAccessConfig {
						return &input.AccessConfig
					}),
					func(prior, desired *ClusterAccessConfig) bool {
						return runtime.Changed(accessBootstrap(prior), accessBootstrap(desired))
					},
				),
				runtime.ReplaceWhen(
					runtime.InputField(func(input *ClusterResource) **ClusterKubernetesNetworkConfig {
						return &input.KubernetesNetworkConfig
					}),
					func(prior, desired *ClusterKubernetesNetworkConfig) bool {
						return runtime.Changed(networkIPFamily(prior), networkIPFamily(desired)) ||
							runtime.Changed(networkServiceCIDR(prior), networkServiceCIDR(desired))
					},
				),
				runtime.ReplaceWhen(
					runtime.InputField(func(input *ClusterResource) **ClusterEncryptionConfig {
						return &input.EncryptionConfig
					}),
					func(prior, desired *ClusterEncryptionConfig) bool {
						return prior != nil && runtime.Changed(prior, desired)
					},
				),
				runtime.ReplaceWhen(
					runtime.InputField(func(input *ClusterResource) *ClusterVPCConfig {
						return &input.VPCConfig
					}),
					func(prior, desired ClusterVPCConfig) bool {
						return requiresEgressReplacement(
							ClusterResource{VPCConfig: prior}, ClusterResource{VPCConfig: desired})
					},
				),
				runtime.ReplaceWhen(
					runtime.InputField(func(input *ClusterResource) **ClusterComputeConfig {
						return &input.ComputeConfig
					}),
					func(prior, desired *ClusterComputeConfig) bool {
						return changesExistingComputeRole(prior, desired)
					},
				),
			},
		},
	}
}

func (r *FargateProfileResource) ResourceDefinition() runtime.ResourceDefinition[FargateProfileResource, *FargateProfileResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[FargateProfileResource, *FargateProfileResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input FargateProfileResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[FargateProfileResource, *FargateProfileResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[FargateProfileResource]{
				runtime.InputField(func(input *FargateProfileResource) *string { return &input.ClusterName }),
				runtime.InputField(func(input *FargateProfileResource) *string { return &input.FargateProfileName }),
				runtime.InputField(func(input *FargateProfileResource) *string { return &input.PodExecutionRoleARN }),
				runtime.InputField(func(input *FargateProfileResource) *[]FargateProfileSelector { return &input.Selector }),
				runtime.InputField(func(input *FargateProfileResource) **[]string { return &input.SubnetIDs }),
			},
		},
	}
}

func (r *NodeGroupResource) ResourceDefinition() runtime.ResourceDefinition[NodeGroupResource, *NodeGroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[NodeGroupResource, *NodeGroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input NodeGroupResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Equality: []runtime.InputEqualityRule[NodeGroupResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *NodeGroupResource) *[]string { return &input.SubnetIDs }),
				func(prior, desired []string) bool {
					return r.EquivalentInput("subnet-ids", NodeGroupResource{SubnetIDs: prior}, NodeGroupResource{SubnetIDs: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *NodeGroupResource) **NodeGroupRemoteAccess { return &input.RemoteAccess }),
				func(prior, desired *NodeGroupRemoteAccess) bool {
					return r.EquivalentInput("remote-access", NodeGroupResource{RemoteAccess: prior}, NodeGroupResource{RemoteAccess: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *NodeGroupResource) **[]NodeGroupTaint { return &input.Taints }),
				func(prior, desired *[]NodeGroupTaint) bool {
					return r.EquivalentInput("taints", NodeGroupResource{Taints: prior}, NodeGroupResource{Taints: desired})
				},
			),
		},
		Replace: runtime.Replacement[NodeGroupResource, *NodeGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[NodeGroupResource]{
				runtime.InputField(func(input *NodeGroupResource) *string { return &input.ClusterName }),
				runtime.InputField(func(input *NodeGroupResource) *string { return &input.NodeGroupName }),
				runtime.InputField(func(input *NodeGroupResource) *string { return &input.NodeRoleARN }),
				runtime.InputField(func(input *NodeGroupResource) *[]string { return &input.SubnetIDs }),
				runtime.InputField(func(input *NodeGroupResource) **string { return &input.AMIType }),
				runtime.InputField(func(input *NodeGroupResource) **string { return &input.CapacityType }),
				runtime.InputField(func(input *NodeGroupResource) **int64 { return &input.DiskSize }),
				runtime.InputField(func(input *NodeGroupResource) **[]string { return &input.InstanceTypes }),
				runtime.InputField(func(input *NodeGroupResource) **NodeGroupRemoteAccess { return &input.RemoteAccess }),
				runtime.InputField(func(input *NodeGroupResource) **string { return &input.LaunchTemplateID }),
				runtime.InputField(func(input *NodeGroupResource) **string { return &input.LaunchTemplateName }),
			},
		},
	}
}
