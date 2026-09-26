package lambdamicrovms

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *MicrovmImageResource) ResourceDefinition() runtime.ResourceDefinition[MicrovmImageResource, *MicrovmImageResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[MicrovmImageResource, *MicrovmImageResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[MicrovmImageResource, *MicrovmImageResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[MicrovmImageResource]{
				runtime.InputField(func(input *MicrovmImageResource) *string { return &input.Name }),
			},
		},
	}
}
