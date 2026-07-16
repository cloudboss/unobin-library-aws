package scheduler

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleValidateInputsAcceptsCompleteBoundaries(t *testing.T) {
	resource := fullSchedule()

	require.NoError(t, resource.ValidateInputs(context.Background(), nil))

	resource = validCapacitySchedule()
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestScheduleValidateInputsRejectsInvalidTopLevelAndTargetValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ScheduleResource)
		match  string
	}{
		{
			name: "empty name",
			mutate: func(r *ScheduleResource) {
				r.Name = aws.String("")
			},
			match: "name",
		},
		{
			name: "long name",
			mutate: func(r *ScheduleResource) {
				r.Name = aws.String(strings.Repeat("n", 65))
			},
			match: "name",
		},
		{
			name: "name characters",
			mutate: func(r *ScheduleResource) {
				r.Name = aws.String("daily/job")
			},
			match: "name",
		},
		{
			name: "empty group",
			mutate: func(r *ScheduleResource) {
				r.GroupName = aws.String("")
			},
			match: "group-name",
		},
		{
			name: "group characters",
			mutate: func(r *ScheduleResource) {
				r.GroupName = aws.String("team/job")
			},
			match: "group-name",
		},
		{
			name: "action enum",
			mutate: func(r *ScheduleResource) {
				r.ActionAfterCompletion = aws.String("ARCHIVE")
			},
			match: "action-after-completion",
		},
		{
			name: "long description",
			mutate: func(r *ScheduleResource) {
				r.Description = aws.String(strings.Repeat("d", 513))
			},
			match: "description",
		},
		{
			name: "window mode",
			mutate: func(r *ScheduleResource) {
				r.FlexibleTimeWindow.Mode = "OTHER"
			},
			match: "mode",
		},
		{
			name: "window minimum",
			mutate: func(r *ScheduleResource) {
				r.FlexibleTimeWindow.MaximumWindowInMinutes = aws.Int64(0)
			},
			match: "maximum-window",
		},
		{
			name: "window maximum",
			mutate: func(r *ScheduleResource) {
				r.FlexibleTimeWindow.MaximumWindowInMinutes = aws.Int64(1441)
			},
			match: "maximum-window",
		},
		{
			name: "flexible window maximum required",
			mutate: func(r *ScheduleResource) {
				r.FlexibleTimeWindow = ScheduleFlexibleTimeWindow{Mode: "FLEXIBLE"}
			},
			match: "requires maximum",
		},
		{
			name: "empty expression",
			mutate: func(r *ScheduleResource) {
				r.ScheduleExpression = ""
			},
			match: "schedule-expression",
		},
		{
			name: "long expression",
			mutate: func(r *ScheduleResource) {
				r.ScheduleExpression = strings.Repeat("e", 257)
			},
			match: "schedule-expression",
		},
		{
			name: "empty timezone",
			mutate: func(r *ScheduleResource) {
				r.ScheduleExpressionTimezone = aws.String("")
			},
			match: "timezone",
		},
		{
			name: "long timezone",
			mutate: func(r *ScheduleResource) {
				r.ScheduleExpressionTimezone = aws.String(strings.Repeat("z", 51))
			},
			match: "timezone",
		},
		{
			name: "state enum",
			mutate: func(r *ScheduleResource) {
				r.State = aws.String("PAUSED")
			},
			match: "state",
		},
		{
			name: "kms key ARN",
			mutate: func(r *ScheduleResource) {
				r.KMSKeyARN = aws.String("arn:aws:s3:::bucket")
			},
			match: "kms-key-arn",
		},
		{
			name: "start date",
			mutate: func(r *ScheduleResource) {
				r.StartDate = aws.String("tomorrow")
			},
			match: "start-date",
		},
		{
			name: "end date",
			mutate: func(r *ScheduleResource) {
				r.EndDate = aws.String("tomorrow")
			},
			match: "end-date",
		},
		{
			name: "target ARN",
			mutate: func(r *ScheduleResource) {
				r.Target.ARN = "not-an-arn"
			},
			match: "target arn",
		},
		{
			name: "long target ARN",
			mutate: func(r *ScheduleResource) {
				r.Target.ARN = "arn:aws:lambda:us-east-1:123456789012:function:" +
					strings.Repeat("x", 1600)
			},
			match: "target arn",
		},
		{
			name: "target role ARN",
			mutate: func(r *ScheduleResource) {
				r.Target.RoleARN = "arn:aws:iam::123456789012:user/not-a-role"
			},
			match: "role-arn",
		},
		{
			name: "empty input",
			mutate: func(r *ScheduleResource) {
				r.Target.Input = aws.String("")
			},
			match: "input",
		},
		{
			name: "multiple templated blocks",
			mutate: func(r *ScheduleResource) {
				r.Target.EventBridgeParameters = &ScheduleEventBridgeParameters{
					DetailType: "job",
					Source:     "unobin.scheduler",
				}
				r.Target.KinesisParameters = &ScheduleTargetKinesisParameters{
					PartitionKey: "job",
				}
			},
			match: "at most one",
		},
		{
			name: "dead letter queue ARN",
			mutate: func(r *ScheduleResource) {
				r.Target.DeadLetterConfig = &ScheduleTargetDeadLetterConfig{
					ARN: "arn:aws:sns:us-east-1:123456789012:topic",
				}
			},
			match: "dead-letter-config",
		},
		{
			name: "retry age minimum",
			mutate: func(r *ScheduleResource) {
				r.Target.RetryPolicy = &ScheduleTargetRetryPolicy{
					MaximumEventAgeInSeconds: aws.Int64(59),
				}
			},
			match: "maximum-event-age",
		},
		{
			name: "retry age maximum",
			mutate: func(r *ScheduleResource) {
				r.Target.RetryPolicy = &ScheduleTargetRetryPolicy{
					MaximumEventAgeInSeconds: aws.Int64(86401),
				}
			},
			match: "maximum-event-age",
		},
		{
			name: "retry attempts minimum",
			mutate: func(r *ScheduleResource) {
				r.Target.RetryPolicy = &ScheduleTargetRetryPolicy{
					MaximumRetryAttempts: aws.Int64(-1),
				}
			},
			match: "maximum-retry-attempts",
		},
		{
			name: "retry attempts maximum",
			mutate: func(r *ScheduleResource) {
				r.Target.RetryPolicy = &ScheduleTargetRetryPolicy{
					MaximumRetryAttempts: aws.Int64(186),
				}
			},
			match: "maximum-retry-attempts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := baseSchedule()
			tt.mutate(&resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.match)
		})
	}
}

