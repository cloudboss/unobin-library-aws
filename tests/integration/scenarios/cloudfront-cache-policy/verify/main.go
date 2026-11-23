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

const name = "cloudfront-cache-policy"

type policyVersion struct {
	ID   string `json:"id"`
	ETag string `json:"etag"`
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("verify CloudFront cache policy: %v", err)
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
	path := filepath.Join(buildDir, "original-cache-policy.json")
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
		return fmt.Errorf("read original cache policy version: %w", err)
	}
	var original policyVersion
	if err := json.Unmarshal(data, &original); err != nil {
		return fmt.Errorf("decode original cache policy version: %w", err)
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
			return errors.New("cache policy is still tracked after destroy")
		}
		_, err := client.GetCachePolicy(ctx, &cloudfront.GetCachePolicyInput{
			Id: aws.String(original.ID),
		})
		var missing *cloudfronttypes.NoSuchCachePolicy
		if errors.As(err, &missing) {
			return nil
		}
		return fmt.Errorf("expected cache policy %s to be missing, got %v", original.ID, err)
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
	result, err := client.GetCachePolicy(ctx, &cloudfront.GetCachePolicyInput{
		Id: aws.String(version.ID),
	})
	if err != nil {
		return fmt.Errorf("get cache policy %s: %w", version.ID, err)
	}
	if result.CachePolicy == nil || result.CachePolicy.CachePolicyConfig == nil {
		return fmt.Errorf("policy %s is missing its configuration", version.ID)
	}
	if aws.ToString(result.CachePolicy.Id) != version.ID || aws.ToString(result.ETag) != version.ETag {
		return fmt.Errorf("cloud policy ID or ETag disagrees with state %+v", version)
	}

	wantName := "unobin-it-cache-policy"
	wantComment := "cache policy integration test"
	wantDefault, wantMax := int64(86400), int64(31536000)
	wantBehavior := cloudfronttypes.CachePolicyCookieBehaviorWhitelist
	wantCookies := []string{"session", "theme"}
	if updated {
		wantName += "-renamed"
		wantComment = ""
		wantDefault, wantMax = 0, 0
		wantBehavior = cloudfronttypes.CachePolicyCookieBehaviorNone
		wantCookies = nil
	}
	config := result.CachePolicy.CachePolicyConfig
	if aws.ToString(config.Name) != wantName || aws.ToString(config.Comment) != wantComment {
		return fmt.Errorf("unexpected policy name or comment: %q, %q",
			aws.ToString(config.Name), aws.ToString(config.Comment))
	}
	if config.DefaultTTL == nil || aws.ToInt64(config.DefaultTTL) != wantDefault ||
		config.MaxTTL == nil || aws.ToInt64(config.MaxTTL) != wantMax ||
		config.MinTTL == nil || aws.ToInt64(config.MinTTL) != 0 {
		return fmt.Errorf("unexpected default, max, or min TTL: %d, %d, %d",
			aws.ToInt64(config.DefaultTTL), aws.ToInt64(config.MaxTTL), aws.ToInt64(config.MinTTL))
	}
	p := config.ParametersInCacheKeyAndForwardedToOrigin
	if p == nil || p.CookiesConfig == nil || p.HeadersConfig == nil || p.QueryStringsConfig == nil {
		return errors.New("cache policy is missing a cache key configuration")
	}
	if aws.ToBool(p.EnableAcceptEncodingGzip) != !updated ||
		aws.ToBool(p.EnableAcceptEncodingBrotli) != updated {
		return errors.New("cache policy has unexpected compression flags")
	}
	if p.CookiesConfig.CookieBehavior != wantBehavior ||
		p.HeadersConfig.HeaderBehavior != cloudfronttypes.CachePolicyHeaderBehaviorNone ||
		p.QueryStringsConfig.QueryStringBehavior != cloudfronttypes.CachePolicyQueryStringBehaviorNone {
		return errors.New("cache policy has unexpected cookie, header, or query string behavior")
	}
	var items []string
	var quantity int32
	if cookies := p.CookiesConfig.Cookies; cookies != nil {
		items = slices.Sorted(slices.Values(cookies.Items))
		quantity = aws.ToInt32(cookies.Quantity)
	}
	if !slices.Equal(items, wantCookies) || quantity != int32(len(wantCookies)) {
		return fmt.Errorf("cache policy has cookies %v with quantity %d, want %v",
			items, quantity, wantCookies)
	}
	return nil
}
