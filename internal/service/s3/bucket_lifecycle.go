package s3

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// lifecycleNotFoundCodes are the S3 codes that mean no lifecycle configuration
// is present: NoSuchLifecycleConfiguration on a bucket without one, NoSuchBucket
// when the bucket is gone, and MethodNotAllowed on an endpoint that does not
// allow the operation. A delete that hits any of these has nothing to remove.
var lifecycleNotFoundCodes = []string{
	"NoSuchBucket",
	"NoSuchLifecycleConfiguration",
	"MethodNotAllowed",
}

// BucketLifecycle is the bucket's lifecycle configuration, a list of rules
// that expire, transition, or abort objects on a schedule. A nil block leaves
// the lifecycle configuration as it is.
type BucketLifecycle struct {
	Rules []BucketLifecycleRule `ub:"rules"`
}

// BucketLifecycleRule is one lifecycle rule. ID and Status are required; Status
// is Enabled or Disabled. A rule scopes its objects with Filter (an empty or
// absent filter matches every object) and takes at least one action: an
// expiration, a list of transitions, a noncurrent-version expiration or
// transitions, or an incomplete-multipart-upload abort. The rules inside the
// transition lists are API-validated: a constraint cannot reach a list inside
// a list element.
type BucketLifecycleRule struct {
	ID                             string                        `ub:"id"`
	Status                         string                        `ub:"status"`
	Filter                         *BucketLifecycleFilter        `ub:"filter"`
	Expiration                     *BucketLifecycleExpiration    `ub:"expiration"`
	Transitions                    []BucketLifecycleTransition   `ub:"transitions"`
	NoncurrentVersionExpiration    *BucketLifecycleNCExpiration  `ub:"noncurrent-version-expiration"`
	NoncurrentVersionTransitions   []BucketLifecycleNCTransition `ub:"noncurrent-version-transitions"`
	AbortIncompleteMultipartUpload *BucketLifecycleAbortUpload   `ub:"abort-incomplete-multipart-upload"`
}

// BucketLifecycleFilter scopes a rule to a subset of objects. Exactly one of
// Prefix, Tag, ObjectSizeGreaterThan, ObjectSizeLessThan, or And applies; an
// empty filter matches every object in the bucket.
type BucketLifecycleFilter struct {
	Prefix                *string             `ub:"prefix"`
	Tag                   *BucketLifecycleTag `ub:"tag"`
	ObjectSizeGreaterThan *int64              `ub:"object-size-greater-than"`
	ObjectSizeLessThan    *int64              `ub:"object-size-less-than"`
	And                   *BucketLifecycleAnd `ub:"and"`
}

// BucketLifecycleTag is a single key-value tag a filter matches on.
type BucketLifecycleTag struct {
	Key   string `ub:"key"`
	Value string `ub:"value"`
}

// BucketLifecycleAnd combines several predicates a rule must match all
// of: an optional prefix, a set of tags, and object-size bounds.
type BucketLifecycleAnd struct {
	Prefix                *string           `ub:"prefix"`
	Tags                  map[string]string `ub:"tags"`
	ObjectSizeGreaterThan *int64            `ub:"object-size-greater-than"`
	ObjectSizeLessThan    *int64            `ub:"object-size-less-than"`
}

// BucketLifecycleExpiration expires the current object version. Exactly one of
// Date, Days, or ExpiredObjectDeleteMarker applies. Date is an RFC3339 time.
type BucketLifecycleExpiration struct {
	Date                      *string `ub:"date"`
	Days                      *int64  `ub:"days"`
	ExpiredObjectDeleteMarker *bool   `ub:"expired-object-delete-marker"`
}

// BucketLifecycleTransition moves the current object version to another
// storage class on a schedule. Exactly one of Date or Days sets the timing; Date
// is an RFC3339 time. StorageClass is one of GLACIER, STANDARD_IA, ONEZONE_IA,
// INTELLIGENT_TIERING, DEEP_ARCHIVE, or GLACIER_IR.
type BucketLifecycleTransition struct {
	Date         *string `ub:"date"`
	Days         *int64  `ub:"days"`
	StorageClass string  `ub:"storage-class"`
}

// BucketLifecycleNCExpiration expires noncurrent object
// versions NoncurrentDays after they become noncurrent, optionally retaining the
// NewerNoncurrentVersions most recent of them.
type BucketLifecycleNCExpiration struct {
	NoncurrentDays          *int64 `ub:"noncurrent-days"`
	NewerNoncurrentVersions *int64 `ub:"newer-noncurrent-versions"`
}

