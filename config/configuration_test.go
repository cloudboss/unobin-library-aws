package config_test

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/goschema"
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
