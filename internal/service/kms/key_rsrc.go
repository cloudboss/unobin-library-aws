package kms

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	kms "github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// policyNameDefault is the only key-policy name KMS accepts. A key has one
// policy, named "default"; KMS rejects any other name. The policy write call
// takes this name implicitly, so it is fixed here rather than exposed as an
// input.
const policyNameDefault = "default"

// Key manages a KMS key: the protected key material plus the policy that
// governs who may use it. The key spec, key usage, custom key store, external
// key, and multi-Region flag are fixed at create time, so a change to any of
// them replaces the key; the policy, description, and tags change in place.
// Whether the key is enabled and whether it rotates are distinct KMS operations
// with no create-time setting, so they are optional fields applied after the
// key exists: an unset enable-key or enable-key-rotation leaves the AWS default
// (created enabled, rotation off), and a set value is reconciled by enabling or
// disabling the key or its rotation.
type Key struct {
	Policy                         *string            `ub:"policy"`
	BypassPolicyLockoutSafetyCheck *bool              `ub:"bypass-policy-lockout-safety-check"`
	Description                    *string            `ub:"description"`
	KeySpec                        *string            `ub:"key-spec"`
	KeyUsage                       *string            `ub:"key-usage"`
	CustomKeyStoreId               *string            `ub:"custom-key-store-id"`
	XksKeyId                       *string            `ub:"xks-key-id"`
	MultiRegion                    *bool              `ub:"multi-region"`
	EnableKey                      *bool              `ub:"enable-key"`
	EnableKeyRotation              *bool              `ub:"enable-key-rotation"`
	RotationPeriodInDays           *int64             `ub:"rotation-period-in-days"`
	Tags                           *map[string]string `ub:"tags"`
}

// KeyOutput holds the values KMS computes for a key. The ARN is the key's
// identity in policies and grants; the key id is the stable handle used to
// read, update, and delete it.
type KeyOutput struct {
	Arn   string `ub:"arn"`
	KeyId string `ub:"key-id"`
}

func (r *Key) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs KMS fixes when a key is created. The key spec,
// key usage, origin (set by a custom key store or an external key), and the
// multi-Region flag cannot be changed on an existing key, so a change to any of
// them requires a new key. Every other input is reconciled in place by Update.
func (r *Key) ReplaceFields() []string {
	return []string{
		"key-spec",
		"key-usage",
		"custom-key-store-id",
		"xks-key-id",
		"multi-region",
	}
}

// Constraints declares the rules KMS places on a key's inputs. An external key
// belongs to an external key store, so an xks key id requires a custom key
// store id. The key spec and key usage each accept a fixed set of values. A
// rotation period applies only when rotation is enabled and must be between 90
// and 2560 days.
func (r Key) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.RequiredWith(r.XksKeyId, r.CustomKeyStoreId),
		constraint.When(constraint.Present(r.KeySpec)).
			Require(constraint.OneOf(r.KeySpec,
				"SYMMETRIC_DEFAULT",
				"RSA_2048", "RSA_3072", "RSA_4096",
				"ECC_NIST_P256", "ECC_NIST_P384", "ECC_NIST_P521",
				"ECC_SECG_P256K1", "ECC_NIST_EDWARDS25519",
				"HMAC_224", "HMAC_256", "HMAC_384", "HMAC_512",
				"ML_DSA_44", "ML_DSA_65", "ML_DSA_87",
				"SM2")).
			Message("key-spec must be a valid KMS key spec"),
		constraint.When(constraint.Present(r.KeyUsage)).
			Require(constraint.OneOf(r.KeyUsage,
				"ENCRYPT_DECRYPT", "SIGN_VERIFY", "GENERATE_VERIFY_MAC", "KEY_AGREEMENT")).
			Message("key-usage must be a valid KMS key usage"),
		constraint.RequiredWith(r.RotationPeriodInDays, r.EnableKeyRotation),
		constraint.When(constraint.Present(r.RotationPeriodInDays)).
			Require(constraint.AtLeast(r.RotationPeriodInDays, 90),
				constraint.AtMost(r.RotationPeriodInDays, 2560)).
			Message("rotation-period-in-days must be between 90 and 2560"),
	}
}

