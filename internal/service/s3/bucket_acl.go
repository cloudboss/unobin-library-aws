package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// BucketAcl sets the bucket's canned access control list. Acl is one of the S3
// canned ACLs: private, public-read, public-read-write, authenticated-read,
// aws-exec-read, bucket-owner-read, bucket-owner-full-control, log-delivery-write.
// A nil block leaves the bucket's ACL as it is; removing a previously-set block
// does NOT reset the ACL -- S3 has no operation to un-set one. This block
// requires ACLs to be enabled: if ownership-controls sets object-ownership to
// BucketOwnerEnforced, ACLs are disabled and setting this fails with
// AccessControlListNotSupported.
type BucketAcl struct {
	Acl string `ub:"acl"`
}

// reconcileAcl writes the bucket's canned ACL when desired differs from prior. A
// removed block (desired nil) is a no-op: S3 has no DeleteBucketAcl and an ACL
// cannot be un-set, so a once-set ACL stays as it is rather than reverting to
// private.
func reconcileAcl(
	ctx context.Context, client *s3.Client, bucket string, desired, prior *BucketAcl,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	if desired == nil {
		return nil
	}
	return bucketConfigPut(ctx, "acl", func(ctx context.Context) error {
		_, err := client.PutBucketAcl(ctx, &s3.PutBucketAclInput{
			Bucket: aws.String(bucket),
			ACL:    s3types.BucketCannedACL(desired.Acl),
		})
		return err
	})
}
