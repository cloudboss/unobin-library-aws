package elbv2

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/elbv2"
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
		Name:          "aws-elbv2",
		Description:   "AWS ELBv2 library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"load-balancer": makeResource[svc.LoadBalancerResource, *svc.LoadBalancerResourceOutput](),
			"target-group":  makeResource[svc.TargetGroupResource, *svc.TargetGroupResourceOutput](),
			"target-group-attachment": makeResource[
				svc.TargetGroupAttachmentResource, *svc.TargetGroupAttachmentResourceOutput](),
			"listener": makeResource[svc.ListenerResource, *svc.ListenerResourceOutput](),
			"listener-rule": makeResource[
				svc.ListenerRuleResource,
				*svc.ListenerRuleResourceOutput](),
			"listener-certificate": makeResource[
				svc.ListenerCertificateResource,
				*svc.ListenerCertificateResourceOutput](),
		},
	}
}
