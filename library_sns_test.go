package library_test

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	library "github.com/cloudboss/unobin-library-aws"
	"github.com/cloudboss/unobin-library-aws/internal/service/sns"
)

// TestLibraryRegistersSns checks the runtime registration: every SNS resource
// is present under Resources and dispatches to its output type.
func TestLibraryRegistersSns(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"sns-topic":              reflect.TypeFor[*sns.TopicOutput](),
		"sns-topic-subscription": reflect.TypeFor[*sns.TopicSubscriptionOutput](),
		"sns-topic-policy":       reflect.TypeFor[*sns.TopicPolicyOutput](),
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
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	resources := map[string]*runtime.TypeSchema{
		"sns-topic": {
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
				"tags":                                     typecheck.TMap(typecheck.TString()),
				"tracing-config":                           typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":   typecheck.TString(),
				"owner": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "(var.archive-policy != null)",
					Require: "(var.fifo-topic == true)",
					Message: "archive-policy requires fifo-topic to be true",
				},
				{
					Kind:    "predicate",
					When:    "(var.fifo-throughput-scope != null)",
					Require: "(var.fifo-topic == true)",
					Message: "fifo-throughput-scope requires fifo-topic to be true",
				},
				{
					Kind:    "predicate",
					When:    "(var.content-based-deduplication == true)",
					Require: "(var.fifo-topic == true)",
					Message: "content-based-deduplication requires fifo-topic to be true",
				},
				{
					Kind: "predicate",
					When: "(var.fifo-throughput-scope != null)",
					Require: "(var.fifo-throughput-scope == 'Topic' || " +
						"var.fifo-throughput-scope == 'MessageGroup')",
					Message: "fifo-throughput-scope must be Topic or MessageGroup",
				},
				{
					Kind: "predicate",
					When: "(var.tracing-config != null)",
					Require: "(var.tracing-config == 'Active' || " +
						"var.tracing-config == 'PassThrough')",
					Message: "tracing-config must be Active or PassThrough",
				},
				{
					Kind: "predicate",
					When: "(var.signature-version != null)",
					Require: "(var.signature-version == '1' || " +
						"var.signature-version == '2')",
					Message: "signature-version must be 1 or 2",
				},
				{
					Kind: "predicate",
					When: "(var.http-success-feedback-sample-rate != null)",
					Require: "(var.http-success-feedback-sample-rate == null || " +
						"var.http-success-feedback-sample-rate >= 0) && " +
						"(var.http-success-feedback-sample-rate == null || " +
						"var.http-success-feedback-sample-rate <= 100)",
					Message: "http-success-feedback-sample-rate must be between 0 and 100",
				},
				{
					Kind: "predicate",
					When: "(var.sqs-success-feedback-sample-rate != null)",
					Require: "(var.sqs-success-feedback-sample-rate == null || " +
						"var.sqs-success-feedback-sample-rate >= 0) && " +
						"(var.sqs-success-feedback-sample-rate == null || " +
						"var.sqs-success-feedback-sample-rate <= 100)",
					Message: "sqs-success-feedback-sample-rate must be between 0 and 100",
				},
				{
					Kind: "predicate",
					When: "(var.application-success-feedback-sample-rate != null)",
					Require: "(var.application-success-feedback-sample-rate == null || " +
						"var.application-success-feedback-sample-rate >= 0) && " +
						"(var.application-success-feedback-sample-rate == null || " +
						"var.application-success-feedback-sample-rate <= 100)",
					Message: "application-success-feedback-sample-rate must be between 0 and 100",
				},
				{
					Kind: "predicate",
					When: "(var.firehose-success-feedback-sample-rate != null)",
					Require: "(var.firehose-success-feedback-sample-rate == null || " +
						"var.firehose-success-feedback-sample-rate >= 0) && " +
						"(var.firehose-success-feedback-sample-rate == null || " +
						"var.firehose-success-feedback-sample-rate <= 100)",
					Message: "firehose-success-feedback-sample-rate must be between 0 and 100",
				},
				{
					Kind: "predicate",
					When: "(var.lambda-success-feedback-sample-rate != null)",
					Require: "(var.lambda-success-feedback-sample-rate == null || " +
						"var.lambda-success-feedback-sample-rate >= 0) && " +
						"(var.lambda-success-feedback-sample-rate == null || " +
						"var.lambda-success-feedback-sample-rate <= 100)",
					Message: "lambda-success-feedback-sample-rate must be between 0 and 100",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.tags", Optional: true},
			},
		},

		"sns-topic-subscription": {
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
					Require: "(var.protocol == 'application' || " +
						"var.protocol == 'email' || " +
						"var.protocol == 'email-json' || " +
						"var.protocol == 'firehose' || " +
						"var.protocol == 'http' || " +
						"var.protocol == 'https' || " +
						"var.protocol == 'lambda' || " +
						"var.protocol == 'sms' || " +
						"var.protocol == 'sqs')",
				},
				{
					Kind: "predicate",
					When: "(var.filter-policy-scope != null)",
					Require: "(var.filter-policy-scope == 'MessageAttributes' || " +
						"var.filter-policy-scope == 'MessageBody')",
					Message: "filter-policy-scope must be MessageAttributes or MessageBody",
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.filter-policy-scope", "var.filter-policy"},
				},
				{
					Kind:    "predicate",
					When:    "(var.protocol == 'firehose')",
					Require: "(var.subscription-role-arn != null)",
					Message: "subscription-role-arn is required when protocol is firehose",
				},
			},
		},

		"sns-topic-policy": {
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
			assert.Equal(t, want, schema.Resources[key])
		})
	}
}
