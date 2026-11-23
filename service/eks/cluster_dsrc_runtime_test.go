package eks_test

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

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"

	awseks "github.com/cloudboss/unobin-library-aws/service/eks"
)

const (
	clusterLookupName = "MiXeD_12-test"
	clusterLookupARN  = "arn:aws-us-gov:eks:us-gov-west-1:123456789012:cluster/Canonical"
)

type lookupConsumer struct {
	Status string
}

func (r *lookupConsumer) Create(context.Context, any) (*lookupConsumer, error) {
	return r, nil
}

func (*lookupConsumer) Read(
	_ context.Context, _ any, prior runtime.Prior[lookupConsumer, *lookupConsumer, any],
) (*lookupConsumer, error) {
	return prior.Outputs, nil
}

func (r *lookupConsumer) Update(
	context.Context, any, runtime.Prior[lookupConsumer, *lookupConsumer, any],
) (*lookupConsumer, error) {
	return r, nil
}

func (*lookupConsumer) Delete(
	context.Context, any, runtime.Prior[lookupConsumer, *lookupConsumer, any],
) error {
	return nil
}

type lookupSeed struct{ Name *string }
type lookupSeedOutput struct{ Value *string }

func (s *lookupSeed) Run(context.Context, any) (*lookupSeedOutput, error) {
	return &lookupSeedOutput{Value: s.Name}, nil
}

type clusterLookupRequest struct {
	method string
	path   string
	auth   string
}

type clusterLookupAPI struct {
	endpoint string
	mu       sync.Mutex
	requests []clusterLookupRequest
	reorder  bool
	status   string
}

func newClusterLookupAPI(t *testing.T) *clusterLookupAPI {
	t.Helper()
	temp := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(temp, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(temp, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "fake-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	t.Setenv("AWS_REGION", "us-east-1")

	api := &clusterLookupAPI{status: "CREATING"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		api.requests = append(api.requests, clusterLookupRequest{
			method: r.Method, path: r.URL.RequestURI(), auth: r.Header.Get("Authorization"),
		})
		reorder, status := api.reorder, api.status
		api.mu.Unlock()
		if r.Method != http.MethodGet || r.URL.Path != "/clusters/"+clusterLookupName {
			t.Errorf("unexpected EKS request %s %s", r.Method, r.URL.RequestURI())
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		logs := []string{"audit", "api", "audit"}
		subnets := []string{"subnet-b", "subnet-a", "subnet-b"}
		if reorder {
			logs = []string{"api", "audit", "api"}
			subnets = []string{"subnet-a", "subnet-b", "subnet-a"}
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(map[string]any{"cluster": map[string]any{
			"name": "Canonical", "arn": clusterLookupARN, "status": status,
			"createdAt": 1781713845,
			"logging": map[string]any{"clusterLogging": []map[string]any{
				{"enabled": true, "types": logs},
			}},
			"resourcesVpcConfig": map[string]any{"subnetIds": subnets, "vpcId": "vpc-1234"},
			"tags": map[string]string{
				"empty": "", "ordinary": "value", "aws:hidden": "reserved",
			},
		}})
		if err != nil {
			t.Errorf("encode EKS response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	api.endpoint = server.URL
	return api
}

func (a *clusterLookupAPI) requireReads(t *testing.T, names ...string) {
	t.Helper()
	a.mu.Lock()
	calls := a.requests
	a.requests = nil
	a.mu.Unlock()

	var want []clusterLookupRequest
	for _, name := range names {
		want = append(want, clusterLookupRequest{method: http.MethodGet, path: "/clusters/" + name})
	}
	for i, call := range calls {
		require.Contains(t, call.auth, "Credential=fake-access-key/")
		require.Contains(t, call.auth, "/us-gov-west-1/eks/aws4_request")
		calls[i].auth = ""
	}
	require.Equal(t, want, calls)
}

func clusterLookupLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := awseks.Library()
	lib.Schema = readLibrarySchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"data-source.cluster": lib.Schema.DataSources["cluster"].Constraints,
	}
	return map[string]*runtime.Library{
		"eks": lib,
		"test": {
			Resources: map[string]runtime.ResourceRegistration{
				"value": runtime.MakeResource(
					runtime.ResourceDefinition[lookupConsumer, *lookupConsumer, any]{
						SchemaVersion: 1,
					},
				),
			},
			Actions: map[string]runtime.ActionRegistration{
				"seed": runtime.MakeAction[lookupSeed, *lookupSeedOutput, any](),
			},
		},
	}
}

func clusterLookupExecutor(
	t *testing.T, fixture string, api *clusterLookupAPI, libraries map[string]*runtime.Library,
	store *local.Store, input any,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "cluster-lookup", "runtime", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(body, libraries), SyntaxSource: &body, Libraries: libraries,
		Inputs: map[string]any{"endpoint": api.endpoint, "lookup": input},
		Store:  store,
	}
}

func saveClusterLookupPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func clusterLookupStep(t *testing.T, plan *runtime.PlanFile, address string) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == address {
			return step
		}
	}
	t.Fatalf("plan has no step %q", address)
	return runtime.PlanStep{}
}

func requireClusterLookupOutputs(t *testing.T, outputs map[string]any) {
	t.Helper()
	require.Equal(t, map[string]any{
		"name": "Canonical", "arn": clusterLookupARN, "status": "CREATING",
		"logs": []any{"api", "audit"},
		"vpc-config": map[string]any{
			"cluster-security-group-id": "", "control-plane-egress-mode": "",
			"endpoint-private-access": false, "endpoint-public-access": false,
			"public-access-cidrs": []any{}, "security-group-ids": []any{},
			"subnet-ids": []any{"subnet-a", "subnet-b"}, "vpc-id": "vpc-1234",
		},
		"tags": map[string]any{"empty": "", "ordinary": "value"},
	}, outputs)
}

func requireClusterLookupConstraint(t *testing.T, err error, rule int, message string) {
	t.Helper()
	require.ErrorContains(t, err,
		fmt.Sprintf("data-source.lookup: schema: constraints[%d] (predicate)", rule))
	require.ErrorContains(t, err, message)
}

func applyInitialClusterLookup(
	t *testing.T, api *clusterLookupAPI, libraries map[string]*runtime.Library,
) (*runtime.Executor, *local.Store) {
	t.Helper()
	store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
	require.NoError(t, err)
	exec := clusterLookupExecutor(t, "resolved", api, libraries, store, clusterLookupName)
	initial := saveClusterLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionCreate,
		clusterLookupStep(t, initial, "resource.consumer").Decision)
	got, err := exec.ApplyPlan(t.Context(), initial)
	require.NoError(t, err)
	requireClusterLookupOutputs(t, got.Outputs)
	api.requireReads(t, clusterLookupName, clusterLookupName)
	return exec, store
}

func TestClusterLookupNativePlan(t *testing.T) {
	api := newClusterLookupAPI(t)
	libraries := clusterLookupLibraries(t)
	exec, store := applyInitialClusterLookup(t, api, libraries)

	unchanged := saveClusterLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp,
		clusterLookupStep(t, unchanged, "resource.consumer").Decision)
	ds := clusterLookupStep(t, unchanged, "data-source.lookup")
	require.Equal(t, runtime.DecisionRead, ds.Decision)
	require.Equal(t, "Canonical", ds.ObservedOutputs["name"])
	require.Equal(t, "CREATING", ds.ObservedOutputs["status"])
	api.mu.Lock()
	api.reorder = true
	api.mu.Unlock()
	applied, err := exec.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	requireClusterLookupOutputs(t, applied.Outputs)
	api.requireReads(t, clusterLookupName, clusterLookupName)

	for _, tc := range []struct {
		name    string
		input   any
		rule    int
		message string
	}{
		{"null", nil, 0, "name is required"},
		{"syntax", "-invalid", 1, "name must be 1 to 100 ASCII characters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := clusterLookupExecutor(t, "resolved", api, libraries, store, tc.input)
			_, err := invalid.Plan(t.Context())
			requireClusterLookupConstraint(t, err, tc.rule, tc.message)
			api.requireReads(t)

			saved := saveClusterLookupPlan(t, exec)
			require.Equal(t, runtime.DecisionNoOp,
				clusterLookupStep(t, saved, "resource.consumer").Decision)
			api.requireReads(t, clusterLookupName)
			_, err = invalid.ApplyPlan(t.Context(), saved)
			requireClusterLookupConstraint(t, err, tc.rule, tc.message)
			api.requireReads(t)
		})
	}

	changed := saveClusterLookupPlan(t, exec)
	api.mu.Lock()
	api.status = "ACTIVE"
	api.mu.Unlock()
	_, err = exec.ApplyPlan(t.Context(), changed)
	require.ErrorContains(t, err, "data source data-source.lookup changed since the plan was computed")
	api.requireReads(t, clusterLookupName, clusterLookupName)
}

