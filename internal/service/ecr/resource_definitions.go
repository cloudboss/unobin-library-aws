package ecr

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *RepositoryResource) ResourceDefinition() runtime.ResourceDefinition[RepositoryResource, *RepositoryResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[RepositoryResource, *RepositoryResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[RepositoryResource, *RepositoryResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RepositoryResource]{
				runtime.InputField(func(input *RepositoryResource) *string { return &input.Name }),
				runtime.InputField(func(input *RepositoryResource) **RepositoryEncryptionConfiguration {
					return &input.EncryptionConfiguration
				}),
			},
		},
	}
}
