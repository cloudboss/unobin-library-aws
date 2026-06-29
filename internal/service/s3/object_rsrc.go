package s3

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// Object manages a single object in a bucket: the bytes plus the metadata,
// encryption, storage class, ACL, and object-lock settings that PutObject
// records. Create and Update both PutObject; Read is a HeadObject; the bucket
// and key form the identity, so a change to either replaces the object while
// every other field updates in place by re-putting. The body comes from at
// most one of an inline string, a file path, or base64-encoded bytes; with
// none set the object is empty.
type Object struct {
	Bucket                    string             `ub:"bucket"`
	Key                       string             `ub:"key"`
	BodyContent               *string            `ub:"body-content"`
	BodyPath                  *string            `ub:"body-path"`
	BodyBase64                *string            `ub:"body-base64"`
	ACL                       *string            `ub:"acl"`
	BucketKeyEnabled          *bool              `ub:"bucket-key-enabled"`
	CacheControl              *string            `ub:"cache-control"`
	ChecksumAlgorithm         *string            `ub:"checksum-algorithm"`
	ContentDisposition        *string            `ub:"content-disposition"`
	ContentEncoding           *string            `ub:"content-encoding"`
	ContentLanguage           *string            `ub:"content-language"`
	ContentType               *string            `ub:"content-type"`
	KmsKeyId                  *string            `ub:"kms-key-id"`
	Metadata                  *map[string]string `ub:"metadata"`
	ServerSideEncryption      *string            `ub:"server-side-encryption"`
	StorageClass              *string            `ub:"storage-class"`
	WebsiteRedirect           *string            `ub:"website-redirect"`
	ObjectLockMode            *string            `ub:"object-lock-mode"`
	ObjectLockRetainUntilDate *string            `ub:"object-lock-retain-until-date"`
	ObjectLockLegalHoldStatus *string            `ub:"object-lock-legal-hold-status"`
	Tags                      *map[string]string `ub:"tags"`
	// PurgeOnDestroy controls how Delete removes the object. False, the default,
	// issues a single DeleteObject, which on a versioned bucket leaves a delete
	// marker. True purges every version and delete marker of this key. It is a
	// delete-time setting only: it rides no put and is not reconciled.
	PurgeOnDestroy *bool `ub:"purge-on-destroy"`
}

// ObjectOutput holds the values S3 computes for an object. The ARN is built
// locally from the partition, bucket, and cleaned key, since HeadObject does
// not return it; the cleaned key is the object's real S3 key, which the input
// key normalizes to. The etag, version id, and checksums identify the stored
// bytes, and the remaining fields are the server-filled defaults a consumer
// reads back when the corresponding input was left to S3.
type ObjectOutput struct {
	Arn                  string `ub:"arn"`
	Key                  string `ub:"key"`
	Etag                 string `ub:"etag"`
	VersionId            string `ub:"version-id"`
	ContentType          string `ub:"content-type"`
	StorageClass         string `ub:"storage-class"`
	ServerSideEncryption string `ub:"server-side-encryption"`
	BucketKeyEnabled     bool   `ub:"bucket-key-enabled"`
	KmsKeyId             string `ub:"kms-key-id"`
	ChecksumCrc32        string `ub:"checksum-crc32"`
	ChecksumCrc32c       string `ub:"checksum-crc32c"`
	ChecksumCrc64nvme    string `ub:"checksum-crc64nvme"`
	ChecksumSha1         string `ub:"checksum-sha1"`
	ChecksumSha256       string `ub:"checksum-sha256"`
}

func (r *Object) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that fix the object's identity. The bucket and
// key name the object, so a change to either is a different object and forces a
// new one. Every other field, the KMS key included, updates in place by
// re-putting.
func (r *Object) ReplaceFields() []string {
	return []string{"bucket", "key"}
}

