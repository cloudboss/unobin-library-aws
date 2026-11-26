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

type policyAPI struct {
	mu       sync.Mutex
	policies map[string]map[string]any
	calls    []targetCall
	endpoint string
	fail     string
}

func newPolicyAPI(t *testing.T) *policyAPI {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	a := &policyAPI{policies: map[string]map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	t.Cleanup(server.Close)
	a.endpoint = server.URL
	return a
}

func policyAPIKey(input map[string]any) string {
	name := input["PolicyName"]
	if name == nil {
		name = input["PolicyNames"].([]any)[0]
	}
	return fmt.Sprintf("%s|%s|%s|%s", input["ServiceNamespace"], input["ResourceId"],
		input["ScalableDimension"], name)
}

func (a *policyAPI) serve(w http.ResponseWriter, req *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	var in map[string]any
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	op := strings.TrimPrefix(req.Header.Get("X-Amz-Target"), "AnyScaleFrontendService.")
	if op == "RegisterScalableTarget" || op == "DescribeScalableTargets" ||
		op == "ListTagsForResource" {
		a.calls = append(a.calls, targetCall{op, "seed"})
		out := map[string]any{}
		switch op {
		case "DescribeScalableTargets":
			out["ScalableTargets"] = []any{map[string]any{
				"ResourceId": "seed", "ServiceNamespace": "dynamodb",
				"ScalableDimension": "dynamodb:table:ReadCapacityUnits", "MinCapacity": 0, "MaxCapacity": 9,
				"ScalableTargetARN": "seed"}}
		case "ListTagsForResource":
			out["Tags"] = map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	key := policyAPIKey(in)
	a.calls = append(a.calls, targetCall{op, key})
	if a.fail == op {
		a.fail = ""
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]any{"__type": "ValidationException",
			"message": "failed read"})
		return
	}
	out := map[string]any{}
	switch op {
	case "PutScalingPolicy":
		if a.policies[key] == nil {
			a.policies[key] = map[string]any{"PolicyARN": key}
		}
		maps.Copy(a.policies[key], in)
	case "DescribeScalingPolicies":
		if policy := a.policies[key]; policy != nil {
			out["ScalingPolicies"] = []any{policy}
		}
	case "DeleteScalingPolicy":
		delete(a.policies, key)
	default:
		http.Error(w, "unexpected "+op, 400)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (a *policyAPI) takeCalls() []targetCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	calls := a.calls
	a.calls = nil
	return calls
}

func policyExecutor(t *testing.T, library *runtime.Library, fixture string,
	a *policyAPI, store state.Backend) *runtime.Executor {
	t.Helper()
	if store == nil {
		var err error
		store, err = local.NewStore(t.TempDir(), "policy", "test", encrypters.Noop{})
		require.NoError(t, err)
	}
	path := filepath.Join("testdata", "ub", "policy", fixture, "factory.ub")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, body)
	require.NoError(t, err)
	source := file.Factory.Body
	libs := map[string]*runtime.Library{"aas": library}
	checker := check.NewSyntax(source, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	fields := []string{"name", "resource-id", "service-namespace", "scalable-dimension", "policy-type",
		"step-scaling-policy-configuration", "target-tracking-scaling-policy-configuration",
		"predictive-scaling-policy-configuration"}
	selectors := map[string]any{}
	for _, field := range fields {
		selectors[field] = "preferred"
	}
	return &runtime.Executor{DAG: checker.DAG(), SyntaxSource: &source, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "policy", Version: "v0", ContentRevision: "p0"},
		Inputs: map[string]any{"endpoint": a.endpoint, "name": "policy", "resource-id": "table/a/b",
			"service-namespace": "dynamodb", "scalable-dimension": "dynamodb:table:ReadCapacityUnits",
			"policy-type": nil, "step-scaling-policy-configuration": nil,
			"target-tracking-scaling-policy-configuration": nil,
			"predictive-scaling-policy-configuration":      nil, "selectors": selectors}}
}

func policyStep(t *testing.T, plan *runtime.PlanFile) *runtime.PlanStep {
	t.Helper()
	for i := range plan.Steps {
		if plan.Steps[i].Address == "resource.policy" {
			return &plan.Steps[i]
		}
	}
	t.Fatal("policy step absent")
	return nil
}

const policyKey = "dynamodb|table/a/b|dynamodb:table:ReadCapacityUnits|policy"

func TestScalingPolicyNativeIdentitiesAndEnum(t *testing.T) {
	lib := targetLibrary(t)
	for _, field := range []string{"name", "resource-id", "service-namespace", "scalable-dimension"} {
		t.Run(field, func(t *testing.T) {
			a := newPolicyAPI(t)
			exec := policyExecutor(t, lib, "evaluated", a, nil)
			exec.Inputs["selectors"].(map[string]any)[field] = "absent"
			_, err := exec.Plan(t.Context())
			require.ErrorContains(t, err, "resource.policy: schema: constraints[")
			require.ErrorContains(t, err, "must be non-null")
			assert.Empty(t, a.takeCalls())
		})
	}
	a := newPolicyAPI(t)
	exec := policyExecutor(t, lib, "evaluated", a, nil)
	exec.Inputs["policy-type"] = "invalid"
	_, err := exec.Plan(t.Context())
	require.ErrorContains(t, err, "resource.policy: schema: constraints[")
	require.ErrorContains(t, err, "PolicyType is invalid")
	assert.Empty(t, a.takeCalls())
}

func TestScalingPolicyReplacementAndObservedDrift(t *testing.T) {
	lib := targetLibrary(t)
	for index, field := range []string{"name",
		"resource-id",
		"service-namespace",
		"scalable-dimension"} {
		t.Run(field, func(t *testing.T) {
			a := newPolicyAPI(t)
			exec := policyExecutor(t, lib, "evaluated", a, nil)
			applyPlan(t, exec, savedPlan(t, exec))
			a.takeCalls()
			exec.Inputs[field] = "new"
			plan := savedPlan(t, exec)
			assert.Equal(t, runtime.DecisionReplace, policyStep(t, plan).Decision)
			assert.Equal(t, []string{field}, policyStep(t, plan).ReplacementReasons)
			a.takeCalls()
			applyPlan(t, exec, plan)
			parts := []string{"policy", "table/a/b", "dynamodb", "dynamodb:table:ReadCapacityUnits"}
			parts[index] = "new"
			newKey := strings.Join([]string{parts[2], parts[1], parts[3], parts[0]}, "|")
			assert.Equal(t, []targetCall{{"DescribeScalingPolicies", policyKey},
				{"DeleteScalingPolicy", policyKey}, {"PutScalingPolicy", newKey},
				{"DescribeScalingPolicies", newKey}}, a.takeCalls())
		})
	}
	a := newPolicyAPI(t)
	exec := policyExecutor(t, lib, "evaluated", a, nil)
	exec.Inputs["policy-type"] = "TargetTrackingScaling"
	exec.Inputs["target-tracking-scaling-policy-configuration"] = map[string]any{
		"target-value": float64(50),
	}
	applyPlan(t, exec, savedPlan(t, exec))
	a.takeCalls()
	a.mu.Lock()
	tracking := a.policies[policyKey]["TargetTrackingScalingPolicyConfiguration"].(map[string]any)
	tracking["TargetValue"] = 20
	a.mu.Unlock()
	plan := savedPlan(t, exec)
	assert.Equal(t, runtime.DecisionUpdate, policyStep(t, plan).Decision)
	a.takeCalls()
	applyPlan(t, exec, plan)
	assert.Equal(t,
		[]targetCall{{"PutScalingPolicy",
			policyKey},
			{"DescribeScalingPolicies",
				policyKey}},
		a.takeCalls())
	snap, err := exec.Store.Current()
	require.NoError(t, err)
	out := snap.Find("resource.policy").Outputs
	assert.Equal(t, policyKey, out["policy-arn"])
	assert.NotContains(t, out, "arn")
	trackingOutput := out["target-tracking-scaling-policy-configuration"].(map[string]any)
	assert.Equal(t, int64(50), trackingOutput["target-value"])
	exec.Inputs["target-tracking-scaling-policy-configuration"] = nil
	applyPlan(t, exec, savedPlan(t, exec))
	assert.NotContains(t, a.takeCalls(), targetCall{"PutScalingPolicy", policyKey})
}

func TestScalingPolicySavedNoOpAndHistoricalDestroy(t *testing.T) {
	lib := targetLibrary(t)
	a := newPolicyAPI(t)
	exec := policyExecutor(t, lib, "evaluated", a, nil)
	applyPlan(t, exec, savedPlan(t, exec))
	a.takeCalls()
	valid := savedPlan(t, exec)
	assert.Equal(t, runtime.DecisionNoOp, policyStep(t, valid).Decision)
	a.takeCalls()
	applyPlan(t, exec, valid)
	assert.Empty(t, a.takeCalls())
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	snapshot.Find("resource.policy").Inputs["name"] = nil
	revision, err := exec.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, exec.Store.SetCurrent(revision))
	exec.Inputs["selectors"].(map[string]any)["name"] = "absent"
	const export = "resource.scaling-policy"
	rules := lib.Constraints[export]
	delete(lib.Constraints, export)
	invalid := savedPlan(t, exec)
	lib.Constraints[export] = rules
	assert.Equal(t, runtime.DecisionNoOp, policyStep(t, invalid).Decision)
	a.takeCalls()
	_, err = exec.Plan(t.Context())
	require.ErrorContains(t, err, "resource.policy: schema: constraints[")
	a.takeCalls()
	fresh := policyExecutor(t, lib, "evaluated", a, exec.Store)
	fresh.Inputs = nil
	_, err = fresh.ApplyPlan(t.Context(), invalid)
	require.ErrorContains(t, err, "resource.policy: schema: constraints[")
	assert.Empty(t, a.takeCalls())
	exec.Destroy = true
	applyPlan(t, exec, savedPlan(t, exec))
	assert.Contains(t, a.takeCalls(), targetCall{"DeleteScalingPolicy", policyKey})
}

