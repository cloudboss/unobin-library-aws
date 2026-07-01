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
			"role":             makeResource[svc.Role, *svc.RoleOutput](),
			"group":            makeResource[svc.Group, *svc.GroupOutput](),
			"user":             makeResource[svc.User, *svc.UserOutput](),
			"access-key":       makeResource[svc.AccessKey, *svc.AccessKeyOutput](),
			"policy":           makeResource[svc.Policy, *svc.PolicyOutput](),
			"instance-profile": makeResource[svc.InstanceProfile, *svc.InstanceProfileOutput](),
			"openid-connect-provider": makeResource[
				svc.OpenIDConnectProvider, *svc.OpenIDConnectProviderOutput](),
			"role-policy-attachment": makeResource[
				svc.RolePolicyAttachment, *svc.RolePolicyAttachmentOutput](),
			"group-policy-attachment": makeResource[
				svc.GroupPolicyAttachment, *svc.GroupPolicyAttachmentOutput](),
			"user-policy-attachment": makeResource[
				svc.UserPolicyAttachment, *svc.UserPolicyAttachmentOutput](),
			"role-policy":  makeResource[svc.RolePolicy, *svc.RolePolicyOutput](),
			"group-policy": makeResource[svc.GroupPolicy, *svc.GroupPolicyOutput](),
			"user-policy":  makeResource[svc.UserPolicy, *svc.UserPolicyOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"openid-connect-provider": makeDataSource[
				svc.OpenIDConnectProviderData, *svc.OpenIDConnectProviderDataOutput](),
		},
	}
}
