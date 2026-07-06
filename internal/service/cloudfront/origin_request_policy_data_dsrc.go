package cloudfront

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cloudboss/unobin/pkg/constraint"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

// OriginRequestPolicyDataSource resolves one existing CloudFront origin request
// policy by exactly one selector: its id, read directly with
// GetOriginRequestPolicy, or its name, resolved by paging through
// ListOriginRequestPolicies and stopping at the first exact name match. The
// final output always comes from GetOriginRequestPolicy. The ARN is composed
// locally because CloudFront does not return one for an origin request policy
// read.
type OriginRequestPolicyDataSource struct {
	Id   *string `ub:"id"`
	Name *string `ub:"name"`
}

// OriginRequestPolicyDataSourceOutput holds the selected origin request policy's
// attributes. Arn is a global CloudFront ARN built from the configured
// partition and account id. The nested config blocks are absent when CloudFront
// omits them; within them the Quantity members on cookie, header, and
// query-string lists are ignored and only non-empty Items lists are exposed.
type OriginRequestPolicyDataSourceOutput struct {
	Id                 string                                           `ub:"id"`
	Arn                string                                           `ub:"arn"`
	Comment            string                                           `ub:"comment"`
	ETag               string                                           `ub:"etag"`
	Name               string                                           `ub:"name"`
	CookiesConfig      *OriginRequestPolicyDataSourceCookiesConfig      `ub:"cookies-config"`
	HeadersConfig      *OriginRequestPolicyDataSourceHeadersConfig      `ub:"headers-config"`
	QueryStringsConfig *OriginRequestPolicyDataSourceQueryStringsConfig `ub:"query-strings-config"`
}

// OriginRequestPolicyDataSourceCookiesConfig describes which cookies CloudFront
// includes in origin requests.
type OriginRequestPolicyDataSourceCookiesConfig struct {
	CookieBehavior string                                    `ub:"cookie-behavior"`
	Cookies        *OriginRequestPolicyDataSourceCookieNames `ub:"cookies"`
}

// OriginRequestPolicyDataSourceCookieNames exposes only the cookie Items list;
// CloudFront's Quantity field is deliberately ignored. Items is returned as a
// sorted, de-duped slice because the source collection is unordered.
type OriginRequestPolicyDataSourceCookieNames struct {
	Items []string `ub:"items"`
}

// OriginRequestPolicyDataSourceHeadersConfig describes which headers CloudFront
// includes in origin requests.
type OriginRequestPolicyDataSourceHeadersConfig struct {
	HeaderBehavior string                                `ub:"header-behavior"`
	Headers        *OriginRequestPolicyDataSourceHeaders `ub:"headers"`
}

// OriginRequestPolicyDataSourceHeaders exposes only the header Items list;
// CloudFront's Quantity field is deliberately ignored. Items is returned as a
// sorted, de-duped slice because the source collection is unordered.
type OriginRequestPolicyDataSourceHeaders struct {
	Items []string `ub:"items"`
}

// OriginRequestPolicyDataSourceQueryStringsConfig describes which query strings
// CloudFront includes in origin requests.
type OriginRequestPolicyDataSourceQueryStringsConfig struct {
	QueryStringBehavior string                                         `ub:"query-string-behavior"`
	QueryStrings        *OriginRequestPolicyDataSourceQueryStringNames `ub:"query-strings"`
}

// OriginRequestPolicyDataSourceQueryStringNames exposes only the query string Items
// list; CloudFront's Quantity field is deliberately ignored. Items is returned
// as a sorted, de-duped slice because the source collection is unordered.
type OriginRequestPolicyDataSourceQueryStringNames struct {
	Items []string `ub:"items"`
}

// Constraints declares the lookup selector rule: callers must set exactly one
// of id or name.
func (r OriginRequestPolicyDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.Id, r.Name),
	}
}

