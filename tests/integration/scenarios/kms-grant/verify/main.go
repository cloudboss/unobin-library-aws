package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
)

type identities struct {
	KeyID     string `json:"key-id"`
	Principal string `json:"principal"`
	Revoked   string `json:"revoked"`
	Retired   string `json:"retired"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := kms.NewFromConfig(cfg)
	phase := os.Getenv("VERIFY_PHASE")
	build := os.Getenv("VERIFY_BUILD_DIR")
	path := filepath.Join(build, "grant-identities.json")
	var old identities
	if phase != "applied" {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &old); err != nil {
			return err
		}
	}
	if phase == "destroyed" {
		page, err := listGrants(ctx, client, old.KeyID)
		var missing *types.NotFoundException
		if err != nil && !errors.As(err, &missing) {
			return err
		}
		for _, grant := range page {
			if aws.ToString(grant.GrantId) == old.Retired || aws.ToString(grant.GrantId) == old.Revoked {
				return fmt.Errorf("grant %s remains after destruction", aws.ToString(grant.GrantId))
			}
		}
		key, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: aws.String(old.KeyID)})
		if err != nil && !errors.As(err, &missing) {
			return err
		}
		if err == nil && (key.KeyMetadata == nil ||
			key.KeyMetadata.KeyState != types.KeyStatePendingDeletion) {
			return fmt.Errorf("key %s is not scheduled for deletion", old.KeyID)
		}
		_, err = iam.NewFromConfig(cfg).GetUser(ctx, &iam.GetUserInput{
			UserName: aws.String("unobin-kms-grant-it"),
		})
		var noUser *iamtypes.NoSuchEntityException
		if !errors.As(err, &noUser) {
			return fmt.Errorf("principal still exists: %v", err)
		}
		return nil
	}
	if phase != "applied" && phase != "updated" {
		return fmt.Errorf("invalid phase %q", phase)
	}
	store, err := local.NewStore(filepath.Join(build, ".unobin", "state"),
		"kms-grant", "config", encrypters.Noop{})
	if err != nil {
		return err
	}
	snapshot, err := store.Current()
	if err != nil {
		return err
	}
	ids := identities{KeyID: snapshot.Outputs["key-id"].(string),
		Principal: snapshot.Outputs["principal"].(string), Revoked: snapshot.Outputs["revoked"].(string),
		Retired: snapshot.Outputs["retired"].(string)}
	grants, err := listGrants(ctx, client, ids.KeyID)
	if err != nil {
		return err
	}
	if err := verifyGrants(snapshot, ids, old, grants, phase == "updated"); err != nil {
		return err
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func listGrants(
	ctx context.Context, client *kms.Client, key string,
) ([]types.GrantListEntry, error) {
	var entries []types.GrantListEntry
	pager := kms.NewListGrantsPaginator(client, &kms.ListGrantsInput{
		KeyId: aws.String(key), Limit: aws.Int32(100),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		entries = append(entries, page.Grants...)
	}
	return entries, nil
}

func verifyGrants(snapshot *state.Snapshot, ids, old identities,
	grants []types.GrantListEntry, updated bool,
) error {
	operations := []types.GrantOperation{types.GrantOperationEncrypt}
	stage := "created"
	if updated {
		operations = []types.GrantOperation{"Decrypt", "Encrypt"}
		stage = "updated"
	}
	for _, expected := range []struct {
		name, id string
		retire   bool
	}{
		{"revoked", ids.Revoked, false}, {"retired", ids.Retired, true},
	} {
		managed := snapshot.Find("resource." + expected.name)
		if managed == nil {
			return fmt.Errorf("missing managed %s grant", expected.name)
		}
		if managed.Outputs["grant-id"] != expected.id ||
			managed.Outputs["key-id"] != managed.Inputs["key-id"] ||
			managed.Outputs["grantee-principal"] != ids.Principal {
			return fmt.Errorf("managed %s grant disagrees with identity", expected.name)
		}
		var found *types.GrantListEntry
		for _, grant := range grants {
			id := aws.ToString(grant.GrantId)
			if updated && (id == old.Revoked || id == old.Retired) {
				return fmt.Errorf("old grant %s remains after replacement", id)
			}
			if id == expected.id {
				found = &grant
			}
		}
		if found == nil {
			return fmt.Errorf("grant %s not found", expected.id)
		}
		actual := slices.Clone(found.Operations)
		slices.Sort(actual)
		if !slices.Equal(actual, operations) || aws.ToString(found.GranteePrincipal) != ids.Principal ||
			aws.ToString(found.Name) != "unobin-grant-"+expected.name || found.Constraints == nil {
			return fmt.Errorf("grant %s remote attributes disagree", expected.id)
		}
		context := found.Constraints.EncryptionContextEquals
		if expected.retire {
			context = found.Constraints.EncryptionContextSubset
			if aws.ToString(found.RetiringPrincipal) != managed.Inputs["retiring-principal"] {
				return fmt.Errorf("grant %s retiring principal disagrees", expected.id)
			}
		}
		if len(context) != 1 || context["stage"] != stage {
			return fmt.Errorf("grant %s context is %v, want stage %s", expected.id, context, stage)
		}
	}
	return nil
}
