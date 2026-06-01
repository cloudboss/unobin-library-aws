// verify checks the KMS group the scenario applied against the phase named in
// the VERIFY_PHASE environment variable. A key has no stable name of its own,
// so applied finds it through the alias, since DescribeKey resolves an alias
// name to its key, and destroyed finds it through the marker tag the scenario
// set, since the alias is gone by then. It only reads cloud state: applied
// requires the key present and enabled with rotation on and the alias pointing
// at it; destroyed requires the alias gone and the key scheduled for deletion,
// which is as removed as a KMS key ever gets. Tearing the group down is the
// destroy plan's job, not the verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

const (
	aliasName   = "alias/unobin-it-key"
	markerKey   = "unobin"
	markerValue = "kms-it"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := kms.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *kms.Client) error {
	key, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{
		KeyId: aws.String(aliasName),
	})
	if err != nil {
		return fmt.Errorf("describe key via %s: %w", aliasName, err)
	}
	keyID := aws.ToString(key.KeyMetadata.KeyId)
	if key.KeyMetadata.KeyState != kmstypes.KeyStateEnabled {
		return fmt.Errorf("key %s is in state %s, want Enabled",
			keyID, key.KeyMetadata.KeyState)
	}

	rotation, err := client.GetKeyRotationStatus(ctx, &kms.GetKeyRotationStatusInput{
		KeyId: aws.String(keyID),
	})
	if err != nil {
		return fmt.Errorf("get key rotation status %s: %w", keyID, err)
	}
	if !rotation.KeyRotationEnabled {
		return fmt.Errorf("key %s rotation is not enabled", keyID)
	}

	target, err := aliasTarget(ctx, client)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("alias %s not found", aliasName)
	}
	if target != keyID {
		return fmt.Errorf("alias %s targets %s, want %s", aliasName, target, keyID)
	}

	fmt.Printf("ok: key %s enabled with rotation on, alias %s targets it\n",
		keyID, aliasName)
	return nil
}

func verifyDestroyed(ctx context.Context, client *kms.Client) error {
	target, err := aliasTarget(ctx, client)
	if err != nil {
		return err
	}
	if target != "" {
		return fmt.Errorf("alias %s still exists, targets %s", aliasName, target)
	}

	keyID, state, err := findMarkedKey(ctx, client)
	if err != nil {
		return err
	}
	switch {
	case keyID == "":
		// The key is fully gone; nothing more to check.
	case state == kmstypes.KeyStatePendingDeletion ||
		state == kmstypes.KeyStatePendingReplicaDeletion:
		// KMS never erases a key at once; scheduled for deletion is as gone as
		// a key gets within the test's window.
	default:
		return fmt.Errorf("key %s is in state %s, want pending deletion or gone",
			keyID, state)
	}

	fmt.Printf("ok: alias %s gone, key scheduled for deletion or removed\n", aliasName)
	return nil
}

// aliasTarget returns the bare key id the alias points at, or the empty string
// when no alias by that name exists.
func aliasTarget(ctx context.Context, client *kms.Client) (string, error) {
	pager := kms.NewListAliasesPaginator(client, &kms.ListAliasesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("list aliases: %w", err)
		}
		for _, a := range page.Aliases {
			if aws.ToString(a.AliasName) == aliasName {
				return aws.ToString(a.TargetKeyId), nil
			}
		}
	}
	return "", nil
}

// findMarkedKey returns the id and state of the key carrying the scenario's
// marker tag, or an empty id when none does. KMS has no lookup by tag, so it
// scans every key and reads each one's tags. A key that vanishes mid-scan is
// skipped.
func findMarkedKey(
	ctx context.Context, client *kms.Client,
) (string, kmstypes.KeyState, error) {
	pager := kms.NewListKeysPaginator(client, &kms.ListKeysInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return "", "", fmt.Errorf("list keys: %w", err)
		}
		for _, k := range page.Keys {
			keyID := aws.ToString(k.KeyId)
			if !keyHasMarker(ctx, client, keyID) {
				continue
			}
			described, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{
				KeyId: aws.String(keyID),
			})
			if err != nil {
				if isNotFound(err) {
					continue
				}
				return "", "", fmt.Errorf("describe key %s: %w", keyID, err)
			}
			return keyID, described.KeyMetadata.KeyState, nil
		}
	}
	return "", "", nil
}

// keyHasMarker reports whether the key carries the scenario's marker tag. A key
// that vanishes mid-scan is treated as unmarked.
func keyHasMarker(ctx context.Context, client *kms.Client, keyID string) bool {
	pager := kms.NewListResourceTagsPaginator(client, &kms.ListResourceTagsInput{
		KeyId: aws.String(keyID),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return false
		}
		for _, t := range page.Tags {
			if aws.ToString(t.TagKey) == markerKey &&
				aws.ToString(t.TagValue) == markerValue {
				return true
			}
		}
	}
	return false
}

func isNotFound(err error) bool {
	var notFound *kmstypes.NotFoundException
	return errors.As(err, &notFound)
}
