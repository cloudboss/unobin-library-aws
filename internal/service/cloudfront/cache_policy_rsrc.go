package cloudfront

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type CachePolicyResource struct {
	Name       string                `ub:"name"`
	Comment    *string               `ub:"comment"`
	DefaultTTL int64                 `ub:"default-ttl"`
	MaxTTL     int64                 `ub:"max-ttl"`
	MinTTL     int64                 `ub:"min-ttl"`
	Parameters CachePolicyParameters `ub:"parameters-in-cache-key-and-forwarded-to-origin"`
}

type CachePolicyParameters struct {
	CookiesConfig              CachePolicyCookiesConfig      `ub:"cookies-config"`
	HeadersConfig              CachePolicyHeadersConfig      `ub:"headers-config"`
	QueryStringsConfig         CachePolicyQueryStringsConfig `ub:"query-strings-config"`
	EnableAcceptEncodingGzip   *bool                         `ub:"enable-accept-encoding-gzip"`
	EnableAcceptEncodingBrotli *bool                         `ub:"enable-accept-encoding-brotli"`
}

type CachePolicyCookiesConfig struct {
	CookieBehavior string            `ub:"cookie-behavior"`
	Cookies        *CachePolicyItems `ub:"cookies"`
}

type CachePolicyHeadersConfig struct {
	HeaderBehavior string            `ub:"header-behavior"`
	Headers        *CachePolicyItems `ub:"headers"`
}

type CachePolicyQueryStringsConfig struct {
	QueryStringBehavior string            `ub:"query-string-behavior"`
	QueryStrings        *CachePolicyItems `ub:"query-strings"`
}

type CachePolicyItems struct {
	Items *[]string `ub:"items"`
}

type CachePolicyResourceOutput struct {
	Id         string                           `ub:"id"`
	Arn        string                           `ub:"arn"`
	ETag       string                           `ub:"etag"`
	Name       string                           `ub:"name"`
	Comment    string                           `ub:"comment"`
	DefaultTTL int64                            `ub:"default-ttl"`
	MaxTTL     int64                            `ub:"max-ttl"`
	MinTTL     int64                            `ub:"min-ttl"`
	Parameters *CachePolicyDataSourceParameters `ub:"parameters-in-cache-key-and-forwarded-to-origin"`
}

type cachePolicyPrior = runtime.Prior[CachePolicyResource, *CachePolicyResourceOutput, *awsCfg]
type cachePolicyDefinition = runtime.ResourceDefinition[
	CachePolicyResource, *CachePolicyResourceOutput, *awsCfg,
]

func (r *CachePolicyResource) ResourceDefinition() cachePolicyDefinition {
	comment := runtime.InputField(func(r *CachePolicyResource) **string { return &r.Comment })
	parameters := runtime.InputField(func(r *CachePolicyResource) *CachePolicyParameters {
		return &r.Parameters
	})
	return cachePolicyDefinition{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[CachePolicyResource]{
			runtime.EqualBy(comment, func(prior, desired *string) bool {
				return aws.ToString(prior) == aws.ToString(desired)
			}),
			runtime.EqualBy(parameters, func(prior, desired CachePolicyParameters) bool {
				return !runtime.Changed(prior.request(), desired.request())
			}),
		},
		StableID: func(_ CachePolicyResource, out *CachePolicyResourceOutput) (string, error) {
			return out.Id, nil
		},
	}
}

func (r CachePolicyResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.DefaultTTL, 86400),
		defaults.Value(r.MaxTTL, 31536000),
		defaults.Value(r.MinTTL, 0),
	}
}

func (r CachePolicyResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.Parameters.CookiesConfig.CookieBehavior,
			"none", "whitelist", "allExcept", "all")).
			Message("cookie-behavior must be one of none, whitelist, allExcept, all"),
		constraint.Must(constraint.OneOf(r.Parameters.HeadersConfig.HeaderBehavior,
			"none", "whitelist")).
			Message("header-behavior must be one of none, whitelist"),
		constraint.Must(constraint.OneOf(r.Parameters.QueryStringsConfig.QueryStringBehavior,
			"none", "whitelist", "allExcept", "all")).
			Message("query-string-behavior must be one of none, whitelist, allExcept, all"),
		constraint.Must(constraint.NotEquals(r.Name, nil)).Message("name must be set"),
	}
}

func (r *CachePolicyResource) config() *cloudfronttypes.CachePolicyConfig {
	config := &cloudfronttypes.CachePolicyConfig{
		Name:                                     aws.String(r.Name),
		DefaultTTL:                               aws.Int64(r.DefaultTTL),
		MaxTTL:                                   aws.Int64(r.MaxTTL),
		MinTTL:                                   aws.Int64(r.MinTTL),
		ParametersInCacheKeyAndForwardedToOrigin: r.Parameters.request(),
	}
	if aws.ToString(r.Comment) != "" {
		config.Comment = r.Comment
	}
	return config
}

