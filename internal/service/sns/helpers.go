package sns

import (
	"context"
	"errors"
	"slices"

	sns "github.com/aws/aws-sdk-go-v2/service/sns"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for sns, configured from cfg.
// It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*sns.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return sns.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is an AWS API error whose service code is one
// of codes. SNS reports a missing topic or subscription as a NotFound error
// reaching the caller as a smithy.APIError, so a resource Read matches the code
// to turn a read of a gone resource into runtime.ErrNotFound.
func isNotFound(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return slices.Contains(codes, apiErr.ErrorCode())
	}
	return false
}
