package autoscaling

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *GroupResource) ResourceDefinition() runtime.ResourceDefinition[GroupResource, *GroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[GroupResource, *GroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[GroupResource, *GroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[GroupResource]{
				runtime.InputField(func(input *GroupResource) *string { return &input.Name }),
			},
		},
	}
}

func (r *LifecycleHookResource) ResourceDefinition() runtime.ResourceDefinition[LifecycleHookResource, *LifecycleHookResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[LifecycleHookResource, *LifecycleHookResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[LifecycleHookResource, *LifecycleHookResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[LifecycleHookResource]{
				runtime.InputField(func(input *LifecycleHookResource) *string { return &input.AutoScalingGroupName }),
				runtime.InputField(func(input *LifecycleHookResource) *string { return &input.Name }),
			},
		},
	}
}

func (r *PolicyResource) ResourceDefinition() runtime.ResourceDefinition[PolicyResource, *PolicyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[PolicyResource, *PolicyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[PolicyResource, *PolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[PolicyResource]{
				runtime.InputField(func(input *PolicyResource) *string { return &input.AutoScalingGroupName }),
				runtime.InputField(func(input *PolicyResource) *string { return &input.Name }),
			},
		},
	}
}
