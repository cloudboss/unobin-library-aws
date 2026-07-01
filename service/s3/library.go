package s3

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/s3"
)

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT]()
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "aws-s3",
		Description:   "AWS S3 library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"bucket": makeResource[svc.Bucket, *svc.BucketOutput](),
			"bucket-notification": makeResource[
				svc.BucketNotification, *svc.BucketNotificationOutput](),
			"bucket-policy": makeResource[
				svc.BucketPolicy, *svc.BucketPolicyOutput](),
			"object": makeResource[svc.Object, *svc.ObjectOutput](),
		},
	}
}
