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
	ResourceDefinition() runtime.ResourceDefinition[T, Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT](PT(new(T)).ResourceDefinition())
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "aws-kms",
		Description: "AWS KMS library for Unobin.",
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI:            "1.1",
			SuggestedUnobinVersion: "v0.13.0",
		},
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"grant": makeResource[svc.GrantResource, *svc.GrantResourceOutput](),
			"key":   makeResource[svc.KeyResource, *svc.KeyResourceOutput](),
			"alias": makeResource[svc.AliasResource, *svc.AliasResourceOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"key": runtime.MakeDataSource[
				svc.KeyDataSource, *svc.KeyDataSourceOutput, *awscfg.Configuration,
			](),
			"alias": runtime.MakeDataSource[
				svc.AliasDataSource, *svc.AliasDataSourceOutput, *awscfg.Configuration,
			](),
		},
	}
}
