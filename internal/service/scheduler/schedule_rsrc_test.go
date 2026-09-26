package scheduler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	schedulerapi "github.com/aws/aws-sdk-go-v2/service/scheduler"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

const (
	teamScheduleARN       = "arn:aws:scheduler:us-east-1:123456789012:schedule/team/daily"
	testTaskDefinitionARN = "arn:aws:ecs:us-east-1:123456789012:" +
		"task-definition/job:1"
)

func TestScheduleReplacementFields(t *testing.T) {
	assert.Equal(t, []string{"name", "group-name"}, (&ScheduleResource{}).ReplaceFields())
}

func TestScheduleCreateSendsCompleteRequestAndReadsImmediately(t *testing.T) {
	resource := fullSchedule()
	client := &fakeScheduleClient{
		createOutputs: []*schedulerapi.CreateScheduleOutput{{
			ScheduleArn: aws.String(teamScheduleARN),
		}},
		getOutputs: []*schedulerapi.GetScheduleOutput{scheduleGetOutput(teamScheduleARN)},
	}

	output, err := resource.create(context.Background(), client, bytes.NewReader(nil))

	require.NoError(t, err)
	assert.Equal(t, teamScheduleARN, output.ARN)
	assert.Equal(t, []string{"create", "get"}, client.calls)
	require.Len(t, client.createInputs, 1)
	in := client.createInputs[0]
	assert.Equal(t, schedulertypes.ActionAfterCompletionDelete, in.ActionAfterCompletion)
	assert.Equal(t, "", aws.ToString(in.Description))
	assert.Equal(t, "2026-07-18T10:00:00Z", in.EndDate.Format(time.RFC3339))
	assert.Equal(t, int32(30), aws.ToInt32(in.FlexibleTimeWindow.MaximumWindowInMinutes))
	assert.Equal(t, schedulertypes.FlexibleTimeWindowModeFlexible,
		in.FlexibleTimeWindow.Mode)
	assert.Equal(t, "team", aws.ToString(in.GroupName))
	assert.Equal(t,
		"arn:aws:kms:us-east-1:123456789012:key/1234",
		aws.ToString(in.KmsKeyArn))
	assert.Equal(t, "daily", aws.ToString(in.Name))
	assert.Equal(t, "cron(0 10 * * ? *)", aws.ToString(in.ScheduleExpression))
	assert.Equal(t, "America/New_York", aws.ToString(in.ScheduleExpressionTimezone))
	assert.Equal(t, "2026-07-17T10:00:00Z", in.StartDate.Format(time.RFC3339))
	assert.Equal(t, schedulertypes.ScheduleStateDisabled, in.State)
	assert.Nil(t, in.ClientToken)
	assert.Equal(t, testTargetARN, aws.ToString(in.Target.Arn))
	assert.Equal(t, testRoleARN, aws.ToString(in.Target.RoleArn))
	assert.Equal(t,
		"arn:aws:sqs:us-east-1:123456789012:schedule-dlq",
		aws.ToString(in.Target.DeadLetterConfig.Arn))
	assert.Equal(t, `{"job":"daily"}`, aws.ToString(in.Target.Input))
	require.NotNil(t, in.Target.RetryPolicy)
	assert.Equal(t, int32(3600),
		aws.ToInt32(in.Target.RetryPolicy.MaximumEventAgeInSeconds))
	assert.Equal(t, int32(4),
		aws.ToInt32(in.Target.RetryPolicy.MaximumRetryAttempts))
	require.NotNil(t, in.Target.EcsParameters)
	ecs := in.Target.EcsParameters
	assert.Equal(t,
		"arn:aws:ecs:us-east-1:123456789012:task-definition/job:1",
		aws.ToString(ecs.TaskDefinitionArn))
	assert.Equal(t, int32(2), aws.ToInt32(ecs.TaskCount))
	assert.Equal(t, schedulertypes.LaunchTypeFargate, ecs.LaunchType)
	assert.Equal(t, "1.4.0", aws.ToString(ecs.PlatformVersion))
	assert.Equal(t, "daily-jobs", aws.ToString(ecs.Group))
	assert.Equal(t, false, aws.ToBool(ecs.EnableECSManagedTags))
	assert.Equal(t, true, aws.ToBool(ecs.EnableExecuteCommand))
	assert.Equal(t, schedulertypes.PropagateTagsTaskDefinition, ecs.PropagateTags)
	assert.Equal(t, "daily", aws.ToString(ecs.ReferenceId))
	assert.Empty(t, ecs.CapacityProviderStrategy)
	assert.Equal(t, []string{"subnet-a", "subnet-b"},
		ecs.NetworkConfiguration.AwsvpcConfiguration.Subnets)
	assert.Equal(t, []string{"sg-a"},
		ecs.NetworkConfiguration.AwsvpcConfiguration.SecurityGroups)
	assert.Equal(t, schedulertypes.AssignPublicIpDisabled,
		ecs.NetworkConfiguration.AwsvpcConfiguration.AssignPublicIp)
	assert.Equal(t, []schedulertypes.PlacementStrategy{{
		Type:  schedulertypes.PlacementStrategyTypeSpread,
		Field: aws.String("attribute:ecs.availability-zone"),
	}}, ecs.PlacementStrategy)
	assert.Equal(t, []map[string]string{
		{"Key": "empty", "Value": ""},
		{"Key": "owner", "Value": "platform"},
	}, ecs.Tags)
	require.Len(t, client.getInputs, 1)
	assert.Equal(t, "team", aws.ToString(client.getInputs[0].GroupName))
	assert.Equal(t, "daily", aws.ToString(client.getInputs[0].Name))
}

