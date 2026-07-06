package iam

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/iam"
)

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
}

type dataSourcePtr[T, Out any] interface {
	*T
	runtime.TypedDataSource[Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT]()
}

func makeDataSource[T, Out any, PT dataSourcePtr[T, Out]]() runtime.DataSourceRegistration {
	return runtime.MakeDataSource[T, Out, *awscfg.Configuration, PT]()
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "aws-iam",
		Description:   "AWS IAM library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"role":       makeResource[svc.RoleResource, *svc.RoleResourceOutput](),
			"group":      makeResource[svc.GroupResource, *svc.GroupResourceOutput](),
			"user":       makeResource[svc.UserResource, *svc.UserResourceOutput](),
			"access-key": makeResource[svc.AccessKeyResource, *svc.AccessKeyResourceOutput](),
			"policy":     makeResource[svc.PolicyResource, *svc.PolicyResourceOutput](),
			"instance-profile": makeResource[
				svc.InstanceProfileResource,
				*svc.InstanceProfileResourceOutput](),
			"openid-connect-provider": makeResource[
				svc.OpenIDConnectProviderResource, *svc.OpenIDConnectProviderResourceOutput](),
			"role-policy-attachment": makeResource[
				svc.RolePolicyAttachmentResource, *svc.RolePolicyAttachmentResourceOutput](),
			"group-policy-attachment": makeResource[
				svc.GroupPolicyAttachmentResource, *svc.GroupPolicyAttachmentResourceOutput](),
			"user-policy-attachment": makeResource[
				svc.UserPolicyAttachmentResource, *svc.UserPolicyAttachmentResourceOutput](),
			"role-policy":  makeResource[svc.RolePolicyResource, *svc.RolePolicyResourceOutput](),
			"group-policy": makeResource[svc.GroupPolicyResource, *svc.GroupPolicyResourceOutput](),
			"user-policy":  makeResource[svc.UserPolicyResource, *svc.UserPolicyResourceOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"openid-connect-provider": makeDataSource[
				svc.OpenIDConnectProviderDataSource, *svc.OpenIDConnectProviderDataSourceOutput](),
		},
	}
}
