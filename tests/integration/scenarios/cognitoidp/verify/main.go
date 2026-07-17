package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

const (
	primaryInitialName = "unobin-it-user-pool-initial"
	primaryUpdatedName = "unobin-it-user-pool-updated"
	clearPoolName      = "unobin-it-user-pool-clear"
)

type verifierClient interface {
	DescribeUserPool(
		context.Context,
		*cognitoidentityprovider.DescribeUserPoolInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.DescribeUserPoolOutput, error)
	GetUserPoolMfaConfig(
		context.Context,
		*cognitoidentityprovider.GetUserPoolMfaConfigInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.GetUserPoolMfaConfigOutput, error)
	ListTagsForResource(
		context.Context,
		*cognitoidentityprovider.ListTagsForResourceInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.ListTagsForResourceOutput, error)
	ListUserPools(
		context.Context,
		*cognitoidentityprovider.ListUserPoolsInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.ListUserPoolsOutput, error)
}

type expectedPool struct {
	name                 string
	tier                 cognitotypes.UserPoolTierType
	allowAdminCreateOnly bool
	tags                 map[string]string
}

type observedPool struct {
	id           string
	arn          string
	creationDate time.Time
}

type recordedPools struct {
	Primary poolIdentity `json:"primary"`
	Clear   poolIdentity `json:"clear"`
}

type poolIdentity struct {
	ID           string `json:"id"`
	ARN          string `json:"arn"`
	CreationDate string `json:"creation_date"`
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	configuration, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := cognitoidentityprovider.NewFromConfig(configuration)
	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "updated":
		return verifyUpdated(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf(
			"VERIFY_PHASE must be applied, updated, or destroyed, got %q",
			phase,
		)
	}
}

func verifyApplied(ctx context.Context, client verifierClient) error {
	primary, err := verifyPresent(ctx, client, expectedPool{
		name:                 primaryInitialName,
		tier:                 cognitotypes.UserPoolTierTypeEssentials,
		allowAdminCreateOnly: false,
		tags: map[string]string{
			"change": "old", "keep": "1", "remove": "yes",
		},
	})
	if err != nil {
		return err
	}
	clear, err := verifyPresent(ctx, client, expectedPool{
		name:                 clearPoolName,
		tier:                 cognitotypes.UserPoolTierTypeEssentials,
		allowAdminCreateOnly: false,
		tags:                 map[string]string{"clear": "yes"},
	})
	if err != nil {
		return err
	}
	return writeRecordedPools(recordedPools{
		Primary: identityFromObserved(primary),
		Clear:   identityFromObserved(clear),
	})
}

func verifyUpdated(ctx context.Context, client verifierClient) error {
	recorded, err := readRecordedPools()
	if err != nil {
		return err
	}
	primary, err := verifyPresent(ctx, client, expectedPool{
		name:                 primaryUpdatedName,
		tier:                 cognitotypes.UserPoolTierTypePlus,
		allowAdminCreateOnly: true,
		tags: map[string]string{
			"add": "yes", "change": "new", "keep": "1",
		},
	})
	if err != nil {
		return err
	}
	if err := verifyIdentity("primary", primary, recorded.Primary); err != nil {
		return err
	}
	clear, err := verifyPresent(ctx, client, expectedPool{
		name:                 clearPoolName,
		tier:                 cognitotypes.UserPoolTierTypeEssentials,
		allowAdminCreateOnly: false,
		tags:                 map[string]string{},
	})
	if err != nil {
		return err
	}
	return verifyIdentity("clear", clear, recorded.Clear)
}

func verifyPresent(
	ctx context.Context,
	client verifierClient,
	expected expectedPool,
) (observedPool, error) {
	id, err := findUserPoolID(ctx, client, expected.name)
	if err != nil {
		return observedPool{}, err
	}
	described, err := client.DescribeUserPool(
		ctx,
		&cognitoidentityprovider.DescribeUserPoolInput{UserPoolId: aws.String(id)},
	)
	if err != nil {
		return observedPool{}, fmt.Errorf("describe user pool %s: %w", expected.name, err)
	}
	if described == nil || described.UserPool == nil {
		return observedPool{}, fmt.Errorf("user pool %s returned no details", expected.name)
	}
	pool := described.UserPool
	if aws.ToString(pool.Id) != id {
		return observedPool{}, fmt.Errorf(
			"user pool %s ID is %q, want %q", expected.name, aws.ToString(pool.Id), id,
		)
	}
	if aws.ToString(pool.Name) != expected.name {
		return observedPool{}, fmt.Errorf(
			"user pool name is %q, want %q", aws.ToString(pool.Name), expected.name,
		)
	}
	arn := aws.ToString(pool.Arn)
	if arn == "" || !strings.HasSuffix(arn, ":userpool/"+id) {
		return observedPool{}, fmt.Errorf("user pool %s ARN is %q", expected.name, arn)
	}
	if pool.CreationDate == nil || pool.LastModifiedDate == nil {
		return observedPool{}, fmt.Errorf("user pool %s has incomplete timestamps", expected.name)
	}
	if pool.UserPoolTier != expected.tier {
		return observedPool{}, fmt.Errorf(
			"user pool %s tier is %q, want %q",
			expected.name,
			pool.UserPoolTier,
			expected.tier,
		)
	}
	if pool.DeletionProtection != cognitotypes.DeletionProtectionTypeInactive {
		return observedPool{}, fmt.Errorf(
			"user pool %s deletion protection is %q, want INACTIVE",
			expected.name,
			pool.DeletionProtection,
		)
	}
	allowAdminCreateOnly := false
	if pool.AdminCreateUserConfig != nil {
		allowAdminCreateOnly = pool.AdminCreateUserConfig.AllowAdminCreateUserOnly
	}
	if allowAdminCreateOnly != expected.allowAdminCreateOnly {
		return observedPool{}, fmt.Errorf(
			"user pool %s allow-admin-create-user-only is %t, want %t",
			expected.name,
			allowAdminCreateOnly,
			expected.allowAdminCreateOnly,
		)
	}
	if err := verifyDefaultMFA(ctx, client, id, expected.name); err != nil {
		return observedPool{}, err
	}
	if err := verifyTags(ctx, client, arn, expected.name, expected.tags); err != nil {
		return observedPool{}, err
	}
	fmt.Printf("ok: user pool %s matches the expected configuration\n", expected.name)
	return observedPool{
		id:           id,
		arn:          arn,
		creationDate: pool.CreationDate.UTC(),
	}, nil
}

