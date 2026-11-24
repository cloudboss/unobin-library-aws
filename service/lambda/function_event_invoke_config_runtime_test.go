package lambda

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const eventInvokeAddress = "resource.settings"

type eventInvokeCall struct {
	method    string
	path      string
	qualifier string
	body      map[string]any
}

type eventInvokeReply struct {
	status int
	body   string
}

type eventInvokeAPI struct {
	t      *testing.T
	region string
	server *httptest.Server
	mu     sync.Mutex
	calls  []eventInvokeCall
	get    eventInvokeReply
	put    eventInvokeReply
	del    eventInvokeReply
}

func newEventInvokeAPI(t *testing.T) *eventInvokeAPI {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing-credentials"))
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	api := &eventInvokeAPI{t: t, region: "us-east-1"}
	api.setGet(http.StatusOK, "{}")
	api.put = eventInvokeReply{http.StatusOK, `{"MaximumRetryAttempts":1}`}
	api.del = eventInvokeReply{http.StatusNoContent, ""}
	api.server = httptest.NewServer(http.HandlerFunc(api.serve))
	t.Cleanup(api.server.Close)
	return api
}

func (a *eventInvokeAPI) setGet(status int, body string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.get = eventInvokeReply{status, body}
}

func (a *eventInvokeAPI) takeCalls() []eventInvokeCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	calls := a.calls
	a.calls = nil
	return calls
}

