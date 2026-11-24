package eventbridge

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

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/eventbridge"
)

const (
	policyAddress  = "resource.policy"
	policyDocument = `{"Statement":[{"Action":["events:PutEvents","events:DescribeEventBus"],` +
		`"Effect":"Allow","Resource":"*"}]}`
	policyRequiredRule = `resource.policy: field "policy": required but is null`
	policyBusRule      = "resource.policy: schema: constraints[1] (predicate): " +
		"event-bus-name must be 1-256 ASCII letters, digits, slash, dot, underscore or hyphen"
	policyJSONRule = "resource.policy: schema: constraints[2] (predicate): policy must be valid JSON"
)

type busPolicyRequest struct {
	Operation string
	Body      map[string]any
}

type busPolicyAPI struct {
	mu             sync.Mutex
	calls          []busPolicyRequest
	policies       map[string]string
	reject         bool
	describes      int
	failDescribeAt int
	malformedAt    int
	regions        []string
	beforeDescribe func()
	deletedBuses   map[string]bool
}

func busPolicyEndpoint(t *testing.T) (string, *busPolicyAPI) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	api := &busPolicyAPI{policies: map[string]string{}, deletedBuses: map[string]bool{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		operation := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "AWSEvents.")
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		api.calls = append(api.calls, busPolicyRequest{operation, body})
		credential := strings.Split(r.Header.Get("Authorization"), "/")
		if len(credential) > 2 {
			api.regions = append(api.regions, credential[2])
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		bus, _ := body["EventBusName"].(string)
		switch operation {
		case "CreateEventBus":
			bus = body["Name"].(string)
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"EventBusArn": "arn:aws:events:us-east-1:123456789012:event-bus/" + bus}))
		case "DeleteEventBus":
			bus = body["Name"].(string)
			api.deletedBuses[bus] = true
			delete(api.policies, bus)
			_, _ = w.Write([]byte(`{}`))
		case "ListTagsForResource":
			_, _ = w.Write([]byte(`{"Tags":[]}`))
		case "PutPermission":
			if api.reject {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"__type":"MalformedPolicy","message":"rejected by API"}`))
				return
			}
			api.policies[bus] = body["Policy"].(string)
			_, _ = w.Write([]byte(`{}`))
		case "DescribeEventBus":
			api.describes++
			if api.beforeDescribe != nil {
				api.beforeDescribe()
			}
			if api.describes == api.failDescribeAt {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"__type":"AccessDeniedException","message":"lookup denied"}`))
				return
			}
			bus = body["Name"].(string)
			if api.deletedBuses[bus] {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException"}`))
				return
			}
			policy := api.policies[bus]
			if api.describes == api.malformedAt {
				policy = "broken remote JSON"
			}
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"Name": bus, "Policy": policy,
				"Arn": "arn:aws:events:us-east-1:123456789012:event-bus/" + bus}))
		case "RemovePermission":
			delete(api.policies, bus)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected operation %s", operation)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL, api
}

func busPolicyExecutor(t *testing.T, fixture string) *runtime.Executor {
	t.Helper()
	lib := runtime.LibraryWithPath(Library(),
		"github.com/cloudboss/unobin-library-aws//service/eventbridge")
	lib.Schema = readLibrarySchema(t)
	lib.Schema.Resources["event-bus-policy"].EvaluatedInputs =
		lib.Schema.Resources["event-bus-policy"].Inputs
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.event-bus-policy": lib.Schema.Resources["event-bus-policy"].Constraints,
	}
	lib.Defaults = map[string][]lang.DefaultSpec{
		"resource.event-bus-policy": lib.Schema.Resources["event-bus-policy"].Defaults,
	}
	libs := map[string]*runtime.Library{
		"eventbridge": lib,
		"test": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[putEventsSeed, map[string]any, *awscfg.Configuration](),
		}},
	}
	path := filepath.Join("testdata", "ub", "bus-policy", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	store, err := local.NewStore(t.TempDir(), "bus-policy", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "bus-policy", Version: "v0", ContentRevision: "test"}}
}

func policyPlanStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == policyAddress {
			return step
		}
	}
	t.Fatal("missing policy resource step")
	return runtime.PlanStep{}
}

func policyInputs(endpoint, bus, policy string) map[string]any {
	return map[string]any{"endpoint": endpoint, "bus": bus, "policy": policy, "trigger": "first"}
}

func (api *busPolicyAPI) takeCalls() []busPolicyRequest {
	api.mu.Lock()
	defer api.mu.Unlock()
	calls := api.calls
	api.calls = nil
	return calls
}