// Constraints declares the rules S3 places on an object's inputs. The body has
// at most one source. The ACL, checksum algorithm, encryption, storage class,
// and the two object-lock enums each accept a fixed set of values when set.
func (r Object) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.BodyContent, r.BodyPath, r.BodyBase64),
		constraint.When(constraint.Present(r.ACL)).
			Require(constraint.OneOf(r.ACL,
				"private", "public-read", "public-read-write", "authenticated-read",
				"aws-exec-read", "bucket-owner-read", "bucket-owner-full-control")).
			Message("acl must be a valid S3 canned ACL"),
		constraint.When(constraint.Present(r.ChecksumAlgorithm)).
			Require(constraint.OneOf(r.ChecksumAlgorithm,
				"CRC32", "CRC32C", "SHA1", "SHA256", "CRC64NVME",
				"SHA512", "MD5", "XXHASH64", "XXHASH3", "XXHASH128")).
			Message("checksum-algorithm must be a valid S3 checksum algorithm"),
		constraint.When(constraint.Present(r.ServerSideEncryption)).
			Require(constraint.OneOf(r.ServerSideEncryption,
				"AES256", "aws:fsx", "aws:kms", "aws:kms:dsse")).
			Message("server-side-encryption must be a valid S3 encryption value"),
		constraint.When(constraint.Present(r.StorageClass)).
			Require(constraint.OneOf(r.StorageClass,
				"STANDARD", "REDUCED_REDUNDANCY", "GLACIER", "STANDARD_IA", "ONEZONE_IA",
				"INTELLIGENT_TIERING", "DEEP_ARCHIVE", "OUTPOSTS", "GLACIER_IR", "SNOW",
				"EXPRESS_ONEZONE", "FSX_OPENZFS", "FSX_ONTAP")).
			Message("storage-class must be a valid S3 storage class"),
		constraint.When(constraint.Present(r.ObjectLockMode)).
			Require(constraint.OneOf(r.ObjectLockMode, "GOVERNANCE", "COMPLIANCE")).
			Message("object-lock-mode must be GOVERNANCE or COMPLIANCE"),
		constraint.When(constraint.Present(r.ObjectLockLegalHoldStatus)).
			Require(constraint.OneOf(r.ObjectLockLegalHoldStatus, "ON", "OFF")).
			Message("object-lock-legal-hold-status must be ON or OFF"),
	}
}

func (r *Object) Create(ctx context.Context, cfg *awsCfg) (*ObjectOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client); err != nil {
		return nil, err
	}
	// S3 is read-after-write consistent for a new object, so the head that
	// follows is a single call with no wait; routing through it fills the
	// computed outputs the put response does not carry.
	return r.read(ctx, client)
}