func (a *eventInvokeAPI) serve(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		a.t.Errorf("read API request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	call := eventInvokeCall{
		method: req.Method, path: req.URL.Path, qualifier: req.URL.Query().Get("Qualifier"),
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &call.body); err != nil {
			a.t.Errorf("decode API request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	scope := strings.SplitN(req.Header.Get("Authorization"), "/", 4)
	if assert.Len(a.t, scope, 4, "SigV4 credential scope") {
		assert.Equal(a.t, a.region, scope[2])
	}
	a.calls = append(a.calls, call)
	var reply eventInvokeReply
	switch req.Method {
	case http.MethodGet:
		reply = a.get
	case http.MethodPut:
		reply = a.put
	case http.MethodDelete:
		reply = a.del
	case http.MethodPost:
		reply = eventInvokeReply{http.StatusOK, "{}"}
	default:
		a.t.Errorf("unexpected API request: %s %s", req.Method, req.URL)
		reply.status = http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(reply.status)
	if _, err := io.WriteString(w, reply.body); err != nil {
		a.t.Errorf("write API response: %v", err)
	}
}

func eventInvokeRequest(method, name, qualifier string, body map[string]any) eventInvokeCall {
	return eventInvokeCall{
		method: method, path: "/2019-09-25/functions/" + name + "/event-invoke-config",
		qualifier: qualifier, body: body,
	}
}

func eventInvokeExecutor(
	t *testing.T, fixture string, api *eventInvokeAPI, store state.Backend,
) *runtime.Executor {
	t.Helper()
	body, checked, err := analyzeEventInvokeFixture(t, fixture)
	require.NoError(t, err)
	if store == nil {
		store, err = local.NewStore(t.TempDir(), "test", "test", encrypters.Noop{})
		require.NoError(t, err)
	}
	lib := Library()
	lib.Schema = checked.Imports.Libraries["lambda"].Schema
	lib.Constraints = map[string][]lang.ConstraintSpec{}
	lib.Defaults = map[string][]lang.DefaultSpec{}
	for name, schema := range lib.Schema.Resources {
		lib.Constraints["resource."+name] = schema.Constraints
		lib.Defaults["resource."+name] = schema.Defaults
	}
	for name, schema := range lib.Schema.Actions {
		lib.Constraints["action."+name] = schema.Constraints
		lib.Defaults["action."+name] = schema.Defaults
	}
	return &runtime.Executor{
		DAG: checked.DAG, SyntaxSource: body, Store: store,
		Libraries: map[string]*runtime.Library{"lambda": lib},
		Inputs: map[string]any{
			"endpoint": api.server.URL, "name": "fn", "region": nil,
			"qualifier": nil, "event-age": nil, "retries": int64(2),
			"on-failure": nil, "on-success": nil,
		},
	}
}

func eventInvokePlanStep(t *testing.T, steps []runtime.PlanStep) runtime.PlanStep {
	t.Helper()
	for _, step := range steps {
		if step.Address == eventInvokeAddress {
			return step
		}
	}
	t.Fatalf("no plan step for %s", eventInvokeAddress)
	return runtime.PlanStep{}
}

func saveEventInvokePlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	src, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(src)
	require.NoError(t, err)
	return saved
}

func applyEventInvokePlan(
	t *testing.T, exec *runtime.Executor, decision runtime.Decision,
) *runtime.PlanFile {
	t.Helper()
	saved := saveEventInvokePlan(t, exec)
	require.Equal(t, decision, eventInvokePlanStep(t, saved.Steps).Decision)
	_, err := exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	return saved
}

func TestEventInvokeNativeEvaluatedInputs(t *testing.T) {
	api := newEventInvokeAPI(t)
	exec := eventInvokeExecutor(t, "runtime", api, nil)
	tests := []struct {
		field      string
		value      any
		diagnostic string
	}{
		{"name", "", "function-name must not be empty"},
		{"qualifier", "", "qualifier must not be empty"},
		{"event-age", int64(59), "maximum-event-age-in-seconds must be between 60 and 21600"},
		{"retries", int64(3), "maximum-retry-attempts must be between 0 and 2"},
		{
			"on-failure", map[string]any{},
			"destination-config.on-failure.destination is required",
		},
		{
			"on-success", map[string]any{},
			"destination-config.on-success.destination is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			old, exists := exec.Inputs[tt.field]
			exec.Inputs[tt.field] = tt.value
			t.Cleanup(func() {
				if exists {
					exec.Inputs[tt.field] = old
				} else {
					delete(exec.Inputs, tt.field)
				}
			})
			_, err := exec.Plan(t.Context())
			require.ErrorContains(t, err, eventInvokeAddress+": schema: constraints[")
			require.ErrorContains(t, err, tt.diagnostic)
			assert.Empty(t, api.takeCalls())
		})
	}
	exec.Inputs["on-success"] = map[string]any{"destination": ""}
	applyEventInvokePlan(t, exec, runtime.DecisionCreate)
	assert.Equal(t, []eventInvokeCall{
		eventInvokeRequest("PUT", "fn", "", map[string]any{
			"MaximumRetryAttempts": float64(2),
			"DestinationConfig": map[string]any{
				"OnSuccess": map[string]any{"Destination": ""},
			},
		}),
		eventInvokeRequest("GET", "fn", "", nil),
	}, api.takeCalls())
}

func TestEventInvokeNativeNoOpAndHistoricalDestroy(t *testing.T) {
	api := newEventInvokeAPI(t)
	exec := eventInvokeExecutor(t, "runtime", api, nil)
	applyEventInvokePlan(t, exec, runtime.DecisionCreate)
	api.takeCalls()

	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	snapshot.Find(eventInvokeAddress).Inputs["maximum-retry-attempts"] = int64(-1)
	rev, err := exec.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, exec.Store.SetCurrent(rev))
	exec.Inputs["retries"] = int64(-1)

	lib := exec.Libraries["lambda"]
	contract := lib.Constraints
	lib.Constraints = nil
	historical := saveEventInvokePlan(t, exec)
	lib.Constraints = contract
	require.Equal(t, runtime.DecisionNoOp, eventInvokePlanStep(t, historical.Steps).Decision)
	_, err = exec.Plan(t.Context())
	require.ErrorContains(t, err, eventInvokeAddress+": schema: constraints[")
	require.ErrorContains(t, err, "maximum-retry-attempts must be between 0 and 2")
	assert.Equal(t, []eventInvokeCall{
		eventInvokeRequest("GET", "fn", "", nil), eventInvokeRequest("GET", "fn", "", nil),
	}, api.takeCalls())

	_, err = exec.ApplyPlan(t.Context(), historical)
	require.ErrorContains(t, err, eventInvokeAddress)
	require.ErrorContains(t, err, "schema: constraints[")
	require.ErrorContains(t, err, "maximum-retry-attempts must be between 0 and 2")
	assert.Empty(t, api.takeCalls())

	exec.Destroy = true
	applyEventInvokePlan(t, exec, runtime.DecisionDestroy)
	assert.Equal(t, []eventInvokeCall{
		eventInvokeRequest("GET", "fn", "", nil),
		eventInvokeRequest("GET", "fn", "", nil),
		eventInvokeRequest("DELETE", "fn", "", nil),
	}, api.takeCalls())
}

