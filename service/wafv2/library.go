package wafv2

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/wafv2"
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
		Name:        "aws-wafv2",
		Description: "AWS WAFv2 library for Unobin.",
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI:            "1.1",
			SuggestedUnobinVersion: "v0.13.0",
		},
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"regex-pattern-set": makeResource[
				svc.RegexPatternSetResource, *svc.RegexPatternSetResourceOutput,
			](),
			"ip-set":     makeResource[svc.IPSetResource, *svc.IPSetResourceOutput](),
			"rule-group": makeRuleGroupResource[svc.RuleGroupResource, *svc.RuleGroupResourceOutput](),
			"web-acl":    makeResource[svc.WebACLResource, *svc.WebACLResourceOutput](),
			"web-acl-association": makeResource[
				svc.WebACLAssociationResource,
				*svc.WebACLAssociationResourceOutput,
			](),
		},
	}
}
