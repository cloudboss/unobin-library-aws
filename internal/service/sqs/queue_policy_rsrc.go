package sqs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// QueuePolicy manages the policy attached to an SQS queue. The queue URL is the
// policy's identity; a queue holds one policy keyed by its URL, so the queue
// cannot change without replacing the policy, while the policy document is
// reconciled in place. The document is sent to SQS verbatim: unobin compares
// inputs as written, so the policy never needs canonicalizing to avoid a
// phantom diff against the form SQS echoes back.
type QueuePolicy struct {
	QueueUrl string `ub:"queue-url"`
	Policy   string `ub:"policy"`
}

// QueuePolicyOutput is empty: a queue policy computes nothing of its own, and
// its identity is the input queue URL. Downstream references point at the
// queue, not the policy text, so there is no value to expose.
type QueuePolicyOutput struct{}

func (r *QueuePolicy) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs SQS fixes for the life of the policy. A queue
// holds a single policy keyed by its URL, so re-pointing the policy at a
// different queue means deleting it here and creating it there. The policy
// document itself is reconciled in place by Update.
func (r *QueuePolicy) ReplaceFields() []string {
	return []string{"queue-url"}
}

func (r *QueuePolicy) Create(ctx context.Context, cfg any) (*QueuePolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client); err != nil {
		return nil, err
	}
	// SetQueueAttributes can return before the policy is readable, so wait for
	// GetQueueAttributes to find it once. Without this an immediately following
	// read could see an absent Policy attribute and take the just-made policy
	// for not set. This wait is create-only; an update reconciles an
	// already-present policy and needs no such wait.
	err = wait.Until(ctx, fmt.Sprintf("queue policy %s", r.QueueUrl),
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
	return &QueuePolicyOutput{}, nil
}

func (r *QueuePolicy) Read(
	ctx context.Context, cfg any, prior *QueuePolicyOutput,
) (*QueuePolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	found, err := r.exists(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("get queue policy: %w", err)
	}
	// A gone queue reports NonExistentQueue, and a live queue can answer with no
	// Policy attribute or an empty one; either means the policy is absent and
	// the resource should be recreated rather than read as an empty policy.
	if !found {
		return nil, runtime.ErrNotFound
	}
	return &QueuePolicyOutput{}, nil
}

func (r *QueuePolicy) Update(
	ctx context.Context, cfg any, prior runtime.Prior[QueuePolicy, *QueuePolicyOutput],
) (*QueuePolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The policy document is the only reconcilable input; re-put it only when it
	// changed, so an unchanged apply makes no write SQS does not need.
	if runtime.Changed(prior.Inputs.Policy, r.Policy) {
		if err := r.put(ctx, client); err != nil {
			return nil, err
		}
	}
	return &QueuePolicyOutput{}, nil
}

func (r *QueuePolicy) Delete(ctx context.Context, cfg any, prior *QueuePolicyOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// SQS has no delete-policy call; setting the Policy attribute to the empty
	// string is the sentinel that removes it.
	_, err = client.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
		QueueUrl:   aws.String(r.QueueUrl),
		Attributes: map[string]string{string(sqstypes.QueueAttributeNamePolicy): ""},
	})
	if err != nil {
		// A queue already gone takes its policy with it, so a missing queue
		// counts as deleted. Only NonExistentQueue is tolerated; any other error
		// is real.
		if !isQueueNotFound(err) {
			return fmt.Errorf("clear queue policy: %w", err)
		}
	}
	// The clear can return before the policy stops being readable, so wait for
	// GetQueueAttributes to report it gone, polling every second since a cleared
	// policy disappears quickly.
	return wait.Until(ctx, fmt.Sprintf("queue policy %s removal", r.QueueUrl),
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

// put writes the queue policy, retrying the transient error that clears on its
// own. A policy naming an IAM principal created moments earlier is rejected as
// InvalidAttributeValue until that principal propagates; the principal settles
// within the propagation window.
func (r *QueuePolicy) put(ctx context.Context, client *sqs.Client) error {
	in := &sqs.SetQueueAttributesInput{
		QueueUrl:   aws.String(r.QueueUrl),
		Attributes: map[string]string{string(sqstypes.QueueAttributeNamePolicy): r.Policy},
	}
	err := retry.OnError(ctx, queuePolicyRetryable, func(ctx context.Context) error {
		_, err := client.SetQueueAttributes(ctx, in)
		return err
	}, retry.WithInterval(time.Second))
	if err != nil {
		return fmt.Errorf("set queue policy: %w", err)
	}
	return nil
}

// exists reports whether the queue currently has a policy. A gone queue
// (NonExistentQueue) and a live queue whose Policy attribute is absent or empty
// both mean no policy is present. Any other error is real and stops the caller.
func (r *QueuePolicy) exists(ctx context.Context, client *sqs.Client) (bool, error) {
	resp, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(r.QueueUrl),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNamePolicy},
	})
	if err != nil {
		if isQueueNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if resp == nil {
		return false, nil
	}
	return resp.Attributes[string(sqstypes.QueueAttributeNamePolicy)] != "", nil
}

// queuePolicyRetryable reports whether a SetQueueAttributes error is the
// self-clearing one: SQS rejects a policy whose named principal has not yet
// propagated with code InvalidAttributeValue and a message naming the Policy
// parameter. The code alone is too broad -- it also covers a genuinely
// malformed value -- so the message is matched too, and only that pairing
// retries.
func queuePolicyRetryable(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorCode() == "InvalidAttributeValue" &&
		strings.Contains(apiErr.ErrorMessage(), "Invalid value for the parameter Policy")
}
