package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// websiteNotFoundCodes are the S3 codes that mean no website configuration is
// present: NoSuchWebsiteConfiguration on a bucket without one, NoSuchBucket when
// the bucket is gone. A delete that hits either has nothing to remove.
var websiteNotFoundCodes = []string{
	"NoSuchBucket",
	"NoSuchWebsiteConfiguration",
}

// BucketWebsite is the bucket's static-website hosting configuration. A valid
// configuration is either a single redirect-all-requests-to, or an
// index-document with optional error-document and routing-rules; the two forms
// are mutually exclusive. A nil block leaves the website configuration as it is.
// (The exactly-one-form rule, the redirect-all exclusivity, and the http|https
// protocol values are all API-validated; nested-block fields cannot carry unobin
// Constraints -- note them here, do not add a Constraints method.)
type BucketWebsite struct {
	IndexDocument         *BucketWebsiteIndexDocument `ub:"index-document"`
	ErrorDocument         *BucketWebsiteErrorDocument `ub:"error-document"`
	RedirectAllRequestsTo *BucketWebsiteRedirectAll   `ub:"redirect-all-requests-to"`
	RoutingRules          []BucketWebsiteRoutingRule  `ub:"routing-rules"`
}

// BucketWebsiteIndexDocument names the suffix appended to a request for a
// directory on the website endpoint, such as index.html.
type BucketWebsiteIndexDocument struct {
	Suffix string `ub:"suffix"`
}

// BucketWebsiteErrorDocument names the object key returned when a 4XX error
// occurs.
type BucketWebsiteErrorDocument struct {
	Key string `ub:"key"`
}

// BucketWebsiteRedirectAll redirects every request to the website endpoint to
// the given host. Protocol is http or https; when unset the original request's
// protocol is used.
type BucketWebsiteRedirectAll struct {
	HostName string  `ub:"host-name"`
	Protocol *string `ub:"protocol"`
}

// BucketWebsiteRoutingRule pairs an optional matching condition with the
// redirect to apply when it matches.
type BucketWebsiteRoutingRule struct {
	Condition *BucketWebsiteCondition `ub:"condition"`
	Redirect  *BucketWebsiteRedirect  `ub:"redirect"`
}

// BucketWebsiteCondition describes when a routing rule's redirect applies, by
// returned HTTP error code, key prefix, or both.
type BucketWebsiteCondition struct {
	HTTPErrorCodeReturnedEquals *string `ub:"http-error-code-returned-equals"`
	KeyPrefixEquals             *string `ub:"key-prefix-equals"`
}

// BucketWebsiteRedirect describes how a matched request is redirected.
// Protocol is http or https. ReplaceKeyPrefixWith and ReplaceKeyWith are
// mutually exclusive.
type BucketWebsiteRedirect struct {
	HostName             *string `ub:"host-name"`
	HTTPRedirectCode     *string `ub:"http-redirect-code"`
	Protocol             *string `ub:"protocol"`
	ReplaceKeyPrefixWith *string `ub:"replace-key-prefix-with"`
	ReplaceKeyWith       *string `ub:"replace-key-with"`
}

// reconcileWebsite writes the bucket's static-website hosting configuration when
// desired differs from prior. A removed block (desired nil) is deleted, which
// turns off website hosting.
func reconcileWebsite(
	ctx context.Context, client *s3.Client, bucket string,
	desired, prior *BucketWebsite,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	if desired == nil {
		return bucketConfigDelete(ctx, "website", websiteNotFoundCodes,
			func(ctx context.Context) error {
				_, err := client.DeleteBucketWebsite(ctx, &s3.DeleteBucketWebsiteInput{
					Bucket: aws.String(bucket),
				})
				return err
			})
	}
	return bucketConfigPut(ctx, "website", func(ctx context.Context) error {
		_, err := client.PutBucketWebsite(ctx, &s3.PutBucketWebsiteInput{
			Bucket:               aws.String(bucket),
			WebsiteConfiguration: websiteConfiguration(desired),
		})
		return err
	})
}

// websiteConfiguration expands the desired block into the SDK type, setting each
// sub-object only when its pointer is present.
func websiteConfiguration(desired *BucketWebsite) *s3types.WebsiteConfiguration {
	config := &s3types.WebsiteConfiguration{}
	if desired.IndexDocument != nil {
		config.IndexDocument = &s3types.IndexDocument{
			Suffix: aws.String(desired.IndexDocument.Suffix),
		}
	}
	if desired.ErrorDocument != nil {
		config.ErrorDocument = &s3types.ErrorDocument{
			Key: aws.String(desired.ErrorDocument.Key),
		}
	}
	if desired.RedirectAllRequestsTo != nil {
		config.RedirectAllRequestsTo = websiteRedirectAll(desired.RedirectAllRequestsTo)
	}
	if len(desired.RoutingRules) > 0 {
		config.RoutingRules = websiteRoutingRules(desired.RoutingRules)
	}
	return config
}

// websiteRedirectAll expands the redirect-all-requests-to block, setting the
// protocol only when present.
func websiteRedirectAll(in *BucketWebsiteRedirectAll) *s3types.RedirectAllRequestsTo {
	out := &s3types.RedirectAllRequestsTo{HostName: aws.String(in.HostName)}
	if in.Protocol != nil {
		out.Protocol = s3types.Protocol(*in.Protocol)
	}
	return out
}

// websiteRoutingRules expands the routing rules, setting each rule's condition
// and redirect only when present.
func websiteRoutingRules(in []BucketWebsiteRoutingRule) []s3types.RoutingRule {
	rules := make([]s3types.RoutingRule, 0, len(in))
	for _, rule := range in {
		out := s3types.RoutingRule{}
		if rule.Condition != nil {
			out.Condition = websiteCondition(rule.Condition)
		}
		if rule.Redirect != nil {
			out.Redirect = websiteRedirect(rule.Redirect)
		}
		rules = append(rules, out)
	}
	return rules
}

// websiteCondition expands a routing rule's condition, setting each field only
// when present.
func websiteCondition(in *BucketWebsiteCondition) *s3types.Condition {
	out := &s3types.Condition{}
	if in.HTTPErrorCodeReturnedEquals != nil {
		out.HttpErrorCodeReturnedEquals = in.HTTPErrorCodeReturnedEquals
	}
	if in.KeyPrefixEquals != nil {
		out.KeyPrefixEquals = in.KeyPrefixEquals
	}
	return out
}

// websiteRedirect expands a routing rule's redirect, setting each field only
// when present and the protocol as the SDK enum.
func websiteRedirect(in *BucketWebsiteRedirect) *s3types.Redirect {
	out := &s3types.Redirect{}
	if in.HostName != nil {
		out.HostName = in.HostName
	}
	if in.HTTPRedirectCode != nil {
		out.HttpRedirectCode = in.HTTPRedirectCode
	}
	if in.Protocol != nil {
		out.Protocol = s3types.Protocol(*in.Protocol)
	}
	if in.ReplaceKeyPrefixWith != nil {
		out.ReplaceKeyPrefixWith = in.ReplaceKeyPrefixWith
	}
	if in.ReplaceKeyWith != nil {
		out.ReplaceKeyWith = in.ReplaceKeyWith
	}
	return out
}
