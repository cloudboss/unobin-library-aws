package applicationautoscaling_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
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

type targetCall struct {
	op  string
	key string
}

type targetAPI struct {
	t        *testing.T
	mu       sync.Mutex
	targets  map[string]map[string]any
	tags     map[string]map[string]any
	calls    []targetCall
	endpoint string
	fail     string
}

func newTargetAPI(t *testing.T) *targetAPI {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	a := &targetAPI{t: t, targets: map[string]map[string]any{}, tags: map[string]map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	t.Cleanup(server.Close)
	a.endpoint = server.URL
	return a
}

func apiKey(input map[string]any) string {
	resource := input["ResourceId"]
	if resource == nil {
		resource = input["ResourceIds"].([]any)[0]
	}
	return fmt.Sprintf("%s|%s|%s", input["ServiceNamespace"], resource, input["ScalableDimension"])
}

func (a *targetAPI) serve(w http.ResponseWriter, req *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	var input map[string]any
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	op := strings.TrimPrefix(req.Header.Get("X-Amz-Target"), "AnyScaleFrontendService.")
	key, _ := input["ResourceARN"].(string)
	if key == "" {
		key = apiKey(input)
	}
	a.calls = append(a.calls, targetCall{op, key})
	if a.fail == op {
		a.fail = ""
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"__type": "ValidationException", "message": "post-create failure",
		})
		return
	}
	result := map[string]any{}
	switch op {
	case "RegisterScalableTarget":
		if a.targets[key] == nil {
			a.targets[key] = map[string]any{"ScalableTargetARN": key, "RoleARN": "computed-role"}
			a.tags[key] = map[string]any{}
		}
		for k, v := range input {
			if k != "Tags" {
				a.targets[key][k] = v
			}
		}
		if tags, ok := input["Tags"].(map[string]any); ok {
			maps.Copy(a.tags[key], tags)
		}
	case "DescribeScalableTargets":
		if target := a.targets[key]; target != nil {
			result["ScalableTargets"] = []any{target}
		}
	case "DeregisterScalableTarget":
		delete(a.targets, key)
		delete(a.tags, key)
	case "ListTagsForResource":
		result["Tags"] = a.tags[key]
	case "UntagResource":
		for _, k := range input["TagKeys"].([]any) {
			delete(a.tags[key], k.(string))
		}
	case "TagResource":
		maps.Copy(a.tags[key], input["Tags"].(map[string]any))
	default:
		http.Error(w, "unexpected operation "+op, 400)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (a *targetAPI) takeCalls() []targetCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	calls := a.calls
	a.calls = nil
	return calls
}

func targetExecutor(
	t *testing.T, library *runtime.Library, fixture string, a *targetAPI, store state.Backend,
) *runtime.Executor {
	t.Helper()
	if store == nil {
		var err error
		store, err = local.NewStore(t.TempDir(), "target", "test", encrypters.Noop{})
		require.NoError(t, err)
	}
	path := filepath.Join("testdata", "ub", fixture, "factory.ub")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, body)
	require.NoError(t, err)
	source := file.Factory.Body
	libs := map[string]*runtime.Library{"aas": library}
	checker := check.NewSyntax(source, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	return &runtime.Executor{
		DAG: checker.DAG(), SyntaxSource: &source, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "target", Version: "v0", ContentRevision: "c0"},
		Inputs: map[string]any{"endpoint": a.endpoint, "resource": "table/a/b",
			"namespace": "dynamodb", "dimension": "dynamodb:table:ReadCapacityUnits",
			"min": int64(0), "max": int64(9), "role": nil, "suspension": nil, "tags": nil,
			"selectors": map[string]any{"resource": "preferred", "namespace": "preferred",
				"dimension": "preferred", "min": "preferred", "max": "preferred"}},
	}
}

func savedPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func applyPlan(t *testing.T, exec *runtime.Executor, plan *runtime.PlanFile) {
	t.Helper()
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
}

