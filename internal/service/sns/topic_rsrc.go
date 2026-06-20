package sns

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	sns "github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

// SNS models a topic as one name plus a bag of string attributes. CreateTopic
// takes only the name, the tags, and the two attributes that cannot be set
// after the fact (whether the topic is FIFO and, for a FIFO topic, its
// throughput scope); every other writable attribute is applied afterward by
// SetTopicAttributes, one attribute per call. These constants are the exact API
// attribute keys, which the resource sends as the AttributeName of each call.
// The HTTP and SQS feedback keys are upper-cased where the others are
// mixed-case, matching the API verbatim.
const (
	topicAttrFifoTopic                            = "FifoTopic"
	topicAttrFifoThroughputScope                  = "FifoThroughputScope"
	topicAttrDisplayName                          = "DisplayName"
	topicAttrPolicy                               = "Policy"
	topicAttrDeliveryPolicy                       = "DeliveryPolicy"
	topicAttrArchivePolicy                        = "ArchivePolicy"
	topicAttrContentBasedDeduplication            = "ContentBasedDeduplication"
	topicAttrKmsMasterKeyID                       = "KmsMasterKeyId"
	topicAttrSignatureVersion                     = "SignatureVersion"
	topicAttrTracingConfig                        = "TracingConfig"
	topicAttrHTTPSuccessFeedbackRoleArn           = "HTTPSuccessFeedbackRoleArn"
	topicAttrHTTPSuccessFeedbackSampleRate        = "HTTPSuccessFeedbackSampleRate"
	topicAttrHTTPFailureFeedbackRoleArn           = "HTTPFailureFeedbackRoleArn"
	topicAttrSQSSuccessFeedbackRoleArn            = "SQSSuccessFeedbackRoleArn"
	topicAttrSQSSuccessFeedbackSampleRate         = "SQSSuccessFeedbackSampleRate"
	topicAttrSQSFailureFeedbackRoleArn            = "SQSFailureFeedbackRoleArn"
	topicAttrApplicationSuccessFeedbackRoleArn    = "ApplicationSuccessFeedbackRoleArn"
	topicAttrApplicationSuccessFeedbackSampleRate = "ApplicationSuccessFeedbackSampleRate"
	topicAttrApplicationFailureFeedbackRoleArn    = "ApplicationFailureFeedbackRoleArn"
	topicAttrFirehoseSuccessFeedbackRoleArn       = "FirehoseSuccessFeedbackRoleArn"
	topicAttrFirehoseSuccessFeedbackSampleRate    = "FirehoseSuccessFeedbackSampleRate"
	topicAttrFirehoseFailureFeedbackRoleArn       = "FirehoseFailureFeedbackRoleArn"
	topicAttrLambdaSuccessFeedbackRoleArn         = "LambdaSuccessFeedbackRoleArn"
	topicAttrLambdaSuccessFeedbackSampleRate      = "LambdaSuccessFeedbackSampleRate"
	topicAttrLambdaFailureFeedbackRoleArn         = "LambdaFailureFeedbackRoleArn"

	// topicAttrOwner is read-only; GetTopicAttributes returns the owner account id
	// under this key.
	topicAttrOwner = "Owner"
)

// topicTrueString is the value SNS expects for a boolean attribute set to true. The
// attribute bag is all strings, so a true boolean is sent as this literal and a
// FIFO topic is recognized by FifoTopic == "true".
const topicTrueString = "true"

// topicNotFoundCode is the API error code SNS reports when a topic does not
// exist. The typed NotFoundException reaches isNotFound under this code, so a
// read or delete of a gone topic matches it.
const topicNotFoundCode = "NotFound"

// fifoNamePattern matches a valid FIFO topic name: up to 251 of the allowed
// characters followed by the required .fifo suffix. SNS appends nothing on its
// own, so a FIFO topic's name must already end with the suffix.
var fifoNamePattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,251}\.fifo$`)

// standardNamePattern matches a valid standard (non-FIFO) topic name: 1 to 256
// of the allowed characters with no .fifo suffix.
var standardNamePattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,256}$`)

