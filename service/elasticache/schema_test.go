package elasticache

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/elasticache"
)

const unobinModulePath = "github.com/cloudboss/unobin"

func TestLibraryRegistersSubnetGroup(t *testing.T) {
	lib := Library()

	assert.Equal(t, "aws-elasticache", lib.Name)
	require.Len(t, lib.Resources, 1)
	require.Contains(t, lib.Resources, "subnet-group")
	assert.Equal(t, reflect.TypeFor[*svc.SubnetGroupResourceOutput](),
		lib.Resources["subnet-group"].OutputType())
	assert.Empty(t, lib.DataSources)
	assert.Empty(t, lib.Actions)
}

func TestSubnetGroupSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 1)

	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"name":        typecheck.TString(),
			"description": typecheck.TString(),
			"subnet-ids":  typecheck.TList(typecheck.TString()),
			"tags":        typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		},
		Outputs: map[string]typecheck.Type{
			"name":   typecheck.TString(),
			"arn":    typecheck.TString(),
			"vpc-id": typecheck.TString(),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind:    "predicate",
				When:    "true",
				Require: "(@core.length(input.subnet-ids) >= 1)",
				Message: "subnet-ids must not be empty",
			},
			{
				Kind:    "predicate",
				When:    "true",
				Require: "(@core.length(input.tags ?? {}) <= 50)",
				Message: "tags holds at most 50 entries",
			},
		},
		Defaults: []lang.DefaultSpec{
			{Field: "input.description", Value: "''"},
		},
	}
	require.Contains(t, schema.Resources, "subnet-group")
	assert.Equal(t, want, schema.Resources["subnet-group"])
}

func readLibrarySchema(t *testing.T) *runtime.LibrarySchema {
	t.Helper()
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
	return schema
}

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