func TestScheduleValidateInputsRejectsInvalidECSValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ScheduleTargetECSParameters)
		match  string
	}{
		{
			name: "task definition required",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.TaskDefinitionARN = ""
			},
			match: "task-definition-arn",
		},
		{
			name: "task count minimum",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.TaskCount = aws.Int64(0)
			},
			match: "task-count",
		},
		{
			name: "task count maximum",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.TaskCount = aws.Int64(11)
			},
			match: "task-count",
		},
		{
			name: "launch type enum",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = aws.String("OTHER")
			},
			match: "launch-type",
		},
		{
			name: "capacity conflicts with launch",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := []ScheduleECSCapacityProviderStrategy{{
					CapacityProvider: "FARGATE",
					Weight:           aws.Int64(1),
				}}
				p.CapacityProviderStrategy = &values
			},
			match: "conflicts",
		},
		{
			name: "too many capacity providers",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = nil
				values := make([]ScheduleECSCapacityProviderStrategy, 7)
				for i := range values {
					values[i] = ScheduleECSCapacityProviderStrategy{
						CapacityProvider: fmt.Sprintf("provider-%d", i),
						Weight:           aws.Int64(1),
					}
				}
				p.CapacityProviderStrategy = &values
			},
			match: "at most 6",
		},
		{
			name: "empty capacity provider",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = nil
				values := []ScheduleECSCapacityProviderStrategy{{
					Weight: aws.Int64(1),
				}}
				p.CapacityProviderStrategy = &values
			},
			match: "capacity-provider",
		},
		{
			name: "capacity base minimum",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = nil
				values := []ScheduleECSCapacityProviderStrategy{{
					CapacityProvider: "provider",
					Base:             aws.Int64(-1),
					Weight:           aws.Int64(1),
				}}
				p.CapacityProviderStrategy = &values
			},
			match: "base",
		},
		{
			name: "capacity base maximum",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = nil
				values := []ScheduleECSCapacityProviderStrategy{{
					CapacityProvider: "provider",
					Base:             aws.Int64(100001),
					Weight:           aws.Int64(1),
				}}
				p.CapacityProviderStrategy = &values
			},
			match: "base",
		},
		{
			name: "capacity weight minimum",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = nil
				values := []ScheduleECSCapacityProviderStrategy{{
					CapacityProvider: "provider",
					Weight:           aws.Int64(-1),
				}}
				p.CapacityProviderStrategy = &values
			},
			match: "weight",
		},
		{
			name: "capacity weight maximum",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = nil
				values := []ScheduleECSCapacityProviderStrategy{{
					CapacityProvider: "provider",
					Weight:           aws.Int64(1001),
				}}
				p.CapacityProviderStrategy = &values
			},
			match: "weight",
		},
		{
			name: "multiple positive bases",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = nil
				values := []ScheduleECSCapacityProviderStrategy{
					{CapacityProvider: "one", Base: aws.Int64(1), Weight: aws.Int64(1)},
					{CapacityProvider: "two", Base: aws.Int64(1), Weight: aws.Int64(1)},
				}
				p.CapacityProviderStrategy = &values
			},
			match: "at most one positive base",
		},
		{
			name: "all zero weights",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = nil
				values := []ScheduleECSCapacityProviderStrategy{{
					CapacityProvider: "provider",
					Weight:           aws.Int64(0),
				}}
				p.CapacityProviderStrategy = &values
			},
			match: "positive weight",
		},
		{
			name: "no subnets",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.NetworkConfiguration.Subnets = nil
			},
			match: "subnets",
		},
		{
			name: "too many subnets",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.NetworkConfiguration.Subnets = make([]string, 17)
				for i := range p.NetworkConfiguration.Subnets {
					p.NetworkConfiguration.Subnets[i] = fmt.Sprintf("subnet-%d", i)
				}
			},
			match: "subnets",
		},
		{
			name: "empty subnet",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.NetworkConfiguration.Subnets = []string{""}
			},
			match: "subnet",
		},
		{
			name: "empty security groups",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := []string{}
				p.NetworkConfiguration.SecurityGroups = &values
			},
			match: "security groups",
		},
		{
			name: "too many security groups",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := []string{"one", "two", "three", "four", "five", "six"}
				p.NetworkConfiguration.SecurityGroups = &values
			},
			match: "security groups",
		},
		{
			name: "empty security group",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := []string{""}
				p.NetworkConfiguration.SecurityGroups = &values
			},
			match: "security group",
		},
		{
			name: "public IP without FARGATE",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = aws.String("EC2")
				p.NetworkConfiguration.AssignPublicIP = aws.Bool(true)
			},
			match: "assign-public-ip",
		},
		{
			name: "FARGATE networking required",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.NetworkConfiguration = nil
			},
			match: "requires network",
		},
		{
			name: "FARGATE placement forbidden",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := []ScheduleTargetECSPlacementConstraint{{
					Type:       "memberOf",
					Expression: aws.String("attribute:ecs.instance-type == t3.small"),
				}}
				p.PlacementConstraints = &values
			},
			match: "forbids placement",
		},
		{
			name: "empty platform version",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.PlatformVersion = aws.String("")
			},
			match: "platform-version",
		},
		{
			name: "platform version without FARGATE",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = aws.String("EC2")
			},
			match: "platform-version requires FARGATE",
		},
		{
			name: "empty group",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.Group = aws.String("")
			},
			match: "ECS group",
		},
		{
			name: "long group",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.Group = aws.String(strings.Repeat("g", 256))
			},
			match: "ECS group",
		},
		{
			name: "too many placement constraints",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = aws.String("EC2")
				p.PlatformVersion = nil
				values := make([]ScheduleTargetECSPlacementConstraint, 11)
				p.PlacementConstraints = &values
			},
			match: "at most 10",
		},
		{
			name: "placement constraint type",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = aws.String("EC2")
				p.PlatformVersion = nil
				values := []ScheduleTargetECSPlacementConstraint{{Type: "other"}}
				p.PlacementConstraints = &values
			},
			match: "type",
		},
		{
			name: "memberOf expression required",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = aws.String("EC2")
				p.PlatformVersion = nil
				values := []ScheduleTargetECSPlacementConstraint{{Type: "memberOf"}}
				p.PlacementConstraints = &values
			},
			match: "requires an expression",
		},
		{
			name: "memberOf expression empty",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = aws.String("EC2")
				p.PlatformVersion = nil
				values := []ScheduleTargetECSPlacementConstraint{{
					Type:       "memberOf",
					Expression: aws.String(""),
				}}
				p.PlacementConstraints = &values
			},
			match: "expression",
		},
		{
			name: "distinctInstance expression forbidden",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.LaunchType = aws.String("EC2")
				p.PlatformVersion = nil
				values := []ScheduleTargetECSPlacementConstraint{{
					Type:       "distinctInstance",
					Expression: aws.String("ignored"),
				}}
				p.PlacementConstraints = &values
			},
			match: "forbids an expression",
		},
		{
			name: "too many placement strategies",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := make([]ScheduleTargetECSPlacementStrategy, 6)
				p.PlacementStrategy = &values
			},
			match: "at most 5",
		},
		{
			name: "placement strategy type",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := []ScheduleTargetECSPlacementStrategy{{Type: "other"}}
				p.PlacementStrategy = &values
			},
			match: "type",
		},
		{
			name: "placement strategy field length",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := []ScheduleTargetECSPlacementStrategy{{
					Type:  "spread",
					Field: aws.String(strings.Repeat("f", 256)),
				}}
				p.PlacementStrategy = &values
			},
			match: "field",
		},
		{
			name: "propagate tags enum",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.PropagateTags = aws.String("SERVICE")
			},
			match: "propagate-tags",
		},
		{
			name: "reference ID length",
			mutate: func(p *ScheduleTargetECSParameters) {
				p.ReferenceID = aws.String(strings.Repeat("r", 1025))
			},
			match: "reference-id",
		},
		{
			name: "too many tags",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := make(map[string]string, 51)
				for i := range 51 {
					values[fmt.Sprintf("tag-%d", i)] = "value"
				}
				p.Tags = &values
			},
			match: "at most 50",
		},
		{
			name: "empty tag key",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := map[string]string{"": "value"}
				p.Tags = &values
			},
			match: "tag key",
		},
		{
			name: "reserved tag prefix",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := map[string]string{"AWS:owner": "value"}
				p.Tags = &values
			},
			match: "aws:",
		},
		{
			name: "long tag value",
			mutate: func(p *ScheduleTargetECSParameters) {
				values := map[string]string{"owner": strings.Repeat("v", 257)}
				p.Tags = &values
			},
			match: "tag value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validECSchedule()
			tt.mutate(resource.Target.ECSParameters)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.match)
		})
	}
}

