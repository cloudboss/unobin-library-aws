package kms

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

const (
	aliasRuntimeName      = "alias/MiXeD_12/app-test"
	aliasRuntimeARNPrefix = "arn:aws-us-gov:kms:us-gov-west-1:123456789012:"
	aliasRuntimeKeyID     = "mrk-123456789abcdef"
)

type aliasLookupValue struct {
	KeyID string `ub:"key-id"`
}

func (v *aliasLookupValue) Create(context.Context, any) (*aliasLookupValue, error) {
	return v, nil
}

func (v *aliasLookupValue) Read(
	_ context.Context, _ any, prior runtime.Prior[aliasLookupValue, *aliasLookupValue, any],
) (*aliasLookupValue, error) {
	return prior.Outputs, nil
}

func (v *aliasLookupValue) Update(
	context.Context, any, runtime.Prior[aliasLookupValue, *aliasLookupValue, any],
) (*aliasLookupValue, error) {
	return v, nil
}

func (*aliasLookupValue) Delete(
	context.Context, any, runtime.Prior[aliasLookupValue, *aliasLookupValue, any],
) error {
	return nil
}

type aliasLookupSeed struct {
	Name *string
}

type aliasLookupSeedOutput struct {
	Value *string
}

func (s *aliasLookupSeed) Run(context.Context, any) (*aliasLookupSeedOutput, error) {
	return &aliasLookupSeedOutput{Value: s.Name}, nil
}

type aliasLookupRequest struct {
	method        string
	path          string
	target        string
	authorization string
	retry         string
	body          map[string]any
}

type aliasLookupAPI struct {
	endpoint string
	mu       sync.Mutex
	calls    []aliasLookupRequest
	keyID    string
}

func newAliasLookupAPI(t *testing.T) *aliasLookupAPI {
	t.Helper()
	temp := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(temp, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(temp, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "fake-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_MAX_ATTEMPTS", "7")

	api := &aliasLookupAPI{keyID: aliasRuntimeKeyID}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("parse KMS request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		call := aliasLookupRequest{
			method: r.Method, path: r.URL.RequestURI(), target: r.Header.Get("X-Amz-Target"),
			authorization: r.Header.Get("Authorization"), retry: r.Header.Get("Amz-Sdk-Request"),
			body: body,
		}
		api.mu.Lock()
		api.calls = append(api.calls, call)
		keyID := api.keyID
		api.mu.Unlock()

		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		switch call.target {
		case "TrentService.ListAliases":
			fmt.Fprintf(w, `{"Aliases":[{"AliasName":%q,"AliasArn":%q,"TargetKeyId":"stale"}]}`,
				aliasRuntimeName, aliasRuntimeARNPrefix+aliasRuntimeName)
		case "TrentService.DescribeKey":
			fmt.Fprintf(w, `{"KeyMetadata":{"KeyId":%q,"Arn":%q,"KeyState":"Enabled"}}`,
				keyID, aliasRuntimeARNPrefix+"key/"+keyID)
		default:
			t.Errorf("unexpected KMS operation %q", call.target)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	api.endpoint = server.URL
	return api
}

func (a *aliasLookupAPI) requireReads(t *testing.T, names ...string) {
	t.Helper()
	a.mu.Lock()
	calls := a.calls
	a.calls = nil
	a.mu.Unlock()
	var want []aliasLookupRequest
	for _, name := range names {
		want = append(want,
			aliasLookupRequest{
				method: "POST", path: "/", target: "TrentService.ListAliases",
				body: map[string]any{},
			},
			aliasLookupRequest{
				method: "POST", path: "/", target: "TrentService.DescribeKey",
				body: map[string]any{"KeyId": name},
			},
		)
	}
	for i, call := range calls {
		require.Contains(t, call.authorization, "Credential=fake-access-key/")
		require.Contains(t, call.authorization, "/us-gov-west-1/kms/aws4_request")
		require.Equal(t, "attempt=1; max=2", call.retry)
		calls[i].authorization = ""
		calls[i].retry = ""
	}
	require.Equal(t, want, calls)
}

func aliasLookupLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.Schema = readLibrarySchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"data-source.alias": lib.Schema.DataSources["alias"].Constraints,
	}
	return map[string]*runtime.Library{
		"kms": lib,
		"test": {
			Resources: map[string]runtime.ResourceRegistration{
				"value": runtime.MakeResource(
					runtime.ResourceDefinition[aliasLookupValue, *aliasLookupValue, any]{
						SchemaVersion: 1,
					}),
			},
			Actions: map[string]runtime.ActionRegistration{
				"seed": runtime.MakeAction[aliasLookupSeed, *aliasLookupSeedOutput, any](),
			},
		},
	}
}

func aliasLookupExecutor(
	t *testing.T,
	fixture string,
	api *aliasLookupAPI,
	libraries map[string]*runtime.Library,
	store *local.Store,
	lookup any,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "runtime", fixture, "factory.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	body := &f.Factory.Body
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libraries), SyntaxSource: body, Libraries: libraries,
		Inputs: map[string]any{"endpoint": api.endpoint, "lookup": lookup},
		Store:  store,
	}
}

func saveAliasLookupPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func aliasLookupStep(t *testing.T, plan *runtime.PlanFile, address string) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == address {
			return step
		}
	}
	t.Fatalf("plan has no step %q", address)
	return runtime.PlanStep{}
}

func aliasRuntimeOutputs() map[string]any {
	return map[string]any{
		"alias-name":     aliasRuntimeName,
		"arn":            aliasRuntimeARNPrefix + aliasRuntimeName,
		"target-key-id":  aliasRuntimeKeyID,
		"target-key-arn": aliasRuntimeARNPrefix + "key/" + aliasRuntimeKeyID,
	}
}

