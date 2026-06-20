package lambda

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// eventSourceMappingPropagationTimeout bounds a create or update that retries
// through a freshly created execution role: the role may not yet be assumable
// by Lambda or not yet granted its permissions, and either clears on its own
// within a few minutes.
const eventSourceMappingPropagationTimeout = 5 * time.Minute

// eventSourceMappingStateTimeout bounds the wait for a mapping to settle after a
// create or update, where it moves through the Creating, Enabling, Disabling, or
// Updating states before reaching Enabled or Disabled.
const eventSourceMappingStateTimeout = 10 * time.Minute

// eventSourceMappingDeleteTimeout bounds the delete: the retry through an
// in-use mapping and the wait for it to leave the Deleting state and disappear.
const eventSourceMappingDeleteTimeout = 5 * time.Minute

// The states an event source mapping reports. A mapping is created, then either
// enabled or disabled depending on the desired enabled value, passing through
// the transitional Creating, Enabling, Disabling, and Updating states. Delete
// moves it to Deleting before it disappears.
const (
	esmStateCreating  = "Creating"
	esmStateEnabling  = "Enabling"
	esmStateEnabled   = "Enabled"
	esmStateDisabling = "Disabling"
	esmStateDisabled  = "Disabled"
	esmStateUpdating  = "Updating"
	esmStateDeleting  = "Deleting"
)

// EventSourceMapping connects an event source -- a Kinesis or DynamoDB stream,
// an SQS queue, an Amazon MSK or self-managed Apache Kafka cluster, an Amazon MQ
// broker, or a DocumentDB change stream -- to a Lambda function, the way
// CloudFormation models AWS::Lambda::EventSourceMapping. Lambda assigns the
// mapping a uuid that is its only handle; there is no ARN-based addressing for
// get, update, or delete. The mapping moves through a state machine, so create,
// update, and delete each wait for the state to settle, and the enabled value is
// reconciled into the Enabling and Disabling states rather than a separate
// enable or disable call. The event source, the starting position, the
// self-managed source endpoints, the Kafka source configs, and the queue and
// topic lists are fixed at creation; every other input reconciles in place.
//
// Logging configuration is not modeled, matching the CloudFormation resource the
// investigation covered.
type EventSourceMapping struct {
	FunctionName                        string                                        `ub:"function-name"`
	Enabled                             *bool                                         `ub:"enabled"`
	EventSourceArn                      *string                                       `ub:"event-source-arn"`
	BatchSize                           *int64                                        `ub:"batch-size"`
	BisectBatchOnFunctionError          *bool                                         `ub:"bisect-batch-on-function-error"`
	MaximumBatchingWindowInSeconds      *int64                                        `ub:"maximum-batching-window-in-seconds"`
	MaximumRecordAgeInSeconds           *int64                                        `ub:"maximum-record-age-in-seconds"`
	MaximumRetryAttempts                *int64                                        `ub:"maximum-retry-attempts"`
	ParallelizationFactor               *int64                                        `ub:"parallelization-factor"`
	TumblingWindowInSeconds             *int64                                        `ub:"tumbling-window-in-seconds"`
	KMSKeyArn                           *string                                       `ub:"kms-key-arn"`
	StartingPosition                    *string                                       `ub:"starting-position"`
	StartingPositionTimestamp           *string                                       `ub:"starting-position-timestamp"`
	FunctionResponseTypes               []string                                      `ub:"function-response-types"`
	Queues                              []string                                      `ub:"queues"`
	Topics                              []string                                      `ub:"topics"`
	FilterCriteria                      *EventSourceMappingFilterCriteria             `ub:"filter-criteria"`
	DestinationConfig                   *EventSourceMappingDestinationConfig          `ub:"destination-config"`
	ScalingConfig                       *EventSourceMappingScalingConfig              `ub:"scaling-config"`
	MetricsConfig                       *EventSourceMappingMetricsConfig              `ub:"metrics-config"`
	ProvisionedPollerConfig             *EventSourceMappingProvisionedPollerConfig    `ub:"provisioned-poller-config"`
	DocumentDBEventSourceConfig         *EventSourceMappingDocumentDBConfig           `ub:"document-db-event-source-config"`
	LoggingConfig                       *EventSourceMappingLoggingConfig              `ub:"logging-config"`
	SelfManagedEventSource              *EventSourceMappingSelfManagedEventSource     `ub:"self-managed-event-source"`
	AmazonManagedKafkaEventSourceConfig *EventSourceMappingAmazonManagedKafka         `ub:"amazon-managed-kafka-event-source-config"`
	SelfManagedKafkaEventSourceConfig   *EventSourceMappingSelfManagedKafka           `ub:"self-managed-kafka-event-source-config"`
	SourceAccessConfigurations          []EventSourceMappingSourceAccessConfiguration `ub:"source-access-configurations"`
	Tags                                map[string]string                             `ub:"tags"`
}

