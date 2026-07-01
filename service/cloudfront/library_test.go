package cloudfront_test

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

	internal "github.com/cloudboss/unobin-library-aws/internal/service/cloudfront"
	awscloudfront "github.com/cloudboss/unobin-library-aws/service/cloudfront"
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

func TestLibraryRegistersCloudFrontLocalKinds(t *testing.T) {
	lib := awscloudfront.Library()
	require.NotNil(t, lib)
	require.Equal(t, "aws-cloudfront", lib.Name)
	require.NotNil(t, lib.Configuration)
	require.Equal(t, reflect.TypeFor[*awscfg.Configuration](), lib.Configuration.ValueType())

	require.Equal(t, []string{
		"distribution",
		"function",
		"origin-access-control",
		"response-headers-policy",
	}, sortedKeys(lib.Resources))
	require.Equal(t, []string{
		"cache-policy-data",
		"origin-request-policy-data",
	}, sortedKeys(lib.DataSources))
	require.Empty(t, sortedKeys(lib.Actions))

	resourceOutputs := map[string]reflect.Type{
		"origin-access-control":   reflect.TypeFor[*internal.OriginAccessControlOutput](),
		"function":                reflect.TypeFor[*internal.FunctionOutput](),
		"response-headers-policy": reflect.TypeFor[*internal.ResponseHeadersPolicyOutput](),
		"distribution":            reflect.TypeFor[*internal.DistributionOutput](),
	}
	for name, outputType := range resourceOutputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.Resources[name].OutputType())
		})
	}

	dataSourceOutputs := map[string]reflect.Type{
		"cache-policy-data":          reflect.TypeFor[*internal.CachePolicyDataOutput](),
		"origin-request-policy-data": reflect.TypeFor[*internal.OriginRequestPolicyDataOutput](),
	}
	for name, outputType := range dataSourceOutputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.DataSources[name].OutputType())
		})
	}
}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(awscloudfront.Library().Configuration)
	require.NoError(t, err)
	require.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	require.NotEmpty(t, view.SchemaDigest)
}

func TestReadCloudFrontServiceSchema(t *testing.T) {
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
		"distribution",
		"function",
		"origin-access-control",
		"response-headers-policy",
	}, sortedKeys(schema.Resources))
	require.Equal(t, []string{
		"cache-policy-data",
		"origin-request-policy-data",
	}, sortedKeys(schema.DataSources))
	require.Empty(t, sortedKeys(schema.Actions))
}
