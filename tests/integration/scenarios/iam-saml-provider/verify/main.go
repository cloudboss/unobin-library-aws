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
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/state/local"
)

type identity struct {
	ARN, UUID  string
	ValidUntil *string
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := iam.NewFromConfig(cfg)
	dir := os.Getenv("VERIFY_BUILD_DIR")
	identityPath := filepath.Join(dir, "saml-provider.json")
	phase := os.Getenv("VERIFY_PHASE")
	var provider identity
	if phase == "applied" {
		store, err := local.NewStore(filepath.Join(dir, ".unobin/state"),
			"iam-saml-provider", "config", encrypters.Noop{})
		if err != nil {
			return err
		}
		snapshot, err := store.Current()
		if err != nil {
			return err
		}
		entry := snapshot.Find("resource.provider")
		if entry == nil {
			return errors.New("state has no managed SAML provider")
		}
		provider.ARN, _ = entry.Outputs["arn"].(string)
		provider.UUID, _ = entry.Outputs["saml-provider-uuid"].(string)
		if until, ok := entry.Outputs["valid-until"].(string); ok {
			provider.ValidUntil = &until
		}
		if provider.ARN == "" {
			return errors.New("state has no persisted SAML provider ARN")
		}
		body, err := json.Marshal(provider)
		if err != nil {
			return err
		}
		if err := os.WriteFile(identityPath, body, 0o600); err != nil {
			return err
		}
	} else {
		body, err := os.ReadFile(identityPath)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(body, &provider); err != nil {
			return err
		}
	}
	if phase == "destroyed" {
		return verifyAbsent(ctx, client, provider.ARN)
	}
	metadataFile := "metadata.xml"
	tags := map[string]string{"keep": "1", "change": "old", "remove": "yes"}
	if phase == "updated" {
		metadataFile = "metadata-update.xml"
		tags = map[string]string{"keep": "1", "change": "new", "add": ""}
	} else if phase != "applied" {
		return fmt.Errorf("unknown verification step %q", phase)
	}
	metadata, err := os.ReadFile(filepath.Join("tests/integration/scenarios/iam-saml-provider",
		metadataFile))
	if err != nil {
		return err
	}
	return verifyProvider(ctx, client, provider, string(metadata), tags)
}

func verifyProvider(ctx context.Context, client *iam.Client, provider identity,
	metadata string, wantTags map[string]string) error {
	result, err := client.GetSAMLProvider(ctx, &iam.GetSAMLProviderInput{
		SAMLProviderArn: aws.String(provider.ARN),
	})
	if err != nil {
		return err
	}
	if result == nil || aws.ToString(result.SAMLMetadataDocument) != metadata {
		return errors.New("remote SAML metadata differs from the raw desired document")
	}
	if aws.ToString(result.SAMLProviderUUID) != provider.UUID {
		return fmt.Errorf("SAML provider UUID changed: got %q, want %q",
			aws.ToString(result.SAMLProviderUUID), provider.UUID)
	}
	var until *string
	if result.ValidUntil != nil {
		until = aws.String(result.ValidUntil.Format(time.RFC3339))
	}
	if (until == nil) != (provider.ValidUntil == nil) ||
		until != nil && *until != *provider.ValidUntil {
		return fmt.Errorf("SAML provider expiration changed: got %v, want %v",
			until, provider.ValidUntil)
	}
	tags := map[string]string{}
	for _, tag := range result.Tags {
		if key := aws.ToString(tag.Key); !strings.HasPrefix(key, "aws:") {
			tags[key] = aws.ToString(tag.Value)
		}
	}
	if !maps.Equal(tags, wantTags) {
		return fmt.Errorf("SAML provider tags are %v, want %v", tags, wantTags)
	}
	return nil
}

func verifyAbsent(ctx context.Context, client *iam.Client, identity string) error {
	_, err := client.GetSAMLProvider(ctx, &iam.GetSAMLProviderInput{
		SAMLProviderArn: aws.String(identity),
	})
	var missing *types.NoSuchEntityException
	if errors.As(err, &missing) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("SAML provider %s still exists", identity)
}
