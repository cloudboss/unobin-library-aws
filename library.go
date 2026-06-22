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

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
}

type dataSourcePtr[T, Out any] interface {
	*T
	runtime.TypedDataSource[Out, *awscfg.Configuration]
}

type actionPtr[T, Out any] interface {
	*T
	runtime.TypedAction[Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT]()
}

func makeDataSource[T, Out any, PT dataSourcePtr[T, Out]]() runtime.DataSourceRegistration {
	return runtime.MakeDataSource[T, Out, *awscfg.Configuration, PT]()
}

func makeAction[T, Out any, PT actionPtr[T, Out]]() runtime.ActionRegistration {
	return runtime.MakeAction[T, Out, *awscfg.Configuration, PT]()
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "aws",
		Description: "AWS library for unobin.",
		Configuration: &cfg.ConfigurationType[*awscfg.Configuration]{
			Description: "AWS library configuration",
			New:         func() *awscfg.Configuration { return &awscfg.Configuration{} },
		},
		Resources: map[string]runtime.ResourceRegistration{
			"ec2-vpc": makeResource[ec2.Vpc, *ec2.VpcOutput](),
			"ec2-security-group": makeResource[
				ec2.SecurityGroup, *ec2.SecurityGroupOutput](),
			"ec2-security-group-ingress-rule": makeResource[
				ec2.SecurityGroupIngressRule, *ec2.SecurityGroupIngressRuleOutput](),
			"ec2-security-group-egress-rule": makeResource[
				ec2.SecurityGroupEgressRule, *ec2.SecurityGroupEgressRuleOutput](),
			"ec2-subnet": makeResource[ec2.Subnet, *ec2.SubnetOutput](),
			"ec2-volume": makeResource[ec2.Volume, *ec2.VolumeOutput](),
			"ec2-launch-template": makeResource[
				ec2.LaunchTemplate, *ec2.LaunchTemplateOutput](),
			"ec2-internet-gateway": makeResource[
				ec2.InternetGateway, *ec2.InternetGatewayOutput](),
			"ec2-route-table": makeResource[
				ec2.RouteTable, *ec2.RouteTableOutput](),
			"ec2-route": makeResource[ec2.Route, *ec2.RouteOutput](),
			"ec2-route-table-association": makeResource[
				ec2.RouteTableAssociation,
				*ec2.RouteTableAssociationOutput](),
			"ec2-eip": makeResource[ec2.Eip, *ec2.EipOutput](),
			"ec2-nat-gateway": makeResource[
				ec2.NatGateway, *ec2.NatGatewayOutput](),
			"ec2-vpc-endpoint": makeResource[
				ec2.VpcEndpoint, *ec2.VpcEndpointOutput](),
			"ec2-key-pair": makeResource[
				ec2.KeyPair, *ec2.KeyPairOutput](),
			"ec2-instance": makeResource[
				ec2.Instance, *ec2.InstanceOutput](),
			"iam-role": makeResource[
				iam.Role, *iam.RoleOutput](),
			"iam-policy": makeResource[
				iam.Policy, *iam.PolicyOutput](),
			"iam-instance-profile": makeResource[
				iam.InstanceProfile, *iam.InstanceProfileOutput](),
			"iam-openid-connect-provider": makeResource[
				iam.OpenIDConnectProvider,
				*iam.OpenIDConnectProviderOutput](),
			"iam-role-policy-attachment": makeResource[
				iam.RolePolicyAttachment,
				*iam.RolePolicyAttachmentOutput](),
			"iam-role-policy": makeResource[
				iam.RolePolicy, *iam.RolePolicyOutput](),
			"kms-key": makeResource[kms.Key, *kms.KeyOutput](),
			"kms-alias": makeResource[
				kms.Alias, *kms.AliasOutput](),
			"s3-bucket": makeResource[s3.Bucket, *s3.BucketOutput](),
			"s3-bucket-notification": makeResource[
				s3.BucketNotification, *s3.BucketNotificationOutput](),
			"s3-bucket-policy": makeResource[
				s3.BucketPolicy, *s3.BucketPolicyOutput](),
			"s3-object": makeResource[s3.Object, *s3.ObjectOutput](),
			"lambda-function": makeResource[
				lambda.Function, *lambda.FunctionOutput](),
			"lambda-permission": makeResource[
				lambda.Permission, *lambda.PermissionOutput](),
			"lambda-event-source-mapping": makeResource[
				lambda.EventSourceMapping, *lambda.EventSourceMappingOutput](),
			"lambda-function-url": makeResource[
				lambda.FunctionUrl, *lambda.FunctionUrlOutput](),
			"eventbridge-rule": makeResource[
				eventbridge.Rule, *eventbridge.RuleOutput](),
			"eventbridge-target": makeResource[
				eventbridge.Target, *eventbridge.TargetOutput](),
			"elbv2-load-balancer": makeResource[
				elbv2.LoadBalancer, *elbv2.LoadBalancerOutput](),
			"elbv2-target-group": makeResource[
				elbv2.TargetGroup, *elbv2.TargetGroupOutput](),
			"elbv2-target-group-attachment": makeResource[
				elbv2.TargetGroupAttachment, *elbv2.TargetGroupAttachmentOutput](),
			"elbv2-listener": makeResource[
				elbv2.Listener, *elbv2.ListenerOutput](),
			"elbv2-listener-rule": makeResource[
				elbv2.ListenerRule, *elbv2.ListenerRuleOutput](),
			"elbv2-listener-certificate": makeResource[
				elbv2.ListenerCertificate, *elbv2.ListenerCertificateOutput](),
			"autoscaling-group": makeResource[
				autoscaling.Group, *autoscaling.GroupOutput](),
			"autoscaling-policy": makeResource[
				autoscaling.Policy, *autoscaling.PolicyOutput](),
			"autoscaling-lifecycle-hook": makeResource[
				autoscaling.LifecycleHook, *autoscaling.LifecycleHookOutput](),
			"rds-subnet-group": makeResource[
				rds.SubnetGroup, *rds.SubnetGroupOutput](),
			"rds-parameter-group": makeResource[
				rds.ParameterGroup, *rds.ParameterGroupOutput](),
			"rds-cluster-parameter-group": makeResource[
				rds.ClusterParameterGroup, *rds.ClusterParameterGroupOutput](),
			"rds-instance": makeResource[
				rds.Instance, *rds.InstanceOutput](),
			"rds-cluster": makeResource[
				rds.Cluster, *rds.ClusterOutput](),
			"rds-cluster-instance": makeResource[
				rds.ClusterInstance, *rds.ClusterInstanceOutput](),
			"cloudwatchlogs-log-group": makeResource[
				cloudwatchlogs.LogGroup, *cloudwatchlogs.LogGroupOutput](),
			"cloudwatchlogs-subscription-filter": makeResource[
				cloudwatchlogs.SubscriptionFilter,
				*cloudwatchlogs.SubscriptionFilterOutput](),
			"cloudwatch-metric-alarm": makeResource[
				cloudwatch.MetricAlarm, *cloudwatch.MetricAlarmOutput](),
			"cloudfront-origin-access-control": makeResource[
				cloudfront.OriginAccessControl,
				*cloudfront.OriginAccessControlOutput](),
			"cloudfront-function": makeResource[
				cloudfront.Function, *cloudfront.FunctionOutput](),
			"cloudfront-response-headers-policy": makeResource[
				cloudfront.ResponseHeadersPolicy,
				*cloudfront.ResponseHeadersPolicyOutput](),
			"cloudfront-distribution": makeResource[
				cloudfront.Distribution, *cloudfront.DistributionOutput](),
			"route53-hosted-zone": makeResource[
				route53.HostedZone, *route53.HostedZoneOutput](),
			"route53-record-set": makeResource[
				route53.RecordSet, *route53.RecordSetOutput](),
			"acm-certificate": makeResource[
				acm.Certificate, *acm.CertificateOutput](),
			"acm-certificate-validation": makeResource[
				acm.CertificateValidation, *acm.CertificateValidationOutput](),
			"sqs-queue": makeResource[sqs.Queue, *sqs.QueueOutput](),
			"sqs-queue-policy": makeResource[
				sqs.QueuePolicy, *sqs.QueuePolicyOutput](),
			"sns-topic": makeResource[sns.Topic, *sns.TopicOutput](),
			"sns-topic-subscription": makeResource[
				sns.TopicSubscription, *sns.TopicSubscriptionOutput](),
			"sns-topic-policy": makeResource[
				sns.TopicPolicy, *sns.TopicPolicyOutput](),
			"dynamodb-table": makeResource[
				dynamodb.Table, *dynamodb.TableOutput](),
			"ssm-parameter": makeResource[
				ssm.Parameter, *ssm.ParameterOutput](),
			"secretsmanager-secret": makeResource[
				secretsmanager.Secret, *secretsmanager.SecretOutput](),
			"secretsmanager-secret-version": makeResource[
				secretsmanager.SecretVersion, *secretsmanager.SecretVersionOutput](),
			"ecr-repository": makeResource[
				ecr.Repository, *ecr.RepositoryOutput](),
			"ecs-cluster": makeResource[ecs.Cluster, *ecs.ClusterOutput](),
			"ecs-task-definition": makeResource[
				ecs.TaskDefinition, *ecs.TaskDefinitionOutput](),
			"ecs-service": makeResource[ecs.Service, *ecs.ServiceOutput](),
			"apigatewayv2-api": makeResource[
				apigatewayv2.Api, *apigatewayv2.ApiOutput](),
			"apigatewayv2-integration": makeResource[
				apigatewayv2.Integration, *apigatewayv2.IntegrationOutput](),
			"apigatewayv2-route": makeResource[
				apigatewayv2.Route, *apigatewayv2.RouteOutput](),
			"apigatewayv2-stage": makeResource[
				apigatewayv2.Stage, *apigatewayv2.StageOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"route53-zone": makeDataSource[route53.ZoneData, *route53.ZoneDataOutput](),
			"ec2-ami":      makeDataSource[ec2.AMI, *ec2.AMIOutput](),
			"ec2-availability-zones": makeDataSource[
				ec2.AvailabilityZones, *ec2.AvailabilityZonesOutput](),
			"ec2-subnets": makeDataSource[
				ec2.Subnets, *ec2.SubnetsOutput](),
			"sts-caller-identity": makeDataSource[
				sts.CallerIdentity, *sts.CallerIdentityOutput](),
			"iam-openid-connect-provider": makeDataSource[
				iam.OpenIDConnectProviderData,
				*iam.OpenIDConnectProviderDataOutput](),
		},
		Actions: map[string]runtime.ActionRegistration{
			"lambda-invoke": makeAction[
				lambda.Invoke, *lambda.InvokeOutput](),
		},
	}
}
