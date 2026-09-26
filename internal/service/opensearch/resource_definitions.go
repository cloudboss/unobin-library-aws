package opensearch

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *DomainResource) ResourceDefinition() runtime.ResourceDefinition[DomainResource, *DomainResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[DomainResource, *DomainResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input DomainResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Equality: []runtime.InputEqualityRule[DomainResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *DomainResource) **DomainVPCOptions { return &input.VPCOptions }),
				func(prior, desired *DomainVPCOptions) bool {
					return r.EquivalentInput("vpc-options", DomainResource{VPCOptions: prior}, DomainResource{VPCOptions: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *DomainResource) **DomainAutoTuneOptions { return &input.AutoTuneOptions }),
				func(prior, desired *DomainAutoTuneOptions) bool {
					return r.EquivalentInput("auto-tune-options", DomainResource{AutoTuneOptions: prior}, DomainResource{AutoTuneOptions: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *DomainResource) **[]DomainLogPublishingOption { return &input.LogPublishingOptions }),
				func(prior, desired *[]DomainLogPublishingOption) bool {
					return r.EquivalentInput("log-publishing-options", DomainResource{LogPublishingOptions: prior}, DomainResource{LogPublishingOptions: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *DomainResource) **string { return &input.AccessPolicies }),
				func(prior, desired *string) bool {
					return r.EquivalentInput("access-policies", DomainResource{AccessPolicies: prior}, DomainResource{AccessPolicies: desired})
				},
			),
		},
		Replace: runtime.Replacement[DomainResource, *DomainResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[DomainResource]{
				runtime.InputField(func(input *DomainResource) *string { return &input.DomainName }),
				runtime.InputField(func(input *DomainResource) **DomainVPCOptions { return &input.VPCOptions }),
			},
		},
	}
}