func TestScalingPolicyDeferredSavedReplacement(t *testing.T) {
	lib := targetLibrary(t)
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			a := newPolicyAPI(t)
			initial := policyExecutor(t, lib, "evaluated", a, nil)
			applyPlan(t, initial, savedPlan(t, initial))
			a.takeCalls()
			exec := policyExecutor(t, lib, "deferred", a, initial.Store)
			exec.Inputs["name"] = "new"
			if invalid {
				exec.Inputs["selectors"].(map[string]any)["name"] = "absent"
			}
			plan := savedPlan(t, exec)
			assert.Equal(t, []string{"resource.seed.scalable-target-arn"},
				policyStep(t, plan).UnresolvedInputs["name"])
			a.takeCalls()
			fresh := policyExecutor(t, lib, "deferred", a, initial.Store)
			fresh.Inputs = nil
			_, err := fresh.ApplyPlan(t.Context(), plan)
			calls := a.takeCalls()
			if invalid {
				require.ErrorContains(t, err, "resource.policy: schema: constraints[")
				assert.Equal(t, []targetCall{{"RegisterScalableTarget", "seed"},
					{"DescribeScalableTargets", "seed"}, {"ListTagsForResource", "seed"}}, calls)
			} else {
				require.NoError(t, err)
				assert.Contains(t, calls, targetCall{"DeleteScalingPolicy", policyKey})
				assert.Contains(t, calls, targetCall{"PutScalingPolicy",
					"dynamodb|table/a/b|dynamodb:table:ReadCapacityUnits|new"})
			}
		})
	}
}

