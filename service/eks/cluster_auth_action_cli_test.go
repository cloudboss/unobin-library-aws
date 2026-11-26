package eks_test

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterAuthCompiledCLI(t *testing.T) {
	clusterAuthEnvironment(t)
	module, dep := moduleRoot(t, "."), unobinRoot(t)
	out := t.TempDir()
	result, err := compile.RunResult(compile.Options{
		FactoryPath: "testdata/ub/cluster-auth/evaluated", OutDir: out,
		Build: true, GoVersion: "1.26.2", CLIVersion: "v0.13.0",
		ReplaceUnobin: dep.Dir, ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(dir string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(dir), nil
		}, Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	source, err := os.ReadFile("testdata/ub/cluster-auth/evaluated/config.ub")
	require.NoError(t, err)
	config := filepath.Join(out, "config.ub")
	require.NoError(t, os.WriteFile(config, source, 0o600))
	planPath := filepath.Join(out, "saved.plan")
	command := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
		cmd.Dir = out
		return cmd.CombinedOutput()
	}
	enc, err := encrypters.NewEnvKey("UNOBIN_EKS_AUTH_KEY")
	require.NoError(t, err)
	plan := func() *runtime.PlanFile {
		output, err := command("plan", "-c", config, "-o", planPath, "--allow-version-mismatch")
		require.NoError(t, err, string(output))
		encoded, err := os.ReadFile(planPath)
		require.NoError(t, err)
		saved, err := runtime.OpenPlan(encoded, func(ref *runtime.StateRef) (
			encrypt.Encrypter, error,
		) {
			require.Equal(t, "env-key", ref.Name)
			return enc, nil
		})
		require.NoError(t, err)
		return saved
	}
	first := plan()
	assert.Equal(t, runtime.DecisionRerun, authStep(t, first).Decision)
	assert.Equal(t, []string{"token"}, authStep(t, first).SensitiveOutputs)
	store, err := local.NewStore(filepath.Join(out, ".unobin", "state"),
		result.FactoryName, "config", enc)
	require.NoError(t, err)
	_, err = store.Current()
	require.ErrorIs(t, err, state.ErrNoCurrent)
	output, err := command("apply", planPath)
	require.NoError(t, err, string(output))
	snapshot, err := store.Current()
	require.NoError(t, err)
	token := snapshot.Find(authAddress).Outputs["token"].(string)
	assert.Contains(t, string(output), "token: <sensitive>")
	assert.NotContains(t, string(output), token)
	output, err = command("output", "-c", config)
	require.NoError(t, err, string(output))
	assert.Contains(t, string(output), "token: <sensitive>")
	assert.NotContains(t, string(output), token)
	skip := plan()
	assert.Equal(t, runtime.DecisionSkip, authStep(t, skip).Decision)
	output, err = command("apply", planPath)
	require.NoError(t, err, string(output))
	snapshot, err = store.Current()
	require.NoError(t, err)
	assert.Equal(t, token, snapshot.Find(authAddress).Outputs["token"])
	saved := plan()
	saved.Inputs["name"] = nil
	changed, err := json.Marshal(saved)
	require.NoError(t, err)
	encoded, err := state.Seal(changed, state.PayloadTypePlan, enc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(planPath, encoded, 0o600))
	output, err = command("apply", planPath)
	require.Error(t, err)
	assert.Contains(t, string(output), "name")
	assert.Contains(t, string(output), "required but is null")
	assert.NotContains(t, string(output), token)
}
