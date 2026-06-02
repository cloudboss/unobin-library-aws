// Package library exports the unobin registration record for the AWS
// library. Library returns the resources, data sources, actions, and
// configuration the library provides, keyed by the names stack source
// uses to reference them.
package library

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"

	"github.com/cloudboss/unobin-library-aws/library/config"
	"github.com/cloudboss/unobin-library-aws/library/ec2"
	"github.com/cloudboss/unobin-library-aws/library/iam"
	"github.com/cloudboss/unobin-library-aws/library/kms"
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
			"ec2-vpc": runtime.MakeResource[ec2.Vpc, *ec2.VpcOutput](),
			"iam-role": runtime.MakeResource[
				iam.Role, *iam.RoleOutput](),
			"iam-policy": runtime.MakeResource[
				iam.Policy, *iam.PolicyOutput](),
			"iam-instance-profile": runtime.MakeResource[
				iam.InstanceProfile, *iam.InstanceProfileOutput](),
			"iam-openid-connect-provider": runtime.MakeResource[
				iam.OpenIDConnectProvider,
				*iam.OpenIDConnectProviderOutput](),
			"iam-role-policy-attachment": runtime.MakeResource[
				iam.RolePolicyAttachment,
				*iam.RolePolicyAttachmentOutput](),
			"kms-key": runtime.MakeResource[kms.Key, *kms.KeyOutput](),
			"kms-alias": runtime.MakeResource[
				kms.Alias, *kms.AliasOutput](),
			"s3-bucket": runtime.MakeResource[s3.Bucket, *s3.BucketOutput](),
			"s3-bucket-policy": runtime.MakeResource[
				s3.BucketPolicy, *s3.BucketPolicyOutput](),
			"s3-object": runtime.MakeResource[s3.Object, *s3.ObjectOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{},
		Actions:     map[string]runtime.ActionRegistration{},
	}
}