func TestEventInvokeDeferredConstraints(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusBase int64
		valid      bool
	}{
		{"valid", 200, true},
		{"invalid", 197, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := newEventInvokeAPI(t)
			initial := eventInvokeExecutor(t, "default", api, nil)
			applyEventInvokePlan(t, initial, runtime.DecisionCreate)
			assert.Equal(t, []eventInvokeCall{
				eventInvokeRequest("PUT", "fn", "", map[string]any{
					"MaximumRetryAttempts": float64(2),
				}),
				eventInvokeRequest("GET", "fn", "", nil),
			}, api.takeCalls())

			exec := eventInvokeExecutor(t, "deferred", api, initial.Store)
			exec.Inputs["name"] = "other"
			exec.Inputs["status-base"] = test.statusBase
			saved := saveEventInvokePlan(t, exec)
			step := eventInvokePlanStep(t, saved.Steps)
			assert.Equal(t, runtime.DecisionReplace, step.Decision)
			assert.Equal(t, []string{"function-name"}, step.ReplacementReasons)
			assert.Equal(t, map[string][]string{
				"maximum-retry-attempts": {"action.seed.status-code"},
			}, step.UnresolvedInputs)
			assert.Equal(t, []eventInvokeCall{eventInvokeRequest("GET", "fn", "", nil)}, api.takeCalls())

			_, err := exec.ApplyPlan(t.Context(), saved)
			invoke := eventInvokeCall{
				method: "POST", path: "/2015-03-31/functions/seed/invocations",
				body: map[string]any{},
			}
			if !test.valid {
				require.ErrorContains(t, err, eventInvokeAddress)
				require.ErrorContains(t, err, "schema: constraints[")
				require.ErrorContains(t, err, "maximum-retry-attempts must be between 0 and 2")
				assert.Equal(t, []eventInvokeCall{invoke}, api.takeCalls())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []eventInvokeCall{
				invoke,
				eventInvokeRequest("GET", "fn", "", nil),
				eventInvokeRequest("DELETE", "fn", "", nil),
				eventInvokeRequest("PUT", "other", "", map[string]any{
					"MaximumRetryAttempts": float64(0),
				}),
				eventInvokeRequest("GET", "other", "", nil),
			}, api.takeCalls())
		})
	}
}

