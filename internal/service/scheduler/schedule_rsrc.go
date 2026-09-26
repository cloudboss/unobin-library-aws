package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	schedulerapi "github.com/aws/aws-sdk-go-v2/service/scheduler"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

type ScheduleFlexibleTimeWindow struct {
	Mode                   string `ub:"mode"`
	MaximumWindowInMinutes *int64 `ub:"maximum-window-in-minutes"`
}

type ScheduleTargetDeadLetterConfig struct {
	ARN string `ub:"arn"`
}

type ScheduleECSCapacityProviderStrategy struct {
	CapacityProvider string `ub:"capacity-provider"`
	Base             *int64 `ub:"base"`
	Weight           *int64 `ub:"weight"`
}

type ScheduleTargetECSNetworkConfiguration struct {
	Subnets        []string  `ub:"subnets"`
	SecurityGroups *[]string `ub:"security-groups"`
	AssignPublicIP *bool     `ub:"assign-public-ip"`
}

type ScheduleTargetECSPlacementConstraint struct {
	Type       string  `ub:"type"`
	Expression *string `ub:"expression"`
}

type ScheduleTargetECSPlacementStrategy struct {
	Type  string  `ub:"type"`
	Field *string `ub:"field"`
}

type ScheduleTargetECSParameters struct {
	TaskDefinitionARN        string                                  `ub:"task-definition-arn"`
	TaskCount                *int64                                  `ub:"task-count"`
	LaunchType               *string                                 `ub:"launch-type"`
	NetworkConfiguration     *ScheduleTargetECSNetworkConfiguration  `ub:"network-configuration"`
	PlatformVersion          *string                                 `ub:"platform-version"`
	Group                    *string                                 `ub:"group"`
	CapacityProviderStrategy *[]ScheduleECSCapacityProviderStrategy  `ub:"capacity-provider-strategy"`
	EnableECSManagedTags     *bool                                   `ub:"enable-ecs-managed-tags"`
	EnableExecuteCommand     *bool                                   `ub:"enable-execute-command"`
	PlacementConstraints     *[]ScheduleTargetECSPlacementConstraint `ub:"placement-constraints"`
	PlacementStrategy        *[]ScheduleTargetECSPlacementStrategy   `ub:"placement-strategy"`
	PropagateTags            *string                                 `ub:"propagate-tags"`
	ReferenceID              *string                                 `ub:"reference-id"`
	Tags                     *map[string]string                      `ub:"tags"`
}

type ScheduleEventBridgeParameters struct {
	DetailType string `ub:"detail-type"`
	Source     string `ub:"source"`
}

type ScheduleTargetKinesisParameters struct {
	PartitionKey string `ub:"partition-key"`
}

type ScheduleTargetRetryPolicy struct {
	MaximumEventAgeInSeconds *int64 `ub:"maximum-event-age-in-seconds"`
	MaximumRetryAttempts     *int64 `ub:"maximum-retry-attempts"`
}

type ScheduleTargetSageMakerPipelineParameter struct {
	Name  string `ub:"name"`
	Value string `ub:"value"`
}

type ScheduleSageMakerPipelineParams struct {
	PipelineParameterList *[]ScheduleTargetSageMakerPipelineParameter `ub:"pipeline-parameter-list"`
}

type ScheduleTargetSQSParameters struct {
	MessageGroupID *string `ub:"message-group-id"`
}

type ScheduleTarget struct {
	ARN                         string                           `ub:"arn"`
	RoleARN                     string                           `ub:"role-arn"`
	DeadLetterConfig            *ScheduleTargetDeadLetterConfig  `ub:"dead-letter-config"`
	ECSParameters               *ScheduleTargetECSParameters     `ub:"ecs-parameters"`
	EventBridgeParameters       *ScheduleEventBridgeParameters   `ub:"eventbridge-parameters"`
	Input                       *string                          `ub:"input"`
	KinesisParameters           *ScheduleTargetKinesisParameters `ub:"kinesis-parameters"`
	RetryPolicy                 *ScheduleTargetRetryPolicy       `ub:"retry-policy"`
	SageMakerPipelineParameters *ScheduleSageMakerPipelineParams `ub:"sage-maker-pipeline-parameters"`
	SQSParameters               *ScheduleTargetSQSParameters     `ub:"sqs-parameters"`
}

