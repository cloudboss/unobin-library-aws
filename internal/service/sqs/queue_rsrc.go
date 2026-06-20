package sqs

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// errCodeQueueDeletedRecently is the error code SQS returns when a queue is
// created with the name of a queue deleted within the last sixty seconds. The
// name frees up on its own once that window passes, so create retries through
// it.
const errCodeQueueDeletedRecently = "AWS.SimpleQueueService.QueueDeletedRecently"

// errCodeQueueDoesNotExist is the older query-protocol code for a missing
// queue. SQS over JSON reports the same condition as QueueDoesNotExist; both
// reach the caller as a smithy.APIError, so a read or delete matches either.
const (
	errCodeQueueDoesNotExist    = "AWS.SimpleQueueService.NonExistentQueue"
	errCodeQueueDoesNotExistNew = "QueueDoesNotExist"
)

// attributeReusePeriodDefault is the data-key reuse period SQS applies when a
// KMS-encrypted queue omits the attribute. The propagation waiter treats an
// expected value equal to this default as matched when the queue does not echo
// the attribute, so it does not spin on a value the cloud never reports.
const attributeReusePeriodDefault = "300"

// fifoNameRegexp matches a FIFO queue name: up to 75 name characters followed
// by the required .fifo suffix. standardNameRegexp matches a standard queue
// name of up to 80 name characters. SQS counts these lengths in characters,
// and the character class is ASCII, so the byte length the regexp enforces
// equals the character length.
var (
	fifoNameRegexp     = regexp.MustCompile(`^[0-9A-Za-z_-]{1,75}\.fifo$`)
	standardNameRegexp = regexp.MustCompile(`^[0-9A-Za-z_-]{1,80}$`)
)

// Queue manages an SQS queue, standard or FIFO. The queue name and the FIFO
// flag are fixed at create time -- a standard queue cannot become a FIFO queue
// or be renamed in place -- so a change to either replaces the queue; every
// other attribute is reconciled in place by Update through a single
// SetQueueAttributes call. An omitted optional input rides as absent and SQS
// applies its own default for it.
//
// Name is required, since SQS does not generate a queue name and CreateQueue
// rejects an empty one. It must match the SQS name rules, which depend on the
// queue type: a FIFO name must match ^[0-9A-Za-z_-]{1,75}\.fifo$ and a standard
// name ^[0-9A-Za-z_-]{1,80}$. That rule is a regular-expression and byte-length
// check enforced in Create rather than a declarative constraint.
type Queue struct {
	Name                          string            `ub:"name"`
	FifoQueue                     *bool             `ub:"fifo-queue"`
	ContentBasedDeduplication     *bool             `ub:"content-based-deduplication"`
	DeduplicationScope            *string           `ub:"deduplication-scope"`
	FifoThroughputLimit           *string           `ub:"fifo-throughput-limit"`
	DelaySeconds                  *int64            `ub:"delay-seconds"`
	MaximumMessageSize            *int64            `ub:"maximum-message-size"`
	MessageRetentionPeriod        *int64            `ub:"message-retention-period"`
	ReceiveMessageWaitTimeSeconds *int64            `ub:"receive-message-wait-time-seconds"`
	VisibilityTimeout             *int64            `ub:"visibility-timeout"`
	KmsMasterKeyId                *string           `ub:"kms-master-key-id"`
	KmsDataKeyReusePeriodSeconds  *int64            `ub:"kms-data-key-reuse-period-seconds"`
	SqsManagedSseEnabled          *bool             `ub:"sqs-managed-sse-enabled"`
	Policy                        *string           `ub:"policy"`
	RedrivePolicy                 *string           `ub:"redrive-policy"`
	RedriveAllowPolicy            *string           `ub:"redrive-allow-policy"`
	Tags                          map[string]string `ub:"tags"`
}

