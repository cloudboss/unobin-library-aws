package iam

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *AccessKeyResource) ResourceDefinition() runtime.ResourceDefinition[AccessKeyResource, *AccessKeyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[AccessKeyResource, *AccessKeyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[AccessKeyResource, *AccessKeyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[AccessKeyResource]{
				runtime.InputField(func(input *AccessKeyResource) *string { return &input.UserName }),
				runtime.InputField(func(input *AccessKeyResource) *string { return &input.PgpKey }),
			},
		},
	}
}

func (r *GroupPolicyAttachmentResource) ResourceDefinition() runtime.ResourceDefinition[GroupPolicyAttachmentResource, *GroupPolicyAttachmentResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[GroupPolicyAttachmentResource, *GroupPolicyAttachmentResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[GroupPolicyAttachmentResource, *GroupPolicyAttachmentResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[GroupPolicyAttachmentResource]{
				runtime.InputField(func(input *GroupPolicyAttachmentResource) *string { return &input.GroupName }),
				runtime.InputField(func(input *GroupPolicyAttachmentResource) *string { return &input.PolicyArn }),
			},
		},
	}
}

func (r *GroupPolicyResource) ResourceDefinition() runtime.ResourceDefinition[GroupPolicyResource, *GroupPolicyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[GroupPolicyResource, *GroupPolicyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[GroupPolicyResource, *GroupPolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[GroupPolicyResource]{
				runtime.InputField(func(input *GroupPolicyResource) *string { return &input.GroupName }),
				runtime.InputField(func(input *GroupPolicyResource) *string { return &input.PolicyName }),
			},
		},
	}
}

func (r *GroupResource) ResourceDefinition() runtime.ResourceDefinition[GroupResource, *GroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[GroupResource, *GroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
	}
}

func (r *InstanceProfileResource) ResourceDefinition() runtime.ResourceDefinition[InstanceProfileResource, *InstanceProfileResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[InstanceProfileResource, *InstanceProfileResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[InstanceProfileResource, *InstanceProfileResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[InstanceProfileResource]{
				runtime.InputField(func(input *InstanceProfileResource) *string { return &input.InstanceProfileName }),
				runtime.InputField(func(input *InstanceProfileResource) **string { return &input.Path }),
			},
		},
	}
}

func (r *OpenIDConnectProviderResource) ResourceDefinition() runtime.ResourceDefinition[OpenIDConnectProviderResource, *OpenIDConnectProviderResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[OpenIDConnectProviderResource, *OpenIDConnectProviderResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[OpenIDConnectProviderResource, *OpenIDConnectProviderResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[OpenIDConnectProviderResource]{
				runtime.InputField(func(input *OpenIDConnectProviderResource) *string { return &input.Url }),
			},
		},
	}
}

func (r *PolicyResource) ResourceDefinition() runtime.ResourceDefinition[PolicyResource, *PolicyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[PolicyResource, *PolicyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[PolicyResource, *PolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[PolicyResource]{
				runtime.InputField(func(input *PolicyResource) *string { return &input.PolicyName }),
				runtime.InputField(func(input *PolicyResource) **string { return &input.Path }),
				runtime.InputField(func(input *PolicyResource) **string { return &input.Description }),
			},
		},
	}
}

func (r *RolePolicyAttachmentResource) ResourceDefinition() runtime.ResourceDefinition[RolePolicyAttachmentResource, *RolePolicyAttachmentResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[RolePolicyAttachmentResource, *RolePolicyAttachmentResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[RolePolicyAttachmentResource, *RolePolicyAttachmentResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RolePolicyAttachmentResource]{
				runtime.InputField(func(input *RolePolicyAttachmentResource) *string { return &input.RoleName }),
				runtime.InputField(func(input *RolePolicyAttachmentResource) *string { return &input.PolicyArn }),
			},
		},
	}
}

func (r *RolePolicyResource) ResourceDefinition() runtime.ResourceDefinition[RolePolicyResource, *RolePolicyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[RolePolicyResource, *RolePolicyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[RolePolicyResource, *RolePolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RolePolicyResource]{
				runtime.InputField(func(input *RolePolicyResource) *string { return &input.RoleName }),
				runtime.InputField(func(input *RolePolicyResource) *string { return &input.PolicyName }),
			},
		},
	}
}

func (r *RoleResource) ResourceDefinition() runtime.ResourceDefinition[RoleResource, *RoleResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[RoleResource, *RoleResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[RoleResource, *RoleResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RoleResource]{
				runtime.InputField(func(input *RoleResource) *string { return &input.RoleName }),
				runtime.InputField(func(input *RoleResource) **string { return &input.Path }),
			},
		},
	}
}

func (r *UserPolicyAttachmentResource) ResourceDefinition() runtime.ResourceDefinition[UserPolicyAttachmentResource, *UserPolicyAttachmentResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[UserPolicyAttachmentResource, *UserPolicyAttachmentResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[UserPolicyAttachmentResource, *UserPolicyAttachmentResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[UserPolicyAttachmentResource]{
				runtime.InputField(func(input *UserPolicyAttachmentResource) *string { return &input.User }),
				runtime.InputField(func(input *UserPolicyAttachmentResource) *string { return &input.PolicyArn }),
			},
		},
	}
}

func (r *UserPolicyResource) ResourceDefinition() runtime.ResourceDefinition[UserPolicyResource, *UserPolicyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[UserPolicyResource, *UserPolicyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[UserPolicyResource, *UserPolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[UserPolicyResource]{
				runtime.InputField(func(input *UserPolicyResource) *string { return &input.UserName }),
				runtime.InputField(func(input *UserPolicyResource) *string { return &input.PolicyName }),
			},
		},
	}
}

func (r *UserResource) ResourceDefinition() runtime.ResourceDefinition[UserResource, *UserResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[UserResource, *UserResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
	}
}
