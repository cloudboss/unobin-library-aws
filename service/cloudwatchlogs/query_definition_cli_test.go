package cloudwatchlogs

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

func TestQueryDefinitionCompiledSavedPlan(t *testing.T) {
	api := newQueryAPI(t)
	build := t.TempDir()
	module, unobin := libraryModuleRoot(t), unobinModuleRoot(t)
	fixtures := filepath.Join("testdata", "ub", "query-definition", "compile", "evaluated")
	result, err := compile.RunResult(compile.Options{
		FactoryPath: filepath.Join(fixtures, "factory.ub"), OutDir: build,
		CLIVersion: "v0.13.0", GoVersion: "1.26.2", StackName: "query-definition",
		Build: true, Version: "v0.0.0", ReplaceUnobin: unobin.Dir,
		ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(directory string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(directory), nil
		},
		Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	writeConfig := func(name string) {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(fixtures, name+".ub"))
		require.NoError(t, err)
		text := strings.ReplaceAll(string(body), "__ENDPOINT__", api.endpoint)
		text = strings.ReplaceAll(text, "__REVISION__", result.ContentRevision)
		require.NoError(t, os.WriteFile(filepath.Join(build, "config.ub"), []byte(text), 0o600))
	}
	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
		cmd.Dir = build
		body, err := cmd.CombinedOutput()
		return string(body), err
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
	writeConfig("valid")
	out, err := run("plan", "-c", "config.ub", "-o", "saved.ubp")
	require.NoError(t, err, out)
	require.Equal(t, runtime.DecisionCreate, queryStep(t, openPlan()).Decision)
	out, err = run("apply", "saved.ubp")
	require.NoError(t, err, out)
	require.Equal(t, []queryCall{{"PutQueryDefinition", map[string]any{
		"name": "query/path", "queryString": ""}}}, queryMutations(api.takeCalls()))
	out, err = run("plan", "-c", "config.ub", "-o", "saved.ubp")
	require.NoError(t, err, out)
	require.Equal(t, runtime.DecisionNoOp, queryStep(t, openPlan()).Decision)
	api.takeCalls()
	out, err = run("apply", "saved.ubp")
	require.NoError(t, err, out)
	require.Empty(t, api.takeCalls())
	for _, name := range []string{"null-name", "null-query"} {
		writeConfig(name)
		out, err = run("plan", "-c", "config.ub", "-o", "bad.ubp")
		require.Error(t, err)
		field := strings.TrimPrefix(name, "null-")
		if field == "query" {
			field = "query-string"
		}
		require.Contains(t, out, filepath.Join(fixtures, "factory.ub"))
		require.Contains(t, out, `type: input "query": field "`+field+`": required but is null`)
		require.Empty(t, queryMutations(api.takeCalls()))
	}
}
