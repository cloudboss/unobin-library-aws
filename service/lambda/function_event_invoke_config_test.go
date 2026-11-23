package lambda

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/sourcecheck"
	"github.com/stretchr/testify/require"
)

type eventInvokeFixtureResolver struct {
	moduleDir  string
	modulePath string
}

func (r eventInvokeFixtureResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	remote, ok := ref.(*resolve.RemoteImport)
	if !ok || remote.URL != r.modulePath || remote.Subdir != "service/lambda" {
		return nil, fmt.Errorf("unexpected test import %v", ref)
	}
	dir := filepath.Join(r.moduleDir, remote.Subdir)
	return &resolve.Source{
		FS:             os.DirFS(dir),
		Path:           dir,
		ModuleRootPath: r.moduleDir,
		ModulePath:     r.modulePath,
		GoImportPath:   r.modulePath + "/" + remote.Subdir,
	}, nil
}

func analyzeEventInvokeFixture(t *testing.T, name string) (
	*syntax.FactoryBody, *program.CheckedBody, error,
) {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata/ub/event-invoke", name))
	require.NoError(t, err)
	src, err := os.ReadFile(filepath.Join(dir, "factory.ub"))
	require.NoError(t, err)
	file, err := syntax.ParseSource("factory.ub", src)
	require.NoError(t, err)
	body := &file.Factory.Body
	module := libraryModuleRoot(t)
	checked, err := sourcecheck.AnalyzeFactoryBody(*body, sourcecheck.Options{
		ProjectDir:  dir,
		Versions:    map[string]string{module.Path: "v0.0.0"},
		Resolver:    eventInvokeFixtureResolver{module.Dir, module.Path},
		SchemaCache: sourcecheck.NewSchemaCache(module, unobinModuleRoot(t)),
	})
	return body, checked, err
}

func TestEventInvokeInvalidSource(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, filepath.Join(local, "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/invalid",
		e2etest.WithSourceDirectory("modules", modules),
	)
}
