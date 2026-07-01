package elbv2_test

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

	internal "github.com/cloudboss/unobin-library-aws/internal/service/elbv2"
	awselbv2 "github.com/cloudboss/unobin-library-aws/service/elbv2"
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

func TestLibraryRegistersELBv2LocalKinds(t *testing.T) {
	lib := awselbv2.Library()
	require.NotNil(t, lib)
	require.Equal(t, "aws-elbv2", lib.Name)
	require.NotNil(t, lib.Configuration)
	require.Equal(t, reflect.TypeFor[*awscfg.Configuration](), lib.Configuration.ValueType())

	require.Equal(t, []string{
		"listener",
		"listener-certificate",
		"listener-rule",
		"load-balancer",
		"target-group",
		"target-group-attachment",
	}, sortedKeys(lib.Resources))
	require.Empty(t, sortedKeys(lib.DataSources))
	require.Empty(t, sortedKeys(lib.Actions))

	resourceOutputs := map[string]reflect.Type{
		"load-balancer":           reflect.TypeFor[*internal.LoadBalancerOutput](),
		"target-group":            reflect.TypeFor[*internal.TargetGroupOutput](),
		"target-group-attachment": reflect.TypeFor[*internal.TargetGroupAttachmentOutput](),
		"listener":                reflect.TypeFor[*internal.ListenerOutput](),
		"listener-rule":           reflect.TypeFor[*internal.ListenerRuleOutput](),
		"listener-certificate":    reflect.TypeFor[*internal.ListenerCertificateOutput](),
	}
	for name, outputType := range resourceOutputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.Resources[name].OutputType())
		})
	}
}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(awselbv2.Library().Configuration)
	require.NoError(t, err)
	require.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	require.NotEmpty(t, view.SchemaDigest)
}

func TestReadELBv2ServiceSchema(t *testing.T) {
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
		"listener",
		"listener-certificate",
		"listener-rule",
		"load-balancer",
		"target-group",
		"target-group-attachment",
	}, sortedKeys(schema.Resources))
	require.Empty(t, sortedKeys(schema.DataSources))
	require.Empty(t, sortedKeys(schema.Actions))
}
