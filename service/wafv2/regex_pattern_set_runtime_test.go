package wafv2_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	awswafv2 "github.com/cloudboss/unobin-library-aws/service/wafv2"
)

const (
	regexPatternSetAddress     = "resource.patterns"
	regexPatternSetLibraryPath = "github.com/cloudboss/unobin-library-aws//service/wafv2"
)

type regexPatternSetRequest struct {
	operation string
	endpoint  string
	region    string
	body      map[string]any
}

type regexPatternSetCloud struct {
	mu           sync.Mutex
	calls        []regexPatternSetRequest
	sets         map[string]map[string]any
	nextID       int
	getError     string
	tagError     string
	deleteError  string
	onGet        func()
	omitGetToken bool
}

func newRegexPatternSetCloud(t *testing.T) *regexPatternSetCloud {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "us-west-2")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing-credentials"))
	return &regexPatternSetCloud{sets: map[string]map[string]any{}}
}

func (c *regexPatternSetCloud) endpoint(t *testing.T, label string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode WAF request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		signing := strings.SplitN(r.Header.Get("Authorization"), "/", 5)
		if !assert.Len(t, signing, 5, "SigV4 credentials") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, operation, _ := strings.Cut(r.Header.Get("X-Amz-Target"), ".")
		call := regexPatternSetRequest{
			operation: operation, endpoint: label, region: signing[2], body: body,
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		c.mu.Lock()
		defer c.mu.Unlock()
		c.calls = append(c.calls, call)
		response, code := c.reply(call)
		if code != "" {
			w.WriteHeader(http.StatusBadRequest)
			response = map[string]any{"__type": code, "message": "test WAF response"}
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode WAF response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func (c *regexPatternSetCloud) reply(call regexPatternSetRequest) (map[string]any, string) {
	body := call.body
	key := fmt.Sprint(call.region, "/", body["Scope"], "/", body["Name"])
	set := c.sets[key]
	switch call.operation {
	case "CreateRegexPatternSet":
		c.nextID++
		id := fmt.Sprintf("id-%d", c.nextID)
		scopePart := "regional"
		if body["Scope"] == "CLOUDFRONT" {
			scopePart = "global"
		}
		arn := fmt.Sprintf("arn:aws:wafv2:%s:123456789012:%s/regexpatternset/%s/%s",
			call.region, scopePart, body["Name"], id)
		c.sets[key] = map[string]any{
			"Id": id, "ARN": arn, "Name": body["Name"],
			"RegularExpressionList": body["RegularExpressionList"], "Description": body["Description"],
		}
		return map[string]any{"Summary": map[string]any{
			"Id": id, "ARN": arn, "LockToken": "observed-token",
		}}, ""
	case "GetRegexPatternSet":
		if c.onGet != nil {
			c.onGet()
		}
		if c.getError != "" {
			return nil, c.getError
		}
		if set == nil || set["Id"] != body["Id"] {
			return nil, "WAFNonexistentItemException"
		}
		if c.omitGetToken {
			return map[string]any{"RegexPatternSet": set}, ""
		}
		return map[string]any{"RegexPatternSet": set, "LockToken": "observed-token"}, ""
	case "UpdateRegexPatternSet":
		if set == nil || set["Id"] != body["Id"] {
			return nil, "WAFNonexistentItemException"
		}
		set["RegularExpressionList"] = body["RegularExpressionList"]
		set["Description"] = body["Description"]
		return map[string]any{"NextLockToken": "unused-token"}, ""
	case "DeleteRegexPatternSet":
		if c.deleteError != "" {
			return nil, c.deleteError
		}
		if set == nil || set["Id"] != body["Id"] {
			return nil, "WAFNonexistentItemException"
		}
		delete(c.sets, key)
		return map[string]any{}, ""
	case "ListTagsForResource":
		if c.tagError != "" {
			return nil, c.tagError
		}
		return map[string]any{}, ""
	default:
		return nil, "UnexpectedOperation"
	}
}

func (c *regexPatternSetCloud) takeCalls() []regexPatternSetRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls = nil
	return calls
}

type regexPatternSetSeed struct {
	NextName  *string
	NextScope *string
	NextRegex *string
}

type regexPatternSetSeedOutput struct {
	Name  *string
	Scope *string
	Regex *string
}

func (s *regexPatternSetSeed) Run(
	context.Context, runtime.NoConfig,
) (*regexPatternSetSeedOutput, error) {
	return &regexPatternSetSeedOutput{Name: s.NextName, Scope: s.NextScope, Regex: s.NextRegex}, nil
}

func regexPatternSetRuntimeLibrary(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	library := runtime.LibraryWithPath(awswafv2.Library(), regexPatternSetLibraryPath)
	library.Schema = readLibrarySchema(t)
	schema := library.Schema.Resources["regex-pattern-set"]
	schema.EvaluatedInputs = maps.Clone(schema.Inputs)
	library.Constraints = map[string][]lang.ConstraintSpec{
		"resource.regex-pattern-set": library.Schema.Resources["regex-pattern-set"].Constraints,
	}
	return map[string]*runtime.Library{
		"aws-wafv2": library,
		"fixture": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[
				regexPatternSetSeed, *regexPatternSetSeedOutput, runtime.NoConfig, *regexPatternSetSeed,
			](),
		}},
	}
}

