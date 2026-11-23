package cloudfront

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type OriginRequestPolicyResource struct {
	Name               string                                `ub:"name"`
	Comment            *string                               `ub:"comment"`
	CookiesConfig      OriginRequestPolicyCookiesConfig      `ub:"cookies-config"`
	HeadersConfig      OriginRequestPolicyHeadersConfig      `ub:"headers-config"`
	QueryStringsConfig OriginRequestPolicyQueryStringsConfig `ub:"query-strings-config"`
}

type OriginRequestPolicyCookiesConfig struct {
	CookieBehavior string                    `ub:"cookie-behavior"`
	Cookies        *OriginRequestPolicyItems `ub:"cookies"`
}

type OriginRequestPolicyHeadersConfig struct {
	HeaderBehavior string                    `ub:"header-behavior"`
	Headers        *OriginRequestPolicyItems `ub:"headers"`
}

type OriginRequestPolicyQueryStringsConfig struct {
	QueryStringBehavior string                    `ub:"query-string-behavior"`
	QueryStrings        *OriginRequestPolicyItems `ub:"query-strings"`
}

type OriginRequestPolicyItems struct {
	Items *[]string `ub:"items"`
}

type OriginRequestPolicyResourceOutput struct {
	Id                 string                                           `ub:"id"`
	Arn                string                                           `ub:"arn"`
	Comment            string                                           `ub:"comment"`
	ETag               string                                           `ub:"etag"`
	Name               string                                           `ub:"name"`
	CookiesConfig      *OriginRequestPolicyDataSourceCookiesConfig      `ub:"cookies-config"`
	HeadersConfig      *OriginRequestPolicyDataSourceHeadersConfig      `ub:"headers-config"`
	QueryStringsConfig *OriginRequestPolicyDataSourceQueryStringsConfig `ub:"query-strings-config"`
}

type originRequestPolicyPrior = runtime.Prior[
	OriginRequestPolicyResource, *OriginRequestPolicyResourceOutput, *awsCfg,
]
type originRequestPolicyDefinition = runtime.ResourceDefinition[
	OriginRequestPolicyResource, *OriginRequestPolicyResourceOutput, *awsCfg,
]

func (r *OriginRequestPolicyResource) ResourceDefinition() originRequestPolicyDefinition {
	comment := runtime.InputField(func(r *OriginRequestPolicyResource) **string {
		return &r.Comment
	})
	cookies := runtime.InputField(func(r *OriginRequestPolicyResource) **OriginRequestPolicyItems {
		return &r.CookiesConfig.Cookies
	})
	headers := runtime.InputField(func(r *OriginRequestPolicyResource) **OriginRequestPolicyItems {
		return &r.HeadersConfig.Headers
	})
	queries := runtime.InputField(func(r *OriginRequestPolicyResource) **OriginRequestPolicyItems {
		return &r.QueryStringsConfig.QueryStrings
	})
	return originRequestPolicyDefinition{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[OriginRequestPolicyResource]{
			runtime.EqualBy(comment, func(prior, desired *string) bool {
				return aws.ToString(prior) == aws.ToString(desired)
			}),
			runtime.EqualBy(cookies, equalOriginRequestPolicyItems),
			runtime.EqualBy(headers, equalOriginRequestPolicyItems),
			runtime.EqualBy(queries, equalOriginRequestPolicyItems),
		},
		StableID: func(
			_ OriginRequestPolicyResource, out *OriginRequestPolicyResourceOutput,
		) (string, error) {
			return out.Id, nil
		},
	}
}

func (r OriginRequestPolicyResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.CookiesConfig.CookieBehavior,
			"none", "whitelist", "all", "allExcept")).
			Message("cookie-behavior must be one of none, whitelist, all, allExcept"),
		constraint.Must(constraint.OneOf(r.HeadersConfig.HeaderBehavior,
			"none", "whitelist", "allViewer", "allViewerAndWhitelistCloudFront", "allExcept")).
			Message("header-behavior must be a supported CloudFront origin request mode"),
		constraint.Must(constraint.OneOf(r.QueryStringsConfig.QueryStringBehavior,
			"none", "whitelist", "all", "allExcept")).
			Message("query-string-behavior must be one of none, whitelist, all, allExcept"),
		constraint.Must(constraint.NotEquals(r.Name, nil)).Message("name must be set"),
	}
}

