package kms

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/cloudboss/unobin/pkg/constraint"
)

const (
	lookupUUID = `[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[ab89][0-9a-f]{3}-[0-9a-f]{12}`
	lookupMRK  = `mrk-[0-9a-f]{32}`
)

var (
	lookupIDPattern    = regexp.MustCompile(`^` + lookupUUID + `|` + lookupMRK + `$`)
	lookupARNPattern   = regexp.MustCompile(`^key/(` + lookupUUID + `|` + lookupMRK + `)$`)
	lookupAliasPattern = regexp.MustCompile(aliasNamePattern)
)

type KeyDataSource struct {
	KeyID       string    `ub:"key-id"`
	GrantTokens *[]string `ub:"grant-tokens"`
}

type KeyDataSourceOutput struct {
	KeyID                       string                `ub:"key-id"`
	Arn                         string                `ub:"arn"`
	AWSAccountID                string                `ub:"aws-account-id"`
	CloudHSMClusterID           string                `ub:"cloud-hsm-cluster-id"`
	CreationDate                string                `ub:"creation-date"`
	CustomKeyStoreID            string                `ub:"custom-key-store-id"`
	DeletionDate                string                `ub:"deletion-date"`
	Description                 string                `ub:"description"`
	ExpirationModel             string                `ub:"expiration-model"`
	KeyManager                  string                `ub:"key-manager"`
	KeySpec                     string                `ub:"key-spec"`
	KeyState                    string                `ub:"key-state"`
	KeyUsage                    string                `ub:"key-usage"`
	Origin                      string                `ub:"origin"`
	ValidTo                     string                `ub:"valid-to"`
	Enabled                     bool                  `ub:"enabled"`
	MultiRegion                 bool                  `ub:"multi-region"`
	PendingDeletionWindowInDays int64                 `ub:"pending-deletion-window-in-days"`
	MultiRegionConfiguration    *keyLookupMultiRegion `ub:"multi-region-configuration"`
	XKSKeyConfiguration         *keyLookupXKS         `ub:"xks-key-configuration"`
}

type keyLookupMultiRegion struct {
	MultiRegionKeyType string              `ub:"multi-region-key-type"`
	PrimaryKey         *keyLookupRegional  `ub:"primary-key"`
	ReplicaKeys        []keyLookupRegional `ub:"replica-keys"`
}

type keyLookupRegional struct {
	Arn    string `ub:"arn"`
	Region string `ub:"region"`
}

type keyLookupXKS struct {
	ID string `ub:"id"`
}

func (r KeyDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.KeyID, nil)).Message("key-id is required"),
	}
}

type keyLookupClient interface {
	DescribeKey(context.Context, *kms.DescribeKeyInput, ...func(*kms.Options)) (
		*kms.DescribeKeyOutput, error,
	)
}

func (r *KeyDataSource) Read(
	ctx context.Context, cfg *awsCfg,
) (*KeyDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

func (r *KeyDataSource) prepareDescribeKey() (*kms.DescribeKeyInput, error) {
	valid := r.KeyID == "" || lookupIDPattern.MatchString(r.KeyID) ||
		lookupAliasPattern.MatchString(r.KeyID)
	if !valid {
		parsed, err := arn.Parse(r.KeyID)
		valid = err == nil && (lookupARNPattern.MatchString(parsed.Resource) ||
			strings.HasPrefix(parsed.Resource, "alias/"))
	}
	if !valid {
		return nil, fmt.Errorf("describe key %q: invalid key selector", r.KeyID)
	}
	in := &kms.DescribeKeyInput{KeyId: aws.String(r.KeyID)}
	if r.GrantTokens != nil && len(*r.GrantTokens) > 0 {
		in.GrantTokens = make([]string, 0, len(*r.GrantTokens))
		for _, token := range *r.GrantTokens {
			if token != "" {
				in.GrantTokens = append(in.GrantTokens, token)
			}
		}
	}
	return in, nil
}

func (r *KeyDataSource) read(
	ctx context.Context, client keyLookupClient,
) (*KeyDataSourceOutput, error) {
	in, err := r.prepareDescribeKey()
	if err != nil {
		return nil, err
	}
	response, err := client.DescribeKey(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("describe key %q: %w", r.KeyID, err)
	}
	if response == nil || response.KeyMetadata == nil {
		return nil, fmt.Errorf("describe key %q: no key metadata returned", r.KeyID)
	}
	key := response.KeyMetadata
	out := &KeyDataSourceOutput{
		KeyID: aws.ToString(key.KeyId), Arn: aws.ToString(key.Arn),
		AWSAccountID:      aws.ToString(key.AWSAccountId),
		CloudHSMClusterID: aws.ToString(key.CloudHsmClusterId),
		CreationDate:      aws.ToTime(key.CreationDate).Format(time.RFC3339),
		CustomKeyStoreID:  aws.ToString(key.CustomKeyStoreId),
		Description:       aws.ToString(key.Description), ExpirationModel: string(key.ExpirationModel),
		KeyManager: string(key.KeyManager), KeySpec: string(key.KeySpec),
		KeyState: string(key.KeyState), KeyUsage: string(key.KeyUsage), Origin: string(key.Origin),
		Enabled: key.Enabled, MultiRegion: aws.ToBool(key.MultiRegion),
		PendingDeletionWindowInDays: int64(aws.ToInt32(key.PendingDeletionWindowInDays)),
	}
	if key.DeletionDate != nil {
		out.DeletionDate = key.DeletionDate.Format(time.RFC3339)
	}
	if key.ValidTo != nil {
		out.ValidTo = key.ValidTo.Format(time.RFC3339)
	}
	if cfg := key.MultiRegionConfiguration; cfg != nil {
		out.MultiRegionConfiguration = &keyLookupMultiRegion{
			MultiRegionKeyType: string(cfg.MultiRegionKeyType),
			ReplicaKeys:        make([]keyLookupRegional, 0, len(cfg.ReplicaKeys)),
		}
		if cfg.PrimaryKey != nil {
			out.MultiRegionConfiguration.PrimaryKey = &keyLookupRegional{
				Arn: aws.ToString(cfg.PrimaryKey.Arn), Region: aws.ToString(cfg.PrimaryKey.Region),
			}
		}
		for _, replica := range cfg.ReplicaKeys {
			out.MultiRegionConfiguration.ReplicaKeys = append(
				out.MultiRegionConfiguration.ReplicaKeys, keyLookupRegional{
					Arn: aws.ToString(replica.Arn), Region: aws.ToString(replica.Region),
				})
		}
	}
	if key.XksKeyConfiguration != nil {
		out.XKSKeyConfiguration = &keyLookupXKS{ID: aws.ToString(key.XksKeyConfiguration.Id)}
	}
	return out, nil
}