func regexPatternSetExecutor(
	t *testing.T, fixture string, libraries map[string]*runtime.Library, endpoint string,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "regex-pattern-set", fixture, "factory.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	body := &parsed.Factory.Body
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libraries), Libraries: libraries, SyntaxSource: body,
		Store: store, Factory: state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"},
		Inputs: map[string]any{
			"aws-config": map[string]any{"endpoint-url": endpoint},
			"scope":      "REGIONAL", "name": "example",
			"name-prefix": nil, "description": nil, "regular-expression-list": nil, "tags": nil,
		},
	}
}

func saveRegexPatternSetPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.SealPlan(plan, encrypters.Noop{})
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	return saved
}

func regexPatternSetStep(
	t *testing.T, plan *runtime.PlanFile, decision runtime.Decision,
) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == regexPatternSetAddress {
			require.Equal(t, decision, step.Decision)
			return step
		}
	}
	t.Fatalf("missing %s", regexPatternSetAddress)
	return runtime.PlanStep{}
}

func applyRegexPatternSet(t *testing.T, executor *runtime.Executor, decision runtime.Decision) {
	t.Helper()
	saved := saveRegexPatternSetPlan(t, executor)
	regexPatternSetStep(t, saved, decision)
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
}

func regexPatternSetSnapshot(t *testing.T, executor *runtime.Executor) *state.Snapshot {
	t.Helper()
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	return snapshot
}

func regexPatternSetPutSnapshot(
	t *testing.T, executor *runtime.Executor, snapshot *state.Snapshot,
) {
	t.Helper()
	rev, err := executor.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(rev))
}

func regexPatternSetGetBody(id, name, scope string) map[string]any {
	return map[string]any{"Id": id, "Scope": scope, "Name": name}
}

func regexPatternSetDeleteBody(id, name, scope string) map[string]any {
	body := regexPatternSetGetBody(id, name, scope)
	body["LockToken"] = "observed-token"
	return body
}

func regexPatternSetCreateBody(name, scope string, patterns []any) map[string]any {
	return map[string]any{
		"Name": name, "Scope": scope, "RegularExpressionList": patterns,
	}
}

func regexPatternSetTagBody(region, scopePart, name, id string) map[string]any {
	arn := fmt.Sprintf(
		"arn:aws:wafv2:%s:123456789012:%s/regexpatternset/%s/%s", region, scopePart, name, id,
	)
	return map[string]any{"ResourceARN": arn}
}

func regexPatternSetReadCalls(endpoint, region, scope, name, id string) []regexPatternSetRequest {
	scopePart := "regional"
	if scope == "CLOUDFRONT" {
		scopePart = "global"
	}
	return []regexPatternSetRequest{
		{"GetRegexPatternSet", endpoint, region, regexPatternSetGetBody(id, name, scope)},
		{"ListTagsForResource", endpoint, region, regexPatternSetTagBody(region, scopePart, name, id)},
	}
}

