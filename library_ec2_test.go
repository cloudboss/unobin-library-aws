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
	"github.com/cloudboss/unobin-library-aws/internal/service/ec2"
)

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
			{
				Kind:   "at-most-one-of",
				Fields: []string{"var.cidr-block", "var.ipv4-netmask-length"},
			},
			{
				Kind:   "required-with",
				Fields: []string{"var.ipv4-netmask-length", "var.ipv4-ipam-pool-id"},
			},
			{
				Kind:   "at-most-one-of",
				Fields: []string{"var.ipv6-cidr-block", "var.ipv6-netmask-length"},
			},
			{
				Kind:   "required-with",
				Fields: []string{"var.ipv6-cidr-block", "var.ipv6-ipam-pool-id"},
			},
			{
				Kind:   "required-with",
				Fields: []string{"var.ipv6-netmask-length", "var.ipv6-ipam-pool-id"},
			},
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
				Fields: []string{
					"var.cidr-ipv4", "var.cidr-ipv6", "var.prefix-list-id",
					"var.referenced-security-group-id",
				},
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
		Defaults: []lang.DefaultSpec{
			{Field: "var.tags", Optional: true},
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
				{
					Kind:   "at-most-one-of",
					Fields: []string{"var.name", "var.name-prefix"},
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.tags", Optional: true},
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

// TestLibraryRegistersEc2Subnet checks the runtime registration: ec2-subnet
// is present under Resources and dispatches to its output type.
func TestLibraryRegistersEc2Subnet(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "ec2-subnet")
	assert.Equal(t, reflect.TypeFor[*ec2.SubnetOutput](),
		lib.Resources["ec2-subnet"].OutputType())
}

// TestEc2SubnetSchema asserts the whole derived TypeSchema for ec2-subnet:
// the input and output field types, that nothing is sensitive, and the
// cross-field constraints derived from the Constraints method.
func TestEc2SubnetSchema(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.Resources, "ec2-subnet")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"assign-ipv6-address-on-creation":                typecheck.TOptional(typecheck.TBoolean()),
			"availability-zone":                              typecheck.TOptional(typecheck.TString()),
			"availability-zone-id":                           typecheck.TOptional(typecheck.TString()),
			"cidr-block":                                     typecheck.TOptional(typecheck.TString()),
			"customer-owned-ipv4-pool":                       typecheck.TOptional(typecheck.TString()),
			"enable-dns64":                                   typecheck.TOptional(typecheck.TBoolean()),
			"enable-lni-at-device-index":                     typecheck.TOptional(typecheck.TInteger()),
			"enable-resource-name-dns-a-record-on-launch":    typecheck.TOptional(typecheck.TBoolean()),
			"enable-resource-name-dns-aaaa-record-on-launch": typecheck.TOptional(typecheck.TBoolean()),
			"ipv4-ipam-pool-id":                              typecheck.TOptional(typecheck.TString()),
			"ipv4-netmask-length":                            typecheck.TOptional(typecheck.TInteger()),
			"ipv6-cidr-block":                                typecheck.TOptional(typecheck.TString()),
			"ipv6-ipam-pool-id":                              typecheck.TOptional(typecheck.TString()),
			"ipv6-native":                                    typecheck.TOptional(typecheck.TBoolean()),
			"ipv6-netmask-length":                            typecheck.TOptional(typecheck.TInteger()),
			"map-customer-owned-ip-on-launch":                typecheck.TOptional(typecheck.TBoolean()),
			"map-public-ip-on-launch":                        typecheck.TOptional(typecheck.TBoolean()),
			"outpost-arn":                                    typecheck.TOptional(typecheck.TString()),
			"private-dns-hostname-type-on-launch":            typecheck.TOptional(typecheck.TString()),
			"tags":                                           typecheck.TMap(typecheck.TString()),
			"vpc-id":                                         typecheck.TString(),
		},
		Outputs: map[string]typecheck.Type{
			"arn":                            typecheck.TString(),
			"availability-zone":              typecheck.TString(),
			"availability-zone-id":           typecheck.TString(),
			"cidr-block":                     typecheck.TString(),
			"id":                             typecheck.TString(),
			"ipv6-cidr-block":                typecheck.TString(),
			"ipv6-cidr-block-association-id": typecheck.TString(),
			"owner-id":                       typecheck.TString(),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind: "at-most-one-of",
				Fields: []string{
					"var.availability-zone", "var.availability-zone-id",
				},
			},
			{
				Kind: "forbidden-with",
				Fields: []string{
					"var.ipv4-netmask-length", "var.cidr-block",
					"var.customer-owned-ipv4-pool",
				},
			},
			{
				Kind: "required-with",
				Fields: []string{
					"var.ipv4-netmask-length", "var.ipv4-ipam-pool-id",
				},
			},
			{
				Kind: "at-most-one-of",
				Fields: []string{
					"var.ipv4-ipam-pool-id", "var.customer-owned-ipv4-pool",
				},
			},
			{
				Kind: "required-with",
				Fields: []string{
					"var.customer-owned-ipv4-pool",
					"var.map-customer-owned-ip-on-launch", "var.outpost-arn",
				},
			},
			{
				Kind: "required-with",
				Fields: []string{
					"var.map-customer-owned-ip-on-launch",
					"var.customer-owned-ipv4-pool", "var.outpost-arn",
				},
			},
			{
				Kind:   "forbidden-with",
				Fields: []string{"var.ipv6-netmask-length", "var.ipv6-cidr-block"},
			},
			{
				Kind: "required-with",
				Fields: []string{
					"var.ipv6-netmask-length", "var.ipv6-ipam-pool-id",
				},
			},
			{
				Kind: "predicate",
				When: "(var.private-dns-hostname-type-on-launch != null)",
				Require: "(var.private-dns-hostname-type-on-launch == 'ip-name' || " +
					"var.private-dns-hostname-type-on-launch == 'resource-name')",
				Message: "private-dns-hostname-type-on-launch must be ip-name or " +
					"resource-name",
			},
			{
				Kind: "predicate",
				When: "(var.enable-lni-at-device-index != null)",
				Require: "(var.enable-lni-at-device-index == null || " +
					"var.enable-lni-at-device-index > 0)",
				Message: "enable-lni-at-device-index must be a positive device position",
			},
		},
		Defaults: []lang.DefaultSpec{
			{Field: "var.tags", Optional: true},
		},
	}
	assert.Equal(t, want, schema.Resources["ec2-subnet"])
}

// TestLibraryRegistersEc2Volume checks the runtime registration: ec2-volume
// is in the resource map with the Volume output type.
func TestLibraryRegistersEc2Volume(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "ec2-volume")
	assert.Equal(t, reflect.TypeFor[*ec2.VolumeOutput](),
		lib.Resources["ec2-volume"].OutputType())
}

