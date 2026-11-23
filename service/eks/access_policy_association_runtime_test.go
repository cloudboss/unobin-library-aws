package eks_test

import (
	"encoding/json"
	"net/http"
	"net/url"
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

const (
	associationAddress = "resource.association"
	associationARN     = "arn:aws:eks::aws:cluster-access-policy/AmazonEKSViewPolicy"
	otherPolicyARN     = "arn:aws:eks::aws:cluster-access-policy/AmazonEKSEditPolicy"
	eastRegion         = "us-east-1"
	westRegion         = "us-west-2"
)

type associationCall struct {
	method, region, cluster, principal, policy string
}

type associationAPI struct {
	mu          sync.Mutex
	calls       []associationCall
	policies    map[associationCall]map[string]any
	bodies      []map[string]any
	seed        map[string]any
	rejectScope bool
}

func newAssociationAPI() *associationAPI {
	return &associationAPI{policies: map[associationCall]map[string]any{}}
}

func (api *associationAPI) endpoint(t *testing.T, region string) string {
	t.Helper()
	return accessEntryHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Contains(t, req.Header.Get("Authorization"), "/"+region+"/eks/aws4_request")
		api.serve(w, req, region)
	}))
}

func (api *associationAPI) serve(w http.ResponseWriter, req *http.Request, region string) {
	api.mu.Lock()
	defer api.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimPrefix(req.URL.EscapedPath(), "/"), "/")
	if len(parts) < 3 || parts[0] != "clusters" || parts[2] != "access-entries" {
		http.NotFound(w, req)
		return
	}
	if parts[1] == "seed" {
		api.serveSeed(w, req, region)
		return
	}
	if len(parts) < 5 || parts[4] != "access-policies" {
		http.NotFound(w, req)
		return
	}
	principal, err := url.PathUnescape(parts[3])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	call := associationCall{
		method: req.Method, region: region, cluster: parts[1], principal: principal,
	}
	switch call.method {
	case http.MethodPost:
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		call.policy, _ = body["policyArn"].(string)
		api.calls = append(api.calls, call)
		api.bodies = append(api.bodies, body)
		if api.rejectScope {
			writeAssociationError(w, http.StatusBadRequest, "InvalidParameterException",
				"scope rejected by EKS")
			return
		}
		call.method = ""
		api.policies[call] = map[string]any{
			"policyArn": call.policy, "accessScope": body["accessScope"],
			"associatedAt": 1780000000, "modifiedAt": 1780000001,
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"clusterName": "ignored", "principalArn": "ignored",
			"associatedAccessPolicy": map[string]any{"policyArn": "ignored"},
		})
	case http.MethodGet:
		api.calls = append(api.calls, call)
		var policies []map[string]any
		for key, policy := range api.policies {
			if key.region == call.region && key.cluster == call.cluster &&
				key.principal == call.principal {
				policies = append(policies, policy)
			}
		}
		if policies == nil {
			writeAssociationError(w, http.StatusNotFound, "ResourceNotFoundException",
				"test association missing")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"associatedAccessPolicies": policies})
	case http.MethodDelete:
		if len(parts) != 6 {
			http.NotFound(w, req)
			return
		}
		call.policy, err = url.PathUnescape(parts[5])
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		api.calls = append(api.calls, call)
		call.method = ""
		if _, ok := api.policies[call]; !ok {
			writeAssociationError(w, http.StatusNotFound, "ResourceNotFoundException",
				"test association missing")
			return
		}
		delete(api.policies, call)
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func writeAssociationError(w http.ResponseWriter, code int, kind, message string) {
	w.Header().Set("X-Amzn-Errortype", kind)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}

func (api *associationAPI) serveSeed(w http.ResponseWriter, req *http.Request, region string) {
	api.calls = append(api.calls, associationCall{req.Method, region, "seed", seedPrincipal, ""})
	_ = json.NewEncoder(w).Encode(map[string]any{"accessEntry": api.seed})
}

func (api *associationAPI) takeCalls() []associationCall {
	api.mu.Lock()
	defer api.mu.Unlock()
	calls := api.calls
	api.calls = nil
	return calls
}

func (api *associationAPI) changePolicy(region string, field string, value any) {
	api.mu.Lock()
	defer api.mu.Unlock()
	key := associationCall{"", region, "demo", oldPrincipal, associationARN}
	api.policies[key][field] = value
}

