package eks_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterAuthCompile(t *testing.T) {
	modules := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(modules, "local"), 0o700))
	require.NoError(t, os.Symlink(moduleRoot(t, ".").Dir, filepath.Join(modules, "local", "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/cluster-auth/source",
		e2etest.WithSourceDirectory("modules", modules))
}

func TestClusterAuthSchema(t *testing.T) {
	action := readLibrarySchema(t).Actions["cluster-auth"]
	require.NotNil(t, action)
	assert.Equal(t, map[string]typecheck.Type{
		"name": typecheck.TString(), "region": typecheck.TOptional(typecheck.TString()),
	}, action.Inputs)
	assert.Equal(t, map[string]typecheck.Type{"token": typecheck.TString()}, action.Outputs)
	assert.Equal(t, []string{"token"}, action.SensitiveOutputs)
	assert.Empty(t, action.SensitiveInputs)
	assert.Empty(t, action.Defaults)
}

func TestClusterAuthCompiledSchema(t *testing.T) {
	module := moduleRoot(t, ".")
	e2etest.RunCompiledCases(t, "testdata/ub/cluster-auth/compiled",
		e2etest.WithGoModule(module.Path, module.Dir))
}
