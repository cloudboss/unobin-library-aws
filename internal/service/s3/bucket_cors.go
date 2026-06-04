package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// corsNotFoundCodes are the S3 codes that mean no CORS configuration is present:
// NoSuchCORSConfiguration on a bucket without one, NoSuchBucket when the bucket
// is gone. A delete that hits either has nothing to remove.
var corsNotFoundCodes = []string{
	"NoSuchBucket",
	"NoSuchCORSConfiguration",
}

// BucketCors is the bucket's cross-origin resource sharing configuration, a
// list of up to 100 rules. A nil block leaves the CORS configuration as it is.
type BucketCors struct {
	Rules []BucketCorsRule `ub:"rules"`
}

// BucketCorsRule is one cross-origin access rule. AllowedMethods and
// AllowedOrigins are required. AllowedMethods values are GET, PUT, POST,
// DELETE, and HEAD, validated by the API: a constraint cannot reach the
// elements of a string list.
type BucketCorsRule struct {
	ID             *string  `ub:"id"`
	AllowedHeaders []string `ub:"allowed-headers"`
	AllowedMethods []string `ub:"allowed-methods"`
	AllowedOrigins []string `ub:"allowed-origins"`
	ExposeHeaders  []string `ub:"expose-headers"`
	MaxAgeSeconds  *int64   `ub:"max-age-seconds"`
}

// reconcileCors writes the bucket's CORS configuration when desired differs from
// prior. A removed block (desired nil) is deleted, which clears cross-origin
// access.
func reconcileCors(
	ctx context.Context, client *s3.Client, bucket string,
	desired, prior *BucketCors,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	if desired == nil {
		return bucketConfigDelete(ctx, "cors", corsNotFoundCodes,
			func(ctx context.Context) error {
				_, err := client.DeleteBucketCors(ctx, &s3.DeleteBucketCorsInput{
					Bucket: aws.String(bucket),
				})
				return err
			})
	}
	return bucketConfigPut(ctx, "cors", func(ctx context.Context) error {
		_, err := client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
			Bucket: aws.String(bucket),
			CORSConfiguration: &s3types.CORSConfiguration{
				CORSRules: corsRules(desired.Rules),
			},
		})
		return err
	})
}

// corsRules expands the desired rules into the SDK type, setting each slice and
// field only when non-empty and the id only when present.
func corsRules(in []BucketCorsRule) []s3types.CORSRule {
	rules := make([]s3types.CORSRule, 0, len(in))
	for _, rule := range in {
		out := s3types.CORSRule{}
		if rule.ID != nil {
			out.ID = rule.ID
		}
		if len(rule.AllowedHeaders) > 0 {
			out.AllowedHeaders = rule.AllowedHeaders
		}
		if len(rule.AllowedMethods) > 0 {
			out.AllowedMethods = rule.AllowedMethods
		}
		if len(rule.AllowedOrigins) > 0 {
			out.AllowedOrigins = rule.AllowedOrigins
		}
		if len(rule.ExposeHeaders) > 0 {
			out.ExposeHeaders = rule.ExposeHeaders
		}
		out.MaxAgeSeconds = ptr.Int32(rule.MaxAgeSeconds)
		rules = append(rules, out)
	}
	return rules
}
