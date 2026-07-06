package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type TerminateMicrovmAction struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

func (r *TerminateMicrovmAction) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*TerminateMicrovmActionOutput, error) {
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
	return &TerminateMicrovmActionOutput{MicrovmIdentifier: r.MicrovmIdentifier}, nil
}