func TestRegexPatternSetRuntimeEmptySerialization(t *testing.T) {
	for _, patterns := range [][]any{nil, {}} {
		t.Run(fmt.Sprintf("%#v", patterns), func(t *testing.T) {
			cloud := newRegexPatternSetCloud(t)
			executor := regexPatternSetExecutor(t, "evaluated", regexPatternSetRuntimeLibrary(t),
				cloud.endpoint(t, "primary"))
			executor.Inputs["regular-expression-list"] = patterns
			applyRegexPatternSet(t, executor, runtime.DecisionCreate)
			expected := []regexPatternSetRequest{{
				"CreateRegexPatternSet", "primary", "us-west-2",
				regexPatternSetCreateBody("example", "REGIONAL", []any{}),
			}}
			expected = append(expected, regexPatternSetReadCalls(
				"primary", "us-west-2", "REGIONAL", "example", "id-1",
			)...)
			require.Equal(t, expected, cloud.takeCalls())
		})
	}
}

func regexPatterns(values ...string) []any {
	patterns := make([]any, 0, len(values))
	for _, value := range values {
		patterns = append(patterns, map[string]any{"regex-string": value})
	}
	return patterns
}

func regexAWS(values ...string) []any {
	patterns := make([]any, 0, len(values))
	for _, value := range values {
		patterns = append(patterns, map[string]any{"RegexString": value})
	}
	return patterns
}

func TestRegexPatternSetRuntimeSetAndDrift(t *testing.T) {
	cloud := newRegexPatternSetCloud(t)
	executor := regexPatternSetExecutor(t, "evaluated", regexPatternSetRuntimeLibrary(t),
		cloud.endpoint(t, "primary"))
	executor.Inputs["description"] = "desired"
	executor.Inputs["regular-expression-list"] = regexPatterns("b+", "a", "b+")
	applyRegexPatternSet(t, executor, runtime.DecisionCreate)
	wantCreate := regexPatternSetCreateBody("example", "REGIONAL", regexAWS("b+", "a"))
	wantCreate["Description"] = "desired"
	read := regexPatternSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1")
	require.Equal(t, append([]regexPatternSetRequest{{
		"CreateRegexPatternSet", "primary", "us-west-2", wantCreate,
	}}, read...), cloud.takeCalls())
	executor.Inputs["regular-expression-list"] = regexPatterns("a", "b+")
	applyRegexPatternSet(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())

	cloud.sets["us-west-2/REGIONAL/example"]["RegularExpressionList"] = regexAWS("drift")
	cloud.sets["us-west-2/REGIONAL/example"]["Description"] = "drift"
	applyRegexPatternSet(t, executor, runtime.DecisionUpdate)
	update := regexPatternSetDeleteBody("id-1", "example", "REGIONAL")
	update["RegularExpressionList"], update["Description"] = regexAWS("a", "b+"), "desired"
	want := append(slices.Clone(read), regexPatternSetRequest{
		"UpdateRegexPatternSet", "primary", "us-west-2", update,
	})
	require.Equal(t, append(want, read...), cloud.takeCalls())

	executor.Inputs["regular-expression-list"], executor.Inputs["description"] = nil, nil
	applyRegexPatternSet(t, executor, runtime.DecisionUpdate)
	update = regexPatternSetDeleteBody("id-1", "example", "REGIONAL")
	update["RegularExpressionList"] = []any{}
	want = append(slices.Clone(read), regexPatternSetRequest{
		"UpdateRegexPatternSet", "primary", "us-west-2", update,
	})
	require.Equal(t, append(want, read...), cloud.takeCalls())
	applyRegexPatternSet(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())
}

