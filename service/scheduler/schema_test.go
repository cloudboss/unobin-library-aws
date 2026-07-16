package scheduler_test

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/scheduler"
)

func TestScheduleSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 1)
	require.Contains(t, schema.Resources, "schedule")
	got := schema.Resources["schedule"]

	assert.Equal(t, expectedScheduleInputs(), got.Inputs)
	assert.Equal(t, map[string]typecheck.Type{
		"arn": typecheck.TString(),
	}, got.Outputs)
	assert.Empty(t, got.SensitiveInputs)
	assert.Empty(t, got.SensitiveOutputs)
	assert.Empty(t, got.Defaults)

	messages := make(map[string]lang.ConstraintSpec, len(got.Constraints))
	for _, value := range got.Constraints {
		messages[value.Message] = value
	}
	for _, message := range []string{
		"action-after-completion must be DELETE or NONE",
		"flexible-time-window mode must be OFF or FLEXIBLE",
		"maximum-window-in-minutes must be between 1 and 1440",
		"FLEXIBLE mode requires maximum-window-in-minutes",
		"state must be ENABLED or DISABLED",
		"target input must not be empty",
		"maximum-event-age-in-seconds must be between 60 and 86400",
		"maximum-retry-attempts must be between 0 and 185",
		"task-count must be between 1 and 10",
		"launch-type must be EC2, FARGATE, or EXTERNAL",
		"capacity-provider-strategy holds at most 6 entries",
		"capacity provider base must be between 0 and 100000",
		"capacity provider weight must be between 0 and 1000",
		"FARGATE requires networking and forbids placement constraints",
		"assign-public-ip true requires FARGATE",
		"platform-version requires FARGATE",
		"networking requires 1 to 16 subnets and at most 5 security groups",
		"placement-constraints holds at most 10 entries",
		"placement constraint type must be distinctInstance or memberOf",
		"memberOf requires an expression",
		"distinctInstance forbids an expression",
		"placement-strategy holds at most 5 entries",
		"placement strategy type must be random, spread, or binpack",
		"propagate-tags must be TASK_DEFINITION",
		"ECS tags holds at most 50 entries",
		"pipeline-parameter-list holds at most 200 entries",
	} {
		assert.Contains(t, messages, message)
	}

	foundTargetExclusivity := false
	foundCapacityConflict := false
	for _, value := range got.Constraints {
		switch value.Kind {
		case "at-most-one-of":
			assert.Equal(t, []string{
				"input.target.ecs-parameters",
				"input.target.eventbridge-parameters",
				"input.target.kinesis-parameters",
				"input.target.sage-maker-pipeline-parameters",
				"input.target.sqs-parameters",
			}, value.Fields)
			foundTargetExclusivity = true
		case "forbidden-with":
			assert.Equal(t, []string{
				"input.target.ecs-parameters.launch-type",
				"input.target.ecs-parameters.capacity-provider-strategy",
			}, value.Fields)
			foundCapacityConflict = true
		}
	}
	assert.True(t, foundTargetExclusivity)
	assert.True(t, foundCapacityConflict)
}

func TestScheduleReplacementFieldsFromRegistration(t *testing.T) {
	resource := &svc.ScheduleResource{}
	assert.Equal(t, []string{"name", "group-name"}, resource.ReplaceFields())
}

