package applicationautoscaling

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/applicationautoscaling"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "aws-applicationautoscaling",
		Description: "AWS Application Auto Scaling library for Unobin.",
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI: "1.1", SuggestedUnobinVersion: "v0.13.0",
		},
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"scalable-target": runtime.MakeResource[
				svc.ScalableTargetResource, *svc.ScalableTargetResourceOutput,
				*awscfg.Configuration, *svc.ScalableTargetResource,
			]((&svc.ScalableTargetResource{}).ResourceDefinition()),
		},
	}
}
