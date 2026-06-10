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
	"github.com/cloudboss/unobin-library-aws/internal/service/apigatewayv2"
)

// TestLibraryRegistersApigatewayv2 checks the runtime registration: the API,
// integration, route, and stage resources are present under Resources and
// dispatch to their output types.
func TestLibraryRegistersApigatewayv2(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"apigatewayv2-api":         reflect.TypeFor[*apigatewayv2.ApiOutput](),
		"apigatewayv2-integration": reflect.TypeFor[*apigatewayv2.IntegrationOutput](),
		"apigatewayv2-route":       reflect.TypeFor[*apigatewayv2.RouteOutput](),
		"apigatewayv2-stage":       reflect.TypeFor[*apigatewayv2.StageOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestApigatewayv2Schemas asserts the whole derived TypeSchema for the four
// API Gateway v2 resources: input and output field types (including the CORS,
// TLS, access-log, and route-settings blocks), the enum and cross-field
// rules, and the optional defaults.
func TestApigatewayv2Schemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	resources := map[string]*runtime.TypeSchema{
		"apigatewayv2-api": {
			Inputs: map[string]typecheck.Type{
				"api-key-selection-expression": typecheck.TOptional(typecheck.TString()),
				"base-path":                    typecheck.TOptional(typecheck.TString()),
				"body":                         typecheck.TOptional(typecheck.TString()),
				"cors-configuration": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "allow-credentials", Type: typecheck.TBoolean(), Optional: true},
					{Name: "allow-headers", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "allow-methods", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "allow-origins", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "expose-headers", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "max-age", Type: typecheck.TInteger(), Optional: true},
				})),
				"credentials-arn":              typecheck.TOptional(typecheck.TString()),
				"description":                  typecheck.TOptional(typecheck.TString()),
				"disable-execute-api-endpoint": typecheck.TOptional(typecheck.TBoolean()),
				"disable-schema-validation":    typecheck.TOptional(typecheck.TBoolean()),
				"fail-on-warnings":             typecheck.TOptional(typecheck.TBoolean()),
				"ip-address-type":              typecheck.TOptional(typecheck.TString()),
				"name":                         typecheck.TString(),
				"protocol-type":                typecheck.TString(),
				"route-key":                    typecheck.TOptional(typecheck.TString()),
				"route-selection-expression":   typecheck.TOptional(typecheck.TString()),
				"tags":                         typecheck.TMap(typecheck.TString()),
				"target":                       typecheck.TOptional(typecheck.TString()),
				"version":                      typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"api-endpoint": typecheck.TString(),
				"api-id":       typecheck.TString(),
				"arn":          typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.protocol-type == 'HTTP' || " +
						"var.protocol-type == 'WEBSOCKET')",
					Message: "protocol-type must be HTTP or WEBSOCKET",
				},
				{
					Kind: "predicate",
					When: "(var.ip-address-type != null)",
					Require: "(var.ip-address-type == 'ipv4' || " +
						"var.ip-address-type == 'dualstack')",
					Message: "ip-address-type must be ipv4 or dualstack",
				},
				{
					Kind: "predicate",
					When: "(var.api-key-selection-expression != null)",
					Require: "(var.api-key-selection-expression == '$context.authorizer.usageIdentifierKey' || " +
						"var.api-key-selection-expression == '$request.header.x-api-key')",
					Message: "api-key-selection-expression must be " +
						"$context.authorizer.usageIdentifierKey or $request.header.x-api-key",
				},
				{
					Kind: "predicate",
					When: "(var.protocol-type == 'WEBSOCKET')",
					Require: "(var.cors-configuration == null) && " +
						"(var.credentials-arn == null) && " +
						"(var.route-key == null) && " +
						"(var.target == null) && " +
						"(var.body == null) && " +
						"(var.fail-on-warnings == null)",
					Message: "cors-configuration, credentials-arn, route-key, target, body, " +
						"and fail-on-warnings are supported only for HTTP APIs",
				},
				{
					Kind:    "predicate",
					When:    "(var.protocol-type == 'WEBSOCKET')",
					Require: "(var.route-selection-expression != null)",
					Message: "route-selection-expression is required for a WebSocket API",
				},
				{
					Kind: "predicate",
					When: "((var.protocol-type == 'HTTP') && " +
						"(var.route-selection-expression != null))",
					Require: "(var.route-selection-expression == '$request.method $request.path')",
					Message: "route-selection-expression for an HTTP API must be " +
						"$request.method $request.path",
				},
				{
					Kind: "predicate",
					When: "(var.base-path != null)",
					Require: "(var.base-path == 'ignore' || " +
						"var.base-path == 'prepend' || " +
						"var.base-path == 'split')",
					Message: "base-path must be ignore, prepend, or split",
				},
				{
					Kind:    "required-with",
					Fields:  []string{"var.fail-on-warnings", "var.body"},
					Message: "fail-on-warnings applies only to a body import",
				},
				{
					Kind:    "required-with",
					Fields:  []string{"var.base-path", "var.body"},
					Message: "base-path applies only to a body import",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.tags", Optional: true},
			},
		},
		"apigatewayv2-integration": {
			Inputs: map[string]typecheck.Type{
				"api-id":                    typecheck.TString(),
				"connection-id":             typecheck.TOptional(typecheck.TString()),
				"connection-type":           typecheck.TOptional(typecheck.TString()),
				"content-handling-strategy": typecheck.TOptional(typecheck.TString()),
				"credentials-arn":           typecheck.TOptional(typecheck.TString()),
				"description":               typecheck.TOptional(typecheck.TString()),
				"integration-method":        typecheck.TOptional(typecheck.TString()),
				"integration-subtype":       typecheck.TOptional(typecheck.TString()),
				"integration-type":          typecheck.TString(),
				"integration-uri":           typecheck.TOptional(typecheck.TString()),
				"passthrough-behavior":      typecheck.TOptional(typecheck.TString()),
				"payload-format-version":    typecheck.TOptional(typecheck.TString()),
				"request-parameters":        typecheck.TMap(typecheck.TString()),
				"request-templates":         typecheck.TMap(typecheck.TString()),
				"response-parameters": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "status-code", Type: typecheck.TString()},
					{Name: "mappings", Type: typecheck.TMap(typecheck.TString())},
				})),
				"template-selection-expression": typecheck.TOptional(typecheck.TString()),
				"timeout-in-millis":             typecheck.TOptional(typecheck.TInteger()),
				"tls-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "server-name-to-verify", Type: typecheck.TString(), Optional: true},
				})),
			},
			Outputs: map[string]typecheck.Type{
				"api-id":         typecheck.TString(),
				"integration-id": typecheck.TString(),
				"integration-response-selection-expression": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.integration-type == 'AWS' || " +
						"var.integration-type == 'AWS_PROXY' || " +
						"var.integration-type == 'HTTP' || " +
						"var.integration-type == 'HTTP_PROXY' || " +
						"var.integration-type == 'MOCK')",
					Message: "integration-type must be AWS, AWS_PROXY, HTTP, HTTP_PROXY, or MOCK",
				},
				{
					Kind:    "predicate",
					When:    "(var.integration-subtype != null)",
					Require: "(var.integration-type == 'AWS_PROXY')",
					Message: "integration-subtype requires integration-type AWS_PROXY",
				},
				{
					Kind:    "predicate",
					When:    "(var.connection-type == 'VPC_LINK')",
					Require: "(var.connection-id != null)",
					Message: "connection-type VPC_LINK requires connection-id",
				},
				{
					Kind: "predicate",
					When: "(var.connection-type != null)",
					Require: "(var.connection-type == 'INTERNET' || " +
						"var.connection-type == 'VPC_LINK')",
					Message: "connection-type must be INTERNET or VPC_LINK",
				},
				{
					Kind: "predicate",
					When: "(var.content-handling-strategy != null)",
					Require: "(var.content-handling-strategy == 'CONVERT_TO_BINARY' || " +
						"var.content-handling-strategy == 'CONVERT_TO_TEXT')",
					Message: "content-handling-strategy must be CONVERT_TO_BINARY or CONVERT_TO_TEXT",
				},
				{
					Kind: "predicate",
					When: "(var.passthrough-behavior != null)",
					Require: "(var.passthrough-behavior == 'WHEN_NO_MATCH' || " +
						"var.passthrough-behavior == 'NEVER' || " +
						"var.passthrough-behavior == 'WHEN_NO_TEMPLATES')",
					Message: "passthrough-behavior must be WHEN_NO_MATCH, NEVER, or WHEN_NO_TEMPLATES",
				},
				{
					Kind: "predicate",
					When: "(var.payload-format-version != null)",
					Require: "(var.payload-format-version == '1.0' || " +
						"var.payload-format-version == '2.0')",
					Message: "payload-format-version must be 1.0 or 2.0",
				},
				{
					Kind: "predicate",
					When: "(var.integration-method != null)",
					Require: "(var.integration-method == 'ANY' || " +
						"var.integration-method == 'DELETE' || " +
						"var.integration-method == 'GET' || " +
						"var.integration-method == 'HEAD' || " +
						"var.integration-method == 'OPTIONS' || " +
						"var.integration-method == 'PATCH' || " +
						"var.integration-method == 'POST' || " +
						"var.integration-method == 'PUT')",
					Message: "integration-method must be a valid HTTP method",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.request-parameters", Optional: true},
				{Field: "var.request-templates", Optional: true},
				{Field: "var.response-parameters", Optional: true},
			},
		},
		"apigatewayv2-route": {
			Inputs: map[string]typecheck.Type{
				"api-id":                              typecheck.TString(),
				"api-key-required":                    typecheck.TOptional(typecheck.TBoolean()),
				"authorization-scopes":                typecheck.TList(typecheck.TString()),
				"authorization-type":                  typecheck.TOptional(typecheck.TString()),
				"authorizer-id":                       typecheck.TOptional(typecheck.TString()),
				"model-selection-expression":          typecheck.TOptional(typecheck.TString()),
				"operation-name":                      typecheck.TOptional(typecheck.TString()),
				"request-models":                      typecheck.TMap(typecheck.TString()),
				"request-parameters":                  typecheck.TMap(typecheck.TBoolean()),
				"route-key":                           typecheck.TString(),
				"route-response-selection-expression": typecheck.TOptional(typecheck.TString()),
				"target":                              typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"api-id":   typecheck.TString(),
				"route-id": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(var.authorization-type != null)",
					Require: "(var.authorization-type == 'NONE' || " +
						"var.authorization-type == 'AWS_IAM' || " +
						"var.authorization-type == 'CUSTOM' || " +
						"var.authorization-type == 'JWT')",
					Message: "authorization-type must be NONE, AWS_IAM, CUSTOM, or JWT",
				},
				{
					Kind: "predicate",
					When: "(var.authorization-type == 'CUSTOM' || " +
						"var.authorization-type == 'JWT')",
					Require: "(var.authorizer-id != null)",
					Message: "authorizer-id is required when authorization-type is CUSTOM or JWT",
				},
				{
					Kind: "predicate",
					When: "(var.operation-name != null)",
					Require: "((var.operation-name != null) && " +
						"(@core.length(var.operation-name) >= 1))",
					Message: "operation-name must not be empty",
				},
				{
					Kind:    "predicate",
					When:    "(var.target != null)",
					Require: "((var.target != null) && (@core.length(var.target) >= 1))",
					Message: "target must not be empty",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.authorization-scopes", Optional: true},
				{Field: "var.request-models", Optional: true},
				{Field: "var.request-parameters", Optional: true},
			},
		},
		"apigatewayv2-stage": {
			Inputs: map[string]typecheck.Type{
				"access-log-settings": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "destination-arn", Type: typecheck.TString()},
					{Name: "format", Type: typecheck.TString()},
				})),
				"api-id":                typecheck.TString(),
				"auto-deploy":           typecheck.TOptional(typecheck.TBoolean()),
				"client-certificate-id": typecheck.TOptional(typecheck.TString()),
				"default-route-settings": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "data-trace-enabled", Type: typecheck.TBoolean(), Optional: true},
					{Name: "detailed-metrics-enabled", Type: typecheck.TBoolean(), Optional: true},
					{Name: "logging-level", Type: typecheck.TString(), Optional: true},
					{Name: "throttling-burst-limit", Type: typecheck.TInteger(), Optional: true},
					{Name: "throttling-rate-limit", Type: typecheck.TNumber(), Optional: true},
				})),
				"deployment-id": typecheck.TOptional(typecheck.TString()),
				"description":   typecheck.TOptional(typecheck.TString()),
				"name":          typecheck.TString(),
				"route-settings": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "route-key", Type: typecheck.TString()},
					{Name: "data-trace-enabled", Type: typecheck.TBoolean(), Optional: true},
					{Name: "detailed-metrics-enabled", Type: typecheck.TBoolean(), Optional: true},
					{Name: "logging-level", Type: typecheck.TString(), Optional: true},
					{Name: "throttling-burst-limit", Type: typecheck.TInteger(), Optional: true},
					{Name: "throttling-rate-limit", Type: typecheck.TNumber(), Optional: true},
				})),
				"stage-variables": typecheck.TMap(typecheck.TString()),
				"tags":            typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"api-id":        typecheck.TString(),
				"arn":           typecheck.TString(),
				"deployment-id": typecheck.TString(),
				"invoke-url":    typecheck.TString(),
				"name":          typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "(var.auto-deploy == true)",
					Require: "(var.deployment-id == null)",
					Message: "deployment-id cannot be set when auto-deploy is enabled",
				},
				{
					Kind: "predicate",
					When: "(var.default-route-settings.logging-level != null)",
					Require: "(var.default-route-settings.logging-level == 'ERROR' || " +
						"var.default-route-settings.logging-level == 'INFO' || " +
						"var.default-route-settings.logging-level == 'OFF')",
					Message: "default-route-settings logging-level must be ERROR, INFO, or OFF",
				},
				{
					Kind: "predicate",
					When: "(@each.value.logging-level != null)",
					Require: "(@each.value.logging-level == 'ERROR' || " +
						"@each.value.logging-level == 'INFO' || " +
						"@each.value.logging-level == 'OFF')",
					Message: "route-settings logging-level must be ERROR, INFO, or OFF",
					ForEach: "var.route-settings",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.route-settings", Optional: true},
				{Field: "var.stage-variables", Optional: true},
				{Field: "var.tags", Optional: true},
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
