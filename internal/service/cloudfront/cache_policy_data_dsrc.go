package cloudfront

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cloudboss/unobin/pkg/constraint"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

// CachePolicyDataSource resolves one existing CloudFront cache policy by exactly one
// selector: its id, read directly with GetCachePolicy, or its name, resolved by
// paging through ListCachePolicies and stopping at the first exact name match.
// The final output always comes from GetCachePolicy. The ARN is composed locally
// because CloudFront does not return one for a cache policy read.
type CachePolicyDataSource struct {
	Id   *string `ub:"id"`
	Name *string `ub:"name"`
}

// CachePolicyDataSourceOutput holds the selected cache policy's attributes. Arn is a
// global CloudFront ARN built from the configured partition and account id. The
// nested parameters block is absent when CloudFront omits it; within it the
// Quantity members on cookie, header, and query-string lists are ignored and
// only their Items lists are exposed.
type CachePolicyDataSourceOutput struct {
	Id                                       string                           `ub:"id"`
	Arn                                      string                           `ub:"arn"`
	Comment                                  string                           `ub:"comment"`
	DefaultTTL                               int64                            `ub:"default-ttl"`
	ETag                                     string                           `ub:"etag"`
	MaxTTL                                   int64                            `ub:"max-ttl"`
	MinTTL                                   int64                            `ub:"min-ttl"`
	Name                                     string                           `ub:"name"`
	ParametersInCacheKeyAndForwardedToOrigin *CachePolicyDataSourceParameters `ub:"parameters-in-cache-key-and-forwarded-to-origin"`
}

// CachePolicyDataSourceParameters is the cache-key parameter block CloudFront returns
// inside CachePolicyConfig.
type CachePolicyDataSourceParameters struct {
	CookiesConfig *CachePolicyDataSourceCookiesConfig `ub:"cookies-config"`

	EnableAcceptEncodingGzip bool `ub:"enable-accept-encoding-gzip"`

	HeadersConfig      *CachePolicyDataSourceHeadersConfig      `ub:"headers-config"`
	QueryStringsConfig *CachePolicyDataSourceQueryStringsConfig `ub:"query-strings-config"`

	EnableAcceptEncodingBrotli bool `ub:"enable-accept-encoding-brotli"`
}

// CachePolicyDataSourceCookiesConfig describes which cookies are part of the cache
// key and forwarded to the origin.
type CachePolicyDataSourceCookiesConfig struct {
	CookieBehavior string                            `ub:"cookie-behavior"`
	Cookies        *CachePolicyDataSourceCookieNames `ub:"cookies"`
}

// CachePolicyDataSourceCookieNames exposes only the cookie Items list; CloudFront's
// Quantity field is deliberately ignored.
type CachePolicyDataSourceCookieNames struct {
	Items []string `ub:"items"`
}

// CachePolicyDataSourceHeadersConfig describes which headers are part of the cache
// key and forwarded to the origin.
type CachePolicyDataSourceHeadersConfig struct {
	HeaderBehavior string                        `ub:"header-behavior"`
	Headers        *CachePolicyDataSourceHeaders `ub:"headers"`
}

// CachePolicyDataSourceHeaders exposes only the header Items list; CloudFront's
// Quantity field is deliberately ignored.
type CachePolicyDataSourceHeaders struct {
	Items []string `ub:"items"`
}

// CachePolicyDataSourceQueryStringsConfig describes which query strings are part of
// the cache key and forwarded to the origin.
type CachePolicyDataSourceQueryStringsConfig struct {
	QueryStringBehavior string                                 `ub:"query-string-behavior"`
	QueryStrings        *CachePolicyDataSourceQueryStringNames `ub:"query-strings"`
}

// CachePolicyDataSourceQueryStringNames exposes only the query string Items list;
// CloudFront's Quantity field is deliberately ignored.
type CachePolicyDataSourceQueryStringNames struct {
	Items []string `ub:"items"`
}

// Constraints declares the lookup selector rule: callers must set exactly one
// of id or name.
func (r CachePolicyDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.Id, r.Name),
	}
}