// Read resolves the origin request policy and returns its current config. A
// lookup that finds no policy returns a descriptive data-source error rather
// than runtime.ErrNotFound.
func (r *OriginRequestPolicyDataSource) Read(
	ctx context.Context, cfg *awsCfg,
) (*OriginRequestPolicyDataSourceOutput, error) {
	client, sdkCfg, err := newClientConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id, err := r.resolveID(ctx, client)
	if err != nil {
		return nil, err
	}
	policy, etag, err := getOriginRequestPolicyDataSource(ctx, client, id)
	if err != nil {
		return nil, err
	}
	accountID, err := originRequestPolicyDataAccountID(ctx, cfg, sdkCfg)
	if err != nil {
		return nil, err
	}
	return originRequestPolicyDataOutput(
		client.Options().Region, accountID, id, etag, policy), nil
}

func (r *OriginRequestPolicyDataSource) resolveID(
	ctx context.Context,
	client *cloudfront.Client,
) (string, error) {
	if err := r.checkSelector(); err != nil {
		return "", err
	}
	if r.Id != nil {
		return *r.Id, nil
	}
	return r.findIDByName(ctx, client, *r.Name)
}

func (r *OriginRequestPolicyDataSource) checkSelector() error {
	switch {
	case r.Id != nil && r.Name != nil:
		return errors.New("exactly one of id or name must be supplied")
	case r.Id == nil && r.Name == nil:
		return errors.New("exactly one of id or name must be supplied")
	default:
		return nil
	}
}