// Topic is an SNS topic: a named publish/subscribe channel plus the attributes
// that govern delivery, encryption, access, and FIFO behavior. CreateTopic
// fixes the name and, for a FIFO topic, the FIFO flag; both are baked into the
// topic's ARN, so a change to either replaces the topic. Every other attribute
// is reconciled in place by Update through SetTopicAttributes.
//
// The name is validated in code rather than by a constraint, since the allowed
// pattern depends on whether the topic is FIFO: a FIFO name must match
// ^[0-9A-Za-z_-]{1,251}\.fifo$ and a standard name ^[0-9A-Za-z_-]{1,256}$.
type Topic struct {
	Name                                 string            `ub:"name"`
	FifoTopic                            *bool             `ub:"fifo-topic"`
	FifoThroughputScope                  *string           `ub:"fifo-throughput-scope"`
	ContentBasedDeduplication            *bool             `ub:"content-based-deduplication"`
	ArchivePolicy                        *string           `ub:"archive-policy"`
	DisplayName                          *string           `ub:"display-name"`
	Policy                               *string           `ub:"policy"`
	DeliveryPolicy                       *string           `ub:"delivery-policy"`
	KmsMasterKeyID                       *string           `ub:"kms-master-key-id"`
	SignatureVersion                     *string           `ub:"signature-version"`
	TracingConfig                        *string           `ub:"tracing-config"`
	HTTPSuccessFeedbackRoleArn           *string           `ub:"http-success-feedback-role-arn"`
	HTTPSuccessFeedbackSampleRate        *int64            `ub:"http-success-feedback-sample-rate"`
	HTTPFailureFeedbackRoleArn           *string           `ub:"http-failure-feedback-role-arn"`
	SQSSuccessFeedbackRoleArn            *string           `ub:"sqs-success-feedback-role-arn"`
	SQSSuccessFeedbackSampleRate         *int64            `ub:"sqs-success-feedback-sample-rate"`
	SQSFailureFeedbackRoleArn            *string           `ub:"sqs-failure-feedback-role-arn"`
	ApplicationSuccessFeedbackRoleArn    *string           `ub:"application-success-feedback-role-arn"`
	ApplicationSuccessFeedbackSampleRate *int64            `ub:"application-success-feedback-sample-rate"`
	ApplicationFailureFeedbackRoleArn    *string           `ub:"application-failure-feedback-role-arn"`
	FirehoseSuccessFeedbackRoleArn       *string           `ub:"firehose-success-feedback-role-arn"`
	FirehoseSuccessFeedbackSampleRate    *int64            `ub:"firehose-success-feedback-sample-rate"`
	FirehoseFailureFeedbackRoleArn       *string           `ub:"firehose-failure-feedback-role-arn"`
	LambdaSuccessFeedbackRoleArn         *string           `ub:"lambda-success-feedback-role-arn"`
	LambdaSuccessFeedbackSampleRate      *int64            `ub:"lambda-success-feedback-sample-rate"`
	LambdaFailureFeedbackRoleArn         *string           `ub:"lambda-failure-feedback-role-arn"`
	Tags                                 map[string]string `ub:"tags"`
}

// TopicOutput holds the values SNS computes for a topic. The ARN is the topic's
// stable handle and identity: subscriptions, access policies, and IAM
// statements all reference it, and Delete keys off it so a replace removes the
// old topic rather than orphaning it. The owner is the account id that holds
// the topic.
type TopicOutput struct {
	Arn   string `ub:"arn"`
	Owner string `ub:"owner"`
}

func (r *Topic) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs SNS fixes when a topic is created. The name and
// the FIFO flag are both encoded in the topic's ARN and cannot be changed
// afterward, so a change to either requires a new topic. Every other input is
// reconciled in place by Update.
func (r *Topic) ReplaceFields() []string {
	return []string{
		"name",
		"fifo-topic",
	}
}