func TestClusterLookupNativeDeferredSavedApply(t *testing.T) {
	api := newClusterLookupAPI(t)
	libraries := clusterLookupLibraries(t)
	for _, tc := range []struct {
		name    string
		input   any
		rule    int
		message string
	}{
		{name: "valid", input: clusterLookupName},
		{"null", nil, 0, "name is required"},
		{"syntax", "Invalid!", 1, "name must be 1 to 100 ASCII characters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, store := applyInitialClusterLookup(t, api, libraries)
			exec := clusterLookupExecutor(t, "deferred", api, libraries, store, tc.input)
			saved := saveClusterLookupPlan(t, exec)
			ds := clusterLookupStep(t, saved, "data-source.lookup")
			require.Equal(t, runtime.DecisionRead, ds.Decision)
			require.Equal(t, map[string][]string{"name": {"action.seed.value"}}, ds.UnresolvedInputs)
			require.Nil(t, ds.ObservedOutputs)
			api.requireReads(t)

			applied, err := exec.ApplyPlan(t.Context(), saved)
			if tc.message != "" {
				requireClusterLookupConstraint(t, err, tc.rule, tc.message)
				api.requireReads(t)
				snapshot, err := store.Current()
				require.NoError(t, err)
				consumer := snapshot.Find("resource.consumer")
				require.NotNil(t, consumer)
				require.Equal(t, map[string]any{"status": "CREATING"}, consumer.Inputs)
				require.Equal(t, map[string]any{"status": "CREATING"}, consumer.Outputs)
				return
			}
			require.NoError(t, err)
			requireClusterLookupOutputs(t, applied.Outputs)
			api.requireReads(t, clusterLookupName)
		})
	}
}

func TestClusterLookupDestroySkipsDesiredRead(t *testing.T) {
	api := newClusterLookupAPI(t)
	libraries := clusterLookupLibraries(t)
	_, store := applyInitialClusterLookup(t, api, libraries)
	exec := clusterLookupExecutor(t, "resolved", api, libraries, store, "-invalid")
	exec.Destroy = true
	saved := saveClusterLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionDestroy,
		clusterLookupStep(t, saved, "data-source.lookup").Decision)

	_, err := exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	api.requireReads(t)
	snapshot, err := store.Current()
	require.NoError(t, err)
	require.Nil(t, snapshot.Find("data-source.lookup"))
	require.Nil(t, snapshot.Find("resource.consumer"))
}
