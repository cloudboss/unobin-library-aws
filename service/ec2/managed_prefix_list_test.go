package ec2

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestManagedPrefixListCompileInputsAndOutputs(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, filepath.Join(local, "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/managed-prefix-list/compile",
		e2etest.WithSourceDirectory("modules", modules))
}

func TestManagedPrefixListCompiledCLIEnvelopeAndApply(t *testing.T) {
	module, unobin := libraryModuleRoot(t), unobinModuleRoot(t)
	for _, fixture := range []string{"literal", "scenario"} {
		t.Run(fixture, func(t *testing.T) {
			path := "testdata/ub/managed-prefix-list/compile/valid/factory.ub"
			if fixture == "scenario" {
				path = "../../tests/integration/scenarios/ec2-managed-prefix-list/factory.ub"
			}
			out := t.TempDir()
			result, err := compile.RunResult(compile.Options{
				FactoryPath: path, OutDir: out, Build: true,
				GoVersion: "1.26.2", CLIVersion: "v0.13.0", ReplaceUnobin: unobin.Dir,
				ReplaceGoModules: map[string]string{module.Path: module.Dir},
				NewResolver: func(directory string) (resolve.Resolver, error) {
					return resolve.NewLocalResolver(directory), nil
				}, Stdout: io.Discard, Stderr: io.Discard,
			})
			require.NoError(t, err)
			if fixture == "scenario" {
				return
			}
			cloud := &prefixCloud{}
			endpoint := cloud.endpoint(t)
			template, err := os.ReadFile(filepath.Join(filepath.Dir(path), "config.ub"))
			require.NoError(t, err)
			configPath, planPath := filepath.Join(out, "config.ub"), filepath.Join(out, "saved.plan")
			require.NoError(t, os.WriteFile(configPath,
				[]byte(strings.ReplaceAll(string(template), "ENDPOINT", endpoint)), 0o600))
			run := func(args ...string) {
				cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
				cmd.Dir = out
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, string(output))
			}
			planArgs := []string{"plan", "--config", configPath, "--out", planPath,
				"--allow-version-mismatch"}
			run(planArgs...)
			open := func() *runtime.PlanFile {
				body, err := os.ReadFile(planPath)
				require.NoError(t, err)
				plan, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
					return encrypters.Noop{}, nil
				})
				require.NoError(t, err)
				return plan
			}
			step := prefixStep(t, open())
			assert.Equal(t, runtime.DecisionCreate, step.Decision)
			assert.Equal(t, map[string]any{"address-family": "IPv4", "name": "desired",
				"max-entries": int64(8), "entries": []any{
					map[string]any{"cidr": "10.0.0.1/08", "description": nil},
					map[string]any{"cidr": "2001:DB8::1/064", "description": ""}},
				"tags": map[string]any{"blank": ""}}, step.Inputs)
			run("apply", planPath)
			assert.True(t, cloud.exists)
			run(planArgs...)
			assert.Equal(t, runtime.DecisionNoOp, prefixStep(t, open()).Decision)
			cloud.mutations()
			run("apply", planPath)
			assert.Empty(t, cloud.mutations())
			run("plan", "--destroy", "--config", configPath, "--out", planPath,
				"--allow-version-mismatch")
			run("apply", planPath)
			assert.False(t, cloud.exists)
		})
	}
}
