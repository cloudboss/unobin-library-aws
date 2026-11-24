package sqs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/testutil"
)

func TestQueueLookupNativeRule(t *testing.T) {
	schema := readLibrarySchema(t).DataSources["queue"]
	require.Equal(t, []string{"schema: constraints[0] (predicate): queue-name is required"},
		testutil.CheckConstraints(t, schema, map[string]any{"queue-name": nil}))
	for _, name := range []string{"", " selector/unchanged ", "queue.fifo"} {
		require.Empty(t, testutil.CheckConstraints(t, schema, map[string]any{"queue-name": name}))
	}
}

func TestQueueLookupInvalidLiterals(t *testing.T) {
	module := libraryModuleRoot(t)
	modules := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(modules, "local"), 0o700))
	require.NoError(t, os.Symlink(module.Dir, filepath.Join(modules, "local", "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/queue-lookup/invalid",
		e2etest.WithSourceDirectory("modules", modules))
}

func TestQueueLookupCompiled(t *testing.T) {
	module := libraryModuleRoot(t)
	e2etest.RunCompiledCases(t, "testdata/ub/queue-lookup/compiled",
		e2etest.WithGoModule(module.Path, module.Dir))
}

func TestQueueLookupManagedFactory(t *testing.T) {
	module := libraryModuleRoot(t)
	fixture := t.TempDir()
	scenario := filepath.Join(fixture, "managed")
	require.NoError(t, os.MkdirAll(filepath.Join(scenario, "src"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(scenario, "stacks"), 0o700))
	for _, pair := range []struct{ from, to string }{
		{"../../tests/integration/scenarios/sqs/factory.ub", "src/factory.ub"},
		{"testdata/ub/queue-lookup/managed/case.json", "case.json"},
		{"../../tests/integration/scenarios/sqs/config.ub", "stacks/dev.ub"},
		{"../../tests/integration/scenarios/sqs/config-update.ub", "stacks/update.ub"},
		{"testdata/ub/queue-lookup/managed/want/validate.stdout", "want/validate.stdout"},
		{"testdata/ub/queue-lookup/managed/want/validate.stderr", "want/validate.stderr"},
	} {
		body, err := os.ReadFile(pair.from)
		require.NoError(t, err)
		target := filepath.Join(scenario, pair.to)
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
		require.NoError(t, os.WriteFile(target, body, 0o600))
	}
	e2etest.RunCompiledCases(t, fixture, e2etest.WithGoModule(module.Path, module.Dir))
}