func targetStep(t *testing.T, plan *runtime.PlanFile) *runtime.PlanStep {
	t.Helper()
	for i := range plan.Steps {
		if plan.Steps[i].Address == "resource.target" {
			return &plan.Steps[i]
		}
	}
	t.Fatal("target step is absent")
	return nil
}

const targetKey = "dynamodb|table/a/b|dynamodb:table:ReadCapacityUnits"
const seedKey = "dynamodb|seed|dynamodb:table:ReadCapacityUnits"

func targetReadCalls(key string) []targetCall {
	return []targetCall{{"DescribeScalableTargets", key}, {"ListTagsForResource", key}}
}

func requireNativeRule(t *testing.T, err error, index int, field string) {
	t.Helper()
	require.ErrorContains(t, err, fmt.Sprintf("resource.target: schema: constraints[%d]", index))
	require.ErrorContains(t, err, field+" must be non-null")
}

func TestScalableTargetNativeNullInputs(t *testing.T) {
	lib := targetLibrary(t)
	for index, field := range []string{"resource-id", "service-namespace", "scalable-dimension",
		"min-capacity", "max-capacity"} {
		t.Run(field, func(t *testing.T) {
			a := newTargetAPI(t)
			exec := targetExecutor(t, lib, "evaluated", a, nil)
			input := []string{"resource", "namespace", "dimension", "min", "max"}[index]
			exec.Inputs["selectors"].(map[string]any)[input] = "absent"
			_, err := exec.Plan(t.Context())
			requireNativeRule(t, err, index, field)
			assert.Empty(t, a.takeCalls())
		})
	}
}

func TestScalableTargetReplacement(t *testing.T) {
	lib := targetLibrary(t)
	for index, field := range []string{"resource-id", "service-namespace", "scalable-dimension"} {
		t.Run(field, func(t *testing.T) {
			a := newTargetAPI(t)
			exec := targetExecutor(t, lib, "evaluated", a, nil)
			applyPlan(t, exec, savedPlan(t, exec))
			a.takeCalls()
			input := []string{"resource", "namespace", "dimension"}[index]
			exec.Inputs[input] = "new"
			plan := savedPlan(t, exec)
			step := targetStep(t, plan)
			assert.Equal(t, runtime.DecisionReplace, step.Decision)
			assert.Equal(t, []string{field}, step.ReplacementReasons)
			assert.Equal(t, targetReadCalls(targetKey), a.takeCalls())
			applyPlan(t, exec, plan)
			parts := []string{"dynamodb", "table/a/b", "dynamodb:table:ReadCapacityUnits"}
			parts[[]int{1, 0, 2}[index]] = "new"
			newKey := strings.Join(parts, "|")
			want := append(targetReadCalls(targetKey), targetCall{"DeregisterScalableTarget", targetKey},
				targetCall{"DescribeScalableTargets", targetKey},
				targetCall{"RegisterScalableTarget", newKey})
			want = append(want, targetReadCalls(newKey)...)
			assert.Equal(t, want, a.takeCalls())
		})
	}
}

