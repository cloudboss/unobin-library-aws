package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type SuspendMicrovmAction struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

func (r *SuspendMicrovmAction) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*SuspendMicrovmActionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_, err = client.SuspendMicrovm(ctx, &awslambdamicrovms.SuspendMicrovmInput{
		MicrovmIdentifier: aws.String(r.MicrovmIdentifier),
	})
	if err != nil {
		return nil, fmt.Errorf("suspend Microvm %s: %w", r.MicrovmIdentifier, err)
	}
	return &SuspendMicrovmActionOutput{MicrovmIdentifier: r.MicrovmIdentifier}, nil
}
