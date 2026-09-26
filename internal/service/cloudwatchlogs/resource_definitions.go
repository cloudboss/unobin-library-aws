package cloudwatchlogs

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *LogGroupResource) ResourceDefinition() runtime.ResourceDefinition[LogGroupResource, *LogGroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[LogGroupResource, *LogGroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[LogGroupResource, *LogGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[LogGroupResource]{
				runtime.InputField(func(input *LogGroupResource) *string { return &input.Name }),
				runtime.InputField(func(input *LogGroupResource) **string { return &input.LogGroupClass }),
			},
		},
	}
}

func (r *MetricFilterResource) ResourceDefinition() runtime.ResourceDefinition[MetricFilterResource, *MetricFilterResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[MetricFilterResource, *MetricFilterResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[MetricFilterResource, *MetricFilterResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[MetricFilterResource]{
				runtime.InputField(func(input *MetricFilterResource) *string { return &input.FilterName }),
				runtime.InputField(func(input *MetricFilterResource) *string { return &input.LogGroupName }),
			},
		},
	}
}

func (r *ResourcePolicyResource) ResourceDefinition() runtime.ResourceDefinition[ResourcePolicyResource, *ResourcePolicyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ResourcePolicyResource, *ResourcePolicyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input ResourcePolicyResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[ResourcePolicyResource, *ResourcePolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ResourcePolicyResource]{
				runtime.InputField(func(input *ResourcePolicyResource) **string { return &input.PolicyName }),
				runtime.InputField(func(input *ResourcePolicyResource) **string { return &input.ResourceArn }),
			},
		},
	}
}

func (r *SubscriptionFilterResource) ResourceDefinition() runtime.ResourceDefinition[SubscriptionFilterResource, *SubscriptionFilterResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[SubscriptionFilterResource, *SubscriptionFilterResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[SubscriptionFilterResource, *SubscriptionFilterResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[SubscriptionFilterResource]{
				runtime.InputField(func(input *SubscriptionFilterResource) *string { return &input.DestinationArn }),
				runtime.InputField(func(input *SubscriptionFilterResource) *string { return &input.LogGroupName }),
				runtime.InputField(func(input *SubscriptionFilterResource) *string { return &input.Name }),
			},
		},
	}
}
