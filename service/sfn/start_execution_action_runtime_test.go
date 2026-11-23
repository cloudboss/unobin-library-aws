package sfn_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

	awssfn "github.com/cloudboss/unobin-library-aws/service/sfn"
)

const (
	startAction = "action.start"
	testMachine = "arn:aws:states:us-east-1:123456789012:stateMachine:example"
	testRun     = "arn:aws:states:us-east-1:123456789012:execution:example:recorded"
	testStart   = "2026-11-23T00:00:00.123Z"

	startJSONRule = "action.start: schema: constraints[0] (predicate): input must be valid JSON"
	startARNRule  = "action.start: schema: constraints[1] (predicate): state-machine-arn is required"
)

type startExecutionSeed struct {
	Echo any `ub:"echo"`
}

func (r *startExecutionSeed) Run(context.Context, *awscfg.Configuration) (map[string]any, error) {
	return map[string]any{"value": r.Echo}, nil
}

type startExecutionRequest struct {
	Operation string
	Body      map[string]any
}

func startRequest(arn, payload string) startExecutionRequest {
	return startExecutionRequest{
		Operation: "AWSStepFunctions.StartExecution",
		Body:      map[string]any{"stateMachineArn": arn, "input": payload},
	}
}

func startExecutionEndpoint(t *testing.T) (string, func() []startExecutionRequest) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	requests := make(chan startExecutionRequest, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case requests <- startExecutionRequest{r.Header.Get("X-Amz-Target"), body}:
		default:
			t.Error("too many SFN requests")
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_, _ = w.Write([]byte(`{"executionArn":"` + testRun + `","startDate":1795392000.1235}`))
	}))
	t.Cleanup(server.Close)
	return server.URL, func() []startExecutionRequest {
		var history []startExecutionRequest
		for len(requests) > 0 {
			history = append(history, <-requests)
		}
		return history
	}
}

func startExecutionExecutor(
	t *testing.T, fixture string, schema *runtime.LibrarySchema,
) (*runtime.Executor, *local.Store) {
	t.Helper()
	lib := runtime.LibraryWithPath(awssfn.Library(),
		"github.com/cloudboss/unobin-library-aws//service/sfn")
	lib.Schema = schema
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"action.start-execution": schema.Actions["start-execution"].Constraints,
	}
	libs := map[string]*runtime.Library{
		"sfn": lib,
		"test": {
			Actions: map[string]runtime.ActionRegistration{
				"seed": runtime.MakeAction[
					startExecutionSeed, map[string]any, *awscfg.Configuration,
				](),
			},
		},
	}
	path := startExecutionFixture(fixture)
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	store, err := local.NewStore(t.TempDir(), "test-start-execution", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{
			Name: "test-start-execution", Version: "v0", ContentRevision: "test",
		},
	}, store
}

func savedStartExecutionPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func startExecutionStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == startAction {
			return step
		}
	}
	t.Fatalf("saved plan has no %s step", startAction)
	return runtime.PlanStep{}
}

func startExecutionStages(events <-chan runtime.ApplyEvent) []runtime.ApplyStage {
	var stages []runtime.ApplyStage
	for len(events) > 0 {
		if event := <-events; event.Address == startAction {
			stages = append(stages, event.Stage)
		}
	}
	return stages
}

func TestStartExecutionLiteralRequests(t *testing.T) {
	endpoint, requests := startExecutionEndpoint(t)
	executor, _ := startExecutionExecutor(t, "literal", readLibrarySchema(t))
	executor.Inputs = map[string]any{"endpoint": endpoint}
	plan := savedStartExecutionPlan(t, executor)
	executor.Inputs = nil
	result, err := executor.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	emptyOptionals := startRequest(testMachine+":live", ` { "items": [] } `)
	emptyOptionals.Body["name"] = ""
	emptyOptionals.Body["traceHeader"] = ""
	arrayWithOptionals := startRequest(testMachine, " [1, true] ")
	arrayWithOptionals.Body["name"] = "example"
	arrayWithOptionals.Body["traceHeader"] = "Root=1-67891233-abcdef012345678912345678"
	assert.ElementsMatch(t, []startExecutionRequest{
		startRequest(testMachine, "{}"),
		startRequest(testMachine+":4", "{}"),
		emptyOptionals,
		arrayWithOptionals,
		startRequest(testMachine, "true"),
		startRequest(testMachine, "null"),
	}, requests())
	assert.Equal(t, map[string]any{"execution-arn": testRun, "start-date": testStart}, result.Outputs)
}