func (r *Key) Create(ctx context.Context, cfg *awsCfg) (*KeyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &kms.CreateKeyInput{
		BypassPolicyLockoutSafetyCheck: aws.ToBool(r.BypassPolicyLockoutSafetyCheck),
		Description:                    r.Description,
		MultiRegion:                    r.MultiRegion,
		Policy:                         r.Policy,
		Tags:                           kmsKeyTags(ptr.Value(r.Tags)),
	}
	if r.KeySpec != nil {
		in.KeySpec = kmstypes.KeySpec(*r.KeySpec)
	}
	if r.KeyUsage != nil {
		in.KeyUsage = kmstypes.KeyUsageType(*r.KeyUsage)
	}
	// A custom key store points the key at a CloudHSM cluster; an external key
	// points it at a key held outside AWS. Either one sets the origin to match,
	// since KMS infers neither from the identifier alone.
	if r.CustomKeyStoreId != nil {
		in.Origin = kmstypes.OriginTypeAwsCloudhsm
		in.CustomKeyStoreId = r.CustomKeyStoreId
	}
	if r.XksKeyId != nil {
		in.Origin = kmstypes.OriginTypeExternalKeyStore
		in.XksKeyId = r.XksKeyId
	}
	// A key policy may name a principal that was created moments earlier, and
	// KMS is eventually consistent about the principals it knows, so a create
	// carrying such a policy is rejected as malformed until the principal is
	// visible. Retry through that window.
	var resp *kms.CreateKeyOutput
	err = retry.OnError(ctx, isMalformedPolicy, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateKey(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create key: %w", err)
	}
	keyID := aws.ToString(resp.KeyMetadata.KeyId)
	// A key is created enabled with rotation off, so only the inputs that differ
	// from those defaults need a call. Rotation is set while the key is still
	// enabled; a disable is left until after, so it cannot get in the way.
	if r.EnableKeyRotation != nil && *r.EnableKeyRotation {
		if err := r.enableRotation(ctx, client, keyID); err != nil {
			return nil, err
		}
	}
	if r.EnableKey != nil && !*r.EnableKey {
		if err := r.disableKey(ctx, client, keyID); err != nil {
			return nil, err
		}
	}
	// Read settles the eventual consistency that follows a create: KMS can
	// briefly report the just-made key as absent, and a later plan that read it
	// absent would take it for deleted and recreate it. Waiting here for it to
	// become visible keeps the next read truthful.
	return r.read(ctx, client, keyID, true)
}

func (r *Key) Read(ctx context.Context, cfg *awsCfg, prior *KeyOutput) (*KeyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.KeyId, false)
}

// read fetches the key by id and returns its computed outputs. When created is
// true the key was just made, so a not-found means it has not propagated yet
// and read waits for it; otherwise a not-found is drift and maps to
// runtime.ErrNotFound at once. A key already scheduled for deletion, in either
// the pending-deletion or pending-replica-deletion state, is logically gone and
// also reads as runtime.ErrNotFound so a plan recreates it.
func (r *Key) read(
	ctx context.Context, client *kms.Client, keyID string, created bool,
) (*KeyOutput, error) {
	var metadata *kmstypes.KeyMetadata
	err := wait.Until(ctx, fmt.Sprintf("key %s", keyID),
		func(ctx context.Context) (bool, error) {
			resp, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{
				KeyId: aws.String(keyID),
			})
			if err != nil {
				if isNotFound(err) {
					if created {
						return false, nil
					}
					return false, runtime.ErrNotFound
				}
				return false, fmt.Errorf("describe key: %w", err)
			}
			if resp.KeyMetadata == nil {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			switch resp.KeyMetadata.KeyState {
			case kmstypes.KeyStatePendingDeletion, kmstypes.KeyStatePendingReplicaDeletion:
				return false, runtime.ErrNotFound
			}
			metadata = resp.KeyMetadata
			return true, nil
		},
	)
	if err != nil {
		return nil, err
	}
	return &KeyOutput{
		Arn:   aws.ToString(metadata.Arn),
		KeyId: aws.ToString(metadata.KeyId),
	}, nil
}