type ScheduleResource struct {
	ActionAfterCompletion      *string                    `ub:"action-after-completion"`
	Description                *string                    `ub:"description"`
	EndDate                    *string                    `ub:"end-date"`
	FlexibleTimeWindow         ScheduleFlexibleTimeWindow `ub:"flexible-time-window"`
	GroupName                  *string                    `ub:"group-name"`
	KMSKeyARN                  *string                    `ub:"kms-key-arn"`
	Name                       *string                    `ub:"name"`
	ScheduleExpression         string                     `ub:"schedule-expression"`
	ScheduleExpressionTimezone *string                    `ub:"schedule-expression-timezone"`
	StartDate                  *string                    `ub:"start-date"`
	State                      *string                    `ub:"state"`
	Target                     ScheduleTarget             `ub:"target"`
}

type ScheduleResourceOutput struct {
	ARN string `ub:"arn"`
}

func (r *ScheduleResource) SchemaVersion() int { return 1 }

func (r *ScheduleResource) ReplaceFields() []string {
	return []string{"name", "group-name"}
}

func (r ScheduleResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.ActionAfterCompletion)).
			Require(constraint.OneOf(r.ActionAfterCompletion, "DELETE", "NONE")).
			Message("action-after-completion must be DELETE or NONE"),
		constraint.Must(constraint.OneOf(r.FlexibleTimeWindow.Mode, "OFF", "FLEXIBLE")).
			Message("flexible-time-window mode must be OFF or FLEXIBLE"),
		constraint.When(constraint.Present(r.FlexibleTimeWindow.MaximumWindowInMinutes)).
			Require(
				constraint.AtLeast(r.FlexibleTimeWindow.MaximumWindowInMinutes, 1),
				constraint.AtMost(r.FlexibleTimeWindow.MaximumWindowInMinutes, 1440),
			).
			Message("maximum-window-in-minutes must be between 1 and 1440"),
		constraint.When(constraint.Equals(r.FlexibleTimeWindow.Mode, "FLEXIBLE")).
			Require(constraint.Present(r.FlexibleTimeWindow.MaximumWindowInMinutes)).
			Message("FLEXIBLE mode requires maximum-window-in-minutes"),
		constraint.When(constraint.Present(r.State)).
			Require(constraint.OneOf(r.State, "ENABLED", "DISABLED")).
			Message("state must be ENABLED or DISABLED"),
		constraint.AtMostOneOf(
			r.Target.ECSParameters,
			r.Target.EventBridgeParameters,
			r.Target.KinesisParameters,
			r.Target.SageMakerPipelineParameters,
			r.Target.SQSParameters,
		),
		constraint.When(constraint.Present(r.Target.Input)).
			Require(constraint.NotEmpty(r.Target.Input)).
			Message("target input must not be empty"),
		constraint.When(constraint.Present(
			r.Target.RetryPolicy.MaximumEventAgeInSeconds)).
			Require(
				constraint.AtLeast(
					r.Target.RetryPolicy.MaximumEventAgeInSeconds, 60),
				constraint.AtMost(
					r.Target.RetryPolicy.MaximumEventAgeInSeconds, 86400),
			).
			Message("maximum-event-age-in-seconds must be between 60 and 86400"),
		constraint.When(constraint.Present(
			r.Target.RetryPolicy.MaximumRetryAttempts)).
			Require(
				constraint.AtLeast(r.Target.RetryPolicy.MaximumRetryAttempts, 0),
				constraint.AtMost(r.Target.RetryPolicy.MaximumRetryAttempts, 185),
			).
			Message("maximum-retry-attempts must be between 0 and 185"),
		constraint.When(constraint.Present(r.Target.ECSParameters.TaskCount)).
			Require(
				constraint.AtLeast(r.Target.ECSParameters.TaskCount, 1),
				constraint.AtMost(r.Target.ECSParameters.TaskCount, 10),
			).
			Message("task-count must be between 1 and 10"),
		constraint.When(constraint.Present(r.Target.ECSParameters.LaunchType)).
			Require(constraint.OneOf(r.Target.ECSParameters.LaunchType,
				"EC2", "FARGATE", "EXTERNAL")).
			Message("launch-type must be EC2, FARGATE, or EXTERNAL"),
		constraint.ForbiddenWith(
			r.Target.ECSParameters.LaunchType,
			r.Target.ECSParameters.CapacityProviderStrategy,
		),
		constraint.Must(constraint.MaxItems(
			r.Target.ECSParameters.CapacityProviderStrategy, 6)).
			Message("capacity-provider-strategy holds at most 6 entries"),
		constraint.ForEach(r.Target.ECSParameters.CapacityProviderStrategy,
			func(s ScheduleECSCapacityProviderStrategy) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(s.Base)).
						Require(
							constraint.AtLeast(s.Base, 0),
							constraint.AtMost(s.Base, 100000),
						).
						Message("capacity provider base must be between 0 and 100000"),
					constraint.When(constraint.Present(s.Weight)).
						Require(
							constraint.AtLeast(s.Weight, 0),
							constraint.AtMost(s.Weight, 1000),
						).
						Message("capacity provider weight must be between 0 and 1000"),
				}
			}),
		constraint.When(constraint.Equals(
			r.Target.ECSParameters.LaunchType, "FARGATE")).
			Require(
				constraint.Present(r.Target.ECSParameters.NetworkConfiguration),
				constraint.Absent(r.Target.ECSParameters.PlacementConstraints),
			).
			Message("FARGATE requires networking and forbids placement constraints"),
		constraint.When(constraint.IsTrue(
			r.Target.ECSParameters.NetworkConfiguration.AssignPublicIP)).
			Require(constraint.Equals(r.Target.ECSParameters.LaunchType, "FARGATE")).
			Message("assign-public-ip true requires FARGATE"),
		constraint.When(constraint.Present(r.Target.ECSParameters.PlatformVersion)).
			Require(constraint.Equals(r.Target.ECSParameters.LaunchType, "FARGATE")).
			Message("platform-version requires FARGATE"),
		constraint.When(constraint.Present(
			r.Target.ECSParameters.NetworkConfiguration)).
			Require(
				constraint.MinItems(
					r.Target.ECSParameters.NetworkConfiguration.Subnets, 1),
				constraint.MaxItems(
					r.Target.ECSParameters.NetworkConfiguration.Subnets, 16),
				constraint.MinItems(
					r.Target.ECSParameters.NetworkConfiguration.SecurityGroups, 1),
				constraint.MaxItems(
					r.Target.ECSParameters.NetworkConfiguration.SecurityGroups, 5),
			).
			Message("networking requires 1 to 16 subnets and at most 5 security groups"),
		constraint.Must(constraint.MaxItems(
			r.Target.ECSParameters.PlacementConstraints, 10)).
			Message("placement-constraints holds at most 10 entries"),
		constraint.ForEach(r.Target.ECSParameters.PlacementConstraints,
			func(c ScheduleTargetECSPlacementConstraint) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(c.Type,
						"distinctInstance", "memberOf")).
						Message("placement constraint type must be distinctInstance or memberOf"),
					constraint.When(constraint.Equals(c.Type, "memberOf")).
						Require(constraint.Present(c.Expression)).
						Message("memberOf requires an expression"),
					constraint.When(constraint.Equals(c.Type, "distinctInstance")).
						Require(constraint.Absent(c.Expression)).
						Message("distinctInstance forbids an expression"),
				}
			}),
		constraint.Must(constraint.MaxItems(
			r.Target.ECSParameters.PlacementStrategy, 5)).
			Message("placement-strategy holds at most 5 entries"),
		constraint.ForEach(r.Target.ECSParameters.PlacementStrategy,
			func(s ScheduleTargetECSPlacementStrategy) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(s.Type,
						"random", "spread", "binpack")).
						Message("placement strategy type must be random, spread, or binpack"),
				}
			}),
		constraint.When(constraint.Present(r.Target.ECSParameters.PropagateTags)).
			Require(constraint.OneOf(
				r.Target.ECSParameters.PropagateTags, "TASK_DEFINITION")).
			Message("propagate-tags must be TASK_DEFINITION"),
		constraint.Must(constraint.MaxItems(r.Target.ECSParameters.Tags, 50)).
			Message("ECS tags holds at most 50 entries"),
		constraint.Must(constraint.MaxItems(
			r.Target.SageMakerPipelineParameters.PipelineParameterList, 200)).
			Message("pipeline-parameter-list holds at most 200 entries"),
	}
}

