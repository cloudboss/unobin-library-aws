package apigatewayv2

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/apigatewayv2"
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
		Name:          "aws-apigatewayv2",
		Description:   "AWS API Gateway v2 library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"api":         makeResource[svc.ApiResource, *svc.ApiResourceOutput](),
			"integration": makeResource[svc.IntegrationResource, *svc.IntegrationResourceOutput](),
			"authorizer":  makeResource[svc.AuthorizerResource, *svc.AuthorizerResourceOutput](),
			"route":       makeResource[svc.RouteResource, *svc.RouteResourceOutput](),
			"stage":       makeResource[svc.StageResource, *svc.StageResourceOutput](),
			"domain-name": makeResource[svc.DomainNameResource, *svc.DomainNameResourceOutput](),
			"api-mapping": makeResource[svc.ApiMappingResource, *svc.ApiMappingResourceOutput](),
		},
	}
}