func TestScheduleCreateGeneratesName(t *testing.T) {
	resource := baseSchedule()
	resource.Name = nil
	random := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	name := "unobin-000102030405060708090a0b"
	arn := "arn:aws:scheduler:us-east-1:123456789012:schedule/default/" + name
	client := &fakeScheduleClient{
		createOutputs: []*schedulerapi.CreateScheduleOutput{{
			ScheduleArn: aws.String(arn),
		}},
		getOutputs: []*schedulerapi.GetScheduleOutput{scheduleGetOutput(arn)},
	}

	output, err := resource.create(context.Background(), client, bytes.NewReader(random))

	require.NoError(t, err)
	assert.Equal(t, arn, output.ARN)
	require.Len(t, client.createInputs, 1)
	assert.Equal(t, name, aws.ToString(client.createInputs[0].Name))
}

func TestScheduleCreateReturnsEntropyErrorBeforeCallingAWS(t *testing.T) {
	resource := baseSchedule()
	resource.Name = nil
	client := &fakeScheduleClient{}

	_, err := resource.create(context.Background(), client, errorReader{})

	require.Error(t, err)
	assert.ErrorContains(t, err, "entropy unavailable")
	assert.Empty(t, client.calls)
}

func TestScheduleCreateOmitsAWSDefaults(t *testing.T) {
	resource := baseSchedule()
	client := &fakeScheduleClient{}

	_, err := resource.create(context.Background(), client, bytes.NewReader(nil))

	require.NoError(t, err)
	in := client.createInputs[0]
	assert.Empty(t, in.ActionAfterCompletion)
	assert.Nil(t, in.Description)
	assert.Nil(t, in.EndDate)
	assert.Nil(t, in.GroupName)
	assert.Nil(t, in.KmsKeyArn)
	assert.Nil(t, in.ScheduleExpressionTimezone)
	assert.Nil(t, in.StartDate)
	assert.Empty(t, in.State)
	assert.Nil(t, in.ClientToken)
	assert.Nil(t, in.Target.DeadLetterConfig)
	assert.Nil(t, in.Target.EcsParameters)
	assert.Nil(t, in.Target.EventBridgeParameters)
	assert.Nil(t, in.Target.Input)
	assert.Nil(t, in.Target.KinesisParameters)
	assert.Nil(t, in.Target.RetryPolicy)
	assert.Nil(t, in.Target.SageMakerPipelineParameters)
	assert.Nil(t, in.Target.SqsParameters)
}

func TestScheduleCreateRejectsMalformedResponses(t *testing.T) {
	tests := []struct {
		name     string
		resource ScheduleResource
		output   *schedulerapi.CreateScheduleOutput
		nilOut   bool
		match    string
	}{
		{
			name:     "nil output",
			resource: baseSchedule(),
			nilOut:   true,
			match:    "empty ARN",
		},
		{
			name:     "empty ARN",
			resource: baseSchedule(),
			output:   &schedulerapi.CreateScheduleOutput{},
			match:    "empty ARN",
		},
		{
			name:     "malformed ARN",
			resource: baseSchedule(),
			output: &schedulerapi.CreateScheduleOutput{
				ScheduleArn: aws.String("not-an-arn"),
			},
			match: "parse created",
		},
		{
			name:     "wrong service",
			resource: baseSchedule(),
			output: &schedulerapi.CreateScheduleOutput{
				ScheduleArn: aws.String(
					"arn:aws:events:us-east-1:123456789012:schedule/default/daily"),
			},
			match: "Scheduler ARN",
		},
		{
			name:     "wrong resource",
			resource: baseSchedule(),
			output: &schedulerapi.CreateScheduleOutput{
				ScheduleArn: aws.String(
					"arn:aws:scheduler:us-east-1:123456789012:group/default/daily"),
			},
			match: "schedule/<group>/<name>",
		},
		{
			name:     "wrong name",
			resource: baseSchedule(),
			output: &schedulerapi.CreateScheduleOutput{
				ScheduleArn: aws.String(
					"arn:aws:scheduler:us-east-1:123456789012:schedule/default/other"),
			},
			match: "expected \"daily\"",
		},
		{
			name: "wrong explicit group",
			resource: func() ScheduleResource {
				value := baseSchedule()
				value.GroupName = aws.String("team")
				return value
			}(),
			output: &schedulerapi.CreateScheduleOutput{
				ScheduleArn: aws.String(testScheduleARN),
			},
			match: "expected \"team\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeScheduleClient{
				createOutputs: []*schedulerapi.CreateScheduleOutput{tt.output},
				createNil:     tt.nilOut,
			}
			_, err := tt.resource.create(
				context.Background(), client, bytes.NewReader(nil))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.match)
			assert.Equal(t, []string{"create"}, client.calls)
		})
	}
}

