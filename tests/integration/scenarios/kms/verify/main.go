package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/state/local"
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
		return verifyApplied(ctx, client, "unobin integration test key", true)
	case "updated":
		return verifyApplied(ctx, client, "unobin integration test key, updated", false)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied, updated, or destroyed, got %q", phase)
	}
}

func verifyApplied(
	ctx context.Context, client *kms.Client, wantDescription string, wantRotation bool,
) error {
	key, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{
		KeyId: aws.String(aliasName),
	})
	if err != nil {
		return fmt.Errorf("describe key via %s: %w", aliasName, err)
	}
	if key.KeyMetadata == nil {
		return fmt.Errorf("describe key via %s returned no metadata", aliasName)
	}
	keyID := aws.ToString(key.KeyMetadata.KeyId)
	if key.KeyMetadata.KeyState != kmstypes.KeyStateEnabled {
		return fmt.Errorf("key %s is in state %s, want Enabled",
			keyID, key.KeyMetadata.KeyState)
	}
	if description := aws.ToString(key.KeyMetadata.Description); description != wantDescription {
		return fmt.Errorf("key %s description is %q, want %q", keyID, description, wantDescription)
	}

	rotation, err := client.GetKeyRotationStatus(ctx, &kms.GetKeyRotationStatusInput{
		KeyId: aws.String(keyID),
	})
	if err != nil {
		return fmt.Errorf("get key rotation status %s: %w", keyID, err)
	}
	if rotation.KeyRotationEnabled != wantRotation {
		return fmt.Errorf("key %s rotation enabled is %t, want %t",
			keyID, rotation.KeyRotationEnabled, wantRotation)
	}

	alias, err := findAlias(ctx, client)
	if err != nil {
		return err
	}
	if alias == nil {
		return fmt.Errorf("alias %s not found", aliasName)
	}
	if target := aws.ToString(alias.TargetKeyId); target != keyID {
		return fmt.Errorf("alias %s targets %s, want %s", aliasName, target, keyID)
	}

	if err := verifyAliasLookup(alias, key.KeyMetadata); err != nil {
		return err
	}
	fmt.Printf("ok: key %s rotation enabled is %t, alias %s lookup agrees with KMS\n",
		keyID, wantRotation, aliasName)
	return nil
}

func verifyAliasLookup(alias *kmstypes.AliasListEntry, key *kmstypes.KeyMetadata) error {
	root := filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), ".unobin", "state")
	store, err := local.NewStore(root, "kms", "config", encrypters.Noop{})
	if err != nil {
		return fmt.Errorf("open scenario state: %w", err)
	}
	snapshot, err := store.Current()
	if err != nil {
		return fmt.Errorf("read scenario state: %w", err)
	}
	managed := snapshot.Find("resource.kms-alias-it")
	lookup := snapshot.Find("data-source.kms-alias-lookup")
	if managed == nil || lookup == nil {
		return fmt.Errorf("scenario state requires both the managed alias and its lookup")
	}
	for _, field := range []struct {
		name    string
		output  string
		managed any
		cloud   string
	}{
		{"alias-name", "lookup-alias-name", managed.Inputs["alias-name"],
			aws.ToString(alias.AliasName)},
		{"arn", "lookup-arn", managed.Outputs["arn"],
			aws.ToString(alias.AliasArn)},
		{"target-key-arn", "lookup-target-key-arn", managed.Outputs["target-key-arn"],
			aws.ToString(key.Arn)},
		{"target-key-id", "lookup-target-key-id", managed.Inputs["target-key-id"],
			aws.ToString(key.KeyId)},
	} {
		if got := lookup.Outputs[field.name]; got != field.cloud || got != field.managed {
			return fmt.Errorf("alias lookup %s is %v, want KMS %q and managed alias %v",
				field.name, got, field.cloud, field.managed)
		}
		if got := snapshot.Outputs[field.output]; got != field.cloud {
			return fmt.Errorf("stack output %s is %v, want %q", field.output, got, field.cloud)
		}
	}
	return nil
}

func verifyDestroyed(ctx context.Context, client *kms.Client) error {
	alias, err := findAlias(ctx, client)
	if err != nil {
		return err
	}
	if alias != nil {
		return fmt.Errorf("alias %s still exists, targets %s",
			aliasName, aws.ToString(alias.TargetKeyId))
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

func findAlias(ctx context.Context, client *kms.Client) (*kmstypes.AliasListEntry, error) {
	pager := kms.NewListAliasesPaginator(client, &kms.ListAliasesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list aliases: %w", err)
		}
		for _, a := range page.Aliases {
			if aws.ToString(a.AliasName) == aliasName {
				return &a, nil
			}
		}
	}
	return nil, nil
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