// EventSourceMappingOutput holds the values Lambda computes for a mapping. Uuid
// is the identity handle that keys every later get, update, and delete. Arn is
// the mapping's own ARN; FunctionArn is the fully resolved function ARN, which
// may differ from the function-name input. State is the settled state-machine
// value the enabled input derives from on a read, and StateTransitionReason,
// LastProcessingResult, and LastModified report the mapping's recent activity.
type EventSourceMappingOutput struct {
	Uuid                  string `ub:"uuid"`
	Arn                   string `ub:"arn"`
	FunctionArn           string `ub:"function-arn"`
	State                 string `ub:"state"`
	StateTransitionReason string `ub:"state-transition-reason"`
	LastProcessingResult  string `ub:"last-processing-result"`
	LastModified          string `ub:"last-modified"`
}

func (r *EventSourceMapping) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs Lambda fixes when a mapping is created. The
// event source and the self-managed source endpoints identify what the mapping
// reads, the starting position is set once, the Kafka source configs hold a
// consumer group id that cannot be changed, and the queue and topic lists name
// the destination once. A change to any of them requires a new mapping; every
// other input reconciles in place by Update.
func (r *EventSourceMapping) ReplaceFields() []string {
	return []string{
		"event-source-arn",
		"self-managed-event-source",
		"amazon-managed-kafka-event-source-config",
		"self-managed-kafka-event-source-config",
		"starting-position",
		"starting-position-timestamp",
		"queues",
		"topics",
	}
}

// Defaults marks the bare collection inputs a mapping may omit. The optional
// nested collections inside the config blocks are reached through pointer
// blocks, which is what makes them omittable to the type checker.
func (r EventSourceMapping) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.FunctionResponseTypes),
		defaults.Optional(r.Queues),
		defaults.Optional(r.Topics),
		defaults.Optional(r.SourceAccessConfigurations),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules Lambda places on a mapping's inputs. The source
