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
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/state/local"
)

const name = "cloudfront-origin-request-policy"

type policyVersion struct {
	ID   string `json:"id"`
	ETag string `json:"etag"`
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("verify CloudFront origin request policy: %v", err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}
	client := cloudfront.NewFromConfig(cfg)
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	if buildDir == "" {
		return errors.New("VERIFY_BUILD_DIR is required")
	}
	path := filepath.Join(buildDir, "original-origin-request-policy.json")
	phase := os.Getenv("VERIFY_PHASE")
	store, err := local.NewStore(filepath.Join(buildDir, ".unobin/state"),
		name, "config", encrypters.Noop{})
	if err != nil {
		return err
	}
	snapshot, err := store.Current()
	if err != nil {
		return fmt.Errorf("read current state: %w", err)
	}
	entry := snapshot.Find("resource.policy")
	if phase == "applied" {
		version, err := stateVersion(entry != nil, snapshot.Outputs)
		if err != nil {
			return err
		}
		if err := verifyConfig(ctx, client, version, false); err != nil {
			return err
		}
		data, err := json.Marshal(version)
		if err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o600)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read original origin request policy version: %w", err)
	}
	var original policyVersion
	if err := json.Unmarshal(data, &original); err != nil {
		return fmt.Errorf("decode original origin request policy version: %w", err)
	}
	switch phase {
	case "updated":
		version, err := stateVersion(entry != nil, snapshot.Outputs)
		if err != nil {
			return err
		}
		if version.ID != original.ID || version.ETag == original.ETag {
			return fmt.Errorf("policy identity or version after update: %+v, original %+v",
				version, original)
		}
		return verifyConfig(ctx, client, version, true)
	case "destroyed":
		if entry != nil {
			return errors.New("origin request policy is still tracked after destroy")
		}
		_, err := client.GetOriginRequestPolicy(ctx, &cloudfront.GetOriginRequestPolicyInput{
			Id: aws.String(original.ID),
		})
		var missing *cloudfronttypes.NoSuchOriginRequestPolicy
		if errors.As(err, &missing) {
			return nil
		}
		return fmt.Errorf("expected origin request policy %s to be missing, got %v", original.ID, err)
	default:
		return fmt.Errorf("unexpected VERIFY_PHASE %q", phase)
	}
}

func stateVersion(tracked bool, outputs map[string]any) (policyVersion, error) {
	id, _ := outputs["id"].(string)
	etag, _ := outputs["etag"].(string)
	if !tracked || id == "" || etag == "" {
		return policyVersion{}, fmt.Errorf("expected a tracked policy with ID and ETag, got %v",
			outputs)
	}
	return policyVersion{ID: id, ETag: etag}, nil
}

func verifyConfig(
	ctx context.Context, client *cloudfront.Client, version policyVersion, updated bool,
) error {
	result, err := client.GetOriginRequestPolicy(ctx, &cloudfront.GetOriginRequestPolicyInput{
		Id: aws.String(version.ID),
	})
	if err != nil {
		return fmt.Errorf("get origin request policy %s: %w", version.ID, err)
	}
	policy := result.OriginRequestPolicy
	if policy == nil || policy.OriginRequestPolicyConfig == nil {
		return fmt.Errorf("policy %s is missing its configuration", version.ID)
	}
	if aws.ToString(policy.Id) != version.ID || aws.ToString(result.ETag) != version.ETag {
		return fmt.Errorf("cloud policy ID or ETag disagrees with state %+v", version)
	}
	cfg := policy.OriginRequestPolicyConfig
	if cfg.CookiesConfig == nil || cfg.HeadersConfig == nil || cfg.QueryStringsConfig == nil {
		return errors.New("policy is missing a request configuration")
	}

	type collection struct {
		behavior string
		items    []string
		quantity int32
	}
	cookies := collection{behavior: string(cfg.CookiesConfig.CookieBehavior)}
	if names := cfg.CookiesConfig.Cookies; names != nil {
		cookies.items = slices.Sorted(slices.Values(names.Items))
		cookies.quantity = aws.ToInt32(names.Quantity)
	}
	headers := collection{behavior: string(cfg.HeadersConfig.HeaderBehavior)}
	if names := cfg.HeadersConfig.Headers; names != nil {
		headers.items = slices.Sorted(slices.Values(names.Items))
		headers.quantity = aws.ToInt32(names.Quantity)
	}
	queries := collection{behavior: string(cfg.QueryStringsConfig.QueryStringBehavior)}
	if names := cfg.QueryStringsConfig.QueryStrings; names != nil {
		queries.items = slices.Sorted(slices.Values(names.Items))
		queries.quantity = aws.ToInt32(names.Quantity)
	}
	wantName := "unobin-it-origin-request-policy"
	wantComment := "origin request policy integration test"
	wantCookies := collection{"whitelist", []string{"session", "theme"}, 2}
	wantHeaders := collection{
		"allViewerAndWhitelistCloudFront",
		[]string{"CloudFront-Is-Mobile-Viewer", "CloudFront-Viewer-Country"}, 2,
	}
	wantQueries := collection{"whitelist", []string{"preview", "version"}, 2}
	if updated {
		wantName += "-renamed"
		wantComment = ""
		wantCookies = collection{behavior: "none"}
		wantHeaders = collection{"whitelist", []string{"X-Request-ID"}, 1}
		wantQueries = collection{"allExcept", []string{"internal"}, 1}
	}
	if aws.ToString(cfg.Name) != wantName || aws.ToString(cfg.Comment) != wantComment {
		return fmt.Errorf("policy name and comment = (%q, %q), want (%q, %q)",
			aws.ToString(cfg.Name), aws.ToString(cfg.Comment), wantName, wantComment)
	}
	for _, pair := range []struct {
		field     string
		got, want collection
	}{
		{"cookies", cookies, wantCookies},
		{"headers", headers, wantHeaders},
		{"query strings", queries, wantQueries},
	} {
		if pair.got.behavior != pair.want.behavior || pair.got.quantity != pair.want.quantity ||
			!slices.Equal(pair.got.items, pair.want.items) {
			return fmt.Errorf("policy %s = %+v, want %+v", pair.field, pair.got, pair.want)
		}
	}
	return nil
}