func TestEventInvokeMutableLifecycle(t *testing.T) {
	api := newEventInvokeAPI(t)
	exec := eventInvokeExecutor(t, "runtime", api, nil)
	exec.Inputs["name"] = "fn:blue"
	exec.Inputs["event-age"] = int64(60)
	exec.Inputs["retries"] = int64(0)
	failure := map[string]any{"destination": "arn:aws:sqs:::failure"}
	success := map[string]any{"destination": "arn:aws:sqs:::success"}
	exec.Inputs["on-failure"] = failure
	exec.Inputs["on-success"] = success
	api.setGet(http.StatusOK, `{"MaximumEventAgeInSeconds":60,"MaximumRetryAttempts":0,
		"DestinationConfig":{"OnFailure":{"Destination":"arn:aws:sqs:::failure"},
			"OnSuccess":{"Destination":"arn:aws:sqs:::success"}}}`)

	create := map[string]any{
		"MaximumEventAgeInSeconds": float64(60),
		"MaximumRetryAttempts":     float64(0),
		"DestinationConfig": map[string]any{
			"OnFailure": map[string]any{"Destination": "arn:aws:sqs:::failure"},
			"OnSuccess": map[string]any{"Destination": "arn:aws:sqs:::success"},
		},
	}
	get := eventInvokeRequest("GET", "fn", "blue", nil)
	applyEventInvokePlan(t, exec, runtime.DecisionCreate)
	assert.Equal(t, []eventInvokeCall{eventInvokeRequest("PUT", "fn:blue", "", create), get},
		api.takeCalls())
	assertEventInvokeOutput(t, exec, map[string]any{
		"function-name": "fn", "qualifier": "blue", "region": "us-east-1",
		"maximum-event-age-in-seconds": int64(60), "maximum-retry-attempts": int64(0),
		"destination-config": map[string]any{"on-failure": failure, "on-success": success},
	})

	applyEventInvokePlan(t, exec, runtime.DecisionNoOp)
	assert.Equal(t, []eventInvokeCall{get}, api.takeCalls())

	exec.Inputs["event-age"] = nil
	exec.Inputs["on-success"] = nil
	saved := saveEventInvokePlan(t, exec)
	require.Equal(t, runtime.DecisionUpdate, eventInvokePlanStep(t, saved.Steps).Decision)
	assert.Equal(t, []eventInvokeCall{get}, api.takeCalls())
	api.setGet(http.StatusOK, `{"MaximumEventAgeInSeconds":21600,"MaximumRetryAttempts":0,
		"DestinationConfig":{"OnFailure":{"Destination":"arn:aws:sqs:::failure"}}}`)
	_, err := exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	update := map[string]any{
		"MaximumRetryAttempts": float64(0),
		"DestinationConfig": map[string]any{
			"OnFailure": map[string]any{"Destination": "arn:aws:sqs:::failure"},
		},
	}
	put := eventInvokeRequest("PUT", "fn", "blue", update)
	assert.Equal(t, []eventInvokeCall{put, get}, api.takeCalls())
	assertEventInvokeOutput(t, exec, map[string]any{
		"function-name": "fn", "qualifier": "blue", "region": "us-east-1",
		"maximum-event-age-in-seconds": int64(21600), "maximum-retry-attempts": int64(0),
		"destination-config": map[string]any{"on-failure": failure, "on-success": nil},
	})

	api.setGet(http.StatusOK, `{"MaximumEventAgeInSeconds":21600,"MaximumRetryAttempts":2}`)
	saved = saveEventInvokePlan(t, exec)
	require.Equal(t, runtime.DecisionUpdate, eventInvokePlanStep(t, saved.Steps).Decision)
	assert.Equal(t, []eventInvokeCall{get}, api.takeCalls())
	api.setGet(http.StatusOK, `{"MaximumEventAgeInSeconds":21600,"MaximumRetryAttempts":0,
		"DestinationConfig":{"OnFailure":{"Destination":"arn:aws:sqs:::failure"}}}`)
	_, err = exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Equal(t, []eventInvokeCall{put, get}, api.takeCalls())

	exec.Inputs["on-failure"] = nil
	exec.Inputs["on-success"] = success
	saved = saveEventInvokePlan(t, exec)
	require.Equal(t, runtime.DecisionUpdate, eventInvokePlanStep(t, saved.Steps).Decision)
	assert.Equal(t, []eventInvokeCall{get}, api.takeCalls())
	api.setGet(http.StatusOK, `{"MaximumEventAgeInSeconds":21600,"MaximumRetryAttempts":0,
		"DestinationConfig":{"OnSuccess":{"Destination":"arn:aws:sqs:::success"}}}`)
	_, err = exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Equal(t, []eventInvokeCall{
		eventInvokeRequest("PUT", "fn", "blue", map[string]any{
			"MaximumRetryAttempts": float64(0),
			"DestinationConfig": map[string]any{
				"OnSuccess": map[string]any{"Destination": "arn:aws:sqs:::success"},
			},
		}),
		get,
	}, api.takeCalls())
	assertEventInvokeOutput(t, exec, map[string]any{
		"function-name": "fn", "qualifier": "blue", "region": "us-east-1",
		"maximum-event-age-in-seconds": int64(21600), "maximum-retry-attempts": int64(0),
		"destination-config": map[string]any{"on-failure": nil, "on-success": success},
	})
}