// Defaults marks the collection inputs a topic may omit.
func (r Topic) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules SNS places on a topic's inputs. The archive
// policy, the FIFO throughput scope, and content-based deduplication apply only
// to FIFO topics, so each requires fifo-topic to be true. The throughput scope,
// tracing config, and signature version each accept a fixed set of values, and
// the five success-feedback sample rates are percentages between 0 and 100. The
// name pattern is checked in code rather than here because it depends on the
// FIFO flag.
func (r Topic) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.ArchivePolicy)).
			Require(constraint.IsTrue(r.FifoTopic)).
			Message("archive-policy requires fifo-topic to be true"),
		constraint.When(constraint.Present(r.FifoThroughputScope)).
			Require(constraint.IsTrue(r.FifoTopic)).
			Message("fifo-throughput-scope requires fifo-topic to be true"),
		constraint.When(constraint.IsTrue(r.ContentBasedDeduplication)).
			Require(constraint.IsTrue(r.FifoTopic)).
			Message("content-based-deduplication requires fifo-topic to be true"),
		constraint.When(constraint.Present(r.FifoThroughputScope)).
			Require(constraint.OneOf(r.FifoThroughputScope, "Topic", "MessageGroup")).
			Message("fifo-throughput-scope must be Topic or MessageGroup"),
		constraint.When(constraint.Present(r.TracingConfig)).
			Require(constraint.OneOf(r.TracingConfig, "Active", "PassThrough")).
			Message("tracing-config must be Active or PassThrough"),
		constraint.When(constraint.Present(r.SignatureVersion)).
			Require(constraint.OneOf(r.SignatureVersion, "1", "2")).
			Message("signature-version must be 1 or 2"),
		constraint.When(constraint.Present(r.HTTPSuccessFeedbackSampleRate)).
			Require(constraint.AtLeast(r.HTTPSuccessFeedbackSampleRate, 0),
				constraint.AtMost(r.HTTPSuccessFeedbackSampleRate, 100)).
			Message("http-success-feedback-sample-rate must be between 0 and 100"),
		constraint.When(constraint.Present(r.SQSSuccessFeedbackSampleRate)).
			Require(constraint.AtLeast(r.SQSSuccessFeedbackSampleRate, 0),
				constraint.AtMost(r.SQSSuccessFeedbackSampleRate, 100)).
			Message("sqs-success-feedback-sample-rate must be between 0 and 100"),
		constraint.When(constraint.Present(r.ApplicationSuccessFeedbackSampleRate)).
			Require(constraint.AtLeast(r.ApplicationSuccessFeedbackSampleRate, 0),
				constraint.AtMost(r.ApplicationSuccessFeedbackSampleRate, 100)).
			Message("application-success-feedback-sample-rate must be between 0 and 100"),
		constraint.When(constraint.Present(r.FirehoseSuccessFeedbackSampleRate)).
			Require(constraint.AtLeast(r.FirehoseSuccessFeedbackSampleRate, 0),
				constraint.AtMost(r.FirehoseSuccessFeedbackSampleRate, 100)).
			Message("firehose-success-feedback-sample-rate must be between 0 and 100"),
		constraint.When(constraint.Present(r.LambdaSuccessFeedbackSampleRate)).
			Require(constraint.AtLeast(r.LambdaSuccessFeedbackSampleRate, 0),
				constraint.AtMost(r.LambdaSuccessFeedbackSampleRate, 100)).
			Message("lambda-success-feedback-sample-rate must be between 0 and 100"),
	}
}

