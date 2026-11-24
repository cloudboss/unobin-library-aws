package eventbridge

import (
	"encoding/json"
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
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventBusPolicyCompile(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, filepath.Join(local, "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/bus-policy-compile",
		e2etest.WithSourceDirectory("modules", modules))
}

func TestEventBusPolicyCompiledCLI(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	module, unobin := libraryModuleRoot(t), unobinModuleRoot(t)
	build := t.TempDir()
	result, err := compile.RunResult(compile.Options{
		FactoryPath: "testdata/ub/bus-policy/compiled", OutDir: build, Build: true,
		GoVersion: "1.26.2", CLIVersion: "v0.13.0", Version: "v0.0.0",
		StackName: "bus-policy", ReplaceUnobin: unobin.Dir,
		ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(directory string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(directory), nil
		}, Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	body, err := os.ReadFile("testdata/ub/bus-policy/compiled/config.ub")
	require.NoError(t, err)
	config := strings.ReplaceAll(string(body), "__ENDPOINT__", endpoint)
	config = strings.ReplaceAll(config, "__REVISION__", result.ContentRevision)
	configPath, planPath := filepath.Join(build, "config.ub"), filepath.Join(build, "saved.ubp")
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))
	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
		cmd.Dir = build
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	open := func() *runtime.PlanFile {
		t.Helper()
		body, err := os.ReadFile(planPath)
		require.NoError(t, err)
		saved, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
			return encrypters.Noop{}, nil
		})
		require.NoError(t, err)
		return saved
	}
	out, err := run("plan", "-c", configPath, "-o", planPath)
	require.NoError(t, err, out)
	step := policyPlanStep(t, open())
	assert.Equal(t, runtime.DecisionCreate, step.Decision)
	assert.Equal(t, map[string]any{"event-bus-name": "external-bus", "policy": `{"Statement":[]}`},
		step.Inputs)
	assert.Empty(t, step.SensitiveInputs)
	assert.Empty(t, step.SensitiveOutputs)
	out, err = run("apply", planPath)
	require.NoError(t, err, out)
	assertPolicyMutations(t, api, []busPolicyRequest{policyPut("external-bus", `{"Statement":[]}`)})
	out, err = run("plan", "-c", configPath, "-o", planPath)
	require.NoError(t, err, out)
	require.Equal(t, runtime.DecisionNoOp, policyPlanStep(t, open()).Decision)
	out, err = run("apply", planPath)
	require.NoError(t, err, out)
	assertPolicyMutations(t, api, nil)
	out, err = run("plan", "-c", configPath, "-o", planPath)
	require.NoError(t, err, out)
	api.takeCalls()
	for _, tt := range []struct {
		name, input, field string
		value              any
		message            string
	}{
		{"null policy", "policy", "policy", nil, `field "policy": required but is null`},
		{"missing policy input", "policy", "policy", nil,
			`field "policy": eval: input.policy: not found`},
		{"null bus", "bus", "event-bus-name", nil,
			`field "event-bus-name": required but is null`},
		{"invalid JSON", "policy", "policy", "broken", "schema: constraints[2]"},
		{"invalid bus", "bus", "event-bus-name", "invalid:bus", "schema: constraints[1]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			saved := open()
			saved.Inputs[tt.input] = tt.value
			for i := range saved.Steps {
				if saved.Steps[i].Address != policyAddress {
					continue
				}
				saved.Steps[i].Inputs[tt.field] = tt.value
				if tt.name == "missing policy input" {
					delete(saved.Inputs, "policy")
					delete(saved.Steps[i].Inputs, tt.field)
				}
			}
			body, err := json.Marshal(saved)
			require.NoError(t, err)
			envelope, err := state.Seal(body, state.PayloadTypePlan, encrypters.Noop{})
			require.NoError(t, err)
			invalid := filepath.Join(build, "invalid.ubp")
			require.NoError(t, os.WriteFile(invalid, envelope, 0o600))
			out, err := run("apply", invalid)
			require.Error(t, err, out)
			assert.Contains(t, out, policyAddress)
			assert.Contains(t, out, tt.message)
			assertPolicyMutations(t, api, nil)
		})
	}
	for _, value := range []string{"null", "'broken'"} {
		invalid := strings.Replace(config, `'{"Statement":[]}'`, value, 1)
		require.NoError(t, os.WriteFile(configPath, []byte(invalid), 0o600))
		out, err := run("plan", "-c", configPath, "-o", filepath.Join(build, "bad.ubp"))
		require.Error(t, err, out)
		if value == "null" {
			assert.Contains(t, out, "factory.ub")
			assert.Contains(t, out, `input "policy": required but is null`)
		} else {
			assert.Contains(t, out, policyAddress)
			assert.Contains(t, out, "schema: constraints[2]")
		}
		assertPolicyMutations(t, api, nil)
	}
}
