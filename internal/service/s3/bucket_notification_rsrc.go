package s3

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	bucketNotificationTimeout = 2 * time.Minute

	bucketNotificationDirectoryBucketMessage = "NotificationConfiguration is not valid, " +
		"expected CreateBucketConfiguration"
)

var directoryBucketNamePattern = regexp.MustCompile(`.+--[a-z0-9-]+--x-s3\z`)

// BucketNotification manages the whole notification configuration of one S3
// bucket. S3 has no create call for this resource: Create and Update both send
// a complete PutBucketNotificationConfiguration request, and Delete sends the
// same request with an empty configuration. Destination ids are optional API
// fields; when omitted, the library sends stable generated ids and returns the
// ids read back from S3.
type BucketNotification struct {
	Bucket         string                              `ub:"bucket"`
	Eventbridge    *bool                               `ub:"eventbridge"`
	LambdaFunction *[]BucketNotificationLambdaFunction `ub:"lambda-function"`
	Queue          *[]BucketNotificationQueue          `ub:"queue"`
	Topic          *[]BucketNotificationTopic          `ub:"topic"`
}

type BucketNotificationLambdaFunction struct {
	Id                *string  `ub:"id"`
	LambdaFunctionArn *string  `ub:"lambda-function-arn"`
	Events            []string `ub:"events"`
	FilterPrefix      *string  `ub:"filter-prefix"`
	FilterSuffix      *string  `ub:"filter-suffix"`
}

type BucketNotificationQueue struct {
	Id           *string  `ub:"id"`
	QueueArn     string   `ub:"queue-arn"`
	Events       []string `ub:"events"`
	FilterPrefix *string  `ub:"filter-prefix"`
	FilterSuffix *string  `ub:"filter-suffix"`
}

type BucketNotificationTopic struct {
	Id           *string  `ub:"id"`
	TopicArn     string   `ub:"topic-arn"`
	Events       []string `ub:"events"`
	FilterPrefix *string  `ub:"filter-prefix"`
	FilterSuffix *string  `ub:"filter-suffix"`
}

// BucketNotificationOutput holds the bucket identity, EventBridge presence,
// and the destination values read back from S3. Destination summaries use output
// names that do not hide the destination input blocks.
type BucketNotificationOutput struct {
	Bucket                  string                            `ub:"bucket"`
	Eventbridge             bool                              `ub:"eventbridge"`
	LambdaFunctionSummaries []BucketNotificationLambdaSummary `ub:"lambda-function-observed-summaries"`
	QueueSummaries          []BucketNotificationQueueSummary  `ub:"queue-observed-summaries"`
	TopicSummaries          []BucketNotificationTopicSummary  `ub:"topic-observed-summaries"`
}

type BucketNotificationLambdaSummary struct {
	Id                string   `ub:"id"`
	LambdaFunctionArn string   `ub:"lambda-function-arn"`
	Events            []string `ub:"events"`
	FilterPrefix      string   `ub:"filter-prefix"`
	FilterSuffix      string   `ub:"filter-suffix"`
}

type BucketNotificationQueueSummary struct {
	Id           string   `ub:"id"`
	QueueArn     string   `ub:"queue-arn"`
	Events       []string `ub:"events"`
	FilterPrefix string   `ub:"filter-prefix"`
	FilterSuffix string   `ub:"filter-suffix"`
}

type BucketNotificationTopicSummary struct {
	Id           string   `ub:"id"`
	TopicArn     string   `ub:"topic-arn"`
	Events       []string `ub:"events"`
	FilterPrefix string   `ub:"filter-prefix"`
	FilterSuffix string   `ub:"filter-suffix"`
}

type bucketNotificationV2Output struct {
	Bucket                     string   `ub:"bucket"`
	Eventbridge                bool     `ub:"eventbridge"`
	LambdaFunctionEffectiveIds []string `ub:"lambda-function-effective-ids"`
	QueueEffectiveIds          []string `ub:"queue-effective-ids"`
	TopicEffectiveIds          []string `ub:"topic-effective-ids"`
}

type bucketNotificationV1Output struct {
	Bucket         string                                  `ub:"bucket"`
	LambdaFunction []bucketNotificationV1DestinationOutput `ub:"lambda-function"`
	Queue          []bucketNotificationV1DestinationOutput `ub:"queue"`
	Topic          []bucketNotificationV1DestinationOutput `ub:"topic"`
}

