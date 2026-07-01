package rds

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/rds"
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
		Name:          "aws-rds",
		Description:   "AWS RDS library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"subnet-group":    makeResource[svc.SubnetGroup, *svc.SubnetGroupOutput](),
			"parameter-group": makeResource[svc.ParameterGroup, *svc.ParameterGroupOutput](),
			"cluster-parameter-group": makeResource[
				svc.ClusterParameterGroup, *svc.ClusterParameterGroupOutput](),
			"cluster":          makeResource[svc.Cluster, *svc.ClusterOutput](),
			"cluster-instance": makeResource[svc.ClusterInstance, *svc.ClusterInstanceOutput](),
			"instance":         makeResource[svc.Instance, *svc.InstanceOutput](),
		},
	}
}
