package cloudtrail

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *TrailResource) ResourceDefinition() runtime.ResourceDefinition[TrailResource, *TrailResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[TrailResource, *TrailResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input TrailResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[TrailResource, *TrailResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[TrailResource]{
				runtime.InputField(func(input *TrailResource) *string { return &input.Name }),
			},
		},
	}
}