func (r *Key) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Key, *KeyOutput],
) (*KeyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	keyID := prior.Outputs.KeyId
	// Rotation cannot be changed on a disabled key, so the key is enabled ahead
	// of any rotation change and disabled only after one.
	enableChanged := r.EnableKey != nil && runtime.Changed(prior.Inputs.EnableKey, r.EnableKey)
	if enableChanged && *r.EnableKey {
		if err := r.enableKey(ctx, client, keyID); err != nil {
			return nil, err
		}
	}
	// AWS keeps the description and policy until they are set again, so each is
	// reconciled only when present rather than cleared on removal. Tags are
	// reconciled as a set to match the desired map.
	if r.Description != nil && runtime.Changed(prior.Inputs.Description, r.Description) {
		_, err := client.UpdateKeyDescription(ctx, &kms.UpdateKeyDescriptionInput{
			KeyId:       aws.String(keyID),
			Description: r.Description,
		})
		if err != nil {
			return nil, fmt.Errorf("update key description: %w", err)
		}
	}
	if r.Policy != nil && runtime.Changed(prior.Inputs.Policy, r.Policy) {
		if err := r.putPolicy(ctx, client, keyID); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, keyID); err != nil {
			return nil, err
		}
	}
	// Rotation reconciles when its toggle or its period changed; the period is
	// only meaningful while rotation is on.
	if r.EnableKeyRotation != nil &&
		(runtime.Changed(prior.Inputs.EnableKeyRotation, r.EnableKeyRotation) ||
			runtime.Changed(prior.Inputs.RotationPeriodInDays, r.RotationPeriodInDays)) {
		if *r.EnableKeyRotation {
			if err := r.enableRotation(ctx, client, keyID); err != nil {
				return nil, err
			}
		} else {
			if err := r.disableRotation(ctx, client, keyID); err != nil {
				return nil, err
			}
		}
	}
	if enableChanged && !*r.EnableKey {
		if err := r.disableKey(ctx, client, keyID); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, keyID, false)
}

func (r *Key) Delete(ctx context.Context, cfg *awsCfg, prior *KeyOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// KMS does not delete a key at once; it schedules the deletion after a
	// recovery window, which AWS defaults to thirty days. Scheduling the
	// deletion is the delete; the length of the window is left to AWS.
	_, err = client.ScheduleKeyDeletion(ctx, &kms.ScheduleKeyDeletionInput{
		KeyId: aws.String(prior.KeyId),
	})
	if err != nil {
		// A key already gone, or already scheduled for deletion by an earlier
		// run, is on its way out either way and counts as deleted.
		if isNotFound(err) || isPendingDeletion(err) {
			return nil
		}
		return fmt.Errorf("schedule key deletion: %w", err)
	}
	return nil
}

// putPolicy writes the key policy. As with create, a policy naming a
// just-created principal can be rejected as malformed until that principal
// propagates, and a key still settling can read as not-found, so it retries
// through both.
func (r *Key) putPolicy(ctx context.Context, client *kms.Client, keyID string) error {
	in := &kms.PutKeyPolicyInput{
		KeyId:                          aws.String(keyID),
		Policy:                         r.Policy,
		PolicyName:                     aws.String(policyNameDefault),
		BypassPolicyLockoutSafetyCheck: aws.ToBool(r.BypassPolicyLockoutSafetyCheck),
	}
	err := retry.OnError(ctx, kmsPolicyRetryable, func(ctx context.Context) error {
		_, err := client.PutKeyPolicy(ctx, in)
		return err
	})
	if err != nil {
		return fmt.Errorf("put key policy: %w", err)
	}
	return nil
}

// enableKey enables the key, retrying while a just-created key is still
// propagating and reports as not found. That window clears in about a second,
// so the retry polls at that interval rather than the slower default.
func (r *Key) enableKey(ctx context.Context, client *kms.Client, keyID string) error {
	err := retry.OnError(ctx, isNotFound, func(ctx context.Context) error {
		_, err := client.EnableKey(ctx, &kms.EnableKeyInput{KeyId: aws.String(keyID)})
		return err
	}, retry.WithInterval(time.Second))
	if err != nil {
		return fmt.Errorf("enable key: %w", err)
	}
	return nil
}

