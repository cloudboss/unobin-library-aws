package kms

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

func keyCompileOptions(t *testing.T, fixture, out string) compile.Options {
	t.Helper()
	module := libraryModuleRoot(t)
	unobin := unobinModuleRoot(t)
	return compile.Options{
		FactoryPath: filepath.Join("testdata", "ub", "key", "compile", fixture, "factory.ub"),
		OutDir:      out, CLIVersion: "v0.13.0", GoVersion: "1.26.2",
		ReplaceUnobin: unobin.Dir, ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(directory string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(directory), nil
		},
		Stdout: io.Discard, Stderr: io.Discard,
	}
}

func keyLookupLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	libraries := aliasLookupLibraries(t)
	lib := libraries["kms"]
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"data-source.key": lib.Schema.DataSources["key"].Constraints,
	}
	return libraries
}

func keyLookupExecutor(
	t *testing.T, fixture string, api *aliasLookupAPI,
	libraries map[string]*runtime.Library, store *local.Store, lookup any,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "key", "runtime", fixture, "factory.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	body := &f.Factory.Body
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libraries), SyntaxSource: body, Libraries: libraries,
		Inputs: map[string]any{"endpoint": api.endpoint, "lookup": lookup}, Store: store,
	}
}

func saveKeyLookupPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.SealPlan(plan, encrypters.Noop{})
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	return saved
}

func requireKeyReads(t *testing.T, api *aliasLookupAPI, inputs ...map[string]any) {
	t.Helper()
	api.mu.Lock()
	calls := api.calls
	api.calls = nil
	api.mu.Unlock()
	var want []aliasLookupRequest
	for _, input := range inputs {
		want = append(want, aliasLookupRequest{
			method: "POST", path: "/", target: "TrentService.DescribeKey", body: input,
		})
	}
	for i, call := range calls {
		require.Contains(t, call.authorization, "Credential=fake-access-key/")
		require.Contains(t, call.authorization, "/us-gov-west-1/kms/aws4_request")
		require.Equal(t, "attempt=1; max=2", call.retry)
		calls[i].authorization, calls[i].retry = "", ""
	}
	require.Equal(t, want, calls)
}

func keyRuntimeOutputs() map[string]any {
	return map[string]any{
		"key-id":         aliasRuntimeKeyID,
		"arn":            aliasRuntimeARNPrefix + "key/" + aliasRuntimeKeyID,
		"aws-account-id": "", "cloud-hsm-cluster-id": "", "creation-date": "0001-01-01T00:00:00Z",
		"custom-key-store-id": "", "deletion-date": "", "description": "", "expiration-model": "",
		"key-manager": "", "key-spec": "", "key-state": "Enabled", "key-usage": "", "origin": "",
		"valid-to": "", "enabled": false, "multi-region": false,
		"pending-deletion-window-in-days": int64(0),
		"multi-region-configuration":      nil, "xks-key-configuration": nil,
	}
}

func keyRuntimeStackOutputs() map[string]any {
	return map[string]any{"key-id": aliasRuntimeKeyID, "key-spec": ""}
}

func requireKeyConstraint(t *testing.T, err error) {
	t.Helper()
	require.ErrorContains(t, err, "data-source.lookup: schema: constraints[0] (predicate)")
	require.ErrorContains(t, err, "key-id is required")
}

func initialKeyLookup(
	t *testing.T, api *aliasLookupAPI, libraries map[string]*runtime.Library,
) (*runtime.Executor, *local.Store) {
	t.Helper()
	store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
	require.NoError(t, err)
	exec := keyLookupExecutor(t, "resolved", api, libraries, store, aliasRuntimeName)
	plan := saveKeyLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionCreate,
		aliasLookupStep(t, plan, "resource.consumer").Decision)
	got, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, keyRuntimeStackOutputs(), got.Outputs)
	body := map[string]any{"KeyId": aliasRuntimeName}
	requireKeyReads(t, api, body, body)
	snapshot, err := store.Current()
	require.NoError(t, err)
	lookup := snapshot.Find("data-source.lookup")
	require.Equal(t, map[string]any{"key-id": aliasRuntimeName}, lookup.Inputs)
	want := keyRuntimeOutputs()
	want["pending-deletion-window-in-days"] = int64(0)
	require.Equal(t, want, lookup.Outputs)
	consumer := snapshot.Find("resource.consumer")
	require.Equal(t, map[string]any{"key-id": aliasRuntimeKeyID}, consumer.Inputs)
	return exec, store
}

