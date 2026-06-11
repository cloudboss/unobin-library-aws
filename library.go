// Package library exports the unobin registration record for the AWS
// library. Library returns the resources, data sources, actions, and
// configuration the library provides, keyed by the names stack source
// uses to reference them.
package library

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"

	"github.com/cloudboss/unobin-library-aws/internal/service/acm"
	"github.com/cloudboss/unobin-library-aws/internal/service/apigatewayv2"
	"github.com/cloudboss/unobin-library-aws/internal/service/autoscaling"
	"github.com/cloudboss/unobin-library-aws/internal/service/cloudfront"
	"github.com/cloudboss/unobin-library-aws/internal/service/cloudwatch"
	"github.com/cloudboss/unobin-library-aws/internal/service/cloudwatchlogs"
	"github.com/cloudboss/unobin-library-aws/internal/service/dynamodb"
	"github.com/cloudboss/unobin-library-aws/internal/service/ec2"
	"github.com/cloudboss/unobin-library-aws/internal/service/ecr"
	"github.com/cloudboss/unobin-library-aws/internal/service/ecs"
	"github.com/cloudboss/unobin-library-aws/internal/service/elbv2"
	"github.com/cloudboss/unobin-library-aws/internal/service/eventbridge"
	"github.com/cloudboss/unobin-library-aws/internal/service/iam"
	"github.com/cloudboss/unobin-library-aws/internal/service/kms"
	"github.com/cloudboss/unobin-library-aws/internal/service/lambda"
	"github.com/cloudboss/unobin-library-aws/internal/service/rds"
	"github.com/cloudboss/unobin-library-aws/internal/service/route53"
	"github.com/cloudboss/unobin-library-aws/internal/service/s3"
	"github.com/cloudboss/unobin-library-aws/internal/service/secretsmanager"
	"github.com/cloudboss/unobin-library-aws/internal/service/sns"
	"github.com/cloudboss/unobin-library-aws/internal/service/sqs"
	"github.com/cloudboss/unobin-library-aws/internal/service/ssm"
	"github.com/cloudboss/unobin-library-aws/internal/service/sts"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "aws",
		Description: "AWS library for unobin.",
		Configuration: &cfg.ConfigurationType{
			Description: "AWS library configuration",
			New:         func() any { return &awscfg.Configuration{} },
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
			"ec2-internet-gateway": runtime.MakeResource[
				ec2.InternetGateway, *ec2.InternetGatewayOutput](),
			"ec2-route-table": runtime.MakeResource[
				ec2.RouteTable, *ec2.RouteTableOutput](),
			"ec2-route": runtime.MakeResource[ec2.Route, *ec2.RouteOutput](),
			"ec2-route-table-association": runtime.MakeResource[
				ec2.RouteTableAssociation,
				*ec2.RouteTableAssociationOutput](),
			"ec2-eip": runtime.MakeResource[ec2.Eip, *ec2.EipOutput](),
			"ec2-nat-gateway": runtime.MakeResource[
				ec2.NatGateway, *ec2.NatGatewayOutput](),
			"ec2-vpc-endpoint": runtime.MakeResource[
				ec2.VpcEndpoint, *ec2.VpcEndpointOutput](),
			"ec2-key-pair": runtime.MakeResource[
				ec2.KeyPair, *ec2.KeyPairOutput](),
			"ec2-instance": runtime.MakeResource[
				ec2.Instance, *ec2.InstanceOutput](),
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
			"iam-role-policy": runtime.MakeResource[
				iam.RolePolicy, *iam.RolePolicyOutput](),
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
			"lambda-event-source-mapping": runtime.MakeResource[
				lambda.EventSourceMapping, *lambda.EventSourceMappingOutput](),
			"lambda-function-url": runtime.MakeResource[
				lambda.FunctionUrl, *lambda.FunctionUrlOutput](),
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
			"autoscaling-group": runtime.MakeResource[
				autoscaling.Group, *autoscaling.GroupOutput](),
			"autoscaling-policy": runtime.MakeResource[
				autoscaling.Policy, *autoscaling.PolicyOutput](),
			"autoscaling-lifecycle-hook": runtime.MakeResource[
				autoscaling.LifecycleHook, *autoscaling.LifecycleHookOutput](),
			"rds-subnet-group": runtime.MakeResource[
				rds.SubnetGroup, *rds.SubnetGroupOutput](),
			"rds-parameter-group": runtime.MakeResource[
				rds.ParameterGroup, *rds.ParameterGroupOutput](),
			"rds-cluster-parameter-group": runtime.MakeResource[
				rds.ClusterParameterGroup, *rds.ClusterParameterGroupOutput](),
			"rds-instance": runtime.MakeResource[
				rds.Instance, *rds.InstanceOutput](),
			"rds-cluster": runtime.MakeResource[
				rds.Cluster, *rds.ClusterOutput](),
			"rds-cluster-instance": runtime.MakeResource[
				rds.ClusterInstance, *rds.ClusterInstanceOutput](),
			"cloudwatchlogs-log-group": runtime.MakeResource[
				cloudwatchlogs.LogGroup, *cloudwatchlogs.LogGroupOutput](),
			"cloudwatch-metric-alarm": runtime.MakeResource[
				cloudwatch.MetricAlarm, *cloudwatch.MetricAlarmOutput](),
			"cloudfront-origin-access-control": runtime.MakeResource[
				cloudfront.OriginAccessControl,
				*cloudfront.OriginAccessControlOutput](),
			"cloudfront-function": runtime.MakeResource[
				cloudfront.Function, *cloudfront.FunctionOutput](),
			"cloudfront-response-headers-policy": runtime.MakeResource[
				cloudfront.ResponseHeadersPolicy,
				*cloudfront.ResponseHeadersPolicyOutput](),
			"cloudfront-distribution": runtime.MakeResource[
				cloudfront.Distribution, *cloudfront.DistributionOutput](),
			"route53-hosted-zone": runtime.MakeResource[
				route53.HostedZone, *route53.HostedZoneOutput](),
			"route53-record-set": runtime.MakeResource[
				route53.RecordSet, *route53.RecordSetOutput](),
			"acm-certificate": runtime.MakeResource[
				acm.Certificate, *acm.CertificateOutput](),
			"acm-certificate-validation": runtime.MakeResource[
				acm.CertificateValidation, *acm.CertificateValidationOutput](),
			"sqs-queue": runtime.MakeResource[sqs.Queue, *sqs.QueueOutput](),
			"sqs-queue-policy": runtime.MakeResource[
				sqs.QueuePolicy, *sqs.QueuePolicyOutput](),
			"sns-topic": runtime.MakeResource[sns.Topic, *sns.TopicOutput](),
			"sns-topic-subscription": runtime.MakeResource[
				sns.TopicSubscription, *sns.TopicSubscriptionOutput](),
			"sns-topic-policy": runtime.MakeResource[
				sns.TopicPolicy, *sns.TopicPolicyOutput](),
			"dynamodb-table": runtime.MakeResource[
				dynamodb.Table, *dynamodb.TableOutput](),
			"ssm-parameter": runtime.MakeResource[
				ssm.Parameter, *ssm.ParameterOutput](),
			"secretsmanager-secret": runtime.MakeResource[
				secretsmanager.Secret, *secretsmanager.SecretOutput](),
			"ecr-repository": runtime.MakeResource[
				ecr.Repository, *ecr.RepositoryOutput](),
			"ecs-cluster": runtime.MakeResource[ecs.Cluster, *ecs.ClusterOutput](),
			"ecs-task-definition": runtime.MakeResource[
				ecs.TaskDefinition, *ecs.TaskDefinitionOutput](),
			"ecs-service": runtime.MakeResource[ecs.Service, *ecs.ServiceOutput](),
			"apigatewayv2-api": runtime.MakeResource[
				apigatewayv2.Api, *apigatewayv2.ApiOutput](),
			"apigatewayv2-integration": runtime.MakeResource[
				apigatewayv2.Integration, *apigatewayv2.IntegrationOutput](),
			"apigatewayv2-route": runtime.MakeResource[
				apigatewayv2.Route, *apigatewayv2.RouteOutput](),
			"apigatewayv2-stage": runtime.MakeResource[
				apigatewayv2.Stage, *apigatewayv2.StageOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"ec2-ami": runtime.MakeDataSource[ec2.AMI, *ec2.AMIOutput](),
			"ec2-availability-zones": runtime.MakeDataSource[
				ec2.AvailabilityZones, *ec2.AvailabilityZonesOutput](),
			"sts-caller-identity": runtime.MakeDataSource[
				sts.CallerIdentity, *sts.CallerIdentityOutput](),
			"iam-openid-connect-provider": runtime.MakeDataSource[
				iam.OpenIDConnectProviderData,
				*iam.OpenIDConnectProviderDataOutput](),
		},
		Actions: map[string]runtime.ActionRegistration{
			"lambda-invoke": runtime.MakeAction[
				lambda.Invoke, *lambda.InvokeOutput](),
		},
	}
}
