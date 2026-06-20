package ssm

import (
	"context"
	"errors"
	"strings"

	ssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for ssm, configured from cfg.
// It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*ssm.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
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