// QueueOutput holds the values SQS computes for a queue. The ARN is the queue's
// identity in policies, event source mappings, and subscriptions, and settles
// only after the post-create read since CreateQueue returns no ARN. The URL is
// the stable handle every attribute call, the read, and the delete address the
// queue by, so it is the identity Delete keys off the prior outputs on a
// replace.
type QueueOutput struct {
	Arn string `ub:"arn"`
	Url string `ub:"url"`
}

func (r *Queue) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs SQS fixes when a queue is created. The queue
// name and the FIFO flag cannot be changed on an existing queue, so a change to
// either requires a new queue. Every other input is reconciled in place by
// Update.
func (r *Queue) ReplaceFields() []string {
	return []string{
		"name",
		"fifo-queue",
	}
}

// Defaults marks the collection inputs a queue may omit.
func (r Queue) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules SQS places on a queue's inputs. A queue uses
// at most one server-side encryption scheme, so a KMS key and SQS-managed
// encryption are mutually exclusive. Content-based deduplication and the two
// high-throughput attributes apply only to FIFO queues. The enum and numeric
// attributes each accept a fixed set or range of values; the name rules are a
// regular-expression check in Create rather than a constraint, since they
// depend on the queue type and count bytes.
func (r Queue) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.KmsMasterKeyId, r.SqsManagedSseEnabled),
		constraint.When(constraint.IsTrue(r.ContentBasedDeduplication)).
			Require(constraint.IsTrue(r.FifoQueue)).
			Message("content-based-deduplication requires fifo-queue to be true"),
		constraint.When(constraint.Present(r.DeduplicationScope)).
			Require(constraint.OneOf(r.DeduplicationScope, "messageGroup", "queue")).
			Message("deduplication-scope must be messageGroup or queue"),
		constraint.When(constraint.Present(r.FifoThroughputLimit)).
			Require(constraint.OneOf(r.FifoThroughputLimit, "perQueue", "perMessageGroupId")).
			Message("fifo-throughput-limit must be perQueue or perMessageGroupId"),
		constraint.When(constraint.Present(r.DelaySeconds)).
			Require(constraint.AtLeast(r.DelaySeconds, 0), constraint.AtMost(r.DelaySeconds, 900)).
			Message("delay-seconds must be between 0 and 900"),
		constraint.When(constraint.Present(r.MaximumMessageSize)).
			Require(constraint.AtLeast(r.MaximumMessageSize, 1024),
				constraint.AtMost(r.MaximumMessageSize, 1048576)).
			Message("maximum-message-size must be between 1024 and 1048576"),
		constraint.When(constraint.Present(r.MessageRetentionPeriod)).
			Require(constraint.AtLeast(r.MessageRetentionPeriod, 60),
				constraint.AtMost(r.MessageRetentionPeriod, 1209600)).
			Message("message-retention-period must be between 60 and 1209600"),
		constraint.When(constraint.Present(r.ReceiveMessageWaitTimeSeconds)).
			Require(constraint.AtLeast(r.ReceiveMessageWaitTimeSeconds, 0),
				constraint.AtMost(r.ReceiveMessageWaitTimeSeconds, 20)).
			Message("receive-message-wait-time-seconds must be between 0 and 20"),
		constraint.When(constraint.Present(r.VisibilityTimeout)).
			Require(constraint.AtLeast(r.VisibilityTimeout, 0),
				constraint.AtMost(r.VisibilityTimeout, 43200)).
			Message("visibility-timeout must be between 0 and 43200"),
		constraint.When(constraint.Present(r.KmsDataKeyReusePeriodSeconds)).
			Require(constraint.AtLeast(r.KmsDataKeyReusePeriodSeconds, 60),
				constraint.AtMost(r.KmsDataKeyReusePeriodSeconds, 86400)).
			Message("kms-data-key-reuse-period-seconds must be between 60 and 86400"),
	}
}

