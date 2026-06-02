package eventbridge

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// The blocks below model the structured members EventBridge accepts on a
// types.Target. Each is a field on the single target PutTargets writes, so a
// block is converted to its SDK type and assembled into the target rather than
// written by its own call. A nil block leaves that member unset, so AWS applies
// its own behavior. The inner enum, range, and length rules each block notes
// are enforced by the EventBridge API; they are not declared as Constraints
// because goschema derives constraints only from top-level fields, not nested
// ones.

// TargetInputTransformer builds custom input for the target from pieces of the
// matched event. InputTemplate is required, 1..8192 characters, and may
// reference the named paths. InputPaths maps a name to a JSONPath into the
// event; it holds at most 100 entries, and no key may begin with "AWS", which
// EventBridge reserves.
type TargetInputTransformer struct {
	InputTemplate string            `ub:"input-template"`
	InputPaths    map[string]string `ub:"input-paths"`
}

func (b *TargetInputTransformer) to() *eventbridgetypes.InputTransformer {
	if b == nil {
		return nil
	}
	return &eventbridgetypes.InputTransformer{
		InputTemplate: aws.String(b.InputTemplate),
		InputPathsMap: b.InputPaths,
	}
}

// TargetRetryPolicy bounds the redelivery of an event to the target before it
// goes to the dead-letter queue. MaximumEventAgeInSeconds is 0..86400 and
// MaximumRetryAttempts is 0..185; both are sent whenever the block is present.
type TargetRetryPolicy struct {
	MaximumEventAgeInSeconds *int64 `ub:"maximum-event-age-in-seconds"`
	MaximumRetryAttempts     *int64 `ub:"maximum-retry-attempts"`
}

func (b *TargetRetryPolicy) to() *eventbridgetypes.RetryPolicy {
	if b == nil {
		return nil
	}
	return &eventbridgetypes.RetryPolicy{
		MaximumEventAgeInSeconds: ptr.Int32(b.MaximumEventAgeInSeconds),
		MaximumRetryAttempts:     ptr.Int32(b.MaximumRetryAttempts),
	}
}

// TargetDeadLetterConfig names the SQS queue where EventBridge sends an event
// that could not be delivered to the target. Arn is an SQS queue ARN, sent only
// when present.
type TargetDeadLetterConfig struct {
	Arn *string `ub:"arn"`
}

func (b *TargetDeadLetterConfig) to() *eventbridgetypes.DeadLetterConfig {
	if b == nil || b.Arn == nil {
		return nil
	}
	return &eventbridgetypes.DeadLetterConfig{Arn: b.Arn}
}

// TargetBatchParameters runs the target as an Batch job. JobDefinition and
// JobName are required. ArraySize turns the job into an array job and applies
// only in 2..10000; JobAttempts overrides the job definition's retry count and
// applies only in 1..10. A value outside its range is left unset rather than
// sent.
type TargetBatchParameters struct {
	JobDefinition string `ub:"job-definition"`
	JobName       string `ub:"job-name"`
	ArraySize     *int64 `ub:"array-size"`
	JobAttempts   *int64 `ub:"job-attempts"`
}

func (b *TargetBatchParameters) to() *eventbridgetypes.BatchParameters {
	if b == nil {
		return nil
	}
	params := &eventbridgetypes.BatchParameters{
		JobDefinition: aws.String(b.JobDefinition),
		JobName:       aws.String(b.JobName),
	}
	if b.ArraySize != nil && *b.ArraySize > 1 && *b.ArraySize <= 10000 {
		params.ArrayProperties = &eventbridgetypes.BatchArrayProperties{
			Size: int32(*b.ArraySize),
		}
	}
	if b.JobAttempts != nil && *b.JobAttempts > 0 && *b.JobAttempts <= 10 {
		params.RetryStrategy = &eventbridgetypes.BatchRetryStrategy{
			Attempts: int32(*b.JobAttempts),
		}
	}
	return params
}

// TargetKinesisParameters controls the shard assignment when the target is a
// Kinesis data stream. PartitionKeyPath is a JSONPath, 1..256, sent only when
// present; without it EventBridge keys on the event id.
type TargetKinesisParameters struct {
	PartitionKeyPath *string `ub:"partition-key-path"`
}

func (b *TargetKinesisParameters) to() *eventbridgetypes.KinesisParameters {
	if b == nil || b.PartitionKeyPath == nil {
		return nil
	}
	return &eventbridgetypes.KinesisParameters{PartitionKeyPath: b.PartitionKeyPath}
}

