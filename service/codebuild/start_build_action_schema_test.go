package codebuild

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/stretchr/testify/require"
)

func TestStartBuildCompile(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(moduleRoot(t).Dir, filepath.Join(local, "aws")))
	scenario := t.TempDir()
	source, err := os.ReadFile("../../tests/integration/scenarios/codebuild-start-build/factory.ub")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(scenario, "factory.ub"), source, 0o600))
	e2etest.RunSourceCases(t, "testdata/ub/start-build",
		e2etest.WithSourceDirectory("modules", modules),
		e2etest.WithSourceDirectory("scenario", scenario))
}