func (r *Queue) Create(ctx context.Context, cfg *awsCfg) (*QueueOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	name, err := r.queueName()
	if err != nil {
		return nil, err
	}
	attributes := r.attributes()
	in := &sqs.CreateQueueInput{
		QueueName:  aws.String(name),
		Attributes: attributes,
		Tags:       r.Tags,
	}
	// Some partitions, such as the ISO partitions, cannot tag a queue as it is
	// created. When the tagged create fails for that reason, create the queue
	// without tags and apply them with a separate call below.
	taggedSeparately := false
	resp, err := r.createQueue(ctx, client, in)
	if err != nil && in.Tags != nil && partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		resp, err = r.createQueue(ctx, client, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create queue: %w", err)
	}
	url := aws.ToString(resp.QueueUrl)
	// CreateQueue returns before the attributes it set are consistently
	// readable, so wait until a GetQueueAttributes echoes every value just sent
	// before treating the queue as settled.
	if err := r.waitAttributesPropagated(ctx, client, url, attributes); err != nil {
		return nil, err
	}
	if taggedSeparately && len(r.Tags) > 0 {
		if err := r.createTags(ctx, client, url); err != nil {
			return nil, err
		}
	}
	// The ARN is not in the create response; the post-create read obtains it.
	return r.read(ctx, client, url)
}

func (r *Queue) Read(ctx context.Context, cfg *awsCfg, prior *QueueOutput) (*QueueOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Url)
}

// read fetches the queue's attributes by URL and returns its computed outputs.
// SQS is read-after-write eventually consistent, so a freshly created or
// updated queue can briefly read as not-found; the read retries through that
// window. A queue that stays not-found, or one whose attribute response is
// empty, is drift and maps to runtime.ErrNotFound so a plan recreates it.
func (r *Queue) read(
	ctx context.Context, client *sqs.Client, url string,
) (*QueueOutput, error) {
	var attributes map[string]string
	err := retry.OnError(ctx, isQueueNotFound, func(ctx context.Context) error {
		resp, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl:       aws.String(url),
			AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll},
		})
		if err != nil {
			return err
		}
		// An empty attribute response is the not-found-equivalent SQS returns
		// for a queue still settling; report it as the not-found code so the
		// retry rides it out like a real not-found.
		if len(resp.Attributes) == 0 {
			return &sqstypes.QueueDoesNotExist{}
		}
		attributes = resp.Attributes
		return nil
	}, retry.WithTimeout(20*time.Second))
	if err != nil {
		if isQueueNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get queue attributes: %w", err)
	}
	return &QueueOutput{
		Arn: attributes[string(sqstypes.QueueAttributeNameQueueArn)],
		Url: url,
	}, nil
}

func (r *Queue) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Queue, *QueueOutput],
) (*QueueOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	url := prior.Outputs.Url
	// SetQueueAttributes reconciles every changed non-tag attribute in one call,
	// so it runs only when at least one of those attributes changed. A tag-only
	// change is left to the tag reconcile below.
	changed := r.changedAttributes(prior.Inputs)
	if len(changed) > 0 {
		_, err := client.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
			QueueUrl:   aws.String(url),
			Attributes: changed,
		})
		if err != nil {
			return nil, fmt.Errorf("set queue attributes: %w", err)
		}
		if err := r.waitAttributesPropagated(ctx, client, url, changed); err != nil {
			return nil, err
		}
	}
	// SQS reconciles tags through its own tag calls, not SetQueueAttributes, so
	// reconcile them as a set whenever they changed.
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client, url); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, url)
}

func (r *Queue) Delete(ctx context.Context, cfg *awsCfg, prior *QueueOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	url := prior.Url
	_, err = client.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(url)})
	if err != nil {
		// A queue already gone counts as deleted.
		if isQueueNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete queue: %w", err)
	}
	// A queue keeps describing for a while after DeleteQueue returns, so wait
	// until a GetQueueAttributes reports it gone before treating it as deleted.
	return r.waitDeleted(ctx, client, url)
}

