package cloudwatchlogs

import (
	"context"
	"errors"

	cloudwatchlogs "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for cloudwatchlogs, configured
// from cfg. It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*cloudwatchlogs.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return cloudwatchlogs.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is CloudWatch Logs' resource-not-found
// error. The Logs API models a missing resource as its own error type; a
// Delete matches the type to treat a delete of a gone log group as success.
// Read instead detects a missing log group by an empty describe result, so
// it does not rely on this predicate.
func isNotFound(err error) bool {
	var notFound *cloudwatchlogstypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}
