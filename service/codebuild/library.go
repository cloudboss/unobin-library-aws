package codebuild

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/codebuild"
)

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
	ResourceDefinition() runtime.ResourceDefinition[T, Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	registration := runtime.MakeResource[T, Out, *awscfg.Configuration, PT](
		PT(new(T)).ResourceDefinition())
	return composeProject(registration, registration.KnownReplacementReasons)
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "aws-codebuild", Description: "AWS CodeBuild library for Unobin.",
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI:            "1.1",
			SuggestedUnobinVersion: "v0.13.0",
		},
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"project": makeResource[svc.ProjectResource, *svc.ProjectResourceOutput](),
		},
		Actions: map[string]runtime.ActionRegistration{
			"start-build": runtime.MakeAction[
				svc.StartBuildAction, *svc.StartBuildActionOutput, *awscfg.Configuration,
			](),
		},
	}
}
