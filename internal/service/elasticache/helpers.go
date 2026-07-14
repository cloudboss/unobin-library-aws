package elasticache

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

func newClient(ctx context.Context, cfg *awsCfg) (*elasticache.Client, error) {
	awsConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return elasticache.NewFromConfig(awsConfig), nil
}