// is identified by exactly one of an event-source ARN or a self-managed source.
// The Kafka source configs pair with their matching source: the Amazon MSK
// config goes with the event-source ARN and cannot combine with a self-managed
// source or its Kafka config, and the self-managed Kafka config goes with the
// self-managed source and cannot combine with an event-source ARN or the MSK
// config. The enum fields take only their valid values, and the numeric and
// item-count fields have bounds. The maximum record age is either the infinite
// sentinel of -1 or a value from 60 to 604800 seconds. Per-element rules on the
// metrics, source-access, function-response-type, and schema-registry lists
// derive through ForEach, reaching the lists nested in a block through its
// pointer. The filter pattern's byte length, the queue and topic element
// lengths, and the URI and consumer-group charsets are left to the Lambda API,
// since a byte-length or regex rule does not derive.
func (r EventSourceMapping) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.EventSourceArn, r.SelfManagedEventSource),
		constraint.ForbiddenWith(r.AmazonManagedKafkaEventSourceConfig,
			r.SelfManagedEventSource, r.SelfManagedKafkaEventSourceConfig),
		constraint.ForbiddenWith(r.SelfManagedKafkaEventSourceConfig,
			r.EventSourceArn, r.AmazonManagedKafkaEventSourceConfig),
		constraint.When(constraint.Present(r.StartingPosition)).
			Require(constraint.OneOf(r.StartingPosition,
				"TRIM_HORIZON", "LATEST", "AT_TIMESTAMP")).
			Message("starting-position must be TRIM_HORIZON, LATEST, or AT_TIMESTAMP"),
		constraint.When(constraint.Present(r.BatchSize)).
			Require(constraint.AtLeast(r.BatchSize, 1), constraint.AtMost(r.BatchSize, 10000)).
			Message("batch-size must be between 1 and 10000"),
		constraint.When(constraint.Present(r.MaximumRecordAgeInSeconds)).
			Require(constraint.Any(constraint.Equals(r.MaximumRecordAgeInSeconds, -1),
				constraint.All(constraint.AtLeast(r.MaximumRecordAgeInSeconds, 60),
					constraint.AtMost(r.MaximumRecordAgeInSeconds, 604800)))).
			Message("maximum-record-age-in-seconds must be -1 or between 60 and 604800"),
		constraint.When(constraint.Present(r.MaximumRetryAttempts)).
			Require(constraint.AtLeast(r.MaximumRetryAttempts, -1),
				constraint.AtMost(r.MaximumRetryAttempts, 10000)).
			Message("maximum-retry-attempts must be between -1 and 10000"),
		constraint.When(constraint.Present(r.ParallelizationFactor)).
			Require(constraint.AtLeast(r.ParallelizationFactor, 1),
				constraint.AtMost(r.ParallelizationFactor, 10)).
			Message("parallelization-factor must be between 1 and 10"),
		constraint.When(constraint.Present(r.TumblingWindowInSeconds)).
			Require(constraint.AtLeast(r.TumblingWindowInSeconds, 0),
				constraint.AtMost(r.TumblingWindowInSeconds, 900)).
			Message("tumbling-window-in-seconds must be between 0 and 900"),
		constraint.When(constraint.Present(r.MaximumBatchingWindowInSeconds)).
			Require(constraint.AtLeast(r.MaximumBatchingWindowInSeconds, 0),
				constraint.AtMost(r.MaximumBatchingWindowInSeconds, 300)).
			Message("maximum-batching-window-in-seconds must be between 0 and 300"),
		constraint.When(constraint.Present(r.ScalingConfig)).
			Require(constraint.AtLeast(r.ScalingConfig.MaximumConcurrency, 2),
				constraint.AtMost(r.ScalingConfig.MaximumConcurrency, 1000)).
			Message("scaling-config maximum-concurrency must be between 2 and 1000"),
		constraint.When(constraint.Present(r.ProvisionedPollerConfig.MaximumPollers)).
			Require(constraint.AtLeast(r.ProvisionedPollerConfig.MaximumPollers, 1),
				constraint.AtMost(r.ProvisionedPollerConfig.MaximumPollers, 2000)).
			Message("provisioned-poller-config maximum-pollers must be between 1 and 2000"),
		constraint.When(constraint.Present(r.ProvisionedPollerConfig.MinimumPollers)).
			Require(constraint.AtLeast(r.ProvisionedPollerConfig.MinimumPollers, 1),
				constraint.AtMost(r.ProvisionedPollerConfig.MinimumPollers, 200)).
			Message("provisioned-poller-config minimum-pollers must be between 1 and 200"),
		constraint.When(constraint.Present(r.DocumentDBEventSourceConfig.FullDocument)).
			Require(constraint.OneOf(r.DocumentDBEventSourceConfig.FullDocument,
				"UpdateLookup", "Default")).
			Message("document-db-event-source-config full-document must be UpdateLookup or Default"),
		constraint.When(constraint.Present(r.LoggingConfig.SystemLogLevel)).
			Require(constraint.OneOf(r.LoggingConfig.SystemLogLevel,
				"DEBUG", "INFO", "WARN")).
			Message("logging-config system-log-level must be DEBUG, INFO, or WARN"),
		constraint.When(constraint.Present(
			r.AmazonManagedKafkaEventSourceConfig.SchemaRegistryConfig.EventRecordFormat)).
			Require(constraint.OneOf(
				r.AmazonManagedKafkaEventSourceConfig.SchemaRegistryConfig.EventRecordFormat,
				"JSON", "SOURCE")).
			Message("amazon-managed-kafka schema-registry event-record-format must be JSON or SOURCE"),
		constraint.When(constraint.Present(
			r.SelfManagedKafkaEventSourceConfig.SchemaRegistryConfig.EventRecordFormat)).
			Require(constraint.OneOf(
				r.SelfManagedKafkaEventSourceConfig.SchemaRegistryConfig.EventRecordFormat,
				"JSON", "SOURCE")).
			Message("self-managed-kafka schema-registry event-record-format must be JSON or SOURCE"),
		constraint.ForEach(r.FunctionResponseTypes, func(t string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.Equals(t, "ReportBatchItemFailures")).
					Message("function-response-types values must be ReportBatchItemFailures"),
			}
		}),
		constraint.ForEach(r.MetricsConfig.Metrics, func(m string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(m, "EventCount", "ErrorCount", "KafkaMetrics")).
					Message("a metrics-config metric must be EventCount, ErrorCount, or KafkaMetrics"),
			}
		}),
		constraint.ForEach(r.SourceAccessConfigurations,
			func(c EventSourceMappingSourceAccessConfiguration) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(c.Type)).
						Require(constraint.OneOf(c.Type, "BASIC_AUTH", "VPC_SUBNET",
							"VPC_SECURITY_GROUP", "SASL_SCRAM_512_AUTH", "SASL_SCRAM_256_AUTH",
							"VIRTUAL_HOST", "CLIENT_CERTIFICATE_TLS_AUTH",
							"SERVER_ROOT_CA_CERTIFICATE")).
						Message("a source-access-configuration type must be a valid auth or VPC type"),
				}
			}),
		constraint.ForEach(
			r.AmazonManagedKafkaEventSourceConfig.SchemaRegistryConfig.AccessConfigs,
			func(a EventSourceMappingKafkaSchemaAccessConfig) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(a.Type)).
						Require(constraint.OneOf(a.Type, "BASIC_AUTH",
							"CLIENT_CERTIFICATE_TLS_AUTH", "SERVER_ROOT_CA_CERTIFICATE")).
						Message("a schema-registry access-config type must be a valid auth type"),
				}
			}),
		constraint.ForEach(
			r.AmazonManagedKafkaEventSourceConfig.SchemaRegistryConfig.SchemaValidationConfigs,
			func(v EventSourceMappingKafkaSchemaValidation) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(v.Attribute)).
						Require(constraint.OneOf(v.Attribute, "KEY", "VALUE")).
						Message("a schema-registry validation attribute must be KEY or VALUE"),
				}
			}),
		constraint.ForEach(
			r.SelfManagedKafkaEventSourceConfig.SchemaRegistryConfig.AccessConfigs,
			func(a EventSourceMappingKafkaSchemaAccessConfig) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(a.Type)).
						Require(constraint.OneOf(a.Type, "BASIC_AUTH",
							"CLIENT_CERTIFICATE_TLS_AUTH", "SERVER_ROOT_CA_CERTIFICATE")).
						Message("a schema-registry access-config type must be a valid auth type"),
				}
			}),
		constraint.ForEach(
			r.SelfManagedKafkaEventSourceConfig.SchemaRegistryConfig.SchemaValidationConfigs,
			func(v EventSourceMappingKafkaSchemaValidation) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(v.Attribute)).
						Require(constraint.OneOf(v.Attribute, "KEY", "VALUE")).
						Message("a schema-registry validation attribute must be KEY or VALUE"),
				}
			}),
	}
}

