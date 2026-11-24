package sqs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

var queueLookupMutations atomic.Int64

type queueLookupConsumer struct {
	URI string `ub:"uri"`
}

func (r *queueLookupConsumer) Create(context.Context, any) (*queueLookupConsumer, error) {
	queueLookupMutations.Add(1)
	return r, nil
}

func (*queueLookupConsumer) Read(
	_ context.Context, _ any, prior runtime.Prior[queueLookupConsumer, *queueLookupConsumer, any],
) (*queueLookupConsumer, error) {
	return prior.Outputs, nil
}

func (r *queueLookupConsumer) Update(
	context.Context, any, runtime.Prior[queueLookupConsumer, *queueLookupConsumer, any],
) (*queueLookupConsumer, error) {
	queueLookupMutations.Add(1)
	return r, nil
}

func (*queueLookupConsumer) Delete(
	context.Context, any, runtime.Prior[queueLookupConsumer, *queueLookupConsumer, any],
) error {
	return nil
}

type queueLookupSeed struct{ Name *string }
type queueLookupSeedOutput struct{ Value *string }

func (r *queueLookupSeed) Run(context.Context, any) (*queueLookupSeedOutput, error) {
	return &queueLookupSeedOutput{Value: r.Name}, nil
}

type queueLookupRequest struct {
	operation string
	body      map[string]any
}

type queueLookupEndpoint struct {
	url         string
	mu          sync.Mutex
	calls       []queueLookupRequest
	arn         string
	createdURL  string
	managedTags map[string]string
}

func queueLookupTestEndpoint(t *testing.T) *queueLookupEndpoint {
	t.Helper()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "fake-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	t.Setenv("AWS_REGION", "us-east-1")
	api := &queueLookupEndpoint{arn: "arbitrary ARN unchanged", createdURL: "https://queue.test/seed"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/", r.URL.RequestURI())
		require.Contains(t, r.Header.Get("Authorization"), "Credential=fake-access-key/")
		require.Contains(t, r.Header.Get("Authorization"), "/us-gov-west-1/sqs/aws4_request")
		operation := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "AmazonSQS.")
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		api.mu.Lock()
		defer api.mu.Unlock()
		api.calls = append(api.calls, queueLookupRequest{operation, body})
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		var response any
		switch operation {
		case "CreateQueue":
			if tags, ok := body["tags"].(map[string]any); ok {
				api.managedTags = map[string]string{}
				for key, value := range tags {
					api.managedTags[key] = value.(string)
				}
			}
			response = map[string]any{"QueueUrl": api.createdURL}
		case "GetQueueUrl":
			response = map[string]any{"QueueUrl": "https://queue.test/kept"}
		case "GetQueueAttributes":
			response = map[string]any{"Attributes": map[string]string{
				"QueueArn": api.arn, "VisibilityTimeout": "30"}}
		case "TagQueue":
			for key, value := range body["Tags"].(map[string]any) {
				api.managedTags[key] = value.(string)
			}
			response = map[string]any{}
		case "ListQueueTags":
			if api.managedTags != nil {
				response = map[string]any{"Tags": api.managedTags}
				break
			}
			response = map[string]any{"Tags": map[string]string{
				"ordinary": "value", "empty": "", "AWS:kept": "upper", "aws:hidden": "reserved"}}
		default:
			t.Errorf("unexpected operation %q", operation)
		}

		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	t.Cleanup(server.Close)
	api.url = server.URL
	return api
}

func (a *queueLookupEndpoint) requireReads(t *testing.T, names ...string) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	var want []queueLookupRequest
	for _, name := range names {
		want = append(want,
			queueLookupRequest{"GetQueueUrl", map[string]any{"QueueName": name}},
			queueLookupRequest{"GetQueueAttributes", map[string]any{
				"QueueUrl": "https://queue.test/kept", "AttributeNames": []any{"QueueArn"}}},
			queueLookupRequest{"ListQueueTags", map[string]any{"QueueUrl": "https://queue.test/kept"}},
		)
	}
	require.Equal(t, want, a.calls)
	a.calls = nil
}

func queueLookupRuntimeLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.Schema = readLibrarySchema(t)
	for _, schemas := range []map[string]*runtime.TypeSchema{
		lib.Schema.Resources, lib.Schema.DataSources, lib.Schema.Actions,
	} {
		for _, schema := range schemas {
			schema.EvaluatedInputs = schema.Inputs
		}
	}
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"data-source.queue": lib.Schema.DataSources["queue"].Constraints,
	}
	return map[string]*runtime.Library{
		"sqs": lib,
		"test": {
			Resources: map[string]runtime.ResourceRegistration{
				"value": runtime.MakeResource(runtime.ResourceDefinition[
					queueLookupConsumer, *queueLookupConsumer, any,
				]{SchemaVersion: 1}),
			},
			Actions: map[string]runtime.ActionRegistration{
				"seed": runtime.MakeAction[queueLookupSeed, *queueLookupSeedOutput, any](),
			},
		},
	}
}

func queueLookupExecutor(
	t *testing.T, fixture string, api *queueLookupEndpoint, libs map[string]*runtime.Library,
	store *local.Store, input any,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "queue-lookup", "runtime", fixture, "factory.ub")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, data)
	require.NoError(t, err)
	body := file.Factory.Body
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(body, libs), SyntaxSource: &body, Libraries: libs,
		Inputs: map[string]any{"endpoint": api.url, "lookup": input}, Store: store,
	}
}

func queueLookupSavedPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
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

func queueLookupStep(t *testing.T, plan *runtime.PlanFile, address string) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == address {
			return step
		}
	}
	t.Fatalf("missing plan step %s", address)
	return runtime.PlanStep{}
}

func requireQueueLookupOutputs(t *testing.T, got map[string]any) {
	t.Helper()
	require.Equal(t, map[string]any{
		"queue-url": "https://queue.test/kept", "queue-arn": "arbitrary ARN unchanged",
		"tags": map[string]any{"ordinary": "value", "empty": "", "AWS:kept": "upper"},
	}, got)
}

func requireQueueLookupNameRule(t *testing.T, err error) {
	t.Helper()
	require.ErrorContains(t, err, `data-source.lookup: field "queue-name": required but is null`)
}

func TestQueueLookupNativeLifecycle(t *testing.T) {
	api := queueLookupTestEndpoint(t)
	libs := queueLookupRuntimeLibraries(t)
	store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
	require.NoError(t, err)
	exec := queueLookupExecutor(t, "resolved", api, libs, store, "selector")
	initial := queueLookupSavedPlan(t, exec)
	require.Equal(t, runtime.DecisionCreate,
		queueLookupStep(t, initial, "resource.consumer").Decision)
	applied, err := exec.ApplyPlan(t.Context(), initial)
	require.NoError(t, err)
	requireQueueLookupOutputs(t, applied.Outputs)
	api.requireReads(t, "selector", "selector")

	unchanged := queueLookupSavedPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp,
		queueLookupStep(t, unchanged, "resource.consumer").Decision)
	requireQueueLookupOutputs(t, queueLookupStep(t, unchanged, "data-source.lookup").ObservedOutputs)
	applied, err = exec.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	requireQueueLookupOutputs(t, applied.Outputs)
	api.requireReads(t, "selector", "selector")

	invalid := queueLookupExecutor(t, "resolved", api, libs, store, nil)
	_, err = invalid.Plan(t.Context())
	requireQueueLookupNameRule(t, err)
	api.requireReads(t)
	before := queueLookupMutations.Load()
	saved := queueLookupSavedPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp,
		queueLookupStep(t, saved, "resource.consumer").Decision)
	api.requireReads(t, "selector")
	_, err = invalid.ApplyPlan(t.Context(), saved)
	requireQueueLookupNameRule(t, err)
	require.Equal(t, before, queueLookupMutations.Load())
	api.requireReads(t)

	drift := queueLookupSavedPlan(t, exec)
	api.mu.Lock()
	api.arn = "registry/changed"
	api.mu.Unlock()
	_, err = exec.ApplyPlan(t.Context(), drift)
	require.ErrorContains(t, err, "data source data-source.lookup changed since the plan was computed")
	api.requireReads(t, "selector", "selector")

	snapshot, err := store.Current()
	require.NoError(t, err)
	snapshot.Find("data-source.lookup").Inputs["queue-name"] = nil
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	invalid.Destroy = true
	_, err = invalid.ApplyPlan(t.Context(), queueLookupSavedPlan(t, invalid))
	require.NoError(t, err)
	api.requireReads(t)
}

