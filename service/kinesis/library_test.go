package kinesis_test

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/kinesis"
	awskinesis "github.com/cloudboss/unobin-library-aws/service/kinesis"
)

const unobinModulePath = "github.com/cloudboss/unobin"

func TestLibraryRegistersStreamResource(t *testing.T) {
	library := awskinesis.Library()
	require.NotNil(t, library)
	assert.Equal(t, "aws-kinesis", library.Name)
	require.NotNil(t, library.Configuration)
	assert.Equal(
		t,
		reflect.TypeFor[*awscfg.Configuration](),
		library.Configuration.ValueType(),
	)
	require.Len(t, library.Resources, 1)
	require.Contains(t, library.Resources, "stream")
	assert.Equal(
		t,
		reflect.TypeFor[*svc.StreamResourceOutput](),
		library.Resources["stream"].OutputType(),
	)
	assert.Empty(t, library.DataSources)
	assert.Empty(t, library.Actions)
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
