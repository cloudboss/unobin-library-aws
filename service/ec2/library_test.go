package ec2_test

import (
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/stretchr/testify/require"

	internal "github.com/cloudboss/unobin-library-aws/internal/service/ec2"
	awsec2 "github.com/cloudboss/unobin-library-aws/service/ec2"
)

const unobinModulePath = "github.com/cloudboss/unobin"

func libraryModuleRoot(t *testing.T) goschema.ModuleRoot {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Path}}\n{{.Dir}}").Output()
	require.NoError(t, err)
	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, parts, 2)
	return goschema.ModuleRoot{Path: parts[0], Dir: parts[1]}
}

func unobinModuleRoot(t *testing.T) goschema.ModuleRoot {
	t.Helper()
	out, err := exec.Command(
		"go", "list", "-m", "-f", "{{.Dir}}", unobinModulePath,
	).Output()
	require.NoError(t, err)
	dir := strings.TrimSpace(string(out))
	require.NotEmpty(t, dir)
	return goschema.ModuleRoot{Path: unobinModulePath, Dir: dir}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestLibraryRegistersEC2LocalKinds(t *testing.T) {
	lib := awsec2.Library()
	require.NotNil(t, lib)
	require.Equal(t, "aws-ec2", lib.Name)
	require.NotNil(t, lib.Configuration)
	require.Equal(t, reflect.TypeFor[*awscfg.Configuration](), lib.Configuration.ValueType())

	require.Equal(t, []string{
		"eip",
		"instance",
		"internet-gateway",
		"key-pair",
		"launch-template",
		"nat-gateway",
		"route",
		"route-table",
		"route-table-association",
		"security-group",
		"security-group-egress-rule",
		"security-group-ingress-rule",
		"subnet",
		"volume",
		"vpc",
		"vpc-endpoint",
	}, sortedKeys(lib.Resources))
	require.Equal(t, []string{
		"ami",
		"availability-zones",
		"security-group-data",
		"subnet-data",
		"subnets",
		"vpc-data",
	}, sortedKeys(lib.DataSources))
	require.Empty(t, sortedKeys(lib.Actions))

	resourcesOutputs := map[string]reflect.Type{
		"vpc":                         reflect.TypeFor[*internal.VpcOutput](),
		"security-group":              reflect.TypeFor[*internal.SecurityGroupOutput](),
		"security-group-ingress-rule": reflect.TypeFor[*internal.SecurityGroupIngressRuleOutput](),
		"security-group-egress-rule":  reflect.TypeFor[*internal.SecurityGroupEgressRuleOutput](),
		"subnet":                      reflect.TypeFor[*internal.SubnetOutput](),
		"volume":                      reflect.TypeFor[*internal.VolumeOutput](),
		"launch-template":             reflect.TypeFor[*internal.LaunchTemplateOutput](),
		"internet-gateway":            reflect.TypeFor[*internal.InternetGatewayOutput](),
		"route-table":                 reflect.TypeFor[*internal.RouteTableOutput](),
		"route":                       reflect.TypeFor[*internal.RouteOutput](),
		"route-table-association":     reflect.TypeFor[*internal.RouteTableAssociationOutput](),
		"eip":                         reflect.TypeFor[*internal.EipOutput](),
		"nat-gateway":                 reflect.TypeFor[*internal.NatGatewayOutput](),
		"vpc-endpoint":                reflect.TypeFor[*internal.VpcEndpointOutput](),
		"key-pair":                    reflect.TypeFor[*internal.KeyPairOutput](),
		"instance":                    reflect.TypeFor[*internal.InstanceOutput](),
	}
	for name, outputType := range resourcesOutputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.Resources[name].OutputType())
		})
	}
	dataSourcesOutputs := map[string]reflect.Type{
		"ami":                 reflect.TypeFor[*internal.AMIOutput](),
		"availability-zones":  reflect.TypeFor[*internal.AvailabilityZonesOutput](),
		"subnets":             reflect.TypeFor[*internal.SubnetsOutput](),
		"subnet-data":         reflect.TypeFor[*internal.SubnetDataOutput](),
		"security-group-data": reflect.TypeFor[*internal.SecurityGroupDataOutput](),
		"vpc-data":            reflect.TypeFor[*internal.VpcDataOutput](),
	}
	for name, outputType := range dataSourcesOutputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.DataSources[name].OutputType())
		})
	}

}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(awsec2.Library().Configuration)
	require.NoError(t, err)
	require.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	require.NotEmpty(t, view.SchemaDigest)
}

func TestReadEC2ServiceSchema(t *testing.T) {
	moduleRoot := libraryModuleRoot(t)
	unobinRoot := unobinModuleRoot(t)
	schema, warnings, err := goschema.Read(".", moduleRoot, unobinRoot)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.True(t, schema.HasConfiguration)

	configSchema, warnings, err := goschema.ReadLibraryConfiguration("../../config", unobinRoot)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, configSchema.ConfigurationIdentity, schema.ConfigurationIdentity)
	require.Equal(t, configSchema.ConfigurationDigest, schema.ConfigurationDigest)

	require.Equal(t, []string{
		"eip",
		"instance",
		"internet-gateway",
		"key-pair",
		"launch-template",
		"nat-gateway",
		"route",
		"route-table",
		"route-table-association",
		"security-group",
		"security-group-egress-rule",
		"security-group-ingress-rule",
		"subnet",
		"volume",
		"vpc",
		"vpc-endpoint",
	}, sortedKeys(schema.Resources))
	require.Equal(t, []string{
		"ami",
		"availability-zones",
		"security-group-data",
		"subnet-data",
		"subnets",
		"vpc-data",
	}, sortedKeys(schema.DataSources))
	require.Empty(t, sortedKeys(schema.Actions))
}