func TestScheduleCreateFailsWhenImmediateReadCannotFindSchedule(t *testing.T) {
	resource := baseSchedule()
	client := &fakeScheduleClient{
		getErrors: []error{&schedulertypes.ResourceNotFoundException{}},
	}

	_, err := resource.create(context.Background(), client, bytes.NewReader(nil))

	assert.ErrorIs(t, err, runtime.ErrNotFound)
	assert.Equal(t, []string{"create", "get"}, client.calls)
}

func TestScheduleReadUsesARNIdentity(t *testing.T) {
	resource := baseSchedule()
	client := &fakeScheduleClient{}

	output, err := resource.read(
		context.Background(), client, &ScheduleResourceOutput{ARN: testScheduleARN})

	require.NoError(t, err)
	assert.Equal(t, testScheduleARN, output.ARN)
	require.Len(t, client.getInputs, 1)
	assert.Equal(t, "default", aws.ToString(client.getInputs[0].GroupName))
	assert.Equal(t, "daily", aws.ToString(client.getInputs[0].Name))
}

func TestScheduleReadMapsOnlyTypedNotFound(t *testing.T) {
	resource := baseSchedule()
	typed := fmt.Errorf("wrapped: %w", &schedulertypes.ResourceNotFoundException{
		Message: aws.String("gone"),
	})
	client := &fakeScheduleClient{getErrors: []error{typed}}

	_, err := resource.read(
		context.Background(), client, &ScheduleResourceOutput{ARN: testScheduleARN})

	assert.ErrorIs(t, err, runtime.ErrNotFound)

	client = &fakeScheduleClient{
		getErrors: []error{errors.New("ResourceNotFoundException: gone")},
	}
	_, err = resource.read(
		context.Background(), client, &ScheduleResourceOutput{ARN: testScheduleARN})
	require.Error(t, err)
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
}

func TestScheduleReadRejectsMalformedResults(t *testing.T) {
	tests := []struct {
		name   string
		prior  *ScheduleResourceOutput
		output *schedulerapi.GetScheduleOutput
		nilOut bool
		match  string
		calls  int
	}{
		{
			name:  "nil prior",
			match: runtime.ErrNotFound.Error(),
		},
		{
			name:  "malformed prior ARN",
			prior: &ScheduleResourceOutput{ARN: "bad"},
			match: "parse prior",
		},
		{
			name:   "nil output",
			prior:  &ScheduleResourceOutput{ARN: testScheduleARN},
			nilOut: true,
			match:  "empty result",
			calls:  1,
		},
		{
			name:   "empty returned ARN",
			prior:  &ScheduleResourceOutput{ARN: testScheduleARN},
			output: &schedulerapi.GetScheduleOutput{},
			match:  "empty result",
			calls:  1,
		},
		{
			name:  "malformed returned ARN",
			prior: &ScheduleResourceOutput{ARN: testScheduleARN},
			output: &schedulerapi.GetScheduleOutput{
				Arn: aws.String("bad"),
			},
			match: "parse returned",
			calls: 1,
		},
		{
			name:  "wrong returned account",
			prior: &ScheduleResourceOutput{ARN: testScheduleARN},
			output: scheduleGetOutput(
				"arn:aws:scheduler:us-east-1:999999999999:schedule/default/daily"),
			match: "requested ARN",
			calls: 1,
		},
		{
			name:  "wrong returned group field",
			prior: &ScheduleResourceOutput{ARN: testScheduleARN},
			output: &schedulerapi.GetScheduleOutput{
				Arn:       aws.String(testScheduleARN),
				GroupName: aws.String("other"),
			},
			match: "returned group",
			calls: 1,
		},
		{
			name:  "wrong returned name field",
			prior: &ScheduleResourceOutput{ARN: testScheduleARN},
			output: &schedulerapi.GetScheduleOutput{
				Arn:  aws.String(testScheduleARN),
				Name: aws.String("other"),
			},
			match: "returned name",
			calls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeScheduleClient{
				getOutputs: []*schedulerapi.GetScheduleOutput{tt.output},
				getNil:     tt.nilOut,
			}
			resource := baseSchedule()
			_, err := resource.read(context.Background(), client, tt.prior)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.match)
			assert.Len(t, client.getInputs, tt.calls)
		})
	}
}

func TestScheduleUpdateReadsWithoutUpdateWhenInputsAreUnchanged(t *testing.T) {
	resource := baseSchedule()
	client := &fakeScheduleClient{}

	output, err := resource.update(context.Background(), client, runtime.Prior[
		ScheduleResource, *ScheduleResourceOutput,
		*awsCfg]{
		Inputs:  resource,
		Outputs: &ScheduleResourceOutput{ARN: testScheduleARN},
	})

	require.NoError(t, err)
	assert.Equal(t, testScheduleARN, output.ARN)
	assert.Equal(t, []string{"get"}, client.calls)
}