// Read resolves the cache policy and returns its current config. A lookup that
// finds no policy returns a descriptive data-source error rather than
// runtime.ErrNotFound.
func (r *CachePolicyDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*CachePolicyDataSourceOutput, error) {
	client, sdkCfg, err := newClientConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id, err := r.resolveID(ctx, client)
	if err != nil {
		return nil, err
	}
	policy, etag, err := getCachePolicyDataSource(ctx, client, id)
	if err != nil {
		return nil, err
	}
	accountID, err := cachePolicyDataAccountID(ctx, cfg, sdkCfg)
	if err != nil {
		return nil, err
	}
	return cachePolicyDataOutput(client.Options().Region, accountID, id, etag, policy), nil
}

func (r *CachePolicyDataSource) resolveID(
	ctx context.Context, client *cloudfront.Client,
) (string, error) {
	if err := r.checkSelector(); err != nil {
		return "", err
	}
	if r.Id != nil {
		return *r.Id, nil
	}
	return r.findIDByName(ctx, client, *r.Name)
}

func (r *CachePolicyDataSource) checkSelector() error {
	switch {
	case r.Id != nil && r.Name != nil:
		return errors.New("exactly one of id or name must be supplied")
	case r.Id == nil && r.Name == nil:
		return errors.New("exactly one of id or name must be supplied")
	default:
		return nil
	}
}

func (r *CachePolicyDataSource) findIDByName(
	ctx context.Context,
	client *cloudfront.Client,
	name string,
) (string, error) {
	var marker *string
	for {
		resp, err := client.ListCachePolicies(ctx, &cloudfront.ListCachePoliciesInput{
			Marker: marker,
		})
		if err != nil {
			return "", fmt.Errorf("list cache policies: %w", err)
		}
		if resp == nil || resp.CachePolicyList == nil {
			return "", fmt.Errorf("no CloudFront Cache Policy named %q found", name)
		}
		list := resp.CachePolicyList
		for _, summary := range list.Items {
			policy := summary.CachePolicy
			if policy == nil || policy.CachePolicyConfig == nil {
				continue
			}
			if aws.ToString(policy.CachePolicyConfig.Name) == name {
				id := aws.ToString(policy.Id)
				if id == "" {
					return "", fmt.Errorf(
						"CloudFront Cache Policy named %q has an empty id", name)
				}
				return id, nil
			}
		}
		next := aws.ToString(list.NextMarker)
		if next == "" {
			break
		}
		marker = aws.String(next)
	}
	return "", fmt.Errorf("no CloudFront Cache Policy named %q found", name)
}

func getCachePolicyDataSource(
	ctx context.Context,
	client *cloudfront.Client,
	id string,
) (*cloudfronttypes.CachePolicy, string, error) {
	resp, err := client.GetCachePolicy(ctx, &cloudfront.GetCachePolicyInput{
		Id: aws.String(id),
	})
	if err != nil {
		if isCachePolicyNotFound(err) {
			return nil, "", fmt.Errorf("CloudFront Cache Policy %q not found", id)
		}
		return nil, "", fmt.Errorf("get cache policy %s: %w", id, err)
	}
	if resp == nil || resp.CachePolicy == nil || resp.CachePolicy.CachePolicyConfig == nil {
		return nil, "", fmt.Errorf("CloudFront Cache Policy %q not found", id)
	}
	return resp.CachePolicy, aws.ToString(resp.ETag), nil
}