func (r *Object) Read(
	ctx context.Context, cfg *awsCfg, prior *ObjectOutput,
) (*ObjectOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

func (r *Object) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Object, *ObjectOutput],
) (*ObjectOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Every put field, including the body, the ACL, and the object-lock
	// settings, is reconciled by re-putting the object. PurgeOnDestroy is a
	// delete-time setting and does not figure here.
	if r.contentChanged(prior.Inputs) {
		if err := r.put(ctx, client); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

func (r *Object) Delete(ctx context.Context, cfg *awsCfg, prior *ObjectOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	key := objectCleanKey(r.Key)
	if aws.ToBool(r.PurgeOnDestroy) {
		return objectPurgeVersions(ctx, client, r.Bucket, key)
	}
	// A plain delete adds a delete marker on a versioned bucket and removes the
	// object on an unversioned one. A bucket or key already gone counts as done.
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r.Bucket),
		Key:    aws.String(key),
	})
	if err != nil && !isNotFound(err, "NoSuchBucket", "NoSuchKey") {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// put builds and sends the PutObject for the object's current inputs. The body
// is read from whichever single source is set; the key is cleaned to its real
// S3 form; a KMS key implies aws:kms encryption; and any object-lock setting
// without an explicit checksum forces CRC32, which S3 requires for a lock put.
func (r *Object) put(ctx context.Context, client *s3.Client) error {
	body, err := r.body()
	if err != nil {
		return err
	}
	in := &s3.PutObjectInput{
		Bucket:                  aws.String(r.Bucket),
		Key:                     aws.String(objectCleanKey(r.Key)),
		Body:                    body,
		BucketKeyEnabled:        r.BucketKeyEnabled,
		CacheControl:            r.CacheControl,
		ContentDisposition:      r.ContentDisposition,
		ContentEncoding:         r.ContentEncoding,
		ContentLanguage:         r.ContentLanguage,
		ContentType:             r.ContentType,
		Metadata:                ptr.Value(r.Metadata),
		WebsiteRedirectLocation: r.WebsiteRedirect,
		Tagging:                 objectTagging(ptr.Value(r.Tags)),
	}
	if r.ACL != nil {
		in.ACL = s3types.ObjectCannedACL(*r.ACL)
	}
	if r.ServerSideEncryption != nil {
		in.ServerSideEncryption = s3types.ServerSideEncryption(*r.ServerSideEncryption)
	}
	if r.StorageClass != nil {
		in.StorageClass = s3types.StorageClass(*r.StorageClass)
	}
	if r.ObjectLockMode != nil {
		in.ObjectLockMode = s3types.ObjectLockMode(*r.ObjectLockMode)
	}
	if r.ObjectLockLegalHoldStatus != nil {
		in.ObjectLockLegalHoldStatus = s3types.ObjectLockLegalHoldStatus(*r.ObjectLockLegalHoldStatus)
	}
	if r.ObjectLockRetainUntilDate != nil {
		t, err := time.Parse(time.RFC3339, *r.ObjectLockRetainUntilDate)
		if err != nil {
			return fmt.Errorf("parse object-lock-retain-until-date: %w", err)
		}
		in.ObjectLockRetainUntilDate = aws.Time(t)
	}
	// A KMS key names the key to encrypt with and so means KMS encryption; S3
	// wants both the key id and the aws:kms scheme on the put.
	if r.KmsKeyId != nil {
		in.SSEKMSKeyId = r.KmsKeyId
		in.ServerSideEncryption = s3types.ServerSideEncryptionAwsKms
	}
	if r.ChecksumAlgorithm != nil {
		in.ChecksumAlgorithm = s3types.ChecksumAlgorithm(*r.ChecksumAlgorithm)
	} else if r.objectLockRequested() {
		// An object-lock put needs an integrity header; S3 rejects it otherwise.
		// CRC32 is the cheapest algorithm that satisfies the requirement.
		in.ChecksumAlgorithm = s3types.ChecksumAlgorithmCrc32
	}
	if _, err := client.PutObject(ctx, in); err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// read fetches the object with HeadObject and returns its computed outputs. A
// head of a missing object is a bare 404 with code NotFound, which maps to
// runtime.ErrNotFound so a plan recreates the object. The head opts into the
// checksum mode so a stored checksum comes back in the same call; an object
// without one simply leaves the checksum outputs empty.
func (r *Object) read(ctx context.Context, client *s3.Client) (*ObjectOutput, error) {
	bucket := r.Bucket
	key := objectCleanKey(r.Key)
	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(key),
		ChecksumMode: s3types.ChecksumModeEnabled,
	})
	if err != nil {
		if isNotFound(err, "NotFound", "NoSuchKey") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("head object: %w", err)
	}
	region := client.Options().Region
	// S3 suppresses the default storage class on the head, so an empty value
	// means the standard class.
	storageClass := string(head.StorageClass)
	if storageClass == "" {
		storageClass = string(s3types.StorageClassStandard)
	}
	return &ObjectOutput{
		Arn:                  objectARN(region, bucket, key),
		Key:                  key,
		Etag:                 strings.Trim(aws.ToString(head.ETag), `"`),
		VersionId:            aws.ToString(head.VersionId),
		ContentType:          aws.ToString(head.ContentType),
		StorageClass:         storageClass,
		ServerSideEncryption: string(head.ServerSideEncryption),
		BucketKeyEnabled:     aws.ToBool(head.BucketKeyEnabled),
		KmsKeyId:             aws.ToString(head.SSEKMSKeyId),
		ChecksumCrc32:        aws.ToString(head.ChecksumCRC32),
		ChecksumCrc32c:       aws.ToString(head.ChecksumCRC32C),
		ChecksumCrc64nvme:    aws.ToString(head.ChecksumCRC64NVME),
		ChecksumSha1:         aws.ToString(head.ChecksumSHA1),
		ChecksumSha256:       aws.ToString(head.ChecksumSHA256),
	}, nil
}