func (r *ScheduleResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*ScheduleResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, rand.Reader)
}

func (r *ScheduleResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[ScheduleResource, *ScheduleResourceOutput, *awsCfg],
) (*ScheduleResourceOutput, error) {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior)
}

func (r *ScheduleResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[ScheduleResource, *ScheduleResourceOutput, *awsCfg],
) (*ScheduleResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior)
}

func (r *ScheduleResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[ScheduleResource, *ScheduleResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior)
}

type scheduleClient interface {
	CreateSchedule(context.Context, *schedulerapi.CreateScheduleInput,
		...func(*schedulerapi.Options)) (*schedulerapi.CreateScheduleOutput, error)
	GetSchedule(context.Context, *schedulerapi.GetScheduleInput,
		...func(*schedulerapi.Options)) (*schedulerapi.GetScheduleOutput, error)
	UpdateSchedule(context.Context, *schedulerapi.UpdateScheduleInput,
		...func(*schedulerapi.Options)) (*schedulerapi.UpdateScheduleOutput, error)
	DeleteSchedule(context.Context, *schedulerapi.DeleteScheduleInput,
		...func(*schedulerapi.Options)) (*schedulerapi.DeleteScheduleOutput, error)
}

func (r *ScheduleResource) create(
	ctx context.Context,
	client scheduleClient,
	random io.Reader,
	options ...retry.Option,
) (*ScheduleResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	name, err := r.resolveName(random)
	if err != nil {
		return nil, err
	}
	in, err := r.createInput(name)
	if err != nil {
		return nil, err
	}
	var out *schedulerapi.CreateScheduleOutput
	err = retry.OnError(ctx, isIAMPropagationError, func(ctx context.Context) error {
		var err error
		out, err = client.CreateSchedule(ctx, in)
		return err
	}, options...)
	if err != nil {
		return nil, fmt.Errorf("create schedule %s: %w", name, err)
	}
	if out == nil || aws.ToString(out.ScheduleArn) == "" {
		return nil, errors.New("create schedule returned an empty ARN")
	}
	createdARN := aws.ToString(out.ScheduleArn)
	identity, err := parseScheduleARN(createdARN)
	if err != nil {
		return nil, fmt.Errorf("parse created schedule ARN: %w", err)
	}
	if identity.name != name {
		return nil, fmt.Errorf(
			"created schedule ARN names %q, expected %q", identity.name, name)
	}
	if r.GroupName != nil && identity.group != *r.GroupName {
		return nil, fmt.Errorf(
			"created schedule ARN names group %q, expected %q",
			identity.group, *r.GroupName)
	}
	return r.read(ctx, client, &ScheduleResourceOutput{ARN: createdARN})
}

