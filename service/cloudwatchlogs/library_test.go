package cloudwatchlogs_test

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

	svc "github.com/cloudboss/unobin-library-aws/internal/service/cloudwatchlogs"
	awscloudwatchlogs "github.com/cloudboss/unobin-library-aws/service/cloudwatchlogs"
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

func TestLibraryRegistersCloudWatchLogsLocalKinds(t *testing.T) {
	lib := awscloudwatchlogs.Library()
	require.NotNil(t, lib)
	require.Equal(t, "aws-cloudwatchlogs", lib.Name)
	require.NotNil(t, lib.Configuration)
	require.Equal(t, reflect.TypeFor[*awscfg.Configuration](), lib.Configuration.ValueType())

	require.Equal(t, []string{
		"log-group",
		"metric-filter",
		"resource-policy",
		"subscription-filter",
	}, sortedKeys(lib.Resources))
	require.Empty(t, sortedKeys(lib.DataSources))
	require.Empty(t, sortedKeys(lib.Actions))

	resourceOutputs := map[string]reflect.Type{
		"log-group":           reflect.TypeFor[*svc.LogGroupOutput](),
		"subscription-filter": reflect.TypeFor[*svc.SubscriptionFilterOutput](),
		"metric-filter":       reflect.TypeFor[*svc.MetricFilterOutput](),
		"resource-policy":     reflect.TypeFor[*svc.ResourcePolicyOutput](),
	}
	for name, outputType := range resourceOutputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.Resources[name].OutputType())
		})
	}
}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(awscloudwatchlogs.Library().Configuration)
	require.NoError(t, err)
	require.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	require.NotEmpty(t, view.SchemaDigest)
}

func TestReadCloudWatchLogsServiceSchema(t *testing.T) {
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
		"log-group",
		"metric-filter",
		"resource-policy",
		"subscription-filter",
	}, sortedKeys(schema.Resources))
	require.Empty(t, sortedKeys(schema.DataSources))
	require.Empty(t, sortedKeys(schema.Actions))
}
