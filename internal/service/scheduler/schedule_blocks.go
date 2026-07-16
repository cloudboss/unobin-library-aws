package scheduler

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	schedulerapi "github.com/aws/aws-sdk-go-v2/service/scheduler"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

func (r *ScheduleResource) createInput(
	name string,
) (*schedulerapi.CreateScheduleInput, error) {
	endDate, err := scheduleDate("end-date", r.EndDate)
	if err != nil {
		return nil, err
	}
	startDate, err := scheduleDate("start-date", r.StartDate)
	if err != nil {
		return nil, err
	}
	in := &schedulerapi.CreateScheduleInput{
		Description:                r.Description,
		EndDate:                    endDate,
		FlexibleTimeWindow:         r.FlexibleTimeWindow.to(),
		GroupName:                  r.GroupName,
		KmsKeyArn:                  r.KMSKeyARN,
		Name:                       aws.String(name),
		ScheduleExpression:         aws.String(r.ScheduleExpression),
		ScheduleExpressionTimezone: r.ScheduleExpressionTimezone,
		StartDate:                  startDate,
		Target:                     r.Target.to(),
	}
	if r.ActionAfterCompletion != nil {
		in.ActionAfterCompletion =
			schedulertypes.ActionAfterCompletion(*r.ActionAfterCompletion)
	}
	if r.State != nil {
		in.State = schedulertypes.ScheduleState(*r.State)
	}
	return in, nil
}

func (r *ScheduleResource) updateInput(
	identity scheduleIdentity,
) (*schedulerapi.UpdateScheduleInput, error) {
	endDate, err := scheduleDate("end-date", r.EndDate)
	if err != nil {
		return nil, err
	}
	startDate, err := scheduleDate("start-date", r.StartDate)
	if err != nil {
		return nil, err
	}
	in := &schedulerapi.UpdateScheduleInput{
		Description:                r.Description,
		EndDate:                    endDate,
		FlexibleTimeWindow:         r.FlexibleTimeWindow.to(),
		GroupName:                  aws.String(identity.group),
		KmsKeyArn:                  r.KMSKeyARN,
		Name:                       aws.String(identity.name),
		ScheduleExpression:         aws.String(r.ScheduleExpression),
		ScheduleExpressionTimezone: r.ScheduleExpressionTimezone,
		StartDate:                  startDate,
		Target:                     r.Target.to(),
	}
	if r.ActionAfterCompletion != nil {
		in.ActionAfterCompletion =
			schedulertypes.ActionAfterCompletion(*r.ActionAfterCompletion)
	}
	if r.State != nil {
		in.State = schedulertypes.ScheduleState(*r.State)
	}
	return in, nil
}

func scheduleDate(field string, value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil, fmt.Errorf("%s must be RFC3339: %w", field, err)
	}
	return aws.Time(parsed), nil
}

func (w ScheduleFlexibleTimeWindow) to() *schedulertypes.FlexibleTimeWindow {
	return &schedulertypes.FlexibleTimeWindow{
		MaximumWindowInMinutes: ptr.Int32(w.MaximumWindowInMinutes),
		Mode:                   schedulertypes.FlexibleTimeWindowMode(w.Mode),
	}
}

func (t ScheduleTarget) to() *schedulertypes.Target {
	return &schedulertypes.Target{
		Arn:                         aws.String(t.ARN),
		DeadLetterConfig:            t.DeadLetterConfig.to(),
		EcsParameters:               t.ECSParameters.to(),
		EventBridgeParameters:       t.EventBridgeParameters.to(),
		Input:                       t.Input,
		KinesisParameters:           t.KinesisParameters.to(),
		RetryPolicy:                 t.RetryPolicy.to(),
		RoleArn:                     aws.String(t.RoleARN),
		SageMakerPipelineParameters: t.SageMakerPipelineParameters.to(),
		SqsParameters:               t.SQSParameters.to(),
	}
}

func (c *ScheduleTargetDeadLetterConfig) to() *schedulertypes.DeadLetterConfig {
	if c == nil {
		return nil
	}
	return &schedulertypes.DeadLetterConfig{Arn: aws.String(c.ARN)}
}

func (p *ScheduleTargetECSParameters) to() *schedulertypes.EcsParameters {
	if p == nil {
		return nil
	}
	out := &schedulertypes.EcsParameters{
		CapacityProviderStrategy: scheduleCapacityProviderStrategy(
			p.CapacityProviderStrategy),
		EnableECSManagedTags: p.EnableECSManagedTags,
		EnableExecuteCommand: p.EnableExecuteCommand,
		Group:                p.Group,
		NetworkConfiguration: p.NetworkConfiguration.to(),
		PlacementConstraints: schedulePlacementConstraints(p.PlacementConstraints),
		PlacementStrategy:    schedulePlacementStrategy(p.PlacementStrategy),
		PlatformVersion:      p.PlatformVersion,
		ReferenceId:          p.ReferenceID,
		Tags:                 scheduleECSTags(p.Tags),
		TaskCount:            ptr.Int32(p.TaskCount),
		TaskDefinitionArn:    aws.String(p.TaskDefinitionARN),
	}
	if p.LaunchType != nil {
		out.LaunchType = schedulertypes.LaunchType(*p.LaunchType)
	}
	if p.PropagateTags != nil {
		out.PropagateTags = schedulertypes.PropagateTags(*p.PropagateTags)
	}
	return out
}

