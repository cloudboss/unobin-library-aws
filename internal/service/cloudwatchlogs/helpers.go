package cloudwatchlogs

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
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
	return cloudwatchlogs.NewFromConfig(awsCfg, func(o *cloudwatchlogs.Options) {
		o.Retryer = logResourceLimitRetryer{Retryer: o.Retryer}
	}), nil
}

// isNotFound reports whether err is CloudWatch Logs' resource-not-found
// error. The Logs API models a missing resource as its own error type, and
// resources map it to absent reads or successful deletes.
func isNotFound(err error) bool {
	var notFound *cloudwatchlogstypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

// logResourceLimitRetryer keeps CloudWatch Logs' resource-quota errors from
// entering the SDK retry loop while delegating all other decisions to the
// configured retryer.
type logResourceLimitRetryer struct {
	aws.Retryer
}

func (r logResourceLimitRetryer) IsErrorRetryable(err error) bool {
	if isLogsResourceLimitExceeded(err) {
		return false
	}
	return r.Retryer.IsErrorRetryable(err)
}

func (r logResourceLimitRetryer) GetAttemptToken(
	ctx context.Context,
) (func(error) error, error) {
	if v2, ok := r.Retryer.(aws.RetryerV2); ok {
		return v2.GetAttemptToken(ctx)
	}
	return r.Retryer.GetInitialToken(), nil
}

func isLogsResourceLimitExceeded(err error) bool {
	var limit *cloudwatchlogstypes.LimitExceededException
	return errors.As(err, &limit) &&
		strings.Contains(limit.ErrorMessage(), "Resource limit exceeded")
}
