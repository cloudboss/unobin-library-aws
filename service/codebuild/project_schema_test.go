package codebuild

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func moduleRoot(t *testing.T, args ...string) goschema.ModuleRoot {
	t.Helper()
	command := append([]string{"list", "-m", "-f", "{{.Path}}\n{{.Dir}}"}, args...)
	out, err := exec.Command("go", command...).Output()
	require.NoError(t, err)
	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, parts, 2)
	return goschema.ModuleRoot{Path: parts[0], Dir: parts[1]}
}

func projectSchema(t *testing.T) *runtime.LibrarySchema {
	t.Helper()
	schema, warnings, err := goschema.Read(".", moduleRoot(t),
		moduleRoot(t, "github.com/cloudboss/unobin"))
	require.NoError(t, err)
	require.Empty(t, warnings)
	return schema
}

func TestProjectCompile(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(moduleRoot(t).Dir, filepath.Join(local, "aws")))
	scenario := t.TempDir()
	source, err := os.ReadFile("../../tests/integration/scenarios/codebuild-project/factory.ub")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(scenario, "factory.ub"), source, 0o600))
	e2etest.RunSourceCases(t, "testdata/ub/compile",
		e2etest.WithSourceDirectory("modules", modules),
		e2etest.WithSourceDirectory("scenario", scenario))
}
