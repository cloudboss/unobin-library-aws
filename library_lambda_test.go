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
	"github.com/cloudboss/unobin-library-aws/internal/service/lambda"
)

// TestLibraryRegistersLambda checks the runtime registration: the two Lambda
// resources are present under Resources and the invoke action under Actions,
// each dispatching to its output type.
func TestLibraryRegistersLambda(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"lambda-function":   reflect.TypeFor[*lambda.FunctionOutput](),
		"lambda-permission": reflect.TypeFor[*lambda.PermissionOutput](),
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
// that nothing is marked sensitive, and the cross-field constraints derived from
// each Constraints method. The deployment package source is flat top-level
// fields so its exactly-one-source rules derive as constraints. normalizeSchema
// (in library_s3_test.go) sorts nested object fields so the comparison is stable
// despite goschema's varying field order.
func TestLambdaSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	resources := map[string]*runtime.TypeSchema{
		"lambda-function": {
			Inputs: map[string]typecheck.Type{
				"architectures":           typecheck.TList(typecheck.TString()),
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
					{Name: "entry-point", Type: typecheck.TList(typecheck.TString())},
					{Name: "working-directory", Type: typecheck.TString(), Optional: true},
					{Name: "command", Type: typecheck.TList(typecheck.TString())},
				})),
				"image-uri":   typecheck.TOptional(typecheck.TString()),
				"kms-key-arn": typecheck.TOptional(typecheck.TString()),
				"layers":      typecheck.TList(typecheck.TString()),
				"logging-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "log-group", Type: typecheck.TString(), Optional: true},
					{Name: "application-log-level", Type: typecheck.TString(), Optional: true},
					{Name: "system-log-level", Type: typecheck.TString(), Optional: true},
					{Name: "log-format", Type: typecheck.TString(), Optional: true},
				})),
				"memory-size":                    typecheck.TOptional(typecheck.TInteger()),
				"package-type":                   typecheck.TOptional(typecheck.TString()),
				"publish":                        typecheck.TOptional(typecheck.TBoolean()),
				"reserved-concurrent-executions": typecheck.TOptional(typecheck.TInteger()),
				"role":                           typecheck.TString(),
				"runtime":                        typecheck.TOptional(typecheck.TString()),
				"s3-bucket":                      typecheck.TOptional(typecheck.TString()),
				"s3-key":                         typecheck.TOptional(typecheck.TString()),
				"s3-object-version":              typecheck.TOptional(typecheck.TString()),
				"skip-destroy":                   typecheck.TOptional(typecheck.TBoolean()),
				"snap-start": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "apply-on", Type: typecheck.TString(), Optional: true},
				})),
				"source-kms-key-arn": typecheck.TOptional(typecheck.TString()),
				"tags":               typecheck.TMap(typecheck.TString()),
				"timeout":            typecheck.TOptional(typecheck.TInteger()),
				"tracing-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "mode", Type: typecheck.TString(), Optional: true},
				})),
				"vpc-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "subnet-ids", Type: typecheck.TList(typecheck.TString())},
					{Name: "security-group-ids", Type: typecheck.TList(typecheck.TString())},
					{Name: "ipv6-allowed-for-dual-stack", Type: typecheck.TBoolean(), Optional: true},
				})),
				"zip-file-content": typecheck.TOptional(typecheck.TString()),
				"zip-file-path":    typecheck.TOptional(typecheck.TString()),
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
					Kind: "exactly-one-of", Fields: []string{
						"var.zip-file-content", "var.zip-file-path",
						"var.s3-bucket", "var.image-uri",
					},
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"var.zip-file-content", "var.zip-file-path",
					},
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.s3-bucket", "var.s3-key"},
				},
				{
					Kind:   "required-with",
					Fields: []string{"var.s3-key", "var.s3-bucket"},
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"var.s3-object-version", "var.zip-file-content",
						"var.zip-file-path", "var.image-uri",
					},
				},
				{
					Kind:   "forbidden-with",
					Fields: []string{"var.source-kms-key-arn", "var.image-uri"},
				},
				{
					Kind:    "predicate",
					When:    "!(var.package-type == 'Image')",
					Require: "(var.handler != null) && (var.runtime != null)",
					Message: "handler and runtime are required for a Zip package",
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
	}
	for key, want := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, normalizeSchema(want), normalizeSchema(schema.Resources[key]))
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
			assert.Equal(t, normalizeSchema(want), normalizeSchema(schema.Actions[key]))
		})
	}
}
