package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// pabNotFoundCodes are the S3 codes that mean no public-access block is present:
// NoSuchPublicAccessBlockConfiguration on a bucket without one, NoSuchBucket on
// a bucket that is gone. A delete that hits either has nothing to remove.
var pabNotFoundCodes = []string{
	"NoSuchBucket",
	"NoSuchPublicAccessBlockConfiguration",
}

// BucketPublicAccessBlock is the bucket's public-access block. The four
// booleans each default to false: the put replaces the whole configuration
// and S3 reads an omitted member back as false, so a nil member means false
// rather than a kept prior value. A nil block leaves the public-access block
// as it is.
type BucketPublicAccessBlock struct {
	BlockPublicAcls       *bool `ub:"block-public-acls"`
	BlockPublicPolicy     *bool `ub:"block-public-policy"`
	IgnorePublicAcls      *bool `ub:"ignore-public-acls"`
	RestrictPublicBuckets *bool `ub:"restrict-public-buckets"`
}

// reconcilePublicAccessBlock writes the bucket's public-access block when desired
// differs from prior, sending the members as given; S3 defaults an omitted one
// to false. A removed block (desired nil) is deleted.
func reconcilePublicAccessBlock(
	ctx context.Context, client *s3.Client, bucket string,
	desired, prior *BucketPublicAccessBlock,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	if desired == nil {
		return bucketConfigDelete(ctx, "public access block", pabNotFoundCodes,
			func(ctx context.Context) error {
				_, err := client.DeletePublicAccessBlock(ctx, &s3.DeletePublicAccessBlockInput{
					Bucket: aws.String(bucket),
				})
				return err
			})
	}
	return bucketConfigPut(ctx, "public access block", func(ctx context.Context) error {
		_, err := client.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
			Bucket: aws.String(bucket),
			PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
				BlockPublicAcls:       desired.BlockPublicAcls,
				BlockPublicPolicy:     desired.BlockPublicPolicy,
				IgnorePublicAcls:      desired.IgnorePublicAcls,
				RestrictPublicBuckets: desired.RestrictPublicBuckets,
			},
		})
		return err
	})
}
