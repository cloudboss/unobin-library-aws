package cognitoidp

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/cognitoidp"
)

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
	ResourceDefinition() runtime.ResourceDefinition[T, Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT](PT(new(T)).ResourceDefinition())
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "aws-cognitoidp",
		Description:   "AWS Cognito Identity Provider library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"user":      makeResource[svc.UserResource, *svc.UserResourceOutput](),
			"user-pool": makeResource[svc.UserPoolResource, *svc.UserPoolResourceOutput](),
			"user-pool-domain": makeResource[
				svc.UserPoolDomainResource,
				*svc.UserPoolDomainResourceOutput,
			](),
			"user-pool-client": makeResource[
				svc.UserPoolClientResource,
				*svc.UserPoolClientResourceOutput,
			](),
		},
	}
}
