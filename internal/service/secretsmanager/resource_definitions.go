package secretsmanager

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *SecretResource) ResourceDefinition() runtime.ResourceDefinition[SecretResource, *SecretResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[SecretResource, *SecretResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[SecretResource, *SecretResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[SecretResource]{
				runtime.InputField(func(input *SecretResource) *string { return &input.Name }),
			},
		},
	}
}

func (r *SecretVersionResource) ResourceDefinition() runtime.ResourceDefinition[SecretVersionResource, *SecretVersionResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[SecretVersionResource, *SecretVersionResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[SecretVersionResource, *SecretVersionResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[SecretVersionResource]{
				runtime.InputField(func(input *SecretVersionResource) *string { return &input.SecretId }),
				runtime.InputField(func(input *SecretVersionResource) **string { return &input.SecretBinaryContent }),
				runtime.InputField(func(input *SecretVersionResource) **string { return &input.SecretString }),
			},
		},
	}
}
