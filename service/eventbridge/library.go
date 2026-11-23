package eventbridge

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/eventbridge"
)

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
	ResourceDefinition() runtime.ResourceDefinition[T, Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT](PT(new(T)).ResourceDefinition())
}

type actionPtr[T, Out any] interface {
	*T
	runtime.TypedAction[Out, *awscfg.Configuration]
}

func makeAction[T, Out any, PT actionPtr[T, Out]]() runtime.ActionRegistration {
	return runtime.MakeAction[T, Out, *awscfg.Configuration, PT]()
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "aws-eventbridge",
		Description: "AWS EventBridge library for Unobin.",
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI:            "1.1",
			SuggestedUnobinVersion: "v0.13.0-a.4",
		},
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"event-bus-policy": makeResource[
				svc.EventBusPolicyResource, *svc.EventBusPolicyResourceOutput](),
			"event-bus": makeResource[svc.EventBusResource, *svc.EventBusResourceOutput](),
			"rule":      makeResource[svc.RuleResource, *svc.RuleResourceOutput](),
			"target":    makeResource[svc.TargetResource, *svc.TargetResourceOutput](),
		},
		Actions: map[string]runtime.ActionRegistration{
			"put-events": makeAction[svc.PutEventsAction, *svc.PutEventsActionOutput](),
		},
	}
}