func TestScheduleUpdateCallsAWSForEveryMutableInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ScheduleResource)
	}{
		{
			name: "action after completion",
			mutate: func(r *ScheduleResource) {
				r.ActionAfterCompletion = aws.String("DELETE")
			},
		},
		{
			name: "description",
			mutate: func(r *ScheduleResource) {
				r.Description = aws.String("updated")
			},
		},
		{
			name: "end date",
			mutate: func(r *ScheduleResource) {
				r.EndDate = aws.String("2026-07-18T10:00:00Z")
			},
		},
		{
			name: "flexible window",
			mutate: func(r *ScheduleResource) {
				r.FlexibleTimeWindow = ScheduleFlexibleTimeWindow{
					Mode:                   "FLEXIBLE",
					MaximumWindowInMinutes: aws.Int64(15),
				}
			},
		},
		{
			name: "kms key",
			mutate: func(r *ScheduleResource) {
				r.KMSKeyARN = aws.String(
					"arn:aws:kms:us-east-1:123456789012:key/1234")
			},
		},
		{
			name: "schedule expression",
			mutate: func(r *ScheduleResource) {
				r.ScheduleExpression = "rate(2 days)"
			},
		},
		{
			name: "timezone",
			mutate: func(r *ScheduleResource) {
				r.ScheduleExpressionTimezone = aws.String("UTC")
			},
		},
		{
			name: "start date",
			mutate: func(r *ScheduleResource) {
				r.StartDate = aws.String("2026-07-17T10:00:00Z")
			},
		},
		{
			name: "state",
			mutate: func(r *ScheduleResource) {
				r.State = aws.String("DISABLED")
			},
		},
		{
			name: "target",
			mutate: func(r *ScheduleResource) {
				r.Target.Input = aws.String(`{"updated":true}`)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prior := baseSchedule()
			current := prior
			tt.mutate(&current)
			client := &fakeScheduleClient{}
			_, err := current.update(context.Background(), client, runtime.Prior[
				ScheduleResource, *ScheduleResourceOutput,
				*awsCfg]{
				Inputs:  prior,
				Outputs: &ScheduleResourceOutput{ARN: testScheduleARN},
			})
			require.NoError(t, err)
			assert.Equal(t, []string{"update", "get"}, client.calls)
		})
	}
}

func TestScheduleUpdateSendsCurrentSnapshotAndClearsRemovedOptionals(t *testing.T) {
	prior := fullSchedule()
	current := baseSchedule()
	client := &fakeScheduleClient{
		getOutputs: []*schedulerapi.GetScheduleOutput{scheduleGetOutput(teamScheduleARN)},
	}

	_, err := current.update(context.Background(), client, runtime.Prior[
		ScheduleResource, *ScheduleResourceOutput,
		*awsCfg]{
		Inputs:  prior,
		Outputs: &ScheduleResourceOutput{ARN: teamScheduleARN},
	})

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	in := client.updateInputs[0]
	assert.Equal(t, "team", aws.ToString(in.GroupName))
	assert.Equal(t, "daily", aws.ToString(in.Name))
	assert.Empty(t, in.ActionAfterCompletion)
	assert.Nil(t, in.Description)
	assert.Nil(t, in.EndDate)
	assert.Nil(t, in.KmsKeyArn)
	assert.Nil(t, in.ScheduleExpressionTimezone)
	assert.Nil(t, in.StartDate)
	assert.Empty(t, in.State)
	assert.Nil(t, in.ClientToken)
	assert.Nil(t, in.Target.DeadLetterConfig)
	assert.Nil(t, in.Target.EcsParameters)
	assert.Nil(t, in.Target.EventBridgeParameters)
	assert.Nil(t, in.Target.Input)
	assert.Nil(t, in.Target.KinesisParameters)
	assert.Nil(t, in.Target.RetryPolicy)
	assert.Nil(t, in.Target.SageMakerPipelineParameters)
	assert.Nil(t, in.Target.SqsParameters)
}

func TestScheduleUpdateClearsMaximumFromRetainedFlexibleWindow(t *testing.T) {
	prior := baseSchedule()
	prior.FlexibleTimeWindow = ScheduleFlexibleTimeWindow{
		Mode:                   "FLEXIBLE",
		MaximumWindowInMinutes: aws.Int64(30),
	}
	current := baseSchedule()

	in := scheduleUpdateRequest(t, prior, current)

	require.NotNil(t, in.FlexibleTimeWindow)
	assert.Equal(t, schedulertypes.FlexibleTimeWindowModeOff,
		in.FlexibleTimeWindow.Mode)
	assert.Nil(t, in.FlexibleTimeWindow.MaximumWindowInMinutes)
}

