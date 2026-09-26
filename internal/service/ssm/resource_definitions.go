package ssm

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *ParameterResource) ResourceDefinition() runtime.ResourceDefinition[ParameterResource, *ParameterResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ParameterResource, *ParameterResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ParameterResource, *ParameterResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ParameterResource]{
				runtime.InputField(func(input *ParameterResource) *string { return &input.Name }),
				runtime.InputField(func(input *ParameterResource) **string { return &input.DataType }),
			},
		},
	}
}