// BucketLifecycleNCTransition moves noncurrent object versions
// to another storage class NoncurrentDays after they become noncurrent,
// optionally retaining the NewerNoncurrentVersions most recent of them.
// StorageClass takes the same values as a current-version transition.
type BucketLifecycleNCTransition struct {
	NoncurrentDays          *int64 `ub:"noncurrent-days"`
	NewerNoncurrentVersions *int64 `ub:"newer-noncurrent-versions"`
	StorageClass            string `ub:"storage-class"`
}

// BucketLifecycleAbortUpload aborts an incomplete multipart
// upload DaysAfterInitiation days after it begins, freeing the stored parts.
type BucketLifecycleAbortUpload struct {
	DaysAfterInitiation *int64 `ub:"days-after-initiation"`
}

// reconcileLifecycle writes the bucket's lifecycle configuration when desired
// differs from prior. A removed block (desired nil) is deleted, which clears
// every rule.
func reconcileLifecycle(
	ctx context.Context, client *s3.Client, bucket string,
	desired, prior *BucketLifecycle,
) error {
	if !runtime.Changed(prior, desired) {
		return nil
	}
	if desired == nil {
		return bucketConfigDelete(ctx, "lifecycle", lifecycleNotFoundCodes,
			func(ctx context.Context) error {
				_, err := client.DeleteBucketLifecycle(ctx, &s3.DeleteBucketLifecycleInput{
					Bucket: aws.String(bucket),
				})
				return err
			})
	}
	rules, err := lifecycleRules(desired.Rules)
	if err != nil {
		return err
	}
	return bucketConfigPut(ctx, "lifecycle", func(ctx context.Context) error {
		_, err := client.PutBucketLifecycleConfiguration(
			ctx, &s3.PutBucketLifecycleConfigurationInput{
				Bucket: aws.String(bucket),
				LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
					Rules: rules,
				},
			})
		return err
	})
}

// lifecycleRules expands the desired rules into the SDK type, setting each
// sub-object on a rule only when its pointer or list is present. A bad RFC3339
// date in any expiration or transition fails the whole expansion.
func lifecycleRules(in []BucketLifecycleRule) ([]s3types.LifecycleRule, error) {
	rules := make([]s3types.LifecycleRule, 0, len(in))
	for _, rule := range in {
		out := s3types.LifecycleRule{
			ID:     aws.String(rule.ID),
			Status: s3types.ExpirationStatus(rule.Status),
		}
		if rule.Filter != nil {
			out.Filter = lifecycleFilter(rule.Filter)
		}
		if rule.Expiration != nil {
			expiration, err := lifecycleExpiration(rule.Expiration)
			if err != nil {
				return nil, err
			}
			out.Expiration = expiration
		}
		if len(rule.Transitions) > 0 {
			transitions, err := lifecycleTransitions(rule.Transitions)
			if err != nil {
				return nil, err
			}
			out.Transitions = transitions
		}
		if rule.NoncurrentVersionExpiration != nil {
			out.NoncurrentVersionExpiration =
				lifecycleNoncurrentVersionExpiration(rule.NoncurrentVersionExpiration)
		}
		if len(rule.NoncurrentVersionTransitions) > 0 {
			out.NoncurrentVersionTransitions =
				lifecycleNoncurrentVersionTransitions(rule.NoncurrentVersionTransitions)
		}
		if rule.AbortIncompleteMultipartUpload != nil {
			out.AbortIncompleteMultipartUpload =
				lifecycleAbortIncompleteMultipartUpload(rule.AbortIncompleteMultipartUpload)
		}
		rules = append(rules, out)
	}
	return rules, nil
}

// lifecycleFilter expands a rule's filter, setting each predicate only when
// present. The size bounds are int64 in the SDK, so they pass straight through.
func lifecycleFilter(in *BucketLifecycleFilter) *s3types.LifecycleRuleFilter {
	out := &s3types.LifecycleRuleFilter{}
	if in.Prefix != nil {
		out.Prefix = in.Prefix
	}
	if in.Tag != nil {
		out.Tag = &s3types.Tag{
			Key:   aws.String(in.Tag.Key),
			Value: aws.String(in.Tag.Value),
		}
	}
	if in.ObjectSizeGreaterThan != nil {
		out.ObjectSizeGreaterThan = in.ObjectSizeGreaterThan
	}
	if in.ObjectSizeLessThan != nil {
		out.ObjectSizeLessThan = in.ObjectSizeLessThan
	}
	if in.And != nil {
		out.And = lifecycleFilterAnd(in.And)
	}
	return out
}

