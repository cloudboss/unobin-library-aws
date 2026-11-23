package iam

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roleLookupValue struct {
	Name string
}

func (v *roleLookupValue) Create(context.Context, any) (*roleLookupValue, error) {
	return v, nil
}

func (v *roleLookupValue) Read(
	_ context.Context, _ any, prior runtime.Prior[roleLookupValue, *roleLookupValue, any],
) (*roleLookupValue, error) {
	return prior.Outputs, nil
}

func (v *roleLookupValue) Update(
	context.Context, any, runtime.Prior[roleLookupValue, *roleLookupValue, any],
) (*roleLookupValue, error) {
	return v, nil
}

func (*roleLookupValue) Delete(
	context.Context, any, runtime.Prior[roleLookupValue, *roleLookupValue, any],
) error {
	return nil
}

type roleLookupSeed struct {
	Name *string
}

type roleLookupSeedOutput struct {
	Value *string
}

func (s *roleLookupSeed) Run(context.Context, any) (*roleLookupSeedOutput, error) {
	return &roleLookupSeedOutput{Value: s.Name}, nil
}

type roleLookupAPI struct {
	endpoint  string
	mu        sync.Mutex
	calls     []url.Values
	canonical string
}

func newRoleLookupAPI(t *testing.T) *roleLookupAPI {
	t.Helper()
	temp := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(temp, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(temp, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "fake-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	api := &roleLookupAPI{canonical: "Canonical"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse IAM request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		api.mu.Lock()
		api.calls = append(api.calls, r.PostForm)
		name := api.canonical
		api.mu.Unlock()
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprintf(w, `<GetRoleResponse><GetRoleResult><Role>
			<RoleName>%s</RoleName><CreateDate>2025-04-02T03:04:05Z</CreateDate>
			</Role></GetRoleResult></GetRoleResponse>`, name)
	}))
	t.Cleanup(server.Close)
	api.endpoint = server.URL
	return api
}

func (a *roleLookupAPI) requireCalls(t *testing.T, names ...string) {
	t.Helper()
	var want []url.Values
	for _, name := range names {
		want = append(want, url.Values{
			"Action": {"GetRole"}, "RoleName": {name}, "Version": {"2010-05-08"},
		})
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	require.Equal(t, want, a.calls)
	a.calls = nil
}

func roleLookupLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.Schema = readLibrarySchema(t)
	rules := lib.Schema.DataSources["role"].Constraints
	require.Equal(t, []lang.ConstraintSpec{{
		Kind: "predicate", When: "true", Require: "(input.role-name != null)",
		Message: "role-name is required",
	}}, rules)
	lib.Constraints = map[string][]lang.ConstraintSpec{"data-source.role": rules}
	return map[string]*runtime.Library{
		"iam": lib,
		"test": {
			Resources: map[string]runtime.ResourceRegistration{
				"value": runtime.MakeResource(
					runtime.ResourceDefinition[roleLookupValue, *roleLookupValue, any]{
						SchemaVersion: 1,
					}),
			},
			Actions: map[string]runtime.ActionRegistration{
				"seed": runtime.MakeAction[roleLookupSeed, *roleLookupSeedOutput, any](),
			},
		},
	}
}

func roleLookupExecutor(
	t *testing.T,
	fixture string,
	api *roleLookupAPI,
	libraries map[string]*runtime.Library,
	store state.Backend,
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

func saveRoleLookupPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func roleLookupStep(t *testing.T, plan *runtime.PlanFile, address string) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == address {
			return step
		}
	}
	t.Fatalf("plan has no step %q", address)
	return runtime.PlanStep{}
}

func requireRoleNameViolation(t *testing.T, err error) {
	t.Helper()
	require.ErrorContains(t, err, "data-source.lookup: schema: constraints[0] (predicate)")
	require.ErrorContains(t, err, "role-name is required")
}

func TestRoleLookupNativePlan(t *testing.T) {
	api := newRoleLookupAPI(t)
	libraries := roleLookupLibraries(t)
	store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
	require.NoError(t, err)
	exec := roleLookupExecutor(t, "resolved", api, libraries, store, "CaNoNiCaL")
	initial := saveRoleLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionCreate, roleLookupStep(t, initial, "resource.consumer").Decision)
	got, err := exec.ApplyPlan(t.Context(), initial)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"name": "Canonical"}, got.Outputs)
	api.requireCalls(t, "CaNoNiCaL", "CaNoNiCaL")

	unchanged := saveRoleLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp, roleLookupStep(t, unchanged, "resource.consumer").Decision)
	ds := roleLookupStep(t, unchanged, "data-source.lookup")
	require.Equal(t, runtime.DecisionRead, ds.Decision)
	require.Equal(t, "Canonical", ds.ObservedOutputs["role-name"])
	got, err = exec.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"name": "Canonical"}, got.Outputs)
	api.requireCalls(t, "CaNoNiCaL", "CaNoNiCaL")

	invalid := roleLookupExecutor(t, "resolved", api, libraries, store, nil)
	_, err = invalid.Plan(t.Context())
	requireRoleNameViolation(t, err)
	api.requireCalls(t)

	changed := saveRoleLookupPlan(t, exec)
	api.mu.Lock()
	api.canonical = "Changed"
	api.mu.Unlock()
	_, err = exec.ApplyPlan(t.Context(), changed)
	require.ErrorContains(t, err, "data source data-source.lookup changed since the plan was computed")
	api.requireCalls(t, "CaNoNiCaL", "CaNoNiCaL")
}

func TestRoleLookupNativeDeferredSavedApply(t *testing.T) {
	api := newRoleLookupAPI(t)
	libraries := roleLookupLibraries(t)
	for _, tc := range []struct {
		name   string
		lookup any
	}{
		{name: "valid", lookup: "CaNoNiCaL"},
		{name: "null", lookup: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
			require.NoError(t, err)
			initial := roleLookupExecutor(t, "resolved", api, libraries, store, "CaNoNiCaL")
			_, err = initial.ApplyPlan(t.Context(), saveRoleLookupPlan(t, initial))
			require.NoError(t, err)
			api.requireCalls(t, "CaNoNiCaL", "CaNoNiCaL")

			exec := roleLookupExecutor(t, "deferred", api, libraries, store, tc.lookup)
			saved := saveRoleLookupPlan(t, exec)
			ds := roleLookupStep(t, saved, "data-source.lookup")
			require.Equal(t, runtime.DecisionRead, ds.Decision)
			require.Equal(t, map[string][]string{"role-name": {"action.seed.value"}},
				ds.UnresolvedInputs)
			require.Nil(t, ds.ObservedOutputs)
			api.requireCalls(t)

			got, err := exec.ApplyPlan(t.Context(), saved)
			if tc.lookup == nil {
				requireRoleNameViolation(t, err)
				api.requireCalls(t)
				snapshot, err := store.Current()
				require.NoError(t, err)
				consumer := snapshot.Find("resource.consumer")
				require.NotNil(t, consumer)
				assert.Equal(t, map[string]any{"name": "Canonical"}, consumer.Inputs)
				return
			}
			require.NoError(t, err)
			require.Equal(t, map[string]any{"name": "Canonical"}, got.Outputs)
			api.requireCalls(t, "CaNoNiCaL")
		})
	}
}