func cachePolicyDataAccountID(
	ctx context.Context,
	cfg *awsCfg,
	sdkCfg aws.Config,
) (string, error) {
	if sdkCfg.Credentials != nil {
		creds, err := sdkCfg.Credentials.Retrieve(ctx)
		if err == nil && creds.AccountID != "" {
			return creds.AccountID, nil
		}
	}
	client := sts.NewFromConfig(sdkCfg, func(o *sts.Options) {
		if cfg == nil {
			return
		}
		if endpoint := cfg.STSEndpoint(); endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	resp, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("get caller identity for cache policy arn: %w", err)
	}
	accountID := aws.ToString(resp.Account)
	if accountID == "" {
		return "", errors.New("get caller identity for cache policy arn returned no account")
	}
	return accountID, nil
}

func cachePolicyDataOutput(
	region string,
	accountID string,
	resolvedID string,
	etag string,
	policy *cloudfronttypes.CachePolicy,
) *CachePolicyDataSourceOutput {
	config := policy.CachePolicyConfig
	id := aws.ToString(policy.Id)
	if id == "" {
		id = resolvedID
	}
	return &CachePolicyDataSourceOutput{
		Id:         id,
		Arn:        cachePolicyDataARN(region, accountID, id),
		Comment:    aws.ToString(config.Comment),
		DefaultTTL: aws.ToInt64(config.DefaultTTL),
		ETag:       etag,
		MaxTTL:     aws.ToInt64(config.MaxTTL),
		MinTTL:     aws.ToInt64(config.MinTTL),
		Name:       aws.ToString(config.Name),
		ParametersInCacheKeyAndForwardedToOrigin: flattenCachePolicyParameters(
			config.ParametersInCacheKeyAndForwardedToOrigin),
	}
}

func cachePolicyDataARN(region, accountID, id string) string {
	return fmt.Sprintf("arn:%s:cloudfront::%s:cache-policy/%s",
		partition.Of(region), accountID, id)
}

func flattenCachePolicyParameters(
	in *cloudfronttypes.ParametersInCacheKeyAndForwardedToOrigin,
) *CachePolicyDataSourceParameters {
	if in == nil {
		return nil
	}
	return &CachePolicyDataSourceParameters{
		CookiesConfig:              flattenCachePolicyCookiesConfig(in.CookiesConfig),
		EnableAcceptEncodingGzip:   aws.ToBool(in.EnableAcceptEncodingGzip),
		HeadersConfig:              flattenCachePolicyHeadersConfig(in.HeadersConfig),
		QueryStringsConfig:         flattenCachePolicyQueryStringsConfig(in.QueryStringsConfig),
		EnableAcceptEncodingBrotli: aws.ToBool(in.EnableAcceptEncodingBrotli),
	}
}

func flattenCachePolicyCookiesConfig(
	in *cloudfronttypes.CachePolicyCookiesConfig,
) *CachePolicyDataSourceCookiesConfig {
	if in == nil {
		return nil
	}
	return &CachePolicyDataSourceCookiesConfig{
		CookieBehavior: string(in.CookieBehavior),
		Cookies:        flattenCachePolicyCookieNames(in.Cookies),
	}
}

func flattenCachePolicyCookieNames(
	in *cloudfronttypes.CookieNames,
) *CachePolicyDataSourceCookieNames {
	if in == nil {
		return nil
	}
	return &CachePolicyDataSourceCookieNames{Items: append([]string(nil), in.Items...)}
}

func flattenCachePolicyHeadersConfig(
	in *cloudfronttypes.CachePolicyHeadersConfig,
) *CachePolicyDataSourceHeadersConfig {
	if in == nil {
		return nil
	}
	return &CachePolicyDataSourceHeadersConfig{
		HeaderBehavior: string(in.HeaderBehavior),
		Headers:        flattenCachePolicyHeaders(in.Headers),
	}
}

func flattenCachePolicyHeaders(in *cloudfronttypes.Headers) *CachePolicyDataSourceHeaders {
	if in == nil {
		return nil
	}
	return &CachePolicyDataSourceHeaders{Items: append([]string(nil), in.Items...)}
}

func flattenCachePolicyQueryStringsConfig(
	in *cloudfronttypes.CachePolicyQueryStringsConfig,
) *CachePolicyDataSourceQueryStringsConfig {
	if in == nil {
		return nil
	}
	return &CachePolicyDataSourceQueryStringsConfig{
		QueryStringBehavior: string(in.QueryStringBehavior),
		QueryStrings:        flattenCachePolicyQueryStringNames(in.QueryStrings),
	}
}

func flattenCachePolicyQueryStringNames(
	in *cloudfronttypes.QueryStringNames,
) *CachePolicyDataSourceQueryStringNames {
	if in == nil {
		return nil
	}
	return &CachePolicyDataSourceQueryStringNames{Items: append([]string(nil), in.Items...)}
}

func isCachePolicyNotFound(err error) bool {
	var notFound *cloudfronttypes.NoSuchCachePolicy
	return errors.As(err, &notFound)
}