func (r *Topic) Create(ctx context.Context, cfg *awsCfg) (*TopicOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validateName(); err != nil {
		return nil, err
	}
	in := &sns.CreateTopicInput{
		Name:       aws.String(r.Name),
		Attributes: r.createAttributes(),
		Tags:       topicTags(r.Tags),
	}
	// Some partitions, such as the ISO partitions, cannot tag a topic as it is
	// created. When the tagged create fails for that reason, create the topic
	// without tags and apply them with a separate call below.
	taggedSeparately := false
	resp, err := client.CreateTopic(ctx, in)
	if err != nil && in.Tags != nil &&
		partition.UnsupportedOperation(topicRegion(client), err) {
		in.Tags = nil
		taggedSeparately = true
		resp, err = client.CreateTopic(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create topic: %w", err)
	}
	topicArn := aws.ToString(resp.TopicArn)
	// CreateTopic accepts only the FIFO attributes, so every other attribute is
	// applied here, one SetTopicAttributes call apiece.
	if err := r.putAttributes(ctx, client, topicArn, r.followOnAttributes()); err != nil {
		return nil, err
	}
	if taggedSeparately && len(r.Tags) > 0 {
		if err := r.syncTags(ctx, client, topicArn); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, topicArn)
}

func (r *Topic) Read(ctx context.Context, cfg *awsCfg, prior *TopicOutput) (*TopicOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Arn)
}

// read fetches the topic's attributes and returns its computed outputs. A topic
// that has gone missing is drift: GetTopicAttributes reports it as a
// NotFoundException, and a topic that returns no attributes is likewise gone, so
// either maps to runtime.ErrNotFound and a plan recreates it.
func (r *Topic) read(
	ctx context.Context, client *sns.Client, topicArn string,
) (*TopicOutput, error) {
	resp, err := client.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{
		TopicArn: aws.String(topicArn),
	})
	if err != nil {
		if isNotFound(err, topicNotFoundCode) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get topic attributes: %w", err)
	}
	if len(resp.Attributes) == 0 {
		return nil, runtime.ErrNotFound
	}
	// The ARN queried is the topic's identity, so it is returned as-is rather
	// than read back from the attribute map; the owner account id comes from the
	// attributes.
	return &TopicOutput{
		Arn:   topicArn,
		Owner: resp.Attributes[topicAttrOwner],
	}, nil
}

func (r *Topic) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Topic, *TopicOutput],
) (*TopicOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	topicArn := prior.Outputs.Arn
	// Each attribute is reconciled only when it changed, so an apply that
	// touches one attribute does not replay the rest and reset a value the
	// config omits. A tag-only change is left to the tag reconcile below.
	changed := r.changedAttributes(prior.Inputs)
	if len(changed) > 0 {
		if err := r.putAttributes(ctx, client, topicArn, changed); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client, topicArn); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, topicArn)
}

func (r *Topic) Delete(ctx context.Context, cfg *awsCfg, prior *TopicOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteTopic(ctx, &sns.DeleteTopicInput{
		TopicArn: aws.String(prior.Arn),
	})
	if err != nil {
		// SNS deletes a topic idempotently: a topic already gone reports as a
		// NotFoundException, reaching isNotFound under the NotFound code, which
		// counts as deleted.
		if isNotFound(err, topicNotFoundCode) {
			return nil
		}
		return fmt.Errorf("delete topic: %w", err)
	}
	return nil
}

// validateName checks the topic name against the pattern SNS requires, which
// depends on whether the topic is FIFO. A FIFO topic's name must end with the
// .fifo suffix and is limited to 251 characters before it; a standard topic's
// name must not include the suffix and may run to 256 characters. SNS appends
// nothing itself, so the supplied name must already be in final form.
func (r *Topic) validateName() error {
	if aws.ToBool(r.FifoTopic) {
		if !fifoNamePattern.MatchString(r.Name) {
			return fmt.Errorf(
				"name %q must match %s for a fifo topic", r.Name, fifoNamePattern)
		}
		return nil
	}
	if !standardNamePattern.MatchString(r.Name) {
		return fmt.Errorf("name %q must match %s", r.Name, standardNamePattern)
	}
	return nil
}

