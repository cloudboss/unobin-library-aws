package eks_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const podIdentityAddress = "resource.association"

const (
	podIdentityRole   = "arn:aws:iam::123456789012:role/workload"
	podIdentityTarget = "arn:aws:iam::999999999999:role/target"
)

func podIdentityExecutor(
	t *testing.T, library *runtime.Library, fixture, endpoint string, store state.Backend,
) *runtime.Executor {
	t.Helper()
	if store == nil {
		var err error
		store, err = local.NewStore(t.TempDir(), "test-pod-identity", "test", encrypters.Noop{})
		require.NoError(t, err)
	}
	path := filepath.Join("testdata", "ub", "pod-identity-association", fixture, "factory.ub")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, data)
	require.NoError(t, err)
	body := file.Factory.Body
	libs := map[string]*runtime.Library{"aws-eks": library}
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	return &runtime.Executor{
		DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "test-pod-identity", Version: "v0", ContentRevision: "c0"},
		Inputs: map[string]any{
			"endpoint": endpoint, "region": "us-east-1", "cluster": "demo",
			"namespace": "default", "account": "workload", "role": podIdentityRole,
			"missing": "preferred", "target": nil, "disabled": nil, "tags": nil,
		},
	}
}

func requirePodIdentityConstraint(t *testing.T, err error, index, message string) {
	t.Helper()
	require.ErrorContains(t, err, podIdentityAddress+": schema: constraints["+index+"] (predicate)")
	require.ErrorContains(t, err, message)
}

func TestPodIdentityAssociationNativeInput(t *testing.T) {
	library := accessEntryLibrary(t)
	for _, tc := range []struct {
		name, input, value, index, message string
	}{
		{"null identity", "missing", "absent", "0", "cluster-name must be a nonempty string"},
		{"empty cluster", "cluster", "", "0", "cluster-name must be a nonempty string"},
		{"empty namespace", "namespace", "", "1", "namespace must be a nonempty string"},
		{"empty account", "account", "", "2", "service-account must be a nonempty string"},
		{"malformed role", "role", "arn:aws:iam:short:", "4", "role-arn must be an ARN"},
		{"malformed target", "target", "bad", "5", "target-role-arn must be an ARN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec := podIdentityExecutor(t, library, "evaluated", "unused", nil)
			exec.Inputs[tc.input] = tc.value
			_, err := exec.Plan(t.Context())
			requirePodIdentityConstraint(t, err, tc.index, tc.message)
			if tc.input == "missing" {
				require.ErrorContains(t, err, "namespace must be a nonempty string")
				require.ErrorContains(t, err, "service-account must be a nonempty string")
				require.ErrorContains(t, err, "role-arn must be a non-null string")
			}
		})
	}
}

type podIdentityCall struct {
	method  string
	cluster string
	id      string
}

type podIdentityAPI struct {
	mu           sync.Mutex
	region       string
	nextID       int
	associations map[string]map[string]any
	calls        []podIdentityCall
	tokens       []any
}

func podIdentityEndpoint(t *testing.T, region string, nextID int) (*podIdentityAPI, string) {
	t.Helper()
	api := &podIdentityAPI{region: region, nextID: nextID, associations: map[string]map[string]any{}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Authorization"), "/"+region+"/eks/aws4_request")
		api.serve(w, r)
	})
	return api, accessEntryHTTPServer(t, handler)
}

func (api *podIdentityAPI) serve(w http.ResponseWriter, req *http.Request) {
	api.mu.Lock()
	defer api.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "clusters" || parts[2] != "pod-identity-associations" {
		http.Error(w, "unexpected EKS request "+req.URL.Path, http.StatusBadRequest)
		return
	}
	cluster := parts[1]
	if len(parts) == 3 && req.Method == http.MethodPost {
		api.calls = append(api.calls, podIdentityCall{req.Method, cluster, ""})
		var fields map[string]any
		if err := json.NewDecoder(req.Body).Decode(&fields); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		api.tokens = append(api.tokens, fields["clientRequestToken"])
		api.nextID++
		id := fmt.Sprintf("a-%d", api.nextID)
		fields["clusterName"], fields["associationId"] = cluster, id
		fields["associationArn"] = fmt.Sprintf(
			"arn:aws:eks:%s:123456789012:podidentityassociation/%s/%s", api.region, cluster, id,
		)
		if fields["targetRoleArn"] != nil {
			fields["externalId"] = "external-from-eks"
		}
		api.associations[cluster+"/"+id] = fields
		_ = json.NewEncoder(w).Encode(map[string]any{"association": fields})
		return
	}
	if len(parts) != 4 {
		http.Error(w, "missing EKS association ID", http.StatusBadRequest)
		return
	}
	id := parts[3]
	api.calls = append(api.calls, podIdentityCall{req.Method, cluster, id})
	key := cluster + "/" + id
	fields := api.associations[key]
	if fields == nil {
		writeAssociationError(w, 404, "ResourceNotFoundException", "association not found")
		return
	}
	switch req.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"association": fields})
	case http.MethodPost:
		var updates map[string]any
		if err := json.NewDecoder(req.Body).Decode(&updates); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		api.tokens = append(api.tokens, updates["clientRequestToken"])
		for _, field := range []string{"disableSessionTags", "roleArn", "targetRoleArn"} {
			fields[field] = updates[field]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"association": fields})
	case http.MethodDelete:
		delete(api.associations, key)
		_ = json.NewEncoder(w).Encode(map[string]any{"association": fields})
	default:
		http.Error(w, "unexpected EKS method "+req.Method, http.StatusBadRequest)
	}
}

