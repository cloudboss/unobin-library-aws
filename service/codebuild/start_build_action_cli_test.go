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

func TestStartBuildCompiledCLI(t *testing.T) {
	endpoint, requests := buildEndpoint(t)
	module, dep := moduleRoot(t), moduleRoot(t, "github.com/cloudboss/unobin")
	out := t.TempDir()
	result, err := compile.RunResult(compile.Options{
		FactoryPath: "testdata/ub/start-build/evaluated", OutDir: out,
		Build: true, GoVersion: "1.26.2", CLIVersion: "v0.13.0",
		ReplaceUnobin: dep.Dir, ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(dir string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(dir), nil
		}, Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	source, err := os.ReadFile("testdata/ub/start-build/evaluated/config.ub")
	require.NoError(t, err)
	config := filepath.Join(out, "config.ub")
	require.NoError(t, os.WriteFile(config,
		[]byte(strings.ReplaceAll(string(source), "ENDPOINT", endpoint)), 0o600))
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
	assert.Equal(t, runtime.DecisionRerun, buildStep(t, plan()).Decision)
	output, err := command("apply", planPath)
	require.NoError(t, err, string(output))
	assert.Equal(t, expectedBuildRequests(map[string]any{"projectName": ""}, "us-east-1"),
		requests())
	saved := plan()
	assert.Equal(t, runtime.DecisionSkip, buildStep(t, saved).Decision)
	output, err = command("apply", planPath)
	require.NoError(t, err, string(output))
	assert.Empty(t, requests())
	for _, test := range buildInvalidCases() {
		saved = plan()
		saved.Inputs[test.field] = test.value
		changed, err := json.Marshal(saved)
		require.NoError(t, err)
		encoded, err := state.Seal(changed, state.PayloadTypePlan, encrypters.Noop{})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(planPath, encoded, 0o600))
		output, err = command("apply", planPath)
		require.Error(t, err)
		assert.Contains(t, string(output), "action.build: field \""+test.path+"\"")
		assert.Contains(t, string(output), "required but is null")
		assert.Empty(t, requests())
	}
}
