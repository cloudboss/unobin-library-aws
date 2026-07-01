package ecs

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/ecs"
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
		Name:          "aws-ecs",
		Description:   "AWS ECS library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"capacity-provider": makeResource[svc.CapacityProvider, *svc.CapacityProviderOutput](),
			"cluster":           makeResource[svc.Cluster, *svc.ClusterOutput](),
			"task-definition":   makeResource[svc.TaskDefinition, *svc.TaskDefinitionOutput](),
			"service":           makeResource[svc.Service, *svc.ServiceOutput](),
		},
	}
}
