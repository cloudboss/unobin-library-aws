package resources

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	kms "github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/library/internal/kmshelpers"
	"github.com/cloudboss/unobin-library-aws/library/internal/retry"
	"github.com/cloudboss/unobin-library-aws/library/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/library/internal/wait"
)

// policyNameDefault is the only key-policy name KMS accepts. A key has one
// policy, named "default"; KMS rejects any other name. The policy write call
// takes this name implicitly, so it is fixed here rather than exposed as an
// input.
const policyNameDefault = "default"

// KmsKey manages a KMS key: the protected key material plus the policy that
// governs who may use it. The key spec, key usage, custom key store, external
// key, and multi-Region flag are fixed at create time, so a change to any of
// them replaces the key; the policy, description, and tags change in place.
// Enabling, disabling, and rotation are separate KMS operations offered as
// actions rather than fields: a key is not declared enabled or rotating at
// create time, it is created enabled and toggled afterward.
type KmsKey struct {
	Policy                         *string           `ub:"policy"`
	BypassPolicyLockoutSafetyCheck *bool             `ub:"bypass-policy-lockout-safety-check"`
	Description                    *string           `ub:"description"`
	KeySpec                        *string           `ub:"key-spec"`
	KeyUsage                       *string           `ub:"key-usage"`
	CustomKeyStoreId               *string           `ub:"custom-key-store-id"`
	XksKeyId                       *string           `ub:"xks-key-id"`
	MultiRegion                    *bool             `ub:"multi-region"`
	Tags                           map[string]string `ub:"tags"`
}

// KmsKeyOutput holds the values KMS computes for a key. The ARN is the key's
// identity in policies and grants; the key id is the stable handle used to
// read, update, and delete it.
type KmsKeyOutput struct {
	Arn   string `ub:"arn"`
	KeyId string `ub:"key-id"`
}

func (r *KmsKey) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs KMS fixes when a key is created. The key spec,
// key usage, origin (set by a custom key store or an external key), and the
// multi-Region flag cannot be changed on an existing key, so a change to any of
// them requires a new key. Every other input is reconciled in place by Update.
func (r *KmsKey) ReplaceFields() []string {
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
// store id. The key spec and key usage each accept a fixed set of values.
func (r KmsKey) Constraints() []constraint.Constraint {
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
	}
}

func (r *KmsKey) Create(ctx context.Context, cfg any) (*KmsKeyOutput, error) {
	client, err := kmshelpers.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &kms.CreateKeyInput{
		BypassPolicyLockoutSafetyCheck: aws.ToBool(r.BypassPolicyLockoutSafetyCheck),
		Description:                    r.Description,
		MultiRegion:                    r.MultiRegion,
		Policy:                         r.Policy,
		Tags:                           kmsKeyTags(r.Tags),
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
	err = retry.OnError(ctx, kmshelpers.IsMalformedPolicy, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateKey(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create key: %w", err)
	}
	// Read settles the eventual consistency that follows a create: KMS can
	// briefly report the just-made key as absent, and a later plan that read it
	// absent would take it for deleted and recreate it. Waiting here for it to
	// become visible keeps the next read truthful.
	return r.read(ctx, client, aws.ToString(resp.KeyMetadata.KeyId), true)
}

func (r *KmsKey) Read(ctx context.Context, cfg any, prior *KmsKeyOutput) (*KmsKeyOutput, error) {
	client, err := kmshelpers.NewClient(ctx, cfg)
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
func (r *KmsKey) read(
	ctx context.Context, client *kms.Client, keyID string, created bool,
) (*KmsKeyOutput, error) {
	var metadata *kmstypes.KeyMetadata
	err := wait.Until(ctx, fmt.Sprintf("key %s", keyID),
		func(ctx context.Context) (bool, error) {
			resp, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{
				KeyId: aws.String(keyID),
			})
			if err != nil {
				if kmshelpers.IsNotFound(err) {
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
	return &KmsKeyOutput{
		Arn:   aws.ToString(metadata.Arn),
		KeyId: aws.ToString(metadata.KeyId),
	}, nil
}

func (r *KmsKey) Update(
	ctx context.Context, cfg any, prior runtime.Prior[KmsKey, *KmsKeyOutput],
) (*KmsKeyOutput, error) {
	client, err := kmshelpers.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	keyID := prior.Outputs.KeyId
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
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client, keyID); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, keyID, false)
}

func (r *KmsKey) Delete(ctx context.Context, cfg any, prior *KmsKeyOutput) error {
	client, err := kmshelpers.NewClient(ctx, cfg)
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
		if kmshelpers.IsNotFound(err) || kmshelpers.IsPendingDeletion(err) {
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
func (r *KmsKey) putPolicy(ctx context.Context, client *kms.Client, keyID string) error {
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

// syncTags reconciles the key's tags with the desired set, reading the live
// tags through the paginated ListResourceTags and writing changes with
// TagResource and UntagResource. KMS addresses key tags by key id.
func (r *KmsKey) syncTags(ctx context.Context, client *kms.Client, keyID string) error {
	return tagsync.Sync(ctx, r.Tags,
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
	return kmshelpers.IsNotFound(err) || kmshelpers.IsMalformedPolicy(err)
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
