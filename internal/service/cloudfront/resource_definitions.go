package cloudfront

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *DistributionResource) ResourceDefinition() runtime.ResourceDefinition[DistributionResource, *DistributionResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[DistributionResource, *DistributionResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
	}
}

func (r *FunctionResource) ResourceDefinition() runtime.ResourceDefinition[FunctionResource, *FunctionResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[FunctionResource, *FunctionResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[FunctionResource, *FunctionResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[FunctionResource]{
				runtime.InputField(func(input *FunctionResource) *string { return &input.Name }),
			},
		},
	}
}

func (r *OriginAccessControlResource) ResourceDefinition() runtime.ResourceDefinition[OriginAccessControlResource, *OriginAccessControlResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[OriginAccessControlResource, *OriginAccessControlResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
	}
}

func (r *ResponseHeadersPolicyResource) ResourceDefinition() runtime.ResourceDefinition[ResponseHeadersPolicyResource, *ResponseHeadersPolicyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ResponseHeadersPolicyResource, *ResponseHeadersPolicyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
	}
}
