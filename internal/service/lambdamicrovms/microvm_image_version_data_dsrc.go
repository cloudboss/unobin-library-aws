package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type MicrovmImageVersionData struct {
	ImageIdentifier string `ub:"image-identifier"`
	ImageVersion    string `ub:"image-version"`
}

func (r *MicrovmImageVersionData) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageVersionDataOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := client.GetMicrovmImageVersion(ctx,
		&awslambdamicrovms.GetMicrovmImageVersionInput{
			ImageIdentifier: aws.String(r.ImageIdentifier),
			ImageVersion:    aws.String(r.ImageVersion),
		})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("Microvm image version %s not found: %w",
				r.ImageVersion, err)
		}
		return nil, fmt.Errorf("read Microvm image version %s: %w", r.ImageVersion, err)
	}
	converted, err := microvmImageVersionOutputFromGet(out)
	if err != nil {
		return nil, fmt.Errorf("convert Microvm image version %s: %w", r.ImageVersion, err)
	}
	return converted, nil
}
