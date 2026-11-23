package eks_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/stretchr/testify/require"
)

func TestClusterLookupInvalidLiterals(t *testing.T) {
	module := moduleRoot(t, ".")
	modules := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(modules, "local"), 0o700))
	require.NoError(t, os.Symlink(module.Dir, filepath.Join(modules, "local", "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/cluster-lookup/invalid",
		e2etest.WithSourceDirectory("modules", modules),
	)
}

func TestClusterLookupCompiled(t *testing.T) {
	module := moduleRoot(t, ".")
	e2etest.RunCompiledCases(t, "testdata/ub/cluster-lookup/compiled",
		e2etest.WithGoModule(module.Path, module.Dir),
	)
}
