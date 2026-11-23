package sfn

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/hashicorp/aws-sdk-go-base/v2/endpoints"
)

type StartExecutionAction struct {
	StateMachineARN string  `ub:"state-machine-arn"`
	Input           *string `ub:"input"`
	Name            *string `ub:"name"`
	TraceHeader     *string `ub:"trace-header"`
	Region          *string `ub:"region"`
}

type StartExecutionActionOutput struct {
	ExecutionARN string `ub:"execution-arn"`
	StartDate    string `ub:"start-date"`
}

func (r StartExecutionAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.IsFormat(r.Input, constraint.FormatJSON)).
			Message("input must be valid JSON"),
		constraint.Must(constraint.Expr(
			"input.state-machine-arn != null", constraint.Bind("state-machine-arn", r.StateMachineARN),
		)).Message("state-machine-arn is required"),
	}
}

func (r *StartExecutionAction) Run(
	ctx context.Context, cfg *awsCfg,
) (*StartExecutionActionOutput, error) {
	sdkConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if region := aws.ToString(r.Region); region != "" {
		if sdkConfig.Region != "" {
			configuredPartition := executionRegionPartition(sdkConfig.Region)
			if partition := executionRegionPartition(region); partition != configuredPartition {
				return nil, fmt.Errorf("region %q belongs to partition %q, expected %q",
					region, partition, configuredPartition)
			}
		}
		sdkConfig.Region = region
	}

	response, err := sfn.NewFromConfig(sdkConfig).StartExecution(ctx, r.prepareRequest())
	if err != nil {
		return nil, fmt.Errorf("start Step Functions execution for %s: %w", r.StateMachineARN, err)
	}
	output := &StartExecutionActionOutput{ExecutionARN: aws.ToString(response.ExecutionArn)}
	if response.StartDate != nil {
		output.StartDate = response.StartDate.UTC().Format(time.RFC3339Nano)
	}
	return output, nil
}

func (r *StartExecutionAction) prepareRequest() *sfn.StartExecutionInput {
	input := r.Input
	if input == nil {
		input = aws.String("{}")
	}
	return &sfn.StartExecutionInput{
		StateMachineArn: aws.String(r.StateMachineARN),
		Input:           input,
		Name:            r.Name,
		TraceHeader:     r.TraceHeader,
	}
}

func executionRegionPartition(region string) string {
	partition, ok := endpoints.PartitionForRegion(endpoints.DefaultPartitions(), region)
	if ok {
		return partition.ID()
	}
	return "aws"
}