func (api *podIdentityAPI) takeCalls() []podIdentityCall {
	api.mu.Lock()
	defer api.mu.Unlock()
	calls := api.calls
	api.calls = nil
	return calls
}

func podIdentityPlanStep(t *testing.T, plan *runtime.PlanFile) *runtime.PlanStep {
	t.Helper()
	for i := range plan.Steps {
		if plan.Steps[i].Address == podIdentityAddress {
			return &plan.Steps[i]
		}
	}
	t.Fatalf("plan has no step for %s", podIdentityAddress)
	return nil
}

func podIdentitySnapshot(t *testing.T, exec *runtime.Executor) *state.Snapshot {
	t.Helper()
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	return snapshot
}

func TestPodIdentityAssociationReplacementAndRegionalPrior(t *testing.T) {
	library := accessEntryLibrary(t)
	for _, tc := range []struct {
		input  string
		value  string
		reason string
	}{
		{"cluster", "other-cluster", "cluster-name"},
		{"namespace", "other-namespace", "namespace"},
		{"account", "other-account", "service-account"},
		{"region", "us-west-2", "region"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			east, endpoint := podIdentityEndpoint(t, "us-east-1", 0)
			exec := podIdentityExecutor(t, library, "evaluated", endpoint, nil)
			applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
			assert.Equal(t, []podIdentityCall{{"POST", "demo", ""}}, east.takeCalls())
			exec.Inputs[tc.input] = tc.value
			destination, newCluster, expectedID := east, "demo", "a-2"
			if tc.input == "region" {
				west, newEndpoint := podIdentityEndpoint(t, tc.value, 10)
				exec.Inputs["endpoint"] = newEndpoint
				destination, expectedID = west, "a-11"
			}
			if tc.input == "cluster" {
				newCluster = tc.value
			}
			plan := saveAccessEntryPlan(t, exec)
			step := podIdentityPlanStep(t, plan)
			require.Equal(t, runtime.DecisionReplace, step.Decision)
			require.Equal(t, []string{tc.reason}, step.ReplacementReasons)
			assert.Equal(t, []podIdentityCall{{"GET", "demo", "a-1"}}, east.takeCalls())
			applyAccessEntryPlan(t, exec, plan)
			oldCalls := []podIdentityCall{{"GET", "demo", "a-1"}, {"DELETE", "demo", "a-1"}}
			create := podIdentityCall{"POST", newCluster, ""}
			if destination == east {
				oldCalls = append(oldCalls, create)
			} else {
				assert.Equal(t, []podIdentityCall{create}, destination.takeCalls())
			}
			assert.Equal(t, oldCalls, east.takeCalls())
			outputs := podIdentitySnapshot(t, exec).Find(podIdentityAddress).Outputs
			assert.Equal(t, expectedID, outputs["association-id"])
			assert.Equal(t, newCluster, outputs["cluster-name"])
		})
	}
}

