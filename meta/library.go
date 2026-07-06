package meta

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/meta"
)

type dataSourcePtr[T, Out any] interface {
	*T
	runtime.TypedDataSource[Out, *awscfg.Configuration]
}

func makeDataSource[T, Out any, PT dataSourcePtr[T, Out]]() runtime.DataSourceRegistration {
	return runtime.MakeDataSource[T, Out, *awscfg.Configuration, PT]()
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "aws-meta",
		Description:   "AWS metadata library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		DataSources: map[string]runtime.DataSourceRegistration{
			"arn":       makeDataSource[svc.ARNDataSource, *svc.ARNDataSourceOutput](),
			"ip-ranges": makeDataSource[svc.IPRangesDataSource, *svc.IPRangesDataSourceOutput](),
			"partition": makeDataSource[svc.PartitionDataSource, *svc.PartitionDataSourceOutput](),
			"region":    makeDataSource[svc.RegionDataSource, *svc.RegionDataSourceOutput](),
			"regions":   makeDataSource[svc.RegionsDataSource, *svc.RegionsDataSourceOutput](),
			"service":   makeDataSource[svc.ServiceDataSource, *svc.ServiceDataSourceOutput](),
			"service-principal": makeDataSource[
				svc.ServicePrincipalDataSource,
				*svc.ServicePrincipalDataSourceOutput](),
		},
	}
}
