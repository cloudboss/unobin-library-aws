package secretsmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"

	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	secretsmanagertypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/cloudboss/unobin-library-aws/internal/config"
)

// newClient returns the AWS SDK Go v2 client for secretsmanager, configured
// from cfg. cfg is the *config.Configuration the runtime hands every lifecycle
// method; the helper unwraps it and builds an aws.Config via
// config.LoadAWSConfig.
func newClient(ctx context.Context, cfg any) (*secretsmanager.Client, error) {
	c, ok := cfg.(*config.Configuration)
	if !ok {
		return nil, fmt.Errorf("secretsmanagerclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := config.LoadAWSConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	return secretsmanager.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is Secrets Manager's resource-not-found error.
// The service models a missing secret as its own error type, so a Read maps the
// type to runtime.ErrNotFound and a Delete treats it as already gone.
func isNotFound(err error) bool {
	var notFound *secretsmanagertypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

// isValueGone reports whether err is a value read against a secret that no
// longer holds a readable value: either the secret itself is gone, or it is
// scheduled for deletion. GetSecretValue reports the scheduled-for-deletion
// case as an InvalidRequestException rather than a not-found, so the message is
// matched to recognize it. Both forms map to runtime.ErrNotFound for the value.
func isValueGone(err error) bool {
	if isNotFound(err) {
		return true
	}
	var invalid *secretsmanagertypes.InvalidRequestException
	if errors.As(err, &invalid) {
		msg := invalid.ErrorMessage()
		return strings.Contains(msg, "because it was deleted") ||
			strings.Contains(msg, "because it was marked for deletion")
	}
	return false
}

// isScheduledForDeletion reports whether err is the create-time race in which a
// secret with the same name is mid-deletion. Secrets Manager reports it as an
// InvalidRequestException whose message names the scheduled or completed
// deletion; the name frees up once the deletion finishes, so a create retries
// through it.
func isScheduledForDeletion(err error) bool {
	var invalid *secretsmanagertypes.InvalidRequestException
	if errors.As(err, &invalid) {
		msg := invalid.ErrorMessage()
		return strings.Contains(msg, "scheduled for deletion") ||
			strings.Contains(msg, "was deleted")
	}
	return false
}
