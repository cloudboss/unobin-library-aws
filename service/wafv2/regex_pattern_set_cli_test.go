package wafv2_test

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
	"github.com/stretchr/testify/require"
)

func TestRegexPatternSetCompiledPlanAndScenario(t *testing.T) {
	module, unobin := moduleRoot(t, "."), unobinRoot(t)
	for _, fixture := range []string{"evaluated", "scenario"} {
		t.Run(fixture, func(t *testing.T) {
			path := "testdata/ub/regex-pattern-set/evaluated/factory.ub"
			if fixture == "scenario" {
				path = "../../tests/integration/scenarios/wafv2/factory.ub"
			}
			out := t.TempDir()
			result, err := compile.RunResult(compile.Options{
				FactoryPath: filepath.Dir(path), OutDir: out, Build: true, GoVersion: "1.26.2",
				CLIVersion: "v0.13.0", ReplaceUnobin: unobin.Dir,
				ReplaceGoModules: map[string]string{module.Path: module.Dir},
				NewResolver: func(directory string) (resolve.Resolver, error) {
					return resolve.NewLocalResolver(directory), nil
				}, Stdout: io.Discard, Stderr: io.Discard,
			})
			require.NoError(t, err)
			if fixture == "scenario" {
				return
			}
			cloud := newRegexPatternSetCloud(t)
			endpoint := cloud.endpoint(t, "primary")
			config, err := os.ReadFile("testdata/ub/regex-pattern-set/cli/config.ub")
			require.NoError(t, err)
			config = []byte(strings.ReplaceAll(string(config), "__ENDPOINT__", endpoint))
			configPath := filepath.Join(out, "config.ub")
			require.NoError(t, os.WriteFile(configPath, config, 0o600))
			planPath := filepath.Join(out, "saved.plan")
			command := func(args ...string) {
				t.Helper()
				cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
				cmd.Dir = out
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, string(output))
			}
			for _, decision := range []runtime.Decision{runtime.DecisionCreate, runtime.DecisionNoOp} {
				command("plan", "--config", configPath, "--out", planPath, "--allow-version-mismatch")
				encoded, err := os.ReadFile(planPath)
				require.NoError(t, err)
				saved, err := runtime.OpenPlan(encoded,
					func(*runtime.StateRef) (encrypt.Encrypter, error) { return encrypters.Noop{}, nil })
				require.NoError(t, err)
				step := regexPatternSetStep(t, saved, decision)
				require.Equal(t, "REGIONAL", step.Inputs["scope"])
				require.Equal(t, "example", step.Inputs["name"])
				command("apply", planPath)
			}
			want := []regexPatternSetRequest{{
				"CreateRegexPatternSet", "primary", "us-west-2",
				regexPatternSetCreateBody("example", "REGIONAL", []any{}),
			}}
			read := regexPatternSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1")
			want = append(want, read...)
			want = append(want, read...)
			require.Equal(t, want, cloud.takeCalls())
		})
	}
}
