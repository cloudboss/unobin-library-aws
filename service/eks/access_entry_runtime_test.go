package eks_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	entryAddress  = "resource.entry"
	oldPrincipal  = "arn:aws:iam::123456789012:role/old"
	newPrincipal  = "arn:aws:iam::123456789012:role/new"
	seedPrincipal = "arn:aws:iam::123456789012:role/seed"
)

type accessEntryCall struct {
	method string
	key    string
}

type accessEntryAPI struct {
	mu          sync.Mutex
	calls       []accessEntryCall
	entries     map[string]map[string]any
	updateBody  map[string]any
	seedCluster string
	seedType    string
	tagRemovals []string
	tagBody     map[string]map[string]string
}

func (api *accessEntryAPI) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	api.mu.Lock()
	defer api.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if arn, ok := strings.CutPrefix(req.URL.Path, "/tags/"); ok {
		api.serveTags(w, req, arn)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(req.URL.Path, "/clusters/"), "/", 3)
	if len(parts) < 2 || parts[1] != "access-entries" {
		http.NotFound(w, req)
		return
	}
	var body map[string]any
	if req.Method == http.MethodPost {
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	cluster := parts[0]
	principal, _ := body["principalArn"].(string)
	if len(parts) == 3 {
		principal = parts[2]
	}
	key := cluster + "/" + principal
	method := req.Method
	if method == http.MethodPost && len(parts) == 3 {
		method = "UPDATE"
	}
	api.calls = append(api.calls, accessEntryCall{method, key})
	entry := api.entries[key]
	switch {
	case method == http.MethodPost:
		entry = body
		entry["accessEntryArn"] = "test-arn/" + key
		entry["clusterName"] = cluster
		entry["createdAt"] = 1780000000
		entry["modifiedAt"] = 1780000001
		if _, present := entry["username"]; !present {
			entry["username"] = "remote-user"
		}
		if cluster == "seed" {
			entry["username"] = api.seedCluster
			entry["kubernetesGroups"] = []string{api.seedType}
		}
		api.entries[key] = entry
	case entry == nil:
		w.Header().Set("X-Amzn-Errortype", "ResourceNotFoundException")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "missing test access entry"})
		return
	case method == "UPDATE":
		api.updateBody = body
		for field, value := range body {
			entry[field] = value
		}
	case method == http.MethodDelete:
		delete(api.entries, key)
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"accessEntry": entry})
}

func (api *accessEntryAPI) serveTags(w http.ResponseWriter, req *http.Request, arn string) {
	api.calls = append(api.calls, accessEntryCall{req.Method, arn})
	entry := api.entries[strings.TrimPrefix(arn, "test-arn/")]
	if entry == nil || entry["accessEntryArn"] != arn {
		http.NotFound(w, req)
		return
	}
	tags, _ := entry["tags"].(map[string]any)
	if tags == nil {
		tags = map[string]any{}
		entry["tags"] = tags
	}
	switch req.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"tags": tags})
	case http.MethodDelete:
		api.tagRemovals = req.URL.Query()["tagKeys"]
		for _, key := range api.tagRemovals {
			delete(tags, key)
		}
	case http.MethodPost:
		var body map[string]map[string]string
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		api.tagBody = body
		for key, value := range body["tags"] {
			tags[key] = value
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (api *accessEntryAPI) takeCalls() []accessEntryCall {
	api.mu.Lock()
	defer api.mu.Unlock()
	calls := api.calls
	api.calls = nil
	return calls
}

func (api *accessEntryAPI) remove(key string) {
	api.mu.Lock()
	defer api.mu.Unlock()
	delete(api.entries, key)
}

func (api *accessEntryAPI) changeMembership(key, username string, groups []string) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.entries[key]["username"] = username
	api.entries[key]["kubernetesGroups"] = groups
}

func accessEntryEndpoint(t *testing.T) (*accessEntryAPI, string) {
	t.Helper()
	api := &accessEntryAPI{entries: map[string]map[string]any{}}
	return api, accessEntryHTTPServer(t, api)
}

func accessEntryHTTPServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	return server.URL
}

