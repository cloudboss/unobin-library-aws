package autoscaling

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/autoscaling"
)

// TestLibraryRegistersAutoscalingGroup checks the runtime registration:
// autoscaling-group is in the resource map with the Group output type.
func TestLibraryRegistersAutoscalingGroup(t *testing.T) {
	lib := Library()
	require.Contains(t, lib.Resources, "group")
	assert.Equal(t, reflect.TypeFor[*svc.GroupResourceOutput](),
		lib.Resources["group"].OutputType())
}

// TestAutoscalingGroupSchema asserts the whole derived TypeSchema for
// autoscaling-group: the input and output field types, that nothing is
// sensitive, the cross-field constraints derived from the Constraints
// method, and the declared optional defaults.
func TestAutoscalingGroupSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Contains(t, schema.Resources, "group")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"availability-zones":        typecheck.TOptional(typecheck.TList(typecheck.TString())),
			"capacity-rebalance":        typecheck.TOptional(typecheck.TBoolean()),
			"default-cooldown":          typecheck.TOptional(typecheck.TInteger()),
			"default-instance-warmup":   typecheck.TOptional(typecheck.TInteger()),
			"desired-capacity":          typecheck.TOptional(typecheck.TInteger()),
			"desired-capacity-type":     typecheck.TOptional(typecheck.TString()),
			"enabled-metrics":           typecheck.TOptional(typecheck.TList(typecheck.TString())),
			"force-delete":              typecheck.TOptional(typecheck.TBoolean()),
			"health-check-grace-period": typecheck.TOptional(typecheck.TInteger()),
			"health-check-type":         typecheck.TOptional(typecheck.TString()),
			"instance-maintenance-policy": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
				{Name: "min-healthy-percentage", Type: typecheck.TInteger()},
				{Name: "max-healthy-percentage", Type: typecheck.TInteger()},
			})),
			"launch-template": typecheck.TObject([]typecheck.ObjectField{
				{Name: "id", Type: typecheck.TString(), Optional: true},
				{Name: "name", Type: typecheck.TString(), Optional: true},
				{Name: "version", Type: typecheck.TString(), Optional: true},
			}),
			"max-instance-lifetime":   typecheck.TOptional(typecheck.TInteger()),
			"max-size":                typecheck.TInteger(),
			"metrics-granularity":     typecheck.TOptional(typecheck.TString()),
			"min-size":                typecheck.TInteger(),
			"name":                    typecheck.TString(),
			"placement-group":         typecheck.TOptional(typecheck.TString()),
			"protect-from-scale-in":   typecheck.TOptional(typecheck.TBoolean()),
			"service-linked-role-arn": typecheck.TOptional(typecheck.TString()),
			"suspended-processes":     typecheck.TOptional(typecheck.TList(typecheck.TString())),
			"tags": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
				{Name: "key", Type: typecheck.TString()},
				{Name: "value", Type: typecheck.TString()},
				{Name: "propagate-at-launch", Type: typecheck.TBoolean()},
			}))),
			"target-group-arns":         typecheck.TOptional(typecheck.TList(typecheck.TString())),
			"termination-policies":      typecheck.TOptional(typecheck.TList(typecheck.TString())),
			"vpc-zone-identifier":       typecheck.TOptional(typecheck.TList(typecheck.TString())),
			"wait-for-capacity-timeout": typecheck.TOptional(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"arn":                     typecheck.TString(),
			"availability-zones":      typecheck.TList(typecheck.TString()),
			"default-cooldown":        typecheck.TInteger(),
			"desired-capacity":        typecheck.TInteger(),
			"health-check-type":       typecheck.TString(),
			"service-linked-role-arn": typecheck.TString(),
			"vpc-zone-identifier":     typecheck.TList(typecheck.TString()),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind:    "predicate",
				When:    "true",
				Require: "(input.min-size >= 0)",
				Message: "min-size must be zero or greater",
			},
			{
				Kind:    "predicate",
				When:    "true",
				Require: "(input.max-size >= 0)",
				Message: "max-size must be zero or greater",
			},
			{
				Kind: "predicate",
				When: "true",
				Require: "(!((input.availability-zones != null) && " +
					"(@core.length(input.availability-zones) >= 1)) || " +
					"!((input.vpc-zone-identifier != null) && " +
					"(@core.length(input.vpc-zone-identifier) >= 1)))",
				Message: "availability-zones and vpc-zone-identifier are mutually exclusive",
			},
			{
				Kind: "predicate",
				When: "true",
				Require: "(((input.availability-zones != null) && " +
					"(@core.length(input.availability-zones) >= 1)) || " +
					"((input.vpc-zone-identifier != null) && " +
					"(@core.length(input.vpc-zone-identifier) >= 1)))",
				Message: "one of availability-zones or vpc-zone-identifier is required",
			},
			{
				Kind:   "at-most-one-of",
				Fields: []string{"input.launch-template.id", "input.launch-template.name"},
			},
			{
				Kind:    "predicate",
				When:    "true",
				Require: "((input.launch-template.id != null) || (input.launch-template.name != null))",
				Message: "launch-template requires one of id or name",
			},
			{
				Kind: "predicate",
				When: "(input.desired-capacity-type != null)",
				Require: "(input.desired-capacity-type == 'units' || input.desired-capacity-type == 'vcpu' || " +
					"input.desired-capacity-type == 'memory-mib')",
				Message: "desired-capacity-type must be units, vcpu, or memory-mib",
			},
			{
				Kind: "predicate",
				When: "(input.health-check-type != null)",
				Require: "(input.health-check-type == 'EC2' || input.health-check-type == 'ELB' || " +
					"input.health-check-type == 'VPC_LATTICE')",
				Message: "health-check-type must be EC2, ELB, or VPC_LATTICE",
			},
			{
				Kind:    "predicate",
				When:    "(input.metrics-granularity != null)",
				Require: "(input.metrics-granularity == '1Minute')",
				Message: "metrics-granularity must be 1Minute",
			},
		},
	}
	assertTypeSchemaEqual(t, want,
		schema.Resources["group"])
}

