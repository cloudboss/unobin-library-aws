package codebuild

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

type buildSeed struct {
	Echo any `ub:"echo"`
}

func (r *buildSeed) Run(context.Context, *awscfg.Configuration) (map[string]any, error) {
	return map[string]any{"value": r.Echo}, nil
}

type buildRequest struct {
	Operation string
	Body      map[string]any
	Region    string
}

func buildEndpoint(t *testing.T) (string, func() []buildRequest) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	history := make(chan buildRequest, 50)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		region := "us-east-1"
		if strings.Contains(r.Header.Get("Authorization"), "/us-west-2/") {
			region = "us-west-2"
		}
		if strings.Contains(r.Header.Get("Authorization"), "/unknown-1/") {
			region = "unknown-1"
		}
		op := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "CodeBuild_20161006.")
		history <- buildRequest{op, body, region}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		if op == "StartBuild" {
			_, _ = w.Write([]byte(`{"build":{"id":"started"}}`))
		} else {
			_, _ = w.Write([]byte(`{"builds":[{"id":"different","buildStatus":"SUCCEEDED"}]}`))
		}
	}))
	t.Cleanup(server.Close)
	return server.URL, func() []buildRequest {
		var out []buildRequest
		for len(history) > 0 {
			out = append(out, <-history)
		}
		return out
	}
}

func buildExecutor(t *testing.T, fixture string, schema *runtime.LibrarySchema) (
	*runtime.Executor, *local.Store,
) {
	t.Helper()
	lib := runtime.LibraryWithPath(Library(),
		"github.com/cloudboss/unobin-library-aws//service/codebuild")
	lib.Schema = schema
	lib.Schema.Actions["start-build"].EvaluatedInputs =
		schema.Actions["start-build"].Inputs
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"action.start-build": schema.Actions["start-build"].Constraints,
	}
	libs := map[string]*runtime.Library{
		"codebuild": lib,
		"test": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[buildSeed, map[string]any, *awscfg.Configuration](),
		}},
	}
	path := filepath.Join("testdata", "ub", "start-build", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	store, err := local.NewStore(t.TempDir(), "test-build", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "test-build", Version: "v0", ContentRevision: "test"},
	}, store
}

func savedBuildPlan(t *testing.T, e *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := e.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func buildStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == "action.build" {
			return step
		}
	}
	t.Fatal("build action absent")
	return runtime.PlanStep{}
}

func expectedBuildRequests(body map[string]any, region string) []buildRequest {
	return []buildRequest{
		{"StartBuild", body, region},
		{"BatchGetBuilds", map[string]any{"ids": []any{"started"}}, region},
	}
}

func buildInvalidCases() []struct {
	name, field, path, message string
	value                      any
} {
	return []struct {
		name, field, path, message string
		value                      any
	}{
		{"project", "project", "project-name", "project-name is required", nil},
		{"name", "variables", "environment-variables-override[0].name",
			"environment variable name is required",
			[]any{map[string]any{"name": nil, "value": ""}}},
		{"value", "variables", "environment-variables-override[0].value",
			"environment variable value is required",
			[]any{map[string]any{"name": "", "value": nil}}},
	}
}

func TestBuildNativeRequiredConstraints(t *testing.T) {
	schema := projectSchema(t)
	for _, fixture := range []string{"evaluated", "deferred"} {
		for _, test := range buildInvalidCases() {
			t.Run(fixture+"/"+test.name, func(t *testing.T) {
				endpoint, requests := buildEndpoint(t)
				e, _ := buildExecutor(t, fixture, schema)
				// Isolate extracted predicates from the earlier structural checks.
				schema.Actions["start-build"].EvaluatedInputs = nil
				e.Inputs = map[string]any{"endpoint": endpoint, "project": "", "variables": nil,
					"trigger": "fixed", "version": nil, "region": nil}
				if fixture == "evaluated" {
					_, err := e.ApplyPlan(t.Context(), savedBuildPlan(t, e))
					require.NoError(t, err)
					assert.Equal(t, expectedBuildRequests(map[string]any{"projectName": ""},
						"us-east-1"), requests())
					saved := savedBuildPlan(t, e)
					require.Equal(t, runtime.DecisionSkip, buildStep(t, saved).Decision)
					e.Inputs[test.field] = test.value
					_, err = e.Plan(t.Context())
					require.ErrorContains(t, err, "action.build: schema: constraints[")
					require.ErrorContains(t, err, test.message)
					_, err = e.ApplyPlan(t.Context(), saved)
					require.ErrorContains(t, err, "action.build: schema: constraints[")
					require.ErrorContains(t, err, test.message)
				} else {
					e.Inputs[test.field] = test.value
					saved := savedBuildPlan(t, e)
					assert.Equal(t, map[string][]string{
						"project-name":                   {"action.project.value"},
						"environment-variables-override": {"action.variables.value"},
					}, buildStep(t, saved).UnresolvedInputs)
					e.Inputs = nil
					_, err := e.ApplyPlan(t.Context(), saved)
					require.ErrorContains(t, err, "action.build: schema: constraints[")
					require.ErrorContains(t, err, test.message)
				}
				assert.Empty(t, requests())
			})
		}
	}
}

