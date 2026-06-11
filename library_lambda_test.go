package library_test

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	library "github.com/cloudboss/unobin-library-aws"
	"github.com/cloudboss/unobin-library-aws/internal/service/lambda"
)

// TestLibraryRegistersLambda checks the runtime registration: the Lambda
// resources are present under Resources and the invoke action under Actions,
// each dispatching to its output type.
func TestLibraryRegistersLambda(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"lambda-function":             reflect.TypeFor[*lambda.FunctionOutput](),
		"lambda-permission":           reflect.TypeFor[*lambda.PermissionOutput](),
		"lambda-event-source-mapping": reflect.TypeFor[*lambda.EventSourceMappingOutput](),
		"lambda-function-url":         reflect.TypeFor[*lambda.FunctionUrlOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
	t.Run("lambda-invoke", func(t *testing.T) {
		require.Contains(t, lib.Actions, "lambda-invoke")
		assert.Equal(t, reflect.TypeFor[*lambda.InvokeOutput](),
			lib.Actions["lambda-invoke"].OutputType())
	})
}

// TestLambdaSchemas asserts the whole TypeSchema goschema reads from this
// library's source for each Lambda construct: the input and output field types,
// that nothing is marked sensitive, the optional defaults, and the cross-field
// constraints derived from each Constraints method, including the dotted rules on
// the code block and the other nested blocks. The comparison is direct, so each
// nested object's fields are listed in goschema's declaration order.
func TestLambdaSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"lambda-function": {
			Inputs: map[string]typecheck.Type{
				"architectures": typecheck.TList(typecheck.TString()),
				"code": typecheck.TObject([]typecheck.ObjectField{
					{Name: "zip-file-content", Type: typecheck.TString(), Optional: true},
					{Name: "zip-file-path", Type: typecheck.TString(), Optional: true},
					{Name: "s3-bucket", Type: typecheck.TString(), Optional: true},
					{Name: "s3-key", Type: typecheck.TString(), Optional: true},
					{Name: "s3-object-version", Type: typecheck.TString(), Optional: true},
					{Name: "image-uri", Type: typecheck.TString(), Optional: true},
					{Name: "source-kms-key-arn", Type: typecheck.TString(), Optional: true},
				}),
				"code-signing-config-arn": typecheck.TOptional(typecheck.TString()),
				"dead-letter-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "target-arn", Type: typecheck.TString(), Optional: true},
				})),
				"description": typecheck.TOptional(typecheck.TString()),
				"environment": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "variables", Type: typecheck.TMap(typecheck.TString())},
				})),
				"ephemeral-storage": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "size", Type: typecheck.TInteger(), Optional: true},
				})),
				"file-system-configs": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "arn", Type: typecheck.TString()},
					{Name: "local-mount-path", Type: typecheck.TString()},
				})),
				"function-name": typecheck.TString(),
				"handler":       typecheck.TOptional(typecheck.TString()),
				"image-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "command", Type: typecheck.TList(typecheck.TString())},
					{Name: "entry-point", Type: typecheck.TList(typecheck.TString())},
					{Name: "working-directory", Type: typecheck.TString(), Optional: true},
				})),
				"kms-key-arn": typecheck.TOptional(typecheck.TString()),
				"layers":      typecheck.TList(typecheck.TString()),
				"logging-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "log-format", Type: typecheck.TString(), Optional: true},
					{Name: "log-group", Type: typecheck.TString(), Optional: true},
					{Name: "application-log-level", Type: typecheck.TString(), Optional: true},
					{Name: "system-log-level", Type: typecheck.TString(), Optional: true},
				})),
				"memory-size":                    typecheck.TOptional(typecheck.TInteger()),
				"package-type":                   typecheck.TOptional(typecheck.TString()),
				"publish":                        typecheck.TOptional(typecheck.TBoolean()),
				"reserved-concurrent-executions": typecheck.TOptional(typecheck.TInteger()),
				"role":                           typecheck.TString(),
				"runtime":                        typecheck.TOptional(typecheck.TString()),
				"skip-destroy":                   typecheck.TOptional(typecheck.TBoolean()),
				"snap-start": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "apply-on", Type: typecheck.TString(), Optional: true},
				})),
				"tags":    typecheck.TMap(typecheck.TString()),
				"timeout": typecheck.TOptional(typecheck.TInteger()),
				"tracing-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "mode", Type: typecheck.TString(), Optional: true},
				})),
				"vpc-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "subnet-ids", Type: typecheck.TList(typecheck.TString())},
					{Name: "security-group-ids", Type: typecheck.TList(typecheck.TString())},
					{Name: "ipv6-allowed-for-dual-stack", Type: typecheck.TBoolean(), Optional: true},
				})),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                            typecheck.TString(),
				"code-sha256":                    typecheck.TString(),
				"invoke-arn":                     typecheck.TString(),
				"last-modified":                  typecheck.TString(),
				"qualified-arn":                  typecheck.TString(),
				"qualified-invoke-arn":           typecheck.TString(),
				"signing-job-arn":                typecheck.TString(),
				"signing-profile-version-arn":    typecheck.TString(),
				"snap-start-optimization-status": typecheck.TString(),
				"source-code-size":               typecheck.TInteger(),
				"version":                        typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "exactly-one-of",
					Fields: []string{
						"var.code.zip-file-content", "var.code.zip-file-path",
						"var.code.s3-bucket", "var.code.image-uri",
					},
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"var.code.zip-file-content", "var.code.zip-file-path",
					},
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.code.s3-bucket", "var.code.s3-key"},
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.code.s3-key", "var.code.s3-bucket"},
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"var.code.s3-object-version", "var.code.zip-file-content",
						"var.code.zip-file-path", "var.code.image-uri",
					},
				},
				{
					Kind:   "forbidden-with",
					Fields: []string{"var.code.source-kms-key-arn", "var.code.image-uri"},
				},
				{
					Kind:    "predicate",
					When:    "!(var.package-type == 'Image')",
					Require: "(var.handler != null) && (var.runtime != null)",
					Message: "handler and runtime are required for a Zip package",
				},
				{
					Kind:    "predicate",
					When:    "(var.package-type == 'Image')",
					Require: "(var.code.image-uri != null)",
					Message: "an Image package requires code.image-uri",
				},
				{
					Kind:    "predicate",
					When:    "(var.package-type != null)",
					Require: "(var.package-type == 'Zip' || var.package-type == 'Image')",
					Message: "package-type must be Zip or Image",
				},
				{
					Kind: "predicate",
					When: "(var.memory-size != null)",
					Require: "(var.memory-size == null || var.memory-size >= 128) && " +
						"(var.memory-size == null || var.memory-size <= 32768)",
					Message: "memory-size must be between 128 and 32768",
				},
				{
					Kind: "predicate",
					When: "(var.timeout != null)",
					Require: "(var.timeout == null || var.timeout >= 1) && (var.timeout == null || " +
						"var.timeout <= 900)",
					Message: "timeout must be between 1 and 900",
				},
				{
					Kind: "predicate",
					When: "(var.reserved-concurrent-executions != null)",
					Require: "(var.reserved-concurrent-executions == null || " +
						"var.reserved-concurrent-executions >= 0)",
					Message: "reserved-concurrent-executions must be zero or greater",
				},
				{
					Kind:    "predicate",
					When:    "(var.image-config != null)",
					Require: "(var.package-type == 'Image')",
					Message: "image-config applies only to an Image package",
				},
				{
					Kind: "predicate",
					When: "(var.tracing-config.mode != null)",
					Require: "(var.tracing-config.mode == 'Active' || " +
						"var.tracing-config.mode == 'PassThrough')",
					Message: "tracing-config mode must be Active or PassThrough",
				},
				{
					Kind: "predicate",
					When: "(var.logging-config.log-format != null)",
					Require: "(var.logging-config.log-format == 'Text' || " +
						"var.logging-config.log-format == 'JSON')",
					Message: "logging-config log-format must be Text or JSON",
				},
				{
					Kind: "predicate",
					When: "(var.logging-config.application-log-level != null)",
					Require: "(var.logging-config.application-log-level == 'TRACE' || " +
						"var.logging-config.application-log-level == 'DEBUG' || " +
						"var.logging-config.application-log-level == 'INFO' || " +
						"var.logging-config.application-log-level == 'WARN' || " +
						"var.logging-config.application-log-level == 'ERROR' || " +
						"var.logging-config.application-log-level == 'FATAL')",
					Message: "application-log-level must be TRACE, DEBUG, INFO, WARN, ERROR, or FATAL",
				},
				{
					Kind:    "predicate",
					When:    "(var.logging-config.application-log-level != null)",
					Require: "(var.logging-config.log-format == 'JSON')",
					Message: "application-log-level requires log-format JSON",
				},
				{
					Kind: "predicate",
					When: "(var.logging-config.system-log-level != null)",
					Require: "(var.logging-config.system-log-level == 'DEBUG' || " +
						"var.logging-config.system-log-level == 'INFO' || " +
						"var.logging-config.system-log-level == 'WARN')",
					Message: "system-log-level must be DEBUG, INFO, or WARN",
				},
				{
					Kind:    "predicate",
					When:    "(var.logging-config.system-log-level != null)",
					Require: "(var.logging-config.log-format == 'JSON')",
					Message: "system-log-level requires log-format JSON",
				},
				{
					Kind: "predicate",
					When: "(var.snap-start.apply-on != null)",
					Require: "(var.snap-start.apply-on == 'None' || " +
						"var.snap-start.apply-on == 'PublishedVersions')",
					Message: "snap-start apply-on must be None or PublishedVersions",
				},
				{
					Kind: "predicate",
					When: "(var.ephemeral-storage.size != null)",
					Require: "(var.ephemeral-storage.size == null || " +
						"var.ephemeral-storage.size >= 512) && " +
						"(var.ephemeral-storage.size == null || " +
						"var.ephemeral-storage.size <= 10240)",
					Message: "ephemeral-storage size must be between 512 and 10240",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.architectures", Optional: true},
				{Field: "var.layers", Optional: true},
				{Field: "var.file-system-configs", Optional: true},
				{Field: "var.tags", Optional: true},
			},
		},
		"lambda-permission": {
			Inputs: map[string]typecheck.Type{
				"action":                   typecheck.TString(),
				"event-source-token":       typecheck.TOptional(typecheck.TString()),
				"function-name":            typecheck.TString(),
				"function-url-auth-type":   typecheck.TOptional(typecheck.TString()),
				"invoked-via-function-url": typecheck.TOptional(typecheck.TBoolean()),
				"principal":                typecheck.TString(),
				"principal-org-id":         typecheck.TOptional(typecheck.TString()),
				"qualifier":                typecheck.TOptional(typecheck.TString()),
				"source-account":           typecheck.TOptional(typecheck.TString()),
				"source-arn":               typecheck.TOptional(typecheck.TString()),
				"statement-id":             typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"statement-id": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(var.function-url-auth-type != null)",
					Require: "(var.function-url-auth-type == 'AWS_IAM' || " +
						"var.function-url-auth-type == 'NONE')",
					Message: "function-url-auth-type must be AWS_IAM or NONE",
				},
			},
		},
		"lambda-event-source-mapping": {
			Inputs: map[string]typecheck.Type{
				"amazon-managed-kafka-event-source-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "consumer-group-id", Type: typecheck.TString(), Optional: true},
					{Name: "schema-registry-config", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "schema-registry-uri", Type: typecheck.TString(), Optional: true},
						{Name: "event-record-format", Type: typecheck.TString(), Optional: true},
						{Name: "access-configs", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "type", Type: typecheck.TString(), Optional: true},
							{Name: "uri", Type: typecheck.TString(), Optional: true},
						}))},
						{Name: "schema-validation-configs", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "attribute", Type: typecheck.TString(), Optional: true},
						}))},
					}), Optional: true},
				})),
				"batch-size":                     typecheck.TOptional(typecheck.TInteger()),
				"bisect-batch-on-function-error": typecheck.TOptional(typecheck.TBoolean()),
				"destination-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "on-failure", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "destination", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
				})),
				"document-db-event-source-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "database-name", Type: typecheck.TString(), Optional: true},
					{Name: "collection-name", Type: typecheck.TString(), Optional: true},
					{Name: "full-document", Type: typecheck.TString(), Optional: true},
				})),
				"enabled":          typecheck.TOptional(typecheck.TBoolean()),
				"event-source-arn": typecheck.TOptional(typecheck.TString()),
				"filter-criteria": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "filters", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "pattern", Type: typecheck.TString(), Optional: true},
					}))},
				})),
				"function-name":           typecheck.TString(),
				"function-response-types": typecheck.TList(typecheck.TString()),
				"kms-key-arn":             typecheck.TOptional(typecheck.TString()),
				"logging-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "system-log-level", Type: typecheck.TString(), Optional: true},
				})),
				"maximum-batching-window-in-seconds": typecheck.TOptional(typecheck.TInteger()),
				"maximum-record-age-in-seconds":      typecheck.TOptional(typecheck.TInteger()),
				"maximum-retry-attempts":             typecheck.TOptional(typecheck.TInteger()),
				"metrics-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "metrics", Type: typecheck.TList(typecheck.TString())},
				})),
				"parallelization-factor": typecheck.TOptional(typecheck.TInteger()),
				"provisioned-poller-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "minimum-pollers", Type: typecheck.TInteger(), Optional: true},
					{Name: "maximum-pollers", Type: typecheck.TInteger(), Optional: true},
					{Name: "poller-group-name", Type: typecheck.TString(), Optional: true},
				})),
				"queues": typecheck.TList(typecheck.TString()),
				"scaling-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "maximum-concurrency", Type: typecheck.TInteger(), Optional: true},
				})),
				"self-managed-event-source": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "endpoints", Type: typecheck.TMap(typecheck.TList(typecheck.TString()))},
				})),
				"self-managed-kafka-event-source-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "consumer-group-id", Type: typecheck.TString(), Optional: true},
					{Name: "schema-registry-config", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "schema-registry-uri", Type: typecheck.TString(), Optional: true},
						{Name: "event-record-format", Type: typecheck.TString(), Optional: true},
						{Name: "access-configs", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "type", Type: typecheck.TString(), Optional: true},
							{Name: "uri", Type: typecheck.TString(), Optional: true},
						}))},
						{Name: "schema-validation-configs", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "attribute", Type: typecheck.TString(), Optional: true},
						}))},
					}), Optional: true},
				})),
				"source-access-configurations": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "type", Type: typecheck.TString(), Optional: true},
					{Name: "uri", Type: typecheck.TString(), Optional: true},
				})),
				"starting-position":           typecheck.TOptional(typecheck.TString()),
				"starting-position-timestamp": typecheck.TOptional(typecheck.TString()),
				"tags":                        typecheck.TMap(typecheck.TString()),
				"topics":                      typecheck.TList(typecheck.TString()),
				"tumbling-window-in-seconds":  typecheck.TOptional(typecheck.TInteger()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                     typecheck.TString(),
				"function-arn":            typecheck.TString(),
				"last-modified":           typecheck.TString(),
				"last-processing-result":  typecheck.TString(),
				"state":                   typecheck.TString(),
				"state-transition-reason": typecheck.TString(),
				"uuid":                    typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "exactly-one-of",
					Fields: []string{"var.event-source-arn", "var.self-managed-event-source"},
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"var.amazon-managed-kafka-event-source-config",
						"var.self-managed-event-source",
						"var.self-managed-kafka-event-source-config",
					},
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"var.self-managed-kafka-event-source-config",
						"var.event-source-arn",
						"var.amazon-managed-kafka-event-source-config",
					},
				},
				{
					Kind: "predicate",
					When: "(var.starting-position != null)",
					Require: "(var.starting-position == 'TRIM_HORIZON' || " +
						"var.starting-position == 'LATEST' || " +
						"var.starting-position == 'AT_TIMESTAMP')",
					Message: "starting-position must be TRIM_HORIZON, LATEST, or AT_TIMESTAMP",
				},
				{
					Kind: "predicate",
					When: "(var.batch-size != null)",
					Require: "(var.batch-size == null || " +
						"var.batch-size >= 1) && " +
						"(var.batch-size == null || " +
						"var.batch-size <= 10000)",
					Message: "batch-size must be between 1 and 10000",
				},
				{
					Kind: "predicate",
					When: "(var.maximum-record-age-in-seconds != null)",
					Require: "((var.maximum-record-age-in-seconds == -1) || " +
						"((var.maximum-record-age-in-seconds == null || " +
						"var.maximum-record-age-in-seconds >= 60) && " +
						"(var.maximum-record-age-in-seconds == null || " +
						"var.maximum-record-age-in-seconds <= 604800)))",
					Message: "maximum-record-age-in-seconds must be -1 or between 60 and 604800",
				},
				{
					Kind: "predicate",
					When: "(var.maximum-retry-attempts != null)",
					Require: "(var.maximum-retry-attempts == null || " +
						"var.maximum-retry-attempts >= -1) && " +
						"(var.maximum-retry-attempts == null || " +
						"var.maximum-retry-attempts <= 10000)",
					Message: "maximum-retry-attempts must be between -1 and 10000",
				},
				{
					Kind: "predicate",
					When: "(var.parallelization-factor != null)",
					Require: "(var.parallelization-factor == null || " +
						"var.parallelization-factor >= 1) && " +
						"(var.parallelization-factor == null || " +
						"var.parallelization-factor <= 10)",
					Message: "parallelization-factor must be between 1 and 10",
				},
				{
					Kind: "predicate",
					When: "(var.tumbling-window-in-seconds != null)",
					Require: "(var.tumbling-window-in-seconds == null || " +
						"var.tumbling-window-in-seconds >= 0) && " +
						"(var.tumbling-window-in-seconds == null || " +
						"var.tumbling-window-in-seconds <= 900)",
					Message: "tumbling-window-in-seconds must be between 0 and 900",
				},
				{
					Kind: "predicate",
					When: "(var.maximum-batching-window-in-seconds != null)",
					Require: "(var.maximum-batching-window-in-seconds == null || " +
						"var.maximum-batching-window-in-seconds >= 0) && " +
						"(var.maximum-batching-window-in-seconds == null || " +
						"var.maximum-batching-window-in-seconds <= 300)",
					Message: "maximum-batching-window-in-seconds must be between 0 and 300",
				},
				{
					Kind: "predicate",
					When: "(var.scaling-config != null)",
					Require: "(var.scaling-config.maximum-concurrency == null || " +
						"var.scaling-config.maximum-concurrency >= 2) && " +
						"(var.scaling-config.maximum-concurrency == null || " +
						"var.scaling-config.maximum-concurrency <= 1000)",
					Message: "scaling-config maximum-concurrency must be between 2 and 1000",
				},
				{
					Kind: "predicate",
					When: "(var.provisioned-poller-config.maximum-pollers != null)",
					Require: "(var.provisioned-poller-config.maximum-pollers == null || " +
						"var.provisioned-poller-config.maximum-pollers >= 1) && " +
						"(var.provisioned-poller-config.maximum-pollers == null || " +
						"var.provisioned-poller-config.maximum-pollers <= 2000)",
					Message: "provisioned-poller-config maximum-pollers must be between 1 and 2000",
				},
				{
					Kind: "predicate",
					When: "(var.provisioned-poller-config.minimum-pollers != null)",
					Require: "(var.provisioned-poller-config.minimum-pollers == null || " +
						"var.provisioned-poller-config.minimum-pollers >= 1) && " +
						"(var.provisioned-poller-config.minimum-pollers == null || " +
						"var.provisioned-poller-config.minimum-pollers <= 200)",
					Message: "provisioned-poller-config minimum-pollers must be between 1 and 200",
				},
				{
					Kind: "predicate",
					When: "(var.document-db-event-source-config.full-document != null)",
					Require: "(var.document-db-event-source-config.full-document == 'UpdateLookup' || " +
						"var.document-db-event-source-config.full-document == 'Default')",
					Message: "document-db-event-source-config full-document must be UpdateLookup or Default",
				},
				{
					Kind: "predicate",
					When: "(var.logging-config.system-log-level != null)",
					Require: "(var.logging-config.system-log-level == 'DEBUG' || " +
						"var.logging-config.system-log-level == 'INFO' || " +
						"var.logging-config.system-log-level == 'WARN')",
					Message: "logging-config system-log-level must be DEBUG, INFO, or WARN",
				},
				{
					Kind: "predicate",
					When: "(var.amazon-managed-kafka-event-source-config.schema-registry-config.event-record-format != null)",
					Require: "(var.amazon-managed-kafka-event-source-config.schema-registry-config.event-record-format == 'JSON' || " +
						"var.amazon-managed-kafka-event-source-config.schema-registry-config.event-record-format == 'SOURCE')",
					Message: "amazon-managed-kafka schema-registry event-record-format must be JSON or SOURCE",
				},
				{
					Kind: "predicate",
					When: "(var.self-managed-kafka-event-source-config.schema-registry-config.event-record-format != null)",
					Require: "(var.self-managed-kafka-event-source-config.schema-registry-config.event-record-format == 'JSON' || " +
						"var.self-managed-kafka-event-source-config.schema-registry-config.event-record-format == 'SOURCE')",
					Message: "self-managed-kafka schema-registry event-record-format must be JSON or SOURCE",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@each.value == 'ReportBatchItemFailures')",
					Message: "function-response-types values must be ReportBatchItemFailures",
					ForEach: "var.function-response-types",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value == 'EventCount' || " +
						"@each.value == 'ErrorCount' || " +
						"@each.value == 'KafkaMetrics')",
					Message: "a metrics-config metric must be EventCount, ErrorCount, or KafkaMetrics",
					ForEach: "var.metrics-config.metrics",
				},
				{
					Kind: "predicate",
					When: "(@each.value.type != null)",
					Require: "(@each.value.type == 'BASIC_AUTH' || " +
						"@each.value.type == 'VPC_SUBNET' || " +
						"@each.value.type == 'VPC_SECURITY_GROUP' || " +
						"@each.value.type == 'SASL_SCRAM_512_AUTH' || " +
						"@each.value.type == 'SASL_SCRAM_256_AUTH' || " +
						"@each.value.type == 'VIRTUAL_HOST' || " +
						"@each.value.type == 'CLIENT_CERTIFICATE_TLS_AUTH' || " +
						"@each.value.type == 'SERVER_ROOT_CA_CERTIFICATE')",
					Message: "a source-access-configuration type must be a valid auth or VPC type",
					ForEach: "var.source-access-configurations",
				},
				{
					Kind: "predicate",
					When: "(@each.value.type != null)",
					Require: "(@each.value.type == 'BASIC_AUTH' || " +
						"@each.value.type == 'CLIENT_CERTIFICATE_TLS_AUTH' || " +
						"@each.value.type == 'SERVER_ROOT_CA_CERTIFICATE')",
					Message: "a schema-registry access-config type must be a valid auth type",
					ForEach: "var.amazon-managed-kafka-event-source-config.schema-registry-config.access-configs",
				},
				{
					Kind: "predicate",
					When: "(@each.value.attribute != null)",
					Require: "(@each.value.attribute == 'KEY' || " +
						"@each.value.attribute == 'VALUE')",
					Message: "a schema-registry validation attribute must be KEY or VALUE",
					ForEach: "var.amazon-managed-kafka-event-source-config.schema-registry-config.schema-validation-configs",
				},
				{
					Kind: "predicate",
					When: "(@each.value.type != null)",
					Require: "(@each.value.type == 'BASIC_AUTH' || " +
						"@each.value.type == 'CLIENT_CERTIFICATE_TLS_AUTH' || " +
						"@each.value.type == 'SERVER_ROOT_CA_CERTIFICATE')",
					Message: "a schema-registry access-config type must be a valid auth type",
					ForEach: "var.self-managed-kafka-event-source-config.schema-registry-config.access-configs",
				},
				{
					Kind: "predicate",
					When: "(@each.value.attribute != null)",
					Require: "(@each.value.attribute == 'KEY' || " +
						"@each.value.attribute == 'VALUE')",
					Message: "a schema-registry validation attribute must be KEY or VALUE",
					ForEach: "var.self-managed-kafka-event-source-config.schema-registry-config.schema-validation-configs",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.function-response-types", Optional: true},
				{Field: "var.queues", Optional: true},
				{Field: "var.topics", Optional: true},
				{Field: "var.source-access-configurations", Optional: true},
				{Field: "var.tags", Optional: true},
			},
		},
		"lambda-function-url": {
			Inputs: map[string]typecheck.Type{
				"auth-type": typecheck.TString(),
				"cors": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "allow-credentials", Type: typecheck.TBoolean(), Optional: true},
					{Name: "allow-headers", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "allow-methods", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "allow-origins", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "expose-headers", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "max-age", Type: typecheck.TInteger(), Optional: true},
				})),
				"function-name": typecheck.TString(),
				"invoke-mode":   typecheck.TOptional(typecheck.TString()),
				"qualifier":     typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"function-arn": typecheck.TString(),
				"function-url": typecheck.TString(),
				"qualifier":    typecheck.TString(),
				"url-id":       typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(var.auth-type == 'AWS_IAM' || var.auth-type == 'NONE')",
					Message: "auth-type must be AWS_IAM or NONE",
				},
				{
					Kind: "predicate",
					When: "(var.invoke-mode != null)",
					Require: "(var.invoke-mode == 'BUFFERED' || " +
						"var.invoke-mode == 'RESPONSE_STREAM')",
					Message: "invoke-mode must be BUFFERED or RESPONSE_STREAM",
				},
				{
					Kind: "predicate",
					When: "(var.cors.max-age != null)",
					Require: "(var.cors.max-age == null || " +
						"var.cors.max-age >= 0) && " +
						"(var.cors.max-age == null || " +
						"var.cors.max-age <= 86400)",
					Message: "cors max-age must be between 0 and 86400 seconds",
				},
			},
		},
	}
	for key, want := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, want, schema.Resources[key])
		})
	}

	actions := map[string]*runtime.TypeSchema{
		"lambda-invoke": {
			Inputs: map[string]typecheck.Type{
				"client-context":  typecheck.TOptional(typecheck.TString()),
				"function-name":   typecheck.TString(),
				"invocation-type": typecheck.TOptional(typecheck.TString()),
				"log-type":        typecheck.TOptional(typecheck.TString()),
				"payload-content": typecheck.TOptional(typecheck.TString()),
				"payload-path":    typecheck.TOptional(typecheck.TString()),
				"qualifier":       typecheck.TOptional(typecheck.TString()),
				"tenant-id":       typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"executed-version": typecheck.TString(),
				"log-result":       typecheck.TString(),
				"payload":          typecheck.TString(),
				"status-code":      typecheck.TInteger(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "exactly-one-of",
					Fields: []string{"var.payload-content", "var.payload-path"},
				},
				{
					Kind: "predicate",
					When: "(var.invocation-type != null)",
					Require: "(var.invocation-type == 'RequestResponse' || " +
						"var.invocation-type == 'Event' || var.invocation-type == 'DryRun')",
					Message: "invocation-type must be RequestResponse, Event, or DryRun",
				},
				{
					Kind:    "predicate",
					When:    "(var.log-type != null)",
					Require: "(var.log-type == 'None' || var.log-type == 'Tail')",
					Message: "log-type must be None or Tail",
				},
			},
		},
	}
	for key, want := range actions {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Actions, key)
			assert.Equal(t, want, schema.Actions[key])
		})
	}
}
