package ecr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

type repositoryConsumer struct {
	URI string `ub:"uri"`
}

func (r *repositoryConsumer) Create(context.Context, any) (*repositoryConsumer, error) {
	return r, nil
}

func (*repositoryConsumer) Read(
	_ context.Context, _ any, prior runtime.Prior[repositoryConsumer, *repositoryConsumer, any],
) (*repositoryConsumer, error) {
	return prior.Outputs, nil
}

func (r *repositoryConsumer) Update(
	context.Context, any, runtime.Prior[repositoryConsumer, *repositoryConsumer, any],
) (*repositoryConsumer, error) {
	return r, nil
}

func (*repositoryConsumer) Delete(
	context.Context, any, runtime.Prior[repositoryConsumer, *repositoryConsumer, any],
) error {
	return nil
}

type repositorySeed struct{ Name *string }
type repositorySeedOutput struct{ Value *string }

func (r *repositorySeed) Run(context.Context, any) (*repositorySeedOutput, error) {
	return &repositorySeedOutput{Value: r.Name}, nil
}

type repositoryRequest struct {
	operation string
	body      map[string]any
}

type repositoryEndpoint struct {
	url   string
	mu    sync.Mutex
	calls []repositoryRequest
	uri   string
}

func repositoryTestEndpoint(t *testing.T) *repositoryEndpoint {
	t.Helper()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "fake-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	t.Setenv("AWS_REGION", "us-east-1")
	api := &repositoryEndpoint{uri: "registry/returned"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/", r.URL.RequestURI())
		require.Contains(t, r.Header.Get("Authorization"), "Credential=fake-access-key/")
		require.Contains(t, r.Header.Get("Authorization"), "/us-gov-west-1/ecr/aws4_request")
		operation := strings.TrimPrefix(r.Header.Get("X-Amz-Target"),
			"AmazonEC2ContainerRegistry_V20150921.")
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		api.mu.Lock()
		defer api.mu.Unlock()
		api.calls = append(api.calls, repositoryRequest{operation, body})
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		var response any
		switch operation {
		case "DescribeRepositories":
			response = map[string]any{"repositories": []any{map[string]any{
				"repositoryName": "returned", "registryId": "remote-registry",
				"repositoryArn": "arn", "repositoryUri": api.uri,
				"imageTagMutability":         "FUTURE",
				"imageScanningConfiguration": map[string]any{"scanOnPush": false},
				"encryptionConfiguration":    map[string]any{"encryptionType": "AES256"},
			}}}
		case "DescribeImages":
			response = map[string]any{"imageDetails": []any{map[string]any{
				"imagePushedAt": 100, "imageTags": []string{"z", "a"},
			}}}
		case "ListTagsForResource":
			response = map[string]any{"tags": []any{
				map[string]any{"Key": "ordinary", "Value": "value"},
				map[string]any{"Key": "empty", "Value": ""},
				map[string]any{"Key": "aws:hidden", "Value": "reserved"},
			}}
		default:
			t.Errorf("unexpected operation %q", operation)
		}
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	t.Cleanup(server.Close)
	api.url = server.URL
	return api
}

func (a *repositoryEndpoint) requireReads(t *testing.T, names ...string) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	var want []repositoryRequest
	for _, name := range names {
		want = append(want,
			repositoryRequest{"DescribeRepositories", map[string]any{
				"repositoryNames": []any{name},
			}},
			repositoryRequest{"DescribeImages", map[string]any{
				"repositoryName": "returned", "registryId": "remote-registry",
			}},
			repositoryRequest{"ListTagsForResource", map[string]any{"resourceArn": "arn"}},
		)
	}
	require.Equal(t, want, a.calls)
	a.calls = nil
}

func repositoryRuntimeLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.Schema = readLibrarySchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"data-source.repository": lib.Schema.DataSources["repository"].Constraints,
	}
	return map[string]*runtime.Library{
		"ecr": lib,
		"test": {
			Resources: map[string]runtime.ResourceRegistration{
				"value": runtime.MakeResource(runtime.ResourceDefinition[
					repositoryConsumer, *repositoryConsumer, any,
				]{SchemaVersion: 1}),
			},
			Actions: map[string]runtime.ActionRegistration{
				"seed": runtime.MakeAction[repositorySeed, *repositorySeedOutput, any](),
			},
		},
	}
}