type bucketNotificationV1DestinationOutput struct {
	Id string `ub:"id"`
}

func (r *BucketNotification) SchemaVersion() int { return 3 }

func (r *BucketNotification) Migrate(
	oldVersion int, prior runtime.MigrationState,
) (runtime.MigrationState, error) {
	switch oldVersion {
	case 1:
		var old bucketNotificationV1Output
		if len(prior.Outputs) > 0 {
			if err := runtime.Decode(&old, prior.Outputs); err != nil {
				return runtime.MigrationState{}, fmt.Errorf(
					"migrate bucket notification v1 outputs: %w", err)
			}
		}
		prior.Outputs = map[string]any{
			"bucket":      old.Bucket,
			"eventbridge": false,
			"lambda-function-observed-summaries": bucketNotificationV1LambdaSummaries(
				old.LambdaFunction),
			"queue-observed-summaries": bucketNotificationV1QueueSummaries(old.Queue),
			"topic-observed-summaries": bucketNotificationV1TopicSummaries(old.Topic),
		}
	case 2:
		var old bucketNotificationV2Output
		if len(prior.Outputs) > 0 {
			if err := runtime.Decode(&old, prior.Outputs); err != nil {
				return runtime.MigrationState{}, fmt.Errorf(
					"migrate bucket notification v2 outputs: %w", err)
			}
		}
		prior.Outputs = map[string]any{
			"bucket":      old.Bucket,
			"eventbridge": old.Eventbridge,
			"lambda-function-observed-summaries": bucketNotificationLambdaSummariesFromIDs(
				old.LambdaFunctionEffectiveIds),
			"queue-observed-summaries": bucketNotificationQueueSummariesFromIDs(
				old.QueueEffectiveIds),
			"topic-observed-summaries": bucketNotificationTopicSummariesFromIDs(
				old.TopicEffectiveIds),
		}
	default:
		return runtime.MigrationState{}, fmt.Errorf(
			"unsupported bucket notification schema version %d", oldVersion)
	}
	return prior, nil
}

func (r *BucketNotification) ReplaceFields() []string {
	return []string{"bucket"}
}

func (r *BucketNotification) Create(
	ctx context.Context, cfg *awsCfg,
) (*BucketNotificationOutput, error) {
	client, err := newBucketNotificationClient(ctx, cfg, r.Bucket)
	if err != nil {
		return nil, err
	}
	desired, err := r.notificationConfiguration(bucketNotificationEffectiveIDSource{})
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client, r.Bucket, desired.notification); err != nil {
		return nil, err
	}
	if err := wait.Until(ctx, fmt.Sprintf("bucket notification %s", r.Bucket),
		func(ctx context.Context) (bool, error) {
			_, err := bucketNotificationFind(ctx, client, r.Bucket)
			if errors.Is(err, runtime.ErrNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return true, nil
		}, wait.WithTimeout(bucketNotificationTimeout)); err != nil {
		return nil, err
	}
	return r.read(ctx, client, r.Bucket)
}

func (r *BucketNotification) Read(
	ctx context.Context, cfg *awsCfg, prior *BucketNotificationOutput,
) (*BucketNotificationOutput, error) {
	client, err := newBucketNotificationClient(ctx, cfg, prior.Bucket)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Bucket)
}

func (r *BucketNotification) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[BucketNotification, *BucketNotificationOutput],
) (*BucketNotificationOutput, error) {
	bucket := prior.Outputs.Bucket
	client, err := newBucketNotificationClient(ctx, cfg, bucket)
	if err != nil {
		return nil, err
	}
	ids := bucketNotificationEffectiveIDSource{
		observed: prior.Observed,
		prior:    prior.Outputs,
	}
	desired, err := r.notificationConfiguration(ids)
	if err != nil {
		return nil, err
	}
	if r.needsPut(prior, desired) {
		if err := r.put(ctx, client, bucket, desired.notification); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, bucket)
}