// createQueue calls CreateQueue and retries it while SQS rejects the name as
// recently deleted. That sixty-second window clears on its own, so the retry
// runs over a bounded span of roughly that length.
func (r *Queue) createQueue(
	ctx context.Context, client *sqs.Client, in *sqs.CreateQueueInput,
) (*sqs.CreateQueueOutput, error) {
	var resp *sqs.CreateQueueOutput
	err := retry.OnError(ctx, isQueueDeletedRecently, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateQueue(ctx, in)
		return err
	}, retry.WithTimeout(90*time.Second))
	return resp, err
}

// queueName validates the queue's name against the SQS name rules for the
// queue's type, which the regular expressions enforce; the character class is
// ASCII so the regexp's length bound is the byte length SQS limits.
func (r *Queue) queueName() (string, error) {
	name := r.Name
	if aws.ToBool(r.FifoQueue) {
		if !fifoNameRegexp.MatchString(name) {
			return "", fmt.Errorf(
				"name %q must match %s for a fifo queue", name, fifoNameRegexp.String())
		}
		return name, nil
	}
	if !standardNameRegexp.MatchString(name) {
		return "", fmt.Errorf(
			"name %q must match %s for a standard queue", name, standardNameRegexp.String())
	}
	return name, nil
}

// attributes builds the full attribute map for the create call from the inputs
// the user set. An unset pointer is left out so SQS applies its own default; a
// set value, including a false bool, is sent. The data-key reuse period is only
// meaningful when the queue uses KMS encryption, so it is sent only alongside a
// KMS key, matching what SQS will accept. The FIFO-only attributes are sent
// only for a FIFO queue, since SQS never echoes them back on a standard queue
// and the propagation wait would otherwise spin on a value the cloud never
// reports.
func (r *Queue) attributes() map[string]string {
	attrs := map[string]string{}
	r.putBool(attrs, sqstypes.QueueAttributeNameFifoQueue, r.FifoQueue)
	if aws.ToBool(r.FifoQueue) {
		r.putBool(attrs, sqstypes.QueueAttributeNameContentBasedDeduplication,
			r.ContentBasedDeduplication)
		r.putString(attrs, sqstypes.QueueAttributeNameDeduplicationScope, r.DeduplicationScope)
		r.putString(attrs, sqstypes.QueueAttributeNameFifoThroughputLimit, r.FifoThroughputLimit)
	}
	r.putInt(attrs, sqstypes.QueueAttributeNameDelaySeconds, r.DelaySeconds)
	r.putInt(attrs, sqstypes.QueueAttributeNameMaximumMessageSize, r.MaximumMessageSize)
	r.putInt(attrs, sqstypes.QueueAttributeNameMessageRetentionPeriod, r.MessageRetentionPeriod)
	r.putInt(attrs, sqstypes.QueueAttributeNameReceiveMessageWaitTimeSeconds,
		r.ReceiveMessageWaitTimeSeconds)
	r.putInt(attrs, sqstypes.QueueAttributeNameVisibilityTimeout, r.VisibilityTimeout)
	r.putString(attrs, sqstypes.QueueAttributeNameKmsMasterKeyId, r.KmsMasterKeyId)
	if r.KmsMasterKeyId != nil {
		r.putInt(attrs, sqstypes.QueueAttributeNameKmsDataKeyReusePeriodSeconds,
			r.KmsDataKeyReusePeriodSeconds)
	}
	r.putBool(attrs, sqstypes.QueueAttributeNameSqsManagedSseEnabled, r.SqsManagedSseEnabled)
	r.putString(attrs, sqstypes.QueueAttributeNamePolicy, r.Policy)
	r.putString(attrs, sqstypes.QueueAttributeNameRedrivePolicy, r.RedrivePolicy)
	r.putString(attrs, sqstypes.QueueAttributeNameRedriveAllowPolicy, r.RedriveAllowPolicy)
	return attrs
}

