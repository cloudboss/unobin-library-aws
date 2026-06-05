package s3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// bucketRegionUSEast1 is the one region whose name must not be sent as a
// LocationConstraint. S3 treats us-east-1 as the global default and rejects a
// CreateBucket that names it, so the create omits the constraint there and
// sets it everywhere else.
const bucketRegionUSEast1 = "us-east-1"

// bucketDeleteBatch is the most objects S3 deletes in one DeleteObjects call.
// A page of versions or delete markers is split into batches no larger than
// this before each bulk delete.
const bucketDeleteBatch = 1000

// Bucket manages an S3 bucket and its configuration as one resource, the way
// CloudFormation models AWS::S3::Bucket. The name and whether Object Lock is
// enabled are fixed at creation, so a change to either replaces the bucket;
// tags and every configuration block reconcile in place. Each block is its own
// S3 operation under the hood -- CreateBucket, then PutBucketVersioning,
// PutPublicAccessBlock, and the rest -- but they are declared together here. A
// nil block leaves that facet of the bucket untouched. EmptyOnDestroy is a
// delete-time switch, not a property of the live bucket.
type Bucket struct {
	Bucket            string                   `ub:"bucket"`
	ObjectLockEnabled *bool                    `ub:"object-lock-enabled"`
	Tags              map[string]string        `ub:"tags"`
	Versioning        *BucketVersioning        `ub:"versioning"`
	PublicAccessBlock *BucketPublicAccessBlock `ub:"public-access-block"`
	OwnershipControls *BucketOwnershipControls `ub:"ownership-controls"`
	Acl               *BucketAcl               `ub:"acl"`
	Accelerate        *BucketAccelerate        `ub:"accelerate"`
	Encryption        *BucketEncryption        `ub:"encryption"`
	Cors              *BucketCors              `ub:"cors"`
	Website           *BucketWebsite           `ub:"website"`
	Logging           *BucketLogging           `ub:"logging"`
	Lifecycle         *BucketLifecycle         `ub:"lifecycle"`
	ObjectLock        *BucketObjectLock        `ub:"object-lock"`
	EmptyOnDestroy    *bool                    `ub:"empty-on-destroy"`
}

// BucketOutput holds the values S3 computes for a bucket. Arn identifies the
// bucket in policies and grants. BucketRegion is the region the bucket lives
// in, reported by S3 rather than assumed. The two domain names are the
// addresses callers reach the bucket at, the regional form pinning the region
// into the host so a request is not redirected.
type BucketOutput struct {
	Arn                      string `ub:"arn"`
	BucketRegion             string `ub:"bucket-region"`
	BucketDomainName         string `ub:"bucket-domain-name"`
	BucketRegionalDomainName string `ub:"bucket-regional-domain-name"`
}

func (r *Bucket) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs S3 fixes when a bucket is created. The name is
// the bucket's identity and S3 offers no rename, and Object Lock can only be
// turned on at creation, so a change to either requires a new bucket. Tags are
// reconciled in place by Update. EmptyOnDestroy never reaches create, so it is
// not a replace trigger.
func (r *Bucket) ReplaceFields() []string {
	return []string{"bucket", "object-lock-enabled"}
}