// createAttributes builds the attribute map CreateTopic accepts, which is only
// the FIFO flag and, for a FIFO topic, its throughput scope. Both are immutable,
// so they ride the create rather than a follow-on call. A standard topic omits
// both and lets SNS apply its defaults.
func (r *Topic) createAttributes() map[string]string {
	attrs := map[string]string{}
	if aws.ToBool(r.FifoTopic) {
		attrs[topicAttrFifoTopic] = topicTrueString
		if r.FifoThroughputScope != nil {
			attrs[topicAttrFifoThroughputScope] = *r.FifoThroughputScope
		}
	}
	return attrs
}

// followOnAttributes returns every set attribute that CreateTopic does not
// accept, each to be applied by its own SetTopicAttributes call after the topic
// exists. An unset attribute is absent from the map, so SNS keeps its default
// rather than being reset. The FIFO throughput scope is omitted here because it
// rides the create; a later change to it is handled by changedAttributes.
func (r *Topic) followOnAttributes() map[string]string {
	attrs := map[string]string{}
	putString := func(key string, value *string) {
		if value != nil {
			attrs[key] = *value
		}
	}
	putBool := func(key string, value *bool) {
		if value != nil {
			attrs[key] = topicBoolString(*value)
		}
	}
	putRate := func(key string, value *int64) {
		if value != nil {
			attrs[key] = fmt.Sprintf("%d", *value)
		}
	}
	putString(topicAttrDisplayName, r.DisplayName)
	putString(topicAttrPolicy, r.Policy)
	putString(topicAttrDeliveryPolicy, r.DeliveryPolicy)
	putString(topicAttrArchivePolicy, r.ArchivePolicy)
	putBool(topicAttrContentBasedDeduplication, r.ContentBasedDeduplication)
	putString(topicAttrKmsMasterKeyID, r.KmsMasterKeyID)
	putString(topicAttrSignatureVersion, r.SignatureVersion)
	putString(topicAttrTracingConfig, r.TracingConfig)
	putString(topicAttrHTTPSuccessFeedbackRoleArn, r.HTTPSuccessFeedbackRoleArn)
	putRate(topicAttrHTTPSuccessFeedbackSampleRate, r.HTTPSuccessFeedbackSampleRate)
	putString(topicAttrHTTPFailureFeedbackRoleArn, r.HTTPFailureFeedbackRoleArn)
	putString(topicAttrSQSSuccessFeedbackRoleArn, r.SQSSuccessFeedbackRoleArn)
	putRate(topicAttrSQSSuccessFeedbackSampleRate, r.SQSSuccessFeedbackSampleRate)
	putString(topicAttrSQSFailureFeedbackRoleArn, r.SQSFailureFeedbackRoleArn)
	putString(topicAttrApplicationSuccessFeedbackRoleArn, r.ApplicationSuccessFeedbackRoleArn)
	putRate(topicAttrApplicationSuccessFeedbackSampleRate, r.ApplicationSuccessFeedbackSampleRate)
	putString(topicAttrApplicationFailureFeedbackRoleArn, r.ApplicationFailureFeedbackRoleArn)
	putString(topicAttrFirehoseSuccessFeedbackRoleArn, r.FirehoseSuccessFeedbackRoleArn)
	putRate(topicAttrFirehoseSuccessFeedbackSampleRate, r.FirehoseSuccessFeedbackSampleRate)
	putString(topicAttrFirehoseFailureFeedbackRoleArn, r.FirehoseFailureFeedbackRoleArn)
	putString(topicAttrLambdaSuccessFeedbackRoleArn, r.LambdaSuccessFeedbackRoleArn)
	putRate(topicAttrLambdaSuccessFeedbackSampleRate, r.LambdaSuccessFeedbackSampleRate)
	putString(topicAttrLambdaFailureFeedbackRoleArn, r.LambdaFailureFeedbackRoleArn)
	return attrs
}