func TestScheduleUpdateClearsMembersFromRetainedRetryPolicy(t *testing.T) {
	prior := baseSchedule()
	prior.Target.RetryPolicy = &ScheduleTargetRetryPolicy{
		MaximumEventAgeInSeconds: aws.Int64(3600),
		MaximumRetryAttempts:     aws.Int64(4),
	}
	current := baseSchedule()
	current.Target.RetryPolicy = &ScheduleTargetRetryPolicy{}

	in := scheduleUpdateRequest(t, prior, current)

	require.NotNil(t, in.Target.RetryPolicy)
	assert.Nil(t, in.Target.RetryPolicy.MaximumEventAgeInSeconds)
	assert.Nil(t, in.Target.RetryPolicy.MaximumRetryAttempts)
}

func TestScheduleUpdateClearsMembersFromRetainedECSParameters(t *testing.T) {
	securityGroups := []string{"sg-prior"}
	placementStrategy := []ScheduleTargetECSPlacementStrategy{{
		Type:  "spread",
		Field: aws.String("attribute:ecs.availability-zone"),
	}}
	tags := map[string]string{"owner": "prior"}
	prior := baseSchedule()
	prior.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN:    testTaskDefinitionARN,
		TaskCount:            aws.Int64(2),
		LaunchType:           aws.String("FARGATE"),
		PlatformVersion:      aws.String("1.4.0"),
		Group:                aws.String("prior-group"),
		EnableECSManagedTags: aws.Bool(true),
		EnableExecuteCommand: aws.Bool(true),
		NetworkConfiguration: &ScheduleTargetECSNetworkConfiguration{
			Subnets:        []string{"subnet-prior"},
			SecurityGroups: &securityGroups,
			AssignPublicIP: aws.Bool(true),
		},
		PlacementStrategy: &placementStrategy,
		PropagateTags:     aws.String("TASK_DEFINITION"),
		ReferenceID:       aws.String("prior-reference"),
		Tags:              &tags,
	}
	current := baseSchedule()
	current.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN: testTaskDefinitionARN,
	}

	in := scheduleUpdateRequest(t, prior, current)

	require.NotNil(t, in.Target.EcsParameters)
	ecs := in.Target.EcsParameters
	assert.Equal(t, testTaskDefinitionARN, aws.ToString(ecs.TaskDefinitionArn))
	assert.Nil(t, ecs.TaskCount)
	assert.Empty(t, ecs.LaunchType)
	assert.Nil(t, ecs.NetworkConfiguration)
	assert.Nil(t, ecs.PlatformVersion)
	assert.Nil(t, ecs.Group)
	assert.Nil(t, ecs.EnableECSManagedTags)
	assert.Nil(t, ecs.EnableExecuteCommand)
	assert.Nil(t, ecs.PlacementStrategy)
	assert.Empty(t, ecs.PropagateTags)
	assert.Nil(t, ecs.ReferenceId)
	assert.Nil(t, ecs.Tags)
}

func TestScheduleUpdateClearsCollectionsFromRetainedECSParameters(t *testing.T) {
	capacity := []ScheduleECSCapacityProviderStrategy{{
		CapacityProvider: "prior-capacity",
		Base:             aws.Int64(2),
		Weight:           aws.Int64(3),
	}}
	constraints := []ScheduleTargetECSPlacementConstraint{{
		Type:       "memberOf",
		Expression: aws.String("attribute:ecs.instance-type == t3.small"),
	}}
	strategy := []ScheduleTargetECSPlacementStrategy{{
		Type:  "spread",
		Field: aws.String("attribute:ecs.availability-zone"),
	}}
	prior := baseSchedule()
	prior.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN:        testTaskDefinitionARN,
		CapacityProviderStrategy: &capacity,
		PlacementConstraints:     &constraints,
		PlacementStrategy:        &strategy,
	}
	current := baseSchedule()
	current.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN: testTaskDefinitionARN,
	}

	in := scheduleUpdateRequest(t, prior, current)

	require.NotNil(t, in.Target.EcsParameters)
	assert.Nil(t, in.Target.EcsParameters.CapacityProviderStrategy)
	assert.Nil(t, in.Target.EcsParameters.PlacementConstraints)
	assert.Nil(t, in.Target.EcsParameters.PlacementStrategy)
}

func TestScheduleUpdateClearsMembersFromRetainedECSNetwork(t *testing.T) {
	securityGroups := []string{"sg-prior"}
	prior := baseSchedule()
	prior.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN: testTaskDefinitionARN,
		LaunchType:        aws.String("FARGATE"),
		NetworkConfiguration: &ScheduleTargetECSNetworkConfiguration{
			Subnets:        []string{"subnet-current"},
			SecurityGroups: &securityGroups,
			AssignPublicIP: aws.Bool(true),
		},
	}
	current := baseSchedule()
	current.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN: testTaskDefinitionARN,
		LaunchType:        aws.String("FARGATE"),
		NetworkConfiguration: &ScheduleTargetECSNetworkConfiguration{
			Subnets: []string{"subnet-current"},
		},
	}

	in := scheduleUpdateRequest(t, prior, current)

	require.NotNil(t, in.Target.EcsParameters)
	require.NotNil(t, in.Target.EcsParameters.NetworkConfiguration)
	network := in.Target.EcsParameters.NetworkConfiguration.AwsvpcConfiguration
	require.NotNil(t, network)
	assert.Equal(t, []string{"subnet-current"}, network.Subnets)
	assert.Empty(t, network.AssignPublicIp)
	assert.Nil(t, network.SecurityGroups)
}

