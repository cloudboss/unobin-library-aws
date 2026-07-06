package sns

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/sns"
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
		Name:          "aws-sns",
		Description:   "AWS SNS library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"topic":        makeResource[svc.TopicResource, *svc.TopicResourceOutput](),
			"topic-policy": makeResource[svc.TopicPolicyResource, *svc.TopicPolicyResourceOutput](),
			"topic-subscription": makeResource[
				svc.TopicSubscriptionResource,
				*svc.TopicSubscriptionResourceOutput](),
		},
	}
}
