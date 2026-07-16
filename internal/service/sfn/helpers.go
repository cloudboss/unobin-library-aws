package sfn

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

func newClient(ctx context.Context, cfg *awsCfg) (*sfn.Client, error) {
	awsConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return sfn.NewFromConfig(awsConfig), nil
}