func TestScalableTargetSavedNoOpAndHistoricalDestroy(t *testing.T) {
	for index, field := range []string{"resource-id", "service-namespace", "scalable-dimension",
		"min-capacity", "max-capacity"} {
		t.Run(field, func(t *testing.T) {
			lib := targetLibrary(t)
			a := newTargetAPI(t)
			exec := targetExecutor(t, lib, "evaluated", a, nil)
			applyPlan(t, exec, savedPlan(t, exec))
			a.takeCalls()
			valid := savedPlan(t, exec)
			assert.Equal(t, runtime.DecisionNoOp, targetStep(t, valid).Decision)
			a.takeCalls()
			applyPlan(t, exec, valid)
			assert.Empty(t, a.takeCalls())
			snapshot, err := exec.Store.Current()
			require.NoError(t, err)
			snapshot.Find("resource.target").Inputs[field] = nil
			revision, err := exec.Store.Write(snapshot)
			require.NoError(t, err)
			require.NoError(t, exec.Store.SetCurrent(revision))
			input := []string{"resource", "namespace", "dimension", "min", "max"}[index]
			exec.Inputs["selectors"].(map[string]any)[input] = "absent"
			const export = "resource.scalable-target"
			rules := lib.Constraints[export]
			delete(lib.Constraints, export)
			noOp := savedPlan(t, exec)
			lib.Constraints[export] = rules
			assert.Equal(t, runtime.DecisionNoOp, targetStep(t, noOp).Decision)
			a.takeCalls()
			_, err = exec.Plan(t.Context())
			requireNativeRule(t, err, index, field)
			assert.Equal(t, targetReadCalls(targetKey), a.takeCalls())
			fresh := targetExecutor(t, lib, "evaluated", a, exec.Store)
			fresh.Inputs = nil
			_, err = fresh.ApplyPlan(t.Context(), noOp)
			requireNativeRule(t, err, index, field)
			assert.Empty(t, a.takeCalls())
			exec.Destroy = true
			applyPlan(t, exec, savedPlan(t, exec))
			want := append(targetReadCalls(targetKey), targetReadCalls(targetKey)...)
			want = append(want, targetCall{"DeregisterScalableTarget", targetKey},
				targetCall{"DescribeScalableTargets", targetKey})
			assert.Equal(t, want, a.takeCalls())
		})
	}
}

func TestScalableTargetDeferredSavedReplacement(t *testing.T) {
	lib := targetLibrary(t)
	for index := -1; index < 5; index++ {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			a := newTargetAPI(t)
			initial := targetExecutor(t, lib, "evaluated", a, nil)
			applyPlan(t, initial, savedPlan(t, initial))
			a.takeCalls()
			exec := targetExecutor(t, lib, "deferred", a, initial.Store)
			exec.Inputs["resource"] = "table/new"
			fields := []string{"resource-id", "service-namespace", "scalable-dimension",
				"min-capacity", "max-capacity"}
			if index >= 0 {
				input := []string{"resource", "namespace", "dimension", "min", "max"}[index]
				exec.Inputs["selectors"].(map[string]any)[input] = "absent"
			}
			plan := savedPlan(t, exec)
			wantPending := map[string][]string{}
			for _, field := range fields {
				wantPending[field] = []string{"resource.seed.scalable-target-arn"}
			}
			assert.Equal(t, wantPending, targetStep(t, plan).UnresolvedInputs)
			assert.Equal(t, []string{"resource-id", "scalable-dimension", "service-namespace"},
				targetStep(t, plan).PendingReplacementReasons)
			a.takeCalls()
			fresh := targetExecutor(t, lib, "deferred", a, initial.Store)
			fresh.Inputs = nil
			_, err := fresh.ApplyPlan(t.Context(), plan)
			if index >= 0 {
				requireNativeRule(t, err, index, fields[index])
				assert.Equal(t, append([]targetCall{{"RegisterScalableTarget", seedKey}},
					targetReadCalls(seedKey)...), a.takeCalls())
			} else {
				require.NoError(t, err)
				calls := a.takeCalls()
				assert.Contains(t, calls, targetCall{"DeregisterScalableTarget", targetKey})
				assert.Contains(t, calls, targetCall{"RegisterScalableTarget",
					"dynamodb|table/new|dynamodb:table:ReadCapacityUnits"})
			}
		})
	}
}