func (r *EventSourceMapping) Create(
	ctx context.Context, cfg *awsCfg,
) (*EventSourceMappingOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in, err := r.createInput()
	if err != nil {
		return nil, err
	}
	uuid, err := r.create(ctx, client, in)
	// Some partitions, such as the ISO partitions, cannot tag a mapping as it is
	// created. When the tagged create fails for that reason, create the mapping
	// without tags; there is no separate tag call here, matching how the mapping
	// sends tags only on its create.
	if err != nil && len(r.Tags) > 0 && partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		uuid, err = r.create(ctx, client, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create event source mapping: %w", err)
	}
	// CreateEventSourceMapping returns while the mapping is still creating and
	// has not reached its settled enabled or disabled state. Wait for it to
	// settle so the outputs, the function ARN, and the computed defaults come
	// from the settled record rather than the unsettled create response.
	if err := r.waitCreated(ctx, client, uuid); err != nil {
		return nil, err
	}
	return r.read(ctx, client, uuid)
}

func (r *EventSourceMapping) Read(
	ctx context.Context, cfg *awsCfg, prior *EventSourceMappingOutput,
) (*EventSourceMappingOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Uuid)
}

func (r *EventSourceMapping) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[EventSourceMapping, *EventSourceMappingOutput],
) (*EventSourceMappingOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	uuid := prior.Outputs.Uuid
	// When none of the updatable fields differ there is nothing to send and the
	// mapping is already reconciled, so the same config applied twice makes no
	// write. The replace fields are handled by a replacement, not here.
	if !r.updateChanged(prior.Inputs) {
		return r.read(ctx, client, uuid)
	}
	in := r.updateInput(uuid, prior.Inputs)
	// An update is rejected while the function's execution role is still
	// settling, the same propagation window a create can hit, so retry through it.
	err = retry.OnError(ctx, isEventSourceMappingPropagation, func(ctx context.Context) error {
		_, err := client.UpdateEventSourceMapping(ctx, in)
		return err
	}, retry.WithTimeout(eventSourceMappingPropagationTimeout))
	if err != nil {
		return nil, fmt.Errorf("update event source mapping %s: %w", uuid, err)
	}
	if err := r.waitUpdated(ctx, client, uuid); err != nil {
		return nil, err
	}
	return r.read(ctx, client, uuid)
}