func TestQueueLookupNativeDeferred(t *testing.T) {
	api := queueLookupTestEndpoint(t)
	libs := queueLookupRuntimeLibraries(t)
	for _, input := range []any{"", "selector", nil} {
		store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
		require.NoError(t, err)
		exec := queueLookupExecutor(t, "deferred", api, libs, store, input)
		saved := queueLookupSavedPlan(t, exec)
		ds := queueLookupStep(t, saved, "data-source.lookup")
		require.Equal(t, map[string][]string{"queue-name": {"action.seed.value"}}, ds.UnresolvedInputs)
		require.Nil(t, ds.ObservedOutputs)
		api.requireReads(t)
		before := queueLookupMutations.Load()
		applied, err := exec.ApplyPlan(t.Context(), saved)
		if input == nil {
			require.Equal(t, before, queueLookupMutations.Load())
			requireQueueLookupNameRule(t, err)
			api.requireReads(t)
			continue
		}
		require.NoError(t, err)
		requireQueueLookupOutputs(t, applied.Outputs)
		api.requireReads(t, input.(string))
	}
}

func queueLookupCompile(t *testing.T, fixture string) (string, string, string) {
	t.Helper()
	module, core := libraryModuleRoot(t), unobinModuleRoot(t)
	build := t.TempDir()
	result, err := compile.RunResult(compile.Options{
		FactoryPath: filepath.Join("testdata", "ub", "queue-lookup", "cli", fixture),
		OutDir:      build, StackName: "queue-lookup", Version: "v0.0.0", Build: true,
		CLIVersion: "v0.13.0", GoVersion: compile.GoMajorMinor(),
		ReplaceUnobin: core.Dir, ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(dir string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(dir), nil
		},
		Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	return build, result.BinaryPath, result.ContentRevision
}

func queueLookupWriteStack(t *testing.T, dir, fixture, input, endpoint, revision string) {
	t.Helper()
	path := filepath.Join("testdata", "ub", "queue-lookup", "cli", fixture, input+".ub")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	source := strings.ReplaceAll(string(body), "__ENDPOINT__", endpoint)
	source = strings.ReplaceAll(source, "__REVISION__", revision)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.ub"), []byte(source), 0o600))
}

func queueLookupCLI(t *testing.T, dir, binary string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binary, args...)
	cmd.Dir = dir
	body, err := cmd.CombinedOutput()
	return string(body), err
}

func queueLookupOpenCLIPlan(t *testing.T, dir, name string) *runtime.PlanFile {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	plan, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	return plan
}

func queueLookupWriteCLIPlan(t *testing.T, dir, name string, plan *runtime.PlanFile) {
	t.Helper()
	raw, err := json.Marshal(plan)
	require.NoError(t, err)
	body, err := sdkstate.Seal(raw, sdkstate.PayloadTypePlan, encrypters.Noop{})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), body, 0o600))
}

