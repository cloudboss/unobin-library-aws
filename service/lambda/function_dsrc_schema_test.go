package lambda

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/stretchr/testify/require"
)

func TestFunctionLookupInvalidSource(t *testing.T) {
	modules := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(modules, "local"), 0o700))
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, filepath.Join(modules, "local", "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/function-lookup/invalid",
		e2etest.WithSourceDirectory("modules", modules))
}

func TestFunctionLookupCompiledSchema(t *testing.T) {
	module := libraryModuleRoot(t)
	e2etest.RunCompiledCases(t, "testdata/ub/function-lookup/compiled",
		e2etest.WithGoModule(module.Path, module.Dir))
}