// TestEc2VolumeSchema asserts the whole derived TypeSchema for ec2-volume:
// the input and output field types, that nothing is sensitive, the
// cross-field constraints derived from the Constraints method, and the
// declared optional defaults.
func TestEc2VolumeSchema(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.Resources, "ec2-volume")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"availability-zone":          typecheck.TString(),
			"encrypted":                  typecheck.TOptional(typecheck.TBoolean()),
			"final-snapshot":             typecheck.TOptional(typecheck.TBoolean()),
			"iops":                       typecheck.TOptional(typecheck.TInteger()),
			"kms-key-id":                 typecheck.TOptional(typecheck.TString()),
			"multi-attach-enabled":       typecheck.TOptional(typecheck.TBoolean()),
			"outpost-arn":                typecheck.TOptional(typecheck.TString()),
			"size":                       typecheck.TOptional(typecheck.TInteger()),
			"snapshot-id":                typecheck.TOptional(typecheck.TString()),
			"tags":                       typecheck.TMap(typecheck.TString()),
			"throughput":                 typecheck.TOptional(typecheck.TInteger()),
			"type":                       typecheck.TOptional(typecheck.TString()),
			"volume-initialization-rate": typecheck.TOptional(typecheck.TInteger()),
		},
		Outputs: map[string]typecheck.Type{
			"create-time": typecheck.TString(),
			"encrypted":   typecheck.TBoolean(),
			"iops":        typecheck.TInteger(),
			"kms-key-id":  typecheck.TString(),
			"size":        typecheck.TInteger(),
			"throughput":  typecheck.TInteger(),
			"type":        typecheck.TString(),
			"volume-id":   typecheck.TString(),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind:   "at-least-one-of",
				Fields: []string{"var.size", "var.snapshot-id"},
			},
			{
				Kind: "predicate",
				When: "(var.type != null)",
				Require: "(var.type == 'standard' || var.type == 'gp2' || var.type == 'gp3' || " +
					"var.type == 'io1' || var.type == 'io2' || var.type == 'sc1' || var.type == 'st1')",
				Message: "type must be standard, gp2, gp3, io1, io2, sc1, or st1",
			},
			{
				Kind:    "predicate",
				When:    "(var.type == 'io1')",
				Require: "(var.iops != null)",
				Message: "iops is required when type is io1",
			},
			{
				Kind:    "predicate",
				When:    "(var.type == 'io2')",
				Require: "(var.iops != null)",
				Message: "iops is required when type is io2",
			},
			{
				Kind:    "predicate",
				When:    "(var.iops != null)",
				Require: "(var.type == 'gp3' || var.type == 'io1' || var.type == 'io2')",
				Message: "iops is valid only for gp3, io1, or io2 volume types",
			},
			{
				Kind: "predicate",
				When: "(var.throughput != null)",
				Require: "(var.type == 'gp3') && (var.throughput == null || var.throughput >= 125) && " +
					"(var.throughput == null || var.throughput <= 2000)",
				Message: "throughput is valid only for gp3 volumes and must be 125 to 2000",
			},
			{
				Kind:    "predicate",
				When:    "(var.multi-attach-enabled == true)",
				Require: "(var.type == 'io1' || var.type == 'io2')",
				Message: "multi-attach-enabled is valid only for io1 or io2 volume types",
			},
			{
				Kind: "predicate",
				When: "(var.volume-initialization-rate != null)",
				Require: "(var.snapshot-id != null) && (var.volume-initialization-rate == null || " +
					"var.volume-initialization-rate >= 100) && (var.volume-initialization-rate == null || " +
					"var.volume-initialization-rate <= 300)",
				Message: "volume-initialization-rate requires snapshot-id and must be 100 to 300",
			},
		},
		Defaults: []lang.DefaultSpec{
			{Field: "var.tags", Optional: true},
		},
	}
	assert.Equal(t, want, schema.Resources["ec2-volume"])
}

// TestLibraryRegistersEc2LaunchTemplate checks the runtime registration:
// ec2-launch-template is in the resource map with the LaunchTemplate output
// type.
func TestLibraryRegistersEc2LaunchTemplate(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "ec2-launch-template")
	assert.Equal(t, reflect.TypeFor[*ec2.LaunchTemplateOutput](),
		lib.Resources["ec2-launch-template"].OutputType())
}

