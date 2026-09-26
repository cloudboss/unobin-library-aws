package s3

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *BucketNotificationResource) ResourceDefinition() runtime.ResourceDefinition[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Migrate:       r.Migrate,
		Replace: runtime.Replacement[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[BucketNotificationResource]{
				runtime.InputField(func(input *BucketNotificationResource) *string { return &input.Bucket }),
			},
		},
	}
}

func (r *BucketPolicyResource) ResourceDefinition() runtime.ResourceDefinition[BucketPolicyResource, *BucketPolicyResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[BucketPolicyResource, *BucketPolicyResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[BucketPolicyResource, *BucketPolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[BucketPolicyResource]{
				runtime.InputField(func(input *BucketPolicyResource) *string { return &input.Bucket }),
			},
		},
	}
}

func (r *BucketResource) ResourceDefinition() runtime.ResourceDefinition[BucketResource, *BucketResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[BucketResource, *BucketResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[BucketResource, *BucketResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[BucketResource]{
				runtime.InputField(func(input *BucketResource) *string { return &input.Bucket }),
				runtime.InputField(func(input *BucketResource) **bool { return &input.ObjectLockEnabled }),
			},
		},
	}
}

func (r *ObjectResource) ResourceDefinition() runtime.ResourceDefinition[ObjectResource, *ObjectResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ObjectResource, *ObjectResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ObjectResource, *ObjectResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ObjectResource]{
				runtime.InputField(func(input *ObjectResource) *string { return &input.Bucket }),
				runtime.InputField(func(input *ObjectResource) *string { return &input.Key }),
			},
		},
	}
}
