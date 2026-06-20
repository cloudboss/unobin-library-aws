package sts

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for sts, configured from cfg.
// It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*sts.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return sts.NewFromConfig(awsCfg), nil
}
