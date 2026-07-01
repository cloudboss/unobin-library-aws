package sns

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/sns"
)

// TestLibraryRegistersSns checks the runtime registration: every SNS resource
// is present under Resources and dispatches to its output type.
func TestLibraryRegistersSns(t *testing.T) {
	lib := Library()
	resources := map[string]reflect.Type{
		"topic":              reflect.TypeFor[*svc.TopicOutput](),
		"topic-subscription": reflect.TypeFor[*svc.TopicSubscriptionOutput](),
		"topic-policy":       reflect.TypeFor[*svc.TopicPolicyOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestSnsSchemas asserts the whole derived TypeSchema for each SNS resource:
// input and output field types, the cross-field and enum constraints each
// Constraints method declares, and the optional defaults.
func TestSnsSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"topic": {
			Inputs: map[string]typecheck.Type{
				"application-failure-feedback-role-arn":    typecheck.TOptional(typecheck.TString()),
				"application-success-feedback-role-arn":    typecheck.TOptional(typecheck.TString()),
				"application-success-feedback-sample-rate": typecheck.TOptional(typecheck.TInteger()),
				"archive-policy":                           typecheck.TOptional(typecheck.TString()),
				"content-based-deduplication":              typecheck.TOptional(typecheck.TBoolean()),
				"delivery-policy":                          typecheck.TOptional(typecheck.TString()),
				"display-name":                             typecheck.TOptional(typecheck.TString()),
				"fifo-throughput-scope":                    typecheck.TOptional(typecheck.TString()),
				"fifo-topic":                               typecheck.TOptional(typecheck.TBoolean()),
				"firehose-failure-feedback-role-arn":       typecheck.TOptional(typecheck.TString()),
				"firehose-success-feedback-role-arn":       typecheck.TOptional(typecheck.TString()),
				"firehose-success-feedback-sample-rate":    typecheck.TOptional(typecheck.TInteger()),
				"http-failure-feedback-role-arn":           typecheck.TOptional(typecheck.TString()),
				"http-success-feedback-role-arn":           typecheck.TOptional(typecheck.TString()),
				"http-success-feedback-sample-rate":        typecheck.TOptional(typecheck.TInteger()),
				"kms-master-key-id":                        typecheck.TOptional(typecheck.TString()),
				"lambda-failure-feedback-role-arn":         typecheck.TOptional(typecheck.TString()),
				"lambda-success-feedback-role-arn":         typecheck.TOptional(typecheck.TString()),
				"lambda-success-feedback-sample-rate":      typecheck.TOptional(typecheck.TInteger()),
				"name":                                     typecheck.TString(),
				"policy":                                   typecheck.TOptional(typecheck.TString()),
				"signature-version":                        typecheck.TOptional(typecheck.TString()),
				"sqs-failure-feedback-role-arn":            typecheck.TOptional(typecheck.TString()),
				"sqs-success-feedback-role-arn":            typecheck.TOptional(typecheck.TString()),
				"sqs-success-feedback-sample-rate":         typecheck.TOptional(typecheck.TInteger()),
				"tags":                                     typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"tracing-config":                           typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":   typecheck.TString(),
				"owner": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "(input.archive-policy != null)",
					Require: "(input.fifo-topic == true)",
					Message: "archive-policy requires fifo-topic to be true",
				},
				{
					Kind:    "predicate",
					When:    "(input.fifo-throughput-scope != null)",
					Require: "(input.fifo-topic == true)",
					Message: "fifo-throughput-scope requires fifo-topic to be true",
				},
				{
					Kind:    "predicate",
					When:    "(input.content-based-deduplication == true)",
					Require: "(input.fifo-topic == true)",
					Message: "content-based-deduplication requires fifo-topic to be true",
				},
				{
					Kind: "predicate",
					When: "(input.fifo-throughput-scope != null)",
					Require: "(input.fifo-throughput-scope == 'Topic' || " +
						"input.fifo-throughput-scope == 'MessageGroup')",
					Message: "fifo-throughput-scope must be Topic or MessageGroup",
				},
				{
					Kind: "predicate",
					When: "(input.tracing-config != null)",
					Require: "(input.tracing-config == 'Active' || " +
						"input.tracing-config == 'PassThrough')",
					Message: "tracing-config must be Active or PassThrough",
				},
				{
					Kind: "predicate",
					When: "(input.signature-version != null)",
					Require: "(input.signature-version == '1' || " +
						"input.signature-version == '2')",
					Message: "signature-version must be 1 or 2",
				},
				{
					Kind: "predicate",
					When: "(input.http-success-feedback-sample-rate != null)",
					Require: "(input.http-success-feedback-sample-rate == null || " +
						"input.http-success-feedback-sample-rate >= 0) && " +
						"(input.http-success-feedback-sample-rate == null || " +
						"input.http-success-feedback-sample-rate <= 100)",
					Message: "http-success-feedback-sample-rate must be between 0 and 100",
				},
				{
					Kind: "predicate",
					When: "(input.sqs-success-feedback-sample-rate != null)",
					Require: "(input.sqs-success-feedback-sample-rate == null || " +
						"input.sqs-success-feedback-sample-rate >= 0) && " +
						"(input.sqs-success-feedback-sample-rate == null || " +
						"input.sqs-success-feedback-sample-rate <= 100)",
					Message: "sqs-success-feedback-sample-rate must be between 0 and 100",
				},
				{
					Kind: "predicate",
					When: "(input.application-success-feedback-sample-rate != null)",
					Require: "(input.application-success-feedback-sample-rate == null || " +
						"input.application-success-feedback-sample-rate >= 0) && " +
						"(input.application-success-feedback-sample-rate == null || " +
						"input.application-success-feedback-sample-rate <= 100)",
					Message: "application-success-feedback-sample-rate must be between 0 and 100",
				},
				{
					Kind: "predicate",
					When: "(input.firehose-success-feedback-sample-rate != null)",
					Require: "(input.firehose-success-feedback-sample-rate == null || " +
						"input.firehose-success-feedback-sample-rate >= 0) && " +
						"(input.firehose-success-feedback-sample-rate == null || " +
						"input.firehose-success-feedback-sample-rate <= 100)",
					Message: "firehose-success-feedback-sample-rate must be between 0 and 100",
				},
				{
					Kind: "predicate",
					When: "(input.lambda-success-feedback-sample-rate != null)",
					Require: "(input.lambda-success-feedback-sample-rate == null || " +
						"input.lambda-success-feedback-sample-rate >= 0) && " +
						"(input.lambda-success-feedback-sample-rate == null || " +
						"input.lambda-success-feedback-sample-rate <= 100)",
					Message: "lambda-success-feedback-sample-rate must be between 0 and 100",
				},
			},
		},

		"topic-subscription": {
			Inputs: map[string]typecheck.Type{
				"confirmation-timeout-in-minutes": typecheck.TOptional(typecheck.TInteger()),
				"delivery-policy":                 typecheck.TOptional(typecheck.TString()),
				"endpoint":                        typecheck.TOptional(typecheck.TString()),
				"endpoint-auto-confirms":          typecheck.TOptional(typecheck.TBoolean()),
				"filter-policy":                   typecheck.TOptional(typecheck.TString()),
				"filter-policy-scope":             typecheck.TOptional(typecheck.TString()),
				"protocol":                        typecheck.TString(),
				"raw-message-delivery":            typecheck.TOptional(typecheck.TBoolean()),
				"redrive-policy":                  typecheck.TOptional(typecheck.TString()),
				"replay-policy":                   typecheck.TOptional(typecheck.TString()),
				"subscription-role-arn":           typecheck.TOptional(typecheck.TString()),
				"topic-arn":                       typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                  typecheck.TString(),
				"filter-policy-scope":  typecheck.TString(),
				"owner":                typecheck.TString(),
				"pending-confirmation": typecheck.TBoolean(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "true",
					Require: "(input.protocol == 'application' || " +
						"input.protocol == 'email' || " +
						"input.protocol == 'email-json' || " +
						"input.protocol == 'firehose' || " +
						"input.protocol == 'http' || " +
						"input.protocol == 'https' || " +
						"input.protocol == 'lambda' || " +
						"input.protocol == 'sms' || " +
						"input.protocol == 'sqs')",
				},
				{
					Kind: "predicate",
					When: "(input.filter-policy-scope != null)",
					Require: "(input.filter-policy-scope == 'MessageAttributes' || " +
						"input.filter-policy-scope == 'MessageBody')",
					Message: "filter-policy-scope must be MessageAttributes or MessageBody",
				},
				{
					Kind:   "required-with",
					Fields: []string{"input.filter-policy-scope", "input.filter-policy"},
				},
				{
					Kind:    "predicate",
					When:    "(input.protocol == 'firehose')",
					Require: "(input.subscription-role-arn != null)",
					Message: "subscription-role-arn is required when protocol is firehose",
				},
			},
		},

		"topic-policy": {
			Inputs: map[string]typecheck.Type{
				"arn":    typecheck.TString(),
				"policy": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"owner":  typecheck.TString(),
				"policy": typecheck.TString(),
			},
		},
	}
	for key, want := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assertTypeSchemaEqual(t, want, schema.Resources[key])
		})
	}
}