func (api *busPolicyAPI) policy(bus string) string {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.policies[bus]
}

func assertPolicyMutations(t *testing.T, api *busPolicyAPI, want []busPolicyRequest) {
	t.Helper()
	var mutations []busPolicyRequest
	for _, call := range api.takeCalls() {
		if call.Operation != "DescribeEventBus" && call.Operation != "ListTagsForResource" {
			mutations = append(mutations, call)
		}
	}
	assert.Equal(t, want, mutations)
}

func policyPut(bus, policy string) busPolicyRequest {
	return busPolicyRequest{"PutPermission", map[string]any{"EventBusName": bus, "Policy": policy}}
}
func policyRemove(bus string) busPolicyRequest {
	return busPolicyRequest{"RemovePermission", map[string]any{
		"EventBusName": bus, "RemoveAllPermissions": true}}
}

func assertPolicyBusExists(t *testing.T, endpoint, name string) {
	t.Helper()
	region := "us-east-1"
	bus := &svc.EventBusResource{Name: name}
	out, err := bus.Read(t.Context(),
		&awscfg.Configuration{Region: &region, EndpointURL: &endpoint},
		runtime.Prior[svc.EventBusResource, *svc.EventBusResourceOutput, *awscfg.Configuration]{})
	require.NoError(t, err)
	assert.Equal(t, &svc.EventBusResourceOutput{
		Name: name, Arn: "arn:aws:events:us-east-1:123456789012:event-bus/" + name,
	}, out)
}

func TestEventBusPolicyNativeLifecycle(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	executor := busPolicyExecutor(t, "default")
	executor.Inputs = policyInputs(endpoint, "unused", policyDocument)
	created := savedEventActionPlan(t, executor)
	require.Equal(t, runtime.DecisionCreate, policyPlanStep(t, created).Decision)
	assert.Empty(t, policyPlanStep(t, created).SensitiveInputs)
	assert.Empty(t, policyPlanStep(t, created).SensitiveOutputs)
	executor.Inputs = nil
	_, err := executor.ApplyPlan(t.Context(), created)
	require.NoError(t, err)
	assert.Equal(t, []busPolicyRequest{
		policyPut("default", policyDocument),
		{"DescribeEventBus", map[string]any{"Name": "default"}},
		{"DescribeEventBus", map[string]any{"Name": "default"}},
	}, api.takeCalls())
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"name": "default", "policy": policyDocument},
		snapshot.Find(policyAddress).Outputs)
	assert.Equal(t, 1, snapshot.Find(policyAddress).SchemaVersion)
	executor.Inputs = created.Inputs
	unchanged := savedEventActionPlan(t, executor)
	require.Equal(t, runtime.DecisionNoOp, policyPlanStep(t, unchanged).Decision)
	_, err = executor.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	assertPolicyMutations(t, api, nil)
	executor.Inputs["policy"] = `{"Statement":[]}`
	updated := savedEventActionPlan(t, executor)
	require.Equal(t, runtime.DecisionUpdate, policyPlanStep(t, updated).Decision)
	_, err = executor.ApplyPlan(t.Context(), updated)
	require.NoError(t, err)
	assertPolicyMutations(t, api, []busPolicyRequest{policyPut("default", `{"Statement":[]}`)})
	executor.Destroy = true
	destroyed := savedEventActionPlan(t, executor)
	_, err = executor.ApplyPlan(t.Context(), destroyed)
	require.NoError(t, err)
	assertPolicyMutations(t, api, []busPolicyRequest{policyRemove("default")})
	snapshot, err = executor.Store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find(policyAddress))
}

