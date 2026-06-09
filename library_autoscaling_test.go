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
	"github.com/cloudboss/unobin-library-aws/internal/service/autoscaling"
)

// TestLibraryRegistersAutoscalingGroup checks the runtime registration:
// autoscaling-group is in the resource map with the Group output type.
func TestLibraryRegistersAutoscalingGroup(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "autoscaling-group")
	assert.Equal(t, reflect.TypeFor[*autoscaling.GroupOutput](),
		lib.Resources["autoscaling-group"].OutputType())
}

// TestAutoscalingGroupSchema asserts the whole derived TypeSchema for
// autoscaling-group: the input and output field types, that nothing is
// sensitive, the cross-field constraints derived from the Constraints
// method, and the declared optional defaults.
func TestAutoscalingGroupSchema(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.Resources, "autoscaling-group")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"availability-zones":        typecheck.TList(typecheck.TString()),
			"capacity-rebalance":        typecheck.TOptional(typecheck.TBoolean()),
			"default-cooldown":          typecheck.TOptional(typecheck.TInteger()),
			"default-instance-warmup":   typecheck.TOptional(typecheck.TInteger()),
			"desired-capacity":          typecheck.TOptional(typecheck.TInteger()),
			"desired-capacity-type":     typecheck.TOptional(typecheck.TString()),
			"enabled-metrics":           typecheck.TList(typecheck.TString()),
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
			"suspended-processes":     typecheck.TList(typecheck.TString()),
			"tags": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
				{Name: "key", Type: typecheck.TString()},
				{Name: "value", Type: typecheck.TString()},
				{Name: "propagate-at-launch", Type: typecheck.TBoolean()},
			})),
			"target-group-arns":         typecheck.TList(typecheck.TString()),
			"termination-policies":      typecheck.TList(typecheck.TString()),
			"vpc-zone-identifier":       typecheck.TList(typecheck.TString()),
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
				Require: "(var.min-size == null || var.min-size >= 0)",
				Message: "min-size must be zero or greater",
			},
			{
				Kind:    "predicate",
				When:    "true",
				Require: "(var.max-size == null || var.max-size >= 0)",
				Message: "max-size must be zero or greater",
			},
			{
				Kind:   "at-most-one-of",
				Fields: []string{"var.availability-zones", "var.vpc-zone-identifier"},
			},
			{
				Kind:    "predicate",
				When:    "true",
				Require: "((var.availability-zones != null) || (var.vpc-zone-identifier != null))",
				Message: "one of availability-zones or vpc-zone-identifier is required",
			},
			{
				Kind:   "at-most-one-of",
				Fields: []string{"var.launch-template.id", "var.launch-template.name"},
			},
			{
				Kind:    "predicate",
				When:    "true",
				Require: "((var.launch-template.id != null) || (var.launch-template.name != null))",
				Message: "launch-template requires one of id or name",
			},
			{
				Kind: "predicate",
				When: "(var.desired-capacity-type != null)",
				Require: "(var.desired-capacity-type == 'units' || var.desired-capacity-type == 'vcpu' || " +
					"var.desired-capacity-type == 'memory-mib')",
				Message: "desired-capacity-type must be units, vcpu, or memory-mib",
			},
			{
				Kind: "predicate",
				When: "(var.health-check-type != null)",
				Require: "(var.health-check-type == 'EC2' || var.health-check-type == 'ELB' || " +
					"var.health-check-type == 'VPC_LATTICE')",
				Message: "health-check-type must be EC2, ELB, or VPC_LATTICE",
			},
			{
				Kind:    "predicate",
				When:    "(var.metrics-granularity != null)",
				Require: "(var.metrics-granularity == '1Minute')",
				Message: "metrics-granularity must be 1Minute",
			},
		},
		Defaults: []lang.DefaultSpec{
			{Field: "var.availability-zones", Optional: true},
			{Field: "var.vpc-zone-identifier", Optional: true},
			{Field: "var.termination-policies", Optional: true},
			{Field: "var.tags", Optional: true},
			{Field: "var.suspended-processes", Optional: true},
			{Field: "var.enabled-metrics", Optional: true},
			{Field: "var.target-group-arns", Optional: true},
		},
	}
	assert.Equal(t, want,
		schema.Resources["autoscaling-group"])
}