func (r *ScheduleResource) read(
	ctx context.Context,
	client scheduleClient,
	prior *ScheduleResourceOutput,
) (*ScheduleResourceOutput, error) {
	if prior == nil {
		return nil, runtime.ErrNotFound
	}
	identity, err := parseScheduleARN(prior.ARN)
	if err != nil {
		return nil, fmt.Errorf("parse prior schedule ARN: %w", err)
	}
	out, err := client.GetSchedule(ctx, &schedulerapi.GetScheduleInput{
		GroupName: aws.String(identity.group),
		Name:      aws.String(identity.name),
	})
	if err != nil {
		if isScheduleNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get schedule %s/%s: %w", identity.group, identity.name, err)
	}
	if out == nil || aws.ToString(out.Arn) == "" {
		return nil, errors.New("get schedule returned an empty result")
	}
	settledARN := aws.ToString(out.Arn)
	settled, err := parseScheduleARN(settledARN)
	if err != nil {
		return nil, fmt.Errorf("parse returned schedule ARN: %w", err)
	}
	if settled.parsed != identity.parsed {
		return nil, fmt.Errorf(
			"get schedule returned ARN %q for requested ARN %q", settledARN, prior.ARN)
	}
	if out.GroupName != nil && aws.ToString(out.GroupName) != identity.group {
		return nil, fmt.Errorf(
			"get schedule returned group %q, expected %q",
			aws.ToString(out.GroupName), identity.group)
	}
	if out.Name != nil && aws.ToString(out.Name) != identity.name {
		return nil, fmt.Errorf(
			"get schedule returned name %q, expected %q",
			aws.ToString(out.Name), identity.name)
	}
	return &ScheduleResourceOutput{ARN: settledARN}, nil
}

