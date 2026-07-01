package cloudwatchlogs

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/cloudwatchlogs"
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
		Name:          "aws-cloudwatchlogs",
		Description:   "AWS CloudWatch Logs library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"log-group":           makeResource[svc.LogGroup, *svc.LogGroupOutput](),
			"subscription-filter": makeResource[svc.SubscriptionFilter, *svc.SubscriptionFilterOutput](),
			"metric-filter":       makeResource[svc.MetricFilter, *svc.MetricFilterOutput](),
			"resource-policy":     makeResource[svc.ResourcePolicy, *svc.ResourcePolicyOutput](),
		},
	}
}
