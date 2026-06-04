package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// ownershipControlsNotFoundCodes are the codes meaning no ownership controls are
// present: OwnershipControlsNotFoundError on a bucket without them, NoSuchBucket
// when the bucket is gone. A clear that hits either has nothing to remove.
var ownershipControlsNotFoundCodes = []string{"NoSuchBucket", "OwnershipControlsNotFoundError"}

// BucketOwnershipControls is the bucket's object-ownership setting.
// ObjectOwnership is BucketOwnerPreferred, ObjectWriter, or BucketOwnerEnforced.
// A nil block leaves ownership controls as they are.
type BucketOwnershipControls struct {
	ObjectOwnership string `ub:"object-ownership"`
}

// reconcileOwnershipControls writes the bucket's ownership controls when desired
// differs from prior, collapsing the single object-ownership setting into the
// one-rule list S3 expects. A removed block (desired nil) is deleted.
func reconcileOwnershipControls(
	ctx context.Context, client *s3.Client, bucket string,
	desired, prior *BucketOwnershipControls,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	if desired == nil {
		return bucketConfigDelete(ctx, "ownership controls", ownershipControlsNotFoundCodes,
			func(ctx context.Context) error {
				_, err := client.DeleteBucketOwnershipControls(ctx,
					&s3.DeleteBucketOwnershipControlsInput{
						Bucket: aws.String(bucket),
					})
				return err
			})
	}
	return bucketConfigPut(ctx, "ownership controls", func(ctx context.Context) error {
		_, err := client.PutBucketOwnershipControls(ctx, &s3.PutBucketOwnershipControlsInput{
			Bucket: aws.String(bucket),
			OwnershipControls: &s3types.OwnershipControls{
				Rules: []s3types.OwnershipControlsRule{
					{ObjectOwnership: s3types.ObjectOwnership(desired.ObjectOwnership)},
				},
			},
		})
		return err
	})
}
