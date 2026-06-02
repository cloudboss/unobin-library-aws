package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// BucketAccelerate is the bucket's Transfer Acceleration configuration. Status
// is Enabled or Suspended. A nil block leaves acceleration as it is. (Status is
// API-validated as Enabled|Suspended; nested-block fields cannot carry unobin
// Constraints, so note valid values in the doc, do not add a Constraints method.)
type BucketAccelerate struct {
	Status string `ub:"status"`
}

// reconcileAccelerate writes the bucket's transfer acceleration configuration
// when desired differs from prior. A removed block (desired nil) suspends
// acceleration, the nearest S3 has to off, since there is no call to clear it.
func reconcileAccelerate(
	ctx context.Context, client *s3.Client, bucket string, desired, prior *BucketAccelerate,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	status := s3types.BucketAccelerateStatusSuspended
	if desired != nil {
		status = s3types.BucketAccelerateStatus(desired.Status)
	}
	return bucketConfigPut(ctx, "accelerate", func(ctx context.Context) error {
		_, err := client.PutBucketAccelerateConfiguration(
			ctx, &s3.PutBucketAccelerateConfigurationInput{
				Bucket:                  aws.String(bucket),
				AccelerateConfiguration: &s3types.AccelerateConfiguration{Status: status},
			})
		return err
	})
}
