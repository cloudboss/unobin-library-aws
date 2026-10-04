package config_test

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/stretchr/testify/require"

	awslibconfig "github.com/cloudboss/unobin-library-aws/config"
)

const unobinModulePath = "github.com/cloudboss/unobin"

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

func TestLibraryConfigurationView(t *testing.T) {
	registration := awslibconfig.LibraryConfiguration()
	require.NotNil(t, registration)
	require.Equal(t, reflect.TypeFor[*awscfg.Configuration](), registration.ValueType())

	view, err := cfg.View(registration)
	require.NoError(t, err)
	require.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	require.NotEmpty(t, view.SchemaDigest)
}

func TestLibraryRegistersConfiguration(t *testing.T) {
	lib := awslibconfig.Library()
	require.NotNil(t, lib)
	require.Equal(t, "aws-config", lib.Name)
	require.Equal(t, runtime.LibraryCompatibility{
		RequiredAPI:            "1.0",
		SuggestedUnobinVersion: "v0.12.0",
	}, lib.Compatibility)
	require.Empty(t, lib.Resources)
	require.Empty(t, lib.DataSources)
	require.Empty(t, lib.Actions)

	view, err := cfg.View(lib.Configuration)
	require.NoError(t, err)
	expected, err := cfg.View(awslibconfig.LibraryConfiguration())
	require.NoError(t, err)
	require.Equal(t, expected.Identity, view.Identity)
	require.Equal(t, expected.SchemaDigest, view.SchemaDigest)

	moduleRoot, err := filepath.Abs("..")
	require.NoError(t, err)
	declaration, err := golibrary.ReadCompatibility(moduleRoot, ".")
	require.NoError(t, err)
	require.Equal(t, lib.Compatibility.RequiredAPI, declaration.RequiredAPI)
	require.Equal(t, lib.Compatibility.SuggestedUnobinVersion, declaration.SuggestedUnobinVersion)
	require.NoError(t, libraryapi.Check(declaration.RequiredAPI, libraryapi.Current()))
}

func TestReadLibraryConfiguration(t *testing.T) {
	view, err := cfg.View(awslibconfig.LibraryConfiguration())
	require.NoError(t, err)

	schema, warnings, err := goschema.ReadLibraryConfiguration(".", unobinModuleRoot(t))
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.True(t, schema.HasConfiguration)
	require.Equal(t, view.Identity, schema.ConfigurationIdentity)
	require.Equal(t, view.SchemaDigest, schema.ConfigurationDigest)
	require.NotEmpty(t, schema.ConfigurationFields)
}
