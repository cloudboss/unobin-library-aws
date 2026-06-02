package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// encryptionNotFoundCodes are the S3 codes that mean no default encryption is
// present: ServerSideEncryptionConfigurationNotFoundError on a bucket without
// it, NoSuchBucket when the bucket is gone. A delete that hits either has
// nothing to remove.
var encryptionNotFoundCodes = []string{
	"NoSuchBucket",
	"ServerSideEncryptionConfigurationNotFoundError",
}

// BucketEncryption is the bucket's default server-side encryption. SSEAlgorithm
// is AES256, aws:kms, or aws:kms:dsse. KMSMasterKeyID applies only with a KMS
// algorithm. BucketKeyEnabled cuts KMS request cost. A nil block leaves
// encryption as it is. (Valid values + the KMS-key-requires-KMS-algorithm rule
// are API-validated; nested-block fields cannot carry unobin Constraints -- note
// them in the doc, do not add a Constraints method.)
type BucketEncryption struct {
	SSEAlgorithm     string  `ub:"sse-algorithm"`
	KMSMasterKeyID   *string `ub:"kms-master-key-id"`
	BucketKeyEnabled *bool   `ub:"bucket-key-enabled"`
}

// reconcileEncryption writes the bucket's default server-side encryption when
// desired differs from prior. A removed block (desired nil) is deleted, which
// returns the bucket to Amazon S3 managed encryption. The single rule carries
// the algorithm, an optional KMS key sent only with a KMS algorithm, and the
// bucket-key toggle.
func reconcileEncryption(
	ctx context.Context, client *s3.Client, bucket string,
	desired, prior *BucketEncryption,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	if desired == nil {
		return bucketConfigDelete(ctx, "encryption", encryptionNotFoundCodes,
			func(ctx context.Context) error {
				_, err := client.DeleteBucketEncryption(ctx, &s3.DeleteBucketEncryptionInput{
					Bucket: aws.String(bucket),
				})
				return err
			})
	}
	byDefault := &s3types.ServerSideEncryptionByDefault{
		SSEAlgorithm: s3types.ServerSideEncryption(desired.SSEAlgorithm),
	}
	if desired.KMSMasterKeyID != nil {
		byDefault.KMSMasterKeyID = desired.KMSMasterKeyID
	}
	return bucketConfigPut(ctx, "encryption", func(ctx context.Context) error {
		_, err := client.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
			Bucket: aws.String(bucket),
			ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
				Rules: []s3types.ServerSideEncryptionRule{
					{
						ApplyServerSideEncryptionByDefault: byDefault,
						BucketKeyEnabled:                   desired.BucketKeyEnabled,
					},
				},
			},
		})
		return err
	})
}