// Defaults marks the collection inputs a bucket may omit.
func (r Bucket) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules S3 places on the bucket's configuration
// blocks: each block's enums and cross-field requirements, the per-rule rules
// of the cors, lifecycle, routing, and grant lists, and the object-lock block's
// dependence on object-lock-enabled. The cors method values and the lifecycle
// transition rules live where a constraint cannot reach (string-list elements
// and lists inside list elements), so the API validates those.
func (r Bucket) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.Accelerate.Status)).
			Require(constraint.OneOf(r.Accelerate.Status, "Enabled", "Suspended")).
			Message("accelerate status must be Enabled or Suspended"),
		constraint.When(constraint.Present(r.Versioning.Status)).
			Require(constraint.OneOf(r.Versioning.Status, "Enabled", "Suspended")).
			Message("versioning status must be Enabled or Suspended"),
		constraint.When(constraint.Present(r.Versioning.MfaDelete)).
			Require(constraint.OneOf(r.Versioning.MfaDelete, "Enabled", "Disabled")).
			Message("versioning mfa-delete must be Enabled or Disabled"),
		constraint.When(constraint.Present(r.Acl.Acl)).
			Require(constraint.OneOf(r.Acl.Acl, "private", "public-read",
				"public-read-write", "authenticated-read", "aws-exec-read",
				"bucket-owner-read", "bucket-owner-full-control",
				"log-delivery-write")).
			Message("acl must be one of the S3 canned bucket ACLs"),
		constraint.When(constraint.Present(r.OwnershipControls.ObjectOwnership)).
			Require(constraint.OneOf(r.OwnershipControls.ObjectOwnership,
				"BucketOwnerPreferred", "ObjectWriter", "BucketOwnerEnforced")).
			Message("object-ownership must be BucketOwnerPreferred, ObjectWriter, or BucketOwnerEnforced"),
		constraint.When(constraint.Present(r.Encryption.SSEAlgorithm)).
			Require(constraint.OneOf(r.Encryption.SSEAlgorithm,
				"AES256", "aws:kms", "aws:kms:dsse")).
			Message("sse-algorithm must be AES256, aws:kms, or aws:kms:dsse"),
		constraint.When(constraint.Present(r.Encryption.KMSMasterKeyID)).
			Require(constraint.OneOf(r.Encryption.SSEAlgorithm,
				"aws:kms", "aws:kms:dsse")).
			Message("kms-master-key-id requires a KMS sse-algorithm"),
		constraint.When(constraint.Present(r.ObjectLock)).
			Require(constraint.IsTrue(r.ObjectLockEnabled)).
			Message("object-lock requires object-lock-enabled to be true"),
		constraint.When(constraint.Present(r.ObjectLock.Rule.DefaultRetention.Mode)).
			Require(constraint.OneOf(r.ObjectLock.Rule.DefaultRetention.Mode,
				"GOVERNANCE", "COMPLIANCE")).
			Message("object-lock mode must be GOVERNANCE or COMPLIANCE"),
		constraint.AtMostOneOf(r.ObjectLock.Rule.DefaultRetention.Days,
			r.ObjectLock.Rule.DefaultRetention.Years),
		constraint.When(constraint.Present(r.ObjectLock)).
			Require(constraint.Any(
				constraint.Present(r.ObjectLock.Rule.DefaultRetention.Days),
				constraint.Present(r.ObjectLock.Rule.DefaultRetention.Years))).
			Message("object-lock retention requires days or years"),
		constraint.ForbiddenWith(r.Website.RedirectAllRequestsTo,
			r.Website.IndexDocument, r.Website.ErrorDocument, r.Website.RoutingRules),
		constraint.When(constraint.Present(r.Website)).
			Require(constraint.Any(constraint.Present(r.Website.IndexDocument),
				constraint.Present(r.Website.RedirectAllRequestsTo))).
			Message("website requires index-document or redirect-all-requests-to"),
		constraint.When(constraint.Present(r.Website.RedirectAllRequestsTo.Protocol)).
			Require(constraint.OneOf(r.Website.RedirectAllRequestsTo.Protocol,
				"http", "https")).
			Message("redirect-all-requests-to protocol must be http or https"),
		constraint.ForEach(r.Website.RoutingRules,
			func(rule BucketWebsiteRoutingRule) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.Present(rule.Redirect)).
						Message("a routing rule requires a redirect"),
					constraint.When(constraint.Present(rule.Redirect.Protocol)).
						Require(constraint.OneOf(rule.Redirect.Protocol, "http", "https")).
						Message("a routing rule redirect protocol must be http or https"),
					constraint.AtMostOneOf(rule.Redirect.ReplaceKeyPrefixWith,
						rule.Redirect.ReplaceKeyWith),
				}
			}),
		constraint.ForEach(r.Cors.Rules,
			func(rule BucketCorsRule) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.Present(rule.AllowedMethods),
						constraint.Present(rule.AllowedOrigins)).
						Message("a cors rule requires allowed-methods and allowed-origins"),
				}
			}),
		constraint.ForEach(r.Lifecycle.Rules,
			func(rule BucketLifecycleRule) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(rule.Status, "Enabled", "Disabled")).
						Message("a lifecycle rule status must be Enabled or Disabled"),
					constraint.Must(constraint.Any(
						constraint.Present(rule.Expiration),
						constraint.Present(rule.Transitions),
						constraint.Present(rule.NoncurrentVersionExpiration),
						constraint.Present(rule.NoncurrentVersionTransitions),
						constraint.Present(rule.AbortIncompleteMultipartUpload))).
						Message("a lifecycle rule needs at least one action"),
					constraint.AtMostOneOf(rule.Filter.Prefix, rule.Filter.Tag,
						rule.Filter.ObjectSizeGreaterThan,
						rule.Filter.ObjectSizeLessThan, rule.Filter.And),
					constraint.AtMostOneOf(rule.Expiration.Date, rule.Expiration.Days,
						rule.Expiration.ExpiredObjectDeleteMarker),
					constraint.When(constraint.Present(rule.Expiration)).
						Require(constraint.Any(constraint.Present(rule.Expiration.Date),
							constraint.Present(rule.Expiration.Days),
							constraint.Present(rule.Expiration.ExpiredObjectDeleteMarker))).
						Message("an expiration needs date, days, or expired-object-delete-marker"),
				}
			}),
		constraint.ForEach(r.Logging.TargetGrants,
			func(grant BucketLoggingTargetGrant) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(grant.Permission,
						"FULL_CONTROL", "READ", "WRITE")).
						Message("a target grant permission must be FULL_CONTROL, READ, or WRITE"),
					constraint.When(constraint.Present(grant.Grantee.Type)).
						Require(constraint.OneOf(grant.Grantee.Type,
							"CanonicalUser", "AmazonCustomerByEmail", "Group")).
						Message("a grantee type must be CanonicalUser, AmazonCustomerByEmail, or Group"),
				}
			}),
		constraint.AtMostOneOf(r.Logging.TargetObjectKeyFormat.PartitionedPrefix,
			r.Logging.TargetObjectKeyFormat.SimplePrefix),
		constraint.When(constraint.Present(r.Logging.TargetObjectKeyFormat)).
			Require(constraint.Any(
				constraint.Present(r.Logging.TargetObjectKeyFormat.PartitionedPrefix),
				constraint.Present(r.Logging.TargetObjectKeyFormat.SimplePrefix))).
			Message("target-object-key-format requires partitioned-prefix or simple-prefix"),
		constraint.When(constraint.Present(
			r.Logging.TargetObjectKeyFormat.PartitionedPrefix.PartitionDateSource)).
			Require(constraint.OneOf(
				r.Logging.TargetObjectKeyFormat.PartitionedPrefix.PartitionDateSource,
				"EventTime", "DeliveryTime")).
			Message("partition-date-source must be EventTime or DeliveryTime"),
	}
}

