package kms

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/kms"
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
		Name:          "aws-kms",
		Description:   "AWS KMS library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"key":   makeResource[svc.Key, *svc.KeyOutput](),
			"alias": makeResource[svc.Alias, *svc.AliasOutput](),
		},
	}
}