func verifyDefaultMFA(
	ctx context.Context,
	client verifierClient,
	id string,
	name string,
) error {
	output, err := client.GetUserPoolMfaConfig(
		ctx,
		&cognitoidentityprovider.GetUserPoolMfaConfigInput{UserPoolId: aws.String(id)},
	)
	if err != nil {
		return fmt.Errorf("get user pool %s MFA configuration: %w", name, err)
	}
	if output == nil {
		return fmt.Errorf("user pool %s returned no MFA configuration", name)
	}
	if output.MfaConfiguration != cognitotypes.UserPoolMfaTypeOff {
		return fmt.Errorf(
			"user pool %s MFA configuration is %q, want OFF",
			name,
			output.MfaConfiguration,
		)
	}
	return nil
}

func verifyTags(
	ctx context.Context,
	client verifierClient,
	arn string,
	name string,
	expected map[string]string,
) error {
	output, err := client.ListTagsForResource(
		ctx,
		&cognitoidentityprovider.ListTagsForResourceInput{
			ResourceArn: aws.String(arn),
		},
	)
	if err != nil {
		return fmt.Errorf("list user pool %s tags: %w", name, err)
	}
	if output == nil {
		return fmt.Errorf("user pool %s returned no tag result", name)
	}
	actual := managedTags(output.Tags)
	if !maps.Equal(actual, expected) {
		return fmt.Errorf("user pool %s tags are %v, want %v", name, actual, expected)
	}
	return nil
}

func managedTags(tags map[string]string) map[string]string {
	managed := make(map[string]string, len(tags))
	for key, value := range tags {
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		managed[key] = value
	}
	return managed
}

func findUserPoolID(
	ctx context.Context,
	client verifierClient,
	name string,
) (string, error) {
	paginator := cognitoidentityprovider.NewListUserPoolsPaginator(
		client,
		&cognitoidentityprovider.ListUserPoolsInput{MaxResults: aws.Int32(60)},
	)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("list user pools: %w", err)
		}
		for _, pool := range page.UserPools {
			if aws.ToString(pool.Name) == name {
				id := aws.ToString(pool.Id)
				if id == "" {
					return "", fmt.Errorf("user pool %s has no ID", name)
				}
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("user pool %s was not found", name)
}

func verifyDestroyed(ctx context.Context, client verifierClient) error {
	recorded, err := readRecordedPools()
	if err != nil {
		return err
	}
	for name, identity := range map[string]poolIdentity{
		"primary": recorded.Primary,
		"clear":   recorded.Clear,
	} {
		_, err := client.DescribeUserPool(
			ctx,
			&cognitoidentityprovider.DescribeUserPoolInput{
				UserPoolId: aws.String(identity.ID),
			},
		)
		if err == nil {
			return fmt.Errorf("%s user pool %s still exists", name, identity.ID)
		}
		var notFound *cognitotypes.ResourceNotFoundException
		if !errors.As(err, &notFound) {
			return fmt.Errorf("describe destroyed %s user pool: %w", name, err)
		}
	}
	fmt.Println("ok: Cognito user pools are gone")
	return nil
}

func identityFromObserved(pool observedPool) poolIdentity {
	return poolIdentity{
		ID:           pool.id,
		ARN:          pool.arn,
		CreationDate: pool.creationDate.Format(time.RFC3339Nano),
	}
}

func verifyIdentity(name string, observed observedPool, expected poolIdentity) error {
	actual := identityFromObserved(observed)
	if actual != expected {
		return fmt.Errorf("%s user pool identity is %+v, want %+v", name, actual, expected)
	}
	return nil
}

func writeRecordedPools(pools recordedPools) error {
	data, err := json.Marshal(pools)
	if err != nil {
		return fmt.Errorf("encode user pool identities: %w", err)
	}
	if err := os.WriteFile(recordedPoolsPath(), data, 0o600); err != nil {
		return fmt.Errorf("record user pool identities: %w", err)
	}
	return nil
}

func readRecordedPools() (recordedPools, error) {
	data, err := os.ReadFile(recordedPoolsPath())
	if err != nil {
		return recordedPools{}, fmt.Errorf("read user pool identities: %w", err)
	}
	var pools recordedPools
	if err := json.Unmarshal(data, &pools); err != nil {
		return recordedPools{}, fmt.Errorf("decode user pool identities: %w", err)
	}
	return pools, nil
}

func recordedPoolsPath() string {
	return filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "cognitoidp-user-pools.json")
}