// body opens the object's body from whichever single source is set: an inline
// string, base64-encoded bytes, or a file at the given path. With none set the
// body is nil, which puts an empty object. The constraints guarantee at most
// one source, so the first match wins.
func (r *Object) body() (io.Reader, error) {
	switch {
	case r.BodyContent != nil:
		return strings.NewReader(*r.BodyContent), nil
	case r.BodyBase64 != nil:
		raw, err := base64.StdEncoding.DecodeString(*r.BodyBase64)
		if err != nil {
			return nil, fmt.Errorf("decode body-base64: %w", err)
		}
		return bytes.NewReader(raw), nil
	case r.BodyPath != nil:
		f, err := os.ReadFile(*r.BodyPath)
		if err != nil {
			return nil, fmt.Errorf("read body-path: %w", err)
		}
		return bytes.NewReader(f), nil
	default:
		return nil, nil
	}
}

// objectLockRequested reports whether any object-lock field is set, in which
// case S3 requires a checksum on the put.
func (r *Object) objectLockRequested() bool {
	return r.ObjectLockMode != nil ||
		r.ObjectLockRetainUntilDate != nil ||
		r.ObjectLockLegalHoldStatus != nil
}

// contentChanged reports whether any put field differs from its prior value, in
// which case Update re-puts. The body, metadata, encryption, ACL, storage
// class, and object-lock settings all ride the put, so a change to any of them
// re-uploads the whole object rather than patching one header.
func (r *Object) contentChanged(prior Object) bool {
	return runtime.Changed(prior.BodyContent, r.BodyContent) ||
		runtime.Changed(prior.BodyPath, r.BodyPath) ||
		runtime.Changed(prior.BodyBase64, r.BodyBase64) ||
		runtime.Changed(prior.ACL, r.ACL) ||
		runtime.Changed(prior.BucketKeyEnabled, r.BucketKeyEnabled) ||
		runtime.Changed(prior.CacheControl, r.CacheControl) ||
		runtime.Changed(prior.ChecksumAlgorithm, r.ChecksumAlgorithm) ||
		runtime.Changed(prior.ContentDisposition, r.ContentDisposition) ||
		runtime.Changed(prior.ContentEncoding, r.ContentEncoding) ||
		runtime.Changed(prior.ContentLanguage, r.ContentLanguage) ||
		runtime.Changed(prior.ContentType, r.ContentType) ||
		runtime.Changed(prior.KmsKeyId, r.KmsKeyId) ||
		runtime.Changed(ptr.Value(prior.Metadata), ptr.Value(r.Metadata)) ||
		runtime.Changed(prior.ServerSideEncryption, r.ServerSideEncryption) ||
		runtime.Changed(prior.StorageClass, r.StorageClass) ||
		runtime.Changed(prior.WebsiteRedirect, r.WebsiteRedirect) ||
		runtime.Changed(prior.ObjectLockMode, r.ObjectLockMode) ||
		runtime.Changed(prior.ObjectLockRetainUntilDate, r.ObjectLockRetainUntilDate) ||
		runtime.Changed(prior.ObjectLockLegalHoldStatus, r.ObjectLockLegalHoldStatus) ||
		runtime.Changed(ptr.Value(prior.Tags), ptr.Value(r.Tags))
}

// objectCleanKey normalizes a key to the form S3 stores it under: a leading
// "./" is stripped, runs of slashes collapse to one, and any leading slash is
// removed. The cleaned key is the object's real key and the basis of its ARN.
func objectCleanKey(key string) string {
	key = strings.TrimPrefix(key, "./")
	for strings.Contains(key, "//") {
		key = strings.ReplaceAll(key, "//", "/")
	}
	return strings.TrimPrefix(key, "/")
}

// objectARN builds the ARN of an object from the partition of the client's
// region, the bucket, and the cleaned key. HeadObject does not return an ARN,
// so it is composed locally in the standard s3 object form.
func objectARN(region, bucket, key string) string {
	return fmt.Sprintf("arn:%s:s3:::%s/%s", partition.Of(region), bucket, key)
}

