package eks

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/eks"
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
		Name:        "aws-eks",
		Description: "AWS EKS library for Unobin.",
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI:            "1.1",
			SuggestedUnobinVersion: "v0.13.0-a.4",
		},
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"access-entry": makeResource[
				svc.AccessEntryResource,
				*svc.AccessEntryResourceOutput,
			](),
			"access-policy-association": makeResource[
				svc.AccessPolicyAssociationResource,
				*svc.AccessPolicyAssociationResourceOutput,
			](),
			"addon":   makeResource[svc.AddonResource, *svc.AddonResourceOutput](),
			"cluster": makeResource[svc.ClusterResource, *svc.ClusterResourceOutput](),
			"fargate-profile": makeResource[
				svc.FargateProfileResource,
				*svc.FargateProfileResourceOutput,
			](),
			"node-group": makeResource[svc.NodeGroupResource, *svc.NodeGroupResourceOutput](),
		},
	}
}