func accessEntryExecutor(
	t *testing.T, library *runtime.Library, fixture, endpoint string, store state.Backend,
) *runtime.Executor {
	t.Helper()
	if store == nil {
		var err error
		store, err = local.NewStore(t.TempDir(), "test-access-entry", "test", encrypters.Noop{})
		require.NoError(t, err)
	}
	libs := map[string]*runtime.Library{"aws-eks": library}
	body := accessEntryFixture(t, fixture)
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	return &runtime.Executor{
		DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "test-access-entry", Version: "v0", ContentRevision: "c0"},
		Inputs: map[string]any{
			"endpoint": endpoint, "cluster": "demo", "principal": oldPrincipal,
			"entry-type": nil, "groups": nil, "username": nil, "tags": nil,
		},
	}
}

func saveAccessEntryPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	data, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(data)
	require.NoError(t, err)
	return saved
}

func accessEntryPlanStep(t *testing.T, plan *runtime.PlanFile) *runtime.PlanStep {
	t.Helper()
	for i := range plan.Steps {
		if plan.Steps[i].Address == entryAddress {
			return &plan.Steps[i]
		}
	}
	t.Fatalf("plan has no step for %s", entryAddress)
	return nil
}

func applyAccessEntryPlan(t *testing.T, exec *runtime.Executor, plan *runtime.PlanFile) {
	t.Helper()
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
}

func TestAccessEntryDefaultPlanAndExternalDeletion(t *testing.T) {
	api, endpoint := accessEntryEndpoint(t)
	library := accessEntryLibrary(t)
	exec := accessEntryExecutor(t, library, "omitted-type", endpoint, nil)
	first := saveAccessEntryPlan(t, exec)
	assert.Equal(t, "STANDARD", accessEntryPlanStep(t, first).Inputs["type"])
	applyAccessEntryPlan(t, exec, first)
	key := "demo/" + oldPrincipal
	assert.Equal(t, []accessEntryCall{{"POST", key}, {"GET", key}}, api.takeCalls())
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"arn":               "test-arn/" + key,
		"cluster-name":      "demo",
		"principal-arn":     oldPrincipal,
		"type":              "STANDARD",
		"user-name":         "remote-user",
		"kubernetes-groups": []any{},
		"tags":              map[string]any{},
		"created-at":        time.Unix(1780000000, 0).UTC().Format(time.RFC3339),
		"modified-at":       time.Unix(1780000001, 0).UTC().Format(time.RFC3339),
	}, snapshot.Find(entryAddress).Outputs)

	current := accessEntryExecutor(t, library, "evaluated", endpoint, exec.Store)
	for _, entryType := range []any{nil, "STANDARD"} {
		current.Inputs["entry-type"] = entryType
		saved := saveAccessEntryPlan(t, current)
		step := accessEntryPlanStep(t, saved)
		assert.Equal(t, runtime.DecisionNoOp, step.Decision)
		assert.Equal(t, entryType, step.Inputs["type"])
		applyExec := accessEntryExecutor(t, library, "evaluated", endpoint, current.Store)
		applyExec.Inputs = nil
		applyAccessEntryPlan(t, applyExec, saved)
		assert.Equal(t, []accessEntryCall{{"GET", key}}, api.takeCalls())
	}
	current.Inputs["entry-type"] = "EC2_LINUX"
	changed := accessEntryPlanStep(t, saveAccessEntryPlan(t, current))
	assert.Equal(t, runtime.DecisionReplace, changed.Decision)
	assert.Equal(t, []string{"type"}, changed.ReplacementReasons)
	api.takeCalls()

	api.remove(key)
	missing := accessEntryPlanStep(t, saveAccessEntryPlan(t, current))
	assert.Equal(t, runtime.DecisionCreate, missing.Decision)
	assert.Equal(t, []accessEntryCall{{"GET", key}}, api.takeCalls())
}

