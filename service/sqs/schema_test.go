package sqs

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/sqs"
)

// TestLibraryRegistersSqs checks the runtime registration: both SQS resources
// are present under Resources and dispatch to their output type.
func TestLibraryRegistersSqs(t *testing.T) {
	lib := Library()
	resources := map[string]reflect.Type{
		"queue":        reflect.TypeFor[*svc.QueueOutput](),
		"queue-policy": reflect.TypeFor[*svc.QueuePolicyOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestSqsSchemas asserts the whole derived TypeSchema for each SQS resource:
// input and output field types, the cross-field and enum constraints each
// Constraints method declares, and the optional defaults.
func TestSqsSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"queue": {
			Inputs: map[string]typecheck.Type{
				"content-based-deduplication":       typecheck.TOptional(typecheck.TBoolean()),
				"deduplication-scope":               typecheck.TOptional(typecheck.TString()),
				"delay-seconds":                     typecheck.TOptional(typecheck.TInteger()),
				"fifo-queue":                        typecheck.TOptional(typecheck.TBoolean()),
				"fifo-throughput-limit":             typecheck.TOptional(typecheck.TString()),
				"kms-data-key-reuse-period-seconds": typecheck.TOptional(typecheck.TInteger()),
				"kms-master-key-id":                 typecheck.TOptional(typecheck.TString()),
				"maximum-message-size":              typecheck.TOptional(typecheck.TInteger()),
				"message-retention-period":          typecheck.TOptional(typecheck.TInteger()),
				"name":                              typecheck.TString(),
				"policy":                            typecheck.TOptional(typecheck.TString()),
				"receive-message-wait-time-seconds": typecheck.TOptional(typecheck.TInteger()),
				"redrive-allow-policy":              typecheck.TOptional(typecheck.TString()),
				"redrive-policy":                    typecheck.TOptional(typecheck.TString()),
				"sqs-managed-sse-enabled":           typecheck.TOptional(typecheck.TBoolean()),
				"tags":                              typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"visibility-timeout":                typecheck.TOptional(typecheck.TInteger()),
			},
			Outputs: map[string]typecheck.Type{
				"arn": typecheck.TString(),
				"url": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "at-most-one-of",
					Fields: []string{"input.kms-master-key-id", "input.sqs-managed-sse-enabled"},
				},
				{
					Kind:    "predicate",
					When:    "(input.content-based-deduplication == true)",
					Require: "(input.fifo-queue == true)",
					Message: "content-based-deduplication requires fifo-queue to be true",
				},
				{
					Kind: "predicate",
					When: "(input.deduplication-scope != null)",
					Require: "(input.deduplication-scope == 'messageGroup' || " +
						"input.deduplication-scope == 'queue')",
					Message: "deduplication-scope must be messageGroup or queue",
				},
				{
					Kind: "predicate",
					When: "(input.fifo-throughput-limit != null)",
					Require: "(input.fifo-throughput-limit == 'perQueue' || " +
						"input.fifo-throughput-limit == 'perMessageGroupId')",
					Message: "fifo-throughput-limit must be perQueue or perMessageGroupId",
				},
				{
					Kind: "predicate",
					When: "(input.delay-seconds != null)",
					Require: "(input.delay-seconds == null || " +
						"input.delay-seconds >= 0) && " +
						"(input.delay-seconds == null || " +
						"input.delay-seconds <= 900)",
					Message: "delay-seconds must be between 0 and 900",
				},
				{
					Kind: "predicate",
					When: "(input.maximum-message-size != null)",
					Require: "(input.maximum-message-size == null || " +
						"input.maximum-message-size >= 1024) && " +
						"(input.maximum-message-size == null || " +
						"input.maximum-message-size <= 1048576)",
					Message: "maximum-message-size must be between 1024 and 1048576",
				},
				{
					Kind: "predicate",
					When: "(input.message-retention-period != null)",
					Require: "(input.message-retention-period == null || " +
						"input.message-retention-period >= 60) && " +
						"(input.message-retention-period == null || " +
						"input.message-retention-period <= 1209600)",
					Message: "message-retention-period must be between 60 and 1209600",
				},
				{
					Kind: "predicate",
					When: "(input.receive-message-wait-time-seconds != null)",
					Require: "(input.receive-message-wait-time-seconds == null || " +
						"input.receive-message-wait-time-seconds >= 0) && " +
						"(input.receive-message-wait-time-seconds == null || " +
						"input.receive-message-wait-time-seconds <= 20)",
					Message: "receive-message-wait-time-seconds must be between 0 and 20",
				},
				{
					Kind: "predicate",
					When: "(input.visibility-timeout != null)",
					Require: "(input.visibility-timeout == null || " +
						"input.visibility-timeout >= 0) && " +
						"(input.visibility-timeout == null || " +
						"input.visibility-timeout <= 43200)",
					Message: "visibility-timeout must be between 0 and 43200",
				},
				{
					Kind: "predicate",
					When: "(input.kms-data-key-reuse-period-seconds != null)",
					Require: "(input.kms-data-key-reuse-period-seconds == null || " +
						"input.kms-data-key-reuse-period-seconds >= 60) && " +
						"(input.kms-data-key-reuse-period-seconds == null || " +
						"input.kms-data-key-reuse-period-seconds <= 86400)",
					Message: "kms-data-key-reuse-period-seconds must be between 60 and 86400",
				},
			},
		},

		"queue-policy": {
			Inputs: map[string]typecheck.Type{
				"policy":    typecheck.TString(),
				"queue-url": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{},
		},
	}
	for key, want := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assertTypeSchemaEqual(t, want, schema.Resources[key])
		})
	}
}
