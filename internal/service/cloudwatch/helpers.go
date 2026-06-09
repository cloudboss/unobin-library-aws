package cloudwatch

import (
	"context"
	"errors"
	"fmt"

	cloudwatch "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/cloudboss/unobin-library-aws/internal/config"
)

// newClient returns the AWS SDK Go v2 client for CloudWatch, configured from
// cfg. cfg is the *config.Configuration the runtime hands every lifecycle
// method; the helper unwraps it and builds an aws.Config via
// config.LoadAWSConfig.
func newClient(ctx context.Context, cfg any) (*cloudwatch.Client, error) {
	c, ok := cfg.(*config.Configuration)
	if !ok {
		return nil, fmt.Errorf("cloudwatchclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := config.LoadAWSConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	return cloudwatch.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is CloudWatch's ResourceNotFoundException, the
// one typed not-found exception this resource sees. DeleteAlarms returns it for
// an alarm already gone, which the delete swallows as success. A read does not
// rely on it: DescribeAlarms does not error on a missing alarm, it returns an
// empty result, which Read maps to runtime.ErrNotFound instead.
func isNotFound(err error) bool {
	var notFound *cloudwatchtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}