func (r *Bucket) Create(ctx context.Context, cfg any) (*BucketOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	region := client.Options().Region
	// In us-east-1 a CreateBucket against a name already owned by the caller
	// silently succeeds and resets the bucket's ACL, so an existing owned bucket
	// would be clobbered rather than reported. Other regions return an
	// already-exists error from create itself, so the pre-check is only needed
	// here.
	if region == bucketRegionUSEast1 {
		gone, err := bucketHeadGone(ctx, client, r.Bucket)
		if err != nil {
			return nil, err
		}
		if !gone {
			return nil, fmt.Errorf("create bucket %s: bucket already exists", r.Bucket)
		}
	}
	tagged, err := r.create(ctx, client, region)
	if err != nil {
		return nil, err
	}
	// S3 is eventually consistent after a create: a HeadBucket can briefly report
	// the just-made bucket as absent, and a later plan that read it absent would
	// take it for deleted and recreate it. Wait for it to become visible before
	// computing outputs.
	if err := r.waitExists(ctx, client); err != nil {
		return nil, err
	}
	// When the endpoint refused tag-on-create the create stripped the tags, so
	// they are written now that the bucket is visible.
	if len(r.Tags) > 0 && !tagged {
		if err := r.putTags(ctx, client); err != nil {
			return nil, err
		}
	}
	// The bucket exists; write each configuration block that was declared. A nil
	// prior means every block is new.
	if err := r.reconcile(ctx, client, nil); err != nil {
		return nil, err
	}
	return r.read(ctx, client, region)
}