func TestQueueLookupCompiledCLIResolved(t *testing.T) {
	api := queueLookupTestEndpoint(t)
	dir, binary, revision := queueLookupCompile(t, "resolved")
	queueLookupWriteStack(t, dir, "resolved", "valid", api.url, revision)
	out, err := queueLookupCLI(t, dir, binary, "plan", "-c", "config.ub", "-o", "initial.ubp")
	require.NoError(t, err, out)
	initial := queueLookupOpenCLIPlan(t, dir, "initial.ubp")
	ds := queueLookupStep(t, initial, "data-source.lookup")
	require.Equal(t, map[string]any{"queue-name": "selector"}, ds.Inputs)
	requireQueueLookupOutputs(t, ds.ObservedOutputs)
	out, err = queueLookupCLI(t, dir, binary, "apply", "initial.ubp")
	require.NoError(t, err, out)
	api.requireReads(t, "selector", "selector")
	store, err := local.NewStore(filepath.Join(dir, ".unobin", "state"),
		"queue-lookup", "config", encrypters.Noop{})
	require.NoError(t, err)
	snapshot, err := store.Current()
	require.NoError(t, err)
	requireQueueLookupOutputs(t, snapshot.Outputs)
	require.Equal(t, map[string]any{"queue-name": "selector"},
		snapshot.Find("data-source.lookup").Inputs)

	out, err = queueLookupCLI(t, dir, binary, "plan", "-c", "config.ub", "-o", "unchanged.ubp")
	require.NoError(t, err, out)
	saved := queueLookupOpenCLIPlan(t, dir, "unchanged.ubp")
	requireQueueLookupOutputs(t, queueLookupStep(t, saved, "data-source.lookup").ObservedOutputs)
	api.requireReads(t, "selector")
	saved.Inputs["lookup"] = nil
	queueLookupWriteCLIPlan(t, dir, "invalid.ubp", saved)
	out, err = queueLookupCLI(t, dir, binary, "apply", "invalid.ubp")
	require.Error(t, err)
	require.Contains(t, out, "data-source.lookup")
	require.Contains(t, out, `field "queue-name": required but is null`)
	api.requireReads(t)
	out, err = queueLookupCLI(t, dir, binary, "apply", "unchanged.ubp")
	require.NoError(t, err, out)
	api.requireReads(t, "selector")

	queueLookupWriteStack(t, dir, "resolved", "null", api.url, revision)
	out, err = queueLookupCLI(t, dir, binary, "plan", "-c", "config.ub", "-o", "null.ubp")
	require.Error(t, err)
	require.Contains(t, out, `input "lookup": required but is null`)
	api.requireReads(t)
}

func TestQueueLookupCompiledCLIDeferred(t *testing.T) {
	api := queueLookupTestEndpoint(t)
	dir, binary, revision := queueLookupCompile(t, "deferred")
	queueLookupWriteStack(t, dir, "deferred", "valid", api.url, revision)
	out, err := queueLookupCLI(t, dir, binary, "plan", "-c", "config.ub", "-o", "deferred.ubp")
	require.NoError(t, err, out)
	saved := queueLookupOpenCLIPlan(t, dir, "deferred.ubp")
	require.Nil(t, queueLookupStep(t, saved, "data-source.lookup").ObservedOutputs)
	require.Equal(t, runtime.DecisionCreate, queueLookupStep(t, saved, "resource.seed").Decision)
	api.requireReads(t)
	saved.Inputs["lookup"] = nil
	queueLookupWriteCLIPlan(t, dir, "invalid.ubp", saved)
	out, err = queueLookupCLI(t, dir, binary, "apply", "invalid.ubp")
	require.Error(t, err)
	require.Contains(t, out, "data-source.lookup")
	require.Contains(t, out, `field "queue-name": required but is null`)
	api.mu.Lock()
	require.Equal(t, []queueLookupRequest{
		{"CreateQueue", map[string]any{"QueueName": "seed", "Attributes": map[string]any{}}},
		{"GetQueueAttributes", map[string]any{
			"QueueUrl": "https://queue.test/seed", "AttributeNames": []any{"All"}}},
	}, api.calls)
	api.calls = nil
	api.mu.Unlock()
	// A fresh state directory lets the valid saved plan create its prerequisite.
	saved.StateRev = ""
	saved.Inputs["lookup"] = "selector"
	saved.Backend.Body["path"] = ".unobin/valid-state"
	queueLookupWriteCLIPlan(t, dir, "valid-deferred.ubp", saved)
	out, err = queueLookupCLI(t, dir, binary, "apply", "valid-deferred.ubp")
	require.NoError(t, err, out)
	api.mu.Lock()
	require.Equal(t, []queueLookupRequest{
		{"CreateQueue", map[string]any{"QueueName": "seed", "Attributes": map[string]any{}}},
		{"GetQueueAttributes", map[string]any{
			"QueueUrl": "https://queue.test/seed", "AttributeNames": []any{"All"}}},
		{"GetQueueUrl", map[string]any{"QueueName": "selector"}},
		{"GetQueueAttributes", map[string]any{
			"QueueUrl": "https://queue.test/kept", "AttributeNames": []any{"QueueArn"}}},
		{"ListQueueTags", map[string]any{"QueueUrl": "https://queue.test/kept"}},
	}, api.calls)
	api.calls = nil
	api.mu.Unlock()
}