func TestKeyLookupNativePlanAndSavedApply(t *testing.T) {
	api := newAliasLookupAPI(t)
	libraries := keyLookupLibraries(t)
	exec, store := initialKeyLookup(t, api, libraries)
	body := map[string]any{"KeyId": aliasRuntimeName}
	saved := saveKeyLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp,
		aliasLookupStep(t, saved, "resource.consumer").Decision)
	require.Equal(t, keyRuntimeOutputs(),
		aliasLookupStep(t, saved, "data-source.lookup").ObservedOutputs)
	got, err := exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	require.Equal(t, keyRuntimeStackOutputs(), got.Outputs)
	requireKeyReads(t, api, body, body)

	invalid := keyLookupExecutor(t, "resolved", api, libraries, store, nil)
	_, err = invalid.Plan(t.Context())
	requireKeyConstraint(t, err)
	requireKeyReads(t, api)
	saved = saveKeyLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp,
		aliasLookupStep(t, saved, "resource.consumer").Decision)
	requireKeyReads(t, api, body)
	_, err = invalid.ApplyPlan(t.Context(), saved)
	requireKeyConstraint(t, err)
	requireKeyReads(t, api)

	for _, selector := range []string{"malformed", "alias/bad!"} {
		invalid := keyLookupExecutor(t, "resolved", api, libraries, store, selector)
		_, err := invalid.Plan(t.Context())
		require.ErrorContains(t, err, "invalid key selector")
		requireKeyReads(t, api)
	}
	saved = saveKeyLookupPlan(t, exec)
	api.mu.Lock()
	api.keyID = "changed-key-id"
	api.mu.Unlock()
	_, err = exec.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "data source data-source.lookup changed since the plan was computed")
	requireKeyReads(t, api, body, body)
	snapshot, err := store.Current()
	require.NoError(t, err)
	require.Equal(t, map[string]any{"key-id": aliasRuntimeKeyID},
		snapshot.Find("resource.consumer").Inputs)
}

func TestKeyLookupARNSelectorOutputs(t *testing.T) {
	api := newAliasLookupAPI(t)
	libraries := keyLookupLibraries(t)
	store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
	require.NoError(t, err)
	selector := aliasRuntimeARNPrefix + aliasRuntimeName
	exec := keyLookupExecutor(t, "resolved", api, libraries, store, selector)
	saved := saveKeyLookupPlan(t, exec)
	_, err = exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	snapshot, err := store.Current()
	require.NoError(t, err)
	lookup := snapshot.Find("data-source.lookup")
	require.Equal(t, map[string]any{"key-id": selector}, lookup.Inputs)
	require.Equal(t, aliasRuntimeKeyID, lookup.Outputs["key-id"])
	require.Equal(t, map[string]any{"key-id": aliasRuntimeKeyID},
		snapshot.Find("resource.consumer").Inputs)
	body := map[string]any{"KeyId": selector}
	requireKeyReads(t, api, body, body)
}

func TestKeyLookupNativeDeferredSavedApply(t *testing.T) {
	api := newAliasLookupAPI(t)
	libraries := keyLookupLibraries(t)
	for _, tc := range []struct {
		name  string
		input any
	}{
		{"valid", aliasRuntimeName}, {"null", nil}, {"parser", "alias/invalid!"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, store := initialKeyLookup(t, api, libraries)
			exec := keyLookupExecutor(t, "deferred", api, libraries, store, tc.input)
			saved := saveKeyLookupPlan(t, exec)
			ds := aliasLookupStep(t, saved, "data-source.lookup")
			require.Equal(t, runtime.DecisionRead, ds.Decision)
			require.Equal(t, map[string][]string{"key-id": {"action.seed.value"}}, ds.UnresolvedInputs)
			require.Nil(t, ds.ObservedOutputs)
			requireKeyReads(t, api)
			got, err := exec.ApplyPlan(t.Context(), saved)
			switch tc.name {
			case "null":
				requireKeyConstraint(t, err)
				requireKeyReads(t, api)
			case "parser":
				require.ErrorContains(t, err, "invalid key selector")
				requireKeyReads(t, api)
			default:
				require.NoError(t, err)
				require.Equal(t, keyRuntimeStackOutputs(), got.Outputs)
				requireKeyReads(t, api, map[string]any{"KeyId": aliasRuntimeName})
			}
			snapshot, err := store.Current()
			require.NoError(t, err)
			require.Equal(t, map[string]any{"key-id": aliasRuntimeKeyID},
				snapshot.Find("resource.consumer").Inputs)
		})
	}
}

func TestKeyLookupDestroyHistoricalInvalidInputs(t *testing.T) {
	api := newAliasLookupAPI(t)
	libraries := keyLookupLibraries(t)
	_, store := initialKeyLookup(t, api, libraries)
	snapshot, err := store.Current()
	require.NoError(t, err)
	snapshot.Find("data-source.lookup").Inputs = map[string]any{"key-id": nil}
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	exec := keyLookupExecutor(t, "resolved", api, libraries, store, nil)
	exec.Destroy = true
	saved := saveKeyLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionDestroy,
		aliasLookupStep(t, saved, "data-source.lookup").Decision)
	_, err = exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	requireKeyReads(t, api)
	snapshot, err = store.Current()
	require.NoError(t, err)
	require.Empty(t, snapshot.Entries)
}