func (r *BucketNotification) Delete(
	ctx context.Context, cfg *awsCfg, prior *BucketNotificationOutput,
) error {
	client, err := newBucketNotificationClient(ctx, cfg, prior.Bucket)
	if err != nil {
		return err
	}
	clear := func(ctx context.Context) error {
		_, err := client.PutBucketNotificationConfiguration(ctx,
			&s3.PutBucketNotificationConfigurationInput{
				Bucket:                    aws.String(prior.Bucket),
				NotificationConfiguration: &s3types.NotificationConfiguration{},
			})
		return err
	}
	err = retry.OnError(ctx, bucketNotificationDeleteRetryable, clear,
		retry.WithTimeout(bucketNotificationTimeout), retry.WithInterval(time.Second))
	if err != nil {
		if isNotFound(err, "NoSuchBucket") {
			return nil
		}
		if isBucketNotificationDirectoryBucketUnsupported(err) {
			return fmt.Errorf("bucket notifications are not supported for %s: %w",
				bucketNotificationBucketKind(prior.Bucket), err)
		}
		return fmt.Errorf("clear bucket notification configuration: %w", err)
	}
	return nil
}

func (r *BucketNotification) needsPut(
	prior runtime.Prior[BucketNotification, *BucketNotificationOutput],
	desired bucketNotificationDesired,
) bool {
	return r.changed(prior.Inputs) || r.observedDrifted(prior.Observed, desired)
}

func (r *BucketNotification) changed(prior BucketNotification) bool {
	return runtime.Changed(prior.Eventbridge, r.Eventbridge) ||
		runtime.Changed(ptr.Value(prior.LambdaFunction), ptr.Value(r.LambdaFunction)) ||
		runtime.Changed(ptr.Value(prior.Queue), ptr.Value(r.Queue)) ||
		runtime.Changed(ptr.Value(prior.Topic), ptr.Value(r.Topic))
}

func (r *BucketNotification) observedDrifted(
	observed *BucketNotificationOutput, desired bucketNotificationDesired,
) bool {
	if observed == nil {
		return false
	}
	return aws.ToBool(r.Eventbridge) != observed.Eventbridge ||
		!bucketNotificationLambdaSummariesEqual(
			desired.lambdaFunctionSummaries, observed.LambdaFunctionSummaries,
		) ||
		!bucketNotificationQueueSummariesEqual(
			desired.queueSummaries, observed.QueueSummaries,
		) ||
		!bucketNotificationTopicSummariesEqual(
			desired.topicSummaries, observed.TopicSummaries,
		)
}

func bucketNotificationLambdaSummariesEqual(
	a, b []BucketNotificationLambdaSummary,
) bool {
	return (len(a) == 0 && len(b) == 0) || reflect.DeepEqual(a, b)
}

func bucketNotificationQueueSummariesEqual(a, b []BucketNotificationQueueSummary) bool {
	return (len(a) == 0 && len(b) == 0) || reflect.DeepEqual(a, b)
}

func bucketNotificationTopicSummariesEqual(a, b []BucketNotificationTopicSummary) bool {
	return (len(a) == 0 && len(b) == 0) || reflect.DeepEqual(a, b)
}

func (r *BucketNotification) put(
	ctx context.Context, client *s3.Client, bucket string,
	notification *s3types.NotificationConfiguration,
) error {
	in := &s3.PutBucketNotificationConfigurationInput{
		Bucket:                    aws.String(bucket),
		NotificationConfiguration: notification,
	}
	put := func(ctx context.Context) error {
		_, err := client.PutBucketNotificationConfiguration(ctx, in)
		return err
	}
	err := retry.OnError(ctx, bucketNotificationRetryable, put,
		retry.WithTimeout(bucketNotificationTimeout))
	if err != nil {
		if isBucketNotificationDirectoryBucketUnsupported(err) {
			return fmt.Errorf("bucket notifications are not supported for %s: %w",
				bucketNotificationBucketKind(bucket), err)
		}
		return fmt.Errorf("put bucket notification configuration: %w", err)
	}
	return nil
}

func (r *BucketNotification) read(
	ctx context.Context, client *s3.Client, bucket string,
) (*BucketNotificationOutput, error) {
	resp, err := bucketNotificationFind(ctx, client, bucket)
	if err != nil {
		return nil, err
	}
	return bucketNotificationOutput(bucket, resp), nil
}

type bucketNotificationDesired struct {
	notification            *s3types.NotificationConfiguration
	lambdaFunctionSummaries []BucketNotificationLambdaSummary
	queueSummaries          []BucketNotificationQueueSummary
	topicSummaries          []BucketNotificationTopicSummary
}