// TestEc2LaunchTemplateSchema asserts the whole derived TypeSchema for
// ec2-launch-template: the nested data block's field types, the outputs,
// that nothing is sensitive, the constraints including the per-element
// ForEach rules, and the declared optional defaults.
func TestEc2LaunchTemplateSchema(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.Resources, "ec2-launch-template")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"data": typecheck.TObject([]typecheck.ObjectField{
				{Name: "image-id", Type: typecheck.TString(), Optional: true},
				{Name: "instance-type", Type: typecheck.TString(), Optional: true},
				{Name: "key-name", Type: typecheck.TString(), Optional: true},
				{Name: "user-data", Type: typecheck.TString(), Optional: true},
				{Name: "ebs-optimized", Type: typecheck.TBoolean(), Optional: true},
				{Name: "disable-api-stop", Type: typecheck.TBoolean(), Optional: true},
				{Name: "disable-api-termination", Type: typecheck.TBoolean(), Optional: true},
				{Name: "instance-initiated-shutdown-behavior", Type: typecheck.TString(), Optional: true},
				{Name: "security-group-ids", Type: typecheck.TList(typecheck.TString()), Optional: true},
				{Name: "security-groups", Type: typecheck.TList(typecheck.TString()), Optional: true},
				{Name: "block-device-mappings", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "device-name", Type: typecheck.TString(), Optional: true},
					{Name: "no-device", Type: typecheck.TString(), Optional: true},
					{Name: "virtual-name", Type: typecheck.TString(), Optional: true},
					{Name: "ebs", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "delete-on-termination", Type: typecheck.TBoolean(), Optional: true},
						{Name: "encrypted", Type: typecheck.TBoolean(), Optional: true},
						{Name: "iops", Type: typecheck.TInteger(), Optional: true},
						{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
						{Name: "snapshot-id", Type: typecheck.TString(), Optional: true},
						{Name: "throughput", Type: typecheck.TInteger(), Optional: true},
						{Name: "volume-initialization-rate", Type: typecheck.TInteger(), Optional: true},
						{Name: "volume-size", Type: typecheck.TInteger(), Optional: true},
						{Name: "volume-type", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
				})), Optional: true},
				{Name: "network-interfaces", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "associate-carrier-ip-address", Type: typecheck.TBoolean(), Optional: true},
					{Name: "associate-public-ip-address", Type: typecheck.TBoolean(), Optional: true},
					{Name: "delete-on-termination", Type: typecheck.TBoolean(), Optional: true},
					{Name: "description", Type: typecheck.TString(), Optional: true},
					{Name: "device-index", Type: typecheck.TInteger(), Optional: true},
					{Name: "interface-type", Type: typecheck.TString(), Optional: true},
					{Name: "ipv4-prefix-count", Type: typecheck.TInteger(), Optional: true},
					{Name: "ipv4-prefixes", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "ipv6-address-count", Type: typecheck.TInteger(), Optional: true},
					{Name: "ipv6-addresses", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "ipv6-prefix-count", Type: typecheck.TInteger(), Optional: true},
					{Name: "ipv6-prefixes", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "network-card-index", Type: typecheck.TInteger(), Optional: true},
					{Name: "network-interface-id", Type: typecheck.TString(), Optional: true},
					{Name: "primary-ipv6", Type: typecheck.TBoolean(), Optional: true},
					{Name: "private-ip-address", Type: typecheck.TString(), Optional: true},
					{Name: "ipv4-addresses", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "ipv4-address-count", Type: typecheck.TInteger(), Optional: true},
					{Name: "subnet-id", Type: typecheck.TString(), Optional: true},
					{Name: "groups", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "ena-srd-specification", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "ena-srd-enabled", Type: typecheck.TBoolean(), Optional: true},
						{Name: "ena-srd-udp-specification", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "ena-srd-udp-enabled", Type: typecheck.TBoolean(), Optional: true},
						}), Optional: true},
					}), Optional: true},
					{Name: "connection-tracking-specification", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "tcp-established-timeout", Type: typecheck.TInteger(), Optional: true},
						{Name: "udp-stream-timeout", Type: typecheck.TInteger(), Optional: true},
						{Name: "udp-timeout", Type: typecheck.TInteger(), Optional: true},
					}), Optional: true},
				})), Optional: true},
				{Name: "iam-instance-profile", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "arn", Type: typecheck.TString(), Optional: true},
					{Name: "name", Type: typecheck.TString(), Optional: true},
				}), Optional: true},
				{Name: "monitoring", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
				}), Optional: true},
				{Name: "metadata-options", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "http-endpoint", Type: typecheck.TString(), Optional: true},
					{Name: "http-protocol-ipv6", Type: typecheck.TString(), Optional: true},
					{Name: "http-put-response-hop-limit", Type: typecheck.TInteger(), Optional: true},
					{Name: "http-tokens", Type: typecheck.TString(), Optional: true},
					{Name: "instance-metadata-tags", Type: typecheck.TString(), Optional: true},
				}), Optional: true},
				{Name: "placement", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "affinity", Type: typecheck.TString(), Optional: true},
					{Name: "availability-zone", Type: typecheck.TString(), Optional: true},
					{Name: "availability-zone-id", Type: typecheck.TString(), Optional: true},
					{Name: "group-id", Type: typecheck.TString(), Optional: true},
					{Name: "group-name", Type: typecheck.TString(), Optional: true},
					{Name: "host-id", Type: typecheck.TString(), Optional: true},
					{Name: "host-resource-group-arn", Type: typecheck.TString(), Optional: true},
					{Name: "partition-number", Type: typecheck.TInteger(), Optional: true},
					{Name: "spread-domain", Type: typecheck.TString(), Optional: true},
					{Name: "tenancy", Type: typecheck.TString(), Optional: true},
				}), Optional: true},
				{Name: "tag-specifications", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "resource-type", Type: typecheck.TString(), Optional: true},
					{Name: "tags", Type: typecheck.TMap(typecheck.TString())},
				})), Optional: true},
				{Name: "credit-specification", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "cpu-credits", Type: typecheck.TString(), Optional: true},
				}), Optional: true},
				{Name: "cpu-options", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "amd-sev-snp", Type: typecheck.TString(), Optional: true},
					{Name: "core-count", Type: typecheck.TInteger(), Optional: true},
					{Name: "nested-virtualization", Type: typecheck.TString(), Optional: true},
					{Name: "threads-per-core", Type: typecheck.TInteger(), Optional: true},
				}), Optional: true},
				{Name: "enclave-options", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
				}), Optional: true},
				{Name: "hibernation-options", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "configured", Type: typecheck.TBoolean(), Optional: true},
				}), Optional: true},
				{Name: "private-dns-name-options", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "enable-resource-name-dns-aaaa-record", Type: typecheck.TBoolean(), Optional: true},
					{Name: "enable-resource-name-dns-a-record", Type: typecheck.TBoolean(), Optional: true},
					{Name: "hostname-type", Type: typecheck.TString(), Optional: true},
				}), Optional: true},
				{Name: "maintenance-options", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "auto-recovery", Type: typecheck.TString(), Optional: true},
				}), Optional: true},
				{Name: "license-specifications",
					Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "license-configuration-arn", Type: typecheck.TString(), Optional: true},
					})), Optional: true},
				{Name: "instance-market-options", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "market-type", Type: typecheck.TString(), Optional: true},
					{Name: "spot-options", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "block-duration-minutes", Type: typecheck.TInteger(), Optional: true},
						{Name: "instance-interruption-behavior", Type: typecheck.TString(), Optional: true},
						{Name: "max-price", Type: typecheck.TString(), Optional: true},
						{Name: "spot-instance-type", Type: typecheck.TString(), Optional: true},
						{Name: "valid-until", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
				}), Optional: true},
				{Name: "capacity-reservation-specification", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "capacity-reservation-preference", Type: typecheck.TString(), Optional: true},
					{Name: "capacity-reservation-target", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "capacity-reservation-id", Type: typecheck.TString(), Optional: true},
						{Name: "capacity-reservation-resource-group-arn", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
				}), Optional: true},
				{Name: "network-performance-options", Type: typecheck.TObject([]typecheck.ObjectField{
					{Name: "bandwidth-weighting", Type: typecheck.TString(), Optional: true},
				}), Optional: true},
			}),
			"default-version":        typecheck.TOptional(typecheck.TInteger()),
			"name":                   typecheck.TString(),
			"tags":                   typecheck.TMap(typecheck.TString()),
			"update-default-version": typecheck.TOptional(typecheck.TBoolean()),
			"version-description":    typecheck.TOptional(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"default-version":    typecheck.TInteger(),
			"latest-version":     typecheck.TInteger(),
			"launch-template-id": typecheck.TString(),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind:   "at-most-one-of",
				Fields: []string{"var.default-version", "var.update-default-version"},
			},
			{
				Kind:   "at-most-one-of",
				Fields: []string{"var.data.security-groups", "var.data.security-group-ids"},
			},
			{
				Kind:   "at-most-one-of",
				Fields: []string{"var.data.iam-instance-profile.arn", "var.data.iam-instance-profile.name"},
			},
			{
				Kind:   "at-most-one-of",
				Fields: []string{"var.data.placement.group-id", "var.data.placement.group-name"},
			},
			{
				Kind:   "at-most-one-of",
				Fields: []string{"var.data.placement.host-resource-group-arn", "var.data.placement.host-id"},
			},
			{
				Kind: "at-most-one-of",
				Fields: []string{
					"var.data.capacity-reservation-specification.capacity-reservation-target." +
						"capacity-reservation-id",
					"var.data.capacity-reservation-specification.capacity-reservation-target." +
						"capacity-reservation-resource-group-arn",
				},
			},
			{
				Kind: "predicate",
				When: "(var.data.capacity-reservation-specification.capacity-reservation-preference != null)",
				Require: "(var.data.capacity-reservation-specification.capacity-reservation-preference == " +
					"'capacity-reservations-only' || " +
					"var.data.capacity-reservation-specification.capacity-reservation-preference == 'open' || " +
					"var.data.capacity-reservation-specification.capacity-reservation-preference == 'none')",
				Message: "capacity-reservation-preference must be capacity-reservations-only, open, or none",
			},
			{
				Kind:    "predicate",
				When:    "(var.version-description != null)",
				Require: "(var.version-description == null || var.version-description <= 255)",
				Message: "version-description must be at most 255 characters",
			},
			{
				Kind: "predicate",
				When: "(var.data.instance-initiated-shutdown-behavior != null)",
				Require: "(var.data.instance-initiated-shutdown-behavior == 'stop' || " +
					"var.data.instance-initiated-shutdown-behavior == 'terminate')",
				Message: "instance-initiated-shutdown-behavior must be stop or terminate",
			},
			{
				Kind: "predicate",
				When: "(var.data.credit-specification.cpu-credits != null)",
				Require: "(var.data.credit-specification.cpu-credits == 'standard' || " +
					"var.data.credit-specification.cpu-credits == 'unlimited')",
				Message: "credit-specification cpu-credits must be standard or unlimited",
			},
			{
				Kind: "predicate",
				When: "(var.data.cpu-options.amd-sev-snp != null)",
				Require: "(var.data.cpu-options.amd-sev-snp == 'enabled' || " +
					"var.data.cpu-options.amd-sev-snp == 'disabled')",
				Message: "cpu-options amd-sev-snp must be enabled or disabled",
			},
			{
				Kind: "predicate",
				When: "(var.data.cpu-options.nested-virtualization != null)",
				Require: "(var.data.cpu-options.nested-virtualization == 'enabled' || " +
					"var.data.cpu-options.nested-virtualization == 'disabled')",
				Message: "cpu-options nested-virtualization must be enabled or disabled",
			},
			{
				Kind: "predicate",
				When: "(var.data.placement.tenancy != null)",
				Require: "(var.data.placement.tenancy == 'default' || " +
					"var.data.placement.tenancy == 'dedicated' || var.data.placement.tenancy == 'host')",
				Message: "placement tenancy must be default, dedicated, or host",
			},
			{
				Kind: "predicate",
				When: "(var.data.private-dns-name-options.hostname-type != null)",
				Require: "(var.data.private-dns-name-options.hostname-type == 'ip-name' || " +
					"var.data.private-dns-name-options.hostname-type == 'resource-name')",
				Message: "private-dns-name-options hostname-type must be ip-name or resource-name",
			},
			{
				Kind: "predicate",
				When: "(var.data.maintenance-options.auto-recovery != null)",
				Require: "(var.data.maintenance-options.auto-recovery == 'default' || " +
					"var.data.maintenance-options.auto-recovery == 'disabled')",
				Message: "maintenance-options auto-recovery must be default or disabled",
			},
			{
				Kind: "predicate",
				When: "(var.data.network-performance-options.bandwidth-weighting != null)",
				Require: "(var.data.network-performance-options.bandwidth-weighting == 'default' || " +
					"var.data.network-performance-options.bandwidth-weighting == 'vpc-1' || " +
					"var.data.network-performance-options.bandwidth-weighting == 'ebs-1')",
				Message: "network-performance-options bandwidth-weighting must be default, vpc-1, or ebs-1",
			},
			{
				Kind: "predicate",
				When: "(var.data.instance-market-options.market-type != null)",
				Require: "(var.data.instance-market-options.market-type == 'spot' || " +
					"var.data.instance-market-options.market-type == 'capacity-block' || " +
					"var.data.instance-market-options.market-type == 'interruptible-capacity-reservation')",
				Message: "instance-market-options market-type must be a valid market type",
			},
			{
				Kind: "predicate",
				When: "(var.data.instance-market-options.spot-options.instance-interruption-behavior != null)",
				Require: "(var.data.instance-market-options.spot-options.instance-interruption-behavior == " +
					"'hibernate' || " +
					"var.data.instance-market-options.spot-options.instance-interruption-behavior == 'stop' || " +
					"var.data.instance-market-options.spot-options.instance-interruption-behavior == " +
					"'terminate')",
				Message: "spot-options instance-interruption-behavior must be hibernate, stop, or terminate",
			},
			{
				Kind: "predicate",
				When: "(var.data.instance-market-options.spot-options.spot-instance-type != null)",
				Require: "(var.data.instance-market-options.spot-options.spot-instance-type == 'one-time' " +
					"|| var.data.instance-market-options.spot-options.spot-instance-type == 'persistent')",
				Message: "spot-options spot-instance-type must be one-time or persistent",
			},
			{
				Kind: "predicate",
				When: "(var.data.metadata-options.http-endpoint != null)",
				Require: "(var.data.metadata-options.http-endpoint == 'enabled' || " +
					"var.data.metadata-options.http-endpoint == 'disabled')",
				Message: "metadata-options http-endpoint must be enabled or disabled",
			},
			{
				Kind: "predicate",
				When: "(var.data.metadata-options.http-tokens != null)",
				Require: "(var.data.metadata-options.http-tokens == 'optional' || " +
					"var.data.metadata-options.http-tokens == 'required')",
				Message: "metadata-options http-tokens must be optional or required",
			},
			{
				Kind: "predicate",
				When: "(var.data.metadata-options.http-protocol-ipv6 != null)",
				Require: "(var.data.metadata-options.http-protocol-ipv6 == 'enabled' || " +
					"var.data.metadata-options.http-protocol-ipv6 == 'disabled')",
				Message: "metadata-options http-protocol-ipv6 must be enabled or disabled",
			},
			{
				Kind: "predicate",
				When: "(var.data.metadata-options.instance-metadata-tags != null)",
				Require: "(var.data.metadata-options.instance-metadata-tags == 'enabled' || " +
					"var.data.metadata-options.instance-metadata-tags == 'disabled')",
				Message: "metadata-options instance-metadata-tags must be enabled or disabled",
			},
			{
				Kind: "predicate",
				When: "(var.data.metadata-options.http-put-response-hop-limit != null)",
				Require: "(var.data.metadata-options.http-put-response-hop-limit == null || " +
					"var.data.metadata-options.http-put-response-hop-limit >= 1) && " +
					"(var.data.metadata-options.http-put-response-hop-limit == null || " +
					"var.data.metadata-options.http-put-response-hop-limit <= 64)",
				Message: "metadata-options http-put-response-hop-limit must be between 1 and 64",
			},
		},
		Defaults: []lang.DefaultSpec{
			{Field: "var.tags", Optional: true},
		},
	}
	assert.Equal(t, want,
		schema.Resources["ec2-launch-template"])
}

