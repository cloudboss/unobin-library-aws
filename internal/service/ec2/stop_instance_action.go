package ec2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/hashicorp/aws-sdk-go-base/v2/endpoints"
)

type StopInstanceAction struct {
	InstanceID string  `ub:"instance-id"`
	Force      *bool   `ub:"force"`
	Timeout    *int64  `ub:"timeout"`
	Region     *string `ub:"region"`
}

type StopInstanceActionOutput struct{}

type stopInstanceClient interface {
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput,
		...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	StopInstances(context.Context, *ec2.StopInstancesInput,
		...func(*ec2.Options)) (*ec2.StopInstancesOutput, error)
}

type stopInstanceClock struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func (r StopInstanceAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Expr(
			"input.instance-id != null", constraint.Bind("instance-id", r.InstanceID),
		)).Message("instance-id is required"),
		constraint.Must(
			constraint.IsMatch(r.InstanceID, "^i-[0-9a-f]{8,17}$"),
		).Message("instance-id must match ^i-[0-9a-f]{8,17}$"),
		constraint.Must(
			constraint.AtLeast(r.Timeout, 30), constraint.AtMost(r.Timeout, 3600),
		).Message("timeout must be between 30 and 3600 seconds"),
	}
}

func (r *StopInstanceAction) Run(
	ctx context.Context, cfg *awsCfg,
) (*StopInstanceActionOutput, error) {
	sdkConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if region := aws.ToString(r.Region); region != "" {
		if sdkConfig.Region != "" && stopInstancePartition(region) !=
			stopInstancePartition(sdkConfig.Region) {
			return nil, fmt.Errorf("stop instance region %q is outside configured partition %q",
				region, stopInstancePartition(sdkConfig.Region))
		}
		sdkConfig.Region = region
	}
	client := ec2.NewFromConfig(sdkConfig, func(options *ec2.Options) {
		options.Retryer = stopInstanceRetryer{Retryer: options.Retryer}
	})
	return r.runStopInstance(ctx, client, stopInstanceClock{
		now: time.Now, sleep: sleepForStopInstance,
	})
}

func (r *StopInstanceAction) prepareStopRequest() (*ec2.StopInstancesInput, time.Duration) {
	timeout := 600 * time.Second
	if r.Timeout != nil {
		timeout = time.Duration(*r.Timeout) * time.Second
	}
	return &ec2.StopInstancesInput{
		InstanceIds: []string{r.InstanceID}, Force: aws.Bool(aws.ToBool(r.Force)),
	}, timeout
}

func (r *StopInstanceAction) runStopInstance(
	ctx context.Context, client stopInstanceClient, clock stopInstanceClock,
) (*StopInstanceActionOutput, error) {
	request, timeout := r.prepareStopRequest()
	instance, err := findStopInstance(ctx, client, r.InstanceID)
	if err != nil {
		if errors.Is(err, errStopInstanceMissing) || isNotFound(err, "InvalidInstanceID.NotFound") {
			return nil, fmt.Errorf("instance not found %s: %w", r.InstanceID, err)
		}
		return nil, fmt.Errorf("describe instance %s: %w", r.InstanceID, err)
	}
	switch instance.State.Name {
	case types.InstanceStateNameStopped:
		return &StopInstanceActionOutput{}, nil
	case types.InstanceStateNameStopping:
	case types.InstanceStateNameRunning:
		if _, err := client.StopInstances(ctx, request); err != nil {
			return nil, fmt.Errorf("stop instance %s: %w", r.InstanceID, err)
		}
	default:
		return nil, fmt.Errorf("cannot stop instance %s in state %q",
			r.InstanceID, instance.State.Name)
	}
	return waitForStopInstance(ctx, client, r.InstanceID, timeout, clock)
}

var errStopInstanceMissing = errors.New("instance lookup did not return one live matching instance")

func findStopInstance(
	ctx context.Context, client stopInstanceClient, id string,
) (*types.Instance, error) {
	request := &ec2.DescribeInstancesInput{InstanceIds: []string{id}}
	var instances []types.Instance
	for {
		page, err := client.DescribeInstances(ctx, request)
		if err != nil {
			return nil, err
		}
		if page == nil {
			return nil, errors.New("describe instances returned a nil response")
		}
		for _, reservation := range page.Reservations {
			instances = append(instances, reservation.Instances...)
		}
		if aws.ToString(page.NextToken) == "" {
			break
		}
		request.NextToken = page.NextToken
	}
	if len(instances) != 1 || instances[0].State == nil ||
		aws.ToString(instances[0].InstanceId) != id ||
		instances[0].State.Name == types.InstanceStateNameTerminated {
		return nil, errStopInstanceMissing
	}
	return &instances[0], nil
}

func waitForStopInstance(
	ctx context.Context, client stopInstanceClient, id string,
	timeout time.Duration, clock stopInstanceClock,
) (*StopInstanceActionOutput, error) {
	deadline := clock.now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for instance %s to stop: %w", id, err)
		}
		if clock.now().After(deadline) {
			return nil, fmt.Errorf("timed out after %s waiting for instance %s to stop", timeout, id)
		}
		instance, err := findStopInstance(ctx, client, id)
		if err != nil {
			return nil, fmt.Errorf("wait for instance %s to stop: %w", id, err)
		}
		switch instance.State.Name {
		case types.InstanceStateNameStopped:
			return &StopInstanceActionOutput{}, nil
		case types.InstanceStateNameRunning, types.InstanceStateNameStopping,
			types.InstanceStateNameShuttingDown:
		default:
			return nil, fmt.Errorf("wait for instance %s: unexpected state %q", id, instance.State.Name)
		}
		if err := clock.sleep(ctx, 10*time.Second); err != nil {
			return nil, fmt.Errorf("wait for instance %s to stop: %w", id, err)
		}
	}
}

func sleepForStopInstance(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func stopInstancePartition(region string) string {
	partition, ok := endpoints.PartitionForRegion(endpoints.DefaultPartitions(), region)
	if ok {
		return partition.ID()
	}
	return "aws"
}

type stopInstanceRetryer struct{ aws.Retryer }

func (r stopInstanceRetryer) GetAttemptToken(ctx context.Context) (func(error) error, error) {
	if v2, ok := r.Retryer.(aws.RetryerV2); ok {
		return v2.GetAttemptToken(ctx)
	}
	return r.GetInitialToken(), nil
}

func (r stopInstanceRetryer) IsErrorRetryable(err error) bool {
	var api smithy.APIError
	if errors.As(err, &api) {
		message := api.ErrorMessage()
		switch api.ErrorCode() {
		case "InsufficientInstanceCapacity":
			return true
		case "InvalidParameterValue":
			if strings.Contains(message,
				"This call cannot be completed because there are pending VPNs or Virtual Interfaces") {
				return true
			}
		case "OperationNotPermitted":
			if strings.Contains(message,
				"Endpoint cannot be created while another endpoint is being created") {
				return true
			}
		case "ConcurrentMutationLimitExceeded":
			if strings.Contains(message, "Cannot initiate another change for this endpoint at this time") {
				return true
			}
		case "VpnConcentratorLimitExceeded", "VpnConnectionLimitExceeded", "VpnGatewayLimitExceeded":
			if strings.Contains(message, "maximum number of mutating objects has been reached") {
				return true
			}
		}
	}
	return r.Retryer.IsErrorRetryable(err)
}
