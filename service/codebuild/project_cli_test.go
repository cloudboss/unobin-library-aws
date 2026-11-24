package codebuild

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectCompiledCLI(t *testing.T) {
	c := newProjectCloud(t)
	module, unobin := moduleRoot(t), moduleRoot(t, "github.com/cloudboss/unobin")
	out := t.TempDir()
	result, err := compile.RunResult(compile.Options{
		FactoryPath: "testdata/ub/compiled/factory.ub", OutDir: out,
		Build: true, GoVersion: "1.26.2", CLIVersion: "v0.13.0",
		ReplaceUnobin: unobin.Dir, ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(dir string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(dir), nil
		}, Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	source, err := os.ReadFile("testdata/ub/runtime/evaluated/config.ub")
	require.NoError(t, err)
	config := filepath.Join(out, "config.ub")
	require.NoError(t, os.WriteFile(config,
		[]byte(strings.ReplaceAll(string(source), "ENDPOINT", c.endpoint)), 0o600))
	planPath := filepath.Join(out, "saved.plan")
	command := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
		cmd.Dir = out
		return cmd.CombinedOutput()
	}
	plan := func() *runtime.PlanFile {
		output, err := command("plan", "-c", config, "-o", planPath, "--allow-version-mismatch")
		require.NoError(t, err, string(output))
		encoded, err := os.ReadFile(planPath)
		require.NoError(t, err)
		saved, err := runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
			return encrypters.Noop{}, nil
		})
		require.NoError(t, err)
		return saved
	}
	first := plan()
	assert.Equal(t, runtime.DecisionCreate, projectStep(t, first).Decision)
	assert.Equal(t, int64(480), projectStep(t, first).Inputs["queued-timeout-in-minutes"])
	output, err := command("apply", planPath)
	require.NoError(t, err, string(output))
	saved := plan()
	assert.Equal(t, runtime.DecisionNoOp, projectStep(t, saved).Decision)
	c.take()
	body, err := json.Marshal(saved)
	require.NoError(t, err)
	for _, field := range []string{"image", "batch-compute-types"} {
		require.NoError(t, json.Unmarshal(body, &saved))
		message := `field "environment.image": required but is null`
		saved.Inputs[field] = nil
		if field == "batch-compute-types" {
			saved.Inputs[field] = []any{"custom"}
			message = "batch compute type must be a supported value"
		}
		for i := range saved.Steps {
			if saved.Steps[i].Address != "resource.project" {
				continue
			}
			if field == "image" {
				saved.Steps[i].Inputs["environment"].(map[string]any)["image"] = nil
			} else {
				saved.Steps[i].Inputs["build-batch-config"] = map[string]any{
					"service-role": "arn:aws:iam::123456789012:role/build",
					"restrictions": map[string]any{"compute-types-allowed": []any{"custom"}},
				}
			}
		}
		changed, err := json.Marshal(saved)
		require.NoError(t, err)
		encoded, err := state.Seal(changed, state.PayloadTypePlan, encrypters.Noop{})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(planPath, encoded, 0o600))
		output, err = command("apply", planPath)
		require.Error(t, err)
		assert.Contains(t, string(output), "resource.project:")
		if field == "batch-compute-types" {
			assert.Contains(t, string(output), "schema: constraints[")
		}
		assert.Contains(t, string(output), message)
		assert.Empty(t, c.take())
	}
}