// reconcile writes each configuration block whose input differs from prior, in
// a fixed order. On create prior is nil and every declared block is written. On
// update a block is written when it changed and cleared when it was removed.
// The blocks run one at a time, so S3 does not serialize them against each
// other the way it does when separate resources race a shared bucket. Ownership
// controls come before the ACL: enforcing bucket-owner ownership disables ACLs,
// so the ACL write must see the ownership setting already in place.
func (r *Bucket) reconcile(ctx context.Context, client *s3.Client, prior *Bucket) error {
	// A nil prior is a create: substitute a zero bucket so every prior block reads
	// as absent, and each reconcile sees its block as new.
	if prior == nil {
		prior = &Bucket{}
	}
	steps := []func() error{
		func() error {
			return reconcileVersioning(ctx, client, r.Bucket, r.Versioning, prior.Versioning)
		},
		func() error {
			return reconcilePublicAccessBlock(
				ctx, client, r.Bucket, r.PublicAccessBlock, prior.PublicAccessBlock)
		},
		func() error {
			return reconcileOwnershipControls(
				ctx, client, r.Bucket, r.OwnershipControls, prior.OwnershipControls)
		},
		func() error {
			return reconcileAcl(ctx, client, r.Bucket, r.Acl, prior.Acl)
		},
		func() error {
			return reconcileAccelerate(ctx, client, r.Bucket, r.Accelerate, prior.Accelerate)
		},
		func() error {
			return reconcileEncryption(ctx, client, r.Bucket, r.Encryption, prior.Encryption)
		},
		func() error {
			return reconcileCors(ctx, client, r.Bucket, r.Cors, prior.Cors)
		},
		func() error {
			return reconcileWebsite(ctx, client, r.Bucket, r.Website, prior.Website)
		},
		func() error {
			return reconcileLogging(ctx, client, r.Bucket, r.Logging, prior.Logging)
		},
		func() error {
			return reconcileLifecycle(ctx, client, r.Bucket, r.Lifecycle, prior.Lifecycle)
		},
		func() error {
			return reconcileObjectLock(ctx, client, r.Bucket, r.ObjectLock, prior.ObjectLock)
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// bucketConfigPut runs one configuration write, retrying the OperationAborted S3
// returns while another operation against the bucket is in progress. The blocks
// reconcile one at a time, so this rarely fires; it polls every second, since
// the conflict clears in well under a second.
func bucketConfigPut(ctx context.Context, what string, put func(context.Context) error) error {
	if err := retry.OnError(ctx, isOperationAborted, put,
		retry.WithInterval(time.Second)); err != nil {
		return fmt.Errorf("configure bucket %s: %w", what, err)
	}
	return nil
}

// bucketConfigDelete clears one configuration, retrying OperationAborted and
// treating the codes that mean the configuration is already absent as success.
func bucketConfigDelete(
	ctx context.Context, what string, goneCodes []string, del func(context.Context) error,
) error {
	err := retry.OnError(ctx, isOperationAborted, del, retry.WithInterval(time.Second))
	if err != nil && !isNotFound(err, goneCodes...) {
		return fmt.Errorf("remove bucket %s: %w", what, err)
	}
	return nil
}

// create issues CreateBucket and reconciles the create-time differences across
// endpoints, reporting whether the bucket's tags were applied by the create.
// The location constraint is omitted in us-east-1 and set elsewhere. Tags ride
// the create where the endpoint allows it; an endpoint that rejects
// tag-on-create, or rejects the whole configuration body, has the offending
// part stripped and the create retried, leaving the tags to be applied
// separately. Every attempt is retried through the transient OperationAborted a
// freshly deleted or in-flight name returns.
func (r *Bucket) create(ctx context.Context, client *s3.Client, region string) (bool, error) {
	tagged := len(r.Tags) > 0
	withTags := r.createInput(region, true, true)
	err := bucketCreateRetry(ctx, client, withTags)
	if err == nil {
		return tagged, nil
	}
	// An endpoint that cannot tag on create reports it as a not-authorized
	// s3:TagResource, an unsupported argument, or malformed XML. Retry without
	// the tags; they are applied after the existence wait.
	if bucketTagOnCreateRejected(err) {
		noTags := r.createInput(region, false, true)
		err = bucketCreateRetry(ctx, client, noTags)
		if err == nil {
			return false, nil
		}
	}
	// A third-party or ISO endpoint may reject the configuration body entirely,
	// still as malformed XML. Drop the whole configuration and retry.
	if bucketConfigRejected(err) {
		bare := r.createInput(region, false, false)
		if err := bucketCreateRetry(ctx, client, bare); err != nil {
			return false, fmt.Errorf("create bucket %s: %w", r.Bucket, err)
		}
		return false, nil
	}
	return false, fmt.Errorf("create bucket %s: %w", r.Bucket, err)
}

// createInput builds a CreateBucket request for the bucket. withTags includes
// the desired tags in the configuration; withConfig includes the configuration
// body at all. The location constraint is set only outside us-east-1. Object
// Lock is requested when the input asks for it.
func (r *Bucket) createInput(region string, withTags, withConfig bool) *s3.CreateBucketInput {
	in := &s3.CreateBucketInput{
		Bucket:                     aws.String(r.Bucket),
		ObjectLockEnabledForBucket: r.ObjectLockEnabled,
	}
	if !withConfig {
		return in
	}
	cfg := &s3types.CreateBucketConfiguration{}
	set := false
	if region != bucketRegionUSEast1 {
		cfg.LocationConstraint = s3types.BucketLocationConstraint(region)
		set = true
	}
	if withTags && len(r.Tags) > 0 {
		cfg.Tags = bucketTags(r.Tags)
		set = true
	}
	if set {
		in.CreateBucketConfiguration = cfg
	}
	return in
}

func (r *Bucket) Read(
	ctx context.Context, cfg any, prior *BucketOutput,
) (*BucketOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, client.Options().Region)
}

// read heads the bucket and computes its outputs. A gone bucket maps to
// runtime.ErrNotFound so a plan recreates it. The region S3 reports for the
// bucket is preferred over the client's region for the outputs, since a bucket
// may live in a different region than the one the call was made from. The
// bucket name is the read key and comes from the receiver, since it is an input
// the prior output does not carry.
func (r *Bucket) read(
	ctx context.Context, client *s3.Client, region string,
) (*BucketOutput, error) {
	resp, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(r.Bucket)})
	if err != nil {
		if bucketIsGone(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("head bucket %s: %w", r.Bucket, err)
	}
	bucketRegion := aws.ToString(resp.BucketRegion)
	if bucketRegion == "" {
		bucketRegion = region
	}
	domain, regional, err := bucketDomainNames(ctx, r.Bucket, bucketRegion)
	if err != nil {
		return nil, err
	}
	return &BucketOutput{
		Arn:                      bucketARN(region, r.Bucket),
		BucketRegion:             bucketRegion,
		BucketDomainName:         domain,
		BucketRegionalDomainName: regional,
	}, nil
}

func (r *Bucket) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Bucket, *BucketOutput],
) (*BucketOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The bucket name and Object Lock are replace-only and EmptyOnDestroy only
	// affects delete. Tags and the configuration blocks reconcile in place.
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client); err != nil {
			return nil, err
		}
	}
	if err := r.reconcile(ctx, client, &prior.Inputs); err != nil {
		return nil, err
	}
	return r.read(ctx, client, client.Options().Region)
}