func TestEventBusPolicyNativeInvalidAndSavedNoOp(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	executor := busPolicyExecutor(t, "evaluated")
	executor.Inputs = policyInputs(endpoint, "old-bus", policyDocument)
	created := savedEventActionPlan(t, executor)
	_, err := executor.ApplyPlan(t.Context(), created)
	require.NoError(t, err)
	assertPolicyMutations(t, api, []busPolicyRequest{policyPut("old-bus", policyDocument)})
	for _, tt := range []struct {
		name, field string
		value       any
		rule        string
	}{
		{"missing policy", "policy", nil, policyRequiredRule},
		{"empty JSON", "policy", "", policyJSONRule},
		{"broken JSON", "policy", "broken", policyJSONRule},
		{"null bus", "bus", nil, `resource.policy: field "event-bus-name": required but is null`},
		{"empty bus", "bus", "", policyBusRule},
		{"long bus", "bus", strings.Repeat("a", 257), policyBusRule},
		{"space bus", "bus", "a b", policyBusRule},
		{"colon bus", "bus", "arn:aws:events:us-east-1:123456789012:event-bus/a", policyBusRule},
		{"Unicode bus", "bus", "é", policyBusRule},
	} {
		t.Run(tt.name, func(t *testing.T) {
			executor.Inputs = policyInputs(endpoint, "old-bus", policyDocument)
			saved := savedEventActionPlan(t, executor)
			require.Equal(t, runtime.DecisionNoOp, policyPlanStep(t, saved).Decision)
			executor.Inputs[tt.field] = tt.value
			_, err := executor.Plan(t.Context())
			require.ErrorContains(t, err, tt.rule)
			_, err = executor.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, tt.rule)
			assertPolicyMutations(t, api, nil)
		})
	}
	executor.Inputs = policyInputs(endpoint, "old-bus", policyDocument)
	executor.Inputs["policy"] = `{"Statement":{"Effect":"Allow","Resource":["*"],` +
		`"Action":["events:DescribeEventBus","events:PutEvents"]}}`
	equivalent := savedEventActionPlan(t, executor)
	require.Equal(t, runtime.DecisionNoOp, policyPlanStep(t, equivalent).Decision)
	_, err = executor.ApplyPlan(t.Context(), equivalent)
	require.NoError(t, err)
	assertPolicyMutations(t, api, nil)
	executor.Inputs = policyInputs(endpoint, "new-bus", policyDocument)
	replacement := savedEventActionPlan(t, executor)
	assert.Equal(t, []busPolicyRequest{
		{"DescribeEventBus", map[string]any{"Name": "old-bus"}},
	}, api.takeCalls())
	step := policyPlanStep(t, replacement)
	require.Equal(t, runtime.DecisionReplace, step.Decision)
	assert.Equal(t, []string{"event-bus-name"}, step.ReplacementReasons)
	_, err = executor.ApplyPlan(t.Context(), replacement)
	require.NoError(t, err)
	assertPolicyMutations(t, api, []busPolicyRequest{
		policyRemove("old-bus"), policyPut("new-bus", policyDocument)})
}

func TestEventBusPolicyNativeDeferred(t *testing.T) {
	for _, stage := range []string{"create", "unchanged", "update", "replace"} {
		for _, tt := range []struct {
			name, field string
			value       any
			rule        string
		}{
			{name: "valid"},
			{"malformed policy", "policy", "broken", policyJSONRule},
			{"null policy", "policy", nil, policyRequiredRule},
			{"invalid bus", "bus", "invalid:bus", policyBusRule},
			{"null bus", "bus", nil, `resource.policy: field "event-bus-name": required but is null`},
		} {
			t.Run(stage+" "+tt.name, func(t *testing.T) {
				endpoint, api := busPolicyEndpoint(t)
				executor := busPolicyExecutor(t, "deferred")
				executor.Inputs = policyInputs(endpoint, "old-bus", policyDocument)
				if stage != "create" {
					first := savedEventActionPlan(t, executor)
					_, err := executor.ApplyPlan(t.Context(), first)
					require.NoError(t, err)
					assertPolicyMutations(t, api, []busPolicyRequest{policyPut("old-bus", policyDocument)})
				}
				executor.Inputs["trigger"] = "second"
				if stage != "unchanged" {
					executor.Inputs["policy"] = `{"Statement":[]}`
				}
				if stage == "replace" {
					executor.Inputs["bus"] = "new-bus"
				}
				if tt.rule != "" {
					executor.Inputs[tt.field] = tt.value
				}
				saved := savedEventActionPlan(t, executor)
				assert.Equal(t, map[string][]string{
					"event-bus-name": {"action.seed.value.bus"}, "policy": {"action.seed.value.policy"},
				}, policyPlanStep(t, saved).UnresolvedInputs)
				executor.Inputs = nil
				_, err := executor.ApplyPlan(t.Context(), saved)
				if tt.rule != "" {
					require.ErrorContains(t, err, tt.rule)
					assertPolicyMutations(t, api, nil)
					if stage != "create" {
						assert.Equal(t, policyDocument, api.policy("old-bus"))
					}
				} else {
					require.NoError(t, err)
					want := []busPolicyRequest{policyPut("old-bus", `{"Statement":[]}`)}
					if stage == "unchanged" {
						want = nil
					}
					if stage == "replace" {
						want = []busPolicyRequest{
							policyRemove("old-bus"), policyPut("new-bus", `{"Statement":[]}`)}
					}
					assertPolicyMutations(t, api, want)
				}
			})
		}
	}
}