func scheduleCapacityProviderStrategy(
	values *[]ScheduleECSCapacityProviderStrategy,
) []schedulertypes.CapacityProviderStrategyItem {
	if values == nil {
		return nil
	}
	out := make([]schedulertypes.CapacityProviderStrategyItem, 0, len(*values))
	for i := range *values {
		value := (*values)[i]
		out = append(out, schedulertypes.CapacityProviderStrategyItem{
			Base:             int32(ptr.Value(value.Base)),
			CapacityProvider: aws.String(value.CapacityProvider),
			Weight:           int32(ptr.Value(value.Weight)),
		})
	}
	return out
}

func (n *ScheduleTargetECSNetworkConfiguration) to() *schedulertypes.NetworkConfiguration {
	if n == nil {
		return nil
	}
	vpc := &schedulertypes.AwsVpcConfiguration{
		Subnets: slices.Clone(n.Subnets),
	}
	if n.SecurityGroups != nil {
		vpc.SecurityGroups = slices.Clone(*n.SecurityGroups)
	}
	if n.AssignPublicIP != nil {
		vpc.AssignPublicIp = schedulertypes.AssignPublicIpDisabled
		if *n.AssignPublicIP {
			vpc.AssignPublicIp = schedulertypes.AssignPublicIpEnabled
		}
	}
	return &schedulertypes.NetworkConfiguration{AwsvpcConfiguration: vpc}
}

func schedulePlacementConstraints(
	values *[]ScheduleTargetECSPlacementConstraint,
) []schedulertypes.PlacementConstraint {
	if values == nil {
		return nil
	}
	out := make([]schedulertypes.PlacementConstraint, 0, len(*values))
	for i := range *values {
		value := (*values)[i]
		out = append(out, schedulertypes.PlacementConstraint{
			Expression: value.Expression,
			Type:       schedulertypes.PlacementConstraintType(value.Type),
		})
	}
	return out
}

func schedulePlacementStrategy(
	values *[]ScheduleTargetECSPlacementStrategy,
) []schedulertypes.PlacementStrategy {
	if values == nil {
		return nil
	}
	out := make([]schedulertypes.PlacementStrategy, 0, len(*values))
	for i := range *values {
		value := (*values)[i]
		out = append(out, schedulertypes.PlacementStrategy{
			Field: value.Field,
			Type:  schedulertypes.PlacementStrategyType(value.Type),
		})
	}
	return out
}

func scheduleECSTags(values *map[string]string) []map[string]string {
	if values == nil {
		return nil
	}
	keys := make([]string, 0, len(*values))
	for key := range *values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, map[string]string{
			"Key":   key,
			"Value": (*values)[key],
		})
	}
	return out
}

func (p *ScheduleEventBridgeParameters) to() *schedulertypes.EventBridgeParameters {
	if p == nil {
		return nil
	}
	return &schedulertypes.EventBridgeParameters{
		DetailType: aws.String(p.DetailType),
		Source:     aws.String(p.Source),
	}
}

func (p *ScheduleTargetKinesisParameters) to() *schedulertypes.KinesisParameters {
	if p == nil {
		return nil
	}
	return &schedulertypes.KinesisParameters{PartitionKey: aws.String(p.PartitionKey)}
}

func (p *ScheduleTargetRetryPolicy) to() *schedulertypes.RetryPolicy {
	if p == nil {
		return nil
	}
	return &schedulertypes.RetryPolicy{
		MaximumEventAgeInSeconds: ptr.Int32(p.MaximumEventAgeInSeconds),
		MaximumRetryAttempts:     ptr.Int32(p.MaximumRetryAttempts),
	}
}

func (p *ScheduleSageMakerPipelineParams) to() *schedulertypes.SageMakerPipelineParameters {
	if p == nil {
		return nil
	}
	var values []schedulertypes.SageMakerPipelineParameter
	if p.PipelineParameterList != nil {
		values = make([]schedulertypes.SageMakerPipelineParameter, 0,
			len(*p.PipelineParameterList))
		for i := range *p.PipelineParameterList {
			value := (*p.PipelineParameterList)[i]
			values = append(values, schedulertypes.SageMakerPipelineParameter{
				Name:  aws.String(value.Name),
				Value: aws.String(value.Value),
			})
		}
	}
	return &schedulertypes.SageMakerPipelineParameters{
		PipelineParameterList: values,
	}
}

func (p *ScheduleTargetSQSParameters) to() *schedulertypes.SqsParameters {
	if p == nil {
		return nil
	}
	return &schedulertypes.SqsParameters{MessageGroupId: p.MessageGroupID}
}
