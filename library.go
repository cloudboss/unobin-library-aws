// Package library exports the unobin registration record for the AWS
// library. Library returns the resources, data sources, actions, and
// configuration the library provides, keyed by the names stack source
// uses to reference them.
package library

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"

	"github.com/cloudboss/unobin-library-aws/library/actions"
	"github.com/cloudboss/unobin-library-aws/library/config"
	"github.com/cloudboss/unobin-library-aws/library/resources"
	"github.com/cloudboss/unobin-library-aws/library/s3"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "aws",
		Description: "AWS library for unobin.",
		Configuration: &cfg.ConfigurationType{
			Description: "AWS library configuration",
			New:         func() any { return &config.Configuration{} },
		},
		Resources: map[string]runtime.ResourceRegistration{
			"ec2-vpc": runtime.MakeResource[resources.Ec2Vpc, *resources.Ec2VpcOutput](),
			"iam-role": runtime.MakeResource[
				resources.IamRole, *resources.IamRoleOutput](),
			"iam-policy": runtime.MakeResource[
				resources.IamPolicy, *resources.IamPolicyOutput](),
			"iam-instance-profile": runtime.MakeResource[
				resources.IamInstanceProfile, *resources.IamInstanceProfileOutput](),
			"iam-openid-connect-provider": runtime.MakeResource[
				resources.IamOpenIDConnectProvider,
				*resources.IamOpenIDConnectProviderOutput](),
			"iam-role-policy-attachment": runtime.MakeResource[
				resources.IamRolePolicyAttachment,
				*resources.IamRolePolicyAttachmentOutput](),
			"kms-key": runtime.MakeResource[resources.KmsKey, *resources.KmsKeyOutput](),
			"kms-alias": runtime.MakeResource[
				resources.KmsAlias, *resources.KmsAliasOutput](),
			"s3-bucket": runtime.MakeResource[s3.Bucket, *s3.BucketOutput](),
			"s3-bucket-policy": runtime.MakeResource[
				s3.BucketPolicy, *s3.BucketPolicyOutput](),
			"s3-object": runtime.MakeResource[s3.Object, *s3.ObjectOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{},
		Actions: map[string]runtime.ActionRegistration{
			"kms-enable-key": runtime.MakeAction[
				actions.KmsEnableKey, *actions.KmsKeyActionOutput](),
			"kms-disable-key": runtime.MakeAction[
				actions.KmsDisableKey, *actions.KmsKeyActionOutput](),
			"kms-enable-key-rotation": runtime.MakeAction[
				actions.KmsEnableKeyRotation, *actions.KmsKeyActionOutput](),
			"kms-disable-key-rotation": runtime.MakeAction[
				actions.KmsDisableKeyRotation, *actions.KmsKeyActionOutput](),
		},
	}
}