func TestEventBusPolicySyntaxOnlyAndBusBoundaries(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	executor := busPolicyExecutor(t, "evaluated")
	for _, bus := range []string{"a", strings.Repeat("z", 256), "aws.partner/example.com/a-b_c.d"} {
		executor.Inputs = policyInputs(endpoint, bus, policyDocument)
		plan := savedEventActionPlan(t, executor)
		require.Equal(t, runtime.DecisionCreate, policyPlanStep(t, plan).Decision)
	}
	api.reject = true
	for _, policy := range []string{`null`, `true`, `3`, `"text"`, `[]`, `{"a":1,"a":2}`} {
		executor.Inputs = policyInputs(endpoint, "bus", policy)
		plan := savedEventActionPlan(t, executor)
		_, err := executor.ApplyPlan(t.Context(), plan)
		require.ErrorContains(t, err, "rejected by API")
		normalized := policy
		if policy == `{"a":1,"a":2}` {
			normalized = `{"a":2}`
		}
		assertPolicyMutations(t, api, []busPolicyRequest{policyPut("bus", normalized)})
	}
}

func TestEventBusPolicyHistoricalDestroy(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	executor := busPolicyExecutor(t, "evaluated")
	executor.Inputs = policyInputs(endpoint, "persisted-bus", policyDocument)
	first := savedEventActionPlan(t, executor)
	_, err := executor.ApplyPlan(t.Context(), first)
	require.NoError(t, err)
	assertPolicyMutations(t, api, []busPolicyRequest{policyPut("persisted-bus", policyDocument)})
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	entry := snapshot.Find(policyAddress)
	entry.Inputs["event-bus-name"] = "invalid:historical"
	entry.Inputs["policy"] = "broken historical JSON"
	rev, err := executor.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(rev))
	executor.Inputs["policy"] = "broken desired JSON"
	executor.Inputs["bus"] = "invalid:desired"
	executor.Destroy = true
	destroyed := savedEventActionPlan(t, executor)
	require.Equal(t, runtime.DecisionDestroy, policyPlanStep(t, destroyed).Decision)
	_, err = executor.ApplyPlan(t.Context(), destroyed)
	require.NoError(t, err)
	assertPolicyMutations(t, api, []busPolicyRequest{policyRemove("persisted-bus")})
}

func TestEventBusPolicyInvalidOtherwiseUnchanged(t *testing.T) {
	for _, tt := range []struct{ field, value, rule string }{
		{"policy", "", policyJSONRule},
		{"event-bus-name", "invalid:bus", policyBusRule},
	} {
		t.Run(tt.field, func(t *testing.T) {
			endpoint, api := busPolicyEndpoint(t)
			executor := busPolicyExecutor(t, "evaluated")
			executor.Inputs = policyInputs(endpoint, "persisted-bus", policyDocument)
			first := savedEventActionPlan(t, executor)
			_, err := executor.ApplyPlan(t.Context(), first)
			require.NoError(t, err)
			assertPolicyMutations(t, api, []busPolicyRequest{policyPut("persisted-bus", policyDocument)})
			snapshot, err := executor.Store.Current()
			require.NoError(t, err)
			snapshot.Find(policyAddress).Inputs[tt.field] = tt.value
			rev, err := executor.Store.Write(snapshot)
			require.NoError(t, err)
			require.NoError(t, executor.Store.SetCurrent(rev))
			input := tt.field
			if input == "event-bus-name" {
				input = "bus"
			}
			executor.Inputs[input] = tt.value
			_, err = executor.Plan(t.Context())
			require.ErrorContains(t, err, tt.rule)
			assertPolicyMutations(t, api, nil)
		})
	}
}

func TestEventBusPolicyPersistedIdentity(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	api.mu.Lock()
	api.policies["prior-bus"] = policyDocument
	api.mu.Unlock()
	region := "us-east-1"
	cfg := &awscfg.Configuration{Region: &region, EndpointURL: &endpoint}
	r := &svc.EventBusPolicyResource{EventBusName: "desired-bus", Policy: `{}`}
	prior := runtime.Prior[svc.EventBusPolicyResource, *svc.EventBusPolicyResourceOutput,
		*awscfg.Configuration]{Outputs: &svc.EventBusPolicyResourceOutput{Name: "prior-bus"}}
	out, err := r.Read(t.Context(), cfg, prior)
	require.NoError(t, err)
	assert.Equal(t, &svc.EventBusPolicyResourceOutput{Name: "prior-bus", Policy: policyDocument}, out)
	out, err = r.Update(t.Context(), cfg, prior)
	require.NoError(t, err)
	assert.Equal(t, &svc.EventBusPolicyResourceOutput{Name: "prior-bus", Policy: `{}`}, out)
	require.NoError(t, r.Delete(t.Context(), cfg, prior))
	assert.Equal(t, []busPolicyRequest{
		{"DescribeEventBus", map[string]any{"Name": "prior-bus"}},
		policyPut("prior-bus", `{}`),
		{"DescribeEventBus", map[string]any{"Name": "prior-bus"}},
		{"DescribeEventBus", map[string]any{"Name": "prior-bus"}},
		policyRemove("prior-bus"),
	}, api.takeCalls())
}