// changedAttributes builds the attribute map for an update from only the
// attributes whose value differs from the prior inputs. The name and the FIFO
// flag are immutable and reconciled by a replace, so they are not part of an
// update. A field cleared to nil is sent as the empty string SQS uses to reset
// a policy attribute; for the other attributes a nil value means leave the
// current setting in place, so only a changed present value is sent. The
// FIFO-only attributes are sent only for a FIFO queue, matching create, since
// SQS never echoes them back on a standard queue.
func (r *Queue) changedAttributes(prior Queue) map[string]string {
	attrs := map[string]string{}
	if aws.ToBool(r.FifoQueue) {
		if runtime.Changed(prior.ContentBasedDeduplication, r.ContentBasedDeduplication) {
			r.putBool(attrs, sqstypes.QueueAttributeNameContentBasedDeduplication,
				r.ContentBasedDeduplication)
		}
		if runtime.Changed(prior.DeduplicationScope, r.DeduplicationScope) {
			r.putString(attrs, sqstypes.QueueAttributeNameDeduplicationScope, r.DeduplicationScope)
		}
		if runtime.Changed(prior.FifoThroughputLimit, r.FifoThroughputLimit) {
			r.putString(attrs, sqstypes.QueueAttributeNameFifoThroughputLimit,
				r.FifoThroughputLimit)
		}
	}
	if runtime.Changed(prior.DelaySeconds, r.DelaySeconds) {
		r.putInt(attrs, sqstypes.QueueAttributeNameDelaySeconds, r.DelaySeconds)
	}
	if runtime.Changed(prior.MaximumMessageSize, r.MaximumMessageSize) {
		r.putInt(attrs, sqstypes.QueueAttributeNameMaximumMessageSize, r.MaximumMessageSize)
	}
	if runtime.Changed(prior.MessageRetentionPeriod, r.MessageRetentionPeriod) {
		r.putInt(attrs, sqstypes.QueueAttributeNameMessageRetentionPeriod,
			r.MessageRetentionPeriod)
	}
	if runtime.Changed(prior.ReceiveMessageWaitTimeSeconds, r.ReceiveMessageWaitTimeSeconds) {
		r.putInt(attrs, sqstypes.QueueAttributeNameReceiveMessageWaitTimeSeconds,
			r.ReceiveMessageWaitTimeSeconds)
	}
	if runtime.Changed(prior.VisibilityTimeout, r.VisibilityTimeout) {
		r.putInt(attrs, sqstypes.QueueAttributeNameVisibilityTimeout, r.VisibilityTimeout)
	}
	if runtime.Changed(prior.KmsMasterKeyId, r.KmsMasterKeyId) {
		r.putString(attrs, sqstypes.QueueAttributeNameKmsMasterKeyId, r.KmsMasterKeyId)
	}
	if r.KmsMasterKeyId != nil &&
		runtime.Changed(prior.KmsDataKeyReusePeriodSeconds, r.KmsDataKeyReusePeriodSeconds) {
		r.putInt(attrs, sqstypes.QueueAttributeNameKmsDataKeyReusePeriodSeconds,
			r.KmsDataKeyReusePeriodSeconds)
	}
	if runtime.Changed(prior.SqsManagedSseEnabled, r.SqsManagedSseEnabled) {
		r.putBool(attrs, sqstypes.QueueAttributeNameSqsManagedSseEnabled, r.SqsManagedSseEnabled)
	}
	r.putChangedPolicy(attrs, sqstypes.QueueAttributeNamePolicy, prior.Policy, r.Policy)
	r.putChangedPolicy(attrs, sqstypes.QueueAttributeNameRedrivePolicy,
		prior.RedrivePolicy, r.RedrivePolicy)
	r.putChangedPolicy(attrs, sqstypes.QueueAttributeNameRedriveAllowPolicy,
		prior.RedriveAllowPolicy, r.RedriveAllowPolicy)
	return attrs
}