func (r *BucketNotification) notificationConfiguration(
	ids bucketNotificationEffectiveIDSource,
) (bucketNotificationDesired, error) {
	desired := bucketNotificationDesired{
		notification: &s3types.NotificationConfiguration{},
	}
	if aws.ToBool(r.Eventbridge) {
		desired.notification.EventBridgeConfiguration = &s3types.EventBridgeConfiguration{}
	}
	lambdaFunctions, lambdaSummaries, err := r.lambdaFunctionConfigurations(ids)
	if err != nil {
		return bucketNotificationDesired{}, err
	}
	desired.notification.LambdaFunctionConfigurations = lambdaFunctions
	desired.lambdaFunctionSummaries = lambdaSummaries
	queues, queueSummaries, err := r.queueConfigurations(ids)
	if err != nil {
		return bucketNotificationDesired{}, err
	}
	desired.notification.QueueConfigurations = queues
	desired.queueSummaries = queueSummaries
	topics, topicSummaries, err := r.topicConfigurations(ids)
	if err != nil {
		return bucketNotificationDesired{}, err
	}
	desired.notification.TopicConfigurations = topics
	desired.topicSummaries = topicSummaries
	return desired, nil
}

func (r *BucketNotification) lambdaFunctionConfigurations(
	ids bucketNotificationEffectiveIDSource,
) ([]s3types.LambdaFunctionConfiguration, []BucketNotificationLambdaSummary, error) {
	if len(ptr.Value(r.LambdaFunction)) == 0 {
		return nil, []BucketNotificationLambdaSummary{}, nil
	}
	configs := make([]s3types.LambdaFunctionConfiguration, 0, len(ptr.Value(r.LambdaFunction)))
	summaries := make([]BucketNotificationLambdaSummary, 0, len(ptr.Value(r.LambdaFunction)))
	for i, item := range ptr.Value(r.LambdaFunction) {
		id, err := bucketNotificationDestinationID(item.Id,
			ids.lambdaFunction(i), "tf-s3-lambda-")
		if err != nil {
			return nil, nil, err
		}
		configs = append(configs, s3types.LambdaFunctionConfiguration{
			Id:                aws.String(id),
			LambdaFunctionArn: item.LambdaFunctionArn,
			Events:            bucketNotificationEvents(item.Events),
			Filter:            bucketNotificationFilter(item.FilterPrefix, item.FilterSuffix),
		})
		summaries = append(summaries, BucketNotificationLambdaSummary{
			Id:                id,
			LambdaFunctionArn: aws.ToString(item.LambdaFunctionArn),
			Events:            bucketNotificationNormalizeEvents(item.Events),
			FilterPrefix:      aws.ToString(item.FilterPrefix),
			FilterSuffix:      aws.ToString(item.FilterSuffix),
		})
	}
	return configs, summaries, nil
}

func (r *BucketNotification) queueConfigurations(
	ids bucketNotificationEffectiveIDSource,
) ([]s3types.QueueConfiguration, []BucketNotificationQueueSummary, error) {
	if len(ptr.Value(r.Queue)) == 0 {
		return nil, []BucketNotificationQueueSummary{}, nil
	}
	configs := make([]s3types.QueueConfiguration, 0, len(ptr.Value(r.Queue)))
	summaries := make([]BucketNotificationQueueSummary, 0, len(ptr.Value(r.Queue)))
	for i, item := range ptr.Value(r.Queue) {
		id, err := bucketNotificationDestinationID(item.Id,
			ids.queue(i), "tf-s3-queue-")
		if err != nil {
			return nil, nil, err
		}
		configs = append(configs, s3types.QueueConfiguration{
			Id:       aws.String(id),
			QueueArn: aws.String(item.QueueArn),
			Events:   bucketNotificationEvents(item.Events),
			Filter:   bucketNotificationFilter(item.FilterPrefix, item.FilterSuffix),
		})
		summaries = append(summaries, BucketNotificationQueueSummary{
			Id:           id,
			QueueArn:     item.QueueArn,
			Events:       bucketNotificationNormalizeEvents(item.Events),
			FilterPrefix: aws.ToString(item.FilterPrefix),
			FilterSuffix: aws.ToString(item.FilterSuffix),
		})
	}
	return configs, summaries, nil
}