func TestAccessEntryReplacesOldIdentityAfterPreflight(t *testing.T) {
	library := accessEntryLibrary(t)
	for _, tc := range []struct {
		name, field, value, key string
	}{
		{"cluster", "cluster-name", "next", "next/" + oldPrincipal},
		{"principal", "principal-arn", newPrincipal, "demo/" + newPrincipal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, endpoint := accessEntryEndpoint(t)
			exec := accessEntryExecutor(t, library, "evaluated", endpoint, nil)
			applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
			api.takeCalls()

			oldKey := "demo/" + oldPrincipal
			for _, malformed := range []string{
				"arn:wrong:iam::123456789012:role/x",
				"arn:aws:iam:invalid:123456789012:role/x",
				"arn:aws:iam::abc:role/x", "arn:aws:iam::123456789012:",
			} {
				exec.Inputs["principal"] = malformed
				invalid := saveAccessEntryPlan(t, exec)
				assert.Equal(t, runtime.DecisionReplace, accessEntryPlanStep(t, invalid).Decision)
				_, err := exec.ApplyPlan(t.Context(), invalid)
				require.ErrorContains(t, err, "principal-arn")
				assert.Equal(t, []accessEntryCall{{"GET", oldKey}}, api.takeCalls())
			}

			exec.Inputs["principal"] = oldPrincipal
			exec.Inputs[tc.name] = tc.value
			saved := saveAccessEntryPlan(t, exec)
			step := accessEntryPlanStep(t, saved)
			assert.Equal(t, runtime.DecisionReplace, step.Decision)
			assert.Equal(t, []string{tc.field}, step.ReplacementReasons)
			applyAccessEntryPlan(t, exec, saved)
			assert.Equal(t, []accessEntryCall{
				{"GET", oldKey}, {"GET", oldKey}, {"DELETE", oldKey},
				{"POST", tc.key}, {"GET", tc.key},
			}, api.takeCalls())
		})
	}
}

func TestAccessEntryNativeRulesOnEvaluatedAndSavedNoOp(t *testing.T) {
	library := accessEntryLibrary(t)
	for _, tc := range []struct {
		name, input, value, rule string
	}{
		{"name", "cluster", "_invalid", "constraints[0]"},
		{"type", "entry-type", "standard", "constraints[1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, endpoint := accessEntryEndpoint(t)
			exec := accessEntryExecutor(t, library, "evaluated", endpoint, nil)
			exec.Inputs[tc.input] = tc.value
			priorKey := exec.Inputs["cluster"].(string) + "/" + oldPrincipal
			_, err := exec.Plan(t.Context())
			requireAccessEntryConstraint(t, err, tc.rule)
			assert.Empty(t, api.takeCalls())

			key := "resource.access-entry"
			rules := library.Constraints[key]
			t.Cleanup(func() { library.Constraints[key] = rules })
			delete(library.Constraints, key)
			applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
			assert.Equal(t, []accessEntryCall{{"POST", priorKey}, {"GET", priorKey}},
				api.takeCalls())
			saved := saveAccessEntryPlan(t, exec)
			assert.Equal(t, runtime.DecisionNoOp, accessEntryPlanStep(t, saved).Decision)
			api.takeCalls()

			library.Constraints[key] = rules
			_, err = exec.Plan(t.Context())
			requireAccessEntryConstraint(t, err, tc.rule)
			assert.Equal(t, []accessEntryCall{{"GET", priorKey}}, api.takeCalls())
			applyExec := accessEntryExecutor(t, library, "evaluated", endpoint, exec.Store)
			applyExec.Inputs = nil
			_, err = applyExec.ApplyPlan(t.Context(), saved)
			requireAccessEntryConstraint(t, err, tc.rule)
			assert.Empty(t, api.takeCalls())

			exec.Destroy = true
			destroy := saveAccessEntryPlan(t, exec)
			applyAccessEntryPlan(t, exec, destroy)
			assert.Equal(t, []accessEntryCall{
				{"GET", priorKey}, {"GET", priorKey}, {"DELETE", priorKey},
			}, api.takeCalls())
		})
	}
}

func requireAccessEntryConstraint(t *testing.T, err error, rule string) {
	t.Helper()
	require.ErrorContains(t, err, entryAddress+": schema: "+rule+" (predicate)")
}