// TestLibraryRegistersEc2Ami checks the runtime registration: ec2-ami is in
// the data source map, the library's first, with the AMI output type.
func TestLibraryRegistersEc2Ami(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.DataSources, "ec2-ami")
	assert.Equal(t, reflect.TypeFor[*ec2.AMIOutput](),
		lib.DataSources["ec2-ami"].OutputType())
}

// TestEc2AmiSchema asserts the whole derived TypeSchema for the ec2-ami data
// source: the query inputs, the selected image's outputs, that nothing is
// sensitive, the owners constraint, and the declared optional defaults.
func TestEc2AmiSchema(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.DataSources, "ec2-ami")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"allow-unsafe-filter": typecheck.TOptional(typecheck.TBoolean()),
			"executable-users":    typecheck.TList(typecheck.TString()),
			"filters": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
				{Name: "name", Type: typecheck.TString()},
				{Name: "values", Type: typecheck.TList(typecheck.TString())},
			})),
			"image-ids":          typecheck.TList(typecheck.TString()),
			"include-deprecated": typecheck.TOptional(typecheck.TBoolean()),
			"most-recent":        typecheck.TOptional(typecheck.TBoolean()),
			"name-regex":         typecheck.TOptional(typecheck.TString()),
			"owners":             typecheck.TList(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"architecture":        typecheck.TString(),
			"arn":                 typecheck.TString(),
			"creation-date":       typecheck.TString(),
			"ena-support":         typecheck.TBoolean(),
			"image-id":            typecheck.TString(),
			"name":                typecheck.TString(),
			"owner-id":            typecheck.TString(),
			"root-device-name":    typecheck.TString(),
			"root-device-type":    typecheck.TString(),
			"root-snapshot-id":    typecheck.TString(),
			"sriov-net-support":   typecheck.TString(),
			"state":               typecheck.TString(),
			"virtualization-type": typecheck.TString(),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind:    "predicate",
				When:    "(var.owners != null)",
				Require: "(var.owners == null || @core.length(var.owners) >= 1)",
				Message: "owners must list at least one owner when given",
			},
		},
		Defaults: []lang.DefaultSpec{
			{Field: "var.owners", Optional: true},
			{Field: "var.executable-users", Optional: true},
			{Field: "var.filters", Optional: true},
			{Field: "var.image-ids", Optional: true},
		},
	}
	assert.Equal(t, want, schema.DataSources["ec2-ami"])
}