func TestStartExecutionEvaluatedInputsAndSkip(t *testing.T) {
	endpoint, requests := startExecutionEndpoint(t)
	executor, store := startExecutionExecutor(t, "settled", readLibrarySchema(t))
	executor.Inputs = map[string]any{
		"endpoint": endpoint, "state-machine-arn": testMachine,
		"payload": `"status"`, "trigger": "fixed",
	}
	events := make(chan runtime.ApplyEvent, 20)
	executor.Events = events

	first := savedStartExecutionPlan(t, executor)
	require.Equal(t, runtime.DecisionRerun, startExecutionStep(t, first).Decision)
	executor.Inputs = nil
	result, err := executor.ApplyPlan(t.Context(), first)
	require.NoError(t, err)
	wantOutputs := map[string]any{"execution-arn": testRun, "start-date": testStart}
	assert.Equal(t, wantOutputs, result.Outputs)
	assert.Equal(t, []startExecutionRequest{startRequest(testMachine, `"status"`)}, requests())
	assert.Equal(t, []runtime.ApplyStage{runtime.StageStart, runtime.StageDone},
		startExecutionStages(events))

	executor.Inputs = first.Inputs
	unchanged := savedStartExecutionPlan(t, executor)
	require.Equal(t, runtime.DecisionSkip, startExecutionStep(t, unchanged).Decision)
	executor.Inputs = nil
	result, err = executor.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	assert.Equal(t, wantOutputs, result.Outputs)
	assert.Empty(t, requests())
	executor.Events = nil

	for _, test := range []struct {
		name  string
		field string
		value any
		rule  string
	}{
		{name: "invalid JSON", field: "payload", value: "{", rule: startJSONRule},
		{name: "null ARN", field: "state-machine-arn", rule: startARNRule},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor.Inputs = map[string]any{
				"endpoint": endpoint, "state-machine-arn": testMachine,
				"payload": "{}", "trigger": "fixed",
			}
			skip := savedStartExecutionPlan(t, executor)
			require.Equal(t, runtime.DecisionSkip, startExecutionStep(t, skip).Decision)
			executor.Inputs[test.field] = test.value
			_, err := executor.Plan(t.Context())
			require.ErrorContains(t, err, test.rule)
			assert.Empty(t, requests())
			_, err = executor.ApplyPlan(t.Context(), skip)
			require.ErrorContains(t, err, test.rule)
			assert.Empty(t, requests())
		})
	}

	executor.Inputs = map[string]any{
		"endpoint": endpoint, "state-machine-arn": "", "payload": "{}", "trigger": "changed",
	}
	rerun := savedStartExecutionPlan(t, executor)
	require.Equal(t, runtime.DecisionRerun, startExecutionStep(t, rerun).Decision)
	_, err = executor.ApplyPlan(t.Context(), rerun)
	require.NoError(t, err)
	assert.Equal(t, []startExecutionRequest{startRequest("", "{}")}, requests())

	snapshot, err := store.Current()
	require.NoError(t, err)
	entry := snapshot.Find(startAction)
	require.NotNil(t, entry)
	entry.Inputs["input"] = "historical invalid input"
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	executor.Inputs["payload"] = "historical invalid input"
	executor.Destroy = true
	destroy := savedStartExecutionPlan(t, executor)
	require.Equal(t, runtime.DecisionDestroy, startExecutionStep(t, destroy).Decision)
	_, err = executor.ApplyPlan(t.Context(), destroy)
	require.NoError(t, err)
	assert.Empty(t, requests())
	snapshot, err = store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find(startAction))
}

func TestStartExecutionDeferredSavedRerun(t *testing.T) {
	schema := readLibrarySchema(t)
	for _, test := range []struct {
		name    string
		arn     any
		payload any
		want    string
		rule    string
	}{
		{name: "valid JSON", arn: testMachine, payload: " [true] ", want: " [true] "},
		{name: "null payload", arn: testMachine, want: "{}"},
		{name: "invalid JSON", arn: testMachine, payload: "[", rule: startJSONRule},
		{name: "null ARN", payload: "{}", rule: startARNRule},
		{name: "empty ARN", arn: "", payload: "{}", want: "{}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint, requests := startExecutionEndpoint(t)
			executor, _ := startExecutionExecutor(t, "deferred", schema)
			executor.Inputs = map[string]any{
				"endpoint": endpoint, "state-machine-arn": test.arn,
				"payload": test.payload, "trigger": "fixed",
			}
			saved := savedStartExecutionPlan(t, executor)
			step := startExecutionStep(t, saved)
			require.Equal(t, runtime.DecisionRerun, step.Decision)
			assert.Equal(t, map[string][]string{
				"state-machine-arn": {"action.seed-arn.value"},
				"input":             {"action.seed-payload.value"},
			}, step.UnresolvedInputs)
			assert.Empty(t, requests())

			executor.Inputs = nil
			events := make(chan runtime.ApplyEvent, 20)
			executor.Events = events
			_, err := executor.ApplyPlan(t.Context(), saved)
			if test.rule != "" {
				require.ErrorContains(t, err, test.rule)
				assert.Empty(t, requests())
				assert.Equal(t, []runtime.ApplyStage{runtime.StageStart, runtime.StageFail},
					startExecutionStages(events))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []startExecutionRequest{
				startRequest(test.arn.(string), test.want),
			}, requests())
		})
	}
}