func TestEventBusPolicyManagedFixtureLifecycle(t *testing.T) {
	for _, failPut := range []bool{false, true} {
		name := map[bool]string{false: "success", true: "failed policy apply"}[failPut]
		t.Run(name, func(t *testing.T) {
			endpoint, api := busPolicyEndpoint(t)
			api.reject = failPut
			executor := busPolicyExecutor(t, "managed-bus")
			executor.Inputs = map[string]any{
				"endpoint": endpoint, "policy": policyDocument, "policies": map[string]any{"managed": true},
			}
			first := savedEventActionPlan(t, executor)
			_, err := executor.ApplyPlan(t.Context(), first)
			if failPut {
				require.ErrorContains(t, err, "rejected by API")
			} else {
				require.NoError(t, err)
			}
			snapshot, err := executor.Store.Current()
			require.NoError(t, err)
			require.NotNil(t, snapshot.Find("resource.bus"), "fixture bus must remain managed after apply")
			api.takeCalls()
			if !failPut {
				executor.Inputs["policies"] = map[string]any{}
				removed := savedEventActionPlan(t, executor)
				_, err = executor.ApplyPlan(t.Context(), removed)
				require.NoError(t, err)
				assertPolicyMutations(t, api, []busPolicyRequest{policyRemove("fixture-bus")})
				snapshot, err = executor.Store.Current()
				require.NoError(t, err)
				require.NotNil(t, snapshot.Find("resource.bus"))
				assert.Nil(t, snapshot.Find("resource.policy['managed']"))
				assert.Empty(t, api.policy("fixture-bus"))
			}
			executor.Inputs["policies"] = map[string]any{"managed": true}
			executor.Destroy = true
			destroyed := savedEventActionPlan(t, executor)
			_, err = executor.ApplyPlan(t.Context(), destroyed)
			require.NoError(t, err)
			assertPolicyMutations(t, api, []busPolicyRequest{
				{"DeleteEventBus", map[string]any{"Name": "fixture-bus"}},
			})
			snapshot, err = executor.Store.Current()
			require.NoError(t, err)
			assert.Nil(t, snapshot.Find("resource.bus"))
		})
	}
}

func TestEventBusPolicyManagedFixtureFailedUpdateCleanup(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	executor := busPolicyExecutor(t, "managed-bus")
	executor.Inputs = map[string]any{
		"endpoint": endpoint, "policy": policyDocument, "policies": map[string]any{"managed": true},
	}
	first := savedEventActionPlan(t, executor)
	_, err := executor.ApplyPlan(t.Context(), first)
	require.NoError(t, err)
	api.takeCalls()
	api.mu.Lock()
	api.reject = true
	api.mu.Unlock()
	executor.Inputs["policy"] = `{"Statement":[]}`
	update := savedEventActionPlan(t, executor)
	_, err = executor.ApplyPlan(t.Context(), update)
	require.ErrorContains(t, err, "rejected by API")
	assertPolicyMutations(t, api, []busPolicyRequest{policyPut("fixture-bus", `{"Statement":[]}`)})
	executor.Destroy = true
	destroy := savedEventActionPlan(t, executor)
	_, err = executor.ApplyPlan(t.Context(), destroy)
	require.NoError(t, err)
	assertPolicyMutations(t, api, []busPolicyRequest{
		policyRemove("fixture-bus"), {"DeleteEventBus", map[string]any{"Name": "fixture-bus"}},
	})
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	assert.Empty(t, snapshot.Entries)
}