func associationExecutor(
	t *testing.T, library *runtime.Library, fixture, endpoint string, backend state.Backend,
) *runtime.Executor {
	t.Helper()
	if backend == nil {
		var err error
		backend, err = local.NewStore(t.TempDir(), "eks-association", "test", encrypters.Noop{})
		require.NoError(t, err)
	}
	path := filepath.Join("testdata", "ub", "access-policy-association", fixture, "factory.ub")
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
		DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: backend,
		Factory: state.FactoryInfo{Name: "eks-association", Version: "v0", ContentRevision: "c0"},
		Inputs: map[string]any{
			"endpoint": endpoint, "region": eastRegion, "cluster": "demo",
			"principal": oldPrincipal, "policy": associationARN,
			"scope-type": "cluster", "namespaces": nil,
		},
	}
}

func associationPlanStep(t *testing.T, plan *runtime.PlanFile) *runtime.PlanStep {
	t.Helper()
	for i := range plan.Steps {
		if plan.Steps[i].Address == associationAddress {
			return &plan.Steps[i]
		}
	}
	t.Fatalf("plan has no step for %s", associationAddress)
	return nil
}

func applyAssociation(
	t *testing.T, exec *runtime.Executor, plan *runtime.PlanFile,
) (map[string]any, map[string]any) {
	t.Helper()
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	entry := snapshot.Find(associationAddress)
	require.NotNil(t, entry)
	return entry.Inputs, entry.Outputs
}

func TestAccessPolicyAssociationSerializedScope(t *testing.T) {
	library := accessEntryLibrary(t)
	for _, tc := range []struct {
		name       string
		scope      string
		namespaces any
		want       map[string]any
		reject     bool
	}{
		{"cluster omitted", "cluster", nil,
			map[string]any{"type": "cluster", "namespaces": []any{}}, false},
		{"cluster empty", "cluster", []any{},
			map[string]any{"type": "cluster", "namespaces": []any{}}, false},
		{"cluster with namespaces", "cluster", []any{"two", "one", "two"},
			map[string]any{"type": "cluster", "namespaces": []any{"one", "two"}}, false},
		{"namespace membership", "namespace", []any{"a*", "A", "a", "", "A"},
			map[string]any{"type": "namespace", "namespaces": []any{"A", "a", "a*"}}, false},
		{"future type", "Future", []any{"bad namespace!"},
			map[string]any{"type": "Future", "namespaces": []any{"bad namespace!"}}, true},
		{"empty type", "", []any{""}, map[string]any{"namespaces": []any{}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newAssociationAPI()
			api.rejectScope = tc.reject
			exec := associationExecutor(t, library, "evaluated", api.endpoint(t, eastRegion), nil)
			exec.Inputs["scope-type"], exec.Inputs["namespaces"] = tc.scope, tc.namespaces
			saved := saveAccessEntryPlan(t, exec)
			_, err := exec.ApplyPlan(t.Context(), saved)
			wantCalls := []associationCall{
				{"POST", eastRegion, "demo", oldPrincipal, associationARN},
			}
			if tc.reject {
				require.ErrorContains(t, err, "scope rejected by EKS")
			} else {
				require.NoError(t, err)
				wantCalls = append(wantCalls, associationCall{"GET", eastRegion, "demo", oldPrincipal, ""})
			}
			assert.Equal(t, wantCalls, api.takeCalls())
			api.mu.Lock()
			defer api.mu.Unlock()
			assert.Equal(t, []map[string]any{
				{"policyArn": associationARN, "accessScope": tc.want},
			}, api.bodies)
		})
	}
}

