package sns

import (
	"context"
	"errors"
	"fmt"
	"slices"

	sns "github.com/aws/aws-sdk-go-v2/service/sns"
	smithy "github.com/aws/smithy-go"

	"github.com/cloudboss/unobin-library-aws/internal/config"
)

// newClient returns the AWS SDK Go v2 client for sns, configured from cfg.
// cfg is the *config.Configuration the runtime hands every lifecycle method;
// the helper unwraps it and builds an aws.Config via config.LoadAWSConfig.
func newClient(ctx context.Context, cfg any) (*sns.Client, error) {
	c, ok := cfg.(*config.Configuration)
	if !ok {
		return nil, fmt.Errorf("snsclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := config.LoadAWSConfig(ctx, c)
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
