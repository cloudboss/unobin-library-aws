package library_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	library "github.com/cloudboss/unobin-library-aws"
	"github.com/cloudboss/unobin-library-aws/internal/service/ec2"
)

// normalizeType sorts the fields of every object type it contains by name.
// goschema builds an object type's fields by ranging a map, so their order
// varies from one read to the next; sorting makes a schema comparison stable
// regardless. Remove once goschema emits object fields in a fixed order.
func normalizeType(t typecheck.Type) typecheck.Type {
	if t.Elem != nil {
		e := normalizeType(*t.Elem)
		t.Elem = &e
	}
	if t.Elems != nil {
		elems := make([]typecheck.Type, len(t.Elems))
		for i, e := range t.Elems {
			elems[i] = normalizeType(e)
		}
		t.Elems = elems
	}
	if t.Fields != nil {
		fields := make([]typecheck.ObjectField, len(t.Fields))
		for i, f := range t.Fields {
			f.Type = normalizeType(f.Type)
			fields[i] = f
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		t.Fields = fields
	}
	return t
}

// normalizeSchema returns a copy of s with every object type's fields sorted, so
// two reads of the same source compare equal despite goschema's varying field
// order. See normalizeType.
func normalizeSchema(s *runtime.TypeSchema) *runtime.TypeSchema {
	norm := func(m map[string]typecheck.Type) map[string]typecheck.Type {
		if m == nil {
			return nil
		}
		out := make(map[string]typecheck.Type, len(m))
		for k, v := range m {
			out[k] = normalizeType(v)
		}
		return out
	}
	return &runtime.TypeSchema{
		Inputs:           norm(s.Inputs),
		Outputs:          norm(s.Outputs),
		SensitiveInputs:  s.SensitiveInputs,
		SensitiveOutputs: s.SensitiveOutputs,
		Constraints:      s.Constraints,
	}
}

// TestLibraryRegistersEc2Vpc checks the runtime registration: ec2-vpc is
// present under Resources and dispatches to the Ec2VpcOutput type.
func TestLibraryRegistersEc2Vpc(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "ec2-vpc")
	assert.Equal(t,
		reflect.TypeFor[*ec2.VpcOutput](),
		lib.Resources["ec2-vpc"].OutputType())
}

// TestEc2VpcSchema checks what the dev CLI reads from this library's
// source: the input and output field types, that nothing is marked
// sensitive, and the full set of cross-field constraints derived from
// the Constraints method.
func TestEc2VpcSchema(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.Resources, "ec2-vpc")

	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"cidr-block":                           typecheck.TOptional(typecheck.TString()),
			"instance-tenancy":                     typecheck.TOptional(typecheck.TString()),
			"amazon-provided-ipv6-cidr-block":      typecheck.TOptional(typecheck.TBoolean()),
			"ipv4-ipam-pool-id":                    typecheck.TOptional(typecheck.TString()),
			"ipv4-netmask-length":                  typecheck.TOptional(typecheck.TInteger()),
			"ipv6-cidr-block":                      typecheck.TOptional(typecheck.TString()),
			"ipv6-cidr-block-network-border-group": typecheck.TOptional(typecheck.TString()),
			"ipv6-ipam-pool-id":                    typecheck.TOptional(typecheck.TString()),
			"ipv6-netmask-length":                  typecheck.TOptional(typecheck.TInteger()),
		},
		Outputs: map[string]typecheck.Type{
			"vpc-id":          typecheck.TString(),
			"dhcp-options-id": typecheck.TString(),
			"owner-id":        typecheck.TString(),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind:    "predicate",
				When:    "(var.instance-tenancy != null)",
				Require: "(var.instance-tenancy == 'default' || var.instance-tenancy == 'dedicated')",
				Message: "instance-tenancy must be default or dedicated",
			},
			{Kind: "at-most-one-of", Fields: []string{"cidr-block", "ipv4-netmask-length"}},
			{Kind: "required-with", Fields: []string{"ipv4-netmask-length", "ipv4-ipam-pool-id"}},
			{Kind: "at-most-one-of", Fields: []string{"ipv6-cidr-block", "ipv6-netmask-length"}},
			{Kind: "required-with", Fields: []string{"ipv6-cidr-block", "ipv6-ipam-pool-id"}},
			{Kind: "required-with", Fields: []string{"ipv6-netmask-length", "ipv6-ipam-pool-id"}},
			{
				Kind:    "predicate",
				When:    "(var.amazon-provided-ipv6-cidr-block == true)",
				Require: "(var.ipv6-cidr-block == null) && (var.ipv6-ipam-pool-id == null)",
				Message: "amazon-provided-ipv6-cidr-block cannot combine with an explicit ipv6 block or pool",
			},
			{
				Kind:    "predicate",
				When:    "(var.ipv6-cidr-block-network-border-group != null)",
				Require: "(var.amazon-provided-ipv6-cidr-block == true)",
				Message: "ipv6-cidr-block-network-border-group requires amazon-provided-ipv6-cidr-block",
			},
		},
	}

	assert.Equal(t, want, schema.Resources["ec2-vpc"])
}

