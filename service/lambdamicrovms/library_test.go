package lambdamicrovms_test

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

	svc "github.com/cloudboss/unobin-library-aws/internal/service/lambdamicrovms"
	awslambdamicrovms "github.com/cloudboss/unobin-library-aws/service/lambdamicrovms"
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

func typ[T any]() reflect.Type {
	return reflect.TypeFor[T]()
}

func expectedDataSources() []string {
	return []string{
		"managed-microvm-image-versions",
		"managed-microvm-images",
		"microvm",
		"microvm-image",
		"microvm-image-build",
		"microvm-image-builds",
		"microvm-image-version",
		"microvm-image-versions",
		"microvm-images",
		"microvms",
	}
}

func expectedActions() []string {
	return []string{
		"create-microvm-auth-token",
		"create-microvm-shell-auth-token",
		"resume-microvm",
		"run-microvm",
		"suspend-microvm",
		"terminate-microvm",
		"update-microvm-image-version-status",
	}
}

func TestLibraryRegistersLambdaMicroVMsLocalKinds(t *testing.T) {
	lib := awslambdamicrovms.Library()
	require.NotNil(t, lib)
	require.Equal(t, "aws-lambdamicrovms", lib.Name)
	require.NotNil(t, lib.Configuration)
	require.Equal(t, reflect.TypeFor[*awscfg.Configuration](), lib.Configuration.ValueType())

	require.Equal(t, []string{"microvm-image"}, sortedKeys(lib.Resources))
	require.Equal(t, expectedDataSources(), sortedKeys(lib.DataSources))
	require.Equal(t, expectedActions(), sortedKeys(lib.Actions))

	require.Equal(
		t,
		reflect.TypeFor[*svc.MicrovmImageResourceOutput](),
		lib.Resources["microvm-image"].OutputType(),
	)

	dataSourceOutputs := map[string]reflect.Type{
		"managed-microvm-image-versions": typ[*svc.ManagedMicrovmImageVersionsDataSourceOutput](),
		"managed-microvm-images":         reflect.TypeFor[*svc.ManagedMicrovmImagesDataSourceOutput](),
		"microvm":                        reflect.TypeFor[*svc.MicrovmDataSourceOutput](),
		"microvm-image":                  reflect.TypeFor[*svc.MicrovmImageDataSourceOutput](),
		"microvm-image-build":            reflect.TypeFor[*svc.MicrovmImageBuildDataSourceOutput](),
		"microvm-image-builds":           reflect.TypeFor[*svc.MicrovmImageBuildsDataSourceOutput](),
		"microvm-image-version":          reflect.TypeFor[*svc.MicrovmImageVersionDataSourceOutput](),
		"microvm-image-versions":         reflect.TypeFor[*svc.MicrovmImageVersionsDataSourceOutput](),
		"microvm-images":                 reflect.TypeFor[*svc.MicrovmImagesDataSourceOutput](),
		"microvms":                       reflect.TypeFor[*svc.MicrovmsDataSourceOutput](),
	}
	for name, outputType := range dataSourceOutputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.DataSources[name].OutputType())
		})
	}

	actionOutputs := map[string]reflect.Type{
		"create-microvm-auth-token":           reflect.TypeFor[*svc.MicrovmAuthTokenActionOutput](),
		"create-microvm-shell-auth-token":     reflect.TypeFor[*svc.MicrovmShellAuthTokenActionOutput](),
		"resume-microvm":                      reflect.TypeFor[*svc.ResumeMicrovmActionOutput](),
		"run-microvm":                         reflect.TypeFor[*svc.RunMicrovmActionOutput](),
		"suspend-microvm":                     reflect.TypeFor[*svc.SuspendMicrovmActionOutput](),
		"terminate-microvm":                   reflect.TypeFor[*svc.TerminateMicrovmActionOutput](),
		"update-microvm-image-version-status": typ[*svc.UpdateMicrovmImageVersionStatusActionOutput](),
	}
	for name, outputType := range actionOutputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.Actions[name].OutputType())
		})
	}
}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(awslambdamicrovms.Library().Configuration)
	require.NoError(t, err)
	require.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	require.NotEmpty(t, view.SchemaDigest)
}

func TestReadLambdaMicroVMsServiceSchema(t *testing.T) {
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

	require.Equal(t, []string{"microvm-image"}, sortedKeys(schema.Resources))
	require.Equal(t, expectedDataSources(), sortedKeys(schema.DataSources))
	require.Equal(t, expectedActions(), sortedKeys(schema.Actions))
}
