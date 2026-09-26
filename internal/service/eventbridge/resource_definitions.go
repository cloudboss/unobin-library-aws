package eventbridge

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *EventBusResource) ResourceDefinition() runtime.ResourceDefinition[EventBusResource, *EventBusResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[EventBusResource, *EventBusResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[EventBusResource, *EventBusResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[EventBusResource]{
				runtime.InputField(func(input *EventBusResource) *string { return &input.Name }),
				runtime.InputField(func(input *EventBusResource) **string { return &input.EventSourceName }),
			},
		},
	}
}

func (r *RuleResource) ResourceDefinition() runtime.ResourceDefinition[RuleResource, *RuleResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[RuleResource, *RuleResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[RuleResource, *RuleResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RuleResource]{
				runtime.InputField(func(input *RuleResource) *string { return &input.Name }),
				runtime.InputField(func(input *RuleResource) **string { return &input.EventBusName }),
			},
		},
	}
}

func (r *TargetResource) ResourceDefinition() runtime.ResourceDefinition[TargetResource, *TargetResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[TargetResource, *TargetResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[TargetResource, *TargetResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[TargetResource]{
				runtime.InputField(func(input *TargetResource) **string { return &input.EventBusName }),
				runtime.InputField(func(input *TargetResource) *string { return &input.Rule }),
				runtime.InputField(func(input *TargetResource) **string { return &input.TargetId }),
			},
		},
	}
}
