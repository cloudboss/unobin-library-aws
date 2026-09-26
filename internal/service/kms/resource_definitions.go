package kms

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *AliasResource) ResourceDefinition() runtime.ResourceDefinition[AliasResource, *AliasResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[AliasResource, *AliasResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[AliasResource, *AliasResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[AliasResource]{
				runtime.InputField(func(input *AliasResource) *string { return &input.AliasName }),
			},
		},
	}
}

func (r *KeyResource) ResourceDefinition() runtime.ResourceDefinition[KeyResource, *KeyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[KeyResource, *KeyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[KeyResource, *KeyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[KeyResource]{
				runtime.InputField(func(input *KeyResource) **string { return &input.KeySpec }),
				runtime.InputField(func(input *KeyResource) **string { return &input.KeyUsage }),
				runtime.InputField(func(input *KeyResource) **string { return &input.CustomKeyStoreId }),
				runtime.InputField(func(input *KeyResource) **string { return &input.XksKeyId }),
				runtime.InputField(func(input *KeyResource) **bool { return &input.MultiRegion }),
			},
		},
	}
}