func (r *EventSourceMapping) Delete(
	ctx context.Context, cfg *awsCfg, prior *EventSourceMappingOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	uuid := prior.Uuid
	// A mapping cannot be deleted while it is still in use by an in-progress
	// operation; Lambda reports this as a resource-in-use conflict that clears on
	// its own, so retry through it.
	err = retry.OnError(ctx, isResourceInUse, func(ctx context.Context) error {
		_, err := client.DeleteEventSourceMapping(ctx, &lambda.DeleteEventSourceMappingInput{
			UUID: aws.String(uuid),
		})
		return err
	}, retry.WithTimeout(eventSourceMappingDeleteTimeout))
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete event source mapping %s: %w", uuid, err)
	}
	// Delete is eventually consistent: the mapping stays in the Deleting state
	// and remains gettable for a moment after the call returns. Wait until a get
	// reports it gone.
	return r.waitDeleted(ctx, client, uuid)
}

// create issues a single CreateEventSourceMapping, retrying through the
// execution-role propagation window. It returns the assigned uuid.
func (r *EventSourceMapping) create(
	ctx context.Context, client *lambda.Client, in *lambda.CreateEventSourceMappingInput,
) (string, error) {
	var uuid string
	err := retry.OnError(ctx, isEventSourceMappingPropagation, func(ctx context.Context) error {
		resp, err := client.CreateEventSourceMapping(ctx, in)
		if err != nil {
			return err
		}
		uuid = aws.ToString(resp.UUID)
		return nil
	}, retry.WithTimeout(eventSourceMappingPropagationTimeout))
	return uuid, err
}

// createInput builds the CreateEventSourceMapping request from the inputs,
// assembling every nested block and converting the typed enum and timestamp
// fields. A malformed starting-position timestamp is returned as an error before
// any call is made.
func (r *EventSourceMapping) createInput() (*lambda.CreateEventSourceMappingInput, error) {
	amk := amazonManagedKafkaConfig(r.AmazonManagedKafkaEventSourceConfig)
	smk := selfManagedKafkaConfig(r.SelfManagedKafkaEventSourceConfig)
	in := &lambda.CreateEventSourceMappingInput{
		FunctionName:                        aws.String(r.FunctionName),
		Enabled:                             r.Enabled,
		EventSourceArn:                      r.EventSourceArn,
		BatchSize:                           ptr.Int32(r.BatchSize),
		BisectBatchOnFunctionError:          r.BisectBatchOnFunctionError,
		MaximumBatchingWindowInSeconds:      ptr.Int32(r.MaximumBatchingWindowInSeconds),
		MaximumRecordAgeInSeconds:           ptr.Int32(r.MaximumRecordAgeInSeconds),
		MaximumRetryAttempts:                ptr.Int32(r.MaximumRetryAttempts),
		ParallelizationFactor:               ptr.Int32(r.ParallelizationFactor),
		TumblingWindowInSeconds:             ptr.Int32(r.TumblingWindowInSeconds),
		KMSKeyArn:                           r.KMSKeyArn,
		Queues:                              r.Queues,
		Topics:                              r.Topics,
		FilterCriteria:                      r.FilterCriteria.to(),
		DestinationConfig:                   r.DestinationConfig.to(),
		ScalingConfig:                       r.ScalingConfig.to(),
		MetricsConfig:                       r.MetricsConfig.to(),
		ProvisionedPollerConfig:             r.ProvisionedPollerConfig.to(),
		DocumentDBEventSourceConfig:         r.DocumentDBEventSourceConfig.to(),
		LoggingConfig:                       r.LoggingConfig.to(),
		SelfManagedEventSource:              r.SelfManagedEventSource.to(),
		AmazonManagedKafkaEventSourceConfig: amk,
		SelfManagedKafkaEventSourceConfig:   smk,
		FunctionResponseTypes:               functionResponseTypes(r.FunctionResponseTypes),
		SourceAccessConfigurations:          sourceAccessConfigurations(r.SourceAccessConfigurations),
		Tags:                                r.Tags,
	}
	if r.StartingPosition != nil {
		in.StartingPosition = lambdatypes.EventSourcePosition(*r.StartingPosition)
	}
	ts, err := startingPositionTimestamp(r.StartingPositionTimestamp)
	if err != nil {
		return nil, err
	}
	in.StartingPositionTimestamp = ts
	return in, nil
}

