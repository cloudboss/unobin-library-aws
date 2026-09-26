package dsql

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *ClusterPeeringResource) ResourceDefinition() runtime.ResourceDefinition[ClusterPeeringResource, *ClusterPeeringResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ClusterPeeringResource, *ClusterPeeringResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input ClusterPeeringResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Equality: []runtime.InputEqualityRule[ClusterPeeringResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *ClusterPeeringResource) *[]string { return &input.Clusters }),
				func(prior, desired []string) bool {
					return r.EquivalentInput("clusters", ClusterPeeringResource{Clusters: prior}, ClusterPeeringResource{Clusters: desired})
				},
			),
		},
	}
}

func (r *ClusterResource) ResourceDefinition() runtime.ResourceDefinition[ClusterResource, *ClusterResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ClusterResource, *ClusterResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input ClusterResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Equality: []runtime.InputEqualityRule[ClusterResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *ClusterResource) **ClusterMultiRegionProperties {
					return &input.MultiRegionProperties
				}),
				func(prior, desired *ClusterMultiRegionProperties) bool {
					if (prior == nil) != (desired == nil) {
						return false
					}
					return r.EquivalentInput("multi-region-properties.clusters",
						ClusterResource{MultiRegionProperties: prior},
						ClusterResource{MultiRegionProperties: desired}) &&
						!runtime.Changed(multiRegionWitnessRegion(prior),
							multiRegionWitnessRegion(desired))
				},
			),
		},
		Replace: runtime.Replacement[ClusterResource, *ClusterResourceOutput, *awsCfg]{
			Rules: []runtime.ReplacementRule[ClusterResource]{
				runtime.ReplaceWhen(
					runtime.InputField(func(input *ClusterResource) **ClusterMultiRegionProperties {
						return &input.MultiRegionProperties
					}),
					func(prior, desired *ClusterMultiRegionProperties) bool {
						return runtime.Changed(multiRegionWitnessRegion(prior),
							multiRegionWitnessRegion(desired))
					},
				),
			},
		},
	}
}