// TestLibraryRegistersAutoscalingScaling checks the runtime registration of the
// scaling-policy and lifecycle-hook resources that complete the group.
func TestLibraryRegistersAutoscalingScaling(t *testing.T) {
	lib := Library()
	resources := map[string]reflect.Type{
		"policy":         reflect.TypeFor[*svc.PolicyResourceOutput](),
		"lifecycle-hook": reflect.TypeFor[*svc.LifecycleHookResourceOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestAutoscalingScalingSchemas asserts the whole derived TypeSchema for the
// scaling policy (with its target-tracking block and policy-type gating) and the
// lifecycle hook.
func TestAutoscalingScalingSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"policy": {
			Inputs: map[string]typecheck.Type{
				"adjustment-type":           typecheck.TOptional(typecheck.TString()),
				"autoscaling-group-name":    typecheck.TString(),
				"cooldown":                  typecheck.TOptional(typecheck.TInteger()),
				"enabled":                   typecheck.TOptional(typecheck.TBoolean()),
				"estimated-instance-warmup": typecheck.TOptional(typecheck.TInteger()),
				"metric-aggregation-type":   typecheck.TOptional(typecheck.TString()),
				"min-adjustment-magnitude":  typecheck.TOptional(typecheck.TInteger()),
				"name":                      typecheck.TString(),
				"policy-type":               typecheck.TOptional(typecheck.TString()),
				"scaling-adjustment":        typecheck.TOptional(typecheck.TInteger()),
				"step-adjustments": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "scaling-adjustment", Type: typecheck.TInteger()},
					{Name: "metric-interval-lower-bound", Type: typecheck.TNumber(), Optional: true},
					{Name: "metric-interval-upper-bound", Type: typecheck.TNumber(), Optional: true},
				}))),
				"target-tracking-configuration": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "target-value", Type: typecheck.TNumber()},
					{Name: "disable-scale-in", Type: typecheck.TBoolean(), Optional: true},
					{Name: "predefined-metric-specification", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "predefined-metric-type", Type: typecheck.TString()},
						{Name: "resource-label", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
					{Name: "customized-metric-specification", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "metric-name", Type: typecheck.TString()},
						{Name: "namespace", Type: typecheck.TString()},
						{Name: "statistic", Type: typecheck.TString()},
						{Name: "unit", Type: typecheck.TString(), Optional: true},
						{Name: "period", Type: typecheck.TInteger(), Optional: true},
						{Name: "dimensions", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "name", Type: typecheck.TString()},
							{Name: "value", Type: typecheck.TString()},
						}))},
						{Name: "metrics", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "id", Type: typecheck.TString()},
							{Name: "expression", Type: typecheck.TString(), Optional: true},
							{Name: "label", Type: typecheck.TString(), Optional: true},
							{Name: "period", Type: typecheck.TInteger(), Optional: true},
							{Name: "return-data", Type: typecheck.TBoolean(), Optional: true},
							{Name: "metric-stat", Type: typecheck.TObject([]typecheck.ObjectField{
								{Name: "metric", Type: typecheck.TObject([]typecheck.ObjectField{
									{Name: "metric-name", Type: typecheck.TString()},
									{Name: "namespace", Type: typecheck.TString()},
									{Name: "dimensions", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
										{Name: "name", Type: typecheck.TString()},
										{Name: "value", Type: typecheck.TString()},
									}))},
								})},
								{Name: "stat", Type: typecheck.TString()},
								{Name: "period", Type: typecheck.TInteger(), Optional: true},
								{Name: "unit", Type: typecheck.TString(), Optional: true},
							}), Optional: true},
						}))},
					}), Optional: true},
				})),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                     typecheck.TString(),
				"autoscaling-group-name":  typecheck.TString(),
				"metric-aggregation-type": typecheck.TString(),
				"name":                    typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(input.policy-type != null)",
					Require: "(input.policy-type == 'SimpleScaling' || " +
						"input.policy-type == 'StepScaling' || " +
						"input.policy-type == 'TargetTrackingScaling' || " +
						"input.policy-type == 'PredictiveScaling')",
					Message: "policy-type must be SimpleScaling, StepScaling, " +
						"TargetTrackingScaling, or PredictiveScaling",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "((input.scaling-adjustment == null) || " +
						"!((input.step-adjustments != null) && " +
						"(@core.length(input.step-adjustments) >= 1)))",
					Message: "scaling-adjustment and step-adjustments are mutually exclusive",
				},
				{
					Kind: "predicate",
					When: "((input.policy-type == null) || " +
						"(input.policy-type == 'SimpleScaling'))",
					Require: "(input.scaling-adjustment != null)",
					Message: "scaling-adjustment is required when policy-type is SimpleScaling",
				},
				{
					Kind: "predicate",
					When: "(input.scaling-adjustment != null)",
					Require: "((input.policy-type == null) || " +
						"(input.policy-type == 'SimpleScaling'))",
					Message: "scaling-adjustment is valid only when policy-type is SimpleScaling",
				},
				{
					Kind: "predicate",
					When: "(input.policy-type == 'StepScaling')",
					Require: "((input.step-adjustments != null) && " +
						"(@core.length(input.step-adjustments) >= 1))",
					Message: "step-adjustments is required when policy-type is StepScaling",
				},
				{
					Kind: "predicate",
					When: "((input.step-adjustments != null) && " +
						"(@core.length(input.step-adjustments) >= 1))",
					Require: "(input.policy-type == 'StepScaling')",
					Message: "step-adjustments is valid only when policy-type is StepScaling",
				},
				{
					Kind:    "predicate",
					When:    "(input.policy-type == 'TargetTrackingScaling')",
					Require: "(input.target-tracking-configuration != null)",
					Message: "target-tracking-configuration is required when policy-type " +
						"is TargetTrackingScaling",
				},
				{
					Kind:    "predicate",
					When:    "(input.target-tracking-configuration != null)",
					Require: "(input.policy-type == 'TargetTrackingScaling')",
					Message: "target-tracking-configuration is valid only when policy-type " +
						"is TargetTrackingScaling",
				},
				{
					Kind: "predicate",
					When: "(input.cooldown != null)",
					Require: "((input.policy-type == null) || " +
						"(input.policy-type == 'SimpleScaling'))",
					Message: "cooldown is valid only when policy-type is SimpleScaling",
				},
				{
					Kind: "predicate",
					When: "(input.estimated-instance-warmup != null)",
					Require: "(input.policy-type == 'StepScaling' || " +
						"input.policy-type == 'TargetTrackingScaling')",
					Message: "estimated-instance-warmup is valid only when policy-type is " +
						"StepScaling or TargetTrackingScaling",
				},
				{
					Kind: "predicate",
					When: "(input.min-adjustment-magnitude != null)",
					Require: "(input.min-adjustment-magnitude == null || " +
						"input.min-adjustment-magnitude >= 1)",
					Message: "min-adjustment-magnitude must be at least 1",
				},
			},
		},
		"lifecycle-hook": {
			Inputs: map[string]typecheck.Type{
				"autoscaling-group-name":  typecheck.TString(),
				"default-result":          typecheck.TOptional(typecheck.TString()),
				"heartbeat-timeout":       typecheck.TOptional(typecheck.TInteger()),
				"lifecycle-transition":    typecheck.TString(),
				"name":                    typecheck.TString(),
				"notification-metadata":   typecheck.TOptional(typecheck.TString()),
				"notification-target-arn": typecheck.TOptional(typecheck.TString()),
				"role-arn":                typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"autoscaling-group-name": typecheck.TString(),
				"default-result":         typecheck.TString(),
				"name":                   typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "true",
					Require: "(input.lifecycle-transition == 'autoscaling:EC2_INSTANCE_LAUNCHING' || " +
						"input.lifecycle-transition == 'autoscaling:EC2_INSTANCE_TERMINATING')",
					Message: "lifecycle-transition must be autoscaling:EC2_INSTANCE_LAUNCHING " +
						"or autoscaling:EC2_INSTANCE_TERMINATING",
				},
				{
					Kind: "predicate",
					When: "(input.default-result != null)",
					Require: "(input.default-result == 'CONTINUE' || " +
						"input.default-result == 'ABANDON')",
					Message: "default-result must be CONTINUE or ABANDON",
				},
				{
					Kind: "predicate",
					When: "(input.heartbeat-timeout != null)",
					Require: "(input.heartbeat-timeout == null || " +
						"input.heartbeat-timeout >= 30) && " +
						"(input.heartbeat-timeout == null || " +
						"input.heartbeat-timeout <= 7200)",
					Message: "heartbeat-timeout must be from 30 to 7200 seconds",
				},
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
