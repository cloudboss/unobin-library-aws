package cloudfront

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/cloudfront"
)

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
}

type dataSourcePtr[T, Out any] interface {
	*T
	runtime.TypedDataSource[Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT]()
}

func makeDataSource[T, Out any, PT dataSourcePtr[T, Out]]() runtime.DataSourceRegistration {
	return runtime.MakeDataSource[T, Out, *awscfg.Configuration, PT]()
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "aws-cloudfront",
		Description:   "AWS CloudFront library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"origin-access-control": makeResource[svc.OriginAccessControl, *svc.OriginAccessControlOutput](),
			"function":              makeResource[svc.Function, *svc.FunctionOutput](),
			"response-headers-policy": makeResource[
				svc.ResponseHeadersPolicy, *svc.ResponseHeadersPolicyOutput](),
			"distribution": makeResource[svc.Distribution, *svc.DistributionOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"cache-policy-data": makeDataSource[svc.CachePolicyData, *svc.CachePolicyDataOutput](),
			"origin-request-policy-data": makeDataSource[
				svc.OriginRequestPolicyData, *svc.OriginRequestPolicyDataOutput](),
		},
	}
}
