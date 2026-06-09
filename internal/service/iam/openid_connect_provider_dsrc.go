package iam

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/cloudboss/unobin/pkg/constraint"
)

// OpenIDConnectProviderData resolves an existing IAM OpenID Connect (OIDC)
// provider by either its arn or its url, exactly one of which must be set. Given
// an arn it reads that provider directly; given a url it lists every provider in
// the account and matches by the url that each provider's arn embeds after its
// slash, then reads the matching arn. Unlike a resource read, a data source must
// resolve, so a lookup that finds nothing returns a descriptive error rather
// than runtime.ErrNotFound.
type OpenIDConnectProviderData struct {
	Arn *string `ub:"arn"`
	Url *string `ub:"url"`
}

// OpenIDConnectProviderDataOutput holds the resolved provider's attributes. Arn
// is the canonical resolved arn and Url is IAM's scheme-less form, so a
// reference to either reads the value IAM stores rather than the lookup input.
type OpenIDConnectProviderDataOutput struct {
	Arn            string            `ub:"arn"`
	Url            string            `ub:"url"`
	ClientIDList   []string          `ub:"client-id-list"`
	ThumbprintList []string          `ub:"thumbprint-list"`
	Tags           map[string]string `ub:"tags"`
}

// Constraints requires exactly one of arn or url, the two mutually exclusive
// lookup keys. The url's own format rules (a valid https url with no query) are a
// string-content check the constraint vocabulary cannot express, so Read
// enforces them instead.
func (r OpenIDConnectProviderData) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.Arn, r.Url),
	}
}

// Read resolves the provider and returns its attributes. It determines the arn
// from the arn input or, failing that, by matching the url input against the
// providers in the account, then reads that arn. A url input is validated before
// use, and any lookup that matches nothing is a descriptive error.
func (r *OpenIDConnectProviderData) Read(
	ctx context.Context, cfg any,
) (*OpenIDConnectProviderDataOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn, err := r.resolveArn(ctx, client)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(arn),
	})
	if err != nil {
		var notFound *iamtypes.NoSuchEntityException
		if errors.As(err, &notFound) {
			return nil, fmt.Errorf("iam oidc provider not found for arn %q", arn)
		}
		return nil, fmt.Errorf("get oidc provider: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("iam oidc provider not found for arn %q", arn)
	}
	return &OpenIDConnectProviderDataOutput{
		Arn:            arn,
		Url:            aws.ToString(resp.Url),
		ClientIDList:   resp.ClientIDList,
		ThumbprintList: resp.ThumbprintList,
		Tags:           tagsToMap(resp.Tags),
	}, nil
}

// resolveArn returns the arn to read. When the arn input is set it is used
// directly; otherwise the url input is validated and matched against the
// account's providers.
func (r *OpenIDConnectProviderData) resolveArn(
	ctx context.Context, client *iam.Client,
) (string, error) {
	if r.Arn != nil {
		return *r.Arn, nil
	}
	wanted := aws.ToString(r.Url)
	if err := validateOpenIDURL(wanted); err != nil {
		return "", err
	}
	return r.findArnByURL(ctx, client, wanted)
}

// findArnByURL lists every OIDC provider in the account and returns the arn
// whose embedded url matches the input url with its leading https scheme
// stripped. IAM stores the url scheme-less, so the comparison strips https from
// the input first. No provider matching the url is a descriptive error.
func (r *OpenIDConnectProviderData) findArnByURL(
	ctx context.Context, client *iam.Client, wanted string,
) (string, error) {
	resp, err := client.ListOpenIDConnectProviders(
		ctx, &iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return "", fmt.Errorf("list oidc providers: %w", err)
	}
	target := strings.TrimPrefix(wanted, "https://")
	for _, entry := range resp.OpenIDConnectProviderList {
		arn := aws.ToString(entry.Arn)
		if arn == "" {
			continue
		}
		embedded, err := urlFromProviderARN(arn)
		if err != nil {
			return "", err
		}
		if embedded == target {
			return arn, nil
		}
	}
	return "", fmt.Errorf("iam oidc provider not found for url %q", wanted)
}

// urlFromProviderARN extracts the url an OIDC provider arn embeds: the arn holds
// the url after its first slash, not a hash. An arn without that slash is
// malformed and is a descriptive error.
func urlFromProviderARN(arn string) (string, error) {
	parts := strings.SplitN(arn, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("malformed oidc provider arn %q", arn)
	}
	return parts[1], nil
}

// validateOpenIDURL rejects a url input that is not a valid https url or that
// has query parameters, mirroring IAM's own url rules. This is a string-content
// check that a derived constraint cannot express, so it runs here.
func validateOpenIDURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url %q is not a valid URL: %w", raw, err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("url %q must use the https scheme", raw)
	}
	if parsed.Host == "" {
		return fmt.Errorf("url %q must include a host", raw)
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("url %q must not contain query parameters", raw)
	}
	return nil
}

// tagsToMap converts IAM's tag list into a map keyed by tag key. A nil list
// yields a nil map.
func tagsToMap(tags []iamtypes.Tag) map[string]string {
	if tags == nil {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, t := range tags {
		out[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return out
}
