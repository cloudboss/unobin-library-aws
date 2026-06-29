package eventbridge

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	eventbridge "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

var (
	eventBusPartnerSourcePattern = regexp.MustCompile(
		`^aws\.partner(/[.\-_A-Za-z0-9]+){2,}$`)
	eventBusARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	eventBusARNRegionPattern    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	eventBusARNAccountPattern   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|` +
			`partner-managed|\d{12}|cw.{10})$`)
)

// EventBus is an EventBridge custom or partner event bus. Name fixes the bus'
// identity and EventSourceName binds a partner bus to its source, so both are
// replace fields. Description, dead-letter queue, KMS key, and logging settings
// update in place through UpdateEventBus, while tags reconcile through the tag
// APIs addressed by the bus ARN. Length, ARN, and partner-source pattern rules
// run in validate because they need character counts or regular expressions.
type EventBus struct {
	Name             string                    `ub:"name"`
	DeadLetterConfig *EventBusDeadLetterConfig `ub:"dead-letter-config"`
	Description      *string                   `ub:"description"`
	EventSourceName  *string                   `ub:"event-source-name"`
	KmsKeyIdentifier *string                   `ub:"kms-key-identifier"`
	LogConfig        *EventBusLogConfig        `ub:"log-config"`
	Tags             *map[string]string        `ub:"tags"`
}

// EventBusOutput holds the ARN DescribeEventBus returns, plus the name handle
// needed to read or delete the prior bus during replacement.
type EventBusOutput struct {
	Arn  string `ub:"arn"`
	Name string `ub:"name"`
}

func (r *EventBus) SchemaVersion() int { return 1 }

// ReplaceFields lists the event bus inputs EventBridge fixes at creation. The
// name is the API identity, and a partner event source can only be matched when
// the bus is created.
func (r *EventBus) ReplaceFields() []string {
	return []string{"name", "event-source-name"}
}

// Constraints declares the enum and reserved-name rules the schema can express.
func (r EventBus) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.Name, "default")).
			Message("name cannot be default"),
		constraint.When(constraint.Present(r.LogConfig.IncludeDetail)).
			Require(constraint.OneOf(r.LogConfig.IncludeDetail, "NONE", "FULL")).
			Message("log-config.include-detail must be NONE or FULL"),
		constraint.When(constraint.Present(r.LogConfig.Level)).
			Require(constraint.OneOf(r.LogConfig.Level, "OFF", "ERROR", "INFO", "TRACE")).
			Message("log-config.level must be OFF, ERROR, INFO, or TRACE"),
	}
}

func (r *EventBus) Create(ctx context.Context, cfg *awsCfg) (*EventBusOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := r.createInput()
	resp, err := client.CreateEventBus(ctx, in)
	taggedSeparately := false
	if err != nil && len(in.Tags) > 0 && partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		resp, err = client.CreateEventBus(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create event bus: %w", err)
	}
	if taggedSeparately {
		busArn, err := r.createdArn(ctx, client, resp)
		if err != nil {
			return nil, err
		}
		if err := tagEventBus(ctx, client, busArn, eventBusUserTags(ptr.Value(r.Tags))); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, r.Name)
}

func (r *EventBus) Read(
	ctx context.Context, cfg *awsCfg, prior *EventBusOutput,
) (*EventBusOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, eventBusName(r.Name, prior))
}

func (r *EventBus) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[EventBus, *EventBusOutput],
) (*EventBusOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	name := eventBusName(r.Name, prior.Outputs)
	arn, err := r.priorArn(ctx, client, name, prior.Outputs)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	if r.eventBusChanged(prior.Inputs) {
		if _, err := client.UpdateEventBus(ctx, r.updateInput(name)); err != nil {
			return nil, fmt.Errorf("update event bus: %w", err)
		}
	}
	return r.read(ctx, client, name)
}

func (r *EventBus) Delete(ctx context.Context, cfg *awsCfg, prior *EventBusOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteEventBus(ctx, &eventbridge.DeleteEventBusInput{
		Name: aws.String(eventBusName(r.Name, prior)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete event bus: %w", err)
	}
	return nil
}

func (r *EventBus) createInput() *eventbridge.CreateEventBusInput {
	return &eventbridge.CreateEventBusInput{
		Name:             aws.String(r.Name),
		DeadLetterConfig: r.DeadLetterConfig.to(),
		Description:      r.Description,
		EventSourceName:  r.EventSourceName,
		KmsKeyIdentifier: r.KmsKeyIdentifier,
		LogConfig:        r.LogConfig.to(),
		Tags:             eventBusTags(eventBusUserTags(ptr.Value(r.Tags))),
	}
}

func (r *EventBus) updateInput(name string) *eventbridge.UpdateEventBusInput {
	in := &eventbridge.UpdateEventBusInput{
		Name:        aws.String(name),
		Description: eventBusDescription(r.Description),
	}
	if r.DeadLetterConfig != nil {
		in.DeadLetterConfig = r.DeadLetterConfig.to()
	}
	if r.KmsKeyIdentifier != nil && *r.KmsKeyIdentifier != "" {
		in.KmsKeyIdentifier = r.KmsKeyIdentifier
	}
	if r.LogConfig != nil {
		in.LogConfig = r.LogConfig.to()
	}
	return in
}

func (r *EventBus) read(
	ctx context.Context, client *eventbridge.Client, name string,
) (*EventBusOutput, error) {
	resp, err := client.DescribeEventBus(ctx, &eventbridge.DescribeEventBusInput{
		Name: aws.String(name),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe event bus: %w", err)
	}
	if emptyEventBus(resp) {
		return nil, runtime.ErrNotFound
	}
	out := eventBusOutput(resp)
	if out.Arn != "" {
		if _, err := readEventBusTags(ctx, client, out.Arn, true); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *EventBus) eventBusChanged(prior EventBus) bool {
	return runtime.Changed(prior.DeadLetterConfig, r.DeadLetterConfig) ||
		runtime.Changed(prior.Description, r.Description) ||
		runtime.Changed(prior.KmsKeyIdentifier, r.KmsKeyIdentifier) ||
		runtime.Changed(prior.LogConfig, r.LogConfig)
}

func (r *EventBus) createdArn(
	ctx context.Context, client *eventbridge.Client, resp *eventbridge.CreateEventBusOutput,
) (string, error) {
	if resp != nil && aws.ToString(resp.EventBusArn) != "" {
		return aws.ToString(resp.EventBusArn), nil
	}
	out, err := r.read(ctx, client, r.Name)
	if err != nil {
		return "", err
	}
	return out.Arn, nil
}

func (r *EventBus) priorArn(
	ctx context.Context,
	client *eventbridge.Client,
	name string,
	prior *EventBusOutput,
) (string, error) {
	if prior != nil && prior.Arn != "" {
		return prior.Arn, nil
	}
	out, err := r.read(ctx, client, name)
	if err != nil {
		return "", err
	}
	return out.Arn, nil
}

func (r *EventBus) syncTags(ctx context.Context, client *eventbridge.Client, arn string) error {
	return tagsync.Sync(ctx, eventBusUserTags(ptr.Value(r.Tags)),
		func(ctx context.Context) (map[string]string, error) {
			return readEventBusTags(ctx, client, arn, true)
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &eventbridge.TagResourceInput{
				ResourceARN: aws.String(arn),
				Tags:        eventBusTags(upsert),
			})
			if err != nil {
				if partition.UnsupportedOperation(region(client), err) {
					return nil
				}
				return fmt.Errorf("tag event bus: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &eventbridge.UntagResourceInput{
				ResourceARN: aws.String(arn),
				TagKeys:     remove,
			})
			if err != nil {
				if partition.UnsupportedOperation(region(client), err) {
					return nil
				}
				return fmt.Errorf("untag event bus: %w", err)
			}
			return nil
		},
	)
}

// validate checks the string length, ARN, and partner-source-name rules the
// constraint vocabulary cannot express.
func (r *EventBus) validate() error {
	if n := utf8.RuneCountInString(r.Name); n < 1 || n > 256 {
		return fmt.Errorf("name must be between 1 and 256 characters, got %d", n)
	}
	if r.Name == defaultEventBusName {
		return fmt.Errorf("name cannot be %q", defaultEventBusName)
	}
	if r.EventSourceName != nil {
		if n := utf8.RuneCountInString(*r.EventSourceName); n < 1 || n > 256 {
			return fmt.Errorf("event-source-name must be between 1 and 256 characters, got %d", n)
		}
		if !eventBusPartnerSourcePattern.MatchString(*r.EventSourceName) {
			return fmt.Errorf("event-source-name %q must match %s",
				*r.EventSourceName, eventBusPartnerSourcePattern.String())
		}
	}
	if r.Description != nil {
		if n := utf8.RuneCountInString(*r.Description); n > 512 {
			return fmt.Errorf("description must be at most 512 characters, got %d", n)
		}
	}
	if r.KmsKeyIdentifier != nil {
		if n := utf8.RuneCountInString(*r.KmsKeyIdentifier); n < 1 || n > 2048 {
			return fmt.Errorf("kms-key-identifier must be between 1 and 2048 characters, got %d", n)
		}
	}
	return r.DeadLetterConfig.validate()
}

// EventBusDeadLetterConfig names the SQS queue where EventBridge sends events
// it cannot deliver from the bus. Arn is optional, but when set it must be a
// valid generic ARN of 1 to 1600 characters; validate checks that pattern and length.
type EventBusDeadLetterConfig struct {
	Arn *string `ub:"arn"`
}

func (b *EventBusDeadLetterConfig) to() *eventbridgetypes.DeadLetterConfig {
	if b == nil {
		return nil
	}
	out := &eventbridgetypes.DeadLetterConfig{}
	if b.Arn != nil && *b.Arn != "" {
		out.Arn = b.Arn
	}
	return out
}

func (b *EventBusDeadLetterConfig) validate() error {
	if b == nil || b.Arn == nil {
		return nil
	}
	if n := utf8.RuneCountInString(*b.Arn); n < 1 || n > 1600 {
		return fmt.Errorf("dead-letter-config.arn must be between 1 and 1600 characters, got %d", n)
	}
	if !validEventBusARN(*b.Arn) {
		return fmt.Errorf("dead-letter-config.arn must be a valid ARN")
	}
	return nil
}

// EventBusLogConfig controls the records EventBridge writes for the bus. When a
// field is absent it is omitted from the API object; IncludeDetail is NONE or
// FULL, and Level is OFF, ERROR, INFO, or TRACE.
type EventBusLogConfig struct {
	IncludeDetail *string `ub:"include-detail"`
	Level         *string `ub:"level"`
}

func (b *EventBusLogConfig) to() *eventbridgetypes.LogConfig {
	if b == nil {
		return nil
	}
	out := &eventbridgetypes.LogConfig{}
	if b.IncludeDetail != nil && *b.IncludeDetail != "" {
		out.IncludeDetail = eventbridgetypes.IncludeDetail(*b.IncludeDetail)
	}
	if b.Level != nil && *b.Level != "" {
		out.Level = eventbridgetypes.Level(*b.Level)
	}
	return out
}

func eventBusName(current string, prior *EventBusOutput) string {
	if prior != nil && prior.Name != "" {
		return prior.Name
	}
	return current
}

func eventBusDescription(description *string) *string {
	if description == nil || *description == "" {
		return aws.String("")
	}
	return description
}

func eventBusOutput(resp *eventbridge.DescribeEventBusOutput) *EventBusOutput {
	return &EventBusOutput{
		Arn:  aws.ToString(resp.Arn),
		Name: aws.ToString(resp.Name),
	}
}

func emptyEventBus(resp *eventbridge.DescribeEventBusOutput) bool {
	return resp == nil || (aws.ToString(resp.Arn) == "" && aws.ToString(resp.Name) == "")
}

func readEventBusTags(
	ctx context.Context,
	client *eventbridge.Client,
	arn string,
	ignoreUnsupported bool,
) (map[string]string, error) {
	resp, err := client.ListTagsForResource(ctx, &eventbridge.ListTagsForResourceInput{
		ResourceARN: aws.String(arn),
	})
	if err != nil {
		if ignoreUnsupported && partition.UnsupportedOperation(region(client), err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("list event bus tags: %w", err)
	}
	return eventBusTagMap(resp.Tags), nil
}

func tagEventBus(
	ctx context.Context,
	client *eventbridge.Client,
	arn string,
	tags map[string]string,
) error {
	if len(tags) == 0 {
		return nil
	}
	_, err := client.TagResource(ctx, &eventbridge.TagResourceInput{
		ResourceARN: aws.String(arn),
		Tags:        eventBusTags(tags),
	})
	if err != nil {
		return fmt.Errorf("tag event bus: %w", err)
	}
	return nil
}

func eventBusTags(tags map[string]string) []eventbridgetypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]eventbridgetypes.Tag, 0, len(tags))
	for key, value := range tags {
		out = append(out, eventbridgetypes.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return out
}

func eventBusTagMap(tags []eventbridgetypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for i := range tags {
		out[aws.ToString(tags[i].Key)] = aws.ToString(tags[i].Value)
	}
	return out
}

func eventBusUserTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validEventBusARN(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return false
	}
	if !eventBusARNPartitionPattern.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !eventBusARNRegionPattern.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" && !eventBusARNAccountPattern.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}