func TestScheduleUpdateClearsMembersFromRetainedECSCapacityItem(t *testing.T) {
	priorCapacity := []ScheduleECSCapacityProviderStrategy{
		{
			CapacityProvider: "primary",
			Base:             aws.Int64(2),
			Weight:           aws.Int64(3),
		},
		{
			CapacityProvider: "secondary",
			Weight:           aws.Int64(1),
		},
	}
	currentCapacity := []ScheduleECSCapacityProviderStrategy{
		{CapacityProvider: "primary"},
		{
			CapacityProvider: "secondary",
			Weight:           aws.Int64(1),
		},
	}
	prior := baseSchedule()
	prior.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN:        testTaskDefinitionARN,
		CapacityProviderStrategy: &priorCapacity,
	}
	current := baseSchedule()
	current.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN:        testTaskDefinitionARN,
		CapacityProviderStrategy: &currentCapacity,
	}

	in := scheduleUpdateRequest(t, prior, current)

	require.Len(t, in.Target.EcsParameters.CapacityProviderStrategy, 2)
	assert.Zero(t, in.Target.EcsParameters.CapacityProviderStrategy[0].Base)
	assert.Zero(t, in.Target.EcsParameters.CapacityProviderStrategy[0].Weight)
	assert.Equal(t, int32(1),
		in.Target.EcsParameters.CapacityProviderStrategy[1].Weight)
}

func TestScheduleUpdateClearsMembersFromRetainedECSPlacementItems(t *testing.T) {
	priorConstraints := []ScheduleTargetECSPlacementConstraint{{
		Type:       "memberOf",
		Expression: aws.String("attribute:ecs.instance-type == t3.small"),
	}}
	currentConstraints := []ScheduleTargetECSPlacementConstraint{{
		Type: "distinctInstance",
	}}
	priorStrategy := []ScheduleTargetECSPlacementStrategy{{
		Type:  "spread",
		Field: aws.String("attribute:ecs.availability-zone"),
	}}
	currentStrategy := []ScheduleTargetECSPlacementStrategy{{
		Type: "random",
	}}
	prior := baseSchedule()
	prior.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN:    testTaskDefinitionARN,
		PlacementConstraints: &priorConstraints,
		PlacementStrategy:    &priorStrategy,
	}
	current := baseSchedule()
	current.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN:    testTaskDefinitionARN,
		PlacementConstraints: &currentConstraints,
		PlacementStrategy:    &currentStrategy,
	}

	in := scheduleUpdateRequest(t, prior, current)

	require.Len(t, in.Target.EcsParameters.PlacementConstraints, 1)
	assert.Equal(t, schedulertypes.PlacementConstraintTypeDistinctInstance,
		in.Target.EcsParameters.PlacementConstraints[0].Type)
	assert.Nil(t, in.Target.EcsParameters.PlacementConstraints[0].Expression)
	require.Len(t, in.Target.EcsParameters.PlacementStrategy, 1)
	assert.Equal(t, schedulertypes.PlacementStrategyTypeRandom,
		in.Target.EcsParameters.PlacementStrategy[0].Type)
	assert.Nil(t, in.Target.EcsParameters.PlacementStrategy[0].Field)
}

func TestScheduleUpdateClearsListFromRetainedSageMakerParameters(t *testing.T) {
	parameters := []ScheduleTargetSageMakerPipelineParameter{{
		Name:  "PriorName",
		Value: "prior-value",
	}}
	prior := baseSchedule()
	prior.Target.SageMakerPipelineParameters = &ScheduleSageMakerPipelineParams{
		PipelineParameterList: &parameters,
	}
	current := baseSchedule()
	current.Target.SageMakerPipelineParameters = &ScheduleSageMakerPipelineParams{}

	in := scheduleUpdateRequest(t, prior, current)

	require.NotNil(t, in.Target.SageMakerPipelineParameters)
	assert.Nil(t, in.Target.SageMakerPipelineParameters.PipelineParameterList)
}

func TestScheduleUpdateClearsMemberFromRetainedSQSParameters(t *testing.T) {
	prior := baseSchedule()
	prior.Target.SQSParameters = &ScheduleTargetSQSParameters{
		MessageGroupID: aws.String("prior-group"),
	}
	current := baseSchedule()
	current.Target.SQSParameters = &ScheduleTargetSQSParameters{}

	in := scheduleUpdateRequest(t, prior, current)

	require.NotNil(t, in.Target.SqsParameters)
	assert.Nil(t, in.Target.SqsParameters.MessageGroupId)
}

func TestScheduleUpdateSendsExplicitEmptyDescription(t *testing.T) {
	prior := baseSchedule()
	current := prior
	current.Description = aws.String("")
	client := &fakeScheduleClient{}

	_, err := current.update(context.Background(), client, runtime.Prior[
		ScheduleResource, *ScheduleResourceOutput,
		*awsCfg]{
		Inputs:  prior,
		Outputs: &ScheduleResourceOutput{ARN: testScheduleARN},
	})

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	require.NotNil(t, client.updateInputs[0].Description)
	assert.Empty(t, *client.updateInputs[0].Description)
}