// updateInput builds the UpdateEventSourceMapping request, setting only the
// members whose inputs differ from the prior ones so the same config applied
// twice sends no write. A removed filter-criteria, metrics, provisioned-poller,
// or scaling config is sent as the empty SDK struct so Lambda clears it; a nil
// member would leave the live value unchanged. The source-access configurations
// are sent only when non-empty, since Lambda does not clear them with a
// sentinel.
func (r *EventSourceMapping) updateInput(
	uuid string, prior EventSourceMapping,
) *lambda.UpdateEventSourceMappingInput {
	in := &lambda.UpdateEventSourceMappingInput{UUID: aws.String(uuid)}
	if runtime.Changed(prior.FunctionName, r.FunctionName) {
		in.FunctionName = aws.String(r.FunctionName)
	}
	if runtime.Changed(prior.Enabled, r.Enabled) {
		in.Enabled = r.Enabled
	}
	if runtime.Changed(prior.BatchSize, r.BatchSize) {
		in.BatchSize = ptr.Int32(r.BatchSize)
	}
	if runtime.Changed(prior.BisectBatchOnFunctionError, r.BisectBatchOnFunctionError) {
		in.BisectBatchOnFunctionError = r.BisectBatchOnFunctionError
	}
	if runtime.Changed(prior.MaximumBatchingWindowInSeconds, r.MaximumBatchingWindowInSeconds) {
		in.MaximumBatchingWindowInSeconds = ptr.Int32(r.MaximumBatchingWindowInSeconds)
	}
	if runtime.Changed(prior.MaximumRecordAgeInSeconds, r.MaximumRecordAgeInSeconds) {
		in.MaximumRecordAgeInSeconds = ptr.Int32(r.MaximumRecordAgeInSeconds)
	}
	if runtime.Changed(prior.MaximumRetryAttempts, r.MaximumRetryAttempts) {
		in.MaximumRetryAttempts = ptr.Int32(r.MaximumRetryAttempts)
	}
	if runtime.Changed(prior.ParallelizationFactor, r.ParallelizationFactor) {
		in.ParallelizationFactor = ptr.Int32(r.ParallelizationFactor)
	}
	if runtime.Changed(prior.TumblingWindowInSeconds, r.TumblingWindowInSeconds) {
		in.TumblingWindowInSeconds = ptr.Int32(r.TumblingWindowInSeconds)
	}
	if runtime.Changed(prior.KMSKeyArn, r.KMSKeyArn) {
		in.KMSKeyArn = r.KMSKeyArn
	}
	if runtime.Changed(prior.FunctionResponseTypes, r.FunctionResponseTypes) {
		in.FunctionResponseTypes = functionResponseTypes(r.FunctionResponseTypes)
	}
	if runtime.Changed(prior.DestinationConfig, r.DestinationConfig) {
		in.DestinationConfig = r.DestinationConfig.to()
	}
	if runtime.Changed(prior.DocumentDBEventSourceConfig, r.DocumentDBEventSourceConfig) {
		in.DocumentDBEventSourceConfig = r.DocumentDBEventSourceConfig.to()
	}
	if runtime.Changed(prior.LoggingConfig, r.LoggingConfig) {
		in.LoggingConfig = r.LoggingConfig.to()
	}
	if runtime.Changed(prior.FilterCriteria, r.FilterCriteria) {
		in.FilterCriteria = r.FilterCriteria.to()
		if in.FilterCriteria == nil {
			in.FilterCriteria = &lambdatypes.FilterCriteria{}
		}
	}
	if runtime.Changed(prior.MetricsConfig, r.MetricsConfig) {
		in.MetricsConfig = r.MetricsConfig.to()
		if in.MetricsConfig == nil {
			in.MetricsConfig = &lambdatypes.EventSourceMappingMetricsConfig{}
		}
	}
	if runtime.Changed(prior.ProvisionedPollerConfig, r.ProvisionedPollerConfig) {
		in.ProvisionedPollerConfig = r.ProvisionedPollerConfig.to()
		if in.ProvisionedPollerConfig == nil {
			in.ProvisionedPollerConfig = &lambdatypes.ProvisionedPollerConfig{}
		}
	}
	if runtime.Changed(prior.ScalingConfig, r.ScalingConfig) {
		in.ScalingConfig = r.ScalingConfig.to()
		if in.ScalingConfig == nil {
			in.ScalingConfig = &lambdatypes.ScalingConfig{}
		}
	}
	if runtime.Changed(prior.SourceAccessConfigurations, r.SourceAccessConfigurations) {
		in.SourceAccessConfigurations = sourceAccessConfigurations(r.SourceAccessConfigurations)
	}
	return in
}