// changedAttributes returns the attributes whose value differs from the prior
// inputs, so Update reconciles only what changed. The FIFO flag and the name
// are immutable and force a replace, so they are not tested here, but the FIFO
// throughput scope can change on an existing FIFO topic and is reconciled when
// it does.
func (r *Topic) changedAttributes(prior Topic) map[string]string {
	attrs := map[string]string{}
	addString := func(key string, before, after *string) {
		if after != nil && runtime.Changed(before, after) {
			attrs[key] = *after
		}
	}
	addBool := func(key string, before, after *bool) {
		if after != nil && runtime.Changed(before, after) {
			attrs[key] = topicBoolString(*after)
		}
	}
	addRate := func(key string, before, after *int64) {
		if after != nil && runtime.Changed(before, after) {
			attrs[key] = fmt.Sprintf("%d", *after)
		}
	}
	addString(topicAttrFifoThroughputScope, prior.FifoThroughputScope, r.FifoThroughputScope)
	addString(topicAttrDisplayName, prior.DisplayName, r.DisplayName)
	addString(topicAttrPolicy, prior.Policy, r.Policy)
	addString(topicAttrDeliveryPolicy, prior.DeliveryPolicy, r.DeliveryPolicy)
	addString(topicAttrArchivePolicy, prior.ArchivePolicy, r.ArchivePolicy)
	addBool(topicAttrContentBasedDeduplication,
		prior.ContentBasedDeduplication, r.ContentBasedDeduplication)
	addString(topicAttrKmsMasterKeyID, prior.KmsMasterKeyID, r.KmsMasterKeyID)
	addString(topicAttrSignatureVersion, prior.SignatureVersion, r.SignatureVersion)
	addString(topicAttrTracingConfig, prior.TracingConfig, r.TracingConfig)
	addString(topicAttrHTTPSuccessFeedbackRoleArn,
		prior.HTTPSuccessFeedbackRoleArn, r.HTTPSuccessFeedbackRoleArn)
	addRate(topicAttrHTTPSuccessFeedbackSampleRate,
		prior.HTTPSuccessFeedbackSampleRate, r.HTTPSuccessFeedbackSampleRate)
	addString(topicAttrHTTPFailureFeedbackRoleArn,
		prior.HTTPFailureFeedbackRoleArn, r.HTTPFailureFeedbackRoleArn)
	addString(topicAttrSQSSuccessFeedbackRoleArn,
		prior.SQSSuccessFeedbackRoleArn, r.SQSSuccessFeedbackRoleArn)
	addRate(topicAttrSQSSuccessFeedbackSampleRate,
		prior.SQSSuccessFeedbackSampleRate, r.SQSSuccessFeedbackSampleRate)
	addString(topicAttrSQSFailureFeedbackRoleArn,
		prior.SQSFailureFeedbackRoleArn, r.SQSFailureFeedbackRoleArn)
	addString(topicAttrApplicationSuccessFeedbackRoleArn,
		prior.ApplicationSuccessFeedbackRoleArn, r.ApplicationSuccessFeedbackRoleArn)
	addRate(topicAttrApplicationSuccessFeedbackSampleRate,
		prior.ApplicationSuccessFeedbackSampleRate, r.ApplicationSuccessFeedbackSampleRate)
	addString(topicAttrApplicationFailureFeedbackRoleArn,
		prior.ApplicationFailureFeedbackRoleArn, r.ApplicationFailureFeedbackRoleArn)
	addString(topicAttrFirehoseSuccessFeedbackRoleArn,
		prior.FirehoseSuccessFeedbackRoleArn, r.FirehoseSuccessFeedbackRoleArn)
	addRate(topicAttrFirehoseSuccessFeedbackSampleRate,
		prior.FirehoseSuccessFeedbackSampleRate, r.FirehoseSuccessFeedbackSampleRate)
	addString(topicAttrFirehoseFailureFeedbackRoleArn,
		prior.FirehoseFailureFeedbackRoleArn, r.FirehoseFailureFeedbackRoleArn)
	addString(topicAttrLambdaSuccessFeedbackRoleArn,
		prior.LambdaSuccessFeedbackRoleArn, r.LambdaSuccessFeedbackRoleArn)
	addRate(topicAttrLambdaSuccessFeedbackSampleRate,
		prior.LambdaSuccessFeedbackSampleRate, r.LambdaSuccessFeedbackSampleRate)
	addString(topicAttrLambdaFailureFeedbackRoleArn,
		prior.LambdaFailureFeedbackRoleArn, r.LambdaFailureFeedbackRoleArn)
	return attrs
}