func TestRegexPatternSetRuntimeImmutableFields(t *testing.T) {
	libraries := regexPatternSetRuntimeLibrary(t)
	for _, tc := range []struct {
		field         string
		initial, next any
	}{
		{"name", "example", "other"},
		{"name-prefix", "first-", "other-"},
		{"scope", "REGIONAL", "CLOUDFRONT"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			cloud := newRegexPatternSetCloud(t)
			executor := regexPatternSetExecutor(t, "evaluated", libraries, cloud.endpoint(t, "primary"))
			if tc.field == "name-prefix" {
				executor.Inputs["name"] = nil
			}
			executor.Inputs[tc.field] = tc.initial
			applyRegexPatternSet(t, executor, runtime.DecisionCreate)
			cloud.takeCalls()
			executor.Inputs[tc.field] = tc.next
			step := regexPatternSetStep(t, saveRegexPatternSetPlan(t, executor), runtime.DecisionReplace)
			require.Equal(t, []string{tc.field}, step.ReplacementReasons)
			previous := regexPatternSetSnapshot(t, executor).Find(regexPatternSetAddress)
			read := regexPatternSetReadCalls("primary", "us-west-2", "REGIONAL",
				previous.Outputs["name"].(string), "id-1")
			require.Equal(t, read, cloud.takeCalls())
		})
	}
}

func TestRegexPatternSetRuntimeSavedReplacementRegions(t *testing.T) {
	libraries := regexPatternSetRuntimeLibrary(t)
	for _, scope := range []string{"REGIONAL", "CLOUDFRONT"} {
		t.Run(scope, func(t *testing.T) {
			cloud := newRegexPatternSetCloud(t)
			old := cloud.endpoint(t, "old")
			executor := regexPatternSetExecutor(t, "evaluated", libraries, old)
			executor.Inputs["scope"] = scope
			executor.Inputs["aws-config"] = map[string]any{"endpoint-url": old, "region": "us-west-2"}
			applyRegexPatternSet(t, executor, runtime.DecisionCreate)
			oldRegion, newRegion := "us-west-2", "us-east-2"
			if scope == "CLOUDFRONT" {
				oldRegion, newRegion = "us-east-1", "us-east-1"
			}
			oldReads := regexPatternSetReadCalls("old", oldRegion, scope, "example", "id-1")
			expected := []regexPatternSetRequest{{
				"CreateRegexPatternSet", "old", oldRegion, regexPatternSetCreateBody("example", scope, []any{}),
			}}
			require.Equal(t, append(expected, oldReads...), cloud.takeCalls())

			executor.Inputs["aws-config"] = map[string]any{
				"endpoint-url": cloud.endpoint(t, "new"), "region": "us-east-2",
			}
			saved := saveRegexPatternSetPlan(t, executor)
			step := regexPatternSetStep(t, saved, runtime.DecisionReplace)
			require.Equal(t, []string{"region"}, step.ReplacementReasons)
			require.Equal(t, oldReads, cloud.takeCalls())
			_, err := executor.ApplyPlan(t.Context(), saved)
			require.NoError(t, err)
			expected = append(slices.Clone(oldReads),
				regexPatternSetRequest{
					"DeleteRegexPatternSet", "old", oldRegion,
					regexPatternSetDeleteBody("id-1", "example", scope),
				},
				regexPatternSetRequest{
					"CreateRegexPatternSet", "new", newRegion,
					regexPatternSetCreateBody("example", scope, []any{}),
				},
			)
			newReads := regexPatternSetReadCalls("new", newRegion, scope, "example", "id-2")
			require.Equal(t, append(expected, newReads...), cloud.takeCalls())
			result := regexPatternSetSnapshot(t, executor).Find(regexPatternSetAddress).Outputs
			require.Equal(t, "id-2", result["id"])
			require.Equal(t, newRegion, result["region"])
			require.Equal(t, scope, result["scope"])
		})
	}
}