// TestLibraryRegistersEc2Routing checks the runtime registration: the seven
// VPC-routing resources are present under Resources and dispatch to their
// output types.
func TestLibraryRegistersEc2Routing(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "ec2-internet-gateway")
	assert.Equal(t, reflect.TypeFor[*ec2.InternetGatewayOutput](),
		lib.Resources["ec2-internet-gateway"].OutputType())
	require.Contains(t, lib.Resources, "ec2-route-table")
	assert.Equal(t, reflect.TypeFor[*ec2.RouteTableOutput](),
		lib.Resources["ec2-route-table"].OutputType())
	require.Contains(t, lib.Resources, "ec2-route")
	assert.Equal(t, reflect.TypeFor[*ec2.RouteOutput](),
		lib.Resources["ec2-route"].OutputType())
	require.Contains(t, lib.Resources, "ec2-route-table-association")
	assert.Equal(t, reflect.TypeFor[*ec2.RouteTableAssociationOutput](),
		lib.Resources["ec2-route-table-association"].OutputType())
	require.Contains(t, lib.Resources, "ec2-eip")
	assert.Equal(t, reflect.TypeFor[*ec2.EipOutput](),
		lib.Resources["ec2-eip"].OutputType())
	require.Contains(t, lib.Resources, "ec2-nat-gateway")
	assert.Equal(t, reflect.TypeFor[*ec2.NatGatewayOutput](),
		lib.Resources["ec2-nat-gateway"].OutputType())
	require.Contains(t, lib.Resources, "ec2-vpc-endpoint")
	assert.Equal(t, reflect.TypeFor[*ec2.VpcEndpointOutput](),
		lib.Resources["ec2-vpc-endpoint"].OutputType())
}