func TestScalableTargetObservedDriftAndOptionalRemoval(t *testing.T) {
	lib := targetLibrary(t)
	a := newTargetAPI(t)
	exec := targetExecutor(t, lib, "evaluated", a, nil)
	exec.Inputs["role"] = "desired-role"
	exec.Inputs["suspension"] = map[string]any{"dynamic-scaling-in-suspended": true}
	exec.Inputs["tags"] = map[string]any{"managed": ""}
	applyPlan(t, exec, savedPlan(t, exec))
	a.takeCalls()
	a.mu.Lock()
	a.targets[targetKey]["MinCapacity"] = float64(7)
	a.targets[targetKey]["MaxCapacity"] = float64(12)
	a.targets[targetKey]["RoleARN"] = "drifted-role"
	a.targets[targetKey]["SuspendedState"] = map[string]any{
		"DynamicScalingInSuspended": false, "DynamicScalingOutSuspended": true,
		"ScheduledScalingSuspended": true,
	}
	a.tags[targetKey] = map[string]any{"unwanted": "x"}
	a.mu.Unlock()
	plan := savedPlan(t, exec)
	assert.Equal(t, runtime.DecisionUpdate, targetStep(t, plan).Decision)
	a.takeCalls()
	applyPlan(t, exec, plan)
	want := []targetCall{{"ListTagsForResource", targetKey},
		targetCall{"UntagResource", targetKey}, targetCall{"TagResource", targetKey},
		targetCall{"RegisterScalableTarget", targetKey}}
	want = append(want, targetReadCalls(targetKey)...)
	assert.Equal(t, want, a.takeCalls())
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	out := snapshot.Find("resource.target").Outputs
	assert.Equal(t, targetKey, out["scalable-target-arn"])
	assert.NotContains(t, out, "arn")
	assert.Equal(t, int64(0), out["min-capacity"])
	assert.Equal(t, int64(9), out["max-capacity"])
	assert.Equal(t, "desired-role", out["role-arn"])
	assert.Equal(t, map[string]any{"dynamic-scaling-in-suspended": true,
		"dynamic-scaling-out-suspended": false, "scheduled-scaling-suspended": false},
		out["suspended-state"])
	assert.Equal(t, map[string]any{"managed": ""}, out["tags"])
	exec.Inputs["role"], exec.Inputs["suspension"] = nil, nil
	applyPlan(t, exec, savedPlan(t, exec))
	assert.NotContains(t, a.takeCalls(), targetCall{"RegisterScalableTarget", targetKey})
	a.mu.Lock()
	a.targets[targetKey]["RoleARN"] = "unmanaged-role"
	a.targets[targetKey]["SuspendedState"] = map[string]any{
		"DynamicScalingInSuspended": true, "DynamicScalingOutSuspended": true,
		"ScheduledScalingSuspended": true,
	}
	a.mu.Unlock()
	exec.Inputs["max"] = int64(13)
	applyPlan(t, exec, savedPlan(t, exec))
	snapshot, err = exec.Store.Current()
	require.NoError(t, err)
	out = snapshot.Find("resource.target").Outputs
	assert.Equal(t, "unmanaged-role", out["role-arn"])
	assert.Equal(t, map[string]any{"dynamic-scaling-in-suspended": true,
		"dynamic-scaling-out-suspended": true, "scheduled-scaling-suspended": true},
		out["suspended-state"])
}

func TestScalableTargetFailedCreateCleanup(t *testing.T) {
	lib := targetLibrary(t)
	for _, operation := range []string{"DescribeScalableTargets", "ListTagsForResource"} {
		t.Run(operation, func(t *testing.T) {
			a := newTargetAPI(t)
			exec := targetExecutor(t, lib, "evaluated", a, nil)
			plan := savedPlan(t, exec)
			a.fail = operation
			_, err := exec.ApplyPlan(t.Context(), plan)
			require.ErrorContains(t, err, "post-create failure")
			want := []targetCall{{"RegisterScalableTarget", targetKey},
				{"DescribeScalableTargets", targetKey}}
			if operation == "ListTagsForResource" {
				want = append(want, targetCall{"ListTagsForResource", targetKey})
			}
			want = append(want, targetCall{"DeregisterScalableTarget", targetKey},
				targetCall{"DescribeScalableTargets", targetKey})
			assert.Equal(t, want, a.takeCalls())
			a.mu.Lock()
			assert.Nil(t, a.targets[targetKey])
			a.mu.Unlock()
			snapshot, err := exec.Store.Current()
			require.ErrorIs(t, err, state.ErrNoCurrent)
			assert.Nil(t, snapshot)
		})
	}
}
