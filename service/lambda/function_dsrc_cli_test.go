package lambda

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
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

func functionLookupCompileOptions(t *testing.T, factory, out string) compile.Options {
	t.Helper()
	module := libraryModuleRoot(t)
	return compile.Options{
		FactoryPath: factory, OutDir: out, CLIVersion: "v0.13.0", GoVersion: "1.26.2",
		ReplaceUnobin:    unobinModuleRoot(t).Dir,
		ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(directory string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(directory), nil
		}, Stdout: io.Discard, Stderr: io.Discard,
	}
}

func TestFunctionLookupCompiledSavedPlan(t *testing.T) {
	a := newFunctionLookupEndpoint(t)
	build := t.TempDir()
	options := functionLookupCompileOptions(t, "testdata/ub/function-lookup/cli", build)
	options.StackName, options.Build, options.Version = "function-lookup", true, "v0.0.0"
	result, err := compile.RunResult(options)
	require.NoError(t, err)
	writeStack := func(name string) {
		t.Helper()
		body, err := os.ReadFile(filepath.Join("testdata/ub/function-lookup/cli", name+".ub"))
		require.NoError(t, err)
		text := strings.ReplaceAll(string(body), "__ENDPOINT__", a.endpoint)
		text = strings.ReplaceAll(text, "__REVISION__", result.ContentRevision)
		require.NoError(t, os.WriteFile(filepath.Join(build, "config.ub"), []byte(text), 0o600))
	}
	run := func(args ...string) (string, error) {
		t.Helper()
		command := exec.CommandContext(t.Context(), result.BinaryPath, args...)
		command.Dir = build
		output, err := command.CombinedOutput()
		return string(output), err
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
	selector := "arn:aws:lambda:ap-south-1:999:function:input"
	writeStack("valid")
	for _, image := range []bool{false, true} {
		a.mu.Lock()
		a.image = image
		a.mu.Unlock()
		output, err := run("plan", "-c", "config.ub", "-o", "saved.ubp")
		require.NoError(t, err, output)
		ds := functionLookupStep(t, openPlan(), "data-source.lookup")
		require.Equal(t, map[string]any{"function-name": selector, "qualifier": nil}, ds.Inputs)
		want := functionLookupExpected()
		if image {
			want["code-signing-config-arn"] = ""
		}
		require.Equal(t, want, ds.ObservedOutputs)
		require.Empty(t, ds.UnresolvedInputs)
		requireFunctionReads(t, a, selector, 1)
		output, err = run("apply", "saved.ubp")
		require.NoError(t, err, output)
		requireFunctionReads(t, a, selector, 1)
		store, err := local.NewStore(filepath.Join(build, ".unobin/state"),
			"function-lookup", "config", encrypters.Noop{})
		require.NoError(t, err)
		snapshot, err := store.Current()
		require.NoError(t, err)
		require.Equal(t, want, snapshot.Find("data-source.lookup").Outputs)
		require.Equal(t, ds.Inputs, snapshot.Find("data-source.lookup").Inputs)
		require.Equal(t, map[string]any{
			"description": "initial", "arn": want["arn"], "qualified-arn": want["qualified-arn"],
		}, snapshot.Outputs)
	}
	for _, invalid := range []string{"null", "missing"} {
		writeStack(invalid)
		output, err := run("plan", "-c", "config.ub", "-o", "invalid.ubp")
		require.Error(t, err)
		require.Contains(t, output, "name")
		if invalid == "null" {
			require.Contains(t, output, "null")
		}
		requireFunctionReads(t, a, "", 0)
	}
	writeStack("valid")
	output, err := run("plan", "-c", "config.ub", "-o", "saved.ubp")
	require.NoError(t, err, output)
	requireFunctionReads(t, a, selector, 1)
	a.mu.Lock()
	a.description = "remote-drift"
	a.mu.Unlock()
	output, err = run("apply", "saved.ubp")
	require.Error(t, err)
	require.Contains(t, output, "data source data-source.lookup changed since the plan was computed")
	requireFunctionReads(t, a, selector, 1)
}

func TestFunctionLookupHTTPAttempts(t *testing.T) {
	a := newFunctionLookupEndpoint(t)
	a.fail = true
	e := functionLookupExecutor(t, "runtime", a, nil, functionLookupLibraries(t, a))
	_, err := e.Plan(t.Context())
	require.ErrorContains(t, err, "ServiceException")
	require.Equal(t, []functionLookupRequest{
		{"GET", "/2015-03-31/functions/selector/versions?MaxItems=10000", "us-gov-west-1",
			"attempt=1; max=1"},
	}, a.takeCalls())
	require.Empty(t, a.mutations)
}

func TestFunctionLookupIntegrationFactory(t *testing.T) {
	module := libraryModuleRoot(t)
	source := filepath.Join(module.Dir, "tests/integration/scenarios/lambda-function-lookup")
	factory := t.TempDir()
	body, err := os.ReadFile(filepath.Join(source, "factory.ub"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(factory, "factory.ub"), body, 0o600))
	prepare := exec.CommandContext(t.Context(), "go", "run", "./prepare")
	prepare.Dir = source
	prepare.Env = append(os.Environ(), "SCENARIO_DIR="+factory)
	output, err := prepare.CombinedOutput()
	require.NoError(t, err, string(output))
	options := functionLookupCompileOptions(t, factory, t.TempDir())
	options.StackName = "lambda-function-lookup"
	_, err = compile.RunResult(options)
	require.NoError(t, err)
}