func TestScalingPolicyNativeNestedRules(t *testing.T) {
	lib := targetLibrary(t)
	type testCase struct {
		name    string
		field   string
		value   any
		message string
	}
	cases := []testCase{
		{"name-bytes", "name", strings.Repeat("é", 128), "name must be at most 255 bytes"},
		{"adjustment-enum", "step-scaling-policy-configuration",
			map[string]any{"adjustment-type": "changeincapacity"}, "AdjustmentType is invalid"},
		{"aggregation-enum", "step-scaling-policy-configuration",
			map[string]any{"metric-aggregation-type": "average"}, "MetricAggregationType is invalid"},
		{"null-step-entry", "step-scaling-policy-configuration",
			map[string]any{"step-adjustment": []any{nil}}, "scaling-adjustment must be non-null"},
		{"null-step-value", "step-scaling-policy-configuration",
			map[string]any{"step-adjustment": []any{map[string]any{"scaling-adjustment": nil}}},
			"scaling-adjustment must be non-null"},
		{"null-target", "target-tracking-scaling-policy-configuration",
			map[string]any{"target-value": nil}, "target-value must be non-null"},
		{"target-conflict", "target-tracking-scaling-policy-configuration",
			map[string]any{"target-value": 0, "customized-metric-specification": map[string]any{},
				"predefined-metric-specification": map[string]any{"predefined-metric-type": ""}},
			"at most one"},
		{"statistic-enum", "target-tracking-scaling-policy-configuration",
			map[string]any{"target-value": 0, "customized-metric-specification": map[string]any{
				"statistic": "sum"}}, "Statistic is invalid"},
		{"target-label-bytes", "target-tracking-scaling-policy-configuration",
			map[string]any{"target-value": 0, "predefined-metric-specification": map[string]any{
				"predefined-metric-type": "", "resource-label": strings.Repeat("x", 1024)}},
			"ResourceLabel byte length is invalid"},
		{"target-null-predefined-type", "target-tracking-scaling-policy-configuration",
			map[string]any{"target-value": 0, "predefined-metric-specification": map[string]any{
				"predefined-metric-type": nil}}, "predefined-metric-type must be non-null"},
		{"predictive-empty-specs", "predictive-scaling-policy-configuration",
			map[string]any{"metric-specification": []any{}}, "metric-specification must be nonempty"},
		{"predictive-null-spec", "predictive-scaling-policy-configuration",
			map[string]any{"metric-specification": []any{nil}},
			"metric-specification entries must be non-null"},
		{"predictive-mode", "predictive-scaling-policy-configuration",
			map[string]any{"metric-specification": []any{map[string]any{}}, "mode": "forecast"},
			"Mode is invalid"},
		{"predictive-breach", "predictive-scaling-policy-configuration",
			map[string]any{"metric-specification": []any{map[string]any{}},
				"max-capacity-breach-behavior": "other"}, "MaxCapacityBreachBehavior is invalid"},
	}
	for _, field := range []string{"dimensions", "metric-name", "namespace", "statistic", "unit"} {
		value := any("")
		if field == "dimensions" {
			value = []any{}
		}
		custom := map[string]any{"metrics": []any{map[string]any{"id": "m"}}, field: value}
		if field == "statistic" {
			custom[field] = "Sum"
		}
		cases = append(cases, testCase{"metric-conflict-" + field,
			"target-tracking-scaling-policy-configuration", map[string]any{"target-value": 0,
				"customized-metric-specification": custom}, "forbidden"})
	}
	for _, field := range []string{"max-capacity-buffer", "scheduling-buffer-time"} {
		for _, value := range []int64{-1, 3601} {
			cases = append(cases,
				testCase{fmt.Sprint(field,
					value),
					"predictive-scaling-policy-configuration",
					map[string]any{"metric-specification": []any{map[string]any{}},
						field: value},
					"permitted range"})
		}
	}
	for _, field := range []string{"id", "expression", "label"} {
		for _, value := range []string{"", strings.Repeat("é", 1024)} {
			query := map[string]any{"id": "m", field: value}
			cases = append(cases, testCase{fmt.Sprint(field, len(value)),
				"target-tracking-scaling-policy-configuration", map[string]any{"target-value": 0,
					"customized-metric-specification": map[string]any{"metrics": []any{query}}},
				"byte length is invalid"})
		}
	}
	for _, variant := range []string{"customized-capacity-metric-specification",
		"customized-load-metric-specification", "customized-scaling-metric-specification"} {
		for _, queries := range []any{[]any{}, []any{map[string]any{"id": "", "expression": ""}}} {
			cases = append(cases,
				testCase{variant + fmt.Sprint(queries),
					"predictive-scaling-policy-configuration",
					map[string]any{"metric-specification": []any{
						map[string]any{variant: map[string]any{"metric-data-query": queries}},
					}},
					""})
		}
	}
	for _, variant := range []string{"predefined-load-metric-specification",
		"predefined-metric-pair-specification", "predefined-scaling-metric-specification"} {
		for _, field := range []string{"predefined-metric-type", "resource-label"} {
			predefined := map[string]any{"predefined-metric-type": "valid", field: ""}
			cases = append(cases, testCase{variant + field, "predictive-scaling-policy-configuration",
				map[string]any{"metric-specification": []any{map[string]any{variant: predefined}}},
				"byte length is invalid"})
		}
	}
	for _, query := range []any{
		nil,
		map[string]any{"id": nil},
		map[string]any{"id": "m", "metric-stat": map[string]any{"stat": nil, "metric": map[string]any{
			"metric-name": "", "namespace": ""}}},
		map[string]any{"id": "m", "metric-stat": map[string]any{"stat": "Average", "metric": nil}},
		map[string]any{"id": "m",
			"metric-stat": map[string]any{"stat": "Average",
				"metric": map[string]any{
					"metric-name": nil, "namespace": ""}}},
		map[string]any{"id": "m",
			"metric-stat": map[string]any{"stat": "Average",
				"metric": map[string]any{
					"metric-name": "", "namespace": nil}}},
		map[string]any{"id": "m", "metric-stat": map[string]any{"stat": "", "metric": map[string]any{
			"metric-name": "", "namespace": ""}}},
		map[string]any{"id": "m", "metric-stat": map[string]any{"stat": strings.Repeat("x", 101),
			"metric": map[string]any{"metric-name": "", "namespace": ""}}},
	} {
		cases = append(cases, testCase{fmt.Sprint("target-query", query),
			"target-tracking-scaling-policy-configuration", map[string]any{"target-value": 0,
				"customized-metric-specification": map[string]any{"metrics": []any{query}}}, ""})
	}
	for _, dimension := range []any{nil, map[string]any{"name": nil, "value": ""},
		map[string]any{"name": "", "value": nil}} {
		cases = append(cases, testCase{fmt.Sprint("target-dimension", dimension),
			"target-tracking-scaling-policy-configuration", map[string]any{"target-value": 0,
				"customized-metric-specification": map[string]any{"dimensions": []any{dimension}}}, ""})
	}
	for _, variant := range []string{"customized-capacity-metric-specification",
		"customized-load-metric-specification", "customized-scaling-metric-specification"} {
		for _, query := range []any{nil, map[string]any{"id": nil},
			map[string]any{"id": "", "metric-stat": map[string]any{"stat": nil, "metric": map[string]any{}}},
			map[string]any{"id": "", "metric-stat": map[string]any{"stat": "", "metric": nil}},
			map[string]any{"id": "", "metric-stat": map[string]any{"stat": "", "metric": map[string]any{
				"dimension": []any{nil}}}},
		} {
			cases = append(cases,
				testCase{fmt.Sprint(variant,
					query),
					"predictive-scaling-policy-configuration",
					map[string]any{"metric-specification": []any{map[string]any{variant: map[string]any{
						"metric-data-query": []any{query}}}}}, ""})
		}
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			a := newPolicyAPI(t)
			exec := policyExecutor(t, lib, "evaluated", a, nil)
			exec.Inputs[tt.field] = tt.value
			_, err := exec.Plan(t.Context())
			require.ErrorContains(t, err, "resource.policy: schema: constraints[")
			if tt.message != "" {
				require.ErrorContains(t, err, tt.message)
			}
			assert.Empty(t, a.takeCalls())
		})
	}
}

