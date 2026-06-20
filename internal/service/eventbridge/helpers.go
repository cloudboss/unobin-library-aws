package eventbridge

import (
	"context"
	"errors"

	eventbridge "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for EventBridge, configured from
// cfg. It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*eventbridge.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return eventbridge.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is EventBridge's ResourceNotFound error.
// EventBridge models each failure as its own error type, so a resource Read
// matches the type to turn a read of a gone rule or target into
// runtime.ErrNotFound.
func isNotFound(err error) bool {
	var notFound *eventbridgetypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

// isConcurrentModification reports whether err is EventBridge's
// ConcurrentModification error. EventBridge raises it when two writes to the
// same rule race, which happens when a rule and its targets, or several
// targets on one rule, are reconciled at once. It clears on its own, so a
// caller retries the operation.
func isConcurrentModification(err error) bool {
	var conflict *eventbridgetypes.ConcurrentModificationException
	return errors.As(err, &conflict)
}
