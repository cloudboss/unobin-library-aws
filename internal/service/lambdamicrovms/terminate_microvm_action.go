package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type TerminateMicrovm struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

func (r *TerminateMicrovm) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*TerminateMicrovmOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_, err = client.TerminateMicrovm(ctx, &awslambdamicrovms.TerminateMicrovmInput{
		MicrovmIdentifier: aws.String(r.MicrovmIdentifier),
	})
	if err != nil {
		return nil, fmt.Errorf("terminate Microvm %s: %w", r.MicrovmIdentifier, err)
	}
	return &TerminateMicrovmOutput{MicrovmIdentifier: r.MicrovmIdentifier}, nil
}