func expectedScheduleInputs() map[string]typecheck.Type {
	window := typecheck.TObject([]typecheck.ObjectField{
		{Name: "mode", Type: typecheck.TString()},
		{Name: "maximum-window-in-minutes", Type: typecheck.TInteger(), Optional: true},
	})
	deadLetter := typecheck.TObject([]typecheck.ObjectField{
		{Name: "arn", Type: typecheck.TString()},
	})
	capacity := typecheck.TObject([]typecheck.ObjectField{
		{Name: "capacity-provider", Type: typecheck.TString()},
		{Name: "base", Type: typecheck.TInteger(), Optional: true},
		{Name: "weight", Type: typecheck.TInteger(), Optional: true},
	})
	network := typecheck.TObject([]typecheck.ObjectField{
		{Name: "subnets", Type: typecheck.TList(typecheck.TString())},
		{
			Name:     "security-groups",
			Type:     typecheck.TList(typecheck.TString()),
			Optional: true,
		},
		{Name: "assign-public-ip", Type: typecheck.TBoolean(), Optional: true},
	})
	placementConstraint := typecheck.TObject([]typecheck.ObjectField{
		{Name: "type", Type: typecheck.TString()},
		{Name: "expression", Type: typecheck.TString(), Optional: true},
	})
	placementStrategy := typecheck.TObject([]typecheck.ObjectField{
		{Name: "type", Type: typecheck.TString()},
		{Name: "field", Type: typecheck.TString(), Optional: true},
	})
	ecs := typecheck.TObject([]typecheck.ObjectField{
		{Name: "task-definition-arn", Type: typecheck.TString()},
		{Name: "task-count", Type: typecheck.TInteger(), Optional: true},
		{Name: "launch-type", Type: typecheck.TString(), Optional: true},
		{Name: "network-configuration", Type: network, Optional: true},
		{Name: "platform-version", Type: typecheck.TString(), Optional: true},
		{Name: "group", Type: typecheck.TString(), Optional: true},
		{
			Name:     "capacity-provider-strategy",
			Type:     typecheck.TList(capacity),
			Optional: true,
		},
		{Name: "enable-ecs-managed-tags", Type: typecheck.TBoolean(), Optional: true},
		{Name: "enable-execute-command", Type: typecheck.TBoolean(), Optional: true},
		{
			Name:     "placement-constraints",
			Type:     typecheck.TList(placementConstraint),
			Optional: true,
		},
		{
			Name:     "placement-strategy",
			Type:     typecheck.TList(placementStrategy),
			Optional: true,
		},
		{Name: "propagate-tags", Type: typecheck.TString(), Optional: true},
		{Name: "reference-id", Type: typecheck.TString(), Optional: true},
		{Name: "tags", Type: typecheck.TMap(typecheck.TString()), Optional: true},
	})
	eventBridge := typecheck.TObject([]typecheck.ObjectField{
		{Name: "detail-type", Type: typecheck.TString()},
		{Name: "source", Type: typecheck.TString()},
	})
	kinesis := typecheck.TObject([]typecheck.ObjectField{
		{Name: "partition-key", Type: typecheck.TString()},
	})
	retryPolicy := typecheck.TObject([]typecheck.ObjectField{
		{
			Name:     "maximum-event-age-in-seconds",
			Type:     typecheck.TInteger(),
			Optional: true,
		},
		{Name: "maximum-retry-attempts", Type: typecheck.TInteger(), Optional: true},
	})
	pipelineParameter := typecheck.TObject([]typecheck.ObjectField{
		{Name: "name", Type: typecheck.TString()},
		{Name: "value", Type: typecheck.TString()},
	})
	sageMaker := typecheck.TObject([]typecheck.ObjectField{
		{
			Name:     "pipeline-parameter-list",
			Type:     typecheck.TList(pipelineParameter),
			Optional: true,
		},
	})
	sqs := typecheck.TObject([]typecheck.ObjectField{
		{Name: "message-group-id", Type: typecheck.TString(), Optional: true},
	})
	target := typecheck.TObject([]typecheck.ObjectField{
		{Name: "arn", Type: typecheck.TString()},
		{Name: "role-arn", Type: typecheck.TString()},
		{Name: "dead-letter-config", Type: deadLetter, Optional: true},
		{Name: "ecs-parameters", Type: ecs, Optional: true},
		{Name: "eventbridge-parameters", Type: eventBridge, Optional: true},
		{Name: "input", Type: typecheck.TString(), Optional: true},
		{Name: "kinesis-parameters", Type: kinesis, Optional: true},
		{Name: "retry-policy", Type: retryPolicy, Optional: true},
		{Name: "sage-maker-pipeline-parameters", Type: sageMaker, Optional: true},
		{Name: "sqs-parameters", Type: sqs, Optional: true},
	})
	return map[string]typecheck.Type{
		"action-after-completion":      typecheck.TOptional(typecheck.TString()),
		"description":                  typecheck.TOptional(typecheck.TString()),
		"end-date":                     typecheck.TOptional(typecheck.TString()),
		"flexible-time-window":         window,
		"group-name":                   typecheck.TOptional(typecheck.TString()),
		"kms-key-arn":                  typecheck.TOptional(typecheck.TString()),
		"name":                         typecheck.TOptional(typecheck.TString()),
		"schedule-expression":          typecheck.TString(),
		"schedule-expression-timezone": typecheck.TOptional(typecheck.TString()),
		"start-date":                   typecheck.TOptional(typecheck.TString()),
		"state":                        typecheck.TOptional(typecheck.TString()),
		"target":                       target,
	}
}
