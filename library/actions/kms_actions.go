package actions

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	kms "github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/cloudboss/unobin/pkg/constraint"

	"github.com/cloudboss/unobin-library-aws/library/internal/kmshelpers"
	"github.com/cloudboss/unobin-library-aws/library/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/library/internal/retry"
)

// KmsKeyActionOutput is the empty result of the KMS key operations. Each
// mutates a key in place and returns nothing to reference.
type KmsKeyActionOutput struct{}

// KmsEnableKey enables a key, the EnableKey operation. A key still settling
// after creation can briefly report as not found, so the call retries through
// that window.
type KmsEnableKey struct {
	KeyId string `ub:"key-id"`
}

func (a *KmsEnableKey) Run(ctx context.Context, cfg any) (*KmsKeyActionOutput, error) {
	client, err := kmshelpers.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	err = retry.OnError(ctx, kmshelpers.IsNotFound, func(ctx context.Context) error {
		_, err := client.EnableKey(ctx, &kms.EnableKeyInput{KeyId: aws.String(a.KeyId)})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("enable key: %w", err)
	}
	return &KmsKeyActionOutput{}, nil
}

// KmsDisableKey disables a key, the DisableKey operation. As with enabling, a
// key that has not finished propagating is retried until it is visible.
type KmsDisableKey struct {
	KeyId string `ub:"key-id"`
}

func (a *KmsDisableKey) Run(ctx context.Context, cfg any) (*KmsKeyActionOutput, error) {
	client, err := kmshelpers.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	err = retry.OnError(ctx, kmshelpers.IsNotFound, func(ctx context.Context) error {
		_, err := client.DisableKey(ctx, &kms.DisableKeyInput{KeyId: aws.String(a.KeyId)})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("disable key: %w", err)
	}
	return &KmsKeyActionOutput{}, nil
}

// KmsEnableKeyRotation turns on automatic rotation for a key, the
// EnableKeyRotation operation, optionally setting the rotation period in days.
// A key still settling can report as not found or not yet enabled, so the call
// retries through both.
type KmsEnableKeyRotation struct {
	KeyId                string `ub:"key-id"`
	RotationPeriodInDays *int64 `ub:"rotation-period-in-days"`
}

// Constraints bounds the rotation period to the range KMS accepts.
func (a KmsEnableKeyRotation) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(a.RotationPeriodInDays)).
			Require(constraint.AtLeast(a.RotationPeriodInDays, 90),
				constraint.AtMost(a.RotationPeriodInDays, 2560)).
			Message("rotation-period-in-days must be between 90 and 2560"),
	}
}

func (a *KmsEnableKeyRotation) Run(ctx context.Context, cfg any) (*KmsKeyActionOutput, error) {
	client, err := kmshelpers.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &kms.EnableKeyRotationInput{
		KeyId:                aws.String(a.KeyId),
		RotationPeriodInDays: ptr.Int32(a.RotationPeriodInDays),
	}
	err = retry.OnError(ctx, kmsRotationRetryable, func(ctx context.Context) error {
		_, err := client.EnableKeyRotation(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("enable key rotation: %w", err)
	}
	return &KmsKeyActionOutput{}, nil
}

// KmsDisableKeyRotation turns off automatic rotation for a key, the
// DisableKeyRotation operation, retrying through the same settling states as
// enabling it.
type KmsDisableKeyRotation struct {
	KeyId string `ub:"key-id"`
}

func (a *KmsDisableKeyRotation) Run(ctx context.Context, cfg any) (*KmsKeyActionOutput, error) {
	client, err := kmshelpers.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	err = retry.OnError(ctx, kmsRotationRetryable, func(ctx context.Context) error {
		_, err := client.DisableKeyRotation(ctx, &kms.DisableKeyRotationInput{
			KeyId: aws.String(a.KeyId),
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("disable key rotation: %w", err)
	}
	return &KmsKeyActionOutput{}, nil
}

// kmsRotationRetryable reports whether a rotation call error clears on its own:
// the key not yet propagated, or still settling into the enabled state.
func kmsRotationRetryable(err error) bool {
	return kmshelpers.IsNotFound(err) || kmshelpers.IsDisabled(err)
}
