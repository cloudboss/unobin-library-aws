package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	"github.com/cloudboss/unobin/pkg/constraint"
)

type MicrovmAuthTokenAction struct {
	MicrovmIdentifier   string              `ub:"microvm-identifier"`
	ExpirationInMinutes int64               `ub:"expiration-in-minutes"`
	AllowedPorts        []PortSpecification `ub:"allowed-ports"`
}

func (r MicrovmAuthTokenAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.AtLeast(r.ExpirationInMinutes, 1),
			constraint.AtMost(r.ExpirationInMinutes, 60)).
			Message("expiration-in-minutes must be between 1 and 60"),
		constraint.Must(constraint.NotEmpty(r.AllowedPorts)).
			Message("allowed-ports must not be empty"),
		constraint.ForEach(r.AllowedPorts, func(p PortSpecification) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ExactlyOneOf(p.AllPorts, p.Port, p.Range),
				constraint.When(constraint.Present(p.AllPorts)).
					Require(constraint.IsTrue(p.AllPorts)).
					Message("all-ports must be true"),
				constraint.When(constraint.Present(p.Port)).
					Require(constraint.AtLeast(p.Port, 1), constraint.AtMost(p.Port, 65535)).
					Message("port must be between 1 and 65535"),
				constraint.When(constraint.Present(p.Range)).
					Require(constraint.AtLeast(p.Range.StartPort, 1),
						constraint.AtMost(p.Range.StartPort, 65535),
						constraint.AtLeast(p.Range.EndPort, 1),
						constraint.AtMost(p.Range.EndPort, 65535)).
					Message("range ports must be between 1 and 65535"),
				constraint.When(constraint.Present(p.Range)).
					Require(constraint.AtMost(p.Range.StartPort, p.Range.EndPort)).
					Message("port range start must be no greater than end"),
			}
		}),
	}
}

func (r *MicrovmAuthTokenAction) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmAuthTokenActionOutput, error) {
	expiration, err := int32FromInt64("expiration-in-minutes", r.ExpirationInMinutes)
	if err != nil {
		return nil, err
	}
	ports, err := portSpecificationsToSDK(r.AllowedPorts)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := client.CreateMicrovmAuthToken(ctx,
		&awslambdamicrovms.CreateMicrovmAuthTokenInput{
			MicrovmIdentifier:   aws.String(r.MicrovmIdentifier),
			ExpirationInMinutes: aws.Int32(expiration),
			AllowedPorts:        ports,
		})
	if err != nil {
		return nil, fmt.Errorf("create Microvm auth token %s: %w", r.MicrovmIdentifier, err)
	}
	return &MicrovmAuthTokenActionOutput{AuthToken: out.AuthToken}, nil
}
