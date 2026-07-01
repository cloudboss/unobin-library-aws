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
			"load-balancer": makeResource[svc.LoadBalancer, *svc.LoadBalancerOutput](),
			"target-group":  makeResource[svc.TargetGroup, *svc.TargetGroupOutput](),
			"target-group-attachment": makeResource[
				svc.TargetGroupAttachment, *svc.TargetGroupAttachmentOutput](),
			"listener":             makeResource[svc.Listener, *svc.ListenerOutput](),
			"listener-rule":        makeResource[svc.ListenerRule, *svc.ListenerRuleOutput](),
			"listener-certificate": makeResource[svc.ListenerCertificate, *svc.ListenerCertificateOutput](),
		},
	}
}