func TestPodIdentityAssociationMutableSettingsAndMissingRead(t *testing.T) {
	library := accessEntryLibrary(t)
	api, endpoint := podIdentityEndpoint(t, "us-east-1", 0)
	initial := podIdentityExecutor(t, library, "omitted", endpoint, nil)
	initial.Inputs = map[string]any{"endpoint": endpoint, "region": "us-east-1"}
	plan := saveAccessEntryPlan(t, initial)
	assert.Equal(t, false, podIdentityPlanStep(t, plan).Inputs["disable-session-tags"])
	applyAccessEntryPlan(t, initial, plan)
	assert.Equal(t, []podIdentityCall{{"POST", "demo", ""}}, api.takeCalls())
	exec := podIdentityExecutor(t, library, "evaluated", endpoint, initial.Store)

	describe := podIdentityCall{"GET", "demo", "a-1"}
	update := podIdentityCall{"POST", "demo", "a-1"}
	for _, tc := range []struct {
		input, output string
		value, want   any
		decision      runtime.Decision
	}{
		{"disabled", "disable-session-tags", false, false, runtime.DecisionNoOp},
		{"disabled", "disable-session-tags", nil, false, runtime.DecisionNoOp},
		{"disabled", "disable-session-tags", true, true, runtime.DecisionUpdate},
		{"disabled", "disable-session-tags", nil, false, runtime.DecisionUpdate},
		{"role", "role-arn", "arn:::::", "arn:::::", runtime.DecisionUpdate},
		{"target", "target-role-arn", podIdentityTarget, podIdentityTarget, runtime.DecisionUpdate},
	} {
		exec.Inputs[tc.input] = tc.value
		plan := saveAccessEntryPlan(t, exec)
		step := podIdentityPlanStep(t, plan)
		require.Equal(t, tc.decision, step.Decision)
		assert.Empty(t, step.ReplacementReasons)
		applyAccessEntryPlan(t, exec, plan)
		wantCalls := []podIdentityCall{describe}
		if tc.decision == runtime.DecisionUpdate {
			wantCalls = append(wantCalls, update)
		}
		assert.Equal(t, wantCalls, api.takeCalls())
		outputs := podIdentitySnapshot(t, exec).Find(podIdentityAddress).Outputs
		assert.Equal(t, "a-1", outputs["association-id"])
		assert.Equal(t, tc.want, outputs[tc.output])
	}

	api.mu.Lock()
	delete(api.associations, "demo/a-1")
	api.mu.Unlock()
	recreate := saveAccessEntryPlan(t, exec)
	require.Equal(t, runtime.DecisionCreate, podIdentityPlanStep(t, recreate).Decision)
	applyAccessEntryPlan(t, exec, recreate)
	assert.Equal(t, []podIdentityCall{describe, {"POST", "demo", ""}}, api.takeCalls())
	outputs := podIdentitySnapshot(t, exec).Find(podIdentityAddress).Outputs
	assert.Equal(t, "a-2", outputs["association-id"])
	api.mu.Lock()
	defer api.mu.Unlock()
	assert.Len(t, api.tokens, 6)
	seenTokens := make(map[string]bool)
	for _, value := range api.tokens {
		token, ok := value.(string)
		assert.True(t, ok)
		assert.NotEmpty(t, token)
		assert.False(t, seenTokens[token], "reused token %s in a different operation", token)
		seenTokens[token] = true
	}
}

func TestPodIdentityAssociationDeferredSavedReplacement(t *testing.T) {
	library := accessEntryLibrary(t)
	for _, tc := range []struct {
		name, input, value, selector, rule, message string
	}{
		{name: "valid"},
		{"null cluster", "cluster", "next", "absent", "0", "cluster-name must be a nonempty string"},
		{"null namespace", "namespace", "next", "absent", "1", "namespace must be a nonempty string"},
		{"null account", "account", "next", "absent", "2", "service-account must be a nonempty string"},
		{"null role", "role", podIdentityRole, "absent", "3", "role-arn must be a non-null string"},
		{"empty account", "account", "", "preferred", "2", "service-account must be a nonempty string"},
		{"malformed role", "role", "arn:aws:iam:short:", "preferred", "4", "role-arn must be an ARN"},
		{"malformed target", "target", "bad", "preferred", "5", "target-role-arn must be an ARN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, endpoint := podIdentityEndpoint(t, "us-east-1", 0)
			initial := podIdentityExecutor(t, library, "evaluated", endpoint, nil)
			applyAccessEntryPlan(t, initial, saveAccessEntryPlan(t, initial))
			assert.Equal(t, []podIdentityCall{{"POST", "demo", ""}}, api.takeCalls())

			exec := podIdentityExecutor(t, library, "deferred", endpoint, initial.Store)
			exec.Inputs["cluster"] = "next"
			selectors := map[string]any{
				"cluster": "preferred", "namespace": "preferred", "account": "preferred",
				"role": "preferred", "target": "absent",
			}
			if tc.input != "" {
				exec.Inputs[tc.input] = tc.value
				selectors[tc.input] = tc.selector
			}
			exec.Inputs["selectors"] = selectors
			plan := saveAccessEntryPlan(t, exec)
			step := podIdentityPlanStep(t, plan)
			assert.Equal(t, map[string][]string{
				"cluster-name":    {"resource.seed.association-id"},
				"namespace":       {"resource.seed.association-id"},
				"service-account": {"resource.seed.association-id"},
				"role-arn":        {"resource.seed.association-id"},
				"target-role-arn": {"resource.seed.association-id"},
			}, step.UnresolvedInputs)
			assert.Equal(t, []string{"cluster-name", "namespace", "service-account"},
				step.PendingReplacementReasons)
			assert.Equal(t, []podIdentityCall{{"GET", "demo", "a-1"}}, api.takeCalls())

			apply := podIdentityExecutor(t, library, "deferred", endpoint, initial.Store)
			apply.Inputs = nil
			_, err := apply.ApplyPlan(t.Context(), plan)
			want := []podIdentityCall{{"POST", "seed", ""}}
			if tc.rule == "" {
				require.NoError(t, err)
				want = append(want, podIdentityCall{"GET", "demo", "a-1"},
					podIdentityCall{"DELETE", "demo", "a-1"}, podIdentityCall{"POST", "next", ""},
					podIdentityCall{"GET", "seed", "a-2"})
				outputs := podIdentitySnapshot(t, apply).Find(podIdentityAddress).Outputs
				assert.Equal(t, "a-3", outputs["association-id"])
				assert.Equal(t, "next", outputs["cluster-name"])
				assert.Nil(t, outputs["target-role-arn"])
			} else {
				requirePodIdentityConstraint(t, err, tc.rule, tc.message)
				outputs := podIdentitySnapshot(t, apply).Find(podIdentityAddress).Outputs
				assert.Equal(t, "a-1", outputs["association-id"])
			}
			assert.Equal(t, want, api.takeCalls())
		})
	}
}

