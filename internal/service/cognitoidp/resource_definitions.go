package cognitoidp

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *UserPoolClientResource) ResourceDefinition() runtime.ResourceDefinition[UserPoolClientResource, *UserPoolClientResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[UserPoolClientResource, *UserPoolClientResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input UserPoolClientResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Equality: []runtime.InputEqualityRule[UserPoolClientResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *UserPoolClientResource) **bool { return &input.GenerateSecret }),
				func(prior, desired *bool) bool {
					return r.EquivalentInput("generate-secret", UserPoolClientResource{GenerateSecret: prior}, UserPoolClientResource{GenerateSecret: desired})
				},
			),
		},
		Replace: runtime.Replacement[UserPoolClientResource, *UserPoolClientResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[UserPoolClientResource]{
				runtime.InputField(func(input *UserPoolClientResource) *string { return &input.UserPoolID }),
				runtime.InputField(func(input *UserPoolClientResource) **bool { return &input.GenerateSecret }),
			},
		},
	}
}

func (r *UserPoolDomainResource) ResourceDefinition() runtime.ResourceDefinition[UserPoolDomainResource, *UserPoolDomainResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[UserPoolDomainResource, *UserPoolDomainResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input UserPoolDomainResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[UserPoolDomainResource, *UserPoolDomainResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[UserPoolDomainResource]{
				runtime.InputField(func(input *UserPoolDomainResource) *string { return &input.Domain }),
				runtime.InputField(func(input *UserPoolDomainResource) *string { return &input.UserPoolID }),
			},
		},
	}
}

func (r *UserPoolResource) ResourceDefinition() runtime.ResourceDefinition[UserPoolResource, *UserPoolResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[UserPoolResource, *UserPoolResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input UserPoolResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Equality: []runtime.InputEqualityRule[UserPoolResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *UserPoolResource) **[]string { return &input.AliasAttributes }),
				func(prior, desired *[]string) bool {
					return r.EquivalentInput("alias-attributes", UserPoolResource{AliasAttributes: prior}, UserPoolResource{AliasAttributes: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *UserPoolResource) **[]string { return &input.UsernameAttributes }),
				func(prior, desired *[]string) bool {
					return r.EquivalentInput("username-attributes", UserPoolResource{UsernameAttributes: prior}, UserPoolResource{UsernameAttributes: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *UserPoolResource) **[]string { return &input.AutoVerifiedAttributes }),
				func(prior, desired *[]string) bool {
					return r.EquivalentInput("auto-verified-attributes", UserPoolResource{AutoVerifiedAttributes: prior}, UserPoolResource{AutoVerifiedAttributes: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *UserPoolResource) **[]string { return &input.EnabledMFAs }),
				func(prior, desired *[]string) bool {
					return r.EquivalentInput("enabled-mfas", UserPoolResource{EnabledMFAs: prior}, UserPoolResource{EnabledMFAs: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *UserPoolResource) **[]UserPoolSchemaAttribute { return &input.Schema }),
				func(prior, desired *[]UserPoolSchemaAttribute) bool {
					return r.EquivalentInput("schema", UserPoolResource{Schema: prior}, UserPoolResource{Schema: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *UserPoolResource) **UserPoolAccountRecoverySetting { return &input.AccountRecoverySetting }),
				func(prior, desired *UserPoolAccountRecoverySetting) bool {
					return r.EquivalentInput("account-recovery-setting", UserPoolResource{AccountRecoverySetting: prior}, UserPoolResource{AccountRecoverySetting: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *UserPoolResource) **UserPoolSignInPolicy { return &input.SignInPolicy }),
				func(prior, desired *UserPoolSignInPolicy) bool {
					return r.EquivalentInput("sign-in-policy", UserPoolResource{SignInPolicy: prior}, UserPoolResource{SignInPolicy: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *UserPoolResource) **UserPoolAttributeUpdateSettings {
					return &input.UserAttributeUpdateSettings
				}),
				func(prior, desired *UserPoolAttributeUpdateSettings) bool {
					return r.EquivalentInput("user-attribute-update-settings", UserPoolResource{UserAttributeUpdateSettings: prior}, UserPoolResource{UserAttributeUpdateSettings: desired})
				},
			),
		},
		Replace: runtime.Replacement[UserPoolResource, *UserPoolResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[UserPoolResource]{
				runtime.InputField(func(input *UserPoolResource) **[]string { return &input.AliasAttributes }),
				runtime.InputField(func(input *UserPoolResource) **[]string { return &input.UsernameAttributes }),
				runtime.InputField(func(input *UserPoolResource) **UserPoolUsernameConfiguration { return &input.UsernameConfiguration }),
			},
		},
	}
}

func (r *UserResource) ResourceDefinition() runtime.ResourceDefinition[UserResource, *UserResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[UserResource, *UserResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input UserResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[UserResource, *UserResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[UserResource]{
				runtime.InputField(func(input *UserResource) *string { return &input.UserPoolID }),
				runtime.InputField(func(input *UserResource) *string { return &input.Username }),
			},
		},
	}
}