func (r *ScheduleResource) update(
	ctx context.Context,
	client scheduleClient,
	prior runtime.Prior[ScheduleResource, *ScheduleResourceOutput, *awsCfg],
	options ...retry.Option,
) (*ScheduleResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	if prior.Outputs == nil {
		return nil, errors.New("update schedule has no prior output")
	}
	identity, err := parseScheduleARN(prior.Outputs.ARN)
	if err != nil {
		return nil, fmt.Errorf("parse prior schedule ARN: %w", err)
	}
	if r.mutableInputsChanged(prior.Inputs) {
		in, err := r.updateInput(identity)
		if err != nil {
			return nil, err
		}
		err = retry.OnError(ctx, isIAMPropagationError, func(ctx context.Context) error {
			_, err := client.UpdateSchedule(ctx, in)
			return err
		}, options...)
		if err != nil {
			return nil, fmt.Errorf(
				"update schedule %s/%s: %w", identity.group, identity.name, err)
		}
	}
	return r.read(ctx, client, prior.Outputs)
}

func (r *ScheduleResource) delete(
	ctx context.Context,
	client scheduleClient,
	prior *ScheduleResourceOutput,
) error {
	if prior == nil {
		return nil
	}
	identity, err := parseScheduleARN(prior.ARN)
	if err != nil {
		return fmt.Errorf("parse prior schedule ARN: %w", err)
	}
	_, err = client.DeleteSchedule(ctx, &schedulerapi.DeleteScheduleInput{
		GroupName: aws.String(identity.group),
		Name:      aws.String(identity.name),
	})
	if err != nil {
		if isScheduleNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete schedule %s/%s: %w", identity.group, identity.name, err)
	}
	return nil
}

type scheduleIdentity struct {
	parsed awsarn.ARN
	group  string
	name   string
}

func parseScheduleARN(value string) (scheduleIdentity, error) {
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return scheduleIdentity{}, err
	}
	if parsed.Partition == "" || parsed.Service != "scheduler" || parsed.Region == "" ||
		parsed.AccountID == "" {
		return scheduleIdentity{}, errors.New("expected an EventBridge Scheduler ARN")
	}
	parts := strings.Split(parsed.Resource, "/")
	if len(parts) != 3 || parts[0] != "schedule" || parts[1] == "" || parts[2] == "" {
		return scheduleIdentity{},
			errors.New("expected an EventBridge Scheduler schedule/<group>/<name> ARN")
	}
	return scheduleIdentity{parsed: parsed, group: parts[1], name: parts[2]}, nil
}

func (r *ScheduleResource) resolveName(random io.Reader) (string, error) {
	if r.Name != nil {
		return *r.Name, nil
	}
	value := make([]byte, 12)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", fmt.Errorf("generate schedule name: %w", err)
	}
	return "unobin-" + hex.EncodeToString(value), nil
}

func (r *ScheduleResource) mutableInputsChanged(prior ScheduleResource) bool {
	return runtime.Changed(prior.ActionAfterCompletion, r.ActionAfterCompletion) ||
		runtime.Changed(prior.Description, r.Description) ||
		runtime.Changed(prior.EndDate, r.EndDate) ||
		runtime.Changed(prior.FlexibleTimeWindow, r.FlexibleTimeWindow) ||
		runtime.Changed(prior.KMSKeyARN, r.KMSKeyARN) ||
		runtime.Changed(prior.ScheduleExpression, r.ScheduleExpression) ||
		runtime.Changed(
			prior.ScheduleExpressionTimezone, r.ScheduleExpressionTimezone) ||
		runtime.Changed(prior.StartDate, r.StartDate) ||
		runtime.Changed(prior.State, r.State) ||
		runtime.Changed(prior.Target, r.Target)
}
