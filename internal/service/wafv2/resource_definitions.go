package wafv2

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *WebACLAssociationResource) ResourceDefinition() runtime.ResourceDefinition[WebACLAssociationResource, *WebACLAssociationResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[WebACLAssociationResource, *WebACLAssociationResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input WebACLAssociationResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[WebACLAssociationResource, *WebACLAssociationResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[WebACLAssociationResource]{
				runtime.InputField(func(input *WebACLAssociationResource) *string { return &input.ResourceARN }),
				runtime.InputField(func(input *WebACLAssociationResource) *string { return &input.WebACLARN }),
			},
		},
	}
}

func (r *WebACLResource) ResourceDefinition() runtime.ResourceDefinition[WebACLResource, *WebACLResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[WebACLResource, *WebACLResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input WebACLResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[WebACLResource, *WebACLResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[WebACLResource]{
				runtime.InputField(func(input *WebACLResource) **string { return &input.Name }),
				runtime.InputField(func(input *WebACLResource) *string { return &input.Scope }),
				runtime.InputField(func(input *WebACLResource) **WebACLApplicationConfig { return &input.ApplicationConfig }),
			},
		},
	}
}
