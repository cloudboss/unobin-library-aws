package eventbridge

import (
	"context"
	"errors"
	"fmt"

	eventbridge "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

// newClient returns the AWS SDK Go v2 client for EventBridge, configured from
// cfg. cfg is the *awscfg.Configuration the runtime hands every lifecycle
// method; the helper unwraps it and builds an aws.Config via
// awscfg.Load.
func newClient(ctx context.Context, cfg any) (*eventbridge.Client, error) {
	c, ok := cfg.(*awscfg.Configuration)
	if !ok {
		return nil, fmt.Errorf("eventbridgeclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := awscfg.Load(ctx, c)
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
