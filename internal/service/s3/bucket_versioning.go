package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// BucketVersioning is the bucket's versioning configuration. Status is
// Enabled or Suspended; MfaDelete, Enabled or Disabled, requires the bucket
// owner's MFA to delete a version or change versioning. A nil block leaves
// versioning as it is.
type BucketVersioning struct {
	Status    string  `ub:"status"`
	MfaDelete *string `ub:"mfa-delete"`
}

// reconcileVersioning writes the bucket's versioning configuration when desired
// differs from prior. A removed block (desired nil) suspends versioning, the
// nearest S3 has to off, since there is no call to return a bucket to
// unversioned. MfaDelete rides the configuration when set.
func reconcileVersioning(
	ctx context.Context, client *s3.Client, bucket string, desired, prior *BucketVersioning,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	status := s3types.BucketVersioningStatusSuspended
	var mfaDelete *string
	if desired != nil {
		status = s3types.BucketVersioningStatus(desired.Status)
		mfaDelete = desired.MfaDelete
	}
	cfg := &s3types.VersioningConfiguration{Status: status}
	if mfaDelete != nil {
		cfg.MFADelete = s3types.MFADelete(*mfaDelete)
	}
	return bucketConfigPut(ctx, "versioning", func(ctx context.Context) error {
		_, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket:                  aws.String(bucket),
			VersioningConfiguration: cfg,
		})
		return err
	})
}