// disableKey disables the key, retrying through the same not-found settling
// window as enableKey, at the same one-second interval.
func (r *Key) disableKey(ctx context.Context, client *kms.Client, keyID string) error {
	err := retry.OnError(ctx, isNotFound, func(ctx context.Context) error {
		_, err := client.DisableKey(ctx, &kms.DisableKeyInput{KeyId: aws.String(keyID)})
		return err
	}, retry.WithInterval(time.Second))
	if err != nil {
		return fmt.Errorf("disable key: %w", err)
	}
	return nil
}

// enableRotation turns on automatic rotation, setting the rotation period when
// one is given. A key still settling can report as not found or not yet
// enabled; that window clears fast, so the call retries through both at a
// one-second interval.
func (r *Key) enableRotation(ctx context.Context, client *kms.Client, keyID string) error {
	in := &kms.EnableKeyRotationInput{
		KeyId:                aws.String(keyID),
		RotationPeriodInDays: ptr.Int32(r.RotationPeriodInDays),
	}
	err := retry.OnError(ctx, kmsRotationRetryable, func(ctx context.Context) error {
		_, err := client.EnableKeyRotation(ctx, in)
		return err
	}, retry.WithInterval(time.Second))
	if err != nil {
		return fmt.Errorf("enable key rotation: %w", err)
	}
	return nil
}

// disableRotation turns off automatic rotation, retrying through the same
// settling states as enableRotation, at the same one-second interval.
func (r *Key) disableRotation(ctx context.Context, client *kms.Client, keyID string) error {
	err := retry.OnError(ctx, kmsRotationRetryable, func(ctx context.Context) error {
		_, err := client.DisableKeyRotation(ctx, &kms.DisableKeyRotationInput{
			KeyId: aws.String(keyID),
		})
		return err
	}, retry.WithInterval(time.Second))
	if err != nil {
		return fmt.Errorf("disable key rotation: %w", err)
	}
	return nil
}

// syncTags reconciles the key's tags with the desired set, reading the live
// tags through the paginated ListResourceTags and writing changes with
// TagResource and UntagResource. KMS addresses key tags by key id.
func (r *Key) syncTags(ctx context.Context, client *kms.Client, keyID string) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			current := map[string]string{}
			pager := kms.NewListResourceTagsPaginator(client, &kms.ListResourceTagsInput{
				KeyId: aws.String(keyID),
			})
			for pager.HasMorePages() {
				page, err := pager.NextPage(ctx)
				if err != nil {
					return nil, fmt.Errorf("list resource tags: %w", err)
				}
				for _, t := range page.Tags {
					current[aws.ToString(t.TagKey)] = aws.ToString(t.TagValue)
				}
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &kms.TagResourceInput{
				KeyId: aws.String(keyID),
				Tags:  kmsKeyTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &kms.UntagResourceInput{
				KeyId:   aws.String(keyID),
				TagKeys: remove,
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}

// kmsPolicyRetryable reports whether a policy call error is one that clears on
// its own: the key not yet propagated, or a principal named in the policy not
// yet visible to KMS.
func kmsPolicyRetryable(err error) bool {
	return isNotFound(err) || isMalformedPolicy(err)
}

// kmsRotationRetryable reports whether a rotation call error clears on its own:
// the key not yet propagated, or still settling into the enabled state.
func kmsRotationRetryable(err error) bool {
	return isNotFound(err) || isDisabled(err)
}

// kmsKeyTags converts a desired tag map into the KMS SDK tag list, ordered by
// key so the request is deterministic. KMS names the tag fields TagKey and
// TagValue, unlike most services.
func kmsKeyTags(tags map[string]string) []kmstypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]kmstypes.Tag, 0, len(tags))
	for _, k := range keys {
		out = append(out, kmstypes.Tag{TagKey: aws.String(k), TagValue: aws.String(tags[k])})
	}
	return out
}