// TargetSqsParameters sets the message group id when the target is an SQS FIFO
// queue. EventBridge documents that such a queue needs content-based
// deduplication, but neither it nor this resource enforces the rule, so the
// field is an optional string with no constraint.
type TargetSqsParameters struct {
	MessageGroupId *string `ub:"message-group-id"`
}

func (b *TargetSqsParameters) to() *eventbridgetypes.SqsParameters {
	if b == nil || b.MessageGroupId == nil {
		return nil
	}
	return &eventbridgetypes.SqsParameters{MessageGroupId: b.MessageGroupId}
}

// TargetHttpParameters supplies headers, path parameter values, and query
// string parameters when the target is an API Gateway endpoint or an
// EventBridge API destination. Each map and the list are sent only when
// non-empty; EventBridge validates each key and value.
type TargetHttpParameters struct {
	HeaderParameters      map[string]string `ub:"header-parameters"`
	QueryStringParameters map[string]string `ub:"query-string-parameters"`
	PathParameterValues   []string          `ub:"path-parameter-values"`
}

func (b *TargetHttpParameters) to() *eventbridgetypes.HttpParameters {
	if b == nil {
		return nil
	}
	params := &eventbridgetypes.HttpParameters{}
	if len(b.HeaderParameters) > 0 {
		params.HeaderParameters = b.HeaderParameters
	}
	if len(b.QueryStringParameters) > 0 {
		params.QueryStringParameters = b.QueryStringParameters
	}
	if len(b.PathParameterValues) > 0 {
		params.PathParameterValues = b.PathParameterValues
	}
	return params
}

// TargetRedshiftDataParameters invokes the Redshift Data API ExecuteStatement
// when the target is a Redshift cluster. Database (1..64) and Sql (1..100000)
// are always sent; DbUser (1..128), StatementName (1..500), and
// SecretsManagerArn are sent only when present. WithEvent attaches the
// triggering event to the statement and defaults to false.
type TargetRedshiftDataParameters struct {
	Database          string  `ub:"database"`
	Sql               string  `ub:"sql"`
	DbUser            *string `ub:"db-user"`
	StatementName     *string `ub:"statement-name"`
	SecretsManagerArn *string `ub:"secrets-manager-arn"`
	WithEvent         *bool   `ub:"with-event"`
}

func (b *TargetRedshiftDataParameters) to() *eventbridgetypes.RedshiftDataParameters {
	if b == nil {
		return nil
	}
	params := &eventbridgetypes.RedshiftDataParameters{
		Database:  aws.String(b.Database),
		Sql:       aws.String(b.Sql),
		WithEvent: aws.ToBool(b.WithEvent),
	}
	if b.DbUser != nil {
		params.DbUser = b.DbUser
	}
	if b.StatementName != nil {
		params.StatementName = b.StatementName
	}
	if b.SecretsManagerArn != nil {
		params.SecretManagerArn = b.SecretsManagerArn
	}
	return params
}

// TargetRunCommandParametersTarget names the EC2 instances that receive the
// command, as one key with its values. Key is required (1..128); Values is a
// required list of at most 50 entries, each 1..256.
type TargetRunCommandParametersTarget struct {
	Key    string   `ub:"key"`
	Values []string `ub:"values"`
}

// TargetRunCommandParameters sends the target as an EC2 Run Command. It holds
// at most 5 instance selectors, each a key and its values.
type TargetRunCommandParameters struct {
	RunCommandTargets []TargetRunCommandParametersTarget `ub:"run-command-targets"`
}

func (b *TargetRunCommandParameters) to() *eventbridgetypes.RunCommandParameters {
	if b == nil {
		return nil
	}
	targets := make([]eventbridgetypes.RunCommandTarget, 0, len(b.RunCommandTargets))
	for i := range b.RunCommandTargets {
		targets = append(targets, eventbridgetypes.RunCommandTarget{
			Key:    aws.String(b.RunCommandTargets[i].Key),
			Values: b.RunCommandTargets[i].Values,
		})
	}
	return &eventbridgetypes.RunCommandParameters{RunCommandTargets: targets}
}

// TargetSageMakerPipelineParametersParameter is one name/value pair passed to a
// SageMaker pipeline execution. Both Name and Value are required.
type TargetSageMakerPipelineParametersParameter struct {
	Name  string `ub:"name"`
	Value string `ub:"value"`
}