func TestEventBusPolicyNativeAcceptedCreateFailure(t *testing.T) {
	for _, stage := range []string{"visibility", "final read", "normalization", "rejected"} {
		t.Run(stage, func(t *testing.T) {
			endpoint, api := busPolicyEndpoint(t)
			api.policies["external-bus"] = `{"external":true}`
			switch stage {
			case "visibility":
				api.failDescribeAt = 1
			case "final read":
				api.failDescribeAt = 2
			case "normalization":
				api.malformedAt = 2
			case "rejected":
				api.reject = true
			}
			executor := busPolicyExecutor(t, "evaluated")
			executor.Inputs = policyInputs(endpoint, "external-bus", policyDocument)
			_, err := executor.ApplyPlan(t.Context(), savedEventActionPlan(t, executor))
			require.Error(t, err)
			want := []busPolicyRequest{policyPut("external-bus", policyDocument)}
			if stage == "rejected" {
				assert.Equal(t, `{"external":true}`, api.policy("external-bus"))
			} else {
				want = append(want, policyRemove("external-bus"))
				assert.Empty(t, api.policy("external-bus"))
			}
			assertPolicyMutations(t, api, want)
			snapshot, err := executor.Store.Current()
			require.ErrorIs(t, err, state.ErrNoCurrent)
			assert.Nil(t, snapshot)
			assertPolicyBusExists(t, endpoint, "external-bus")
		})
	}
}

func TestEventBusPolicyNativeAcceptedUpdateFailure(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	executor := busPolicyExecutor(t, "evaluated")
	executor.Inputs = policyInputs(endpoint, "external-bus", policyDocument)
	_, err := executor.ApplyPlan(t.Context(), savedEventActionPlan(t, executor))
	require.NoError(t, err)
	api.takeCalls()
	executor.Inputs["policy"] = `{"Statement":[]}`
	saved := savedEventActionPlan(t, executor)
	api.failDescribeAt = api.describes + 1
	_, err = executor.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "lookup denied")
	assertPolicyMutations(t, api, []busPolicyRequest{policyPut("external-bus", `{"Statement":[]}`)})
	assert.Equal(t, `{"Statement":[]}`, api.policy("external-bus"))
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	require.NotNil(t, snapshot.Find(policyAddress))
}

func TestEventBusPolicyPreparationBeforeReplacementDelete(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	executor := busPolicyExecutor(t, "deferred")
	executor.Inputs = policyInputs(endpoint, "old-bus", policyDocument)
	_, err := executor.ApplyPlan(t.Context(), savedEventActionPlan(t, executor))
	require.NoError(t, err)
	api.takeCalls()
	executor.Inputs["trigger"] = "second"
	executor.Inputs["bus"] = "new-bus"
	executor.Inputs["policy"] = "1e1000"
	saved := savedEventActionPlan(t, executor)
	assert.Equal(t, map[string][]string{
		"event-bus-name": {"action.seed.value.bus"}, "policy": {"action.seed.value.policy"},
	}, policyPlanStep(t, saved).UnresolvedInputs)
	_, err = executor.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "prepare event bus policy")
	assertPolicyMutations(t, api, nil)
	assert.Equal(t, policyDocument, api.policy("old-bus"))
}

func TestEventBusPolicyNativeRegionalReplacement(t *testing.T) {
	oldEndpoint, oldAPI := busPolicyEndpoint(t)
	newEndpoint, newAPI := busPolicyEndpoint(t)
	executor := busPolicyExecutor(t, "regional")
	executor.Inputs = policyInputs(oldEndpoint, "bus", policyDocument)
	executor.Inputs["region"] = "us-east-1"
	_, err := executor.ApplyPlan(t.Context(), savedEventActionPlan(t, executor))
	require.NoError(t, err)
	oldAPI.takeCalls()
	executor.Inputs["endpoint"], executor.Inputs["region"] = newEndpoint, "us-west-2"
	saved := savedEventActionPlan(t, executor)
	step := policyPlanStep(t, saved)
	require.Equal(t, runtime.DecisionReplace, step.Decision)
	assert.Equal(t, []string{"region"}, step.ReplacementReasons)
	assert.Equal(t, []busPolicyRequest{
		{"DescribeEventBus", map[string]any{"Name": "bus"}},
	}, oldAPI.takeCalls())
	assert.Empty(t, newAPI.takeCalls())
	executor.Inputs = nil
	_, err = executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Equal(t, []busPolicyRequest{
		{"DescribeEventBus", map[string]any{"Name": "bus"}}, policyRemove("bus"),
	}, oldAPI.takeCalls())
	assert.Equal(t, []busPolicyRequest{policyPut("bus", policyDocument),
		{"DescribeEventBus", map[string]any{"Name": "bus"}},
		{"DescribeEventBus", map[string]any{"Name": "bus"}},
	}, newAPI.takeCalls())
	assert.Equal(t, []string{"us-east-1", "us-east-1", "us-east-1", "us-east-1",
		"us-east-1", "us-east-1"}, oldAPI.regions)
	assert.Equal(t, []string{"us-west-2", "us-west-2", "us-west-2"}, newAPI.regions)
}

