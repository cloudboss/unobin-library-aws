package applicationautoscaling_test

import (
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func targetCompileOptions(t *testing.T, factory, out string) compile.Options {
	t.Helper()
	module := moduleRoot(t, "")
	return compile.Options{
		FactoryPath: factory, OutDir: out, CLIVersion: "v0.13.0", GoVersion: "1.26.2",
		ReplaceUnobin:    moduleRoot(t, "github.com/cloudboss/unobin").Dir,
		ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(directory string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(directory), nil
		}, Stdout: io.Discard, Stderr: io.Discard,
	}
}

func TestScalableTargetCompiledSavedPlan(t *testing.T) {
	a := newTargetAPI(t)
	build := t.TempDir()
	options := targetCompileOptions(t, "testdata/ub/evaluated", build)
	options.StackName, options.Build, options.Version = "target-cli", true, "v0.0.0"
	result, err := compile.RunResult(options)
	require.NoError(t, err)
	writeConfig := func(selector string) {
		t.Helper()
		body, err := os.ReadFile("testdata/ub/cli/config.ub")
		require.NoError(t, err)
		text := strings.ReplaceAll(string(body), "__ENDPOINT__", a.endpoint)
		text = strings.ReplaceAll(text, "__REVISION__", result.ContentRevision)
		text = strings.ReplaceAll(text, "__MIN__", selector)
		require.NoError(t, os.WriteFile(filepath.Join(build, "config.ub"), []byte(text), 0o600))
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
		cmd.Dir = build
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	openPlan := func() *runtime.PlanFile {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(build, "saved.ubp"))
		require.NoError(t, err)
		plan, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
			return encrypters.Noop{}, nil
		})
		require.NoError(t, err)
		return plan
	}
	writeConfig("preferred")
	run("plan", "-c", "config.ub", "-o", "saved.ubp")
	plan := openPlan()
	step := targetStep(t, plan)
	assert.Equal(t, runtime.DecisionCreate, step.Decision)
	assert.Equal(t, int64(0), step.Inputs["min-capacity"])
	assert.Equal(t, map[string]any{"dynamic-scaling-in-suspended": nil,
		"dynamic-scaling-out-suspended": nil, "scheduled-scaling-suspended": nil},
		step.Inputs["suspended-state"])
	run("apply", "saved.ubp")
	assert.Equal(t, append([]targetCall{{"RegisterScalableTarget", targetKey}},
		targetReadCalls(targetKey)...), a.takeCalls())
	run("plan", "-c", "config.ub", "-o", "saved.ubp")
	unchanged := targetStep(t, openPlan())
	assert.Equal(t, runtime.DecisionNoOp, unchanged.Decision)
	assert.Equal(t, targetKey, unchanged.ObservedOutputs["scalable-target-arn"])
	assert.NotContains(t, unchanged.ObservedOutputs, "arn")
	assert.Equal(t, map[string]any{"dynamic-scaling-in-suspended": false,
		"dynamic-scaling-out-suspended": false, "scheduled-scaling-suspended": false},
		unchanged.ObservedOutputs["suspended-state"])
	assert.Equal(t, targetReadCalls(targetKey), a.takeCalls())
	run("apply", "saved.ubp")
	assert.Empty(t, a.takeCalls())
	writeConfig("absent")
	cmd := exec.CommandContext(t.Context(), result.BinaryPath,
		"plan", "-c", "config.ub", "-o", "invalid.ubp")
	cmd.Dir = build
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(out), `resource.target: field "min-capacity": required but is null`)
	assert.Empty(t, a.takeCalls())
}

func TestScalableTargetIntegrationFactory(t *testing.T) {
	options := targetCompileOptions(t,
		"../../tests/integration/scenarios/applicationautoscaling-scalable-target", t.TempDir())
	options.StackName = "applicationautoscaling-scalable-target"
	_, err := compile.RunResult(options)
	require.NoError(t, err)
}