func TestBuildEvaluatedSavedSkipAndDestroy(t *testing.T) {
	endpoint, requests := buildEndpoint(t)
	e, store := buildExecutor(t, "evaluated", projectSchema(t))
	valid := func() map[string]any {
		return map[string]any{
			"endpoint": endpoint, "project": "", "variables": []any{}, "trigger": "first",
			"version": nil, "region": nil,
		}
	}
	e.Inputs = valid()
	first := savedBuildPlan(t, e)
	assert.Equal(t, runtime.DecisionRerun, buildStep(t, first).Decision)
	e.Inputs = nil
	out, err := e.ApplyPlan(t.Context(), first)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"build-id": "started"}, out.Outputs)
	assert.Equal(t, expectedBuildRequests(map[string]any{
		"projectName": "", "environmentVariablesOverride": []any{},
	}, "us-east-1"), requests())
	e.Inputs = valid()
	skip := savedBuildPlan(t, e)
	assert.Equal(t, runtime.DecisionSkip, buildStep(t, skip).Decision)
	e.Inputs = nil
	unchanged, err := e.ApplyPlan(t.Context(), skip)
	require.NoError(t, err)
	assert.Equal(t, out.Outputs, unchanged.Outputs)
	assert.Empty(t, requests())
	for _, test := range buildInvalidCases() {
		t.Run(test.name, func(t *testing.T) {
			e.Inputs = valid()
			saved := savedBuildPlan(t, e)
			require.Equal(t, runtime.DecisionSkip, buildStep(t, saved).Decision)
			e.Inputs[test.field] = test.value
			_, err := e.Plan(t.Context())
			require.ErrorContains(t, err, "action.build: field \""+test.path+"\"")
			require.ErrorContains(t, err, "required but is null")
			_, err = e.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, "action.build: field \""+test.path+"\"")
			require.ErrorContains(t, err, "required but is null")
			assert.Empty(t, requests())
		})
	}
	e.Inputs = valid()
	e.Inputs["trigger"] = "changed"
	rerun := savedBuildPlan(t, e)
	assert.Equal(t, runtime.DecisionRerun, buildStep(t, rerun).Decision)
	_, err = e.ApplyPlan(t.Context(), rerun)
	require.NoError(t, err)
	assert.Equal(t, expectedBuildRequests(map[string]any{
		"projectName": "", "environmentVariablesOverride": []any{},
	}, "us-east-1"), requests())
	snapshot, err := store.Current()
	require.NoError(t, err)
	snapshot.Find("action.build").Inputs["project-name"] = nil
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	e.Destroy = true
	e.Inputs["project"] = nil
	destroy := savedBuildPlan(t, e)
	assert.Equal(t, runtime.DecisionDestroy, buildStep(t, destroy).Decision)
	_, err = e.ApplyPlan(t.Context(), destroy)
	require.NoError(t, err)
	assert.Empty(t, requests())
	snapshot, err = store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find("action.build"))
}

