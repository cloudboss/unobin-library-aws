package applicationautoscaling_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"

	aas "github.com/cloudboss/unobin-library-aws/service/applicationautoscaling"
)

func moduleRoot(t *testing.T, module string) goschema.ModuleRoot {
	t.Helper()
	args := []string{"list", "-m", "-f", "{{.Path}}\n{{.Dir}}"}
	if module != "" {
		args = append(args, module)
	}
	out, err := exec.CommandContext(t.Context(), "go", args...).CombinedOutput()
	require.NoError(t, err, string(out))
	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, parts, 2)
	return goschema.ModuleRoot{Path: parts[0], Dir: parts[1]}
}

func targetLibrary(t *testing.T) *runtime.Library {
	t.Helper()
	schema, warnings, err := goschema.Read(".", moduleRoot(t, ""),
		moduleRoot(t, "github.com/cloudboss/unobin"))
	require.NoError(t, err)
	require.Empty(t, warnings)
	library := runtime.LibraryWithPath(aas.Library(),
		"github.com/cloudboss/unobin-library-aws//service/applicationautoscaling")
	library.Schema = schema
	library.Constraints = make(map[string][]lang.ConstraintSpec)
	library.Defaults = make(map[string][]lang.DefaultSpec)
	for name, schema := range schema.Resources {
		library.Constraints["resource."+name] = schema.Constraints
		library.Defaults["resource."+name] = schema.Defaults
	}
	return library
}

func TestScalableTargetCompile(t *testing.T) {
	modules := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(modules, "local"), 0o700))
	require.NoError(t, os.Symlink(moduleRoot(t, "").Dir, filepath.Join(modules, "local", "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/compile",
		e2etest.WithSourceDirectory("modules", modules))
}
