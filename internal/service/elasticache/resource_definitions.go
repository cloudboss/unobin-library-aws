package elasticache

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *ReplicationGroupResource) ResourceDefinition() runtime.ResourceDefinition[ReplicationGroupResource, *ReplicationGroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ReplicationGroupResource, *ReplicationGroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input ReplicationGroupResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Equality: []runtime.InputEqualityRule[ReplicationGroupResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *ReplicationGroupResource) *string { return &input.ReplicationGroupID }),
				func(prior, desired string) bool {
					return r.EquivalentInput("replication-group-id", ReplicationGroupResource{ReplicationGroupID: prior}, ReplicationGroupResource{ReplicationGroupID: desired})
				},
			),
		},
		Replace: runtime.Replacement[ReplicationGroupResource, *ReplicationGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ReplicationGroupResource]{
				runtime.InputField(func(input *ReplicationGroupResource) **bool { return &input.AtRestEncryptionEnabled }),
				runtime.InputField(func(input *ReplicationGroupResource) **bool { return &input.DataTieringEnabled }),
				runtime.InputField(func(input *ReplicationGroupResource) **string { return &input.Durability }),
				runtime.InputField(func(input *ReplicationGroupResource) **string { return &input.GlobalReplicationGroupID }),
				runtime.InputField(func(input *ReplicationGroupResource) **string { return &input.KMSKeyID }),
				runtime.InputField(func(input *ReplicationGroupResource) **string { return &input.NetworkType }),
				runtime.InputField(func(input *ReplicationGroupResource) **ReplicationGroupNodeGroups {
					return &input.NodeGroupConfiguration
				}),
				runtime.InputField(func(input *ReplicationGroupResource) **int64 { return &input.Port }),
				runtime.InputField(func(input *ReplicationGroupResource) **[]string { return &input.PreferredCacheClusterAZs }),
				runtime.InputField(func(input *ReplicationGroupResource) *string { return &input.ReplicationGroupID }),
				runtime.InputField(func(input *ReplicationGroupResource) **[]string { return &input.SecurityGroupNames }),
				runtime.InputField(func(input *ReplicationGroupResource) **[]string { return &input.SnapshotArns }),
				runtime.InputField(func(input *ReplicationGroupResource) **string { return &input.SnapshotName }),
				runtime.InputField(func(input *ReplicationGroupResource) **string { return &input.SubnetGroupName }),
			},
			Rules: []runtime.ReplacementRule[ReplicationGroupResource]{
				runtime.ReplaceWhen(
					runtime.InputField(func(input *ReplicationGroupResource) *string {
						return &input.Engine
					}),
					func(_, desired string) bool { return desired == "redis" },
				),
			},
		},
	}
}

func (r *SubnetGroupResource) ResourceDefinition() runtime.ResourceDefinition[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input SubnetGroupResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[SubnetGroupResource]{
				runtime.InputField(func(input *SubnetGroupResource) *string { return &input.Name }),
			},
		},
	}
}