func (r *OriginRequestPolicyDataSource) findIDByName(
	ctx context.Context,
	client *cloudfront.Client,
	name string,
) (string, error) {
	var marker *string
	for {
		in := &cloudfront.ListOriginRequestPoliciesInput{}
		if marker != nil {
			in.Marker = marker
		}
		resp, err := client.ListOriginRequestPolicies(ctx, in)
		if err != nil {
			return "", fmt.Errorf("list origin request policies: %w", err)
		}
		if resp == nil || resp.OriginRequestPolicyList == nil {
			return "", fmt.Errorf("no CloudFront Origin Request Policy named %q found", name)
		}
		list := resp.OriginRequestPolicyList
		for _, summary := range list.Items {
			policy := summary.OriginRequestPolicy
			if policy == nil || policy.OriginRequestPolicyConfig == nil {
				continue
			}
			if aws.ToString(policy.OriginRequestPolicyConfig.Name) == name {
				id := aws.ToString(policy.Id)
				if id == "" {
					return "", fmt.Errorf(
						"CloudFront Origin Request Policy named %q has an empty id", name)
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
	return "", fmt.Errorf("no CloudFront Origin Request Policy named %q found", name)
}

func getOriginRequestPolicyDataSource(
	ctx context.Context,
	client *cloudfront.Client,
	id string,
) (*cloudfronttypes.OriginRequestPolicy, string, error) {
	resp, err := client.GetOriginRequestPolicy(ctx, &cloudfront.GetOriginRequestPolicyInput{
		Id: aws.String(id),
	})
	if err != nil {
		if isOriginRequestPolicyNotFound(err) {
			return nil, "", fmt.Errorf("CloudFront Origin Request Policy %q not found", id)
		}
		return nil, "", fmt.Errorf("get origin request policy %s: %w", id, err)
	}
	if resp == nil || resp.OriginRequestPolicy == nil ||
		resp.OriginRequestPolicy.OriginRequestPolicyConfig == nil {
		return nil, "", fmt.Errorf("CloudFront Origin Request Policy %q not found", id)
	}
	return resp.OriginRequestPolicy, aws.ToString(resp.ETag), nil
}

func originRequestPolicyDataAccountID(
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
		return "", fmt.Errorf("get caller identity for origin request policy arn: %w", err)
	}
	accountID := aws.ToString(resp.Account)
	if accountID == "" {
		return "", errors.New(
			"get caller identity for origin request policy arn returned no account")
	}
	return accountID, nil
}

func originRequestPolicyDataOutput(
	region string,
	accountID string,
	resolvedID string,
	etag string,
	policy *cloudfronttypes.OriginRequestPolicy,
) *OriginRequestPolicyDataSourceOutput {
	config := policy.OriginRequestPolicyConfig
	id := aws.ToString(policy.Id)
	if id == "" {
		id = resolvedID
	}
	return &OriginRequestPolicyDataSourceOutput{
		Id:                 id,
		Arn:                originRequestPolicyDataARN(region, accountID, id),
		Comment:            aws.ToString(config.Comment),
		ETag:               etag,
		Name:               aws.ToString(config.Name),
		CookiesConfig:      flattenOriginRequestPolicyCookiesConfig(config.CookiesConfig),
		HeadersConfig:      flattenOriginRequestPolicyHeadersConfig(config.HeadersConfig),
		QueryStringsConfig: flattenOriginRequestPolicyQueryStringsConfig(config.QueryStringsConfig),
	}
}

func originRequestPolicyDataARN(region, accountID, id string) string {
	return fmt.Sprintf("arn:%s:cloudfront::%s:origin-request-policy/%s",
		partition.Of(region), accountID, id)
}

func flattenOriginRequestPolicyCookiesConfig(
	in *cloudfronttypes.OriginRequestPolicyCookiesConfig,
) *OriginRequestPolicyDataSourceCookiesConfig {
	if in == nil {
		return nil
	}
	return &OriginRequestPolicyDataSourceCookiesConfig{
		CookieBehavior: string(in.CookieBehavior),
		Cookies:        flattenOriginRequestPolicyCookieNames(in.Cookies),
	}
}

func flattenOriginRequestPolicyCookieNames(
	in *cloudfronttypes.CookieNames,
) *OriginRequestPolicyDataSourceCookieNames {
	if in == nil {
		return nil
	}
	items := stableOriginRequestPolicyItems(in.Items)
	if len(items) == 0 {
		return nil
	}
	return &OriginRequestPolicyDataSourceCookieNames{Items: items}
}

func flattenOriginRequestPolicyHeadersConfig(
	in *cloudfronttypes.OriginRequestPolicyHeadersConfig,
) *OriginRequestPolicyDataSourceHeadersConfig {
	if in == nil {
		return nil
	}
	return &OriginRequestPolicyDataSourceHeadersConfig{
		HeaderBehavior: string(in.HeaderBehavior),
		Headers:        flattenOriginRequestPolicyHeaders(in.Headers),
	}
}

func flattenOriginRequestPolicyHeaders(
	in *cloudfronttypes.Headers,
) *OriginRequestPolicyDataSourceHeaders {
	if in == nil {
		return nil
	}
	items := stableOriginRequestPolicyItems(in.Items)
	if len(items) == 0 {
		return nil
	}
	return &OriginRequestPolicyDataSourceHeaders{Items: items}
}

func flattenOriginRequestPolicyQueryStringsConfig(
	in *cloudfronttypes.OriginRequestPolicyQueryStringsConfig,
) *OriginRequestPolicyDataSourceQueryStringsConfig {
	if in == nil {
		return nil
	}
	return &OriginRequestPolicyDataSourceQueryStringsConfig{
		QueryStringBehavior: string(in.QueryStringBehavior),
		QueryStrings:        flattenOriginRequestPolicyQueryStringNames(in.QueryStrings),
	}
}

func flattenOriginRequestPolicyQueryStringNames(
	in *cloudfronttypes.QueryStringNames,
) *OriginRequestPolicyDataSourceQueryStringNames {
	if in == nil {
		return nil
	}
	items := stableOriginRequestPolicyItems(in.Items)
	if len(items) == 0 {
		return nil
	}
	return &OriginRequestPolicyDataSourceQueryStringNames{Items: items}
}

func stableOriginRequestPolicyItems(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := append([]string(nil), items...)
	sort.Strings(out)
	unique := out[:0]
	for _, item := range out {
		if len(unique) == 0 || unique[len(unique)-1] != item {
			unique = append(unique, item)
		}
	}
	return unique
}

func isOriginRequestPolicyNotFound(err error) bool {
	var notFound *cloudfronttypes.NoSuchOriginRequestPolicy
	return errors.As(err, &notFound)
}
