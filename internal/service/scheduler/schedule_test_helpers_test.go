package scheduler

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	schedulerapi "github.com/aws/aws-sdk-go-v2/service/scheduler"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
)

const (
	testScheduleARN = "arn:aws:scheduler:us-east-1:123456789012:schedule/default/daily"
	testRoleARN     = "arn:aws:iam::123456789012:role/scheduler"
	testTargetARN   = "arn:aws:lambda:us-east-1:123456789012:function:scheduled"
)

type fakeScheduleClient struct {
	calls []string

	createInputs  []*schedulerapi.CreateScheduleInput
	createOutputs []*schedulerapi.CreateScheduleOutput
	createErrors  []error
	createNil     bool

	getInputs  []*schedulerapi.GetScheduleInput
	getOutputs []*schedulerapi.GetScheduleOutput
	getErrors  []error
	getNil     bool

	updateInputs  []*schedulerapi.UpdateScheduleInput
	updateOutputs []*schedulerapi.UpdateScheduleOutput
	updateErrors  []error

	deleteInputs []*schedulerapi.DeleteScheduleInput
	deleteErrors []error
}

func (c *fakeScheduleClient) CreateSchedule(
	_ context.Context,
	in *schedulerapi.CreateScheduleInput,
	_ ...func(*schedulerapi.Options),
) (*schedulerapi.CreateScheduleOutput, error) {
	c.calls = append(c.calls, "create")
	c.createInputs = append(c.createInputs, in)
	if err := pop(&c.createErrors); err != nil {
		return nil, err
	}
	if c.createNil {
		return nil, nil
	}
	if out := pop(&c.createOutputs); out != nil {
		return out, nil
	}
	return &schedulerapi.CreateScheduleOutput{ScheduleArn: aws.String(testScheduleARN)}, nil
}

func (c *fakeScheduleClient) GetSchedule(
	_ context.Context,
	in *schedulerapi.GetScheduleInput,
	_ ...func(*schedulerapi.Options),
) (*schedulerapi.GetScheduleOutput, error) {
	c.calls = append(c.calls, "get")
	c.getInputs = append(c.getInputs, in)
	if err := pop(&c.getErrors); err != nil {
		return nil, err
	}
	if c.getNil {
		return nil, nil
	}
	if out := pop(&c.getOutputs); out != nil {
		return out, nil
	}
	return &schedulerapi.GetScheduleOutput{
		Arn:       aws.String(testScheduleARN),
		GroupName: aws.String("default"),
		Name:      aws.String("daily"),
	}, nil
}

func (c *fakeScheduleClient) UpdateSchedule(
	_ context.Context,
	in *schedulerapi.UpdateScheduleInput,
	_ ...func(*schedulerapi.Options),
) (*schedulerapi.UpdateScheduleOutput, error) {
	c.calls = append(c.calls, "update")
	c.updateInputs = append(c.updateInputs, in)
	if err := pop(&c.updateErrors); err != nil {
		return nil, err
	}
	if out := pop(&c.updateOutputs); out != nil {
		return out, nil
	}
	return &schedulerapi.UpdateScheduleOutput{ScheduleArn: aws.String(testScheduleARN)}, nil
}

func (c *fakeScheduleClient) DeleteSchedule(
	_ context.Context,
	in *schedulerapi.DeleteScheduleInput,
	_ ...func(*schedulerapi.Options),
) (*schedulerapi.DeleteScheduleOutput, error) {
	c.calls = append(c.calls, "delete")
	c.deleteInputs = append(c.deleteInputs, in)
	if err := pop(&c.deleteErrors); err != nil {
		return nil, err
	}
	return &schedulerapi.DeleteScheduleOutput{}, nil
}

func pop[T any](values *[]T) T {
	var zero T
	if len(*values) == 0 {
		return zero
	}
	value := (*values)[0]
	*values = (*values)[1:]
	return value
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func baseSchedule() ScheduleResource {
	return ScheduleResource{
		Name:               aws.String("daily"),
		ScheduleExpression: "rate(1 day)",
		FlexibleTimeWindow: ScheduleFlexibleTimeWindow{
			Mode: string(schedulertypes.FlexibleTimeWindowModeOff),
		},
		Target: ScheduleTarget{
			ARN:     testTargetARN,
			RoleARN: testRoleARN,
		},
	}
}