func requireAliasConstraint(t *testing.T, err error, rule int, message string) {
	t.Helper()
	require.ErrorContains(t, err,
		fmt.Sprintf("data-source.lookup: schema: constraints[%d] (predicate)", rule))
	require.ErrorContains(t, err, message)
}

func applyInitialAliasLookup(
	t *testing.T,
	api *aliasLookupAPI,
	libraries map[string]*runtime.Library,
) (*runtime.Executor, *local.Store) {
	t.Helper()
	store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
	require.NoError(t, err)
	exec := aliasLookupExecutor(t, "resolved", api, libraries, store, aliasRuntimeName)
	initial := saveAliasLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionCreate,
		aliasLookupStep(t, initial, "resource.consumer").Decision)
	got, err := exec.ApplyPlan(t.Context(), initial)
	require.NoError(t, err)
	require.Equal(t, aliasRuntimeOutputs(), got.Outputs)
	api.requireReads(t, aliasRuntimeName, aliasRuntimeName)
	return exec, store
}

func TestAliasLookupNativePlan(t *testing.T) {
	api := newAliasLookupAPI(t)
	libraries := aliasLookupLibraries(t)
	exec, store := applyInitialAliasLookup(t, api, libraries)

	unchanged := saveAliasLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp,
		aliasLookupStep(t, unchanged, "resource.consumer").Decision)
	ds := aliasLookupStep(t, unchanged, "data-source.lookup")
	require.Equal(t, runtime.DecisionRead, ds.Decision)
	require.Equal(t, aliasRuntimeOutputs(), ds.ObservedOutputs)
	got, err := exec.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	require.Equal(t, aliasRuntimeOutputs(), got.Outputs)
	api.requireReads(t, aliasRuntimeName, aliasRuntimeName)

	for _, tc := range []struct {
		name    string
		input   any
		rule    int
		message string
	}{
		{"null", nil, 0, "alias-name is required"},
		{"syntax", "Alias/Acme", 1, "alias-name must start with alias/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := aliasLookupExecutor(t, "resolved", api, libraries, store, tc.input)
			_, err := invalid.Plan(t.Context())
			requireAliasConstraint(t, err, tc.rule, tc.message)
			api.requireReads(t)

			saved := saveAliasLookupPlan(t, exec)
			require.Equal(t, runtime.DecisionNoOp,
				aliasLookupStep(t, saved, "resource.consumer").Decision)
			api.requireReads(t, aliasRuntimeName)
			_, err = invalid.ApplyPlan(t.Context(), saved)
			requireAliasConstraint(t, err, tc.rule, tc.message)
			api.requireReads(t)
		})
	}

	changed := saveAliasLookupPlan(t, exec)
	api.mu.Lock()
	api.keyID = "changed-key-id"
	api.mu.Unlock()
	_, err = exec.ApplyPlan(t.Context(), changed)
	require.ErrorContains(t, err,
		"data source data-source.lookup changed since the plan was computed")
	api.requireReads(t, aliasRuntimeName, aliasRuntimeName)
}

func TestAliasLookupNativeDeferredSavedApply(t *testing.T) {
	api := newAliasLookupAPI(t)
	libraries := aliasLookupLibraries(t)
	for _, tc := range []struct {
		name    string
		input   any
		rule    int
		message string
	}{
		{name: "valid", input: aliasRuntimeName},
		{"null", nil, 0, "alias-name is required"},
		{"syntax", "alias/forbidden!", 1, "alias-name must start with alias/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, store := applyInitialAliasLookup(t, api, libraries)
			exec := aliasLookupExecutor(t, "deferred", api, libraries, store, tc.input)
			saved := saveAliasLookupPlan(t, exec)
			ds := aliasLookupStep(t, saved, "data-source.lookup")
			require.Equal(t, runtime.DecisionRead, ds.Decision)
			require.Equal(t, map[string][]string{"alias-name": {"action.seed.value"}},
				ds.UnresolvedInputs)
			require.Nil(t, ds.ObservedOutputs)
			api.requireReads(t)

			got, err := exec.ApplyPlan(t.Context(), saved)
			if tc.message != "" {
				requireAliasConstraint(t, err, tc.rule, tc.message)
				api.requireReads(t)
				snapshot, err := store.Current()
				require.NoError(t, err)
				consumer := snapshot.Find("resource.consumer")
				require.NotNil(t, consumer)
				want := map[string]any{"key-id": aliasRuntimeKeyID}
				require.Equal(t, want, consumer.Inputs)
				require.Equal(t, want, consumer.Outputs)
				return
			}
			require.NoError(t, err)
			require.Equal(t, aliasRuntimeOutputs(), got.Outputs)
			api.requireReads(t, aliasRuntimeName)
		})
	}
}

func TestAliasLookupDestroySkipsDesiredRead(t *testing.T) {
	api := newAliasLookupAPI(t)
	libraries := aliasLookupLibraries(t)
	_, store := applyInitialAliasLookup(t, api, libraries)
	exec := aliasLookupExecutor(t, "resolved", api, libraries, store, "invalid")
	exec.Destroy = true
	saved := saveAliasLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionDestroy,
		aliasLookupStep(t, saved, "data-source.lookup").Decision)
	_, err := exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	api.requireReads(t)
	snapshot, err := store.Current()
	require.NoError(t, err)
	require.Nil(t, snapshot.Find("data-source.lookup"))
	require.Nil(t, snapshot.Find("resource.consumer"))
}