func TestRegexPatternSetRuntimeConstraintPhases(t *testing.T) {
	libraries := regexPatternSetRuntimeLibrary(t)
	for _, tc := range []struct {
		field      string
		value      any
		diagnostic string
	}{
		{"scope", nil, `field "scope": required but is null`},
		{"scope", "regional", "constraints[2]"},
		{"name-prefix", "prefix-", "constraints[0] (at-most-one-of [name, name-prefix])"},
		{"name", "invalid!", "constraints[3]"},
		{"description", strings.Repeat("é", 128) + "a", "description must be 1 to 256 bytes"},
		{"regular-expression-list", []any{map[string]any{"regex-string": nil}},
			`field "regular-expression-list[0].regex-string": required but is null`},
		{"regular-expression-list", regexPatterns("["), "constraints[8]"},
		{"regular-expression-list", regexPatterns(strings.Repeat("é", 101)),
			"regex-string must be 1 to 200 bytes"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			cloud := newRegexPatternSetCloud(t)
			executor := regexPatternSetExecutor(t, "evaluated", libraries, cloud.endpoint(t, "primary"))
			applyRegexPatternSet(t, executor, runtime.DecisionCreate)
			cloud.takeCalls()
			applyRegexPatternSet(t, executor, runtime.DecisionNoOp)
			require.Equal(t, regexPatternSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1"),
				cloud.takeCalls())
			saved := saveRegexPatternSetPlan(t, executor)
			regexPatternSetStep(t, saved, runtime.DecisionNoOp)
			cloud.takeCalls()

			executor.Inputs[tc.field] = tc.value
			_, err := executor.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, regexPatternSetAddress)
			require.ErrorContains(t, err, tc.diagnostic)
			require.Empty(t, cloud.takeCalls())

			historical := regexPatternSetSnapshot(t, executor)
			historical.Find(regexPatternSetAddress).Inputs[tc.field] = tc.value
			regexPatternSetPutSnapshot(t, executor, historical)
			_, err = executor.Plan(t.Context())
			require.ErrorContains(t, err, regexPatternSetAddress)
			require.ErrorContains(t, err, tc.diagnostic)
			read := regexPatternSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1")
			if strings.HasPrefix(tc.diagnostic, "field ") {
				require.Empty(t, cloud.takeCalls())
			} else {
				require.Equal(t, read, cloud.takeCalls())
			}

			executor.Destroy = true
			applyRegexPatternSet(t, executor, runtime.DecisionDestroy)
			want := append(slices.Clone(read), read...)
			want = append(want, regexPatternSetRequest{
				"DeleteRegexPatternSet", "primary", "us-west-2",
				regexPatternSetDeleteBody("id-1", "example", "REGIONAL"),
			})
			require.Equal(t, want, cloud.takeCalls())
		})
	}
}

func TestRegexPatternSetRuntimeDeferredReplacement(t *testing.T) {
	libraries := regexPatternSetRuntimeLibrary(t)
	for _, next := range []string{"valid-next", "invalid!"} {
		t.Run(next, func(t *testing.T) {
			cloud := newRegexPatternSetCloud(t)
			url := cloud.endpoint(t, "primary")
			initial := regexPatternSetExecutor(t, "evaluated", libraries, url)
			applyRegexPatternSet(t, initial, runtime.DecisionCreate)
			cloud.takeCalls()

			executor := regexPatternSetExecutor(t, "deferred", libraries, url)
			executor.Store = initial.Store
			executor.Inputs = maps.Clone(initial.Inputs)
			executor.Inputs["name"] = next
			saved := saveRegexPatternSetPlan(t, executor)
			step := regexPatternSetStep(t, saved, runtime.DecisionReplace)
			require.Equal(t, map[string][]string{"name": {"action.seed.name"}}, step.UnresolvedInputs)
			require.Equal(t, []string{"name"}, step.PendingReplacementReasons)
			read := regexPatternSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1")
			require.Equal(t, read, cloud.takeCalls())
			_, err := executor.ApplyPlan(t.Context(), saved)
			if next == "invalid!" {
				require.ErrorContains(t, err, regexPatternSetAddress)
				require.ErrorContains(t, err, "constraints[3]")
				require.Empty(t, cloud.takeCalls())
				require.Equal(t, "id-1", regexPatternSetSnapshot(t, executor).
					Find(regexPatternSetAddress).Outputs["id"])
				return
			}
			require.NoError(t, err)
			want := append(slices.Clone(read),
				regexPatternSetRequest{
					"DeleteRegexPatternSet", "primary", "us-west-2",
					regexPatternSetDeleteBody("id-1", "example", "REGIONAL"),
				},
				regexPatternSetRequest{
					"CreateRegexPatternSet", "primary", "us-west-2",
					regexPatternSetCreateBody(next, "REGIONAL", []any{}),
				},
			)
			newReads := regexPatternSetReadCalls("primary", "us-west-2", "REGIONAL", next, "id-2")
			require.Equal(t, append(want, newReads...), cloud.takeCalls())
		})
	}
}