// putAttributes applies a set of attributes to a topic, one SetTopicAttributes
// call per attribute, ordered by key so the calls are deterministic.
func (r *Topic) putAttributes(
	ctx context.Context, client *sns.Client, topicArn string, attrs map[string]string,
) error {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := r.putAttribute(ctx, client, topicArn, k, attrs[k]); err != nil {
			return err
		}
	}
	return nil
}

// putAttribute sets one topic attribute, retrying through two windows that
// clear on their own. A feedback-role or policy attribute may name an IAM
// principal created moments earlier: until that principal propagates, SNS
// rejects the write either as an authorization failure (the caller is not yet
// allowed to pass the role) or as an invalid parameter (the referenced ARN is
// not yet resolvable). Both clear once IAM catches up, so the call retries over
// the default two-minute window.
func (r *Topic) putAttribute(
	ctx context.Context, client *sns.Client, topicArn, name, value string,
) error {
	in := &sns.SetTopicAttributesInput{
		TopicArn:       aws.String(topicArn),
		AttributeName:  aws.String(name),
		AttributeValue: aws.String(value),
	}
	err := retry.OnError(ctx, isTopicAttributeWriteRetryable, func(ctx context.Context) error {
		_, err := client.SetTopicAttributes(ctx, in)
		return err
	})
	if err != nil {
		return fmt.Errorf("set topic attribute %s: %w", name, err)
	}
	return nil
}

// syncTags reconciles the topic's tags with the desired set, reading the live
// tags through ListTagsForResource and writing changes with TagResource and
// UntagResource. SNS addresses a topic's tags by its ARN.
func (r *Topic) syncTags(ctx context.Context, client *sns.Client, topicArn string) error {
	return tagsync.Sync(ctx, r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx,
				&sns.ListTagsForResourceInput{ResourceArn: aws.String(topicArn)})
			if err != nil {
				return nil, fmt.Errorf("list tags for resource: %w", err)
			}
			current := map[string]string{}
			for _, t := range resp.Tags {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &sns.TagResourceInput{
				ResourceArn: aws.String(topicArn),
				Tags:        topicTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &sns.UntagResourceInput{
				ResourceArn: aws.String(topicArn),
				TagKeys:     remove,
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}

// isTopicAttributeWriteRetryable reports whether a SetTopicAttributes error is one
// that clears on its own while a referenced IAM principal propagates: an
// authorization error whose message names a still-missing identity-based
// permission, or an invalid-parameter error from an ARN that is not yet
// resolvable. Any other authorization error is a real permission problem and is
// not retried.
func isTopicAttributeWriteRetryable(err error) bool {
	var invalid *snstypes.InvalidParameterException
	if errors.As(err, &invalid) {
		return true
	}
	var auth *snstypes.AuthorizationErrorException
	if errors.As(err, &auth) {
		msg := auth.ErrorMessage()
		return strings.Contains(msg, "no identity-based policy allows") ||
			strings.Contains(msg, "is not authorized to perform")
	}
	return false
}

// topicBoolString renders a boolean as the lowercase string SNS expects in the
// attribute bag.
func topicBoolString(b bool) string {
	if b {
		return topicTrueString
	}
	return "false"
}

// topicRegion returns the region the client is configured for, used to decide
// whether a create that sends tags must retry without them on a partition that
// cannot tag a topic at create time.
func topicRegion(client *sns.Client) string {
	return client.Options().Region
}

// topicTags converts a desired tag map into the SNS SDK tag list, ordered by
// key so the request is deterministic.
func topicTags(tags map[string]string) []snstypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]snstypes.Tag, 0, len(tags))
	for _, k := range keys {
		out = append(out, snstypes.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return out
}