func TestAccessPolicyAssociationScopeEqualityAndTimestampDrift(t *testing.T) {
	api := newAssociationAPI()
	exec := associationExecutor(t, accessEntryLibrary(t), "evaluated",
		api.endpoint(t, eastRegion), nil)
	get := associationCall{"GET", eastRegion, "demo", oldPrincipal, ""}
	_, outputs := applyAssociation(t, exec, saveAccessEntryPlan(t, exec))
	assert.Equal(t, map[string]any{
		"cluster-name": "demo", "principal-arn": oldPrincipal, "policy-arn": associationARN,
		"access-scope":  map[string]any{"type": "cluster", "namespaces": []any{}},
		"associated-at": "2026-05-28T20:26:40Z", "modified-at": "2026-05-28T20:26:41Z",
	}, outputs)
	api.takeCalls()

	exec.Inputs["namespaces"] = []any{}
	saved := saveAccessEntryPlan(t, exec)
	assert.Equal(t, runtime.DecisionNoOp, associationPlanStep(t, saved).Decision)
	applyAssociation(t, exec, saved)
	assert.Equal(t, []associationCall{get}, api.takeCalls())

	exec.Inputs["scope-type"] = "namespace"
	exec.Inputs["namespaces"] = []any{"a", "B", "a*", ""}
	saved = saveAccessEntryPlan(t, exec)
	assert.Equal(t, runtime.DecisionReplace, associationPlanStep(t, saved).Decision)
	applyAssociation(t, exec, saved)
	api.takeCalls()

	exec.Inputs["namespaces"] = []any{"B", "a*", "a", "B"}
	saved = saveAccessEntryPlan(t, exec)
	assert.Equal(t, runtime.DecisionNoOp, associationPlanStep(t, saved).Decision)
	applyAssociation(t, exec, saved)
	assert.Equal(t, []associationCall{get}, api.takeCalls())

	api.changePolicy(eastRegion, "associatedAt", 1780000010)
	api.changePolicy(eastRegion, "modifiedAt", 1780000011)
	saved = saveAccessEntryPlan(t, exec)
	assert.Equal(t, runtime.DecisionUpdate, associationPlanStep(t, saved).Decision)
	_, outputs = applyAssociation(t, exec, saved)
	assert.Equal(t, "2026-05-28T20:26:50Z", outputs["associated-at"])
	assert.Equal(t, "2026-05-28T20:26:51Z", outputs["modified-at"])
	assert.Equal(t, []associationCall{get}, api.takeCalls())

	exec.Inputs["namespaces"] = []any{"b", "a*", "a"}
	saved = saveAccessEntryPlan(t, exec)
	step := associationPlanStep(t, saved)
	assert.Equal(t, runtime.DecisionReplace, step.Decision)
	assert.Equal(t, []string{"access-scope"}, step.ReplacementReasons)
}

func TestAccessPolicyAssociationReplacesPriorTarget(t *testing.T) {
	library := accessEntryLibrary(t)
	for _, tc := range []struct {
		name, input, reason, value         string
		cluster, principal, policy, region string
	}{
		{"cluster", "cluster", "cluster-name", "next",
			"next", oldPrincipal, associationARN, eastRegion},
		{"principal", "principal", "principal-arn", newPrincipal,
			"demo", newPrincipal, associationARN, eastRegion},
		{"policy", "policy", "policy-arn", otherPolicyARN,
			"demo", oldPrincipal, otherPolicyARN, eastRegion},
		{"scope", "scope-type", "access-scope", "namespace",
			"demo", oldPrincipal, associationARN, eastRegion},
		{"region", "region", "region", westRegion,
			"demo", oldPrincipal, associationARN, westRegion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newAssociationAPI()
			exec := associationExecutor(t, library, "evaluated", api.endpoint(t, eastRegion), nil)
			applyAssociation(t, exec, saveAccessEntryPlan(t, exec))
			api.takeCalls()
			exec.Inputs[tc.input] = tc.value
			if tc.region == westRegion {
				exec.Inputs["endpoint"] = api.endpoint(t, westRegion)
				api.mu.Lock()
				old := associationCall{"", eastRegion, "demo", oldPrincipal, associationARN}
				newKey := associationCall{"", westRegion, "demo", oldPrincipal, associationARN}
				api.policies[newKey] = api.policies[old]
				api.mu.Unlock()
			}
			saved := saveAccessEntryPlan(t, exec)
			step := associationPlanStep(t, saved)
			assert.Equal(t, runtime.DecisionReplace, step.Decision)
			assert.Equal(t, []string{tc.reason}, step.ReplacementReasons)
			applyAssociation(t, exec, saved)
			oldRead := associationCall{"GET", eastRegion, "demo", oldPrincipal, ""}
			assert.Equal(t, []associationCall{
				oldRead, oldRead, {"DELETE", eastRegion, "demo", oldPrincipal, associationARN},
				{"POST", tc.region, tc.cluster, tc.principal, tc.policy},
				{"GET", tc.region, tc.cluster, tc.principal, ""},
			}, api.takeCalls())
		})
	}
}