func TestAccessEntryNativeRulesOnDeferredReplacement(t *testing.T) {
	library := accessEntryLibrary(t)
	for _, tc := range []struct {
		name, cluster, entryType, rule string
	}{
		{"valid", "next", "EC2_LINUX", ""},
		{"invalid name", "_invalid", "STANDARD", "constraints[0]"},
		{"invalid type", "next", "standard", "constraints[1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, endpoint := accessEntryEndpoint(t)
			initial := accessEntryExecutor(t, library, "evaluated", endpoint, nil)
			applyAccessEntryPlan(t, initial, saveAccessEntryPlan(t, initial))
			api.takeCalls()
			exec := accessEntryExecutor(t, library, "deferred", endpoint, initial.Store)
			api.mu.Lock()
			api.seedCluster, api.seedType = tc.cluster, tc.entryType
			api.mu.Unlock()
			saved := saveAccessEntryPlan(t, exec)
			step := accessEntryPlanStep(t, saved)
			assert.Equal(t, map[string][]string{
				"cluster-name": {"resource.seed.user-name"},
				"type":         {"resource.seed.kubernetes-groups[...]"},
			}, step.UnresolvedInputs)
			assert.Equal(t, []string{"cluster-name", "type"}, step.PendingReplacementReasons)
			oldKey, seedKey := "demo/"+oldPrincipal, "seed/"+seedPrincipal
			assert.Equal(t, []accessEntryCall{{"GET", oldKey}}, api.takeCalls())
			exec.Inputs = nil
			_, err := exec.ApplyPlan(t.Context(), saved)
			calls := []accessEntryCall{{"POST", seedKey}, {"GET", seedKey}}
			if tc.rule == "" {
				require.NoError(t, err)
				nextKey := tc.cluster + "/" + oldPrincipal
				calls = append(calls,
					accessEntryCall{"GET", oldKey}, accessEntryCall{"DELETE", oldKey},
					accessEntryCall{"POST", nextKey}, accessEntryCall{"GET", nextKey},
					accessEntryCall{"GET", seedKey},
				)
			} else {
				requireAccessEntryConstraint(t, err, tc.rule)
			}
			assert.Equal(t, calls, api.takeCalls())
		})
	}
}

func TestAccessEntryMembershipAndMissingDestroy(t *testing.T) {
	api, endpoint := accessEntryEndpoint(t)
	exec := accessEntryExecutor(t, accessEntryLibrary(t), "evaluated", endpoint, nil)
	exec.Inputs["groups"], exec.Inputs["username"] = []any{"b", "a"}, "old"
	applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
	api.takeCalls()
	key := "demo/" + oldPrincipal

	exec.Inputs["groups"] = []any{"a", "b", "a"}
	reordered := saveAccessEntryPlan(t, exec)
	assert.Equal(t, runtime.DecisionNoOp, accessEntryPlanStep(t, reordered).Decision)
	applyAccessEntryPlan(t, exec, reordered)
	assert.Equal(t, []accessEntryCall{{"GET", key}}, api.takeCalls())

	exec.Inputs["groups"], exec.Inputs["username"] = []any{}, ""
	clear := saveAccessEntryPlan(t, exec)
	assert.Equal(t, runtime.DecisionUpdate, accessEntryPlanStep(t, clear).Decision)
	applyAccessEntryPlan(t, exec, clear)
	assert.Equal(t, []accessEntryCall{{"GET", key}, {"UPDATE", key}, {"GET", key}}, api.takeCalls())
	api.mu.Lock()
	body := api.updateBody
	api.mu.Unlock()
	assert.Regexp(t, `^[0-9a-f-]{36}$`, body["clientRequestToken"])
	assert.Equal(t, map[string]any{
		"kubernetesGroups": []any{}, "username": "", "clientRequestToken": body["clientRequestToken"],
	}, body)

	api.remove(key)
	exec.Destroy = true
	destroy := saveAccessEntryPlan(t, exec)
	applyAccessEntryPlan(t, exec, destroy)
	assert.Equal(t, []accessEntryCall{{"GET", key}, {"GET", key}}, api.takeCalls())
}