func TestScheduleCreateAndUpdateRetryOnlyIAMPropagationError(t *testing.T) {
	propagation := &schedulertypes.ValidationException{
		Message: aws.String(iamPropagationMessage),
	}
	resource := baseSchedule()
	createClient := &fakeScheduleClient{createErrors: []error{propagation, nil}}

	_, err := resource.create(
		context.Background(),
		createClient,
		bytes.NewReader(nil),
		retry.WithInterval(0),
		retry.WithTimeout(time.Second),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"create", "create", "get"}, createClient.calls)

	prior := resource
	resource.Description = aws.String("updated")
	updateClient := &fakeScheduleClient{updateErrors: []error{propagation, nil}}
	_, err = resource.update(context.Background(), updateClient, runtime.Prior[
		ScheduleResource, *ScheduleResourceOutput,
		*awsCfg]{
		Inputs:  prior,
		Outputs: &ScheduleResourceOutput{ARN: testScheduleARN},
	}, retry.WithInterval(0), retry.WithTimeout(time.Second))
	require.NoError(t, err)
	assert.Equal(t, []string{"update", "update", "get"}, updateClient.calls)
}

func TestScheduleRetryPreservesErrorsAndCancellation(t *testing.T) {
	propagation := &schedulertypes.ValidationException{
		Message: aws.String(iamPropagationMessage),
	}
	resource := baseSchedule()

	client := &fakeScheduleClient{createErrors: []error{propagation}}
	_, err := resource.create(
		context.Background(),
		client,
		bytes.NewReader(nil),
		retry.WithTimeout(0),
		retry.WithInterval(0),
	)
	assert.ErrorIs(t, err, propagation)
	assert.Len(t, client.createInputs, 1)

	other := &schedulertypes.ValidationException{Message: aws.String("other")}
	client = &fakeScheduleClient{createErrors: []error{other, nil}}
	_, err = resource.create(
		context.Background(),
		client,
		bytes.NewReader(nil),
		retry.WithTimeout(time.Second),
		retry.WithInterval(0),
	)
	assert.ErrorIs(t, err, other)
	assert.Len(t, client.createInputs, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client = &fakeScheduleClient{createErrors: []error{propagation}}
	_, err = resource.create(
		ctx,
		client,
		bytes.NewReader(nil),
		retry.WithTimeout(time.Minute),
		retry.WithInterval(time.Minute),
	)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Len(t, client.createInputs, 1)
}

func TestScheduleDeleteUsesARNIdentityAndAcceptsTypedNotFound(t *testing.T) {
	resource := baseSchedule()
	client := &fakeScheduleClient{}

	err := resource.delete(
		context.Background(), client, &ScheduleResourceOutput{ARN: teamScheduleARN})

	require.NoError(t, err)
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, "team", aws.ToString(client.deleteInputs[0].GroupName))
	assert.Equal(t, "daily", aws.ToString(client.deleteInputs[0].Name))

	client = &fakeScheduleClient{
		deleteErrors: []error{fmt.Errorf(
			"wrapped: %w", &schedulertypes.ResourceNotFoundException{})},
	}
	err = resource.delete(
		context.Background(), client, &ScheduleResourceOutput{ARN: teamScheduleARN})
	require.NoError(t, err)

	plain := errors.New("ResourceNotFoundException: gone")
	client = &fakeScheduleClient{deleteErrors: []error{plain}}
	err = resource.delete(
		context.Background(), client, &ScheduleResourceOutput{ARN: teamScheduleARN})
	assert.ErrorIs(t, err, plain)
}

func TestScheduleDeleteRejectsMalformedARNBeforeCallingAWS(t *testing.T) {
	client := &fakeScheduleClient{}
	resource := baseSchedule()

	err := resource.delete(
		context.Background(), client, &ScheduleResourceOutput{ARN: "bad"})

	require.Error(t, err)
	assert.Empty(t, client.calls)
}

