package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// BucketLogging is the bucket's server access logging configuration.
// TargetBucket and TargetPrefix name where access logs are delivered and the key
// prefix S3 assigns them. TargetGrants optionally grant log-delivery permissions
// to other grantees; they are unsupported on buckets that enforce bucket-owner
// ownership. TargetObjectKeyFormat optionally chooses the log object key layout:
// exactly one of partitioned-prefix or simple-prefix applies. A nil block leaves
// logging as it is.
type BucketLogging struct {
	TargetBucket          string                              `ub:"target-bucket"`
	TargetPrefix          string                              `ub:"target-prefix"`
	TargetGrants          []BucketLoggingTargetGrant          `ub:"target-grants"`
	TargetObjectKeyFormat *BucketLoggingTargetObjectKeyFormat `ub:"target-object-key-format"`
}

// BucketLoggingTargetGrant grants one grantee a logging permission on the
// target bucket.
type BucketLoggingTargetGrant struct {
	Grantee    *BucketLoggingGrantee `ub:"grantee"`
	Permission string                `ub:"permission"`
}

// BucketLoggingGrantee identifies the person or group a logging permission is
// granted to. Type selects which identifier applies: a canonical user ID, an
// email address, or a group URI.
type BucketLoggingGrantee struct {
	Type         string  `ub:"type"`
	EmailAddress *string `ub:"email-address"`
	ID           *string `ub:"id"`
	URI          *string `ub:"uri"`
}

// BucketLoggingTargetObjectKeyFormat chooses the key layout for delivered log
// objects. Exactly one of PartitionedPrefix or SimplePrefix applies; SimplePrefix
// is a presence flag whose true value selects the simple, empty-object format.
type BucketLoggingTargetObjectKeyFormat struct {
	PartitionedPrefix *BucketLoggingPartitionedPrefix `ub:"partitioned-prefix"`
	SimplePrefix      *bool                           `ub:"simple-prefix"`
}

// BucketLoggingPartitionedPrefix partitions delivered log object keys.
// PartitionDateSource is EventTime or DeliveryTime.
type BucketLoggingPartitionedPrefix struct {
	PartitionDateSource string `ub:"partition-date-source"`
}

// reconcileLogging writes the bucket's server access logging configuration when
// desired differs from prior. A removed block (desired nil) puts an empty status,
// the nearest S3 has to off, since there is no call to delete logging. The
// configuration carries the target bucket and prefix, optional grants, and the
// optional log object key format.
func reconcileLogging(
	ctx context.Context, client *s3.Client, bucket string, desired, prior *BucketLogging,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	status := &s3types.BucketLoggingStatus{}
	if desired != nil {
		enabled := &s3types.LoggingEnabled{
			TargetBucket: aws.String(desired.TargetBucket),
			TargetPrefix: aws.String(desired.TargetPrefix),
		}
		if len(desired.TargetGrants) > 0 {
			enabled.TargetGrants = loggingTargetGrants(desired.TargetGrants)
		}
		if desired.TargetObjectKeyFormat != nil {
			enabled.TargetObjectKeyFormat = loggingKeyFormat(desired.TargetObjectKeyFormat)
		}
		status.LoggingEnabled = enabled
	}
	return bucketConfigPut(ctx, "logging", func(ctx context.Context) error {
		_, err := client.PutBucketLogging(ctx, &s3.PutBucketLoggingInput{
			Bucket:              aws.String(bucket),
			BucketLoggingStatus: status,
		})
		return err
	})
}

// loggingTargetGrants maps the logging target grants to their SDK form.
func loggingTargetGrants(grants []BucketLoggingTargetGrant) []s3types.TargetGrant {
	out := make([]s3types.TargetGrant, 0, len(grants))
	for _, g := range grants {
		grant := s3types.TargetGrant{
			Permission: s3types.BucketLogsPermission(g.Permission),
		}
		if g.Grantee != nil {
			grant.Grantee = loggingGrantee(g.Grantee)
		}
		out = append(out, grant)
	}
	return out
}

// loggingGrantee maps one logging grantee to its SDK form, sending only the
// identifier fields that are set.
func loggingGrantee(g *BucketLoggingGrantee) *s3types.Grantee {
	grantee := &s3types.Grantee{Type: s3types.Type(g.Type)}
	if g.EmailAddress != nil {
		grantee.EmailAddress = g.EmailAddress
	}
	if g.ID != nil {
		grantee.ID = g.ID
	}
	if g.URI != nil {
		grantee.URI = g.URI
	}
	return grantee
}

// loggingKeyFormat maps the log object key format to its SDK form. PartitionedPrefix
// rides when set; SimplePrefix becomes an empty object when its flag is true.
func loggingKeyFormat(f *BucketLoggingTargetObjectKeyFormat) *s3types.TargetObjectKeyFormat {
	format := &s3types.TargetObjectKeyFormat{}
	if f.PartitionedPrefix != nil {
		format.PartitionedPrefix = &s3types.PartitionedPrefix{
			PartitionDateSource: s3types.PartitionDateSource(f.PartitionedPrefix.PartitionDateSource),
		}
	}
	if f.SimplePrefix != nil && *f.SimplePrefix {
		format.SimplePrefix = &s3types.SimplePrefix{}
	}
	return format
}