func (r *Bucket) Delete(ctx context.Context, cfg any, prior *BucketOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	force := aws.ToBool(r.EmptyOnDestroy)
	// A non-empty bucket cannot be deleted. With EmptyOnDestroy the bucket's
	// contents are removed first; without it, the BucketNotEmpty from a populated
	// bucket is left to propagate so the operator sees why the delete failed.
	if force {
		if err := bucketEmptyAll(ctx, client, r.Bucket); err != nil {
			return err
		}
	}
	err = bucketDeleteCall(ctx, client, r.Bucket)
	if err != nil {
		// A bucket already gone counts as deleted.
		if bucketIsGone(err) {
			return nil
		}
		// A race may have repopulated the bucket between the empty and the delete;
		// empty once more and delete again before giving up.
		if force && isNotFound(err, "BucketNotEmpty") {
			if err := bucketEmptyAll(ctx, client, r.Bucket); err != nil {
				return err
			}
			if err := bucketDeleteCall(ctx, client, r.Bucket); err != nil {
				if bucketIsGone(err) {
					return nil
				}
				return fmt.Errorf("delete bucket %s: %w", r.Bucket, err)
			}
		} else {
			return fmt.Errorf("delete bucket %s: %w", r.Bucket, err)
		}
	}
	// S3 delete is eventually consistent: a HeadBucket can still find the bucket
	// for a moment after DeleteBucket returns, and a plan that read it back as
	// present would re-attempt the delete. Wait until it reports gone.
	return r.waitGone(ctx, client)
}

// waitExists polls HeadBucket until the bucket is visible, for the propagation
// window after a create.
func (r *Bucket) waitExists(ctx context.Context, client *s3.Client) error {
	return wait.Until(ctx, fmt.Sprintf("bucket %s", r.Bucket),
		func(ctx context.Context) (bool, error) {
			gone, err := bucketHeadGone(ctx, client, r.Bucket)
			if err != nil {
				return false, err
			}
			return !gone, nil
		},
	)
}

