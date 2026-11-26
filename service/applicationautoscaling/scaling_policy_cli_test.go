package applicationautoscaling_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScalingPolicyCompiledSavedPlan(t *testing.T) {
	a := newPolicyAPI(t)
	build := t.TempDir()
	options := targetCompileOptions(t, "testdata/ub/policy/compiled", build)
	options.StackName, options.Build, options.Version = "policy-cli", true, "v0.0.0"
	result, err := compile.RunResult(options)
	require.NoError(t, err)
	writeConfig := func(selector string) {
		t.Helper()
		body, err := os.ReadFile("testdata/ub/policy/cli/config.ub")
		require.NoError(t, err)
		text := strings.ReplaceAll(string(body), "__ENDPOINT__", a.endpoint)
		text = strings.ReplaceAll(text, "__REVISION__", result.ContentRevision)
		text = strings.ReplaceAll(text, "__NAME__", selector)
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
	step := policyStep(t, openPlan())
	assert.Equal(t, runtime.DecisionCreate, step.Decision)
	assert.Equal(t, "StepScaling", step.Inputs["policy-type"])
	run("apply", "saved.ubp")
	assert.Equal(t,
		[]targetCall{{"PutScalingPolicy",
			policyKey},
			{"DescribeScalingPolicies",
				policyKey}},
		a.takeCalls())
	run("plan", "-c", "config.ub", "-o", "saved.ubp")
	step = policyStep(t, openPlan())
	assert.Equal(t, runtime.DecisionNoOp, step.Decision)
	assert.Equal(t, policyKey, step.ObservedOutputs["policy-arn"])
	assert.Equal(t, []targetCall{{"DescribeScalingPolicies", policyKey}}, a.takeCalls())
	invalid := openPlan()
	invalidName := strings.Repeat("x", 256)
	invalid.Inputs["name"] = invalidName
	policyStep(t, invalid).Inputs["name"] = invalidName
	body, err := json.Marshal(invalid)
	require.NoError(t, err)
	envelope, err := state.Seal(body, state.PayloadTypePlan, encrypters.Noop{})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(build, "invalid-noop.ubp"), envelope, 0o600))
	invalidApply := exec.CommandContext(t.Context(), result.BinaryPath, "apply", "invalid-noop.ubp")
	invalidApply.Dir = build
	rejected, err := invalidApply.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(rejected), "resource.policy: schema: constraints[")
	require.Contains(t, string(rejected), "name must be at most 255 bytes")
	assert.Empty(t, a.takeCalls())
	run("plan", "-c", "config.ub", "-o", "saved.ubp")
	a.takeCalls()
	run("apply", "saved.ubp")
	assert.Empty(t, a.takeCalls())
	writeConfig("absent")
	cmd := exec.CommandContext(t.Context(), result.BinaryPath,
		"plan", "-c", "config.ub", "-o", "invalid.ubp")
	cmd.Dir = build
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(out), `resource.policy: field "name": required but is null`)
	assert.Empty(t, a.takeCalls())
}

func TestScalingPolicyIntegrationFactory(t *testing.T) {
	options := targetCompileOptions(t,
		"../../tests/integration/scenarios/applicationautoscaling-scaling-policy", t.TempDir())
	options.StackName = "applicationautoscaling-scaling-policy"
	_, err := compile.RunResult(options)
	require.NoError(t, err)
}