func (r *BucketNotification) topicConfigurations(
	ids bucketNotificationEffectiveIDSource,
) ([]s3types.TopicConfiguration, []BucketNotificationTopicSummary, error) {
	if len(ptr.Value(r.Topic)) == 0 {
		return nil, []BucketNotificationTopicSummary{}, nil
	}
	configs := make([]s3types.TopicConfiguration, 0, len(ptr.Value(r.Topic)))
	summaries := make([]BucketNotificationTopicSummary, 0, len(ptr.Value(r.Topic)))
	for i, item := range ptr.Value(r.Topic) {
		id, err := bucketNotificationDestinationID(item.Id,
			ids.topic(i), "tf-s3-topic-")
		if err != nil {
			return nil, nil, err
		}
		configs = append(configs, s3types.TopicConfiguration{
			Id:       aws.String(id),
			TopicArn: aws.String(item.TopicArn),
			Events:   bucketNotificationEvents(item.Events),
			Filter:   bucketNotificationFilter(item.FilterPrefix, item.FilterSuffix),
		})
		summaries = append(summaries, BucketNotificationTopicSummary{
			Id:           id,
			TopicArn:     item.TopicArn,
			Events:       bucketNotificationNormalizeEvents(item.Events),
			FilterPrefix: aws.ToString(item.FilterPrefix),
			FilterSuffix: aws.ToString(item.FilterSuffix),
		})
	}
	return configs, summaries, nil
}

func bucketNotificationFind(
	ctx context.Context, client *s3.Client, bucket string,
) (*s3.GetBucketNotificationConfigurationOutput, error) {
	resp, err := client.GetBucketNotificationConfiguration(ctx,
		&s3.GetBucketNotificationConfigurationInput{Bucket: aws.String(bucket)})
	if err != nil {
		if isNotFound(err, "NoSuchBucket") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get bucket notification configuration: %w", err)
	}
	if bucketNotificationZero(resp) {
		return nil, runtime.ErrNotFound
	}
	return resp, nil
}

func bucketNotificationZero(resp *s3.GetBucketNotificationConfigurationOutput) bool {
	if resp == nil {
		return true
	}
	return reflect.ValueOf(*resp).IsZero()
}

func bucketNotificationOutput(
	bucket string, resp *s3.GetBucketNotificationConfigurationOutput,
) *BucketNotificationOutput {
	return &BucketNotificationOutput{
		Bucket:      bucket,
		Eventbridge: resp.EventBridgeConfiguration != nil,
		LambdaFunctionSummaries: bucketNotificationLambdaSummaries(
			resp.LambdaFunctionConfigurations),
		QueueSummaries: bucketNotificationQueueSummaries(resp.QueueConfigurations),
		TopicSummaries: bucketNotificationTopicSummaries(resp.TopicConfigurations),
	}
}

func bucketNotificationLambdaSummaries(
	configs []s3types.LambdaFunctionConfiguration,
) []BucketNotificationLambdaSummary {
	if len(configs) == 0 {
		return []BucketNotificationLambdaSummary{}
	}
	out := make([]BucketNotificationLambdaSummary, 0, len(configs))
	for _, cfg := range configs {
		prefix, suffix := bucketNotificationFilterValues(cfg.Filter)
		out = append(out, BucketNotificationLambdaSummary{
			Id:                aws.ToString(cfg.Id),
			LambdaFunctionArn: aws.ToString(cfg.LambdaFunctionArn),
			Events:            bucketNotificationEventStrings(cfg.Events),
			FilterPrefix:      prefix,
			FilterSuffix:      suffix,
		})
	}
	return out
}

func bucketNotificationQueueSummaries(
	configs []s3types.QueueConfiguration,
) []BucketNotificationQueueSummary {
	if len(configs) == 0 {
		return []BucketNotificationQueueSummary{}
	}
	out := make([]BucketNotificationQueueSummary, 0, len(configs))
	for _, cfg := range configs {
		prefix, suffix := bucketNotificationFilterValues(cfg.Filter)
		out = append(out, BucketNotificationQueueSummary{
			Id:           aws.ToString(cfg.Id),
			QueueArn:     aws.ToString(cfg.QueueArn),
			Events:       bucketNotificationEventStrings(cfg.Events),
			FilterPrefix: prefix,
			FilterSuffix: suffix,
		})
	}
	return out
}

