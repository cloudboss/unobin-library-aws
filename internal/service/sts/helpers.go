package sts

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

// newClient returns the AWS SDK Go v2 client for sts, configured from cfg.
// cfg is the *awscfg.Configuration the runtime hands every lifecycle method;
// the helper unwraps it and builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg any) (*sts.Client, error) {
	c, ok := cfg.(*awscfg.Configuration)
	if !ok {
		return nil, fmt.Errorf("stsclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := awscfg.Load(ctx, c)
	if err != nil {
		return nil, err
	}
	return sts.NewFromConfig(awsCfg), nil
}
