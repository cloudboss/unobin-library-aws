package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type MicrovmImageBuildData struct {
	ImageIdentifier string `ub:"image-identifier"`
	ImageVersion    string `ub:"image-version"`
	BuildId         string `ub:"build-id"`
}

func (r *MicrovmImageBuildData) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageBuildDataOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := client.GetMicrovmImageBuild(ctx,
		&awslambdamicrovms.GetMicrovmImageBuildInput{
			ImageIdentifier: aws.String(r.ImageIdentifier),
			ImageVersion:    aws.String(r.ImageVersion),
			BuildId:         aws.String(r.BuildId),
		})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("Microvm image build %s not found: %w", r.BuildId, err)
		}
		return nil, fmt.Errorf("read Microvm image build %s: %w", r.BuildId, err)
	}
	return microvmImageBuildOutputFromGet(out), nil
}