func TestScalingPolicyNativeBoundaryValues(t *testing.T) {
	lib := targetLibrary(t)
	a := newPolicyAPI(t)
	exec := policyExecutor(t, lib, "evaluated", a, nil)
	exec.Inputs["name"] = strings.Repeat("é", 127) + "x"
	exec.Inputs["target-tracking-scaling-policy-configuration"] = map[string]any{
		"target-value": 0, "customized-metric-specification": map[string]any{
			"metrics": []any{map[string]any{"id": strings.Repeat("x", 255),
				"expression": strings.Repeat("x", 2047), "label": strings.Repeat("x", 2047),
				"metric-stat": map[string]any{"stat": strings.Repeat("x", 100), "metric": map[string]any{
					"metric-name": "", "namespace": ""}}}}}}
	plan := savedPlan(t, exec)
	assert.Equal(t, runtime.DecisionCreate, policyStep(t, plan).Decision)
	assert.Empty(t, a.takeCalls())
	exec.Inputs["target-tracking-scaling-policy-configuration"] = map[string]any{
		"target-value": 0, "predefined-metric-specification": map[string]any{
			"predefined-metric-type": "", "resource-label": strings.Repeat("x", 1023)}}
	savedPlan(t, exec)
	for _, variant := range []string{"predefined-load-metric-specification",
		"predefined-metric-pair-specification", "predefined-scaling-metric-specification"} {
		exec.Inputs["predictive-scaling-policy-configuration"] = map[string]any{
			"max-capacity-buffer": int64(100), "scheduling-buffer-time": int64(3600),
			"metric-specification": []any{map[string]any{variant: map[string]any{
				"predefined-metric-type": strings.Repeat("x",
					128),
				"resource-label": strings.Repeat("x",
					1023)}}}}
		savedPlan(t, exec)
	}
	for _, variant := range []string{"customized-capacity-metric-specification",
		"customized-load-metric-specification", "customized-scaling-metric-specification"} {
		exec.Inputs["predictive-scaling-policy-configuration"] = map[string]any{
			"max-capacity-buffer": int64(0), "scheduling-buffer-time": int64(0),
			"metric-specification": []any{map[string]any{variant: map[string]any{
				"metric-data-query": []any{map[string]any{"id": "", "expression": strings.Repeat("x", 2048),
					"label": "", "metric-stat": map[string]any{"stat": "", "metric": map[string]any{
						"metric-name": "", "namespace": ""}}}}}}}}
		savedPlan(t, exec)
	}
	assert.Empty(t, a.takeCalls())
}