func TestQueueLookupNativeDependencyTagUpdate(t *testing.T) {
	api := queueLookupTestEndpoint(t)
	api.createdURL = "https://queue.test/kept"
	libraries := queueLookupRuntimeLibraries(t)
	store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
	require.NoError(t, err)
	executor := queueLookupExecutor(t, "ordered", api, libraries, store, "seed")
	executor.Inputs["phase"] = "initial"
	initial := queueLookupSavedPlan(t, executor)
	require.Nil(t, queueLookupStep(t, initial, "data-source.lookup").ObservedOutputs)
	applied, err := executor.ApplyPlan(t.Context(), initial)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"phase": "initial"}, applied.Outputs)
	api.mu.Lock()
	require.Equal(t, []queueLookupRequest{
		{"CreateQueue", map[string]any{"QueueName": "seed", "Attributes": map[string]any{},
			"tags": map[string]any{"phase": "initial"}}},
		{"GetQueueAttributes", map[string]any{
			"QueueUrl": "https://queue.test/kept", "AttributeNames": []any{"All"}}},
		{"GetQueueUrl", map[string]any{"QueueName": "seed"}},
		{"GetQueueAttributes", map[string]any{
			"QueueUrl": "https://queue.test/kept", "AttributeNames": []any{"QueueArn"}}},
		{"ListQueueTags", map[string]any{"QueueUrl": "https://queue.test/kept"}},
		{"GetQueueAttributes", map[string]any{
			"QueueUrl": "https://queue.test/kept", "AttributeNames": []any{"All"}}},
	}, api.calls)
	api.calls = nil
	api.mu.Unlock()

	executor.Inputs["phase"] = "updated"
	update := queueLookupSavedPlan(t, executor)
	require.Equal(t, runtime.DecisionUpdate, queueLookupStep(t, update, "resource.seed").Decision)
	ds := queueLookupStep(t, update, "data-source.lookup")
	require.Equal(t, map[string]any{"queue-name": "seed"}, ds.Inputs)
	require.Nil(t, ds.ObservedOutputs)
	api.mu.Lock()
	require.Equal(t, []queueLookupRequest{{"GetQueueAttributes", map[string]any{
		"QueueUrl": "https://queue.test/kept", "AttributeNames": []any{"All"}}}}, api.calls)
	api.calls = nil
	api.mu.Unlock()
	applied, err = executor.ApplyPlan(t.Context(), update)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"phase": "updated"}, applied.Outputs)
	snapshot, err := store.Current()
	require.NoError(t, err)
	require.Equal(t, map[string]any{"uri": "updated"}, snapshot.Find("resource.consumer").Outputs)
	require.Equal(t, map[string]any{"phase": "updated"},
		snapshot.Find("data-source.lookup").Outputs["tags"])
	api.mu.Lock()
	defer api.mu.Unlock()
	require.Equal(t, []queueLookupRequest{
		{"ListQueueTags", map[string]any{"QueueUrl": "https://queue.test/kept"}},
		{"TagQueue", map[string]any{"QueueUrl": "https://queue.test/kept",
			"Tags": map[string]any{"phase": "updated"}}},
		{"GetQueueAttributes", map[string]any{
			"QueueUrl": "https://queue.test/kept", "AttributeNames": []any{"All"}}},
		{"GetQueueUrl", map[string]any{"QueueName": "seed"}},
		{"GetQueueAttributes", map[string]any{
			"QueueUrl": "https://queue.test/kept", "AttributeNames": []any{"QueueArn"}}},
		{"ListQueueTags", map[string]any{"QueueUrl": "https://queue.test/kept"}},
		{"GetQueueAttributes", map[string]any{
			"QueueUrl": "https://queue.test/kept", "AttributeNames": []any{"All"}}},
	}, api.calls)
	api.calls = nil
}