// TestEc2RoutingSchemas asserts the whole derived TypeSchema -- input and
// output field types, the cross-field constraints, and the declared optional
// defaults -- for each VPC-routing resource.
func TestEc2RoutingSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	tests := []struct {
		key  string
		want *runtime.TypeSchema
	}{
		{
			key: "ec2-internet-gateway",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"tags":   typecheck.TMap(typecheck.TString()),
					"vpc-id": typecheck.TOptional(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"internet-gateway-id": typecheck.TString(),
					"owner-id":            typecheck.TString(),
					"vpc-id":              typecheck.TString(),
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "ec2-route-table",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"tags":   typecheck.TMap(typecheck.TString()),
					"vpc-id": typecheck.TString(),
				},
				Outputs: map[string]typecheck.Type{
					"owner-id":       typecheck.TString(),
					"route-table-id": typecheck.TString(),
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "ec2-route",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"carrier-gateway-id":          typecheck.TOptional(typecheck.TString()),
					"core-network-arn":            typecheck.TOptional(typecheck.TString()),
					"destination-cidr-block":      typecheck.TOptional(typecheck.TString()),
					"destination-ipv6-cidr-block": typecheck.TOptional(typecheck.TString()),
					"destination-prefix-list-id":  typecheck.TOptional(typecheck.TString()),
					"egress-only-gateway-id":      typecheck.TOptional(typecheck.TString()),
					"gateway-id":                  typecheck.TOptional(typecheck.TString()),
					"local-gateway-id":            typecheck.TOptional(typecheck.TString()),
					"nat-gateway-id":              typecheck.TOptional(typecheck.TString()),
					"network-interface-id":        typecheck.TOptional(typecheck.TString()),
					"route-table-id":              typecheck.TString(),
					"transit-gateway-id":          typecheck.TOptional(typecheck.TString()),
					"vpc-endpoint-id":             typecheck.TOptional(typecheck.TString()),
					"vpc-peering-connection-id":   typecheck.TOptional(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"state": typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "exactly-one-of",
						Fields: []string{
							"var.destination-cidr-block",
							"var.destination-ipv6-cidr-block",
							"var.destination-prefix-list-id",
						},
					},
					{
						Kind: "exactly-one-of",
						Fields: []string{
							"var.carrier-gateway-id",
							"var.core-network-arn",
							"var.egress-only-gateway-id",
							"var.gateway-id",
							"var.local-gateway-id",
							"var.nat-gateway-id",
							"var.network-interface-id",
							"var.transit-gateway-id",
							"var.vpc-endpoint-id",
							"var.vpc-peering-connection-id",
						},
					},
					{
						Kind: "forbidden-with",
						Fields: []string{
							"var.carrier-gateway-id",
							"var.destination-ipv6-cidr-block",
						},
					},
					{
						Kind: "forbidden-with",
						Fields: []string{
							"var.egress-only-gateway-id",
							"var.destination-cidr-block",
						},
					},
					{
						Kind: "forbidden-with",
						Fields: []string{
							"var.vpc-endpoint-id",
							"var.destination-prefix-list-id",
						},
					},
					{
						Kind:    "predicate",
						When:    "(var.gateway-id != null)",
						Require: "(var.gateway-id != 'local')",
						Message: "gateway-id cannot be local; the local route is not managed here",
					},
				},
			},
		},
		{
			key: "ec2-route-table-association",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"gateway-id":     typecheck.TOptional(typecheck.TString()),
					"route-table-id": typecheck.TString(),
					"subnet-id":      typecheck.TOptional(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"route-table-association-id": typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "exactly-one-of",
						Fields: []string{
							"var.subnet-id",
							"var.gateway-id",
						},
					},
				},
			},
		},
		{
			key: "ec2-eip",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"address":                  typecheck.TOptional(typecheck.TString()),
					"customer-owned-ipv4-pool": typecheck.TOptional(typecheck.TString()),
					"domain":                   typecheck.TOptional(typecheck.TString()),
					"ipam-pool-id":             typecheck.TOptional(typecheck.TString()),
					"network-border-group":     typecheck.TOptional(typecheck.TString()),
					"public-ipv4-pool":         typecheck.TOptional(typecheck.TString()),
					"tags":                     typecheck.TMap(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"allocation-id":  typecheck.TString(),
					"association-id": typecheck.TString(),
					"private-ip":     typecheck.TString(),
					"public-ip":      typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind:    "predicate",
						When:    "(var.domain != null)",
						Require: "(var.domain == 'vpc' || var.domain == 'standard')",
						Message: "domain must be vpc or standard",
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "ec2-nat-gateway",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"allocation-id":     typecheck.TOptional(typecheck.TString()),
					"connectivity-type": typecheck.TOptional(typecheck.TString()),
					"private-ip":        typecheck.TOptional(typecheck.TString()),
					"secondary-allocation-ids": typecheck.TList(
						typecheck.TString()),
					"secondary-private-ip-address-count": typecheck.TOptional(
						typecheck.TInteger()),
					"secondary-private-ip-addresses": typecheck.TList(
						typecheck.TString()),
					"subnet-id": typecheck.TString(),
					"tags":      typecheck.TMap(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"nat-gateway-id":       typecheck.TString(),
					"network-interface-id": typecheck.TString(),
					"private-ip":           typecheck.TString(),
					"public-ip":            typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(var.connectivity-type != null)",
						Require: "(var.connectivity-type == 'public' || " +
							"var.connectivity-type == 'private')",
						Message: "connectivity-type must be public or private",
					},
					{
						Kind: "predicate",
						When: "((var.connectivity-type == 'public') || " +
							"(var.connectivity-type == null))",
						Require: "(var.allocation-id != null)",
						Message: "allocation-id is required for a public NAT gateway",
					},
					{
						Kind:    "predicate",
						When:    "(var.connectivity-type == 'private')",
						Require: "(var.allocation-id == null)",
						Message: "allocation-id is not supported with connectivity-type private",
					},
					{
						Kind:    "predicate",
						When:    "(var.connectivity-type == 'private')",
						Require: "(var.secondary-allocation-ids == null)",
						Message: "secondary-allocation-ids is not supported with " +
							"connectivity-type private",
					},
					{
						Kind:    "predicate",
						When:    "(var.secondary-private-ip-address-count != null)",
						Require: "(var.connectivity-type == 'private')",
						Message: "secondary-private-ip-address-count is supported only with " +
							"connectivity-type private",
					},
					{
						Kind: "at-most-one-of",
						Fields: []string{
							"var.secondary-private-ip-address-count",
							"var.secondary-private-ip-addresses",
						},
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.secondary-allocation-ids", Optional: true},
					{Field: "var.secondary-private-ip-addresses", Optional: true},
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "ec2-vpc-endpoint",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"dns-options": typecheck.TOptional(typecheck.TObject(
						[]typecheck.ObjectField{
							{
								Name:     "dns-record-ip-type",
								Type:     typecheck.TString(),
								Optional: true,
							},
							{
								Name:     "private-dns-only-for-inbound-resolver-endpoint",
								Type:     typecheck.TBoolean(),
								Optional: true,
							},
						})),
					"ip-address-type":     typecheck.TOptional(typecheck.TString()),
					"policy":              typecheck.TOptional(typecheck.TString()),
					"private-dns-enabled": typecheck.TOptional(typecheck.TBoolean()),
					"route-table-ids":     typecheck.TList(typecheck.TString()),
					"security-group-ids":  typecheck.TList(typecheck.TString()),
					"service-name":        typecheck.TString(),
					"subnet-ids":          typecheck.TList(typecheck.TString()),
					"tags":                typecheck.TMap(typecheck.TString()),
					"vpc-endpoint-type":   typecheck.TOptional(typecheck.TString()),
					"vpc-id":              typecheck.TString(),
				},
				Outputs: map[string]typecheck.Type{
					"cidr-blocks": typecheck.TList(typecheck.TString()),
					"dns-entries": typecheck.TList(typecheck.TObject(
						[]typecheck.ObjectField{
							{Name: "dns-name", Type: typecheck.TString()},
							{Name: "hosted-zone-id", Type: typecheck.TString()},
						})),
					"network-interface-ids": typecheck.TList(typecheck.TString()),
					"owner-id":              typecheck.TString(),
					"policy":                typecheck.TString(),
					"prefix-list-id":        typecheck.TString(),
					"state":                 typecheck.TString(),
					"vpc-endpoint-id":       typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(var.vpc-endpoint-type != null)",
						Require: "(var.vpc-endpoint-type == 'Gateway' || " +
							"var.vpc-endpoint-type == 'Interface' || " +
							"var.vpc-endpoint-type == 'GatewayLoadBalancer')",
						Message: "vpc-endpoint-type must be Gateway, Interface, or " +
							"GatewayLoadBalancer",
					},
					{
						Kind: "predicate",
						When: "(var.ip-address-type != null)",
						Require: "(var.ip-address-type == 'ipv4' || " +
							"var.ip-address-type == 'dualstack' || " +
							"var.ip-address-type == 'ipv6')",
						Message: "ip-address-type must be ipv4, dualstack, or ipv6",
					},
					{
						Kind: "predicate",
						When: "(var.dns-options.dns-record-ip-type != null)",
						Require: "(var.dns-options.dns-record-ip-type == 'ipv4' || " +
							"var.dns-options.dns-record-ip-type == 'dualstack' || " +
							"var.dns-options.dns-record-ip-type == 'ipv6' || " +
							"var.dns-options.dns-record-ip-type == 'service-defined')",
						Message: "dns-options dns-record-ip-type must be ipv4, " +
							"dualstack, ipv6, or service-defined",
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.route-table-ids", Optional: true},
					{Field: "var.security-group-ids", Optional: true},
					{Field: "var.subnet-ids", Optional: true},
					{Field: "var.tags", Optional: true},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			require.Contains(t, schema.Resources, tt.key)
			assert.Equal(t, tt.want, schema.Resources[tt.key],
				tt.key)
		})
	}
}