func (p CachePolicyParameters) request() *cloudfronttypes.ParametersInCacheKeyAndForwardedToOrigin {
	cookies := &cloudfronttypes.CachePolicyCookiesConfig{
		CookieBehavior: cloudfronttypes.CachePolicyCookieBehavior(p.CookiesConfig.CookieBehavior),
	}
	if items := p.CookiesConfig.Cookies.effectiveItems(); len(items) > 0 {
		cookies.Cookies = &cloudfronttypes.CookieNames{
			Items: items, Quantity: aws.Int32(int32(len(items))),
		}
	}
	headers := &cloudfronttypes.CachePolicyHeadersConfig{
		HeaderBehavior: cloudfronttypes.CachePolicyHeaderBehavior(p.HeadersConfig.HeaderBehavior),
	}
	if items := p.HeadersConfig.Headers.effectiveItems(); len(items) > 0 {
		headers.Headers = &cloudfronttypes.Headers{
			Items: items, Quantity: aws.Int32(int32(len(items))),
		}
	}
	queries := &cloudfronttypes.CachePolicyQueryStringsConfig{
		QueryStringBehavior: cloudfronttypes.CachePolicyQueryStringBehavior(
			p.QueryStringsConfig.QueryStringBehavior),
	}
	if items := p.QueryStringsConfig.QueryStrings.effectiveItems(); len(items) > 0 {
		queries.QueryStrings = &cloudfronttypes.QueryStringNames{
			Items: items, Quantity: aws.Int32(int32(len(items))),
		}
	}
	return &cloudfronttypes.ParametersInCacheKeyAndForwardedToOrigin{
		CookiesConfig:              cookies,
		HeadersConfig:              headers,
		QueryStringsConfig:         queries,
		EnableAcceptEncodingGzip:   aws.Bool(aws.ToBool(p.EnableAcceptEncodingGzip)),
		EnableAcceptEncodingBrotli: aws.Bool(aws.ToBool(p.EnableAcceptEncodingBrotli)),
	}
}

func (items *CachePolicyItems) effectiveItems() []string {
	if items == nil || items.Items == nil {
		return nil
	}
	return canonicalCachePolicyItems(*items.Items)
}

func canonicalCachePolicyItems(items []string) []string {
	canonical := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" {
			canonical = append(canonical, item)
		}
	}
	slices.Sort(canonical)
	return slices.Compact(canonical)
}

func (r *CachePolicyResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*CachePolicyResourceOutput, error) {
	config := r.config()
	client, sdkCfg, err := newClientConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create cache policy: %w", err)
	}
	accountID, err := cachePolicyAccountID(ctx, cfg, sdkCfg)
	if err != nil {
		return nil, fmt.Errorf("create cache policy: %w", err)
	}
	resp, err := client.CreateCachePolicy(ctx, &cloudfront.CreateCachePolicyInput{
		CachePolicyConfig: config,
	})
	if err != nil {
		return nil, fmt.Errorf("create cache policy: %w", err)
	}
	if resp == nil || resp.CachePolicy == nil || aws.ToString(resp.CachePolicy.Id) == "" {
		return nil, errors.New("create cache policy: missing policy ID")
	}
	id := aws.ToString(resp.CachePolicy.Id)
	etag := aws.ToString(resp.ETag)
	if resp.CachePolicy.CachePolicyConfig != nil && etag != "" {
		return cachePolicyResourceOutput(sdkCfg.Region, accountID, id, etag, resp.CachePolicy), nil
	}
	policy, etag, err := getCachePolicy(ctx, client, id)
	if err != nil {
		return nil, fmt.Errorf("create cache policy %s observation: %w", id, err)
	}
	return cachePolicyResourceOutput(sdkCfg.Region, accountID, id, etag, policy), nil
}

func (r *CachePolicyResource) Read(
	ctx context.Context, cfg *awsCfg, prior cachePolicyPrior,
) (*CachePolicyResourceOutput, error) {
	id := prior.Outputs.Id
	client, sdkCfg, err := newClientConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("read cache policy %s: %w", id, err)
	}
	policy, etag, err := getCachePolicy(ctx, client, id)
	if err != nil {
		return nil, err
	}
	accountID, err := cachePolicyAccountID(ctx, cfg, sdkCfg)
	if err != nil {
		return nil, fmt.Errorf("read cache policy %s: %w", id, err)
	}
	return cachePolicyResourceOutput(sdkCfg.Region, accountID, id, etag, policy), nil
}