func bucketNotificationTopicSummaries(
	configs []s3types.TopicConfiguration,
) []BucketNotificationTopicSummary {
	if len(configs) == 0 {
		return []BucketNotificationTopicSummary{}
	}
	out := make([]BucketNotificationTopicSummary, 0, len(configs))
	for _, cfg := range configs {
		prefix, suffix := bucketNotificationFilterValues(cfg.Filter)
		out = append(out, BucketNotificationTopicSummary{
			Id:           aws.ToString(cfg.Id),
			TopicArn:     aws.ToString(cfg.TopicArn),
			Events:       bucketNotificationEventStrings(cfg.Events),
			FilterPrefix: prefix,
			FilterSuffix: suffix,
		})
	}
	return out
}

func bucketNotificationFilter(
	prefix, suffix *string,
) *s3types.NotificationConfigurationFilter {
	rules := make([]s3types.FilterRule, 0, 2)
	if aws.ToString(prefix) != "" {
		rules = append(rules, s3types.FilterRule{
			Name:  s3types.FilterRuleNamePrefix,
			Value: prefix,
		})
	}
	if aws.ToString(suffix) != "" {
		rules = append(rules, s3types.FilterRule{
			Name:  s3types.FilterRuleNameSuffix,
			Value: suffix,
		})
	}
	if len(rules) == 0 {
		return nil
	}
	return &s3types.NotificationConfigurationFilter{
		Key: &s3types.S3KeyFilter{FilterRules: rules},
	}
}

func bucketNotificationFilterValues(
	filter *s3types.NotificationConfigurationFilter,
) (prefix, suffix string) {
	if filter == nil || filter.Key == nil {
		return "", ""
	}
	for _, rule := range filter.Key.FilterRules {
		switch strings.ToLower(string(rule.Name)) {
		case "prefix":
			prefix = aws.ToString(rule.Value)
		case "suffix":
			suffix = aws.ToString(rule.Value)
		}
	}
	return prefix, suffix
}

func bucketNotificationEvents(events []string) []s3types.Event {
	if len(events) == 0 {
		return nil
	}
	out := make([]s3types.Event, 0, len(events))
	for _, event := range events {
		if event == "" {
			continue
		}
		out = append(out, s3types.Event(event))
	}
	return out
}

func bucketNotificationEventStrings(events []s3types.Event) []string {
	if len(events) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, string(event))
	}
	return bucketNotificationNormalizeEvents(out)
}

func bucketNotificationNormalizeEvents(events []string) []string {
	if len(events) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(events))
	out := make([]string, 0, len(events))
	for _, event := range events {
		if event == "" {
			continue
		}
		if _, ok := seen[event]; ok {
			continue
		}
		seen[event] = struct{}{}
		out = append(out, event)
	}
	slices.Sort(out)
	return out
}

func bucketNotificationDestinationID(id *string, effectiveID, prefix string) (string, error) {
	if aws.ToString(id) != "" {
		return aws.ToString(id), nil
	}
	if effectiveID != "" {
		return effectiveID, nil
	}
	return bucketNotificationGeneratedID(prefix)
}

func bucketNotificationGeneratedID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate bucket notification destination id: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
}

type bucketNotificationEffectiveIDSource struct {
	observed *BucketNotificationOutput
	prior    *BucketNotificationOutput
}

func (s bucketNotificationEffectiveIDSource) lambdaFunction(idx int) string {
	if id := bucketNotificationLambdaIDAt(s.observed, idx); id != "" {
		return id
	}
	return bucketNotificationLambdaIDAt(s.prior, idx)
}

func (s bucketNotificationEffectiveIDSource) queue(idx int) string {
	if id := bucketNotificationQueueIDAt(s.observed, idx); id != "" {
		return id
	}
	return bucketNotificationQueueIDAt(s.prior, idx)
}

func (s bucketNotificationEffectiveIDSource) topic(idx int) string {
	if id := bucketNotificationTopicIDAt(s.observed, idx); id != "" {
		return id
	}
	return bucketNotificationTopicIDAt(s.prior, idx)
}

func bucketNotificationLambdaIDAt(out *BucketNotificationOutput, idx int) string {
	if out == nil || idx >= len(out.LambdaFunctionSummaries) {
		return ""
	}
	return out.LambdaFunctionSummaries[idx].Id
}