// TestLibraryRegistersEc2KeyPair checks the runtime registration: ec2-key-pair
// is present under Resources and dispatches to the KeyPairOutput type.
func TestLibraryRegistersEc2KeyPair(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "ec2-key-pair")
	assert.Equal(t, reflect.TypeFor[*ec2.KeyPairOutput](),
		lib.Resources["ec2-key-pair"].OutputType())
}

// TestEc2KeyPairSchema asserts the whole derived TypeSchema for ec2-key-pair:
// the input and output field types and the declared optional defaults.
func TestEc2KeyPairSchema(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.Resources, "ec2-key-pair")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"key-name":   typecheck.TString(),
			"public-key": typecheck.TString(),
			"tags":       typecheck.TMap(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"fingerprint": typecheck.TString(),
			"key-pair-id": typecheck.TString(),
			"key-type":    typecheck.TString(),
		},
		Defaults: []lang.DefaultSpec{
			{Field: "var.tags", Optional: true},
		},
	}
	assert.Equal(t, want, schema.Resources["ec2-key-pair"])
}

// TestLibraryRegistersEc2Instance checks the runtime registration: ec2-instance
// is present under Resources and dispatches to the InstanceOutput type.
func TestLibraryRegistersEc2Instance(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "ec2-instance")
	assert.Equal(t, reflect.TypeFor[*ec2.InstanceOutput](),
		lib.Resources["ec2-instance"].OutputType())
}

// TestEc2InstanceSchema asserts the whole derived TypeSchema for ec2-instance:
// the input and output field types, the cross-field constraints -- including
// the nested-selector rules on the launch-template, metadata-options, and
// root-block-device blocks and the ForEach rules on the volume lists -- and
// the declared optional defaults.
func TestEc2InstanceSchema(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.Resources, "ec2-instance")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"ami":                         typecheck.TOptional(typecheck.TString()),
			"associate-public-ip-address": typecheck.TOptional(typecheck.TBoolean()),
			"availability-zone":           typecheck.TOptional(typecheck.TString()),
			"disable-api-stop":            typecheck.TOptional(typecheck.TBoolean()),
			"disable-api-termination":     typecheck.TOptional(typecheck.TBoolean()),
			"ebs-block-device": typecheck.TList(typecheck.TObject(
				[]typecheck.ObjectField{
					{Name: "device-name", Type: typecheck.TString()},
					{
						Name:     "delete-on-termination",
						Type:     typecheck.TBoolean(),
						Optional: true,
					},
					{Name: "encrypted", Type: typecheck.TBoolean(), Optional: true},
					{Name: "iops", Type: typecheck.TInteger(), Optional: true},
					{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
					{Name: "snapshot-id", Type: typecheck.TString(), Optional: true},
					{Name: "throughput", Type: typecheck.TInteger(), Optional: true},
					{Name: "volume-size", Type: typecheck.TInteger(), Optional: true},
					{Name: "volume-type", Type: typecheck.TString(), Optional: true},
				})),
			"ebs-optimized": typecheck.TOptional(typecheck.TBoolean()),
			"ephemeral-block-device": typecheck.TList(typecheck.TObject(
				[]typecheck.ObjectField{
					{Name: "device-name", Type: typecheck.TString()},
					{Name: "no-device", Type: typecheck.TBoolean(), Optional: true},
					{Name: "virtual-name", Type: typecheck.TString(), Optional: true},
				})),
			"force-destroy":        typecheck.TOptional(typecheck.TBoolean()),
			"iam-instance-profile": typecheck.TOptional(typecheck.TString()),
			"instance-initiated-shutdown-behavior": typecheck.TOptional(
				typecheck.TString()),
			"instance-type": typecheck.TOptional(typecheck.TString()),
			"key-name":      typecheck.TOptional(typecheck.TString()),
			"launch-template": typecheck.TOptional(typecheck.TObject(
				[]typecheck.ObjectField{
					{Name: "id", Type: typecheck.TString(), Optional: true},
					{Name: "name", Type: typecheck.TString(), Optional: true},
					{Name: "version", Type: typecheck.TString(), Optional: true},
				})),
			"metadata-options": typecheck.TOptional(typecheck.TObject(
				[]typecheck.ObjectField{
					{Name: "http-endpoint", Type: typecheck.TString(), Optional: true},
					{
						Name:     "http-protocol-ipv6",
						Type:     typecheck.TString(),
						Optional: true,
					},
					{
						Name:     "http-put-response-hop-limit",
						Type:     typecheck.TInteger(),
						Optional: true,
					},
					{Name: "http-tokens", Type: typecheck.TString(), Optional: true},
					{
						Name:     "instance-metadata-tags",
						Type:     typecheck.TString(),
						Optional: true,
					},
				})),
			"monitoring": typecheck.TOptional(typecheck.TBoolean()),
			"private-ip": typecheck.TOptional(typecheck.TString()),
			"root-block-device": typecheck.TOptional(typecheck.TObject(
				[]typecheck.ObjectField{
					{
						Name:     "delete-on-termination",
						Type:     typecheck.TBoolean(),
						Optional: true,
					},
					{Name: "encrypted", Type: typecheck.TBoolean(), Optional: true},
					{Name: "iops", Type: typecheck.TInteger(), Optional: true},
					{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
					{Name: "throughput", Type: typecheck.TInteger(), Optional: true},
					{Name: "volume-size", Type: typecheck.TInteger(), Optional: true},
					{Name: "volume-type", Type: typecheck.TString(), Optional: true},
				})),
			"source-dest-check":      typecheck.TOptional(typecheck.TBoolean()),
			"subnet-id":              typecheck.TOptional(typecheck.TString()),
			"tags":                   typecheck.TMap(typecheck.TString()),
			"tenancy":                typecheck.TOptional(typecheck.TString()),
			"user-data":              typecheck.TOptional(typecheck.TString()),
			"user-data-base64":       typecheck.TOptional(typecheck.TString()),
			"volume-tags":            typecheck.TMap(typecheck.TString()),
			"vpc-security-group-ids": typecheck.TList(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"availability-zone":            typecheck.TString(),
			"instance-id":                  typecheck.TString(),
			"instance-state":               typecheck.TString(),
			"primary-network-interface-id": typecheck.TString(),
			"private-dns":                  typecheck.TString(),
			"private-ip":                   typecheck.TString(),
			"public-dns":                   typecheck.TString(),
			"public-ip":                    typecheck.TString(),
			"root-device-name":             typecheck.TString(),
			"root-volume-id":               typecheck.TString(),
			"subnet-id":                    typecheck.TString(),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind: "at-least-one-of",
				Fields: []string{
					"var.ami",
					"var.launch-template",
				},
			},
			{
				Kind: "at-least-one-of",
				Fields: []string{
					"var.instance-type",
					"var.launch-template",
				},
			},
			{
				Kind: "at-most-one-of",
				Fields: []string{
					"var.user-data",
					"var.user-data-base64",
				},
			},
			{
				Kind: "predicate",
				When: "(var.tenancy != null)",
				Require: "(var.tenancy == 'default' || var.tenancy == 'dedicated' || " +
					"var.tenancy == 'host')",
				Message: "tenancy must be default, dedicated, or host",
			},
			{
				Kind: "predicate",
				When: "(var.launch-template != null)",
				Require: "(((var.launch-template.id != null) && " +
					"(var.launch-template.name == null)) || " +
					"((var.launch-template.id == null) && " +
					"(var.launch-template.name != null)))",
				Message: "launch-template requires exactly one of id and name",
			},
			{
				Kind: "predicate",
				When: "(var.metadata-options.http-endpoint != null)",
				Require: "(var.metadata-options.http-endpoint == 'enabled' || " +
					"var.metadata-options.http-endpoint == 'disabled')",
				Message: "metadata-options http-endpoint must be enabled or disabled",
			},
			{
				Kind: "predicate",
				When: "(var.metadata-options.http-tokens != null)",
				Require: "(var.metadata-options.http-tokens == 'optional' || " +
					"var.metadata-options.http-tokens == 'required')",
				Message: "metadata-options http-tokens must be optional or required",
			},
			{
				Kind: "predicate",
				When: "(var.metadata-options.http-protocol-ipv6 != null)",
				Require: "(var.metadata-options.http-protocol-ipv6 == 'enabled' || " +
					"var.metadata-options.http-protocol-ipv6 == 'disabled')",
				Message: "metadata-options http-protocol-ipv6 must be enabled " +
					"or disabled",
			},
			{
				Kind: "predicate",
				When: "(var.metadata-options.instance-metadata-tags != null)",
				Require: "(var.metadata-options.instance-metadata-tags == 'enabled' " +
					"|| var.metadata-options.instance-metadata-tags == 'disabled')",
				Message: "metadata-options instance-metadata-tags must be enabled " +
					"or disabled",
			},
			{
				Kind: "predicate",
				When: "(var.metadata-options.http-put-response-hop-limit != null)",
				Require: "(var.metadata-options.http-put-response-hop-limit == null " +
					"|| var.metadata-options.http-put-response-hop-limit >= 1) && " +
					"(var.metadata-options.http-put-response-hop-limit == null || " +
					"var.metadata-options.http-put-response-hop-limit <= 64)",
				Message: "metadata-options http-put-response-hop-limit must be " +
					"1 to 64",
			},
			{
				Kind: "predicate",
				When: "((var.root-block-device.iops != null) && " +
					"(var.root-block-device.volume-type != null))",
				Require: "(var.root-block-device.volume-type == 'gp3' || " +
					"var.root-block-device.volume-type == 'io1' || " +
					"var.root-block-device.volume-type == 'io2')",
				Message: "root-block-device iops is valid only for gp3, io1, " +
					"or io2 volume types",
			},
			{
				Kind: "predicate",
				When: "(var.root-block-device.volume-type == 'io1' || " +
					"var.root-block-device.volume-type == 'io2')",
				Require: "(var.root-block-device.iops != null)",
				Message: "root-block-device iops is required when volume-type " +
					"is io1 or io2",
			},
			{
				Kind: "predicate",
				When: "((var.root-block-device.throughput != null) && " +
					"(var.root-block-device.volume-type != null))",
				Require: "(var.root-block-device.volume-type == 'gp3')",
				Message: "root-block-device throughput is valid only for gp3 volumes",
			},
			{
				Kind: "predicate",
				When: "((@each.value.iops != null) && " +
					"(@each.value.volume-type != null))",
				Require: "(@each.value.volume-type == 'gp3' || " +
					"@each.value.volume-type == 'io1' || " +
					"@each.value.volume-type == 'io2')",
				Message: "iops is valid only for gp3, io1, or io2 volume types",
				ForEach: "var.ebs-block-device",
			},
			{
				Kind: "predicate",
				When: "(@each.value.volume-type == 'io1' || " +
					"@each.value.volume-type == 'io2')",
				Require: "(@each.value.iops != null)",
				Message: "iops is required when volume-type is io1 or io2",
				ForEach: "var.ebs-block-device",
			},
			{
				Kind: "predicate",
				When: "((@each.value.throughput != null) && " +
					"(@each.value.volume-type != null))",
				Require: "(@each.value.volume-type == 'gp3')",
				Message: "throughput is valid only for gp3 volumes",
				ForEach: "var.ebs-block-device",
			},
			{
				Kind: "predicate",
				When: "!(@each.value.no-device == true)",
				Require: "((@each.value.virtual-name != null) && " +
					"(@core.length(@each.value.virtual-name) >= 1))",
				Message: "virtual-name is required unless no-device is true",
				ForEach: "var.ephemeral-block-device",
			},
		},
		Defaults: []lang.DefaultSpec{
			{Field: "var.vpc-security-group-ids", Optional: true},
			{Field: "var.ebs-block-device", Optional: true},
			{Field: "var.ephemeral-block-device", Optional: true},
			{Field: "var.volume-tags", Optional: true},
			{Field: "var.tags", Optional: true},
		},
	}
	assert.Equal(t, want, schema.Resources["ec2-instance"])
}