func TestPodIdentityAssociationSavedNoOpAndHistoricalDestroy(t *testing.T) {
	const export = "resource.pod-identity-association"
	library := accessEntryLibrary(t)
	for _, tc := range []struct {
		name, input, value, rule, message string
		historical                        map[string]any
	}{
		{
			name: "null identity", input: "missing", value: "absent", rule: "0",
			message: "cluster-name must be a nonempty string",
			historical: map[string]any{
				"cluster-name": nil, "namespace": nil, "service-account": nil, "role-arn": nil,
			},
		},
		{
			name: "empty identity", input: "namespace", value: "", rule: "1",
			message:    "namespace must be a nonempty string",
			historical: map[string]any{"namespace": ""},
		},
		{
			name: "malformed role", input: "role", value: "bad", rule: "4",
			message:    "role-arn must be an ARN",
			historical: map[string]any{"role-arn": "bad"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, endpoint := podIdentityEndpoint(t, "us-east-1", 0)
			exec := podIdentityExecutor(t, library, "evaluated", endpoint, nil)
			applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
			assert.Equal(t, []podIdentityCall{{"POST", "demo", ""}}, api.takeCalls())
			prior := podIdentitySnapshot(t, exec)
			maps.Copy(prior.Find(podIdentityAddress).Inputs, tc.historical)
			revision, err := exec.Store.Write(prior)
			require.NoError(t, err)
			require.NoError(t, exec.Store.SetCurrent(revision))

			exec.Inputs[tc.input] = tc.value
			rules := library.Constraints[export]
			t.Cleanup(func() { library.Constraints[export] = rules })
			delete(library.Constraints, export)
			noOp := saveAccessEntryPlan(t, exec)
			library.Constraints[export] = rules
			require.Equal(t, runtime.DecisionNoOp, podIdentityPlanStep(t, noOp).Decision)
			describe := podIdentityCall{"GET", "demo", "a-1"}
			assert.Equal(t, []podIdentityCall{describe}, api.takeCalls())

			_, err = exec.Plan(t.Context())
			requirePodIdentityConstraint(t, err, tc.rule, tc.message)
			assert.Equal(t, []podIdentityCall{describe}, api.takeCalls())

			fresh := podIdentityExecutor(t, library, "evaluated", endpoint, exec.Store)
			fresh.Inputs = nil
			_, err = fresh.ApplyPlan(t.Context(), noOp)
			requirePodIdentityConstraint(t, err, tc.rule, tc.message)
			assert.Empty(t, api.takeCalls())

			exec.Destroy = true
			applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
			assert.Equal(t, []podIdentityCall{describe, describe, {"DELETE", "demo", "a-1"}},
				api.takeCalls())
			assert.Nil(t, podIdentitySnapshot(t, exec).Find(podIdentityAddress))
		})
	}
}
