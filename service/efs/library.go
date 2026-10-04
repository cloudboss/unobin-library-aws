package efs

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/efs"
)

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
	ResourceDefinition() runtime.ResourceDefinition[T, Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT](PT(new(T)).ResourceDefinition())
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "aws-efs",
		Description: "AWS EFS library for Unobin.",
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI:            "1.0",
			SuggestedUnobinVersion: "v0.12.0",
		},
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"access-point": makeResource[
				svc.AccessPointResource,
				*svc.AccessPointResourceOutput,
			](),
			"file-system": makeResource[
				svc.FileSystemResource,
				*svc.FileSystemResourceOutput,
			](),
			"mount-target": makeResource[
				svc.MountTargetResource,
				*svc.MountTargetResourceOutput,
			](),
		},
	}
}