// updateChanged reports whether any input UpdateEventSourceMapping sends differs
// from the prior inputs. Comparing the inputs directly, rather than the built
// request, catches a list or block emptied to nil, which an update still needs
// to reconcile.
func (r *EventSourceMapping) updateChanged(prior EventSourceMapping) bool {
	return runtime.Changed(prior.FunctionName, r.FunctionName) ||
		runtime.Changed(prior.Enabled, r.Enabled) ||
		runtime.Changed(prior.BatchSize, r.BatchSize) ||
		runtime.Changed(prior.BisectBatchOnFunctionError, r.BisectBatchOnFunctionError) ||
		runtime.Changed(prior.MaximumBatchingWindowInSeconds, r.MaximumBatchingWindowInSeconds) ||
		runtime.Changed(prior.MaximumRecordAgeInSeconds, r.MaximumRecordAgeInSeconds) ||
		runtime.Changed(prior.MaximumRetryAttempts, r.MaximumRetryAttempts) ||
		runtime.Changed(prior.ParallelizationFactor, r.ParallelizationFactor) ||
		runtime.Changed(prior.TumblingWindowInSeconds, r.TumblingWindowInSeconds) ||
		runtime.Changed(prior.KMSKeyArn, r.KMSKeyArn) ||
		runtime.Changed(prior.FunctionResponseTypes, r.FunctionResponseTypes) ||
		runtime.Changed(prior.DestinationConfig, r.DestinationConfig) ||
		runtime.Changed(prior.DocumentDBEventSourceConfig, r.DocumentDBEventSourceConfig) ||
		runtime.Changed(prior.LoggingConfig, r.LoggingConfig) ||
		runtime.Changed(prior.FilterCriteria, r.FilterCriteria) ||
		runtime.Changed(prior.MetricsConfig, r.MetricsConfig) ||
		runtime.Changed(prior.ProvisionedPollerConfig, r.ProvisionedPollerConfig) ||
		runtime.Changed(prior.ScalingConfig, r.ScalingConfig) ||
		runtime.Changed(prior.SourceAccessConfigurations, r.SourceAccessConfigurations)
}

// read fetches the mapping by its uuid and composes its outputs. A mapping
// Lambda reports as gone maps to runtime.ErrNotFound so a plan recreates it. The
// enabled input is not echoed; it is reconciled into the state on write and
// derived from the state on a read, so it does not appear in the output.
func (r *EventSourceMapping) read(
	ctx context.Context, client *lambda.Client, uuid string,
) (*EventSourceMappingOutput, error) {
	resp, err := client.GetEventSourceMapping(ctx, &lambda.GetEventSourceMappingInput{
		UUID: aws.String(uuid),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get event source mapping %s: %w", uuid, err)
	}
	if resp == nil || resp.UUID == nil {
		return nil, runtime.ErrNotFound
	}
	out := &EventSourceMappingOutput{
		Uuid:                  aws.ToString(resp.UUID),
		Arn:                   aws.ToString(resp.EventSourceMappingArn),
		FunctionArn:           aws.ToString(resp.FunctionArn),
		State:                 aws.ToString(resp.State),
		StateTransitionReason: aws.ToString(resp.StateTransitionReason),
		LastProcessingResult:  aws.ToString(resp.LastProcessingResult),
	}
	if resp.LastModified != nil {
		out.LastModified = resp.LastModified.Format(time.RFC3339)
	}
	return out, nil
}

// waitCreated polls until the mapping leaves the transitional create states and
// reaches a settled enabled or disabled state. A mapping that disappears while
// settling is unexpected and stops the wait with a not-found error.
func (r *EventSourceMapping) waitCreated(
	ctx context.Context, client *lambda.Client, uuid string,
) error {
	return wait.Until(ctx, fmt.Sprintf("event source mapping %s to settle", uuid),
		r.settledProbe(client, uuid, esmStateCreating, esmStateEnabling, esmStateDisabling),
		wait.WithInterval(time.Second),
		wait.WithTimeout(eventSourceMappingStateTimeout),
	)
}

// waitUpdated polls until the mapping leaves the transitional update states and
// reaches a settled enabled or disabled state.
func (r *EventSourceMapping) waitUpdated(
	ctx context.Context, client *lambda.Client, uuid string,
) error {
	return wait.Until(ctx, fmt.Sprintf("event source mapping %s update", uuid),
		r.settledProbe(client, uuid, esmStateDisabling, esmStateEnabling, esmStateUpdating),
		wait.WithInterval(time.Second),
		wait.WithTimeout(eventSourceMappingStateTimeout),
	)
}

// settledProbe builds a wait probe that reports ready when the mapping reaches
// the settled Enabled or Disabled state and not ready while it is in one of the
// given transitional states. A get that reports the mapping gone aborts the
// wait, since a mapping being created or updated should not vanish.
func (r *EventSourceMapping) settledProbe(
	client *lambda.Client, uuid string, pending ...string,
) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		resp, err := client.GetEventSourceMapping(ctx, &lambda.GetEventSourceMappingInput{
			UUID: aws.String(uuid),
		})
		if err != nil {
			if isNotFound(err) {
				return false, fmt.Errorf("event source mapping %s disappeared", uuid)
			}
			return false, fmt.Errorf("get event source mapping %s: %w", uuid, err)
		}
		state := aws.ToString(resp.State)
		switch state {
		case esmStateEnabled, esmStateDisabled:
			return true, nil
		}
		if slices.Contains(pending, state) {
			return false, nil
		}
		// An unexpected state, such as a failed transition, is not one the wait
		// should spin through; report it rather than timing out.
		return false, fmt.Errorf("event source mapping %s in unexpected state %q", uuid, state)
	}
}

