package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type ResumeMicrovmAction struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

func (r *ResumeMicrovmAction) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*ResumeMicrovmActionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_, err = client.ResumeMicrovm(ctx, &awslambdamicrovms.ResumeMicrovmInput{
		MicrovmIdentifier: aws.String(r.MicrovmIdentifier),
	})
	if err != nil {
		return nil, fmt.Errorf("resume Microvm %s: %w", r.MicrovmIdentifier, err)
	}
	return &ResumeMicrovmActionOutput{MicrovmIdentifier: r.MicrovmIdentifier}, nil
}
