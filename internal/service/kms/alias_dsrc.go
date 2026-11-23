package kms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/constraint"
)

const aliasNamePattern = `^alias/[0-9A-Za-z_/-]+$`

type AliasDataSource struct {
	AliasName string `ub:"alias-name"`
}

type AliasDataSourceOutput struct {
	AliasName    string `ub:"alias-name"`
	Arn          string `ub:"arn"`
	TargetKeyArn string `ub:"target-key-arn"`
	TargetKeyID  string `ub:"target-key-id"`
}

func (r AliasDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.AliasName, nil)).
			Message("alias-name is required"),
		constraint.Must(constraint.IsMatch(r.AliasName, aliasNamePattern)).
			Message("alias-name must start with alias/ and end with one or more " +
				"ASCII letters, digits, underscores, slashes, or hyphens"),
	}
}

type aliasLookupClient interface {
	kms.ListAliasesAPIClient
	DescribeKey(context.Context, *kms.DescribeKeyInput, ...func(*kms.Options)) (
		*kms.DescribeKeyOutput, error,
	)
}

func (r *AliasDataSource) Read(
	ctx context.Context, cfg *awsCfg,
) (*AliasDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

func (r *AliasDataSource) read(
	ctx context.Context, client aliasLookupClient,
) (*AliasDataSourceOutput, error) {
	pages := kms.NewListAliasesPaginator(client, &kms.ListAliasesInput{})
	var match kmstypes.AliasListEntry
	matches := 0
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list aliases for %q: %w", r.AliasName, err)
		}
		for _, entry := range page.Aliases {
			if aws.ToString(entry.AliasName) == r.AliasName {
				match = entry
				matches++
			}
		}
	}
	switch matches {
	case 0:
		return nil, fmt.Errorf("list aliases for %q: no exact match", r.AliasName)
	case 1:
	default:
		return nil, fmt.Errorf("list aliases for %q: multiple exact matches", r.AliasName)
	}

	out, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: aws.String(r.AliasName)})
	if err != nil {
		return nil, fmt.Errorf("describe key for alias %q: %w", r.AliasName, err)
	}
	if out == nil || out.KeyMetadata == nil {
		return nil, fmt.Errorf("describe key for alias %q: no key metadata returned", r.AliasName)
	}
	key := out.KeyMetadata
	switch key.KeyState {
	case kmstypes.KeyStatePendingDeletion, kmstypes.KeyStatePendingReplicaDeletion:
		return nil, fmt.Errorf("describe key for alias %q: key is in state %s",
			r.AliasName, key.KeyState)
	}
	return &AliasDataSourceOutput{
		AliasName:    aws.ToString(match.AliasName),
		Arn:          aws.ToString(match.AliasArn),
		TargetKeyArn: aws.ToString(key.Arn),
		TargetKeyID:  aws.ToString(key.KeyId),
	}, nil
}