// waitDeleted polls until a get reports the mapping gone. While it is still in
// the Deleting state the get succeeds, so that is treated as not-yet-gone.
func (r *EventSourceMapping) waitDeleted(
	ctx context.Context, client *lambda.Client, uuid string,
) error {
	return wait.Until(ctx, fmt.Sprintf("event source mapping %s to be gone", uuid),
		func(ctx context.Context) (bool, error) {
			_, err := client.GetEventSourceMapping(ctx, &lambda.GetEventSourceMappingInput{
				UUID: aws.String(uuid),
			})
			if err != nil {
				if isNotFound(err) {
					return true, nil
				}
				return false, fmt.Errorf("get event source mapping %s: %w", uuid, err)
			}
			return false, nil
		},
		wait.WithInterval(time.Second),
		wait.WithTimeout(eventSourceMappingDeleteTimeout),
	)
}

// startingPositionTimestamp parses the optional RFC3339 starting-position
// timestamp into the SDK time value, leaving it unset when no value is given.
func startingPositionTimestamp(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil, fmt.Errorf("parse starting-position-timestamp %q: %w", *s, err)
	}
	return &t, nil
}

// functionResponseTypes converts the desired response-type names to the SDK
// list. An empty input leaves the member unset.
func functionResponseTypes(names []string) []lambdatypes.FunctionResponseType {
	if len(names) == 0 {
		return nil
	}
	out := make([]lambdatypes.FunctionResponseType, 0, len(names))
	for _, n := range names {
		out = append(out, lambdatypes.FunctionResponseType(n))
	}
	return out
}

// eventSourceMappingRetryMessages are the substrings Lambda puts in an
// InvalidParameterValueException that mark a freshly created execution role that
// has not yet propagated, rather than a real validation failure. A create or
// update is retried through one of these and fails at once on any other invalid
// parameter.
var eventSourceMappingRetryMessages = []string{
	"cannot be assumed by Lambda",
	"execution role does not have permissions",
	"ensure the role can perform",
}

// isEventSourceMappingPropagation reports whether err is the invalid-parameter
// error Lambda returns while the function's execution role is still settling. A
// plain invalid-parameter error with no recognized message is a real failure and
// is not retried.
func isEventSourceMappingPropagation(err error) bool {
	var invalid *lambdatypes.InvalidParameterValueException
	if !errors.As(err, &invalid) {
		return false
	}
	msg := invalid.ErrorMessage()
	for _, m := range eventSourceMappingRetryMessages {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// isResourceInUse reports whether err is Lambda's ResourceInUse error, which a
// delete hits while the mapping is still in use by an in-progress operation. It
// clears on its own, so the delete is retried.
func isResourceInUse(err error) bool {
	var inUse *lambdatypes.ResourceInUseException
	return errors.As(err, &inUse)
}