func TestEventBusPolicyExistingConfiguration(t *testing.T) {
	for _, saved := range []string{"explicit region", "SDK region", "absent"} {
		t.Run(saved, func(t *testing.T) {
			oldEndpoint, oldAPI := busPolicyEndpoint(t)
			newEndpoint, newAPI := busPolicyEndpoint(t)
			t.Setenv("AWS_REGION", "us-east-2")
			region, newRegion := "us-east-1", "us-west-2"
			prior := runtime.Prior[svc.EventBusPolicyResource, *svc.EventBusPolicyResourceOutput,
				*awscfg.Configuration]{Outputs: &svc.EventBusPolicyResourceOutput{Name: "persisted"}}
			cfg := &awscfg.Configuration{Region: &newRegion, EndpointURL: &newEndpoint}
			wantAPI, unusedAPI, signingRegion := oldAPI, newAPI, region
			if saved == "absent" {
				wantAPI, unusedAPI, signingRegion = newAPI, oldAPI, newRegion
			} else {
				prior.Configuration = &awscfg.Configuration{Region: &region, EndpointURL: &oldEndpoint}
				if saved == "SDK region" {
					prior.Configuration.Region = nil
					signingRegion = "us-east-2"
				}
			}
			wantAPI.policies["persisted"] = policyDocument
			r := &svc.EventBusPolicyResource{EventBusName: "desired", Policy: `{}`}
			_, err := r.Read(t.Context(), cfg, prior)
			require.NoError(t, err)
			_, err = r.Update(t.Context(), cfg, prior)
			require.NoError(t, err)
			require.NoError(t, r.Delete(t.Context(), cfg, prior))
			assert.Equal(t, []busPolicyRequest{
				{"DescribeEventBus", map[string]any{"Name": "persisted"}},
				policyPut("persisted", `{}`),
				{"DescribeEventBus", map[string]any{"Name": "persisted"}},
				{"DescribeEventBus", map[string]any{"Name": "persisted"}},
				policyRemove("persisted"),
			}, wantAPI.takeCalls())
			assert.Empty(t, unusedAPI.takeCalls())
			assert.Equal(t, []string{signingRegion, signingRegion, signingRegion,
				signingRegion, signingRegion}, wantAPI.regions)
		})
	}
}

func TestEventBusPolicyNativeCanceledCreate(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	api.policies["external-bus"] = `{"external":true}`
	executor := busPolicyExecutor(t, "evaluated")
	executor.Inputs = policyInputs(endpoint, "external-bus", policyDocument)
	saved := savedEventActionPlan(t, executor)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	api.beforeDescribe = cancel
	_, err := executor.ApplyPlan(ctx, saved)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assertPolicyMutations(t, api, []busPolicyRequest{
		policyPut("external-bus", policyDocument), policyRemove("external-bus"),
	})
	assert.Empty(t, api.policy("external-bus"))
	snapshot, err := executor.Store.Current()
	require.ErrorIs(t, err, state.ErrNoCurrent)
	assert.Nil(t, snapshot)
	assertPolicyBusExists(t, endpoint, "external-bus")
}

func TestEventBusPolicyExplicitNullPredicate(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	executor := busPolicyExecutor(t, "evaluated")
	executor.Libraries["eventbridge"].Schema.Resources["event-bus-policy"].EvaluatedInputs = nil
	executor.Inputs = policyInputs(endpoint, "bus", policyDocument)
	executor.Inputs["policy"] = nil
	_, err := executor.Plan(t.Context())
	require.ErrorContains(t, err,
		"resource.policy: schema: constraints[0] (predicate): policy is required")
	assertPolicyMutations(t, api, nil)
}

func TestEventBusPolicyNativeRegionalFailedReplacement(t *testing.T) {
	oldEndpoint, oldAPI := busPolicyEndpoint(t)
	newEndpoint, newAPI := busPolicyEndpoint(t)
	executor := busPolicyExecutor(t, "regional")
	executor.Inputs = policyInputs(oldEndpoint, "old-bus", policyDocument)
	executor.Inputs["region"] = "us-east-1"
	_, err := executor.ApplyPlan(t.Context(), savedEventActionPlan(t, executor))
	require.NoError(t, err)
	oldAPI.takeCalls()
	newAPI.policies["new-bus"] = `{"external":true}`
	newAPI.failDescribeAt = 1
	executor.Inputs["endpoint"], executor.Inputs["region"] = newEndpoint, "us-west-2"
	executor.Inputs["bus"] = "new-bus"
	saved := savedEventActionPlan(t, executor)
	assert.Equal(t, []string{"event-bus-name", "region"},
		policyPlanStep(t, saved).ReplacementReasons)
	_, err = executor.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "lookup denied")
	assertPolicyMutations(t, oldAPI, []busPolicyRequest{policyRemove("old-bus")})
	assertPolicyMutations(t, newAPI, []busPolicyRequest{
		policyPut("new-bus", policyDocument), policyRemove("new-bus"),
	})
	assert.Empty(t, oldAPI.policy("old-bus"))
	assert.Empty(t, newAPI.policy("new-bus"))
}