func (r *OriginRequestPolicyResource) config() *cloudfronttypes.OriginRequestPolicyConfig {
	cookies := &cloudfronttypes.OriginRequestPolicyCookiesConfig{
		CookieBehavior: cloudfronttypes.OriginRequestPolicyCookieBehavior(
			r.CookiesConfig.CookieBehavior),
	}
	if items := r.CookiesConfig.Cookies.effectiveItems(); len(items) > 0 {
		cookies.Cookies = &cloudfronttypes.CookieNames{
			Items: items, Quantity: aws.Int32(int32(len(items))),
		}
	}
	headers := &cloudfronttypes.OriginRequestPolicyHeadersConfig{
		HeaderBehavior: cloudfronttypes.OriginRequestPolicyHeaderBehavior(
			r.HeadersConfig.HeaderBehavior),
	}
	if items := r.HeadersConfig.Headers.effectiveItems(); len(items) > 0 {
		headers.Headers = &cloudfronttypes.Headers{
			Items: items, Quantity: aws.Int32(int32(len(items))),
		}
	}
	queries := &cloudfronttypes.OriginRequestPolicyQueryStringsConfig{
		QueryStringBehavior: cloudfronttypes.OriginRequestPolicyQueryStringBehavior(
			r.QueryStringsConfig.QueryStringBehavior),
	}
	if items := r.QueryStringsConfig.QueryStrings.effectiveItems(); len(items) > 0 {
		queries.QueryStrings = &cloudfronttypes.QueryStringNames{
			Items: items, Quantity: aws.Int32(int32(len(items))),
		}
	}
	config := &cloudfronttypes.OriginRequestPolicyConfig{
		Name:               aws.String(r.Name),
		CookiesConfig:      cookies,
		HeadersConfig:      headers,
		QueryStringsConfig: queries,
	}
	if aws.ToString(r.Comment) != "" {
		config.Comment = r.Comment
	}
	return config
}

func (items *OriginRequestPolicyItems) effectiveItems() []string {
	if items == nil || items.Items == nil {
		return nil
	}
	canonical := stableOriginRequestPolicyItems(*items.Items)
	if len(canonical) != 0 && canonical[0] == "" {
		canonical = canonical[1:]
	}
	return canonical
}

func equalOriginRequestPolicyItems(prior, desired *OriginRequestPolicyItems) bool {
	return slices.Equal(prior.effectiveItems(), desired.effectiveItems())
}

func (r *OriginRequestPolicyResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*OriginRequestPolicyResourceOutput, error) {
	config := r.config()
	client, sdkCfg, err := newClientConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create origin request policy: %w", err)
	}
	accountID, err := originRequestPolicyDataAccountID(ctx, cfg, sdkCfg)
	if err != nil {
		return nil, fmt.Errorf("create origin request policy: %w", err)
	}
	resp, err := client.CreateOriginRequestPolicy(ctx, &cloudfront.CreateOriginRequestPolicyInput{
		OriginRequestPolicyConfig: config,
	})
	if err != nil {
		return nil, fmt.Errorf("create origin request policy: %w", err)
	}
	if resp == nil || resp.OriginRequestPolicy == nil ||
		aws.ToString(resp.OriginRequestPolicy.Id) == "" {
		return nil, errors.New("create origin request policy: missing policy ID")
	}
	policy := resp.OriginRequestPolicy
	id, etag := aws.ToString(policy.Id), aws.ToString(resp.ETag)
	if policy.OriginRequestPolicyConfig != nil && etag != "" {
		output := originRequestPolicyDataOutput(sdkCfg.Region, accountID, id, etag, policy)
		return (*OriginRequestPolicyResourceOutput)(output), nil
	}
	return observeOriginRequestPolicy(ctx, client, accountID, id, "create")
}

func (r *OriginRequestPolicyResource) Read(
	ctx context.Context, cfg *awsCfg, prior originRequestPolicyPrior,
) (*OriginRequestPolicyResourceOutput, error) {
	id := prior.Outputs.Id
	client, sdkCfg, err := newClientConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("read origin request policy %s: %w", id, err)
	}
	policy, etag, err := getOriginRequestPolicy(ctx, client, id)
	if err != nil {
		return nil, err
	}
	accountID, err := originRequestPolicyDataAccountID(ctx, cfg, sdkCfg)
	if err != nil {
		return nil, fmt.Errorf("read origin request policy %s: %w", id, err)
	}
	output := originRequestPolicyDataOutput(sdkCfg.Region, accountID, id, etag, policy)
	return (*OriginRequestPolicyResourceOutput)(output), nil
}

