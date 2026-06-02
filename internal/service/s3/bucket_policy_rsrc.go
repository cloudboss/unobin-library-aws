package s3

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// BucketPolicy manages the resource policy attached to an S3 bucket. The
// bucket name is the policy's identity; S3 holds one policy per bucket, so the
// bucket cannot change without replacing the policy, while the policy document
// is reconciled in place. The document is sent to S3 verbatim: unobin compares
// inputs as written, so the policy never needs canonicalizing to avoid a
// phantom diff against the form S3 echoes back.
type BucketPolicy struct {
	Bucket string `ub:"bucket"`
	Policy string `ub:"policy"`
}

// BucketPolicyOutput is empty: a bucket policy computes nothing of its own,
// and its identity is the input bucket name. Downstream references point at the
// bucket, not the policy text, so there is no value to expose.
type BucketPolicyOutput struct{}

func (r *BucketPolicy) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs S3 fixes for the life of the policy. A bucket
// holds a single policy keyed by its name, so re-pointing the policy at a
// different bucket means deleting it here and creating it there. The policy
// document itself is reconciled in place by Update.
func (r *BucketPolicy) ReplaceFields() []string {
	return []string{"bucket"}
}

func (r *BucketPolicy) Create(ctx context.Context, cfg any) (*BucketPolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client); err != nil {
		return nil, err
	}
	// PutBucketPolicy can return before the policy is readable, and the bucket
	// itself may still be settling, so wait for GetBucketPolicy to find it once.
	// Without this an immediately following read could see NoSuchBucketPolicy
	// and take the just-made policy for absent. This wait is create-only; an
	// update reconciles an already-present policy and needs no such wait.
	err = wait.Until(ctx, fmt.Sprintf("bucket policy %s", r.Bucket),
		func(ctx context.Context) (bool, error) {
			found, err := r.exists(ctx, client)
			if err != nil {
				return false, err
			}
			return found, nil
		},
	)
	if err != nil {
		return nil, err
	}
	return &BucketPolicyOutput{}, nil
}

func (r *BucketPolicy) Read(
	ctx context.Context, cfg any, prior *BucketPolicyOutput,
) (*BucketPolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(r.Bucket),
	})
	if err != nil {
		// A bucket with no policy reports NoSuchBucketPolicy, and a bucket that
		// is gone reports NoSuchBucket; either means the policy is absent and the
		// resource should be recreated.
		if isNotFound(err, "NoSuchBucket", "NoSuchBucketPolicy") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get bucket policy: %w", err)
	}
	// S3 can answer with an empty body rather than the not-found error; an
	// absent policy document is the same drift.
	if resp == nil || resp.Policy == nil {
		return nil, runtime.ErrNotFound
	}
	return &BucketPolicyOutput{}, nil
}

func (r *BucketPolicy) Update(
	ctx context.Context, cfg any, prior runtime.Prior[BucketPolicy, *BucketPolicyOutput],
) (*BucketPolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client); err != nil {
		return nil, err
	}
	return &BucketPolicyOutput{}, nil
}

func (r *BucketPolicy) Delete(ctx context.Context, cfg any, prior *BucketPolicyOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// The delete retries OperationAborted, since the bucket's other configurations
	// are torn down at the same time and S3 serializes operations on a bucket.
	err = retry.OnError(ctx, isOperationAborted, func(ctx context.Context) error {
		_, err := client.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{
			Bucket: aws.String(r.Bucket),
		})
		return err
	}, retry.WithInterval(time.Second))
	if err != nil {
		// A bucket already gone takes its policy with it, so a missing bucket
		// counts as deleted. A missing policy on a live bucket does not, so only
		// NoSuchBucket is tolerated.
		if !isNotFound(err, "NoSuchBucket") {
			return fmt.Errorf("delete bucket policy: %w", err)
		}
	}
	// The delete can return before the policy stops being readable, so wait for
	// GetBucketPolicy to report it gone, polling every second since a deleted
	// policy disappears quickly.
	return wait.Until(ctx, fmt.Sprintf("bucket policy %s removal", r.Bucket),
		func(ctx context.Context) (bool, error) {
			found, err := r.exists(ctx, client)
			if err != nil {
				return false, err
			}
			return !found, nil
		},
		wait.WithInterval(time.Second),
	)
}

// put writes the bucket policy, retrying the transient errors that clear on
// their own. A policy naming an IAM principal created moments earlier is
// rejected as MalformedPolicy until that principal propagates, and a bucket
// created moments earlier reports NoSuchBucket until it is globally visible;
// both settle within the propagation window.
func (r *BucketPolicy) put(ctx context.Context, client *s3.Client) error {
	in := &s3.PutBucketPolicyInput{
		Bucket: aws.String(r.Bucket),
		Policy: aws.String(r.Policy),
	}
	err := retry.OnError(ctx, bucketPolicyRetryable, func(ctx context.Context) error {
		_, err := client.PutBucketPolicy(ctx, in)
		return err
	}, retry.WithInterval(time.Second))
	if err != nil {
		return fmt.Errorf("put bucket policy: %w", err)
	}
	return nil
}

// exists reports whether the bucket currently has a policy. The two not-found
// codes mean no policy is present; an empty body means the same. Any other
// error is real and stops the wait.
func (r *BucketPolicy) exists(ctx context.Context, client *s3.Client) (bool, error) {
	resp, err := client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(r.Bucket),
	})
	if err != nil {
		if isNotFound(err, "NoSuchBucket", "NoSuchBucketPolicy") {
			return false, nil
		}
		return false, fmt.Errorf("get bucket policy: %w", err)
	}
	return resp != nil && resp.Policy != nil, nil
}

// bucketPolicyRetryable reports whether a PutBucketPolicy error is one that
// clears on its own: a malformed policy whose named principal has not yet
// propagated, a bucket not yet globally visible, or a sibling configuration
// operation holding the bucket (OperationAborted). IsNotFound matches a
// smithy.APIError by service code, so it serves for MalformedPolicy as well as
// the genuine not-found codes.
func bucketPolicyRetryable(err error) bool {
	return isNotFound(err, "MalformedPolicy", "NoSuchBucket") ||
		isOperationAborted(err)
}