func TestBuildDeferredRequiredInputs(t *testing.T) {
	schema := projectSchema(t)
	for _, test := range append(buildInvalidCases(),
		struct {
			name, field, path, message string
			value                      any
		}{name: "valid"},
	) {
		t.Run(test.name, func(t *testing.T) {
			endpoint, requests := buildEndpoint(t)
			e, _ := buildExecutor(t, "deferred", schema)
			e.Inputs = map[string]any{"endpoint": endpoint, "project": "", "variables": nil,
				"trigger": "fixed"}
			if test.field != "" {
				e.Inputs[test.field] = test.value
			}
			saved := savedBuildPlan(t, e)
			assert.Equal(t, map[string][]string{
				"project-name":                   {"action.project.value"},
				"environment-variables-override": {"action.variables.value"},
			}, buildStep(t, saved).UnresolvedInputs)
			assert.Empty(t, requests())
			e.Inputs = nil
			out, err := e.ApplyPlan(t.Context(), saved)
			if test.message != "" {
				require.ErrorContains(t, err, "action.build: field \""+test.path+"\"")
				require.ErrorContains(t, err, "required but is null")
				assert.Empty(t, requests())
			} else {
				require.NoError(t, err)
				assert.Equal(t, map[string]any{"build-id": "started"}, out.Outputs)
				assert.Equal(t, expectedBuildRequests(map[string]any{"projectName": ""},
					"us-east-1"), requests())
			}
		})
	}
}

func TestBuildHTTPInputsAndRegion(t *testing.T) {
	schema := projectSchema(t)
	for _, test := range []struct {
		name                  string
		vars, version, region any
		body                  map[string]any
	}{
		{"omitted", nil, nil, nil, map[string]any{"projectName": ""}},
		{"empty", []any{}, "", "", map[string]any{"projectName": "", "sourceVersion": "",
			"environmentVariablesOverride": []any{}}},
		{"populated", []any{map[string]any{"name": "", "value": "", "type": nil},
			map[string]any{"name": "x", "value": "a", "type": ""},
			map[string]any{"name": "x", "value": "b", "type": "custom"}}, "ref", "us-west-2",
			map[string]any{"projectName": "", "sourceVersion": "ref",
				"environmentVariablesOverride": []any{
					map[string]any{"name": "", "value": ""},
					map[string]any{"name": "x", "value": "a"},
					map[string]any{"name": "x", "value": "b", "type": "custom"},
				}}},
		{"unknown region", nil, nil, "unknown-1", map[string]any{"projectName": ""}},
		{"government", nil, nil, "us-gov-west-1", nil},
		{"china", nil, nil, "cn-north-1", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint, requests := buildEndpoint(t)
			e, _ := buildExecutor(t, "evaluated", schema)
			e.Inputs = map[string]any{"endpoint": endpoint, "project": "", "trigger": "fixed",
				"variables": test.vars, "version": test.version, "region": test.region}
			_, err := e.ApplyPlan(t.Context(), savedBuildPlan(t, e))
			if test.body == nil {
				require.ErrorContains(t, err, "outside configured partition")
				assert.Empty(t, requests())
				return
			}
			require.NoError(t, err)
			region := "us-east-1"
			if r, ok := test.region.(string); ok && r != "" {
				region = r
			}
			assert.Equal(t, expectedBuildRequests(test.body, region), requests())
		})
	}
}

func TestBuildLiteralRequests(t *testing.T) {
	endpoint, requests := buildEndpoint(t)
	e, _ := buildExecutor(t, "literal-run", projectSchema(t))
	e.Inputs = map[string]any{"endpoint": endpoint}
	saved := savedBuildPlan(t, e)
	e.Inputs = nil
	out, err := e.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"build-id": "started"}, out.Outputs)
	want := expectedBuildRequests(map[string]any{"projectName": "omitted"}, "us-east-1")
	want = append(want, expectedBuildRequests(map[string]any{"projectName": "nulls"},
		"us-east-1")...)
	want = append(want, expectedBuildRequests(map[string]any{"projectName": "empty",
		"sourceVersion": "", "environmentVariablesOverride": []any{}}, "us-east-1")...)
	want = append(want, expectedBuildRequests(map[string]any{"projectName": "", "sourceVersion": "ref",
		"environmentVariablesOverride": []any{
			map[string]any{"name": "", "value": ""},
			map[string]any{"name": "x", "value": "a"},
			map[string]any{"name": "x", "value": "b"},
			map[string]any{"name": "x", "value": "c", "type": "custom"},
		}}, "us-east-1")...)
	assert.ElementsMatch(t, want, requests())
}
