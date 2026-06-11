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
	"github.com/cloudboss/unobin-library-aws/internal/service/eventbridge"
)

// TestLibraryRegistersEventbridge checks the runtime registration: the rule and
// target resources are present under Resources and dispatch to their output
// types.
func TestLibraryRegistersEventbridge(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"eventbridge-rule":   reflect.TypeFor[*eventbridge.RuleOutput](),
		"eventbridge-target": reflect.TypeFor[*eventbridge.TargetOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestEventbridgeSchemas asserts the whole TypeSchema goschema reads from this
// library's source for the rule and target: the input and output field types,
// that nothing is marked sensitive, and the cross-field constraints derived from
// each Constraints method. A target's parameter blocks are nested objects, and
// the only rule goschema can derive over them is the top-level at-most-one-of on
// the three input forms; every inner bound is enforced by the EventBridge API.
// Nested object fields are listed in goschema's declaration order, which the
// comparison checks directly.
func TestEventbridgeSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"eventbridge-rule": {
			Inputs: map[string]typecheck.Type{
				"name":                typecheck.TString(),
				"event-bus-name":      typecheck.TOptional(typecheck.TString()),
				"description":         typecheck.TOptional(typecheck.TString()),
				"event-pattern":       typecheck.TOptional(typecheck.TString()),
				"schedule-expression": typecheck.TOptional(typecheck.TString()),
				"role-arn":            typecheck.TOptional(typecheck.TString()),
				"state":               typecheck.TOptional(typecheck.TString()),
				"tags":                typecheck.TMap(typecheck.TString()),
				"force-destroy":       typecheck.TOptional(typecheck.TBoolean()),
			},
			Outputs: map[string]typecheck.Type{
				"arn": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "at-least-one-of",
					Fields: []string{"var.event-pattern", "var.schedule-expression"},
				},
				{
					Kind: "predicate",
					When: "(var.state != null)",
					Require: "(var.state == 'ENABLED' || var.state == 'DISABLED' || " +
						"var.state == 'ENABLED_WITH_ALL_CLOUDTRAIL_MANAGEMENT_EVENTS')",
					Message: "state must be ENABLED, DISABLED, or " +
						"ENABLED_WITH_ALL_CLOUDTRAIL_MANAGEMENT_EVENTS",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.tags", Optional: true},
			},
		},
		"eventbridge-target": {
			Inputs: map[string]typecheck.Type{
				"rule":           typecheck.TString(),
				"arn":            typecheck.TString(),
				"event-bus-name": typecheck.TOptional(typecheck.TString()),
				"target-id":      typecheck.TOptional(typecheck.TString()),
				"role-arn":       typecheck.TOptional(typecheck.TString()),
				"input":          typecheck.TOptional(typecheck.TString()),
				"input-path":     typecheck.TOptional(typecheck.TString()),
				"input-transformer": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "input-template", Type: typecheck.TString(), Optional: false},
					{Name: "input-paths", Type: typecheck.TMap(typecheck.TString()), Optional: false},
				})),
				"retry-policy": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "maximum-event-age-in-seconds", Type: typecheck.TInteger(), Optional: true},
					{Name: "maximum-retry-attempts", Type: typecheck.TInteger(), Optional: true},
				})),
				"dead-letter-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "arn", Type: typecheck.TString(), Optional: true},
				})),
				"ecs-parameters": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "task-definition-arn", Type: typecheck.TString(), Optional: false},
					{Name: "task-count", Type: typecheck.TInteger(), Optional: true},
					{Name: "launch-type", Type: typecheck.TString(), Optional: true},
					{Name: "platform-version", Type: typecheck.TString(), Optional: true},
					{Name: "group", Type: typecheck.TString(), Optional: true},
					{Name: "enable-ecs-managed-tags", Type: typecheck.TBoolean(), Optional: true},
					{Name: "enable-execute-command", Type: typecheck.TBoolean(), Optional: true},
					{Name: "propagate-tags", Type: typecheck.TString(), Optional: true},
					{Name: "network-configuration", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "subnets", Type: typecheck.TList(typecheck.TString()), Optional: false},
						{Name: "security-groups", Type: typecheck.TList(typecheck.TString()),
							Optional: false},
						{Name: "assign-public-ip", Type: typecheck.TBoolean(), Optional: true},
					}), Optional: true},
					{Name: "capacity-provider-strategy",
						Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "capacity-provider", Type: typecheck.TString(), Optional: false},
							{Name: "base", Type: typecheck.TInteger(), Optional: true},
							{Name: "weight", Type: typecheck.TInteger(), Optional: true},
						})), Optional: false},
					{Name: "placement-constraints",
						Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "type", Type: typecheck.TString(), Optional: false},
							{Name: "expression", Type: typecheck.TString(), Optional: true},
						})), Optional: false},
					{Name: "placement-strategy",
						Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "type", Type: typecheck.TString(), Optional: false},
							{Name: "field", Type: typecheck.TString(), Optional: true},
						})), Optional: false},
					{Name: "tags", Type: typecheck.TMap(typecheck.TString()), Optional: false},
				})),
				"batch-parameters": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "job-definition", Type: typecheck.TString(), Optional: false},
					{Name: "job-name", Type: typecheck.TString(), Optional: false},
					{Name: "array-size", Type: typecheck.TInteger(), Optional: true},
					{Name: "job-attempts", Type: typecheck.TInteger(), Optional: true},
				})),
				"kinesis-parameters": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "partition-key-path", Type: typecheck.TString(), Optional: true},
				})),
				"sqs-parameters": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "message-group-id", Type: typecheck.TString(), Optional: true},
				})),
				"http-parameters": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "header-parameters", Type: typecheck.TMap(typecheck.TString()), Optional: false},
					{Name: "query-string-parameters", Type: typecheck.TMap(typecheck.TString()),
						Optional: false},
					{Name: "path-parameter-values", Type: typecheck.TList(typecheck.TString()),
						Optional: false},
				})),
				"redshift-data-parameters": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "database", Type: typecheck.TString(), Optional: false},
					{Name: "sql", Type: typecheck.TString(), Optional: false},
					{Name: "db-user", Type: typecheck.TString(), Optional: true},
					{Name: "statement-name", Type: typecheck.TString(), Optional: true},
					{Name: "secrets-manager-arn", Type: typecheck.TString(), Optional: true},
					{Name: "with-event", Type: typecheck.TBoolean(), Optional: true},
				})),
				"run-command-parameters": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "run-command-targets",
						Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "key", Type: typecheck.TString(), Optional: false},
							{Name: "values", Type: typecheck.TList(typecheck.TString()), Optional: false},
						})), Optional: false},
				})),
				"sage-maker-pipeline-parameters": typecheck.TOptional(
					typecheck.TObject([]typecheck.ObjectField{
						{Name: "pipeline-parameter-list",
							Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
								{Name: "name", Type: typecheck.TString(), Optional: false},
								{Name: "value", Type: typecheck.TString(), Optional: false},
							})), Optional: false},
					})),
				"app-sync-parameters": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "graphql-operation", Type: typecheck.TString(), Optional: true},
				})),
				"force-destroy": typecheck.TOptional(typecheck.TBoolean()),
			},
			Outputs: map[string]typecheck.Type{
				"target-id": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "at-most-one-of",
					Fields: []string{"var.input", "var.input-path", "var.input-transformer"},
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.input-transformer.input-paths == null || " +
						"@core.length(var.input-transformer.input-paths) <= 100)",
					Message: "input-paths holds at most 100 entries",
				},
				{
					Kind: "predicate",
					When: "(var.retry-policy.maximum-event-age-in-seconds != null)",
					Require: "(var.retry-policy.maximum-event-age-in-seconds == null || " +
						"var.retry-policy.maximum-event-age-in-seconds >= 0) && " +
						"(var.retry-policy.maximum-event-age-in-seconds == null || " +
						"var.retry-policy.maximum-event-age-in-seconds <= 86400)",
					Message: "maximum-event-age-in-seconds must be between 0 and 86400",
				},
				{
					Kind: "predicate",
					When: "(var.retry-policy.maximum-retry-attempts != null)",
					Require: "(var.retry-policy.maximum-retry-attempts == null || " +
						"var.retry-policy.maximum-retry-attempts >= 0) && " +
						"(var.retry-policy.maximum-retry-attempts == null || " +
						"var.retry-policy.maximum-retry-attempts <= 185)",
					Message: "maximum-retry-attempts must be between 0 and 185",
				},
				{
					Kind: "predicate",
					When: "(var.batch-parameters.array-size != null)",
					Require: "(var.batch-parameters.array-size == null || " +
						"var.batch-parameters.array-size >= 2) && (var.batch-parameters.array-size == null || " +
						"var.batch-parameters.array-size <= 10000)",
					Message: "array-size must be between 2 and 10000",
				},
				{
					Kind: "predicate",
					When: "(var.batch-parameters.job-attempts != null)",
					Require: "(var.batch-parameters.job-attempts == null || " +
						"var.batch-parameters.job-attempts >= 1) && (var.batch-parameters.job-attempts == null || " +
						"var.batch-parameters.job-attempts <= 10)",
					Message: "job-attempts must be between 1 and 10",
				},
				{
					Kind: "predicate",
					When: "(var.ecs-parameters.launch-type != null)",
					Require: "(var.ecs-parameters.launch-type == 'EC2' || " +
						"var.ecs-parameters.launch-type == 'FARGATE' || " +
						"var.ecs-parameters.launch-type == 'EXTERNAL')",
					Message: "launch-type must be EC2, FARGATE, or EXTERNAL",
				},
				{
					Kind:    "predicate",
					When:    "(var.ecs-parameters.propagate-tags != null)",
					Require: "(var.ecs-parameters.propagate-tags == 'TASK_DEFINITION')",
					Message: "propagate-tags must be TASK_DEFINITION",
				},
				{
					Kind:    "predicate",
					When:    "(var.ecs-parameters.network-configuration != null)",
					Require: "(var.ecs-parameters.network-configuration.subnets != null)",
					Message: "an ECS network configuration requires subnets",
				},
				{
					Kind: "predicate",
					When: "(@each.value.base != null)",
					Require: "(@each.value.base == null || " +
						"@each.value.base >= 0) && (@each.value.base == null || @each.value.base <= 100000)",
					Message: "a capacity provider base must be between 0 and 100000",
					ForEach: "var.ecs-parameters.capacity-provider-strategy",
				},
				{
					Kind: "predicate",
					When: "(@each.value.weight != null)",
					Require: "(@each.value.weight == null || " +
						"@each.value.weight >= 0) && (@each.value.weight == null || @each.value.weight <= 1000)",
					Message: "a capacity provider weight must be between 0 and 1000",
					ForEach: "var.ecs-parameters.capacity-provider-strategy",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@each.value.type == 'distinctInstance' || @each.value.type == 'memberOf')",
					Message: "a placement constraint type must be distinctInstance or memberOf",
					ForEach: "var.ecs-parameters.placement-constraints",
				},
				{
					Kind:    "predicate",
					When:    "(@each.value.type == 'memberOf')",
					Require: "(@each.value.expression != null)",
					Message: "a memberOf placement constraint requires an expression",
					ForEach: "var.ecs-parameters.placement-constraints",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.type == 'random' || @each.value.type == 'spread' || " +
						"@each.value.type == 'binpack')",
					Message: "a placement strategy type must be random, spread, or binpack",
					ForEach: "var.ecs-parameters.placement-strategy",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.run-command-parameters.run-command-targets == null || " +
						"@core.length(var.run-command-parameters.run-command-targets) <= 5)",
					Message: "run-command-targets holds at most 5 entries",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "((@each.value.values != null) && " +
						"(@core.length(@each.value.values) >= 1)) && " +
						"(@each.value.values == null || @core.length(@each.value.values) <= 50)",
					Message: "a run command target takes 1 to 50 values",
					ForEach: "var.run-command-parameters.run-command-targets",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.sage-maker-pipeline-parameters.pipeline-parameter-list == null || " +
						"@core.length(var.sage-maker-pipeline-parameters.pipeline-parameter-list) <= 200)",
					Message: "pipeline-parameter-list holds at most 200 entries",
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
}
