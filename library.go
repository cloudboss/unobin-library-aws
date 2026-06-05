// Package library exports the unobin registration record for the AWS
// library. Library returns the resources, data sources, actions, and
// configuration the library provides, keyed by the names stack source
// uses to reference them.
package library

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"

	"github.com/cloudboss/unobin-library-aws/internal/config"
	"github.com/cloudboss/unobin-library-aws/internal/service/ec2"
	"github.com/cloudboss/unobin-library-aws/internal/service/elbv2"
	"github.com/cloudboss/unobin-library-aws/internal/service/eventbridge"
	"github.com/cloudboss/unobin-library-aws/internal/service/iam"
	"github.com/cloudboss/unobin-library-aws/internal/service/kms"
	"github.com/cloudboss/unobin-library-aws/internal/service/lambda"
	"github.com/cloudboss/unobin-library-aws/internal/service/s3"
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
			"ec2-security-group": runtime.MakeResource[
				ec2.SecurityGroup, *ec2.SecurityGroupOutput](),
			"ec2-security-group-ingress-rule": runtime.MakeResource[
				ec2.SecurityGroupIngressRule, *ec2.SecurityGroupIngressRuleOutput](),
			"ec2-security-group-egress-rule": runtime.MakeResource[
				ec2.SecurityGroupEgressRule, *ec2.SecurityGroupEgressRuleOutput](),
			"ec2-subnet": runtime.MakeResource[ec2.Subnet, *ec2.SubnetOutput](),
			"ec2-volume": runtime.MakeResource[ec2.Volume, *ec2.VolumeOutput](),
			"ec2-launch-template": runtime.MakeResource[
				ec2.LaunchTemplate, *ec2.LaunchTemplateOutput](),
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
			"lambda-function": runtime.MakeResource[
				lambda.Function, *lambda.FunctionOutput](),
			"lambda-permission": runtime.MakeResource[
				lambda.Permission, *lambda.PermissionOutput](),
			"eventbridge-rule": runtime.MakeResource[
				eventbridge.Rule, *eventbridge.RuleOutput](),
			"eventbridge-target": runtime.MakeResource[
				eventbridge.Target, *eventbridge.TargetOutput](),
			"elbv2-load-balancer": runtime.MakeResource[
				elbv2.LoadBalancer, *elbv2.LoadBalancerOutput](),
			"elbv2-target-group": runtime.MakeResource[
				elbv2.TargetGroup, *elbv2.TargetGroupOutput](),
			"elbv2-listener": runtime.MakeResource[
				elbv2.Listener, *elbv2.ListenerOutput](),
			"elbv2-listener-rule": runtime.MakeResource[
				elbv2.ListenerRule, *elbv2.ListenerRuleOutput](),
			"elbv2-listener-certificate": runtime.MakeResource[
				elbv2.ListenerCertificate, *elbv2.ListenerCertificateOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"ec2-ami": runtime.MakeDataSource[ec2.AMI, *ec2.AMIOutput](),
		},
		Actions: map[string]runtime.ActionRegistration{
			"lambda-invoke": runtime.MakeAction[
				lambda.Invoke, *lambda.InvokeOutput](),
		},
	}
}