func TestScheduleValidateInputsRejectsInvalidSpecializedTargets(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ScheduleResource)
		match  string
	}{
		{
			name: "event detail required",
			mutate: func(r *ScheduleResource) {
				r.Target.EventBridgeParameters = &ScheduleEventBridgeParameters{
					Source: "unobin.scheduler",
				}
			},
			match: "detail-type",
		},
		{
			name: "event detail length",
			mutate: func(r *ScheduleResource) {
				r.Target.EventBridgeParameters = &ScheduleEventBridgeParameters{
					DetailType: strings.Repeat("d", 129),
					Source:     "unobin.scheduler",
				}
			},
			match: "detail-type",
		},
		{
			name: "event source required",
			mutate: func(r *ScheduleResource) {
				r.Target.EventBridgeParameters = &ScheduleEventBridgeParameters{
					DetailType: "job",
				}
			},
			match: "source",
		},
		{
			name: "event source reserved",
			mutate: func(r *ScheduleResource) {
				r.Target.EventBridgeParameters = &ScheduleEventBridgeParameters{
					DetailType: "job",
					Source:     "aws.scheduler",
				}
			},
			match: "source pattern",
		},
		{
			name: "event source characters",
			mutate: func(r *ScheduleResource) {
				r.Target.EventBridgeParameters = &ScheduleEventBridgeParameters{
					DetailType: "job",
					Source:     "unobin scheduler",
				}
			},
			match: "source pattern",
		},
		{
			name: "kinesis partition key required",
			mutate: func(r *ScheduleResource) {
				r.Target.KinesisParameters = &ScheduleTargetKinesisParameters{}
			},
			match: "partition-key",
		},
		{
			name: "kinesis partition key length",
			mutate: func(r *ScheduleResource) {
				r.Target.KinesisParameters = &ScheduleTargetKinesisParameters{
					PartitionKey: strings.Repeat("k", 257),
				}
			},
			match: "partition-key",
		},
		{
			name: "too many SageMaker parameters",
			mutate: func(r *ScheduleResource) {
				values := make([]ScheduleTargetSageMakerPipelineParameter, 201)
				r.Target.SageMakerPipelineParameters =
					&ScheduleSageMakerPipelineParams{
						PipelineParameterList: &values,
					}
			},
			match: "at most 200",
		},
		{
			name: "SageMaker parameter name pattern",
			mutate: func(r *ScheduleResource) {
				values := []ScheduleTargetSageMakerPipelineParameter{{
					Name:  "bad name",
					Value: "value",
				}}
				r.Target.SageMakerPipelineParameters =
					&ScheduleSageMakerPipelineParams{
						PipelineParameterList: &values,
					}
			},
			match: "parameter name",
		},
		{
			name: "SageMaker parameter value required",
			mutate: func(r *ScheduleResource) {
				values := []ScheduleTargetSageMakerPipelineParameter{{
					Name: "name",
				}}
				r.Target.SageMakerPipelineParameters =
					&ScheduleSageMakerPipelineParams{
						PipelineParameterList: &values,
					}
			},
			match: "parameter value",
		},
		{
			name: "SQS message group required when set",
			mutate: func(r *ScheduleResource) {
				r.Target.SQSParameters = &ScheduleTargetSQSParameters{
					MessageGroupID: aws.String(""),
				}
			},
			match: "message-group-id",
		},
		{
			name: "SQS message group length",
			mutate: func(r *ScheduleResource) {
				r.Target.SQSParameters = &ScheduleTargetSQSParameters{
					MessageGroupID: aws.String(strings.Repeat("g", 129)),
				}
			},
			match: "message-group-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := baseSchedule()
			tt.mutate(&resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.match)
		})
	}
}

