// verify checks the S3 group the scenario applied against the phase named in
// the VERIFY_PHASE environment variable. It only reads cloud state: applied
// requires the bucket present with versioning enabled, the public access block
// fully on, a bucket policy in place, and the object readable; destroyed
// requires the bucket gone, which takes the whole group with it. Tearing the
// group down is the destroy plan's job, not the verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	bucketName = "unobin-it-bucket"
	objectKey  = "hello.txt"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	// Against a custom endpoint such as LocalStack, reach buckets by path rather
	// than the virtual-hosted bucket.host name its DNS does not resolve.
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if cfg.BaseEndpoint != nil {
			o.UsePathStyle = true
		}
	})

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *s3.Client) error {
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	}); err != nil {
		return fmt.Errorf("head bucket %s: %w", bucketName, err)
	}

	versioning, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return fmt.Errorf("get bucket versioning %s: %w", bucketName, err)
	}
	if versioning.Status != s3types.BucketVersioningStatusEnabled {
		return fmt.Errorf("bucket %s versioning is %q, want Enabled",
			bucketName, versioning.Status)
	}

	pab, err := client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return fmt.Errorf("get public access block %s: %w", bucketName, err)
	}
	cfg := pab.PublicAccessBlockConfiguration
	if cfg == nil || !aws.ToBool(cfg.BlockPublicAcls) || !aws.ToBool(cfg.BlockPublicPolicy) ||
		!aws.ToBool(cfg.IgnorePublicAcls) || !aws.ToBool(cfg.RestrictPublicBuckets) {
		return fmt.Errorf("bucket %s public access block is not fully on: %+v", bucketName, cfg)
	}

	policy, err := client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return fmt.Errorf("get bucket policy %s: %w", bucketName, err)
	}
	if aws.ToString(policy.Policy) == "" {
		return fmt.Errorf("bucket %s has an empty policy", bucketName)
	}

	if _, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	}); err != nil {
		return fmt.Errorf("head object %s/%s: %w", bucketName, objectKey, err)
	}

	fmt.Printf("ok: bucket %s versioned, locked down, policied, with object %s\n",
		bucketName, objectKey)
	return nil
}

func verifyDestroyed(ctx context.Context, client *s3.Client) error {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketName)})
	if err == nil {
		return fmt.Errorf("bucket %s still exists", bucketName)
	}
	if !isNotFound(err) {
		return fmt.Errorf("head bucket %s: %w", bucketName, err)
	}
	fmt.Printf("ok: bucket %s and its group are gone\n", bucketName)
	return nil
}

// isNotFound reports whether err means the bucket is gone. HeadBucket answers a
// missing general-purpose bucket with a bare HTTP 404 and no service code, so
// the status is checked alongside the NoSuchBucket and NotFound codes.
func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchBucket", "NotFound":
			return true
		}
	}
	var respErr *smithyhttp.ResponseError
	return errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotFound
}
