package sqs

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *QueuePolicyResource) ResourceDefinition() runtime.ResourceDefinition[QueuePolicyResource, *QueuePolicyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[QueuePolicyResource, *QueuePolicyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[QueuePolicyResource, *QueuePolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[QueuePolicyResource]{
				runtime.InputField(func(input *QueuePolicyResource) *string { return &input.QueueUrl }),
			},
		},
	}
}

func (r *QueueResource) ResourceDefinition() runtime.ResourceDefinition[QueueResource, *QueueResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[QueueResource, *QueueResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[QueueResource, *QueueResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[QueueResource]{
				runtime.InputField(func(input *QueueResource) *string { return &input.Name }),
				runtime.InputField(func(input *QueueResource) **bool { return &input.FifoQueue }),
			},
		},
	}
}
