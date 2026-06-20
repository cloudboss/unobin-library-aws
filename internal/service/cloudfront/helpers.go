package cloudfront

import (
	"context"

	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for CloudFront, configured from
// cfg. It builds an aws.Config via awscfg.Load. Each CloudFront resource owns
// its own typed not-found predicate beside it, since the service models a
// missing resource as a per-resource error type, so this helper holds only the
// client constructor.
func newClient(ctx context.Context, cfg *awsCfg) (*cloudfront.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return cloudfront.NewFromConfig(awsCfg), nil
}
