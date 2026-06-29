package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	"github.com/cloudboss/unobin/pkg/constraint"
)

type MicrovmShellAuthToken struct {
	MicrovmIdentifier   string `ub:"microvm-identifier"`
	ExpirationInMinutes int64  `ub:"expiration-in-minutes"`
}

func (r MicrovmShellAuthToken) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.AtLeast(r.ExpirationInMinutes, 1),
			constraint.AtMost(r.ExpirationInMinutes, 60)).
			Message("expiration-in-minutes must be between 1 and 60"),
	}
}

func (r *MicrovmShellAuthToken) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmShellAuthTokenOutput, error) {
	expiration, err := int32FromInt64("expiration-in-minutes", r.ExpirationInMinutes)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := client.CreateMicrovmShellAuthToken(ctx,
		&awslambdamicrovms.CreateMicrovmShellAuthTokenInput{
			MicrovmIdentifier:   aws.String(r.MicrovmIdentifier),
			ExpirationInMinutes: aws.Int32(expiration),
		})
	if err != nil {
		return nil, fmt.Errorf("create Microvm shell auth token %s: %w", r.MicrovmIdentifier, err)
	}
	return &MicrovmShellAuthTokenOutput{AuthToken: out.AuthToken}, nil
}