// TargetSageMakerPipelineParameters starts a SageMaker Model Building Pipeline.
// PipelineParameterList holds at most 200 name/value pairs.
type TargetSageMakerPipelineParameters struct {
	PipelineParameterList []TargetSageMakerPipelineParametersParameter `ub:"pipeline-parameter-list"`
}

func (b *TargetSageMakerPipelineParameters) to() *eventbridgetypes.SageMakerPipelineParameters {
	if b == nil {
		return nil
	}
	params := make([]eventbridgetypes.SageMakerPipelineParameter, 0,
		len(b.PipelineParameterList))
	for i := range b.PipelineParameterList {
		params = append(params, eventbridgetypes.SageMakerPipelineParameter{
			Name:  aws.String(b.PipelineParameterList[i].Name),
			Value: aws.String(b.PipelineParameterList[i].Value),
		})
	}
	return &eventbridgetypes.SageMakerPipelineParameters{PipelineParameterList: params}
}

// TargetAppSyncParameters runs a GraphQL operation when the target is an
// AppSync API. GraphqlOperation is 1..1048576, sent only when present.
type TargetAppSyncParameters struct {
	GraphqlOperation *string `ub:"graphql-operation"`
}

func (b *TargetAppSyncParameters) to() *eventbridgetypes.AppSyncParameters {
	if b == nil || b.GraphqlOperation == nil {
		return nil
	}
	return &eventbridgetypes.AppSyncParameters{GraphQLOperation: b.GraphqlOperation}
}

// TargetEcsParametersNetworkConfiguration sets the awsvpc networking for an ECS
// task. Subnets is required; SecurityGroups is optional. AssignPublicIp maps
// the boolean to the API's ENABLED or DISABLED, defaulting to DISABLED.
type TargetEcsParametersNetworkConfiguration struct {
	Subnets        []string `ub:"subnets"`
	SecurityGroups []string `ub:"security-groups"`
	AssignPublicIp *bool    `ub:"assign-public-ip"`
}

func (b *TargetEcsParametersNetworkConfiguration) to() *eventbridgetypes.NetworkConfiguration {
	if b == nil {
		return nil
	}
	assign := eventbridgetypes.AssignPublicIpDisabled
	if aws.ToBool(b.AssignPublicIp) {
		assign = eventbridgetypes.AssignPublicIpEnabled
	}
	return &eventbridgetypes.NetworkConfiguration{
		AwsvpcConfiguration: &eventbridgetypes.AwsVpcConfiguration{
			Subnets:        b.Subnets,
			SecurityGroups: b.SecurityGroups,
			AssignPublicIp: assign,
		},
	}
}

// TargetEcsParametersCapacityProviderStrategy picks the capacity provider for
// the task. CapacityProvider is required; Base is 0..100000 and Weight is
// 0..1000.
type TargetEcsParametersCapacityProviderStrategy struct {
	CapacityProvider string `ub:"capacity-provider"`
	Base             *int64 `ub:"base"`
	Weight           *int64 `ub:"weight"`
}

// TargetEcsParametersPlacementConstraint constrains where the task runs. Type
// is required and is distinctInstance or memberOf; Expression is a cluster
// query language expression, required for memberOf and ignored otherwise.
type TargetEcsParametersPlacementConstraint struct {
	Type       string  `ub:"type"`
	Expression *string `ub:"expression"`
}

// TargetEcsParametersPlacementStrategy orders the task placement. Type is
// required and is random, spread, or binpack; Field names the attribute the
// spread or binpack acts on, 0..255.
type TargetEcsParametersPlacementStrategy struct {
	Type  string  `ub:"type"`
	Field *string `ub:"field"`
}

// TargetEcsParameters runs the target as an ECS task. TaskDefinitionArn is
// required. TaskCount defaults to 1; EnableEcsManagedTags and
// EnableExecuteCommand default to false. LaunchType is EC2, FARGATE, or
// EXTERNAL, and PropagateTags is TASK_DEFINITION when set. NetworkConfiguration
// holds the awsvpc settings, and the capacity provider strategy, placement
// constraints, and placement strategies tune scheduling. Tags label the task.
type TargetEcsParameters struct {
	TaskDefinitionArn        string                                        `ub:"task-definition-arn"`
	TaskCount                *int64                                        `ub:"task-count"`
	LaunchType               *string                                       `ub:"launch-type"`
	PlatformVersion          *string                                       `ub:"platform-version"`
	Group                    *string                                       `ub:"group"`
	EnableEcsManagedTags     *bool                                         `ub:"enable-ecs-managed-tags"`
	EnableExecuteCommand     *bool                                         `ub:"enable-execute-command"`
	PropagateTags            *string                                       `ub:"propagate-tags"`
	NetworkConfiguration     *TargetEcsParametersNetworkConfiguration      `ub:"network-configuration"`
	CapacityProviderStrategy []TargetEcsParametersCapacityProviderStrategy `ub:"capacity-provider-strategy"`
	PlacementConstraints     []TargetEcsParametersPlacementConstraint      `ub:"placement-constraints"`
	PlacementStrategy        []TargetEcsParametersPlacementStrategy        `ub:"placement-strategy"`
	Tags                     map[string]string                             `ub:"tags"`
}

