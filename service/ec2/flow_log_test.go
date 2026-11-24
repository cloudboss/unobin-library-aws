package ec2

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlowLogCompileInputsAndOutputs(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, filepath.Join(local, "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/flow-log/compile",
		e2etest.WithSourceDirectory("modules", modules))
}

func TestFlowLogCompiledPlanEnvelopeAndScenario(t *testing.T) {
	module, unobin := libraryModuleRoot(t), unobinModuleRoot(t)
	for _, name := range []string{"literal", "scenario"} {
		t.Run(name, func(t *testing.T) {
			path := "testdata/ub/flow-log/compile/valid/factory.ub"
			if name == "scenario" {
				path = "../../tests/integration/scenarios/ec2-flow-log/factory.ub"
			}
			out := t.TempDir()
			result, err := compile.RunResult(compile.Options{
				FactoryPath: path, OutDir: out, Build: true, GoVersion: "1.26.2", CLIVersion: "v0.13.0",
				ReplaceUnobin: unobin.Dir, ReplaceGoModules: map[string]string{module.Path: module.Dir},
				NewResolver: func(directory string) (resolve.Resolver, error) {
					return resolve.NewLocalResolver(directory), nil
				}, Stdout: io.Discard, Stderr: io.Discard,
			})
			require.NoError(t, err)
			if name == "scenario" {
				return
			}
			config, err := os.ReadFile("testdata/ub/flow-log/compile/valid/config.ub")
			require.NoError(t, err)
			configPath := filepath.Join(out, "config.ub")
			require.NoError(t, os.WriteFile(configPath, config, 0o600))
			planPath := filepath.Join(out, "saved.plan")
			cmd := exec.CommandContext(t.Context(), result.BinaryPath, "plan", "--config", configPath,
				"--out", planPath, "--allow-version-mismatch")
			cmd.Dir = out
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))
			encoded, err := os.ReadFile(planPath)
			require.NoError(t, err)
			saved, err := runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
				return encrypters.Noop{}, nil
			})
			require.NoError(t, err)
			step := peeringRuntimeStep(t, saved)
			assert.Equal(t, runtime.DecisionCreate, step.Decision)
			assert.Equal(t, map[string]any{"vpc-id": "vpc-1", "destination-options": map[string]any{},
				"tags": map[string]any{"blank": ""}}, step.Inputs)
		})
	}
}