func TestAccessPolicyAssociationScopeDriftBeforeAndAfterRefresh(t *testing.T) {
	api := newAssociationAPI()
	exec := associationExecutor(t, accessEntryLibrary(t), "evaluated",
		api.endpoint(t, eastRegion), nil)
	applyAssociation(t, exec, saveAccessEntryPlan(t, exec))
	api.takeCalls()

	api.changePolicy(eastRegion, "accessScope", map[string]any{
		"type": "namespace", "namespaces": []string{"external"},
	})
	get := associationCall{"GET", eastRegion, "demo", oldPrincipal, ""}
	direct := associationPlanStep(t, saveAccessEntryPlan(t, exec))
	assert.Equal(t, runtime.DecisionReplace, direct.Decision)
	assert.Equal(t, []string{"access-scope"}, direct.ReplacementReasons)
	assert.Equal(t, []associationCall{get}, api.takeCalls())

	result, err := exec.Refresh(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, result.Refreshed)
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	entry := snapshot.Find(associationAddress)
	require.NotNil(t, entry)
	assert.Equal(t, map[string]any{"type": "cluster", "namespaces": nil},
		entry.Inputs["access-scope"])
	assert.Equal(t, map[string]any{"type": "namespace", "namespaces": []any{"external"}},
		entry.Outputs["access-scope"])
	after := associationPlanStep(t, saveAccessEntryPlan(t, exec))
	assert.Equal(t, runtime.DecisionNoOp, after.Decision)
	assert.Empty(t, after.ReplacementReasons)
	assert.Equal(t, []associationCall{get, get}, api.takeCalls())
}

func requireAssociationConstraint(t *testing.T, err error, rule string) {
	t.Helper()
	require.ErrorContains(t, err, associationAddress+": schema: "+rule+" (predicate)")
}

func TestAccessPolicyAssociationNativeRulesOnEvaluatedAndSavedNoOp(t *testing.T) {
	for _, tc := range []struct {
		input, value, rule string
	}{
		{"cluster", "_invalid", "constraints[0]"},
		{"principal", "arn:bad:iam::123456789012:role/demo", "constraints[1]"},
		{"policy", "arn:aws:eks:wrong:aws:cluster-access-policy/demo", "constraints[2]"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			library := accessEntryLibrary(t)
			api := newAssociationAPI()
			exec := associationExecutor(t, library, "evaluated", api.endpoint(t, eastRegion), nil)
			exec.Inputs[tc.input] = tc.value
			_, err := exec.Plan(t.Context())
			requireAssociationConstraint(t, err, tc.rule)
			assert.Empty(t, api.takeCalls())

			key := "resource.access-policy-association"
			rules := library.Constraints[key]
			delete(library.Constraints, key)
			applyAssociation(t, exec, saveAccessEntryPlan(t, exec))
			cluster := exec.Inputs["cluster"].(string)
			principal := exec.Inputs["principal"].(string)
			policy := exec.Inputs["policy"].(string)
			get := associationCall{"GET", eastRegion, cluster, principal, ""}
			assert.Equal(t, []associationCall{{"POST", eastRegion, cluster, principal, policy}, get},
				api.takeCalls())

			saved := saveAccessEntryPlan(t, exec)
			assert.Equal(t, runtime.DecisionNoOp, associationPlanStep(t, saved).Decision)
			assert.Equal(t, []associationCall{get}, api.takeCalls())
			library.Constraints[key] = rules

			_, err = exec.Plan(t.Context())
			requireAssociationConstraint(t, err, tc.rule)
			assert.Equal(t, []associationCall{get}, api.takeCalls())

			applyExec := associationExecutor(t, library, "evaluated", "", exec.Store)
			applyExec.Inputs = nil
			_, err = applyExec.ApplyPlan(t.Context(), saved)
			requireAssociationConstraint(t, err, tc.rule)
			assert.Empty(t, api.takeCalls())

			exec.Destroy = true
			destroy := saveAccessEntryPlan(t, exec)
			_, err = exec.ApplyPlan(t.Context(), destroy)
			require.NoError(t, err)
			assert.Equal(t, []associationCall{
				get, get, {"DELETE", eastRegion, cluster, principal, policy},
			}, api.takeCalls())
			snapshot, err := exec.Store.Current()
			require.NoError(t, err)
			assert.Nil(t, snapshot.Find(associationAddress))
		})
	}
}