// waitGone polls HeadBucket until the bucket reports gone after a delete. It
// polls every second, since a deleted bucket disappears quickly, unlike the
// slower wait for a create to become visible.
func (r *Bucket) waitGone(ctx context.Context, client *s3.Client) error {
	return wait.Until(ctx, fmt.Sprintf("bucket %s to be gone", r.Bucket),
		func(ctx context.Context) (bool, error) {
			return bucketHeadGone(ctx, client, r.Bucket)
		},
		wait.WithInterval(time.Second),
	)
}

// putTags writes the desired tags onto the bucket, replacing whatever tag set
// is there. It is the separate-tagging path taken after a create that could
// not tag the bucket inline.
func (r *Bucket) putTags(ctx context.Context, client *s3.Client) error {
	_, err := client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket:  aws.String(r.Bucket),
		Tagging: &s3types.Tagging{TagSet: bucketTags(r.Tags)},
	})
	if err != nil {
		return fmt.Errorf("put bucket tagging %s: %w", r.Bucket, err)
	}
	return nil
}

// syncTags reconciles the bucket's tags with the desired set. S3 has no
// per-tag write, only a whole-set replace with PutBucketTagging and a
// whole-set clear with DeleteBucketTagging, so the live set is read and
// compared and the call is skipped when nothing differs. The shared diff
// decides equality and keeps the reserved aws: tags out of the comparison; its
// result is only consulted for whether a change is needed, since S3 cannot act
// on the per-key sets it returns. GetBucketTagging returns NoSuchTagSet on a
// bucket that has never been tagged, taken here as an empty set.
func (r *Bucket) syncTags(ctx context.Context, client *s3.Client) error {
	current, err := r.readTags(ctx, client)
	if err != nil {
		return err
	}
	upsert, remove := tagsync.Diff(current, r.Tags)
	if len(upsert) == 0 && len(remove) == 0 {
		return nil
	}
	if len(r.Tags) == 0 {
		_, err := client.DeleteBucketTagging(ctx, &s3.DeleteBucketTaggingInput{
			Bucket: aws.String(r.Bucket),
		})
		if err != nil {
			return fmt.Errorf("delete bucket tagging %s: %w", r.Bucket, err)
		}
		return nil
	}
	return r.putTags(ctx, client)
}

// readTags returns the bucket's live tags as a map. A bucket that has never
// been tagged returns the NoSuchTagSet code, taken as an empty set.
func (r *Bucket) readTags(ctx context.Context, client *s3.Client) (map[string]string, error) {
	resp, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		Bucket: aws.String(r.Bucket),
	})
	if err != nil {
		if isNotFound(err, "NoSuchTagSet") {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("get bucket tagging %s: %w", r.Bucket, err)
	}
	current := make(map[string]string, len(resp.TagSet))
	for _, t := range resp.TagSet {
		current[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return current, nil
}

// bucketCreateRetry issues CreateBucket, retrying through OperationAborted. S3
// returns that code transiently when a bucket of the same name was just
// deleted or another operation against the name is in flight; it clears once
// the conflicting operation finishes. Draining a freshly deleted bucket of the
// same name can run past the default two-minute window, so the budget is
// widened to give the conflict time to clear.
func bucketCreateRetry(ctx context.Context, client *s3.Client, in *s3.CreateBucketInput) error {
	return retry.OnError(ctx, isOperationAborted, func(ctx context.Context) error {
		_, err := client.CreateBucket(ctx, in)
		return err
	}, retry.WithTimeout(5*time.Minute), retry.WithInterval(time.Second))
}

// bucketDeleteCall issues DeleteBucket, retrying the transient OperationAborted
// S3 returns when another operation against the bucket is still in flight, which
// happens because the bucket's other configurations are torn down at the same
// time. The caller classifies the returned error: a gone bucket is success, a
// non-empty one may be re-emptied and retried.
func bucketDeleteCall(ctx context.Context, client *s3.Client, bucket string) error {
	return retry.OnError(ctx, isOperationAborted, func(ctx context.Context) error {
		_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
		return err
	}, retry.WithInterval(time.Second))
}

// bucketEmptyAll removes everything in the bucket so it can be deleted: every
// object version, then every delete marker. Both are paginated through
// ListObjectVersions and removed in bulk a batch at a time. A bucket or key
// that vanishes mid-empty, from a concurrent delete or because the bucket is
// already gone, is treated as already removed. An object the bulk delete reports
// as AccessDenied is held by a legal hold or governance retention; it is
// released and retried so an Object Lock bucket can still be emptied.
func bucketEmptyAll(ctx context.Context, client *s3.Client, bucket string) error {
	// The listing is URL-encoded so a key holding any byte, including ones the
	// response XML could not otherwise carry, comes back intact and is decoded
	// before use.
	pager := s3.NewListObjectVersionsPaginator(client, &s3.ListObjectVersionsInput{
		Bucket:       aws.String(bucket),
		EncodingType: s3types.EncodingTypeUrl,
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if bucketIsGone(err) {
				return nil
			}
			return fmt.Errorf("list object versions %s: %w", bucket, err)
		}
		var bulk []s3types.ObjectIdentifier
		add := func(rawKey, versionID *string) error {
			key, err := url.QueryUnescape(aws.ToString(rawKey))
			if err != nil {
				return fmt.Errorf("decode object key in %s: %w", bucket, err)
			}
			// A key holding a character the bulk DeleteObjects request body cannot
			// represent would make the whole batch malformed, so it is removed one
			// call at a time; the rest go in the batch.
			if !bucketKeyInXMLRange(key) {
				return bucketDeleteOne(ctx, client, bucket, key, versionID)
			}
			bulk = append(bulk, s3types.ObjectIdentifier{Key: aws.String(key), VersionId: versionID})
			return nil
		}
		for _, v := range page.Versions {
			if err := add(v.Key, v.VersionId); err != nil {
				return err
			}
		}
		for _, m := range page.DeleteMarkers {
			if err := add(m.Key, m.VersionId); err != nil {
				return err
			}
		}
		if err := bucketDeleteObjects(ctx, client, bucket, bulk); err != nil {
			return err
		}
	}
	return nil
}

// bucketDeleteOne removes a single object version, the path for a key holding a
// character the bulk DeleteObjects body cannot represent. A bucket or key gone
// underneath counts as removed.
func bucketDeleteOne(
	ctx context.Context, client *s3.Client, bucket, key string, versionID *string,
) error {
	_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: versionID,
	})
	if err != nil && !bucketIsGone(err) {
		return fmt.Errorf("delete object %s/%s: %w", bucket, key, err)
	}
	return nil
}

