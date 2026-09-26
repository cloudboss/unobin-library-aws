package sns

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *TopicPolicyResource) ResourceDefinition() runtime.ResourceDefinition[TopicPolicyResource, *TopicPolicyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[TopicPolicyResource, *TopicPolicyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[TopicPolicyResource, *TopicPolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[TopicPolicyResource]{
				runtime.InputField(func(input *TopicPolicyResource) *string { return &input.Arn }),
			},
		},
	}
}

func (r *TopicResource) ResourceDefinition() runtime.ResourceDefinition[TopicResource, *TopicResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[TopicResource, *TopicResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[TopicResource, *TopicResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[TopicResource]{
				runtime.InputField(func(input *TopicResource) *string { return &input.Name }),
				runtime.InputField(func(input *TopicResource) **bool { return &input.FifoTopic }),
			},
		},
	}
}

func (r *TopicSubscriptionResource) ResourceDefinition() runtime.ResourceDefinition[TopicSubscriptionResource, *TopicSubscriptionResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[TopicSubscriptionResource, *TopicSubscriptionResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[TopicSubscriptionResource, *TopicSubscriptionResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[TopicSubscriptionResource]{
				runtime.InputField(func(input *TopicSubscriptionResource) *string { return &input.Protocol }),
				runtime.InputField(func(input *TopicSubscriptionResource) **string { return &input.Endpoint }),
				runtime.InputField(func(input *TopicSubscriptionResource) *string { return &input.TopicArn }),
			},
		},
	}
}
