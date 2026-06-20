package acm

import (
	"context"
	"errors"

	acm "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for acm, configured from cfg.
// It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*acm.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return acm.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is ACM's resource-not-found error. ACM
// models a missing certificate as its own error type, so a Read matches the
// type to turn a read of a gone certificate into runtime.ErrNotFound. This
// is the same condition the Terraform provider tests with its typed error
// check.
func isNotFound(err error) bool {
	var notFound *acmtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

// isInUse reports whether err is ACM's in-use error. ACM raises it when a
// certificate is still referenced by another service, such as a load balancer
// listener; the reference can take many minutes to clear, so a delete retries
// while this error persists.
func isInUse(err error) bool {
	var inUse *acmtypes.ResourceInUseException
	return errors.As(err, &inUse)
}
