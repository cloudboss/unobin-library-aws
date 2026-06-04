package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// BucketObjectLock is the bucket's Object Lock default retention
// rule. It requires object lock to be enabled on the bucket at creation
// (object-lock-enabled = true on the bucket). A nil block leaves any existing
// rule in place -- object lock cannot be turned off once enabled. Mode is
// GOVERNANCE or COMPLIANCE, and the retention takes exactly one of days or
// years.
type BucketObjectLock struct {
	Rule BucketObjectLockRule `ub:"rule"`
}

type BucketObjectLockRule struct {
	DefaultRetention BucketObjectLockDefaultRetention `ub:"default-retention"`
}

type BucketObjectLockDefaultRetention struct {
	Mode  string `ub:"mode"`
	Days  *int64 `ub:"days"`
	Years *int64 `ub:"years"`
}

// reconcileObjectLock writes the bucket's Object Lock default
// retention rule when desired differs from prior. A removed block (desired nil)
// is a no-op: object lock cannot be disabled once enabled, and S3 has no call to
// remove a default retention rule. ObjectLockEnabled is always Enabled.
func reconcileObjectLock(
	ctx context.Context, client *s3.Client, bucket string,
	desired, prior *BucketObjectLock,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	if desired == nil {
		return nil
	}
	retention := desired.Rule.DefaultRetention
	return bucketConfigPut(ctx, "object lock configuration", func(ctx context.Context) error {
		_, err := client.PutObjectLockConfiguration(ctx, &s3.PutObjectLockConfigurationInput{
			Bucket: aws.String(bucket),
			ObjectLockConfiguration: &s3types.ObjectLockConfiguration{
				ObjectLockEnabled: s3types.ObjectLockEnabledEnabled,
				Rule: &s3types.ObjectLockRule{
					DefaultRetention: &s3types.DefaultRetention{
						Mode:  s3types.ObjectLockRetentionMode(retention.Mode),
						Days:  ptr.Int32(retention.Days),
						Years: ptr.Int32(retention.Years),
					},
				},
			},
		})
		return err
	})
}