func repositoryExecutor(
	t *testing.T, fixture string, api *repositoryEndpoint, libs map[string]*runtime.Library,
	store *local.Store, input any,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "repository-lookup", "runtime", fixture, "factory.ub")
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

func repositorySavedPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func repositoryStep(t *testing.T, plan *runtime.PlanFile, address string) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == address {
			return step
		}
	}
	t.Fatalf("missing plan step %s", address)
	return runtime.PlanStep{}
}

func requireRepositoryOutputs(t *testing.T, got map[string]any) {
	t.Helper()
	require.Equal(t, map[string]any{
		"name": "returned", "arn": "arn", "registry-id": "remote-registry",
		"repository-uri": "registry/returned", "image-tag-mutability": "FUTURE",
		"encryption-configuration": map[string]any{
			"encryption-type": "AES256", "kms-key": "",
		},
		"image-scanning-configuration":           map[string]any{"scan-on-push": false},
		"image-tag-mutability-exclusion-filters": []any{},
		"most-recent-image-tags":                 []any{"z", "a"},
		"tags":                                   map[string]any{"ordinary": "value", "empty": ""},
	}, got)
}

func requireRepositoryNameRule(t *testing.T, err error) {
	t.Helper()
	require.ErrorContains(t, err, "data-source.lookup: schema: constraints[0] (predicate)")
	require.ErrorContains(t, err, "name is required")
}

func TestRepositoryLookupNativeLifecycle(t *testing.T) {
	api := repositoryTestEndpoint(t)
	libs := repositoryRuntimeLibraries(t)
	store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
	require.NoError(t, err)
	exec := repositoryExecutor(t, "resolved", api, libs, store, "selector")
	initial := repositorySavedPlan(t, exec)
	require.Equal(t, runtime.DecisionCreate,
		repositoryStep(t, initial, "resource.consumer").Decision)
	applied, err := exec.ApplyPlan(t.Context(), initial)
	require.NoError(t, err)
	requireRepositoryOutputs(t, applied.Outputs)
	api.requireReads(t, "selector", "selector")

	unchanged := repositorySavedPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp,
		repositoryStep(t, unchanged, "resource.consumer").Decision)
	requireRepositoryOutputs(t, repositoryStep(t, unchanged, "data-source.lookup").ObservedOutputs)
	applied, err = exec.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	requireRepositoryOutputs(t, applied.Outputs)
	api.requireReads(t, "selector", "selector")

	invalid := repositoryExecutor(t, "resolved", api, libs, store, nil)
	_, err = invalid.Plan(t.Context())
	requireRepositoryNameRule(t, err)
	api.requireReads(t)
	saved := repositorySavedPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp,
		repositoryStep(t, saved, "resource.consumer").Decision)
	api.requireReads(t, "selector")
	_, err = invalid.ApplyPlan(t.Context(), saved)
	requireRepositoryNameRule(t, err)
	api.requireReads(t)

	drift := repositorySavedPlan(t, exec)
	api.mu.Lock()
	api.uri = "registry/changed"
	api.mu.Unlock()
	_, err = exec.ApplyPlan(t.Context(), drift)
	require.ErrorContains(t, err, "data source data-source.lookup changed since the plan was computed")
	api.requireReads(t, "selector", "selector")

	invalid.Destroy = true
	_, err = invalid.ApplyPlan(t.Context(), repositorySavedPlan(t, invalid))
	require.NoError(t, err)
	api.requireReads(t)
}

func TestRepositoryLookupNativeDeferred(t *testing.T) {
	api := repositoryTestEndpoint(t)
	libs := repositoryRuntimeLibraries(t)
	for _, input := range []any{"", "selector", nil} {
		store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
		require.NoError(t, err)
		exec := repositoryExecutor(t, "deferred", api, libs, store, input)
		saved := repositorySavedPlan(t, exec)
		ds := repositoryStep(t, saved, "data-source.lookup")
		require.Equal(t, map[string][]string{"name": {"action.seed.value"}}, ds.UnresolvedInputs)
		require.Nil(t, ds.ObservedOutputs)
		api.requireReads(t)
		applied, err := exec.ApplyPlan(t.Context(), saved)
		if input == nil {
			requireRepositoryNameRule(t, err)
			api.requireReads(t)
			continue
		}
		require.NoError(t, err)
		requireRepositoryOutputs(t, applied.Outputs)
		api.requireReads(t, input.(string))
	}
}