func TestScheduleARNParsing(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "valid", value: testScheduleARN, valid: true},
		{name: "malformed", value: "bad"},
		{
			name:  "empty partition",
			value: "arn::scheduler:us-east-1:123456789012:schedule/default/daily",
		},
		{
			name:  "wrong service",
			value: "arn:aws:events:us-east-1:123456789012:schedule/default/daily",
		},
		{
			name:  "empty region",
			value: "arn:aws:scheduler::123456789012:schedule/default/daily",
		},
		{
			name:  "empty account",
			value: "arn:aws:scheduler:us-east-1::schedule/default/daily",
		},
		{
			name:  "wrong resource kind",
			value: "arn:aws:scheduler:us-east-1:123456789012:group/default/daily",
		},
		{
			name:  "missing group",
			value: "arn:aws:scheduler:us-east-1:123456789012:schedule//daily",
		},
		{
			name:  "missing name",
			value: "arn:aws:scheduler:us-east-1:123456789012:schedule/default/",
		},
		{
			name:  "extra component",
			value: "arn:aws:scheduler:us-east-1:123456789012:schedule/default/daily/x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity, err := parseScheduleARN(tt.value)
			if tt.valid {
				require.NoError(t, err)
				assert.Equal(t, "default", identity.group)
				assert.Equal(t, "daily", identity.name)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestIAMPropagationPredicate(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "exact message",
			err: &schedulertypes.ValidationException{
				Message: aws.String(iamPropagationMessage),
			},
			want: true,
		},
		{
			name: "wrapped exact message",
			err: fmt.Errorf("create: %w", &schedulertypes.ValidationException{
				Message: aws.String("prefix: " + iamPropagationMessage),
			}),
			want: true,
		},
		{
			name: "wrong case",
			err: &schedulertypes.ValidationException{
				Message: aws.String(strings.ToLower(iamPropagationMessage)),
			},
		},
		{
			name: "wrong validation message",
			err: &schedulertypes.ValidationException{
				Message: aws.String("other"),
			},
		},
		{
			name: "plain error",
			err:  errors.New(iamPropagationMessage),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isIAMPropagationError(tt.err))
		})
	}
}

func fullSchedule() ScheduleResource {
	securityGroups := []string{"sg-a"}
	placementStrategy := []ScheduleTargetECSPlacementStrategy{{
		Type:  "spread",
		Field: aws.String("attribute:ecs.availability-zone"),
	}}
	tags := map[string]string{
		"owner": "platform",
		"empty": "",
	}
	return ScheduleResource{
		ActionAfterCompletion: aws.String("DELETE"),
		Description:           aws.String(""),
		EndDate:               aws.String("2026-07-18T10:00:00Z"),
		FlexibleTimeWindow: ScheduleFlexibleTimeWindow{
			Mode:                   "FLEXIBLE",
			MaximumWindowInMinutes: aws.Int64(30),
		},
		GroupName:                  aws.String("team"),
		KMSKeyARN:                  aws.String("arn:aws:kms:us-east-1:123456789012:key/1234"),
		Name:                       aws.String("daily"),
		ScheduleExpression:         "cron(0 10 * * ? *)",
		ScheduleExpressionTimezone: aws.String("America/New_York"),
		StartDate:                  aws.String("2026-07-17T10:00:00Z"),
		State:                      aws.String("DISABLED"),
		Target: ScheduleTarget{
			ARN:     testTargetARN,
			RoleARN: testRoleARN,
			DeadLetterConfig: &ScheduleTargetDeadLetterConfig{
				ARN: "arn:aws:sqs:us-east-1:123456789012:schedule-dlq",
			},
			ECSParameters: &ScheduleTargetECSParameters{
				TaskDefinitionARN: "arn:aws:ecs:us-east-1:123456789012:" +
					"task-definition/job:1",
				TaskCount:            aws.Int64(2),
				LaunchType:           aws.String("FARGATE"),
				PlatformVersion:      aws.String("1.4.0"),
				Group:                aws.String("daily-jobs"),
				EnableECSManagedTags: aws.Bool(false),
				EnableExecuteCommand: aws.Bool(true),
				NetworkConfiguration: &ScheduleTargetECSNetworkConfiguration{
					Subnets:        []string{"subnet-a", "subnet-b"},
					SecurityGroups: &securityGroups,
					AssignPublicIP: aws.Bool(false),
				},
				PlacementStrategy: &placementStrategy,
				PropagateTags:     aws.String("TASK_DEFINITION"),
				ReferenceID:       aws.String("daily"),
				Tags:              &tags,
			},
			Input: aws.String(`{"job":"daily"}`),
			RetryPolicy: &ScheduleTargetRetryPolicy{
				MaximumEventAgeInSeconds: aws.Int64(3600),
				MaximumRetryAttempts:     aws.Int64(4),
			},
		},
	}
}

func scheduleGetOutput(arn string) *schedulerapi.GetScheduleOutput {
	identity, err := parseScheduleARN(arn)
	if err != nil {
		panic(err)
	}
	return &schedulerapi.GetScheduleOutput{
		Arn:       aws.String(arn),
		GroupName: aws.String(identity.group),
		Name:      aws.String(identity.name),
	}
}

func scheduleUpdateRequest(
	t *testing.T,
	prior ScheduleResource,
	current ScheduleResource,
) *schedulerapi.UpdateScheduleInput {
	t.Helper()
	getOutput := scheduleGetOutput(teamScheduleARN)
	getOutput.FlexibleTimeWindow = prior.FlexibleTimeWindow.to()
	getOutput.Target = prior.Target.to()
	client := &fakeScheduleClient{
		getOutputs: []*schedulerapi.GetScheduleOutput{getOutput},
	}

	_, err := current.update(context.Background(), client, runtime.Prior[
		ScheduleResource, *ScheduleResourceOutput,
		*awsCfg]{
		Inputs:  prior,
		Outputs: &ScheduleResourceOutput{ARN: teamScheduleARN},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"update", "get"}, client.calls)
	require.Len(t, client.updateInputs, 1)
	return client.updateInputs[0]
}