// bucketKeyInXMLRange reports whether every character in key is one the XML the
// bulk DeleteObjects request is built from can represent. S3 allows keys to
// hold bytes XML cannot, such as the low control characters; such a key is
// removed with a single DeleteObject rather than in a bulk batch.
func bucketKeyInXMLRange(key string) bool {
	for _, r := range key {
		switch {
		case r == 0x09 || r == 0x0A || r == 0x0D:
		case r >= 0x20 && r <= 0xD7FF:
		case r >= 0xE000 && r <= 0xFFFD:
		case r >= 0x10000 && r <= 0x10FFFF:
		default:
			return false
		}
	}
	return true
}

// bucketDeleteObjects removes the given object versions in bulk, in batches no
// larger than the S3 per-request limit. Each batch runs in quiet mode so only
// failures come back. The bulk delete carries no governance bypass, which S3
// rejects on a bucket without Object Lock; a version reported AccessDenied is
// held by a legal hold or governance retention and is released and deleted again
// singly. A bucket or key gone underneath is success.
func bucketDeleteObjects(
	ctx context.Context, client *s3.Client, bucket string, ids []s3types.ObjectIdentifier,
) error {
	for start := 0; start < len(ids); start += bucketDeleteBatch {
		end := min(start+bucketDeleteBatch, len(ids))
		batch := ids[start:end]
		resp, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{Objects: batch, Quiet: aws.Bool(true)},
		})
		if err != nil {
			if bucketIsGone(err) {
				return nil
			}
			return fmt.Errorf("delete objects %s: %w", bucket, err)
		}
		if err := bucketReleaseAndRetry(ctx, client, bucket, resp.Errors); err != nil {
			return err
		}
	}
	return nil
}