func TestKeyLookupHTTPGrantTokens(t *testing.T) {
	api := newAliasLookupAPI(t)
	libraries := keyLookupLibraries(t)
	for _, tc := range []struct {
		name   string
		input  any
		tokens any
	}{
		{name: "omitted"}, {name: "null"}, {name: "empty", input: []any{}},
		{name: "mixed", input: []any{"", "b", "a", "b", ""}, tokens: []any{"b", "a", "b"}},
		{name: "all empty", input: []any{"", ""}, tokens: []any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := local.NewStore(t.TempDir(), "tokens", "test", encrypters.Noop{})
			require.NoError(t, err)
			fixture := "tokens"
			if tc.name == "omitted" {
				fixture = "resolved"
			}
			exec := keyLookupExecutor(t, fixture, api, libraries, store, aliasRuntimeName)
			exec.Inputs["tokens"] = tc.input
			saved := saveKeyLookupPlan(t, exec)
			_, err = exec.ApplyPlan(t.Context(), saved)
			require.NoError(t, err)
			body := map[string]any{"KeyId": aliasRuntimeName}
			if tc.tokens != nil {
				body["GrantTokens"] = tc.tokens
			}
			requireKeyReads(t, api, body, body)
		})
	}
}

func TestKeyLookupCompiledSavedPlan(t *testing.T) {
	api := newAliasLookupAPI(t)
	build := t.TempDir()
	opts := keyCompileOptions(t, "evaluated", build)
	opts.StackName, opts.Build = "key-lookup", true
	opts.Version = "v0.0.0"
	result, err := compile.RunResult(opts)
	require.NoError(t, err)
	writeStack := func(fixture string) {
		t.Helper()
		path := filepath.Join("testdata", "ub", "key", "compile", "evaluated", fixture+".ub")
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		body = []byte(strings.ReplaceAll(string(body), "__ENDPOINT__", api.endpoint))
		body = []byte(strings.ReplaceAll(string(body), "__REVISION__", result.ContentRevision))
		require.NoError(t, os.WriteFile(filepath.Join(build, "config.ub"), body, 0o600))
	}
	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
		cmd.Dir = build
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	writeStack("valid")
	out, err := run("plan", "-c", "config.ub", "-o", "saved.ubp")
	require.NoError(t, err, out)
	body, err := os.ReadFile(filepath.Join(build, "saved.ubp"))
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	ds := aliasLookupStep(t, saved, "data-source.lookup")
	require.Equal(t, map[string]any{"key-id": aliasRuntimeName}, ds.Inputs)
	require.Equal(t, keyRuntimeOutputs(), ds.ObservedOutputs)
	request := map[string]any{"KeyId": aliasRuntimeName}
	requireKeyReads(t, api, request)
	out, err = run("apply", "saved.ubp")
	require.NoError(t, err, out)
	requireKeyReads(t, api, request)
	store, err := local.NewStore(filepath.Join(build, ".unobin", "state"),
		"key-lookup", "config", encrypters.Noop{})
	require.NoError(t, err)
	snapshot, err := store.Current()
	require.NoError(t, err)
	require.Equal(t, keyRuntimeStackOutputs(), snapshot.Outputs)
	require.Equal(t, map[string]any{"key-id": aliasRuntimeName},
		snapshot.Find("data-source.lookup").Inputs)
	require.Equal(t, aliasRuntimeKeyID, snapshot.Find("data-source.lookup").Outputs["key-id"])
	writeStack("null")
	out, err = run("plan", "-c", "config.ub", "-o", "invalid.ubp")
	require.Error(t, err)
	require.Contains(t, out, "lookup")
	require.Contains(t, out, "null")
	requireKeyReads(t, api)
}

func TestKeyLookupConfiguredRetryAttempts(t *testing.T) {
	api := newAliasLookupAPI(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		api.mu.Lock()
		api.calls = append(api.calls, aliasLookupRequest{
			method: r.Method, path: r.URL.RequestURI(), target: r.Header.Get("X-Amz-Target"),
			authorization: r.Header.Get("Authorization"), retry: r.Header.Get("Amz-Sdk-Request"),
			body: body,
		})
		api.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"__type":"KMSInternalException","message":"retry"}`)
	}))
	defer server.Close()
	api.endpoint = server.URL
	store, err := local.NewStore(t.TempDir(), "retry", "test", encrypters.Noop{})
	require.NoError(t, err)
	exec := keyLookupExecutor(t, "resolved", api, keyLookupLibraries(t), store, aliasRuntimeName)
	_, err = exec.Plan(t.Context())
	require.ErrorContains(t, err, "KMSInternalException")
	api.mu.Lock()
	defer api.mu.Unlock()
	for i := range api.calls {
		require.Contains(t, api.calls[i].authorization, "/us-gov-west-1/kms/aws4_request")
		api.calls[i].authorization = ""
	}
	require.Equal(t, []aliasLookupRequest{
		{method: "POST", path: "/", target: "TrentService.DescribeKey",
			retry: "attempt=1; max=2", body: map[string]any{"KeyId": aliasRuntimeName}},
		{method: "POST", path: "/", target: "TrentService.DescribeKey",
			retry: "attempt=2; max=2", body: map[string]any{"KeyId": aliasRuntimeName}},
	}, api.calls)
}
