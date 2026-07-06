package secretsmanager

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/secretsmanager"
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
		Name:          "aws-secretsmanager",
		Description:   "AWS Secrets Manager library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"secret":         makeResource[svc.SecretResource, *svc.SecretResourceOutput](),
			"secret-version": makeResource[svc.SecretVersionResource, *svc.SecretVersionResourceOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"secret-version": makeDataSource[
				svc.SecretVersionDataSource,
				*svc.SecretVersionDataSourceOutput](),
		},
	}
}
