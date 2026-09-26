package apigatewayv2

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *ApiMappingResource) ResourceDefinition() runtime.ResourceDefinition[ApiMappingResource, *ApiMappingResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ApiMappingResource, *ApiMappingResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ApiMappingResource, *ApiMappingResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ApiMappingResource]{
				runtime.InputField(func(input *ApiMappingResource) *string { return &input.ApiId }),
				runtime.InputField(func(input *ApiMappingResource) *string { return &input.DomainName }),
			},
		},
	}
}

func (r *ApiResource) ResourceDefinition() runtime.ResourceDefinition[ApiResource, *ApiResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ApiResource, *ApiResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ApiResource, *ApiResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ApiResource]{
				runtime.InputField(func(input *ApiResource) *string { return &input.ProtocolType }),
				runtime.InputField(func(input *ApiResource) **string { return &input.CredentialsArn }),
				runtime.InputField(func(input *ApiResource) **string { return &input.RouteKey }),
				runtime.InputField(func(input *ApiResource) **string { return &input.Target }),
			},
		},
	}
}

func (r *AuthorizerResource) ResourceDefinition() runtime.ResourceDefinition[AuthorizerResource, *AuthorizerResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[AuthorizerResource, *AuthorizerResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[AuthorizerResource, *AuthorizerResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[AuthorizerResource]{
				runtime.InputField(func(input *AuthorizerResource) *string { return &input.ApiId }),
			},
		},
	}
}

func (r *DomainNameResource) ResourceDefinition() runtime.ResourceDefinition[DomainNameResource, *DomainNameResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[DomainNameResource, *DomainNameResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[DomainNameResource, *DomainNameResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[DomainNameResource]{
				runtime.InputField(func(input *DomainNameResource) *string { return &input.DomainName }),
			},
		},
	}
}

func (r *IntegrationResource) ResourceDefinition() runtime.ResourceDefinition[IntegrationResource, *IntegrationResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[IntegrationResource, *IntegrationResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[IntegrationResource, *IntegrationResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[IntegrationResource]{
				runtime.InputField(func(input *IntegrationResource) *string { return &input.ApiId }),
				runtime.InputField(func(input *IntegrationResource) *string { return &input.IntegrationType }),
				runtime.InputField(func(input *IntegrationResource) **string { return &input.IntegrationSubtype }),
			},
		},
	}
}

func (r *RouteResource) ResourceDefinition() runtime.ResourceDefinition[RouteResource, *RouteResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[RouteResource, *RouteResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[RouteResource, *RouteResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RouteResource]{
				runtime.InputField(func(input *RouteResource) *string { return &input.ApiId }),
			},
		},
	}
}

func (r *StageResource) ResourceDefinition() runtime.ResourceDefinition[StageResource, *StageResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[StageResource, *StageResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[StageResource, *StageResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[StageResource]{
				runtime.InputField(func(input *StageResource) *string { return &input.ApiId }),
				runtime.InputField(func(input *StageResource) *string { return &input.Name }),
			},
		},
	}
}