func TestScalingPolicySourceSetsAndOrderedPredictiveLists(t *testing.T) {
	lib := targetLibrary(t)
	a := newPolicyAPI(t)
	exec := policyExecutor(t, lib, "evaluated", a, nil)
	first := map[string]any{"scaling-adjustment": int64(1), "metric-interval-lower-bound": float64(0)}
	second := map[string]any{"scaling-adjustment": int64(-1),
		"metric-interval-upper-bound": float64(-0.5)}
	step := map[string]any{"step-adjustment": []any{first, second}}
	exec.Inputs["step-scaling-policy-configuration"] = step
	applyPlan(t, exec, savedPlan(t, exec))
	a.takeCalls()
	step["step-adjustment"] = []any{second, first}
	plan := savedPlan(t, exec)
	assert.Equal(t, runtime.DecisionNoOp, policyStep(t, plan).Decision)
	a.takeCalls()
	applyPlan(t, exec, plan)
	assert.Empty(t, a.takeCalls())
	exec.Inputs["step-scaling-policy-configuration"] = nil
	query := map[string]any{"id": "metric", "metric-stat": map[string]any{
		"stat": "Sum", "metric": map[string]any{"metric-name": "name", "namespace": "NS",
			"dimensions": []any{map[string]any{"name": "z", "value": "v"},
				map[string]any{"name": "a", "value": "v"}}}}}
	expression := map[string]any{"id": "expression", "expression": "SUM(metric)"}
	custom := map[string]any{"metrics": []any{query, expression}}
	exec.Inputs["target-tracking-scaling-policy-configuration"] = map[string]any{
		"target-value": float64(50), "customized-metric-specification": custom}
	applyPlan(t, exec, savedPlan(t, exec))
	a.takeCalls()
	custom["metrics"] = []any{expression, query}
	metric := query["metric-stat"].(map[string]any)["metric"].(map[string]any)
	dimensions := metric["dimensions"].([]any)
	metric["dimensions"] = []any{dimensions[1], dimensions[0]}
	plan = savedPlan(t, exec)
	assert.Equal(t, runtime.DecisionNoOp, policyStep(t, plan).Decision)
	a.takeCalls()
	applyPlan(t, exec, plan)
	assert.Empty(t, a.takeCalls())
	exec.Inputs["target-tracking-scaling-policy-configuration"] = nil
	exec.Inputs["policy-type"] = "PredictiveScaling"
	predictive := map[string]any{"metric-specification": []any{
		map[string]any{"target-value": float64(0)}, map[string]any{"target-value": float64(1)}}}
	exec.Inputs["predictive-scaling-policy-configuration"] = predictive
	applyPlan(t, exec, savedPlan(t, exec))
	a.takeCalls()
	specifications := predictive["metric-specification"].([]any)
	predictive["metric-specification"] = []any{specifications[1], specifications[0]}
	plan = savedPlan(t, exec)
	assert.Equal(t, runtime.DecisionUpdate, policyStep(t, plan).Decision)
	a.takeCalls()
	applyPlan(t, exec, plan)
	assert.Contains(t, a.takeCalls(), targetCall{"PutScalingPolicy", policyKey})
}