// TestLibraryRegistersEc2AvailabilityZones checks the runtime registration:
// ec2-availability-zones is present under DataSources and dispatches to the
// AvailabilityZonesOutput type.
func TestLibraryRegistersEc2AvailabilityZones(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.DataSources, "ec2-availability-zones")
	assert.Equal(t, reflect.TypeFor[*ec2.AvailabilityZonesOutput](),
		lib.DataSources["ec2-availability-zones"].OutputType())
}

// TestEc2AvailabilityZonesSchema asserts the whole derived TypeSchema for the
// ec2-availability-zones data source: the query inputs, the three list
// outputs, the state enum constraint, and the declared optional defaults.
func TestEc2AvailabilityZonesSchema(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.DataSources, "ec2-availability-zones")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"all-availability-zones": typecheck.TOptional(typecheck.TBoolean()),
			"exclude-names":          typecheck.TList(typecheck.TString()),
			"exclude-zone-ids":       typecheck.TList(typecheck.TString()),
			"filters": typecheck.TList(typecheck.TObject(
				[]typecheck.ObjectField{
					{Name: "name", Type: typecheck.TString()},
					{Name: "values", Type: typecheck.TList(typecheck.TString())},
				})),
			"state": typecheck.TOptional(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"group-names": typecheck.TList(typecheck.TString()),
			"names":       typecheck.TList(typecheck.TString()),
			"zone-ids":    typecheck.TList(typecheck.TString()),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind: "predicate",
				When: "(var.state != null)",
				Require: "(var.state == 'available' || var.state == 'information' || " +
					"var.state == 'impaired' || var.state == 'unavailable' || " +
					"var.state == 'constrained')",
				Message: "state must be one of available, information, impaired, " +
					"unavailable, or constrained",
			},
		},
		Defaults: []lang.DefaultSpec{
			{Field: "var.filters", Optional: true},
			{Field: "var.exclude-names", Optional: true},
			{Field: "var.exclude-zone-ids", Optional: true},
		},
	}
	assert.Equal(t, want, schema.DataSources["ec2-availability-zones"])
}
