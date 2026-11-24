package eventbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
)

const (
	sendAction = "action.send"
	testTime   = "2026-11-22T23:45:00Z"
	timeRule   = "action.send: schema: constraints[1] (predicate): " +
		"time must be an RFC3339 timestamp"
	sourceRule   = "action.send: schema: constraints[0] (predicate): source is required"
	resourceRule = "action.send: schema: constraints[2] (predicate): resources must not contain null"
)

type putEventsSeed struct {
	Echo any `ub:"echo"`
}

func (r *putEventsSeed) Run(context.Context, *awscfg.Configuration) (map[string]any, error) {
	return map[string]any{"value": r.Echo}, nil
}

type eventRequest struct {
	Entries []map[string]any
}

func eventActionEndpoint(t *testing.T) (string, <-chan eventRequest) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	requests := make(chan eventRequest, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "AWSEvents.PutEvents", r.Header.Get("X-Amz-Target"))
		var body eventRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode EventBridge request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case requests <- body:
		default:
			t.Error("unexpected extra EventBridge request")
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"Entries":[{"EventId":"id"}],"FailedEntryCount":0}`))
	}))
	t.Cleanup(server.Close)
	return server.URL, requests
}

func eventActionExecutor(t *testing.T, fixture string) *runtime.Executor {
	t.Helper()
	lib := runtime.LibraryWithPath(Library(),
		"github.com/cloudboss/unobin-library-aws//service/eventbridge")
	lib.Schema = readLibrarySchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"action.put-events": lib.Schema.Actions["put-events"].Constraints,
	}
	libs := map[string]*runtime.Library{
		"eventbridge": lib,
		"test": {
			Actions: map[string]runtime.ActionRegistration{
				"seed": runtime.MakeAction[putEventsSeed, map[string]any, *awscfg.Configuration](),
			},
		},
	}
	path := filepath.Join("testdata", "ub", "put-events", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	store, err := local.NewStore(t.TempDir(), "test-put-events", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "test-put-events", Version: "v0", ContentRevision: "test"},
	}
}

func savedEventActionPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func eventActionStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == sendAction {
			return step
		}
	}
	t.Fatalf("saved plan has no %s step", sendAction)
	return runtime.PlanStep{}
}

func assertEventRequest(t *testing.T, requests <-chan eventRequest, want []map[string]any) {
	t.Helper()
	select {
	case req := <-requests:
		assert.Equal(t, eventRequest{Entries: want}, req)
	default:
		t.Fatal("missing PutEvents request")
	}
	assert.Empty(t, requests)
}

func takeSendStages(events <-chan runtime.ApplyEvent) []runtime.ApplyStage {
	var stages []runtime.ApplyStage
	for len(events) > 0 {
		if event := <-events; event.Address == sendAction {
			stages = append(stages, event.Stage)
		}
	}
	return stages
}

func TestPutEventsEvaluatedInputsAndSkip(t *testing.T) {
	endpoint, requests := eventActionEndpoint(t)
	executor := eventActionExecutor(t, "settled")
	valid := []any{
		map[string]any{"source": "omitted"},
		map[string]any{"source": "", "time": nil, "resources": []any{}},
		map[string]any{"source": "null list", "resources": nil},
		map[string]any{"source": "explicit values", "resources": []any{"second", "", "first"}},
	}
	want := []map[string]any{
		{"Source": "omitted"},
		{"Source": "", "Resources": []any{}},
		{"Source": "null list"},
		{"Source": "explicit values", "Resources": []any{"second", "", "first"}},
	}
	executor.Inputs = map[string]any{"endpoint": endpoint, "entries": valid, "trigger": "fixed"}
	first := savedEventActionPlan(t, executor)
	require.Equal(t, runtime.DecisionRerun, eventActionStep(t, first).Decision)
	events := make(chan runtime.ApplyEvent, 20)
	executor.Events = events
	executor.Inputs = nil
	_, err := executor.ApplyPlan(t.Context(), first)
	require.NoError(t, err)
	assertEventRequest(t, requests, want)
	assert.Equal(t, []runtime.ApplyStage{runtime.StageStart, runtime.StageDone},
		takeSendStages(events))
	executor.Events = nil

	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	entry := snapshot.Find(sendAction)
	require.NotNil(t, entry)
	assert.Empty(t, entry.Outputs)

	executor.Inputs = first.Inputs
	unchanged := savedEventActionPlan(t, executor)
	require.Equal(t, runtime.DecisionSkip, eventActionStep(t, unchanged).Decision)
	executor.Inputs = nil
	_, err = executor.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	assert.Empty(t, requests)

	executor.Inputs = first.Inputs
	for _, tt := range []struct {
		name       string
		entry      map[string]any
		diagnostic string
	}{
		{name: "missing source", entry: map[string]any{}, diagnostic: sourceRule + " (entries[0])"},
		{
			name: "null source", entry: map[string]any{"source": nil},
			diagnostic: sourceRule + " (entries[0])",
		},
		{
			name: "bad timestamp", entry: map[string]any{"source": "test", "time": "bad"},
			diagnostic: timeRule + " (entries[0])",
		},
		{
			name:       "null resource",
			entry:      map[string]any{"source": "test", "resources": []any{"valid", nil}},
			diagnostic: resourceRule + " (entries[0].resources[1])",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			executor.Inputs["entries"] = valid
			skip := savedEventActionPlan(t, executor)
			require.Equal(t, runtime.DecisionSkip, eventActionStep(t, skip).Decision)

			executor.Inputs["entries"] = []any{tt.entry}
			_, err := executor.Plan(t.Context())
			assert.Empty(t, requests)
			require.ErrorContains(t, err, tt.diagnostic)
			_, err = executor.ApplyPlan(t.Context(), skip)
			assert.Empty(t, requests)
			require.ErrorContains(t, err, tt.diagnostic)
		})
	}

	executor.Inputs["entries"] = valid
	executor.Inputs["trigger"] = "different"
	rerun := savedEventActionPlan(t, executor)
	require.Equal(t, runtime.DecisionRerun, eventActionStep(t, rerun).Decision)
	_, err = executor.ApplyPlan(t.Context(), rerun)
	require.NoError(t, err)
	assertEventRequest(t, requests, want)

	executor.Destroy = true
	destroy := savedEventActionPlan(t, executor)
	require.Equal(t, runtime.DecisionDestroy, eventActionStep(t, destroy).Decision)
	_, err = executor.ApplyPlan(t.Context(), destroy)
	require.NoError(t, err)
	assert.Empty(t, requests)
	snapshot, err = executor.Store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find(sendAction))
}

func TestPutEventsDeferredSavedPlan(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture string
		value   any
		time    any
		rule    string
		entries []map[string]any
	}{
		{
			name: "valid time", fixture: "deferred-time", value: testTime,
			entries: []map[string]any{{"Source": "test.deferred", "Time": float64(1795391100)}},
		},
		{
			name: "invalid time", fixture: "deferred-time", value: "bad",
			rule: timeRule + " (entries[0])",
		},
		{
			name: "explicit empty source", fixture: "deferred-source", value: "",
			entries: []map[string]any{{"Source": ""}, {"Source": "test.second"}},
		},
		{
			name: "source resolves to null", fixture: "deferred-source",
			rule: sourceRule + " (entries[0])",
		},
		{
			name: "source missing from upstream object", fixture: "deferred-entries",
			value: []any{map[string]any{"detail": "{}"}}, rule: sourceRule + " (entries[0])",
		},
		{
			name: "null resource from upstream object", fixture: "deferred-entries",
			value: []any{
				map[string]any{"source": "valid", "resources": []any{""}},
				map[string]any{"source": "invalid", "resources": []any{"valid", nil}},
			},
			rule: resourceRule + " (entries[1].resources[1])",
		},
		{
			name: "known bad time beside pending source", fixture: "deferred-source",
			value: "seed", time: "bad", rule: timeRule + " (entries[1])",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, requests := eventActionEndpoint(t)
			executor := eventActionExecutor(t, tt.fixture)
			executor.Inputs = map[string]any{
				"endpoint": endpoint, "value": tt.value, "time": tt.time,
			}
			if tt.name == "known bad time beside pending source" {
				_, err := executor.Plan(t.Context())
				require.ErrorContains(t, err, tt.rule)
				assert.Empty(t, requests)
				return
			}
			saved := savedEventActionPlan(t, executor)
			step := eventActionStep(t, saved)
			require.Equal(t, runtime.DecisionRerun, step.Decision)
			assert.Equal(t, map[string][]string{"entries": {"action.seed.value"}},
				step.UnresolvedInputs)
			assert.Empty(t, requests)
			executor.Inputs = nil
			events := make(chan runtime.ApplyEvent, 20)
			executor.Events = events
			_, err := executor.ApplyPlan(t.Context(), saved)
			if tt.rule != "" {
				assert.Empty(t, requests)
				require.ErrorContains(t, err, tt.rule)
				assert.Equal(t, []runtime.ApplyStage{runtime.StageStart, runtime.StageFail},
					takeSendStages(events))
				return
			}
			require.NoError(t, err)
			assertEventRequest(t, requests, tt.entries)
		})
	}
}