func assertEventInvokeOutput(t *testing.T, exec *runtime.Executor, want map[string]any) {
	t.Helper()
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	entry := snapshot.Find(eventInvokeAddress)
	require.NotNil(t, entry)
	assert.Equal(t, want, entry.Outputs)
}

func TestEventInvokeReplacementPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, function, qualifier, destination, diagnostic string
	}{
		{"extra segment", "fn:a:b", "", "", "cannot separate function-name and qualifier"},
		{"two qualifiers", "fn:a", "b", "", "invalid inline qualifier"},
		{"bad destination", "other", "", "invalid", "on-success.destination must be a valid ARN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newEventInvokeAPI(t)
			exec := eventInvokeExecutor(t, "runtime", api, nil)
			exec.Inputs["name"] = tc.function
			if tc.qualifier != "" {
				exec.Inputs["qualifier"] = tc.qualifier
			}
			if tc.destination != "" {
				exec.Inputs["on-success"] = map[string]any{"destination": tc.destination}
			}
			saved := saveEventInvokePlan(t, exec)
			require.Equal(t, runtime.DecisionCreate, eventInvokePlanStep(t, saved.Steps).Decision)
			_, err := exec.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, tc.diagnostic)
			assert.Empty(t, api.takeCalls())

			initial := eventInvokeExecutor(t, "runtime", api, exec.Store)
			initial.Inputs["name"] = "old"
			applyEventInvokePlan(t, initial, runtime.DecisionCreate)
			api.takeCalls()
			saved = saveEventInvokePlan(t, exec)
			require.Equal(t, runtime.DecisionReplace, eventInvokePlanStep(t, saved.Steps).Decision)
			assert.Equal(t, []eventInvokeCall{
				eventInvokeRequest("GET", "old", "", nil),
			}, api.takeCalls())
			_, err = exec.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, tc.diagnostic)
			assert.Empty(t, api.takeCalls())
		})
	}
}

func TestEventInvokeReplacementKeys(t *testing.T) {
	for _, tc := range []struct {
		field, function, qualifier string
	}{
		{"function-name", "other", "blue"},
		{"qualifier", "fn", "green"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			api := newEventInvokeAPI(t)
			exec := eventInvokeExecutor(t, "runtime", api, nil)
			exec.Inputs["qualifier"] = "blue"
			applyEventInvokePlan(t, exec, runtime.DecisionCreate)
			api.takeCalls()
			exec.Inputs["name"] = tc.function
			exec.Inputs["qualifier"] = tc.qualifier
			saved := applyEventInvokePlan(t, exec, runtime.DecisionReplace)
			assert.Equal(t, []string{tc.field}, eventInvokePlanStep(t, saved.Steps).ReplacementReasons)
			old := eventInvokeRequest("GET", "fn", "blue", nil)
			assert.Equal(t, []eventInvokeCall{
				old, old, eventInvokeRequest("DELETE", "fn", "blue", nil),
				eventInvokeRequest("PUT", tc.function, tc.qualifier, map[string]any{
					"MaximumRetryAttempts": float64(2),
					"DestinationConfig":    map[string]any{},
				}),
				eventInvokeRequest("GET", tc.function, tc.qualifier, nil),
			}, api.takeCalls())
		})
	}
}

