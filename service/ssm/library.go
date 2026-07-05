package ssm

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/ssm"
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
		Name:          "aws-ssm",
		Description:   "AWS SSM library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"parameter": makeResource[svc.Parameter, *svc.ParameterOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"parameter": makeDataSource[svc.ParameterData, *svc.ParameterDataOutput](),
		},
	}
}
