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
// method, and the declared optional defaults. normalizeSchema sorts nested
// object fields so the comparison is stable despite goschema varying their
// order.
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
	assert.Equal(t, normalizeSchema(want),
		normalizeSchema(schema.Resources["autoscaling-group"]))
}