func TestAccessPolicyAssociationRejectsEvaluatedNullRequiredFields(t *testing.T) {
	library := accessEntryLibrary(t)
	for _, tc := range []struct{ input, rule string }{
		{"cluster", "constraints[3]"},
		{"principal", "constraints[4]"},
		{"policy", "constraints[5]"},
		{"scope-type", "constraints[6]"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			api := newAssociationAPI()
			exec := associationExecutor(t, library, "evaluated", api.endpoint(t, eastRegion), nil)
			exec.Inputs[tc.input] = nil
			_, err := exec.Plan(t.Context())
			requireAssociationConstraint(t, err, tc.rule)
			assert.Empty(t, api.takeCalls())

			key := "resource.access-policy-association"
			rules := library.Constraints[key]
			t.Cleanup(func() { library.Constraints[key] = rules })
			delete(library.Constraints, key)
			saved := saveAccessEntryPlan(t, exec)
			assert.Equal(t, runtime.DecisionCreate, associationPlanStep(t, saved).Decision)
			library.Constraints[key] = rules
			exec.Inputs = nil
			_, err = exec.ApplyPlan(t.Context(), saved)
			requireAssociationConstraint(t, err, tc.rule)
			assert.Empty(t, api.takeCalls())
		})
	}
}

func TestAccessPolicyAssociationNativeRulesOnDeferredSavedPlans(t *testing.T) {
	library := accessEntryLibrary(t)
	for _, existing := range []bool{false, true} {
		name := "create"
		if existing {
			name = "replace"
		}
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name, cluster, principal, policy, rule string
			}{
				{"valid", "next", newPrincipal, otherPolicyARN, ""},
				{"invalid name", "_invalid", newPrincipal, otherPolicyARN, "constraints[0]"},
				{"invalid principal", "next", "arn:bad:iam:::role/demo",
					otherPolicyARN, "constraints[1]"},
				{"invalid policy", "next", newPrincipal, "arn:aws:eks::bad:policy",
					"constraints[2]"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					api := newAssociationAPI()
					endpoint := api.endpoint(t, eastRegion)
					api.seed = map[string]any{
						"accessEntryArn": tc.principal, "clusterName": "seed",
						"principalArn": seedPrincipal, "username": tc.cluster,
						"kubernetesGroups": []string{tc.policy},
					}
					var store state.Backend
					oldGet := associationCall{"GET", eastRegion, "demo", oldPrincipal, ""}
					if existing {
						prior := associationExecutor(t, library, "evaluated", endpoint, nil)
						applyAssociation(t, prior, saveAccessEntryPlan(t, prior))
						store = prior.Store
						api.takeCalls()
					}
					exec := associationExecutor(t, library, "deferred", endpoint, store)
					saved := saveAccessEntryPlan(t, exec)
					step := associationPlanStep(t, saved)
					assert.Equal(t, map[string][]string{
						"cluster-name":  {"resource.seed.user-name"},
						"principal-arn": {"resource.seed.arn"},
						"policy-arn":    {"resource.seed.kubernetes-groups[...]"},
					}, step.UnresolvedInputs)
					if existing {
						assert.Equal(t, []string{"cluster-name", "policy-arn", "principal-arn"},
							step.PendingReplacementReasons)
						assert.Equal(t, []associationCall{oldGet}, api.takeCalls())
					} else {
						assert.Equal(t, runtime.DecisionCreate, step.Decision)
						assert.Empty(t, api.takeCalls())
					}

					exec.Inputs = nil
					_, err := exec.ApplyPlan(t.Context(), saved)
					want := []associationCall{
						{"POST", eastRegion, "seed", seedPrincipal, ""},
						{"GET", eastRegion, "seed", seedPrincipal, ""},
					}
					if tc.rule != "" {
						requireAssociationConstraint(t, err, tc.rule)
					} else {
						require.NoError(t, err)
						if existing {
							want = append(want, oldGet,
								associationCall{"DELETE", eastRegion, "demo", oldPrincipal, associationARN})
						}
						want = append(want,
							associationCall{"POST", eastRegion, tc.cluster, tc.principal, tc.policy},
							associationCall{"GET", eastRegion, tc.cluster, tc.principal, ""},
							associationCall{"GET", eastRegion, "seed", seedPrincipal, ""})
					}
					assert.Equal(t, want, api.takeCalls())
				})
			}
		})
	}
}