func (r *OriginRequestPolicyResource) Update(
	ctx context.Context, cfg *awsCfg, prior originRequestPolicyPrior,
) (*OriginRequestPolicyResourceOutput, error) {
	id := prior.Outputs.Id
	etag := priorOriginRequestPolicyETag(prior)
	if etag == "" {
		return nil, fmt.Errorf("update origin request policy %s: missing ETag", id)
	}
	client, sdkCfg, err := newClientConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("update origin request policy %s: %w", id, err)
	}
	accountID, err := originRequestPolicyDataAccountID(ctx, cfg, sdkCfg)
	if err != nil {
		return nil, fmt.Errorf("update origin request policy %s: %w", id, err)
	}
	_, err = client.UpdateOriginRequestPolicy(ctx, &cloudfront.UpdateOriginRequestPolicyInput{
		Id: aws.String(id), IfMatch: aws.String(etag), OriginRequestPolicyConfig: r.config(),
	})
	if err != nil {
		return nil, fmt.Errorf("update origin request policy %s: %w", id, err)
	}
	return observeOriginRequestPolicy(ctx, client, accountID, id, "update")
}

func (r *OriginRequestPolicyResource) Delete(
	ctx context.Context, cfg *awsCfg, prior originRequestPolicyPrior,
) error {
	id := prior.Outputs.Id
	etag := priorOriginRequestPolicyETag(prior)
	if etag == "" {
		return fmt.Errorf("delete origin request policy %s: missing ETag", id)
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("delete origin request policy %s: %w", id, err)
	}
	_, err = client.DeleteOriginRequestPolicy(ctx, &cloudfront.DeleteOriginRequestPolicyInput{
		Id: aws.String(id), IfMatch: aws.String(etag),
	})
	if err != nil && !isOriginRequestPolicyNotFound(err) {
		return fmt.Errorf("delete origin request policy %s: %w", id, err)
	}
	return nil
}

func priorOriginRequestPolicyETag(prior originRequestPolicyPrior) string {
	if prior.Observed != nil {
		return prior.Observed.ETag
	}
	return prior.Outputs.ETag
}

func observeOriginRequestPolicy(
	ctx context.Context, client *cloudfront.Client, accountID, id, operation string,
) (*OriginRequestPolicyResourceOutput, error) {
	policy, etag, err := getOriginRequestPolicy(ctx, client, id)
	if errors.Is(err, runtime.ErrNotFound) {
		return nil, fmt.Errorf("%s origin request policy %s observation: missing policy",
			operation, id)
	}
	if err != nil {
		return nil, fmt.Errorf("%s origin request policy %s observation: %w", operation, id, err)
	}
	output := originRequestPolicyDataOutput(client.Options().Region, accountID, id, etag, policy)
	return (*OriginRequestPolicyResourceOutput)(output), nil
}

func getOriginRequestPolicy(
	ctx context.Context, client *cloudfront.Client, id string,
) (*cloudfronttypes.OriginRequestPolicy, string, error) {
	resp, err := client.GetOriginRequestPolicy(ctx, &cloudfront.GetOriginRequestPolicyInput{
		Id: aws.String(id),
	})
	if isOriginRequestPolicyNotFound(err) {
		return nil, "", runtime.ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("get origin request policy %s: %w", id, err)
	}
	if resp == nil || resp.OriginRequestPolicy == nil ||
		resp.OriginRequestPolicy.OriginRequestPolicyConfig == nil {
		return nil, "", runtime.ErrNotFound
	}
	if got := aws.ToString(resp.OriginRequestPolicy.Id); got != "" && got != id {
		return nil, "", fmt.Errorf("get origin request policy %s: returned policy ID %q", id, got)
	}
	etag := aws.ToString(resp.ETag)
	if etag == "" {
		return nil, "", fmt.Errorf("get origin request policy %s: missing ETag", id)
	}
	return resp.OriginRequestPolicy, etag, nil
}