func (b *TargetEcsParameters) to() *eventbridgetypes.EcsParameters {
	if b == nil {
		return nil
	}
	params := &eventbridgetypes.EcsParameters{
		TaskDefinitionArn:    aws.String(b.TaskDefinitionArn),
		TaskCount:            ecsTaskCount(b.TaskCount),
		EnableECSManagedTags: aws.ToBool(b.EnableEcsManagedTags),
		EnableExecuteCommand: aws.ToBool(b.EnableExecuteCommand),
		PlatformVersion:      b.PlatformVersion,
		Group:                b.Group,
		NetworkConfiguration: b.NetworkConfiguration.to(),
		Tags:                 ecsTags(b.Tags),
	}
	if b.LaunchType != nil {
		params.LaunchType = eventbridgetypes.LaunchType(*b.LaunchType)
	}
	if b.PropagateTags != nil {
		params.PropagateTags = eventbridgetypes.PropagateTags(*b.PropagateTags)
	}
	params.CapacityProviderStrategy = ecsCapacityProviderStrategy(b.CapacityProviderStrategy)
	params.PlacementConstraints = ecsPlacementConstraints(b.PlacementConstraints)
	params.PlacementStrategy = ecsPlacementStrategy(b.PlacementStrategy)
	return params
}

// ecsTaskCount applies the ECS default of one task when no count is given.
func ecsTaskCount(count *int64) *int32 {
	if count == nil {
		return aws.Int32(1)
	}
	return ptr.Int32(count)
}

// ecsTags converts the task tags to the SDK list, leaving the member unset when
// none are given.
func ecsTags(tags map[string]string) []eventbridgetypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]eventbridgetypes.Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, eventbridgetypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return out
}

// ecsCapacityProviderStrategy converts the capacity provider strategy items to
// the SDK list, leaving the member unset when none are given.
func ecsCapacityProviderStrategy(
	items []TargetEcsParametersCapacityProviderStrategy,
) []eventbridgetypes.CapacityProviderStrategyItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]eventbridgetypes.CapacityProviderStrategyItem, 0, len(items))
	for i := range items {
		out = append(out, eventbridgetypes.CapacityProviderStrategyItem{
			CapacityProvider: aws.String(items[i].CapacityProvider),
			Base:             int32Value(items[i].Base),
			Weight:           int32Value(items[i].Weight),
		})
	}
	return out
}

// ecsPlacementConstraints converts the placement constraints to the SDK list,
// leaving the member unset when none are given.
func ecsPlacementConstraints(
	items []TargetEcsParametersPlacementConstraint,
) []eventbridgetypes.PlacementConstraint {
	if len(items) == 0 {
		return nil
	}
	out := make([]eventbridgetypes.PlacementConstraint, 0, len(items))
	for i := range items {
		out = append(out, eventbridgetypes.PlacementConstraint{
			Type:       eventbridgetypes.PlacementConstraintType(items[i].Type),
			Expression: items[i].Expression,
		})
	}
	return out
}

// ecsPlacementStrategy converts the placement strategies to the SDK list,
// leaving the member unset when none are given.
func ecsPlacementStrategy(
	items []TargetEcsParametersPlacementStrategy,
) []eventbridgetypes.PlacementStrategy {
	if len(items) == 0 {
		return nil
	}
	out := make([]eventbridgetypes.PlacementStrategy, 0, len(items))
	for i := range items {
		out = append(out, eventbridgetypes.PlacementStrategy{
			Type:  eventbridgetypes.PlacementStrategyType(items[i].Type),
			Field: items[i].Field,
		})
	}
	return out
}

// int32Value narrows an optional 64-bit count to the SDK's non-pointer int32,
// using zero when the value is absent.
func int32Value(v *int64) int32 {
	if v == nil {
		return 0
	}
	return int32(*v)
}
