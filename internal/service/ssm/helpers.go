package ssm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	smithy "github.com/aws/smithy-go"

	"github.com/cloudboss/unobin-library-aws/internal/config"
)

// newClient returns the AWS SDK Go v2 client for ssm, configured from cfg.
// cfg is the *config.Configuration the runtime hands every lifecycle method;
// the helper unwraps it and builds an aws.Config via config.LoadAWSConfig.
func newClient(ctx context.Context, cfg any) (*ssm.Client, error) {
	c, ok := cfg.(*config.Configuration)
	if !ok {
		return nil, fmt.Errorf("ssmclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := config.LoadAWSConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	return ssm.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is SSM's ParameterNotFound error. SSM models a
// missing parameter as its own typed exception, returned by GetParameter and
// DeleteParameter, so a resource Read matches the type to turn a read of a gone
// parameter into runtime.ErrNotFound and a Delete treats it as already gone.
func isNotFound(err error) bool {
	var notFound *ssmtypes.ParameterNotFound
	return errors.As(err, &notFound)
}

// isTierUnsupported reports whether err is the ValidationException SSM returns
// when a request asks for a parameter tier the account or Region does not
// offer. SSM gives no distinct code for it, so the match is on the
// ValidationException code together with the "Tier is not supported" message.
// A caller clears the requested tier and retries once with the server default.
func isTierUnsupported(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorCode() == "ValidationException" &&
		strings.Contains(apiErr.ErrorMessage(), "Tier is not supported")
}
