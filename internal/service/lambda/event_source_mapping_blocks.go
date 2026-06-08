package lambda

import (
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// The nested blocks below model the structured members an event source mapping
// accepts on CreateEventSourceMapping and UpdateEventSourceMapping. Each is a
// field on the request, converted to its SDK type and assembled into the input.
// A nil block leaves that member unset on create, so Lambda applies its default.
// On update, several removable blocks are sent as the EMPTY SDK struct rather
// than nil so Lambda clears them; that sentinel is built in the resource's
// Update, not here. The enum, range, and item-count rules on a block's fields
// are declared in EventSourceMapping's Constraints.

// EventSourceMappingFilterCriteria holds the event filtering patterns Lambda
// evaluates before invoking the function. A pattern is a JSON document in
// Lambda's filter syntax. The set holds up to twenty filters; an empty pattern
// is allowed.
type EventSourceMappingFilterCriteria struct {
	Filters []EventSourceMappingFilter `ub:"filters"`
}

// EventSourceMappingFilter is one filtering pattern within the filter criteria.
type EventSourceMappingFilter struct {
	Pattern *string `ub:"pattern"`
}

func (b *EventSourceMappingFilterCriteria) to() *lambdatypes.FilterCriteria {
	if b == nil {
		return nil
	}
	out := &lambdatypes.FilterCriteria{}
	if len(b.Filters) > 0 {
		out.Filters = make([]lambdatypes.Filter, 0, len(b.Filters))
		for _, f := range b.Filters {
			out.Filters = append(out.Filters, lambdatypes.Filter{Pattern: f.Pattern})
		}
	}
	return out
}

// EventSourceMappingDestinationConfig names where Lambda sends an event after
// processing. Only the on-failure destination is configurable here; Lambda does
// not accept an on-success destination on an event source mapping.
type EventSourceMappingDestinationConfig struct {
	OnFailure *EventSourceMappingOnFailure `ub:"on-failure"`
}

// EventSourceMappingOnFailure names the SNS topic, SQS queue, S3 bucket, or
// Kafka topic that receives records Lambda could not process.
type EventSourceMappingOnFailure struct {
	Destination *string `ub:"destination"`
}

func (b *EventSourceMappingDestinationConfig) to() *lambdatypes.DestinationConfig {
	if b == nil {
		return nil
	}
	out := &lambdatypes.DestinationConfig{}
	if b.OnFailure != nil {
		out.OnFailure = &lambdatypes.OnFailure{Destination: b.OnFailure.Destination}
	}
	return out
}

// EventSourceMappingScalingConfig limits how many concurrent instances an
// Amazon SQS event source can invoke.
type EventSourceMappingScalingConfig struct {
	MaximumConcurrency *int64 `ub:"maximum-concurrency"`
}

func (b *EventSourceMappingScalingConfig) to() *lambdatypes.ScalingConfig {
	if b == nil {
		return nil
	}
	return &lambdatypes.ScalingConfig{
		MaximumConcurrency: ptr.Int32(b.MaximumConcurrency),
	}
}

// EventSourceMappingMetricsConfig selects which metrics the event source
// mapping produces. The only value Lambda accepts on creation is EventCount.
type EventSourceMappingMetricsConfig struct {
	Metrics []string `ub:"metrics"`
}

func (b *EventSourceMappingMetricsConfig) to() *lambdatypes.EventSourceMappingMetricsConfig {
	if b == nil {
		return nil
	}
	out := &lambdatypes.EventSourceMappingMetricsConfig{}
	if len(b.Metrics) > 0 {
		out.Metrics = make([]lambdatypes.EventSourceMappingMetric, 0, len(b.Metrics))
		for _, m := range b.Metrics {
			out.Metrics = append(out.Metrics, lambdatypes.EventSourceMappingMetric(m))
		}
	}
	return out
}

// EventSourceMappingLoggingConfig sets the detail level of the system logs an
// Amazon MSK or self-managed Apache Kafka event source mapping emits.
// SystemLogLevel is DEBUG, INFO, or WARN, where DEBUG is the most detailed and
// WARN the least; Lambda sends logs at the selected level and lower.
type EventSourceMappingLoggingConfig struct {
	SystemLogLevel *string `ub:"system-log-level"`
}

func (b *EventSourceMappingLoggingConfig) to() *lambdatypes.EventSourceMappingLoggingConfig {
	if b == nil {
		return nil
	}
	out := &lambdatypes.EventSourceMappingLoggingConfig{}
	if b.SystemLogLevel != nil {
		out.SystemLogLevel = lambdatypes.EventSourceMappingSystemLogLevel(*b.SystemLogLevel)
	}
	return out
}

// EventSourceMappingProvisionedPollerConfig sets the minimum and maximum number
// of event pollers for an Amazon SQS, Amazon MSK, or self-managed Apache Kafka
// event source. PollerGroupName groups several mappings to share poller
// capacity within the event source's VPC.
type EventSourceMappingProvisionedPollerConfig struct {
	MinimumPollers  *int64  `ub:"minimum-pollers"`
	MaximumPollers  *int64  `ub:"maximum-pollers"`
	PollerGroupName *string `ub:"poller-group-name"`
}

func (b *EventSourceMappingProvisionedPollerConfig) to() *lambdatypes.ProvisionedPollerConfig {
	if b == nil {
		return nil
	}
	return &lambdatypes.ProvisionedPollerConfig{
		MinimumPollers:  ptr.Int32(b.MinimumPollers),
		MaximumPollers:  ptr.Int32(b.MaximumPollers),
		PollerGroupName: b.PollerGroupName,
	}
}

// EventSourceMappingDocumentDBConfig configures a DocumentDB change-stream event
// source: the database and optional collection to consume, and what DocumentDB
// sends on update operations. FullDocument is UpdateLookup to receive the whole
// document with each change, or Default to receive only the changed fields.
type EventSourceMappingDocumentDBConfig struct {
	DatabaseName   *string `ub:"database-name"`
	CollectionName *string `ub:"collection-name"`
	FullDocument   *string `ub:"full-document"`
}

func (b *EventSourceMappingDocumentDBConfig) to() *lambdatypes.DocumentDBEventSourceConfig {
	if b == nil {
		return nil
	}
	out := &lambdatypes.DocumentDBEventSourceConfig{
		DatabaseName:   b.DatabaseName,
		CollectionName: b.CollectionName,
	}
	if b.FullDocument != nil {
		out.FullDocument = lambdatypes.FullDocument(*b.FullDocument)
	}
	return out
}

// EventSourceMappingSelfManagedEventSource names the bootstrap servers of a
// self-managed Apache Kafka cluster. Endpoints maps the well-known key
// KAFKA_BOOTSTRAP_SERVERS to the broker host:port list. The block is fixed at
// creation.
type EventSourceMappingSelfManagedEventSource struct {
	Endpoints map[string][]string `ub:"endpoints"`
}

func (b *EventSourceMappingSelfManagedEventSource) to() *lambdatypes.SelfManagedEventSource {
	if b == nil {
		return nil
	}
	return &lambdatypes.SelfManagedEventSource{Endpoints: b.Endpoints}
}

// EventSourceMappingAmazonManagedKafka configures an Amazon MSK event
// source: the consumer group to join and an optional schema registry. The block
// is fixed at creation, since the consumer group id cannot be changed once set.
type EventSourceMappingAmazonManagedKafka struct {
	ConsumerGroupId      *string                                `ub:"consumer-group-id"`
	SchemaRegistryConfig *EventSourceMappingKafkaSchemaRegistry `ub:"schema-registry-config"`
}

func amazonManagedKafkaConfig(
	b *EventSourceMappingAmazonManagedKafka,
) *lambdatypes.AmazonManagedKafkaEventSourceConfig {
	if b == nil {
		return nil
	}
	return &lambdatypes.AmazonManagedKafkaEventSourceConfig{
		ConsumerGroupId:      b.ConsumerGroupId,
		SchemaRegistryConfig: b.SchemaRegistryConfig.to(),
	}
}

// EventSourceMappingSelfManagedKafka configures a self-managed Apache
// Kafka event source: the consumer group to join and an optional schema
// registry. The block is fixed at creation, since the consumer group id cannot
// be changed once set.
type EventSourceMappingSelfManagedKafka struct {
	ConsumerGroupId      *string                                `ub:"consumer-group-id"`
	SchemaRegistryConfig *EventSourceMappingKafkaSchemaRegistry `ub:"schema-registry-config"`
}

func selfManagedKafkaConfig(
	b *EventSourceMappingSelfManagedKafka,
) *lambdatypes.SelfManagedKafkaEventSourceConfig {
	if b == nil {
		return nil
	}
	return &lambdatypes.SelfManagedKafkaEventSourceConfig{
		ConsumerGroupId:      b.ConsumerGroupId,
		SchemaRegistryConfig: b.SchemaRegistryConfig.to(),
	}
}

// EventSourceMappingKafkaSchemaRegistry configures a Kafka schema registry both
// Kafka source configs share. SchemaRegistryUri is the Glue registry ARN or the
// Confluent registry URL, EventRecordFormat selects JSON or SOURCE delivery, and
// the access and validation configs tell Lambda how to authenticate and which
// message attributes to validate.
type EventSourceMappingKafkaSchemaRegistry struct {
	SchemaRegistryUri       *string                                     `ub:"schema-registry-uri"`
	EventRecordFormat       *string                                     `ub:"event-record-format"`
	AccessConfigs           []EventSourceMappingKafkaSchemaAccessConfig `ub:"access-configs"`
	SchemaValidationConfigs []EventSourceMappingKafkaSchemaValidation   `ub:"schema-validation-configs"`
}

// EventSourceMappingKafkaSchemaAccessConfig tells Lambda how to authenticate
// with a Confluent schema registry: the auth type and the Secrets Manager ARN
// holding the credentials. A Glue registry needs no access config.
type EventSourceMappingKafkaSchemaAccessConfig struct {
	Type *string `ub:"type"`
	URI  *string `ub:"uri"`
}

// EventSourceMappingKafkaSchemaValidation names a message attribute the schema
// registry validates and filters. Attribute is KEY or VALUE.
type EventSourceMappingKafkaSchemaValidation struct {
	Attribute *string `ub:"attribute"`
}

func (b *EventSourceMappingKafkaSchemaRegistry) to() *lambdatypes.KafkaSchemaRegistryConfig {
	if b == nil {
		return nil
	}
	out := &lambdatypes.KafkaSchemaRegistryConfig{
		SchemaRegistryURI: b.SchemaRegistryUri,
	}
	if b.EventRecordFormat != nil {
		out.EventRecordFormat = lambdatypes.SchemaRegistryEventRecordFormat(*b.EventRecordFormat)
	}
	if len(b.AccessConfigs) > 0 {
		out.AccessConfigs = make([]lambdatypes.KafkaSchemaRegistryAccessConfig, 0,
			len(b.AccessConfigs))
		for _, a := range b.AccessConfigs {
			ac := lambdatypes.KafkaSchemaRegistryAccessConfig{URI: a.URI}
			if a.Type != nil {
				ac.Type = lambdatypes.KafkaSchemaRegistryAuthType(*a.Type)
			}
			out.AccessConfigs = append(out.AccessConfigs, ac)
		}
	}
	if len(b.SchemaValidationConfigs) > 0 {
		out.SchemaValidationConfigs = make([]lambdatypes.KafkaSchemaValidationConfig, 0,
			len(b.SchemaValidationConfigs))
		for _, v := range b.SchemaValidationConfigs {
			vc := lambdatypes.KafkaSchemaValidationConfig{}
			if v.Attribute != nil {
				vc.Attribute = lambdatypes.KafkaSchemaValidationAttribute(*v.Attribute)
			}
			out.SchemaValidationConfigs = append(out.SchemaValidationConfigs, vc)
		}
	}
	return out
}

// EventSourceMappingSourceAccessConfiguration is one authentication protocol or
// VPC component that secures the event source. Type names the protocol or
// component, such as VPC_SUBNET or SASL_SCRAM_512_AUTH, and URI holds the value
// for that type, typically a Secrets Manager secret ARN or a VPC component id.
type EventSourceMappingSourceAccessConfiguration struct {
	Type *string `ub:"type"`
	URI  *string `ub:"uri"`
}

// sourceAccessConfigurations expands the desired source access configurations
// into the SDK list. An empty input leaves the member unset.
func sourceAccessConfigurations(
	in []EventSourceMappingSourceAccessConfiguration,
) []lambdatypes.SourceAccessConfiguration {
	if len(in) == 0 {
		return nil
	}
	out := make([]lambdatypes.SourceAccessConfiguration, 0, len(in))
	for _, c := range in {
		sac := lambdatypes.SourceAccessConfiguration{URI: c.URI}
		if c.Type != nil {
			sac.Type = lambdatypes.SourceAccessType(*c.Type)
		}
		out = append(out, sac)
	}
	return out
}