func TestEventBusPolicyNativeExternalOwnership(t *testing.T) {
	endpoint, api := busPolicyEndpoint(t)
	api.policies["external-bus"] = `{"external":true}`
	executor := busPolicyExecutor(t, "evaluated")
	executor.Inputs = policyInputs(endpoint, "external-bus", policyDocument)
	_, err := executor.ApplyPlan(t.Context(), savedEventActionPlan(t, executor))
	require.NoError(t, err)
	assertPolicyMutations(t, api, []busPolicyRequest{policyPut("external-bus", policyDocument)})
	assert.Equal(t, policyDocument, api.policy("external-bus"))
	api.mu.Lock()
	api.policies["external-bus"] = `{"Statement":[{"Effect":"Allow","Action":"events:*",` +
		`"Resource":"*"}]}`
	api.mu.Unlock()
	saved := savedEventActionPlan(t, executor)
	require.Equal(t, runtime.DecisionUpdate, policyPlanStep(t, saved).Decision)
	_, err = executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assertPolicyMutations(t, api, []busPolicyRequest{policyPut("external-bus", policyDocument)})
	assert.Equal(t, policyDocument, api.policy("external-bus"))
	api.mu.Lock()
	api.policies["external-bus"] = `{"external":true}`
	api.mu.Unlock()
	executor.Destroy = true
	_, err = executor.ApplyPlan(t.Context(), savedEventActionPlan(t, executor))
	require.NoError(t, err)
	assertPolicyMutations(t, api, []busPolicyRequest{policyRemove("external-bus")})
	assert.Empty(t, api.policy("external-bus"))
	assertPolicyBusExists(t, endpoint, "external-bus")
}

func TestEventBusPolicyHistoricalConfigurationDestroy(t *testing.T) {
	for _, missing := range []string{"configuration", "region"} {
		t.Run(missing, func(t *testing.T) {
			oldEndpoint, oldAPI := busPolicyEndpoint(t)
			newEndpoint, newAPI := busPolicyEndpoint(t)
			t.Setenv("AWS_REGION", "us-east-2")
			executor := busPolicyExecutor(t, "regional")
			executor.Inputs = policyInputs(oldEndpoint, "persisted", policyDocument)
			executor.Inputs["region"] = "us-east-1"
			_, err := executor.ApplyPlan(t.Context(), savedEventActionPlan(t, executor))
			require.NoError(t, err)
			oldAPI.takeCalls()
			oldAPI.regions = nil
			t.Setenv("AWS_ENDPOINT_URL", newEndpoint)
			newAPI.policies["persisted"] = policyDocument
			snapshot, err := executor.Store.Current()
			require.NoError(t, err)
			entry := snapshot.Find(policyAddress)
			if missing == "configuration" {
				entry.Configuration = nil
			} else {
				delete(entry.Configuration, "region")
			}
			revision, err := executor.Store.Write(snapshot)
			require.NoError(t, err)
			require.NoError(t, executor.Store.SetCurrent(revision))
			executor.Inputs["endpoint"], executor.Inputs["region"] = newEndpoint, "us-west-2"
			executor.Destroy = true
			_, err = executor.ApplyPlan(t.Context(), savedEventActionPlan(t, executor))
			require.NoError(t, err)
			if missing == "configuration" {
				assertPolicyMutations(t, newAPI, []busPolicyRequest{policyRemove("persisted")})
				assert.Empty(t, oldAPI.takeCalls())
				assert.Equal(t, []string{"us-east-2", "us-east-2", "us-east-2"}, newAPI.regions)
			} else {
				assertPolicyMutations(t, oldAPI, []busPolicyRequest{policyRemove("persisted")})
				assert.Empty(t, newAPI.takeCalls())
				assert.Equal(t, []string{"us-east-2", "us-east-2", "us-east-2"}, oldAPI.regions)
			}
			snapshot, err = executor.Store.Current()
			require.NoError(t, err)
			assert.Nil(t, snapshot.Find(policyAddress))
		})
	}
}