func TestAccessEntryMembershipDrift(t *testing.T) {
	api, endpoint := accessEntryEndpoint(t)
	exec := accessEntryExecutor(t, accessEntryLibrary(t), "evaluated", endpoint, nil)
	exec.Inputs["groups"], exec.Inputs["username"] = []any{"configured"}, "configured"
	applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
	api.takeCalls()

	key := "demo/" + oldPrincipal
	api.changeMembership(key, "remote", []string{"remote"})
	applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
	assert.Equal(t, []accessEntryCall{{"GET", key}, {"UPDATE", key}, {"GET", key}}, api.takeCalls())
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	outputs := snapshot.Find(entryAddress).Outputs
	assert.Equal(t, "configured", outputs["user-name"])
	assert.Equal(t, []any{"configured"}, outputs["kubernetes-groups"])

	exec.Inputs["groups"], exec.Inputs["username"] = nil, nil
	applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
	assert.Equal(t, []accessEntryCall{{"GET", key}, {"GET", key}}, api.takeCalls())
	api.changeMembership(key, "remote", []string{"remote"})
	applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
	assert.Equal(t, []accessEntryCall{{"GET", key}, {"GET", key}}, api.takeCalls())
	snapshot, err = exec.Store.Current()
	require.NoError(t, err)
	outputs = snapshot.Find(entryAddress).Outputs
	assert.Equal(t, "remote", outputs["user-name"])
	assert.Equal(t, []any{"remote"}, outputs["kubernetes-groups"])
}

func TestAccessEntryConfiguredTagDrift(t *testing.T) {
	api, endpoint := accessEntryEndpoint(t)
	exec := accessEntryExecutor(t, accessEntryLibrary(t), "evaluated", endpoint, nil)
	desired := map[string]any{"name": "configured", "empty": "", "same": "retained"}
	exec.Inputs["tags"] = desired
	applyAccessEntryPlan(t, exec, saveAccessEntryPlan(t, exec))
	key := "demo/" + oldPrincipal
	assert.Equal(t, []accessEntryCall{{"POST", key}, {"GET", key}}, api.takeCalls())

	api.mu.Lock()
	api.entries[key]["tags"] = map[string]any{
		"name": "changed", "extra": "remove", "same": "retained", "aws:owner": "service",
	}
	api.mu.Unlock()

	saved := saveAccessEntryPlan(t, exec)
	step := accessEntryPlanStep(t, saved)
	assert.Equal(t, runtime.DecisionUpdate, step.Decision)
	assert.Equal(t, desired, step.PriorInputs["tags"])
	assert.Equal(t, desired, step.Inputs["tags"])
	applyAccessEntryPlan(t, exec, saved)

	arn := "test-arn/" + key
	assert.Equal(t, []accessEntryCall{
		{"GET", key}, {"GET", arn}, {"DELETE", arn}, {"POST", arn}, {"GET", key},
	}, api.takeCalls())
	api.mu.Lock()
	assert.Equal(t, []string{"extra"}, api.tagRemovals)
	assert.Equal(t, map[string]map[string]string{"tags": {"name": "configured", "empty": ""}},
		api.tagBody)
	assert.Equal(t, map[string]any{
		"name": "configured", "empty": "", "same": "retained", "aws:owner": "service",
	}, api.entries[key]["tags"])
	api.mu.Unlock()
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	assert.Equal(t, desired, snapshot.Find(entryAddress).Outputs["tags"])
}

func TestAccessEntryNativeOperationTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	canceled := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/clusters/demo/access-entries", r.URL.Path)
		_, err := io.Copy(io.Discard, r.Body)
		assert.NoError(t, err)
		select {
		case <-r.Context().Done():
			canceled <- r.Context().Err()
		case <-ctx.Done():
		}
	})
	endpoint := accessEntryHTTPServer(t, handler)
	exec := accessEntryExecutor(t, accessEntryLibrary(t), "timeout", endpoint, nil)
	plan := saveAccessEntryPlan(t, exec)
	require.Equal(t, runtime.DecisionCreate, accessEntryPlanStep(t, plan).Decision)

	_, err := exec.ApplyPlan(ctx, plan)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "create EKS access entry demo/"+oldPrincipal)
	require.NoError(t, ctx.Err())
	select {
	case err := <-canceled:
		assert.ErrorIs(t, err, context.Canceled)
	case <-ctx.Done():
		t.Fatal("EKS request did not observe cancellation")
	}
}
