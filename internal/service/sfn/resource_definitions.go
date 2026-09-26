package sfn

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *StateMachineResource) ResourceDefinition() runtime.ResourceDefinition[StateMachineResource, *StateMachineResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[StateMachineResource, *StateMachineResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input StateMachineResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[StateMachineResource, *StateMachineResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[StateMachineResource]{
				runtime.InputField(func(input *StateMachineResource) **string { return &input.Name }),
				runtime.InputField(func(input *StateMachineResource) *string { return &input.Type }),
			},
		},
	}
}
