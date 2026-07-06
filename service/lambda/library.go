package lambda

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/lambda"
)

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
}

type actionPtr[T, Out any] interface {
	*T
	runtime.TypedAction[Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT]()
}

func makeAction[T, Out any, PT actionPtr[T, Out]]() runtime.ActionRegistration {
	return runtime.MakeAction[T, Out, *awscfg.Configuration, PT]()
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "aws-lambda",
		Description:   "AWS Lambda library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"function":   makeResource[svc.FunctionResource, *svc.FunctionResourceOutput](),
			"alias":      makeResource[svc.AliasResource, *svc.AliasResourceOutput](),
			"permission": makeResource[svc.PermissionResource, *svc.PermissionResourceOutput](),
			"event-source-mapping": makeResource[
				svc.EventSourceMappingResource,
				*svc.EventSourceMappingResourceOutput](),
			"function-url": makeResource[svc.FunctionUrlResource, *svc.FunctionUrlResourceOutput](),
		},
		Actions: map[string]runtime.ActionRegistration{
			"invoke": makeAction[svc.InvokeAction, *svc.InvokeActionOutput](),
		},
	}
}
