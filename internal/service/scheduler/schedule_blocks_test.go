package scheduler

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleTargetSpecializedConverters(t *testing.T) {
	sageMakerValues := []ScheduleTargetSageMakerPipelineParameter{
		{Name: "BatchSize", Value: "64"},
		{Name: "Mode", Value: "fast"},
	}
	target := ScheduleTarget{
		ARN:     testTargetARN,
		RoleARN: testRoleARN,
		EventBridgeParameters: &ScheduleEventBridgeParameters{
			DetailType: "job",
			Source:     "unobin.scheduler",
		},
		KinesisParameters: &ScheduleTargetKinesisParameters{
			PartitionKey: "partition",
		},
		SageMakerPipelineParameters: &ScheduleSageMakerPipelineParams{
			PipelineParameterList: &sageMakerValues,
		},
		SQSParameters: &ScheduleTargetSQSParameters{
			MessageGroupID: aws.String("jobs"),
		},
	}

	got := target.to()

	require.NotNil(t, got.EventBridgeParameters)
	assert.Equal(t, "job", aws.ToString(got.EventBridgeParameters.DetailType))
	assert.Equal(t, "unobin.scheduler", aws.ToString(got.EventBridgeParameters.Source))
	require.NotNil(t, got.KinesisParameters)
	assert.Equal(t, "partition", aws.ToString(got.KinesisParameters.PartitionKey))
	require.NotNil(t, got.SageMakerPipelineParameters)
	assert.Equal(t, []schedulertypes.SageMakerPipelineParameter{
		{Name: aws.String("BatchSize"), Value: aws.String("64")},
		{Name: aws.String("Mode"), Value: aws.String("fast")},
	}, got.SageMakerPipelineParameters.PipelineParameterList)
	require.NotNil(t, got.SqsParameters)
	assert.Equal(t, "jobs", aws.ToString(got.SqsParameters.MessageGroupId))
}

func TestScheduleECSConverterPreservesPointersAndDefaultZeros(t *testing.T) {
	capacity := []ScheduleECSCapacityProviderStrategy{
		{
			CapacityProvider: "one",
		},
		{
			CapacityProvider: "two",
			Base:             aws.Int64(0),
			Weight:           aws.Int64(1),
		},
	}
	securityGroups := []string{}
	placements := []ScheduleTargetECSPlacementConstraint{{
		Type:       "memberOf",
		Expression: aws.String("attribute:ecs.instance-type == t3.small"),
	}}
	strategies := []ScheduleTargetECSPlacementStrategy{{
		Type:  "binpack",
		Field: aws.String("memory"),
	}}
	tags := map[string]string{}
	parameters := &ScheduleTargetECSParameters{
		TaskDefinitionARN:        "arn:aws:ecs:us-east-1:123456789012:task-definition/job:1",
		TaskCount:                aws.Int64(0),
		CapacityProviderStrategy: &capacity,
		EnableECSManagedTags:     aws.Bool(false),
		EnableExecuteCommand:     aws.Bool(false),
		NetworkConfiguration: &ScheduleTargetECSNetworkConfiguration{
			Subnets:        []string{"subnet-a"},
			SecurityGroups: &securityGroups,
		},
		PlacementConstraints: &placements,
		PlacementStrategy:    &strategies,
		Tags:                 &tags,
	}

	got := parameters.to()

	assert.Equal(t, int32(0), aws.ToInt32(got.TaskCount))
	require.NotNil(t, got.EnableECSManagedTags)
	assert.False(t, aws.ToBool(got.EnableECSManagedTags))
	require.NotNil(t, got.EnableExecuteCommand)
	assert.False(t, aws.ToBool(got.EnableExecuteCommand))
	assert.Equal(t, []schedulertypes.CapacityProviderStrategyItem{
		{
			CapacityProvider: aws.String("one"),
			Base:             0,
			Weight:           0,
		},
		{
			CapacityProvider: aws.String("two"),
			Base:             0,
			Weight:           1,
		},
	}, got.CapacityProviderStrategy)
	require.NotNil(t, got.NetworkConfiguration)
	assert.Empty(t, got.NetworkConfiguration.AwsvpcConfiguration.AssignPublicIp)
	require.NotNil(t, got.NetworkConfiguration.AwsvpcConfiguration.SecurityGroups)
	assert.Empty(t, got.NetworkConfiguration.AwsvpcConfiguration.SecurityGroups)
	assert.Equal(t, []schedulertypes.PlacementConstraint{{
		Type:       schedulertypes.PlacementConstraintTypeMemberOf,
		Expression: aws.String("attribute:ecs.instance-type == t3.small"),
	}}, got.PlacementConstraints)
	assert.Equal(t, []schedulertypes.PlacementStrategy{{
		Type:  schedulertypes.PlacementStrategyTypeBinpack,
		Field: aws.String("memory"),
	}}, got.PlacementStrategy)
	require.NotNil(t, got.Tags)
	assert.Empty(t, got.Tags)
}

func TestScheduleRetryConverterPreservesZeroAttempts(t *testing.T) {
	policy := (&ScheduleTargetRetryPolicy{
		MaximumEventAgeInSeconds: aws.Int64(60),
		MaximumRetryAttempts:     aws.Int64(0),
	}).to()

	require.NotNil(t, policy.MaximumRetryAttempts)
	assert.Zero(t, aws.ToInt32(policy.MaximumRetryAttempts))
	assert.Equal(t, int32(60), aws.ToInt32(policy.MaximumEventAgeInSeconds))
}

func TestScheduleOptionalConvertersPreserveNil(t *testing.T) {
	target := ScheduleTarget{
		ARN:     testTargetARN,
		RoleARN: testRoleARN,
	}

	got := target.to()

	assert.Nil(t, got.DeadLetterConfig)
	assert.Nil(t, got.EcsParameters)
	assert.Nil(t, got.EventBridgeParameters)
	assert.Nil(t, got.KinesisParameters)
	assert.Nil(t, got.RetryPolicy)
	assert.Nil(t, got.SageMakerPipelineParameters)
	assert.Nil(t, got.SqsParameters)
}
