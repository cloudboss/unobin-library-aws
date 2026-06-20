package kms

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	kms "github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// Alias manages a friendly name that points at a KMS key. The alias name
// is the alias's identity: KMS has no rename, so a change to it replaces the
// alias. The target key can be re-pointed in place, and accepts either a key
// id or a key ARN, which KMS treats as the same key.
type Alias struct {
	AliasName   string `ub:"alias-name"`
	TargetKeyId string `ub:"target-key-id"`
}

// AliasOutput holds the values KMS computes for an alias. Arn is the
// alias's own ARN; TargetKeyArn is the ARN of the key it points at, which KMS
// reports only as a bare key id, so it is reconstructed from the alias ARN.
type AliasOutput struct {
	Arn          string `ub:"arn"`
	TargetKeyArn string `ub:"target-key-arn"`
}

func (r *Alias) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs KMS cannot change on an existing alias. The
// alias name is the alias's identity and KMS offers no rename, so a change to
// it requires replacing the alias.
func (r *Alias) ReplaceFields() []string {
	return []string{"alias-name"}
}

func (r *Alias) Create(ctx context.Context, cfg *awsCfg) (*AliasOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &kms.CreateAliasInput{
		AliasName:   aws.String(r.AliasName),
		TargetKeyId: aws.String(r.TargetKeyId),
	}
	// The target key may have been created moments earlier and not yet
	// propagated, which CreateAlias reports as not-found; retry until KMS sees
	// it. Key propagation can take minutes, so the window is generous, matching
	// the condition the Terraform provider retries with its typed
	// NotFoundException check.
	err = retry.OnError(ctx, isNotFound, func(ctx context.Context) error {
		_, err := client.CreateAlias(ctx, in)
		return err
	}, retry.WithTimeout(10*time.Minute))
	if err != nil {
		return nil, fmt.Errorf("create alias: %w", err)
	}
	// Read settles the eventual consistency that follows a create: KMS can
	// briefly omit the just-made alias from a list, and a later plan that read
	// it absent would take it for deleted and recreate it. Waiting here for it
	// to become visible keeps the next read truthful.
	return r.read(ctx, client, r.AliasName, true)
}

func (r *Alias) Read(
	ctx context.Context, cfg *awsCfg, prior *AliasOutput,
) (*AliasOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The alias name is the alias's identity and the prior output does not
	// carry it, so the read keys off the receiver, which holds the name.
	return r.read(ctx, client, r.AliasName, false)
}

// read lists aliases and returns the computed outputs for the one named name.
// When created is true the alias was just made, so a not-yet-listed alias is
// still propagating and read waits for it; otherwise a missing alias is drift
// and maps to runtime.ErrNotFound.
func (r *Alias) read(
	ctx context.Context, client *kms.Client, name string, created bool,
) (*AliasOutput, error) {
	var entry *kmstypes.AliasListEntry
	err := wait.Until(ctx, fmt.Sprintf("alias %s", name),
		func(ctx context.Context) (bool, error) {
			found, err := findAlias(ctx, client, name)
			if err != nil {
				return false, fmt.Errorf("list aliases: %w", err)
			}
			if found == nil {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			entry = found
			return true, nil
		},
	)
	if err != nil {
		return nil, err
	}
	aliasARN := aws.ToString(entry.AliasArn)
	keyARN, err := keyARN(aliasARN, aws.ToString(entry.TargetKeyId))
	if err != nil {
		return nil, err
	}
	return &AliasOutput{Arn: aliasARN, TargetKeyArn: keyARN}, nil
}

func (r *Alias) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Alias, *AliasOutput],
) (*AliasOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The alias name is replace-only, so the only in-place change is the
	// target key.
	if runtime.Changed(prior.Inputs.TargetKeyId, r.TargetKeyId) {
		_, err := client.UpdateAlias(ctx, &kms.UpdateAliasInput{
			AliasName:   aws.String(r.AliasName),
			TargetKeyId: aws.String(r.TargetKeyId),
		})
		if err != nil {
			return nil, fmt.Errorf("update alias: %w", err)
		}
	}
	return r.read(ctx, client, r.AliasName, false)
}

func (r *Alias) Delete(ctx context.Context, cfg *awsCfg, prior *AliasOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteAlias(ctx, &kms.DeleteAliasInput{
		AliasName: aws.String(r.AliasName),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete alias: %w", err)
	}
	return nil
}

// findAlias returns the alias entry whose name matches name, or nil when no
// alias has that name. KMS has no get-by-name call, so it pages through every
// alias in the account and region.
func findAlias(
	ctx context.Context, client *kms.Client, name string,
) (*kmstypes.AliasListEntry, error) {
	pages := kms.NewListAliasesPaginator(client, &kms.ListAliasesInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for i := range page.Aliases {
			if aws.ToString(page.Aliases[i].AliasName) == name {
				return &page.Aliases[i], nil
			}
		}
	}
	return nil, nil
}

// keyARN reconstructs the ARN of the key an alias points at. KMS reports the
// target only as a bare key id, so the key ARN is built from the alias ARN,
// which shares the key's partition, region, and account, with the resource
// replaced by key/<id>. This mirrors the Terraform provider's aliasARNToKeyARN.
func keyARN(aliasARN, targetKeyID string) (string, error) {
	parsed, err := arn.Parse(aliasARN)
	if err != nil {
		return "", fmt.Errorf("parse alias arn %s: %w", aliasARN, err)
	}
	return arn.ARN{
		Partition: parsed.Partition,
		Service:   parsed.Service,
		Region:    parsed.Region,
		AccountID: parsed.AccountID,
		Resource:  "key/" + targetKeyID,
	}.String(), nil
}