func TestScalingPolicyOptionalEmptyCollectionsNativeNoOp(t *testing.T) {
	lib := targetLibrary(t)
	for _, family := range []string{"step", "inline", "empty-queries", "query", "predictive"} {
		t.Run(family, func(t *testing.T) {
			a := newPolicyAPI(t)
			exec := policyExecutor(t, lib, "evaluated", a, nil)
			var optional map[string]any
			var field string
			switch family {
			case "step":
				optional, field = map[string]any{}, "step-adjustment"
				exec.Inputs["step-scaling-policy-configuration"] = optional
			case "inline":
				optional, field = map[string]any{}, "dimensions"
				exec.Inputs["target-tracking-scaling-policy-configuration"] = map[string]any{
					"target-value": 0, "customized-metric-specification": optional}
			case "empty-queries":
				optional, field = map[string]any{}, "metrics"
				exec.Inputs["target-tracking-scaling-policy-configuration"] = map[string]any{
					"target-value": 0, "customized-metric-specification": optional}
			case "query":
				optional, field = map[string]any{"metric-name": "name", "namespace": "NS"}, "dimensions"
				exec.Inputs["target-tracking-scaling-policy-configuration"] = map[string]any{
					"target-value": 0, "customized-metric-specification": map[string]any{
						"metrics": []any{map[string]any{"id": "m", "metric-stat": map[string]any{
							"stat": "Sum", "metric": optional}}}}}
			case "predictive":
				optional, field = map[string]any{"metric-name": "", "namespace": ""}, "dimension"
				exec.Inputs["predictive-scaling-policy-configuration"] = map[string]any{
					"metric-specification": []any{map[string]any{"target-value": 1,
						"customized-load-metric-specification": map[string]any{
							"metric-data-query": []any{map[string]any{"id": "m", "metric-stat": map[string]any{
								"stat": "Sum", "metric": optional}}}}}}}
			}
			applyPlan(t, exec, savedPlan(t, exec))
			a.takeCalls()
			optional[field] = []any{}
			plan := savedPlan(t, exec)
			assert.Equal(t, runtime.DecisionNoOp, policyStep(t, plan).Decision)
			assert.Equal(t, []targetCall{{"DescribeScalingPolicies", policyKey}}, a.takeCalls())
			applyPlan(t, exec, plan)
			assert.Empty(t, a.takeCalls())
			delete(optional, field)
			plan = savedPlan(t, exec)
			assert.Equal(t, runtime.DecisionNoOp, policyStep(t, plan).Decision)
			a.takeCalls()
			applyPlan(t, exec, plan)
			assert.Empty(t, a.takeCalls())
		})
	}
}