func TestRegexPatternSetRuntimeUpdateKeepsEffectiveRegion(t *testing.T) {
	cloud := newRegexPatternSetCloud(t)
	executor := regexPatternSetExecutor(t, "evaluated", regexPatternSetRuntimeLibrary(t),
		cloud.endpoint(t, "prior"))
	applyRegexPatternSet(t, executor, runtime.DecisionCreate)
	cloud.takeCalls()

	t.Setenv("AWS_REGION", "us-east-2")
	executor.Inputs["aws-config"] = map[string]any{"endpoint-url": cloud.endpoint(t, "current")}
	executor.Inputs["description"] = "updated"
	applyRegexPatternSet(t, executor, runtime.DecisionUpdate)
	update := regexPatternSetDeleteBody("id-1", "example", "REGIONAL")
	update["Description"] = "updated"
	update["RegularExpressionList"] = []any{}
	want := regexPatternSetReadCalls("prior", "us-west-2", "REGIONAL", "example", "id-1")
	want = append(want,
		regexPatternSetRequest{"UpdateRegexPatternSet", "current", "us-west-2", update})
	want = append(want,
		regexPatternSetReadCalls("current", "us-west-2", "REGIONAL", "example", "id-1")...)
	require.Equal(t, want, cloud.takeCalls())
}

func TestRegexPatternSetRuntimeRefreshOnlyRecreatesMissingSet(t *testing.T) {
	libraries := regexPatternSetRuntimeLibrary(t)
	for _, tc := range []struct {
		name        string
		serviceCode string
		wrongName   bool
		diagnostic  string
	}{
		{name: "missing", serviceCode: "WAFNonexistentItemException"},
		{name: "permission", serviceCode: "AccessDeniedException", diagnostic: "AccessDeniedException"},
		{name: "conflicting identity", wrongName: true, diagnostic: "identity does not match"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := newRegexPatternSetCloud(t)
			executor := regexPatternSetExecutor(t, "evaluated", libraries, cloud.endpoint(t, "primary"))
			applyRegexPatternSet(t, executor, runtime.DecisionCreate)
			cloud.takeCalls()
			cloud.mu.Lock()
			cloud.getError = tc.serviceCode
			if tc.wrongName {
				cloud.sets["us-west-2/REGIONAL/example"]["Name"] = "different"
			}
			cloud.mu.Unlock()
			if tc.diagnostic == "" {
				step := regexPatternSetStep(t, saveRegexPatternSetPlan(t, executor), runtime.DecisionCreate)
				require.True(t, step.Gone())
			} else {
				_, err := executor.Plan(t.Context())
				require.ErrorContains(t, err, regexPatternSetAddress)
				require.ErrorContains(t, err, tc.diagnostic)
			}
			require.Equal(t, []regexPatternSetRequest{{
				"GetRegexPatternSet", "primary", "us-west-2",
				regexPatternSetGetBody("id-1", "example", "REGIONAL"),
			}}, cloud.takeCalls())
		})
	}
}