func (r *CachePolicyResource) Update(
	ctx context.Context, cfg *awsCfg, prior cachePolicyPrior,
) (*CachePolicyResourceOutput, error) {
	id := prior.Outputs.Id
	etag := priorCachePolicyETag(prior)
	if etag == "" {
		return nil, fmt.Errorf("update cache policy %s: missing ETag", id)
	}
	client, sdkCfg, err := newClientConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("update cache policy %s: %w", id, err)
	}
	accountID, err := cachePolicyAccountID(ctx, cfg, sdkCfg)
	if err != nil {
		return nil, fmt.Errorf("update cache policy %s: %w", id, err)
	}
	_, err = client.UpdateCachePolicy(ctx, &cloudfront.UpdateCachePolicyInput{
		Id: aws.String(id), IfMatch: aws.String(etag), CachePolicyConfig: r.config(),
	})
	if err != nil {
		return nil, fmt.Errorf("update cache policy %s: %w", id, err)
	}
	policy, etag, err := getCachePolicy(ctx, client, id)
	if err != nil {
		return nil, fmt.Errorf("update cache policy %s observation: %w", id, err)
	}
	return cachePolicyResourceOutput(sdkCfg.Region, accountID, id, etag, policy), nil
}

func (r *CachePolicyResource) Delete(
	ctx context.Context, cfg *awsCfg, prior cachePolicyPrior,
) error {
	id := prior.Outputs.Id
	etag := priorCachePolicyETag(prior)
	if etag == "" {
		return fmt.Errorf("delete cache policy %s: missing ETag", id)
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("delete cache policy %s: %w", id, err)
	}
	_, err = client.DeleteCachePolicy(ctx, &cloudfront.DeleteCachePolicyInput{
		Id: aws.String(id), IfMatch: aws.String(etag),
	})
	if err != nil && !isCachePolicyNotFound(err) {
		return fmt.Errorf("delete cache policy %s: %w", id, err)
	}
	return nil
}

func priorCachePolicyETag(prior cachePolicyPrior) string {
	if prior.Observed != nil {
		return prior.Observed.ETag
	}
	return prior.Outputs.ETag
}

func getCachePolicy(
	ctx context.Context, client *cloudfront.Client, id string,
) (*cloudfronttypes.CachePolicy, string, error) {
	resp, err := client.GetCachePolicy(ctx, &cloudfront.GetCachePolicyInput{Id: aws.String(id)})
	if isCachePolicyNotFound(err) {
		return nil, "", runtime.ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("get cache policy %s: %w", id, err)
	}
	if resp == nil || resp.CachePolicy == nil || resp.CachePolicy.CachePolicyConfig == nil {
		return nil, "", runtime.ErrNotFound
	}
	if got := aws.ToString(resp.CachePolicy.Id); got != "" && got != id {
		return nil, "", fmt.Errorf("get cache policy %s: returned policy ID %q", id, got)
	}
	etag := aws.ToString(resp.ETag)
	if etag == "" {
		return nil, "", fmt.Errorf("get cache policy %s: missing ETag", id)
	}
	return resp.CachePolicy, etag, nil
}

func cachePolicyResourceOutput(
	region, accountID, id, etag string, policy *cloudfronttypes.CachePolicy,
) *CachePolicyResourceOutput {
	data := cachePolicyDataOutput(region, accountID, id, etag, policy)
	parameters := data.ParametersInCacheKeyAndForwardedToOrigin
	if parameters != nil {
		if cfg := parameters.CookiesConfig; cfg != nil && cfg.Cookies != nil {
			cfg.Cookies.Items = canonicalCachePolicyItems(cfg.Cookies.Items)
			if len(cfg.Cookies.Items) == 0 {
				cfg.Cookies = nil
			}
		}
		if cfg := parameters.HeadersConfig; cfg != nil && cfg.Headers != nil {
			cfg.Headers.Items = canonicalCachePolicyItems(cfg.Headers.Items)
			if len(cfg.Headers.Items) == 0 {
				cfg.Headers = nil
			}
		}
		if cfg := parameters.QueryStringsConfig; cfg != nil && cfg.QueryStrings != nil {
			cfg.QueryStrings.Items = canonicalCachePolicyItems(cfg.QueryStrings.Items)
			if len(cfg.QueryStrings.Items) == 0 {
				cfg.QueryStrings = nil
			}
		}
	}
	return &CachePolicyResourceOutput{
		Id: data.Id, Arn: data.Arn, ETag: data.ETag, Name: data.Name, Comment: data.Comment,
		DefaultTTL: data.DefaultTTL, MaxTTL: data.MaxTTL, MinTTL: data.MinTTL, Parameters: parameters,
	}
}
