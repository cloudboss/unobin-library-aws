package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type MicrovmDataSource struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

func (r *MicrovmDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := client.GetMicrovm(ctx, &awslambdamicrovms.GetMicrovmInput{
		MicrovmIdentifier: aws.String(r.MicrovmIdentifier),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("Microvm %s not found: %w", r.MicrovmIdentifier, err)
		}
		return nil, fmt.Errorf("read Microvm %s: %w", r.MicrovmIdentifier, err)
	}
	return microvmOutputFromGet(out), nil
}
