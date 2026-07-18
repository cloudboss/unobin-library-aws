package wafv2_test

import (
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/wafv2"
	awswafv2 "github.com/cloudboss/unobin-library-aws/service/wafv2"
)

const unobinModulePath = "github.com/cloudboss/unobin"

func TestLibraryRegistersWebACLResource(t *testing.T) {
	library := awswafv2.Library()
	require.NotNil(t, library)
	assert.Equal(t, "aws-wafv2", library.Name)
	require.NotNil(t, library.Configuration)
	assert.Equal(t, reflect.TypeFor[*awscfg.Configuration](),
		library.Configuration.ValueType())
	require.Equal(t, []string{"web-acl"}, registrationKeys(library.Resources))
	registration := library.Resources["web-acl"]
	assert.Equal(t, reflect.TypeFor[*svc.WebACLResource](),
		reflect.TypeOf(registration.NewReceiver()))
	assert.Equal(t, reflect.TypeFor[*svc.WebACLResourceOutput](),
		registration.OutputType())
	assert.Empty(t, library.DataSources)
	assert.Empty(t, library.Actions)
}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(awswafv2.Library().Configuration)
	require.NoError(t, err)
	assert.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	assert.NotEmpty(t, view.SchemaDigest)
}

func readLibrarySchema(t *testing.T) *runtime.LibrarySchema {
	t.Helper()
	moduleRoot := moduleRoot(t, ".")
	unobinRoot := unobinRoot(t)
	schema, warnings, err := goschema.Read(".", moduleRoot, unobinRoot)
	require.NoError(t, err)
	require.Empty(t, warnings)
	return schema
}

func moduleRoot(t *testing.T, directory string) goschema.ModuleRoot {
	t.Helper()
	command := exec.Command("go", "list", "-m", "-f", "{{.Path}}\n{{.Dir}}")
	command.Dir = directory
	out, err := command.Output()
	require.NoError(t, err)
	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, parts, 2)
	return goschema.ModuleRoot{Path: parts[0], Dir: parts[1]}
}

func unobinRoot(t *testing.T) goschema.ModuleRoot {
	t.Helper()
	out, err := exec.Command(
		"go", "list", "-m", "-f", "{{.Dir}}", unobinModulePath,
	).Output()
	require.NoError(t, err)
	directory := strings.TrimSpace(string(out))
	require.NotEmpty(t, directory)
	return goschema.ModuleRoot{Path: unobinModulePath, Dir: directory}
}

func registrationKeys(registrations map[string]runtime.ResourceRegistration) []string {
	keys := make([]string, 0, len(registrations))
	for key := range registrations {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