// bucketReleaseAndRetry handles the per-object failures a bulk delete reports.
// An AccessDenied means the object is held by a legal hold or governance
// retention, so its hold is turned off and it is deleted singly with the
// governance bypass set, valid because only an Object Lock bucket reports it.
// Any other per-object error is surfaced.
func bucketReleaseAndRetry(
	ctx context.Context, client *s3.Client, bucket string, failures []s3types.Error,
) error {
	for i := range failures {
		f := failures[i]
		if aws.ToString(f.Code) != "AccessDenied" {
			return fmt.Errorf("delete object %s/%s: %s",
				bucket, aws.ToString(f.Key), aws.ToString(f.Message))
		}
		_, err := client.PutObjectLegalHold(ctx, &s3.PutObjectLegalHoldInput{
			Bucket:    aws.String(bucket),
			Key:       f.Key,
			VersionId: f.VersionId,
			LegalHold: &s3types.ObjectLockLegalHold{
				Status: s3types.ObjectLockLegalHoldStatusOff,
			},
		})
		if err != nil {
			if bucketIsGone(err) {
				continue
			}
			return fmt.Errorf("release legal hold %s/%s: %w",
				bucket, aws.ToString(f.Key), err)
		}
		_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket:                    aws.String(bucket),
			Key:                       f.Key,
			VersionId:                 f.VersionId,
			BypassGovernanceRetention: aws.Bool(true),
		})
		if err != nil && !bucketIsGone(err) {
			return fmt.Errorf("delete object %s/%s: %w", bucket, aws.ToString(f.Key), err)
		}
	}
	return nil
}

// bucketHeadGone reports whether a HeadBucket finds the bucket missing. A
// not-found is a clean false-with-nil; any other error is returned.
func bucketHeadGone(ctx context.Context, client *s3.Client, bucket string) (bool, error) {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	if err != nil {
		if bucketIsGone(err) {
			return true, nil
		}
		return false, fmt.Errorf("head bucket %s: %w", bucket, err)
	}
	return false, nil
}

// bucketIsGone reports whether err means the bucket no longer exists. S3
// signals a missing bucket inconsistently: HeadBucket on a general-purpose
// bucket returns a bare HTTP 404 with no service code, while other calls
// return a NoSuchBucket code or a NotFound code. All three are treated as gone,
// so the HTTP status is checked alongside the codes rather than the codes
// alone.
func bucketIsGone(err error) bool {
	if isNotFound(err, "NoSuchBucket", "NotFound", "NoSuchKey") {
		return true
	}
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotFound {
		return true
	}
	return false
}

// bucketTagOnCreateRejected reports whether err is an endpoint refusing to tag
// a bucket at create time: a not-authorized s3:TagResource, an unsupported
// argument, or malformed XML. The caller retries the create without tags and
// tags the bucket separately afterward.
func bucketTagOnCreateRejected(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "UnsupportedArgument", "MalformedXML":
		return true
	}
	return strings.Contains(apiErr.ErrorMessage(), "is not authorized to perform: s3:TagResource")
}

// bucketConfigRejected reports whether err is an endpoint refusing the whole
// CreateBucketConfiguration body, which it returns as malformed XML. The caller
// retries the create with no configuration at all.
func bucketConfigRejected(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorCode() == "MalformedXML"
}

// bucketARN builds the ARN of a bucket. An S3 bucket ARN names neither account
// nor region, only the partition and the bucket name, since a bucket name is
// globally unique.
func bucketARN(region, bucket string) string {
	return fmt.Sprintf("arn:%s:s3:::%s", partition.Of(region), bucket)
}

// bucketDomainNames builds a bucket's global and regional domain names. The
// partition's DNS suffix comes from the S3 endpoint resolver, which carries
// AWS's own partition data and tracks new partitions as the SDK is updated, so
// the library keeps no suffix table of its own. The resolver routes a request
// for the actual bucket, which it sends path-style for a name it cannot put in
// the host (one with dots), so the suffix is read from the bucket-less service
// endpoint, whose host is always s3.<region>.<suffix>, and the names are then
// built as strings, the form every bucket takes.
func bucketDomainNames(
	ctx context.Context, bucket, region string,
) (domain, regional string, err error) {
	ep, err := s3.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, s3.EndpointParameters{
		Region: aws.String(region),
	})
	if err != nil {
		return "", "", fmt.Errorf("resolve s3 endpoint for %s: %w", region, err)
	}
	suffix, ok := strings.CutPrefix(ep.URI.Host, "s3."+region+".")
	if !ok {
		return "", "", fmt.Errorf("unexpected s3 endpoint host %q for region %s", ep.URI.Host, region)
	}
	domain = bucket + ".s3." + suffix
	regional = bucket + ".s3." + region + "." + suffix
	return domain, regional, nil
}

// bucketTags converts a desired tag map into the S3 SDK tag list, ordered by
// key so the request is deterministic.
func bucketTags(tags map[string]string) []s3types.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]s3types.Tag, 0, len(tags))
	for _, k := range keys {
		out = append(out, s3types.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return out
}