func TestScheduleValidateInputsAcceptsEventBridgePathSource(t *testing.T) {
	resource := baseSchedule()
	resource.Target.EventBridgeParameters = &ScheduleEventBridgeParameters{
		DetailType: "job",
		Source:     "$.detail.sources[0]",
	}

	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestScheduleInvalidInputsMakeNoAPICalls(t *testing.T) {
	resource := baseSchedule()
	resource.ScheduleExpression = ""
	client := &fakeScheduleClient{}

	_, err := resource.create(context.Background(), client, strings.NewReader(""))

	require.Error(t, err)
	assert.Empty(t, client.calls)
}

func validECSchedule() ScheduleResource {
	resource := baseSchedule()
	securityGroups := []string{"sg-a"}
	resource.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN: "arn:aws:ecs:us-east-1:123456789012:" +
			"task-definition/job:1",
		TaskCount:            aws.Int64(1),
		LaunchType:           aws.String("FARGATE"),
		PlatformVersion:      aws.String("1.4.0"),
		Group:                aws.String("jobs"),
		PropagateTags:        aws.String("TASK_DEFINITION"),
		ReferenceID:          aws.String("job"),
		EnableExecuteCommand: aws.Bool(false),
		NetworkConfiguration: &ScheduleTargetECSNetworkConfiguration{
			Subnets:        []string{"subnet-a"},
			SecurityGroups: &securityGroups,
			AssignPublicIP: aws.Bool(false),
		},
	}
	return resource
}

func validCapacitySchedule() ScheduleResource {
	resource := baseSchedule()
	values := []ScheduleECSCapacityProviderStrategy{
		{
			CapacityProvider: "one",
			Base:             aws.Int64(1),
			Weight:           aws.Int64(0),
		},
		{
			CapacityProvider: "two",
			Base:             aws.Int64(0),
			Weight:           aws.Int64(1),
		},
	}
	resource.Target.ECSParameters = &ScheduleTargetECSParameters{
		TaskDefinitionARN:        "arn:aws:ecs:us-east-1:123456789012:task-definition/job:1",
		CapacityProviderStrategy: &values,
	}
	return resource
}
