package ecr

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/stretchr/testify/require"
)

func TestRepositoryLookupCompile(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, filepath.Join(local, "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/repository-lookup",
		e2etest.WithSourceDirectory("modules", modules))
}

func TestRepositoryLookupBuild(t *testing.T) {
	module := libraryModuleRoot(t)
	core := unobinModuleRoot(t)
	err := compile.Run(compile.Options{
		FactoryPath: filepath.Join("testdata", "ub", "repository-lookup", "valid"),
		OutDir:      t.TempDir(), GoVersion: "1.26.2", CLIVersion: "v0.13.0",
		ReplaceUnobin:    core.Dir,
		ReplaceGoModules: map[string]string{module.Path: module.Dir},
		Build:            true,
		Stdout:           io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
}