// lifecycleFilterAnd expands a filter's And operator, setting each predicate
// only when present and ordering the tags by key for a deterministic request.
func lifecycleFilterAnd(in *BucketLifecycleAnd) *s3types.LifecycleRuleAndOperator {
	out := &s3types.LifecycleRuleAndOperator{}
	if in.Prefix != nil {
		out.Prefix = in.Prefix
	}
	if len(in.Tags) > 0 {
		out.Tags = bucketTags(in.Tags)
	}
	if in.ObjectSizeGreaterThan != nil {
		out.ObjectSizeGreaterThan = in.ObjectSizeGreaterThan
	}
	if in.ObjectSizeLessThan != nil {
		out.ObjectSizeLessThan = in.ObjectSizeLessThan
	}
	return out
}

// lifecycleExpiration expands a rule's expiration, setting each field only when
// present. Date is parsed as RFC3339; Days narrows to the SDK int32 width.
func lifecycleExpiration(
	in *BucketLifecycleExpiration,
) (*s3types.LifecycleExpiration, error) {
	out := &s3types.LifecycleExpiration{}
	if in.Date != nil {
		t, err := time.Parse(time.RFC3339, *in.Date)
		if err != nil {
			return nil, fmt.Errorf("parse expiration date: %w", err)
		}
		out.Date = aws.Time(t)
	}
	if in.Days != nil {
		out.Days = ptr.Int32(in.Days)
	}
	if in.ExpiredObjectDeleteMarker != nil {
		out.ExpiredObjectDeleteMarker = in.ExpiredObjectDeleteMarker
	}
	return out, nil
}

// lifecycleTransitions expands a rule's transitions, setting each field only
// when present. Date is parsed as RFC3339; Days narrows to the SDK int32 width;
// StorageClass becomes the SDK enum.
func lifecycleTransitions(
	in []BucketLifecycleTransition,
) ([]s3types.Transition, error) {
	out := make([]s3types.Transition, 0, len(in))
	for _, transition := range in {
		t := s3types.Transition{
			StorageClass: s3types.TransitionStorageClass(transition.StorageClass),
		}
		if transition.Date != nil {
			parsed, err := time.Parse(time.RFC3339, *transition.Date)
			if err != nil {
				return nil, fmt.Errorf("parse transition date: %w", err)
			}
			t.Date = aws.Time(parsed)
		}
		if transition.Days != nil {
			t.Days = ptr.Int32(transition.Days)
		}
		out = append(out, t)
	}
	return out, nil
}

// lifecycleNoncurrentVersionExpiration expands a rule's noncurrent-version
// expiration, narrowing each count to the SDK int32 width when present.
func lifecycleNoncurrentVersionExpiration(
	in *BucketLifecycleNCExpiration,
) *s3types.NoncurrentVersionExpiration {
	out := &s3types.NoncurrentVersionExpiration{}
	if in.NoncurrentDays != nil {
		out.NoncurrentDays = ptr.Int32(in.NoncurrentDays)
	}
	if in.NewerNoncurrentVersions != nil {
		out.NewerNoncurrentVersions = ptr.Int32(in.NewerNoncurrentVersions)
	}
	return out
}

// lifecycleNoncurrentVersionTransitions expands a rule's noncurrent-version
// transitions, narrowing each count to the SDK int32 width when present and
// converting StorageClass to the SDK enum.
func lifecycleNoncurrentVersionTransitions(
	in []BucketLifecycleNCTransition,
) []s3types.NoncurrentVersionTransition {
	out := make([]s3types.NoncurrentVersionTransition, 0, len(in))
	for _, transition := range in {
		t := s3types.NoncurrentVersionTransition{
			StorageClass: s3types.TransitionStorageClass(transition.StorageClass),
		}
		if transition.NoncurrentDays != nil {
			t.NoncurrentDays = ptr.Int32(transition.NoncurrentDays)
		}
		if transition.NewerNoncurrentVersions != nil {
			t.NewerNoncurrentVersions = ptr.Int32(transition.NewerNoncurrentVersions)
		}
		out = append(out, t)
	}
	return out
}

// lifecycleAbortIncompleteMultipartUpload expands a rule's incomplete-multipart
// abort, narrowing the day count to the SDK int32 width when present.
func lifecycleAbortIncompleteMultipartUpload(
	in *BucketLifecycleAbortUpload,
) *s3types.AbortIncompleteMultipartUpload {
	out := &s3types.AbortIncompleteMultipartUpload{}
	if in.DaysAfterInitiation != nil {
		out.DaysAfterInitiation = ptr.Int32(in.DaysAfterInitiation)
	}
	return out
}