func bucketNotificationQueueIDAt(out *BucketNotificationOutput, idx int) string {
	if out == nil || idx >= len(out.QueueSummaries) {
		return ""
	}
	return out.QueueSummaries[idx].Id
}

func bucketNotificationTopicIDAt(out *BucketNotificationOutput, idx int) string {
	if out == nil || idx >= len(out.TopicSummaries) {
		return ""
	}
	return out.TopicSummaries[idx].Id
}

func bucketNotificationV1LambdaSummaries(
	ids []bucketNotificationV1DestinationOutput,
) []BucketNotificationLambdaSummary {
	if len(ids) == 0 {
		return []BucketNotificationLambdaSummary{}
	}
	out := make([]BucketNotificationLambdaSummary, 0, len(ids))
	for _, id := range ids {
		out = append(out, BucketNotificationLambdaSummary{Id: id.Id})
	}
	return out
}

func bucketNotificationV1QueueSummaries(
	ids []bucketNotificationV1DestinationOutput,
) []BucketNotificationQueueSummary {
	if len(ids) == 0 {
		return []BucketNotificationQueueSummary{}
	}
	out := make([]BucketNotificationQueueSummary, 0, len(ids))
	for _, id := range ids {
		out = append(out, BucketNotificationQueueSummary{Id: id.Id})
	}
	return out
}

func bucketNotificationV1TopicSummaries(
	ids []bucketNotificationV1DestinationOutput,
) []BucketNotificationTopicSummary {
	if len(ids) == 0 {
		return []BucketNotificationTopicSummary{}
	}
	out := make([]BucketNotificationTopicSummary, 0, len(ids))
	for _, id := range ids {
		out = append(out, BucketNotificationTopicSummary{Id: id.Id})
	}
	return out
}

func bucketNotificationLambdaSummariesFromIDs(ids []string) []BucketNotificationLambdaSummary {
	if len(ids) == 0 {
		return []BucketNotificationLambdaSummary{}
	}
	out := make([]BucketNotificationLambdaSummary, 0, len(ids))
	for _, id := range ids {
		out = append(out, BucketNotificationLambdaSummary{Id: id})
	}
	return out
}

func bucketNotificationQueueSummariesFromIDs(ids []string) []BucketNotificationQueueSummary {
	if len(ids) == 0 {
		return []BucketNotificationQueueSummary{}
	}
	out := make([]BucketNotificationQueueSummary, 0, len(ids))
	for _, id := range ids {
		out = append(out, BucketNotificationQueueSummary{Id: id})
	}
	return out
}

func bucketNotificationTopicSummariesFromIDs(ids []string) []BucketNotificationTopicSummary {
	if len(ids) == 0 {
		return []BucketNotificationTopicSummary{}
	}
	out := make([]BucketNotificationTopicSummary, 0, len(ids))
	for _, id := range ids {
		out = append(out, BucketNotificationTopicSummary{Id: id})
	}
	return out
}

type bucketNotificationClientKind int

const (
	bucketNotificationClientS3 bucketNotificationClientKind = iota
	bucketNotificationClientS3Express
)

func newBucketNotificationClient(
	ctx context.Context, cfg *awsCfg, bucket string,
) (*s3.Client, error) {
	if bucketNotificationClientKindFor(bucket) == bucketNotificationClientS3Express {
		return newDirectoryBucketClient(ctx, cfg)
	}
	return newClient(ctx, cfg)
}

func bucketNotificationClientKindFor(bucket string) bucketNotificationClientKind {
	if isDirectoryBucketName(bucket) {
		return bucketNotificationClientS3Express
	}
	return bucketNotificationClientS3
}

func bucketNotificationRetryable(err error) bool {
	return isNotFound(err, "NoSuchBucket")
}

func bucketNotificationDeleteRetryable(err error) bool {
	return isOperationAborted(err)
}

func isBucketNotificationDirectoryBucketUnsupported(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidArgument" &&
		strings.Contains(apiErr.ErrorMessage(), bucketNotificationDirectoryBucketMessage)
}

func bucketNotificationBucketKind(bucket string) string {
	if isDirectoryBucketName(bucket) {
		return "directory bucket " + bucket
	}
	return "bucket " + bucket
}

func isDirectoryBucketName(bucket string) bool {
	return directoryBucketNamePattern.MatchString(bucket)
}