func TestEventInvokeRegionReplacement(t *testing.T) {
	for _, tc := range []struct {
		name    string
		initial any
		desired string
	}{
		{"distinct configured regions", "us-east-1", "us-west-2"},
		{"omission and equivalent configured region", nil, "us-east-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			east := newEventInvokeAPI(t)
			west := east
			if tc.initial != nil {
				west = newEventInvokeAPI(t)
				west.mu.Lock()
				west.region = tc.desired
				west.mu.Unlock()
			}
			exec := eventInvokeExecutor(t, "runtime", east, nil)
			exec.Inputs["region"] = tc.initial
			applyEventInvokePlan(t, exec, runtime.DecisionCreate)
			east.takeCalls()

			exec.Inputs["region"] = tc.desired
			exec.Inputs["endpoint"] = west.server.URL
			saved := applyEventInvokePlan(t, exec, runtime.DecisionReplace)
			assert.Equal(t, []string{"region"},
				eventInvokePlanStep(t, saved.Steps).ReplacementReasons)

			get := eventInvokeRequest("GET", "fn", "", nil)
			prior := []eventInvokeCall{get, get, eventInvokeRequest("DELETE", "fn", "", nil)}
			desired := []eventInvokeCall{
				eventInvokeRequest("PUT", "fn", "", map[string]any{
					"MaximumRetryAttempts": float64(2),
					"DestinationConfig":    map[string]any{},
				}),
				get,
			}
			if east == west {
				assert.Equal(t, append(prior, desired...), east.takeCalls())
			} else {
				assert.Equal(t, prior, east.takeCalls())
				assert.Equal(t, desired, west.takeCalls())
			}
			assertEventInvokeOutput(t, exec, map[string]any{
				"function-name": "fn", "qualifier": "", "region": tc.desired,
				"maximum-event-age-in-seconds": int64(0), "maximum-retry-attempts": int64(0),
				"destination-config": nil,
			})
		})
	}
}

func TestEventInvokeWriteGetResults(t *testing.T) {
	for _, decision := range []runtime.Decision{runtime.DecisionCreate, runtime.DecisionUpdate} {
		t.Run(string(decision), func(t *testing.T) {
			api := newEventInvokeAPI(t)
			exec := eventInvokeExecutor(t, "default", api, nil)
			if decision == runtime.DecisionUpdate {
				applyEventInvokePlan(t, exec, runtime.DecisionCreate)
				api.takeCalls()
				exec = eventInvokeExecutor(t, "runtime", api, exec.Store)
				exec.Inputs["retries"] = int64(0)
			}
			put := eventInvokeRequest("PUT", "fn", "", map[string]any{
				"MaximumRetryAttempts": float64(2),
			})
			if decision == runtime.DecisionUpdate {
				put.body["MaximumRetryAttempts"] = float64(0)
				put.body["DestinationConfig"] = map[string]any{}
			}
			get := eventInvokeRequest("GET", "fn", "", nil)

			api.setGet(http.StatusOK, "{}")
			saved := saveEventInvokePlan(t, exec)
			require.Equal(t, decision, eventInvokePlanStep(t, saved.Steps).Decision)
			api.takeCalls()

			api.setGet(http.StatusForbidden, `{"__type":"AccessDeniedException","message":"denied"}`)
			_, err := exec.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, "get function event invoke config")
			require.ErrorContains(t, err, "AccessDeniedException")
			assert.Equal(t, []eventInvokeCall{put, get}, api.takeCalls())

			api.setGet(http.StatusNotFound, `{"__type":"ResourceNotFoundException"}`)
			_, err = exec.ApplyPlan(t.Context(), saved)
			require.ErrorIs(t, err, runtime.ErrNotFound)
			require.ErrorContains(t, err, "after put")
			assert.Equal(t, []eventInvokeCall{put, get}, api.takeCalls())
			next := saveEventInvokePlan(t, exec)
			require.Equal(t, runtime.DecisionCreate, eventInvokePlanStep(t, next.Steps).Decision)
			if decision == runtime.DecisionUpdate {
				assert.Equal(t, []eventInvokeCall{get}, api.takeCalls())
			} else {
				assert.Empty(t, api.takeCalls())
			}

			api.setGet(http.StatusOK, "{}")
			_, err = exec.ApplyPlan(t.Context(), saved)
			require.NoError(t, err)
			assert.Equal(t, []eventInvokeCall{put, get}, api.takeCalls())
			assertEventInvokeOutput(t, exec, map[string]any{
				"function-name": "fn", "qualifier": "", "region": "us-east-1",
				"maximum-event-age-in-seconds": int64(0), "maximum-retry-attempts": int64(0),
				"destination-config": nil,
			})
		})
	}
}

