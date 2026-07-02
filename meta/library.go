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
			"arn":               makeDataSource[svc.ARN, *svc.ARNOutput](),
			"ip-ranges":         makeDataSource[svc.IPRanges, *svc.IPRangesOutput](),
			"partition":         makeDataSource[svc.Partition, *svc.PartitionOutput](),
			"region":            makeDataSource[svc.Region, *svc.RegionOutput](),
			"regions":           makeDataSource[svc.Regions, *svc.RegionsOutput](),
			"service":           makeDataSource[svc.Service, *svc.ServiceOutput](),
			"service-principal": makeDataSource[svc.ServicePrincipal, *svc.ServicePrincipalOutput](),
		},
	}
}
