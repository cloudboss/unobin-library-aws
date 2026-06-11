package kms

import (
	"context"
	"errors"
	"fmt"
	"strings"

	kms "github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

// newClient returns the AWS SDK Go v2 client for kms, configured from cfg.
// cfg is the *awscfg.Configuration the runtime hands every lifecycle
// method; the helper unwraps it and builds an aws.Config via
// awscfg.Load.
func newClient(ctx context.Context, cfg any) (*kms.Client, error) {
	c, ok := cfg.(*awscfg.Configuration)
	if !ok {
		return nil, fmt.Errorf("kmsclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := awscfg.Load(ctx, c)
	if err != nil {
		return nil, err
	}
	return kms.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is KMS's NotFound error. KMS models each
// failure as its own error type, so a Read matches the type to turn a read
// of a gone key or alias into runtime.ErrNotFound. This is the same
// condition the Terraform provider tests with its typed error check.
func isNotFound(err error) bool {
	var notFound *kmstypes.NotFoundException
	return errors.As(err, &notFound)
}

// isMalformedPolicy reports whether err is the malformed-policy error KMS
// returns when a key policy names a principal that was created moments
// earlier and has not propagated. KMS is eventually consistent about the
// principals it knows, so a create or a policy update that trips this
// succeeds once the principal is visible, and a caller retries.
func isMalformedPolicy(err error) bool {
	var malformed *kmstypes.MalformedPolicyDocumentException
	return errors.As(err, &malformed)
}

// isDisabled reports whether err is KMS's Disabled error. Enabling rotation
// on a key that is still settling into the enabled state can briefly fail
// this way, so a caller retries until the key is ready.
func isDisabled(err error) bool {
	var disabled *kmstypes.DisabledException
	return errors.As(err, &disabled)
}

// isPendingDeletion reports whether err is the invalid-state error KMS
// returns when an operation targets a key already scheduled for deletion. A
// delete treats it as success: the key is on its way out either way.
func isPendingDeletion(err error) bool {
	var invalidState *kmstypes.KMSInvalidStateException
	return errors.As(err, &invalidState) &&
		strings.Contains(invalidState.ErrorMessage(), "is pending deletion")
}