func TestRegexPatternSetRuntimeFailedInitialization(t *testing.T) {
	libraries := regexPatternSetRuntimeLibrary(t)
	for _, phase := range []string{
		"get", "tags", "cancel", "cleanup-error", "missing-token", "missing-token-cleanup-denied",
	} {
		t.Run(phase, func(t *testing.T) {
			cloud := newRegexPatternSetCloud(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch phase {
			case "get":
				cloud.getError = "AccessDeniedException"
			case "cancel":
				cloud.onGet = cancel
			case "missing-token", "missing-token-cleanup-denied":
				cloud.omitGetToken = true
				if phase == "missing-token-cleanup-denied" {
					gets := 0
					cloud.onGet = func() {
						gets++
						if gets > 1 {
							cloud.getError = "AccessDeniedException"
						}
					}
				}
			default:
				cloud.tagError = "AccessDeniedException"
			}
			if phase == "cleanup-error" {
				cloud.deleteError = "WAFOptimisticLockException"
			}
			executor := regexPatternSetExecutor(t, "evaluated", libraries, cloud.endpoint(t, "primary"))
			saved := saveRegexPatternSetPlan(t, executor)
			result, err := executor.ApplyPlan(ctx, saved)
			require.Nil(t, result)
			if phase == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			} else if cloud.omitGetToken {
				require.ErrorContains(t, err, "response has no lock token")
			} else {
				require.ErrorContains(t, err, "AccessDeniedException")
			}
			snapshot, snapshotErr := executor.Store.Current()
			if snapshotErr != nil {
				require.ErrorIs(t, snapshotErr, state.ErrNoCurrent)
			} else {
				require.Nil(t, snapshot.Find(regexPatternSetAddress))
			}
			want := []regexPatternSetRequest{{
				"CreateRegexPatternSet", "primary", "us-west-2",
				regexPatternSetCreateBody("example", "REGIONAL", []any{}),
			}}
			read := regexPatternSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1")
			if phase == "get" || phase == "cancel" || cloud.omitGetToken {
				read = read[:1]
			}
			want = append(want, read...)
			want = append(want, regexPatternSetRequest{
				"DeleteRegexPatternSet", "primary", "us-west-2",
				regexPatternSetDeleteBody("id-1", "example", "REGIONAL"),
			})
			require.Equal(t, want, cloud.takeCalls())
			if phase == "cleanup-error" {
				require.ErrorContains(t, err, "WAFOptimisticLockException")
			} else {
				require.Empty(t, cloud.sets)
			}
		})
	}
}

func TestRegexPatternSetRuntimeDeferredRequiredInputs(t *testing.T) {
	libraries := regexPatternSetRuntimeLibrary(t)
	for _, field := range []string{"valid", "scope", "regex-string"} {
		t.Run(field, func(t *testing.T) {
			cloud := newRegexPatternSetCloud(t)
			url := cloud.endpoint(t, "primary")
			initial := regexPatternSetExecutor(t, "evaluated", libraries, url)
			applyRegexPatternSet(t, initial, runtime.DecisionCreate)
			cloud.takeCalls()
			executor := regexPatternSetExecutor(t, "deferred-inputs", libraries, url)
			executor.Store = initial.Store
			executor.Inputs["name"] = "next"
			executor.Inputs["regex-string"] = "a+"
			if field != "valid" {
				executor.Inputs[field] = nil
			}
			saved := saveRegexPatternSetPlan(t, executor)
			step := regexPatternSetStep(t, saved, runtime.DecisionReplace)
			require.Equal(t, map[string][]string{
				"scope":                   {"action.seed.scope"},
				"regular-expression-list": {"action.seed.regex"},
			}, step.UnresolvedInputs)
			cloud.takeCalls()
			_, err := executor.ApplyPlan(t.Context(), saved)
			if field != "valid" {
				require.ErrorContains(t, err, regexPatternSetAddress)
				path := field
				if field == "regex-string" {
					path = "regular-expression-list[0].regex-string"
				}
				require.ErrorContains(t, err, fmt.Sprintf("field %q: required but is null", path))
				require.Empty(t, cloud.takeCalls())
				require.Equal(t, "id-1", regexPatternSetSnapshot(t, executor).
					Find(regexPatternSetAddress).Outputs["id"])
				return
			}
			require.NoError(t, err)
			want := regexPatternSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1")
			want = append(want, regexPatternSetRequest{
				"DeleteRegexPatternSet", "primary", "us-west-2",
				regexPatternSetDeleteBody("id-1", "example", "REGIONAL"),
			}, regexPatternSetRequest{
				"CreateRegexPatternSet", "primary", "us-west-2",
				regexPatternSetCreateBody("next", "REGIONAL", regexAWS("a+")),
			})
			want = append(want,
				regexPatternSetReadCalls("primary", "us-west-2", "REGIONAL", "next", "id-2")...)
			require.Equal(t, want, cloud.takeCalls())
		})
	}
}