// putString records a string attribute when the pointer is set, leaving it out
// when nil so SQS keeps its own default or current value.
func (r *Queue) putString(attrs map[string]string, name sqstypes.QueueAttributeName, v *string) {
	if v != nil {
		attrs[string(name)] = *v
	}
}

// putBool records a bool attribute as "true" or "false" when the pointer is
// set. A set false is sent, since the user chose it; an unset bool is left out.
func (r *Queue) putBool(attrs map[string]string, name sqstypes.QueueAttributeName, v *bool) {
	if v != nil {
		attrs[string(name)] = strconv.FormatBool(*v)
	}
}

// putInt records an integer attribute as its decimal string when the pointer
// is set, leaving it out when nil.
func (r *Queue) putInt(attrs map[string]string, name sqstypes.QueueAttributeName, v *int64) {
	if v != nil {
		attrs[string(name)] = strconv.FormatInt(*v, 10)
	}
}

// putChangedPolicy records a policy attribute on update when it changed.
// Clearing a policy to nil sends the empty string SQS uses to reset it, so a
// removed policy is reset rather than left in place; a changed present value is
// sent verbatim.
func (r *Queue) putChangedPolicy(
	attrs map[string]string, name sqstypes.QueueAttributeName, prior, current *string,
) {
	if !runtime.Changed(prior, current) {
		return
	}
	if current == nil {
		attrs[string(name)] = ""
		return
	}
	attrs[string(name)] = *current
}

// waitAttributesPropagated polls GetQueueAttributes until every attribute just
// sent equals the value the cloud reports. SQS is eventually consistent after a
// write, so the wait re-confirms the values over consecutive reads rather than
// trusting a single one. The match is not a plain string compare for a few
// attributes: a policy, redrive policy, or redrive-allow policy is compared as
// equivalent JSON so re-ordered keys or whitespace do not look like a mismatch,
// and the data-key reuse period is treated as matched when the queue is
// unencrypted or when the expected value is the default the cloud does not echo.
func (r *Queue) waitAttributesPropagated(
	ctx context.Context, client *sqs.Client, url string, expected map[string]string,
) error {
	if len(expected) == 0 {
		return nil
	}
	what := fmt.Sprintf("queue %s attributes", url)
	return wait.UntilStable(ctx, what, 6, func(ctx context.Context) (bool, error) {
		resp, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl:       aws.String(url),
			AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll},
		})
		if err != nil {
			if isQueueNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("get queue attributes: %w", err)
		}
		for name, want := range expected {
			if !attributeMatches(name, want, resp.Attributes) {
				return false, nil
			}
		}
		return true, nil
	})
}

// waitDeleted polls GetQueueAttributes until the queue reports as gone, since a
// deleted queue keeps describing for a while after DeleteQueue returns. The
// not-found is confirmed over fifteen consecutive reads, within a three-minute
// budget, so a lagging replica that still describes the queue does not end the
// wait early.
func (r *Queue) waitDeleted(ctx context.Context, client *sqs.Client, url string) error {
	what := fmt.Sprintf("queue %s deletion", url)
	return wait.UntilStable(ctx, what, 15, func(ctx context.Context) (bool, error) {
		_, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl:       aws.String(url),
			AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll},
		})
		if err != nil {
			if isQueueNotFound(err) {
				return true, nil
			}
			return false, fmt.Errorf("get queue attributes: %w", err)
		}
		return false, nil
	}, wait.WithInterval(3*time.Second), wait.WithTimeout(3*time.Minute))
}

