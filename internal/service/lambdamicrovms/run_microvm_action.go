package lambdamicrovms

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	"github.com/cloudboss/unobin/pkg/constraint"
)

const maxRunHookPayloadBytes = 16_384

type RunMicrovmAction struct {
	ImageIdentifier          string      `ub:"image-identifier"`
	ImageVersion             *string     `ub:"image-version"`
	ExecutionRoleArn         *string     `ub:"execution-role-arn"`
	IngressNetworkConnectors *[]string   `ub:"ingress-network-connectors"`
	EgressNetworkConnectors  *[]string   `ub:"egress-network-connectors"`
	IdlePolicy               *IdlePolicy `ub:"idle-policy"`
	Logging                  *Logging    `ub:"logging"`
	MaximumDurationInSeconds *int64      `ub:"maximum-duration-in-seconds"`
	RunHookPayloadContent    *string     `ub:"run-hook-payload-content"`
	RunHookPayloadPath       *string     `ub:"run-hook-payload-path"`
}

func (r RunMicrovmAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.RunHookPayloadContent, r.RunHookPayloadPath),
		constraint.When(constraint.Present(r.MaximumDurationInSeconds)).
			Require(constraint.AtLeast(r.MaximumDurationInSeconds, 1),
				constraint.AtMost(r.MaximumDurationInSeconds, 28800)).
			Message("maximum-duration-in-seconds must be between 1 and 28800"),
		constraint.When(constraint.Present(r.Logging)).Require(constraint.Any(
			constraint.All(
				constraint.Present(r.Logging.CloudWatch),
				constraint.Absent(r.Logging.Disabled),
			),
			constraint.All(
				constraint.Absent(r.Logging.CloudWatch),
				constraint.Present(r.Logging.Disabled),
			),
		)).Message("logging must set exactly one of cloud-watch or disabled"),
		constraint.When(constraint.Present(r.Logging.Disabled)).
			Require(constraint.IsTrue(r.Logging.Disabled)).
			Message("logging disabled must be true"),
	}
}

func (r *RunMicrovmAction) Run(ctx context.Context, cfg *awsCfg) (*RunMicrovmActionOutput, error) {
	payload, err := r.payload()
	if err != nil {
		return nil, err
	}
	maximumDuration, err := int32PtrFromOptionalInt64(
		"maximum-duration-in-seconds", r.MaximumDurationInSeconds)
	if err != nil {
		return nil, err
	}
	idlePolicy, err := idlePolicyToSDK(r.IdlePolicy)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := client.RunMicrovm(ctx, &awslambdamicrovms.RunMicrovmInput{
		ImageIdentifier:          aws.String(r.ImageIdentifier),
		ImageVersion:             r.ImageVersion,
		ExecutionRoleArn:         r.ExecutionRoleArn,
		IngressNetworkConnectors: stringSliceValue(r.IngressNetworkConnectors),
		EgressNetworkConnectors:  stringSliceValue(r.EgressNetworkConnectors),
		IdlePolicy:               idlePolicy,
		Logging:                  loggingToSDK(r.Logging),
		MaximumDurationInSeconds: maximumDuration,
		RunHookPayload:           payload,
	})
	if err != nil {
		return nil, fmt.Errorf("run Microvm from image %s: %w", r.ImageIdentifier, err)
	}
	return (*RunMicrovmActionOutput)(microvmOutputFromRun(out)), nil
}

func (r *RunMicrovmAction) payload() (*string, error) {
	if r.RunHookPayloadContent != nil && r.RunHookPayloadPath != nil {
		return nil, errors.New("at most one run-hook payload source may be set")
	}
	var payload *string
	if r.RunHookPayloadContent != nil {
		payload = r.RunHookPayloadContent
	}
	if r.RunHookPayloadPath != nil {
		content, err := os.ReadFile(*r.RunHookPayloadPath)
		if err != nil {
			return nil, fmt.Errorf("read run-hook-payload-path: %w", err)
		}
		value := string(content)
		payload = &value
	}
	if payload != nil && len(*payload) > maxRunHookPayloadBytes {
		return nil, fmt.Errorf("run-hook-payload exceeds %d bytes", maxRunHookPayloadBytes)
	}
	return payload, nil
}
