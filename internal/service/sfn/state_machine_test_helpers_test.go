package sfn

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
)

const (
	testStateMachineARN = "arn:aws:states:us-east-1:123456789012:stateMachine:workflow"
	testRoleARN         = "arn:aws:iam::123456789012:role/workflow"
	testDefinition      = `{"StartAt":"Pass","States":{"Pass":{"Type":"Pass","End":true}}}`
)

type fakeStateMachineClient struct {
	calls []string

	validateInputs  []*sfn.ValidateStateMachineDefinitionInput
	validateOutputs []*sfn.ValidateStateMachineDefinitionOutput
	validateErrors  []error

	createInputs  []*sfn.CreateStateMachineInput
	createOutputs []*sfn.CreateStateMachineOutput
	createErrors  []error
	createNil     bool

	describeInputs  []*sfn.DescribeStateMachineInput
	describeOutputs []*sfn.DescribeStateMachineOutput
	describeErrors  []error
	describeNil     bool

	updateInputs  []*sfn.UpdateStateMachineInput
	updateOutputs []*sfn.UpdateStateMachineOutput
	updateErrors  []error

	deleteInputs []*sfn.DeleteStateMachineInput
	deleteErrors []error

	listTagInputs  []*sfn.ListTagsForResourceInput
	listTagOutputs []*sfn.ListTagsForResourceOutput
	listTagErrors  []error
	tagInputs      []*sfn.TagResourceInput
	tagErrors      []error
	untagInputs    []*sfn.UntagResourceInput
	untagErrors    []error
}

func (c *fakeStateMachineClient) ValidateStateMachineDefinition(
	_ context.Context,
	in *sfn.ValidateStateMachineDefinitionInput,
	_ ...func(*sfn.Options),
) (*sfn.ValidateStateMachineDefinitionOutput, error) {
	c.calls = append(c.calls, "validate")
	c.validateInputs = append(c.validateInputs, in)
	if err := pop(&c.validateErrors); err != nil {
		return nil, err
	}
	if out := pop(&c.validateOutputs); out != nil {
		return out, nil
	}
	return &sfn.ValidateStateMachineDefinitionOutput{
		Result: sfntypes.ValidateStateMachineDefinitionResultCodeOk,
	}, nil
}

func (c *fakeStateMachineClient) CreateStateMachine(
	_ context.Context,
	in *sfn.CreateStateMachineInput,
	_ ...func(*sfn.Options),
) (*sfn.CreateStateMachineOutput, error) {
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
	return &sfn.CreateStateMachineOutput{StateMachineArn: aws.String(testStateMachineARN)}, nil
}

func (c *fakeStateMachineClient) DescribeStateMachine(
	_ context.Context,
	in *sfn.DescribeStateMachineInput,
	_ ...func(*sfn.Options),
) (*sfn.DescribeStateMachineOutput, error) {
	c.calls = append(c.calls, "describe")
	c.describeInputs = append(c.describeInputs, in)
	if err := pop(&c.describeErrors); err != nil {
		return nil, err
	}
	if c.describeNil {
		return nil, nil
	}
	if out := pop(&c.describeOutputs); out != nil {
		return out, nil
	}
	return stateMachineDescription(testDefinition, testRoleARN, "revision-1"), nil
}

func (c *fakeStateMachineClient) UpdateStateMachine(
	_ context.Context,
	in *sfn.UpdateStateMachineInput,
	_ ...func(*sfn.Options),
) (*sfn.UpdateStateMachineOutput, error) {
	c.calls = append(c.calls, "update")
	c.updateInputs = append(c.updateInputs, in)
	if err := pop(&c.updateErrors); err != nil {
		return nil, err
	}
	if out := pop(&c.updateOutputs); out != nil {
		return out, nil
	}
	return &sfn.UpdateStateMachineOutput{RevisionId: aws.String("revision-2")}, nil
}

func (c *fakeStateMachineClient) DeleteStateMachine(
	_ context.Context,
	in *sfn.DeleteStateMachineInput,
	_ ...func(*sfn.Options),
) (*sfn.DeleteStateMachineOutput, error) {
	c.calls = append(c.calls, "delete")
	c.deleteInputs = append(c.deleteInputs, in)
	if err := pop(&c.deleteErrors); err != nil {
		return nil, err
	}
	return &sfn.DeleteStateMachineOutput{}, nil
}

func (c *fakeStateMachineClient) ListTagsForResource(
	_ context.Context,
	in *sfn.ListTagsForResourceInput,
	_ ...func(*sfn.Options),
) (*sfn.ListTagsForResourceOutput, error) {
	c.calls = append(c.calls, "list-tags")
	c.listTagInputs = append(c.listTagInputs, in)
	if err := pop(&c.listTagErrors); err != nil {
		return nil, err
	}
	if out := pop(&c.listTagOutputs); out != nil {
		return out, nil
	}
	return &sfn.ListTagsForResourceOutput{}, nil
}

func (c *fakeStateMachineClient) TagResource(
	_ context.Context,
	in *sfn.TagResourceInput,
	_ ...func(*sfn.Options),
) (*sfn.TagResourceOutput, error) {
	c.calls = append(c.calls, "tag")
	c.tagInputs = append(c.tagInputs, in)
	if err := pop(&c.tagErrors); err != nil {
		return nil, err
	}
	return &sfn.TagResourceOutput{}, nil
}

func (c *fakeStateMachineClient) UntagResource(
	_ context.Context,
	in *sfn.UntagResourceInput,
	_ ...func(*sfn.Options),
) (*sfn.UntagResourceOutput, error) {
	c.calls = append(c.calls, "untag")
	c.untagInputs = append(c.untagInputs, in)
	if err := pop(&c.untagErrors); err != nil {
		return nil, err
	}
	return &sfn.UntagResourceOutput{}, nil
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

type fakeStateMachineClock struct {
	now      time.Time
	sleeps   []time.Duration
	sleepErr error
}

func (c *fakeStateMachineClock) Now() time.Time {
	return c.now
}

func (c *fakeStateMachineClock) Sleep(ctx context.Context, duration time.Duration) error {
	c.sleeps = append(c.sleeps, duration)
	if c.sleepErr != nil {
		return c.sleepErr
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	c.now = c.now.Add(duration)
	return nil
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func baseStateMachine() StateMachineResource {
	return StateMachineResource{
		Name:       aws.String("workflow"),
		Definition: testDefinition,
		RoleARN:    testRoleARN,
		Type:       string(sfntypes.StateMachineTypeStandard),
	}
}

func stateMachineDescription(
	definition string,
	roleARN string,
	revisionID string,
) *sfn.DescribeStateMachineOutput {
	return &sfn.DescribeStateMachineOutput{
		StateMachineArn: aws.String(testStateMachineARN),
		Definition:      aws.String(definition),
		RoleArn:         aws.String(roleARN),
		RevisionId:      aws.String(revisionID),
		Status:          sfntypes.StateMachineStatusActive,
		Type:            sfntypes.StateMachineTypeStandard,
	}
}

func stateMachineOptions(clock *fakeStateMachineClock) stateMachineOperationOptions {
	return stateMachineOperationOptions{
		clock:        clock,
		createWindow: 5 * time.Minute,
		updateWindow: time.Minute,
		deleteWindow: 5 * time.Minute,
	}
}