// objectTagging encodes a tag map as the URL query string PutObject's Tagging
// field expects, with keys sorted so the request is deterministic. With no tags
// it returns nil so the field is omitted.
func objectTagging(tags map[string]string) *string {
	if len(tags) == 0 {
		return nil
	}
	values := url.Values{}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		values.Set(k, tags[k])
	}
	return aws.String(values.Encode())
}

// objectPurgeVersions removes every version and delete marker of one key. It
// pages ListObjectVersions by the key as a prefix, keeps only the entries whose
// key matches exactly, and bulk-deletes each page by version id with governance
// retention bypassed. A version the delete is refused for on an access-denied
// error has its legal hold turned off and is deleted again. A bucket or key
// already gone counts as done.
func objectPurgeVersions(ctx context.Context, client *s3.Client, bucket, key string) error {
	pager := s3.NewListObjectVersionsPaginator(client, &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(key),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err, "NoSuchBucket", "NoSuchKey") {
				return nil
			}
			return fmt.Errorf("list object versions: %w", err)
		}
		ids := make([]s3types.ObjectIdentifier, 0, len(page.Versions)+len(page.DeleteMarkers))
		for _, v := range page.Versions {
			if aws.ToString(v.Key) == key {
				ids = append(ids, s3types.ObjectIdentifier{
					Key:       v.Key,
					VersionId: v.VersionId,
				})
			}
		}
		for _, m := range page.DeleteMarkers {
			if aws.ToString(m.Key) == key {
				ids = append(ids, s3types.ObjectIdentifier{
					Key:       m.Key,
					VersionId: m.VersionId,
				})
			}
		}
		if err := objectDeleteVersions(ctx, client, bucket, ids); err != nil {
			return err
		}
	}
	return nil
}

// objectDeleteVersions bulk-deletes the given version identifiers in pages of
// up to a thousand, the DeleteObjects limit. The bulk delete carries no
// governance bypass, which S3 rejects on a bucket without Object Lock; a
// version that comes back AccessDenied -- the case that would need the bypass --
// has its legal hold cleared and is deleted again singly with the bypass set,
// valid there because only an Object Lock bucket produces that error. A bucket
// or key already gone counts as done.
func objectDeleteVersions(
	ctx context.Context, client *s3.Client, bucket string, ids []s3types.ObjectIdentifier,
) error {
	const batch = 1000
	for len(ids) > 0 {
		page := ids[:min(batch, len(ids))]
		ids = ids[len(page):]
		out, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{
				Objects: page,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			if isNotFound(err, "NoSuchBucket", "NoSuchKey") {
				return nil
			}
			return fmt.Errorf("delete object versions: %w", err)
		}
		if err := objectRetryDenied(ctx, client, bucket, out.Errors); err != nil {
			return err
		}
	}
	return nil
}

// objectRetryDenied handles the per-object errors a bulk delete reports. A
// version refused with AccessDenied is under a legal hold; turning the hold off
// and deleting that single version again clears it. Any other per-object error
// is fatal.
func objectRetryDenied(
	ctx context.Context, client *s3.Client, bucket string, errs []s3types.Error,
) error {
	for _, e := range errs {
		if aws.ToString(e.Code) != "AccessDenied" {
			return fmt.Errorf("delete object version %s: %s",
				aws.ToString(e.Key), aws.ToString(e.Message))
		}
		_, err := client.PutObjectLegalHold(ctx, &s3.PutObjectLegalHoldInput{
			Bucket:    aws.String(bucket),
			Key:       e.Key,
			VersionId: e.VersionId,
			LegalHold: &s3types.ObjectLockLegalHold{
				Status: s3types.ObjectLockLegalHoldStatusOff,
			},
		})
		if err != nil {
			return fmt.Errorf("clear legal hold on %s: %w", aws.ToString(e.Key), err)
		}
		_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket:                    aws.String(bucket),
			Key:                       e.Key,
			VersionId:                 e.VersionId,
			BypassGovernanceRetention: aws.Bool(true),
		})
		if err != nil && !isNotFound(err, "NoSuchBucket", "NoSuchKey") {
			return fmt.Errorf("delete object version %s: %w", aws.ToString(e.Key), err)
		}
	}
	return nil
}
