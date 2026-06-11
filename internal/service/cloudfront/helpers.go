package cloudfront

import (
	"context"
	"fmt"

	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

// newClient returns the AWS SDK Go v2 client for CloudFront, configured from
// cfg. cfg is the *awscfg.Configuration the runtime hands every lifecycle
// method; the helper unwraps it and builds an aws.Config via
// awscfg.Load. Each CloudFront resource owns its own typed not-found
// predicate beside it, since the service models a missing resource as a
// per-resource error type, so this helper holds only the client constructor.
func newClient(ctx context.Context, cfg any) (*cloudfront.Client, error) {
	c, ok := cfg.(*awscfg.Configuration)
	if !ok {
		return nil, fmt.Errorf("cloudfrontclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := awscfg.Load(ctx, c)
	if err != nil {
		return nil, err
	}
	return cloudfront.NewFromConfig(awsCfg), nil
}
