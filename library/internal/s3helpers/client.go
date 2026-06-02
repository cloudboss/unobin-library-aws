package s3helpers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"

	"github.com/cloudboss/unobin-library-aws/library/config"
)

// NewClient returns the AWS SDK Go v2 client for s3, configured from cfg.
// cfg is the *config.Configuration the runtime hands every lifecycle
// method; the helper unwraps it and builds an aws.Config via
// config.LoadAWSConfig.
func NewClient(ctx context.Context, cfg any) (*s3.Client, error) {
	c, ok := cfg.(*config.Configuration)
	if !ok {
		return nil, fmt.Errorf("s3client: unexpected configuration type %T", cfg)
	}
	awsCfg, err := config.LoadAWSConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		// A custom endpoint -- LocalStack or another S3-compatible server -- is
		// reached by path (host/bucket), not by the virtual-hosted bucket.host
		// name the default addressing builds, which the endpoint's DNS does not
		// resolve. Real AWS, with no base endpoint set, keeps virtual hosting.
		if awsCfg.BaseEndpoint != nil {
			o.UsePathStyle = true
		}
	}), nil
}

// IsNotFound reports whether err is an AWS API error whose service code is
// one of codes. S3 reports a missing bucket, object, or sub-resource config
// with a service code: some are typed exceptions (NoSuchBucket, NoSuchKey,
// NotFound), others are plain codes (NoSuchBucketPolicy,
// NoSuchPublicAccessBlockConfiguration). Both reach the caller as a
// smithy.APIError, so a resource Read matches the code to turn a read of a
// gone resource into runtime.ErrNotFound.
func IsNotFound(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return slices.Contains(codes, apiErr.ErrorCode())
	}
	return false
}

// IsOperationAborted reports whether err is S3's OperationAborted. S3
// serializes the configuration operations against a single bucket -- its
// versioning, policy, public access block, and the rest -- and rejects one
// that arrives while another is in progress with this code. Several such
// resources sharing a bucket apply at once, so each retries through it until
// the bucket is free.
func IsOperationAborted(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "OperationAborted"
}