// TestLibraryRegistersAutoscalingScaling checks the runtime registration of the
// scaling-policy and lifecycle-hook resources that complete the group.
func TestLibraryRegistersAutoscalingScaling(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"autoscaling-policy":         reflect.TypeFor[*autoscaling.PolicyOutput](),
		"autoscaling-lifecycle-hook": reflect.TypeFor[*autoscaling.LifecycleHookOutput](),
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
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	resources := map[string]*runtime.TypeSchema{
		"autoscaling-policy": {
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
				"step-adjustments": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "scaling-adjustment", Type: typecheck.TInteger()},
					{Name: "metric-interval-lower-bound", Type: typecheck.TNumber(), Optional: true},
					{Name: "metric-interval-upper-bound", Type: typecheck.TNumber(), Optional: true},
				})),
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
					When: "(var.policy-type != null)",
					Require: "(var.policy-type == 'SimpleScaling' || " +
						"var.policy-type == 'StepScaling' || " +
						"var.policy-type == 'TargetTrackingScaling' || " +
						"var.policy-type == 'PredictiveScaling')",
					Message: "policy-type must be SimpleScaling, StepScaling, " +
						"TargetTrackingScaling, or PredictiveScaling",
				},
				{
					Kind:   "at-most-one-of",
					Fields: []string{"var.scaling-adjustment", "var.step-adjustments"},
				},
				{
					Kind: "predicate",
					When: "((var.policy-type == null) || " +
						"(var.policy-type == 'SimpleScaling'))",
					Require: "(var.scaling-adjustment != null)",
					Message: "scaling-adjustment is required when policy-type is SimpleScaling",
				},
				{
					Kind: "predicate",
					When: "(var.scaling-adjustment != null)",
					Require: "((var.policy-type == null) || " +
						"(var.policy-type == 'SimpleScaling'))",
					Message: "scaling-adjustment is valid only when policy-type is SimpleScaling",
				},
				{
					Kind:    "predicate",
					When:    "(var.policy-type == 'StepScaling')",
					Require: "(var.step-adjustments != null)",
					Message: "step-adjustments is required when policy-type is StepScaling",
				},
				{
					Kind:    "predicate",
					When:    "(var.step-adjustments != null)",
					Require: "(var.policy-type == 'StepScaling')",
					Message: "step-adjustments is valid only when policy-type is StepScaling",
				},
				{
					Kind:    "predicate",
					When:    "(var.policy-type == 'TargetTrackingScaling')",
					Require: "(var.target-tracking-configuration != null)",
					Message: "target-tracking-configuration is required when policy-type " +
						"is TargetTrackingScaling",
				},
				{
					Kind:    "predicate",
					When:    "(var.target-tracking-configuration != null)",
					Require: "(var.policy-type == 'TargetTrackingScaling')",
					Message: "target-tracking-configuration is valid only when policy-type " +
						"is TargetTrackingScaling",
				},
				{
					Kind: "predicate",
					When: "(var.cooldown != null)",
					Require: "((var.policy-type == null) || " +
						"(var.policy-type == 'SimpleScaling'))",
					Message: "cooldown is valid only when policy-type is SimpleScaling",
				},
				{
					Kind: "predicate",
					When: "(var.estimated-instance-warmup != null)",
					Require: "(var.policy-type == 'StepScaling' || " +
						"var.policy-type == 'TargetTrackingScaling')",
					Message: "estimated-instance-warmup is valid only when policy-type is " +
						"StepScaling or TargetTrackingScaling",
				},
				{
					Kind: "predicate",
					When: "(var.min-adjustment-magnitude != null)",
					Require: "(var.min-adjustment-magnitude == null || " +
						"var.min-adjustment-magnitude >= 1)",
					Message: "min-adjustment-magnitude must be at least 1",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.step-adjustments", Optional: true},
			},
		},
		"autoscaling-lifecycle-hook": {
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
					Require: "(var.lifecycle-transition == 'autoscaling:EC2_INSTANCE_LAUNCHING' || " +
						"var.lifecycle-transition == 'autoscaling:EC2_INSTANCE_TERMINATING')",
					Message: "lifecycle-transition must be autoscaling:EC2_INSTANCE_LAUNCHING " +
						"or autoscaling:EC2_INSTANCE_TERMINATING",
				},
				{
					Kind: "predicate",
					When: "(var.default-result != null)",
					Require: "(var.default-result == 'CONTINUE' || " +
						"var.default-result == 'ABANDON')",
					Message: "default-result must be CONTINUE or ABANDON",
				},
				{
					Kind: "predicate",
					When: "(var.heartbeat-timeout != null)",
					Require: "(var.heartbeat-timeout == null || " +
						"var.heartbeat-timeout >= 30) && " +
						"(var.heartbeat-timeout == null || " +
						"var.heartbeat-timeout <= 7200)",
					Message: "heartbeat-timeout must be from 30 to 7200 seconds",
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