// TestLibraryRegistersEc2SecurityGroups checks the runtime registration: the
// security group and its ingress and egress rule resources are present under
// Resources and dispatch to their output types.
func TestLibraryRegistersEc2SecurityGroups(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"ec2-security-group":              reflect.TypeFor[*ec2.SecurityGroupOutput](),
		"ec2-security-group-ingress-rule": reflect.TypeFor[*ec2.SecurityGroupIngressRuleOutput](),
		"ec2-security-group-egress-rule":  reflect.TypeFor[*ec2.SecurityGroupEgressRuleOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestEc2SecurityGroupSchemas checks the input and output field types and the
// cross-field constraints the dev CLI reads for the security group and its rule
// resources. The ingress and egress rules are mirrors, so they share a schema.
func TestEc2SecurityGroupSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	ruleSchema := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"security-group-id":            typecheck.TString(),
			"ip-protocol":                  typecheck.TString(),
			"from-port":                    typecheck.TOptional(typecheck.TInteger()),
			"to-port":                      typecheck.TOptional(typecheck.TInteger()),
			"cidr-ipv4":                    typecheck.TOptional(typecheck.TString()),
			"cidr-ipv6":                    typecheck.TOptional(typecheck.TString()),
			"prefix-list-id":               typecheck.TOptional(typecheck.TString()),
			"referenced-security-group-id": typecheck.TOptional(typecheck.TString()),
			"description":                  typecheck.TOptional(typecheck.TString()),
			"tags":                         typecheck.TMap(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"security-group-rule-id": typecheck.TString(),
			"arn":                    typecheck.TString(),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind: "exactly-one-of",
				Fields: []string{"cidr-ipv4", "cidr-ipv6", "prefix-list-id",
					"referenced-security-group-id"},
			},
			{
				Kind: "predicate",
				When: "(var.from-port != null)",
				Require: "(var.from-port == null || var.from-port >= -1) && " +
					"(var.from-port == null || var.from-port <= 65535)",
				Message: "from-port must be between -1 and 65535",
			},
			{
				Kind: "predicate",
				When: "(var.to-port != null)",
				Require: "(var.to-port == null || var.to-port >= -1) && " +
					"(var.to-port == null || var.to-port <= 65535)",
				Message: "to-port must be between -1 and 65535",
			},
		},
	}

	cases := map[string]*runtime.TypeSchema{
		"ec2-security-group": {
			Inputs: map[string]typecheck.Type{
				"name":                   typecheck.TOptional(typecheck.TString()),
				"name-prefix":            typecheck.TOptional(typecheck.TString()),
				"description":            typecheck.TString(),
				"vpc-id":                 typecheck.TOptional(typecheck.TString()),
				"tags":                   typecheck.TMap(typecheck.TString()),
				"revoke-rules-on-delete": typecheck.TOptional(typecheck.TBoolean()),
			},
			Outputs: map[string]typecheck.Type{
				"id":       typecheck.TString(),
				"arn":      typecheck.TString(),
				"owner-id": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{Kind: "at-most-one-of", Fields: []string{"name", "name-prefix"}},
			},
		},
		"ec2-security-group-ingress-rule": ruleSchema,
		"ec2-security-group-egress-rule":  ruleSchema,
	}

	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, want, schema.Resources[key])
		})
	}
}
