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
	"github.com/cloudboss/unobin-library-aws/library/resources"
)

// TestLibraryRegistersEc2Vpc checks the runtime registration: ec2-vpc is
// present under Resources and dispatches to the Ec2VpcOutput type.
func TestLibraryRegistersEc2Vpc(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "ec2-vpc")
	assert.Equal(t,
		reflect.TypeFor[*resources.Ec2VpcOutput](),
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