func TestEventInvokeReadAndDeleteErrors(t *testing.T) {
	api := newEventInvokeAPI(t)
	exec := eventInvokeExecutor(t, "default", api, nil)
	applyEventInvokePlan(t, exec, runtime.DecisionCreate)
	api.takeCalls()
	get := eventInvokeRequest("GET", "fn", "", nil)
	del := eventInvokeRequest("DELETE", "fn", "", nil)

	api.setGet(http.StatusNotFound, `{"__type":"ResourceNotFoundException"}`)
	recreate := saveEventInvokePlan(t, exec)
	require.Equal(t, runtime.DecisionCreate, eventInvokePlanStep(t, recreate.Steps).Decision)
	assert.Equal(t, []eventInvokeCall{get}, api.takeCalls())
	api.setGet(http.StatusForbidden, `{"__type":"AccessDeniedException","message":"denied"}`)
	_, err := exec.Plan(t.Context())
	require.ErrorContains(t, err, eventInvokeAddress)
	require.ErrorContains(t, err, "get function event invoke config")
	require.ErrorContains(t, err, "AccessDeniedException")
	assert.Equal(t, []eventInvokeCall{get}, api.takeCalls())

	api.setGet(http.StatusOK, "{}")
	applyEventInvokePlan(t, exec, runtime.DecisionNoOp)
	assert.Equal(t, []eventInvokeCall{get}, api.takeCalls())

	exec.Destroy = true
	saved := saveEventInvokePlan(t, exec)
	require.Equal(t, runtime.DecisionDestroy, eventInvokePlanStep(t, saved.Steps).Decision)
	assert.Equal(t, []eventInvokeCall{get}, api.takeCalls())

	api.mu.Lock()
	api.del = eventInvokeReply{http.StatusForbidden, `{"__type":"AccessDeniedException"}`}
	api.mu.Unlock()
	_, err = exec.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "delete function event invoke config")
	require.ErrorContains(t, err, "AccessDeniedException")
	assert.Equal(t, []eventInvokeCall{get, del}, api.takeCalls())

	api.mu.Lock()
	api.del = eventInvokeReply{http.StatusNotFound, `{"__type":"ResourceNotFoundException"}`}
	api.mu.Unlock()
	_, err = exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Equal(t, []eventInvokeCall{get, del}, api.takeCalls())
}

func TestEventInvokeFunctionKeys(t *testing.T) {
	const full = "arn:aws:lambda:us-east-1:123456789012:function:fn"
	const partial = "123456789012:function:fn"
	for _, tc := range []struct {
		input, base, qualifier string
	}{
		{"arn:blue", "arn", "blue"},
		{full, full, ""},
		{full + ":$LATEST", full, "$LATEST"},
		{partial, partial, ""},
		{partial + ":5", partial, "5"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			api := newEventInvokeAPI(t)
			exec := eventInvokeExecutor(t, "default", api, nil)
			exec.Inputs["name"] = tc.input
			applyEventInvokePlan(t, exec, runtime.DecisionCreate)
			get := eventInvokeRequest("GET", tc.base, tc.qualifier, nil)
			assert.Equal(t, []eventInvokeCall{
				eventInvokeRequest("PUT", tc.input, "", map[string]any{
					"MaximumRetryAttempts": float64(2),
				}),
				get,
			}, api.takeCalls())
			assertEventInvokeOutput(t, exec, map[string]any{
				"function-name": tc.base, "qualifier": tc.qualifier, "region": "us-east-1",
				"maximum-event-age-in-seconds": int64(0), "maximum-retry-attempts": int64(0),
				"destination-config": nil,
			})
			exec.Destroy = true
			applyEventInvokePlan(t, exec, runtime.DecisionDestroy)
			assert.Equal(t, []eventInvokeCall{
				get, get, eventInvokeRequest("DELETE", tc.base, tc.qualifier, nil),
			}, api.takeCalls())
		})
	}
}