// attributeMatches reports whether the value the cloud reports for an attribute
// equals the value just sent. Most attributes compare by string. A policy,
// redrive policy, or redrive-allow policy compares as equivalent JSON, since
// SQS re-serializes these and may re-order keys. The data-key reuse period is
// treated as matched when the queue is unencrypted (no KMS key reported) or
// when the expected value is the default SQS does not echo, so the wait does
// not spin on a value the cloud never reports.
func attributeMatches(name, want string, actual map[string]string) bool {
	got, present := actual[name]
	switch name {
	case string(sqstypes.QueueAttributeNamePolicy),
		string(sqstypes.QueueAttributeNameRedrivePolicy),
		string(sqstypes.QueueAttributeNameRedriveAllowPolicy):
		return jsonEqual(want, got)
	case string(sqstypes.QueueAttributeNameKmsDataKeyReusePeriodSeconds):
		if actual[string(sqstypes.QueueAttributeNameKmsMasterKeyId)] == "" {
			return true
		}
		if !present && want == attributeReusePeriodDefault {
			return true
		}
		return got == want
	default:
		return got == want
	}
}

// jsonEqual reports whether two policy strings are equal as JSON, comparing
// their decoded structure so key order and whitespace do not matter. An empty
// expected string matches an empty or absent actual value, the form SQS reports
// for a policy that was cleared. A string that does not parse as JSON falls
// back to an exact compare.
func jsonEqual(want, got string) bool {
	if want == "" {
		return got == ""
	}
	var wantValue, gotValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		return want == got
	}
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		return want == got
	}
	return reflect.DeepEqual(wantValue, gotValue)
}

// createTags applies the queue's tags after a create that could not tag the
// queue in the same call. An unsupported-operation error is tolerated only when
// the user set no explicit tags, the default-tags-only case where there is
// nothing the user asked for to lose.
func (r *Queue) createTags(ctx context.Context, client *sqs.Client, url string) error {
	_, err := client.TagQueue(ctx, &sqs.TagQueueInput{
		QueueUrl: aws.String(url),
		Tags:     r.Tags,
	})
	if err != nil {
		if len(r.Tags) == 0 && partition.UnsupportedOperation(region(client), err) {
			return nil
		}
		return fmt.Errorf("tag queue: %w", err)
	}
	return nil
}

// syncTags reconciles the queue's tags with the desired set, reading the live
// tags through ListQueueTags and writing changes with TagQueue and UntagQueue.
// SQS addresses a queue's tags by its URL.
func (r *Queue) syncTags(ctx context.Context, client *sqs.Client, url string) error {
	return tagsync.Sync(ctx, r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListQueueTags(ctx, &sqs.ListQueueTagsInput{
				QueueUrl: aws.String(url),
			})
			if err != nil {
				return nil, fmt.Errorf("list queue tags: %w", err)
			}
			return resp.Tags, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagQueue(ctx, &sqs.TagQueueInput{
				QueueUrl: aws.String(url),
				Tags:     upsert,
			}); err != nil {
				return fmt.Errorf("tag queue: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagQueue(ctx, &sqs.UntagQueueInput{
				QueueUrl: aws.String(url),
				TagKeys:  remove,
			}); err != nil {
				return fmt.Errorf("untag queue: %w", err)
			}
			return nil
		},
	)
}

// region returns the region the client is configured for, used to decide
// whether a create that sends tags must retry without them on a partition that
// cannot tag a queue at create time.
func region(client *sqs.Client) string {
	return client.Options().Region
}

// isQueueNotFound reports whether err is the SQS error for a missing queue,
// under either the JSON-protocol code or the older query-protocol code.
func isQueueNotFound(err error) bool {
	return isNotFound(err, errCodeQueueDoesNotExist, errCodeQueueDoesNotExistNew)
}

// isQueueDeletedRecently reports whether err is the SQS error returned when a
// queue is created with the name of a queue deleted within the last sixty
// seconds. The name frees up once that window passes, so a caller retries.
func isQueueDeletedRecently(err error) bool {
	return isNotFound(err, errCodeQueueDeletedRecently)
}
