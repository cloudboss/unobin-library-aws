package ecr

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/stretchr/testify/require"
)

func TestReplicationConfigurationCompile(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, filepath.Join(local, "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/replication-compile",
		e2etest.WithSourceDirectory("modules", modules))
}

func TestReplicationCompiledScenarioPlan(t *testing.T) {
	module, core := libraryModuleRoot(t), unobinModuleRoot(t)
	out := t.TempDir()
	scenario := "../../tests/integration/scenarios/ecr-replication-configuration"
	result, err := compile.RunResult(compile.Options{
		FactoryPath: scenario,
		OutDir:      out, Build: true, GoVersion: "1.26.2", CLIVersion: "v0.13.0",
		ReplaceUnobin:    core.Dir,
		ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(directory string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(directory), nil
		}, Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	config, err := os.ReadFile(filepath.Join(scenario, "config.ub"))
	require.NoError(t, err)
	configPath := filepath.Join(out, "config.ub")
	require.NoError(t, os.WriteFile(configPath, config, 0o600))
	planPath := filepath.Join(out, "scenario.plan")
	command := exec.CommandContext(t.Context(), result.BinaryPath, "plan", "--config", configPath,
		"--out", planPath, "--allow-version-mismatch")
	command.Dir = out
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	encoded, err := os.ReadFile(planPath)
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, runtime.DecisionCreate, replicationStep(t, saved).Decision)
	require.Equal(t, map[string]any{"rules": []any{map[string]any{
		"destinations": []any{map[string]any{"region": "us-west-2", "registry-id": "000000000001"}},
		"repository-filters": []any{map[string]any{
			"filter": "unobin-initial", "filter-type": "PREFIX_MATCH",
		}},
	}}}, replicationStep(t, saved).Inputs["replication-configuration"])
}