func TestScalingPolicyFailedCreateCleanup(t *testing.T) {
	lib := targetLibrary(t)
	a := newPolicyAPI(t)
	exec := policyExecutor(t, lib, "evaluated", a, nil)
	plan := savedPlan(t, exec)
	a.fail = "DescribeScalingPolicies"
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.ErrorContains(t, err, "failed read")
	assert.Equal(t, []targetCall{{"PutScalingPolicy", policyKey},
		{"DescribeScalingPolicies", policyKey}, {"DeleteScalingPolicy", policyKey}}, a.takeCalls())
	a.mu.Lock()
	assert.Empty(t, a.policies)
	a.mu.Unlock()
	snapshot, err := exec.Store.Current()
	require.ErrorIs(t, err, state.ErrNoCurrent)
	assert.Nil(t, snapshot)
}

func TestScalingPolicyNestedSavedNoOpAndDeferredRules(t *testing.T) {
	cases := []struct {
		name, field, message string
		value                any
	}{
		{"enum", "policy-type", "PolicyType is invalid", "invalid"},
		{"conflict", "target-tracking-scaling-policy-configuration", "at most one",
			map[string]any{"target-value": float64(0), "customized-metric-specification": map[string]any{},
				"predefined-metric-specification": map[string]any{"predefined-metric-type": ""}}},
		{"bound", "predictive-scaling-policy-configuration", "permitted range",
			map[string]any{"metric-specification": []any{map[string]any{}},
				"scheduling-buffer-time": int64(3601)}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			lib := targetLibrary(t)
			a := newPolicyAPI(t)
			initial := policyExecutor(t, lib, "evaluated", a, nil)
			applyPlan(t, initial, savedPlan(t, initial))
			a.takeCalls()
			deferred := policyExecutor(t, lib, "deferred", a, initial.Store)
			deferred.Inputs["name"] = "replacement"
			deferred.Inputs[tt.field] = tt.value
			plan := savedPlan(t, deferred)
			assert.Equal(t, []string{"resource.seed.scalable-target-arn"},
				policyStep(t, plan).UnresolvedInputs[tt.field])
			a.takeCalls()
			fresh := policyExecutor(t, lib, "deferred", a, initial.Store)
			fresh.Inputs = nil
			_, err := fresh.ApplyPlan(t.Context(), plan)
			require.ErrorContains(t, err, "resource.policy: schema: constraints[")
			require.ErrorContains(t, err, tt.message)
			assert.Equal(t, []targetCall{{"RegisterScalableTarget", "seed"},
				{"DescribeScalableTargets", "seed"}, {"ListTagsForResource", "seed"}}, a.takeCalls())
			a = newPolicyAPI(t)
			initial = policyExecutor(t, lib, "evaluated", a, nil)
			applyPlan(t, initial, savedPlan(t, initial))
			a.takeCalls()
			snapshot, err := initial.Store.Current()
			require.NoError(t, err)
			snapshot.Find("resource.policy").Inputs[tt.field] = tt.value
			revision, err := initial.Store.Write(snapshot)
			require.NoError(t, err)
			require.NoError(t, initial.Store.SetCurrent(revision))
			initial.Inputs[tt.field] = tt.value
			const export = "resource.scaling-policy"
			rules := lib.Constraints[export]
			delete(lib.Constraints, export)
			noOp := savedPlan(t, initial)
			lib.Constraints[export] = rules
			assert.Equal(t, runtime.DecisionNoOp, policyStep(t, noOp).Decision)
			a.takeCalls()
			_, err = initial.Plan(t.Context())
			require.ErrorContains(t, err, "resource.policy: schema: constraints[")
			require.ErrorContains(t, err, tt.message)
			a.takeCalls()
			fresh = policyExecutor(t, lib, "evaluated", a, initial.Store)
			fresh.Inputs = nil
			_, err = fresh.ApplyPlan(t.Context(), noOp)
			require.ErrorContains(t, err, "resource.policy: schema: constraints[")
			require.ErrorContains(t, err, tt.message)
			assert.Empty(t, a.takeCalls())
		})
	}
}
