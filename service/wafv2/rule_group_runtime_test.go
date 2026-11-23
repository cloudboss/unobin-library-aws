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
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	awswafv2 "github.com/cloudboss/unobin-library-aws/service/wafv2"
)

const ruleGroupAddress = "resource.group"

type ruleGroupRuntimeCloud struct {
	mu     sync.Mutex
	calls  []ipSetRequest
	groups map[string]map[string]any
	tags   map[string]map[string]string
	nextID int
	token  string
}

func newRuleGroupRuntimeCloud(t *testing.T) *ruleGroupRuntimeCloud {
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
	return &ruleGroupRuntimeCloud{
		groups: map[string]map[string]any{}, tags: map[string]map[string]string{},
		token: "observed-token",
	}
}

func (c *ruleGroupRuntimeCloud) endpoint(t *testing.T, label string) string {
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
		call := ipSetRequest{operation: operation, endpoint: label, region: signing[2], body: body}
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

func (c *ruleGroupRuntimeCloud) reply(call ipSetRequest) (map[string]any, string) {
	body := call.body
	key := fmt.Sprint(call.region, "/", body["Scope"], "/", body["Name"])
	group := c.groups[key]
	arn, _ := body["ResourceARN"].(string)
	switch call.operation {
	case "CreateRuleGroup":
		c.nextID++
		id := fmt.Sprintf("id-%d", c.nextID)
		arn = ruleGroupARN(call.region, body["Scope"].(string), body["Name"].(string), id)
		group = map[string]any{"Id": id, "ARN": arn, "Name": body["Name"], "Capacity": body["Capacity"]}
		setRuleGroupCloudCore(group, body)
		c.groups[key] = group
		c.tags[arn] = map[string]string{"aws:system": "true"}
		c.writeTags(arn, body["Tags"])
		return map[string]any{"Summary": map[string]any{"Id": id}, "LockToken": "create-token"}, ""
	case "GetRuleGroup":
		if group == nil || group["Id"] != body["Id"] {
			return nil, "WAFNonexistentItemException"
		}
		return map[string]any{"RuleGroup": group, "LockToken": c.token}, ""
	case "UpdateRuleGroup":
		if group == nil || group["Id"] != body["Id"] || body["LockToken"] != c.token {
			return nil, "WAFOptimisticLockException"
		}
		setRuleGroupCloudCore(group, body)
		return map[string]any{"NextLockToken": "unused-token"}, ""
	case "DeleteRuleGroup":
		if group == nil || group["Id"] != body["Id"] {
			return nil, "WAFNonexistentItemException"
		}
		if body["LockToken"] != c.token {
			return nil, "WAFOptimisticLockException"
		}
		delete(c.groups, key)
		delete(c.tags, group["ARN"].(string))
		return map[string]any{}, ""
	case "ListTagsForResource":
		var tags []any
		for _, key := range slices.Sorted(maps.Keys(c.tags[arn])) {
			tags = append(tags, map[string]string{"Key": key, "Value": c.tags[arn][key]})
		}
		return map[string]any{"TagInfoForResource": map[string]any{"TagList": tags},
			"NextMarker": "ignored"}, ""
	case "UntagResource":
		for _, key := range body["TagKeys"].([]any) {
			delete(c.tags[arn], key.(string))
		}
		return map[string]any{}, ""
	case "TagResource":
		c.writeTags(arn, body["Tags"])
		return map[string]any{}, ""
	default:
		return nil, "UnexpectedOperation"
	}
}

func (c *ruleGroupRuntimeCloud) writeTags(arn string, raw any) {
	tags, _ := raw.([]any)
	for _, rawTag := range tags {
		tag := rawTag.(map[string]any)
		c.tags[arn][tag["Key"].(string)] = tag["Value"].(string)
	}
}

func setRuleGroupCloudCore(group, request map[string]any) {
	for _, key := range []string{"VisibilityConfig", "Description", "Rules", "CustomResponseBodies"} {
		value, present := request[key]
		if present {
			group[key] = value
		} else {
			delete(group, key)
		}
	}
}

func (c *ruleGroupRuntimeCloud) takeCalls() []ipSetRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls = nil
	return calls
}

func ruleGroupARN(region, scope, name, id string) string {
	prefix := "regional"
	if scope == "CLOUDFRONT" {
		prefix = "global"
	}
	return fmt.Sprintf("arn:aws:wafv2:%s:123456789012:%s/rulegroup/%s/%s", region, prefix, name, id)
}

func ruleGroupReadCalls(endpoint, region, scope, name, id string) []ipSetRequest {
	return []ipSetRequest{
		{"GetRuleGroup", endpoint, region, ipSetGetBody(id, name, scope)},
		{"ListTagsForResource", endpoint, region, map[string]any{
			"ResourceARN": ruleGroupARN(region, scope, name, id),
		}},
	}
}

type ruleGroupSeed struct {
	Echo any
}

type ruleGroupSeedOutput struct {
	Value any
}

func (s *ruleGroupSeed) Run(context.Context, runtime.NoConfig) (*ruleGroupSeedOutput, error) {
	return &ruleGroupSeedOutput{Value: s.Echo}, nil
}

func ruleGroupRuntimeLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	library := runtime.LibraryWithPath(awswafv2.Library(), ipSetLibraryPath)
	library.Schema = readLibrarySchema(t)
	library.Constraints = map[string][]lang.ConstraintSpec{
		"resource.rule-group": library.Schema.Resources["rule-group"].Constraints,
	}
	return map[string]*runtime.Library{
		"aws-wafv2": library,
		"fixture": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[
				ruleGroupSeed, *ruleGroupSeedOutput, runtime.NoConfig, *ruleGroupSeed,
			](),
		}},
	}
}

func newRuleGroupExecutor(
	t *testing.T, fixture string, libraries map[string]*runtime.Library, endpoint string,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "rule-group", fixture, "factory.ub")
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
			"scope":      "REGIONAL", "capacity": int64(20), "name": "example",
			"name-prefix": nil, "description": nil,
			"visibility": ruleGroupVisibility("group-metric"),
			"rules":      nil, "rules-json": nil, "custom-response-bodies": nil, "tags": nil,
		},
	}
}

func ruleGroupVisibility(metric string) map[string]any {
	return map[string]any{
		"cloudwatch-metrics-enabled": false, "metric-name": metric, "sampled-requests-enabled": false,
	}
}

func ruleGroupVisibilitySDK(metric string) map[string]any {
	return map[string]any{
		"CloudWatchMetricsEnabled": false, "MetricName": metric, "SampledRequestsEnabled": false,
	}
}

func ruleGroupPlanStep(
	t *testing.T, plan *runtime.PlanFile, decision runtime.Decision,
) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == ruleGroupAddress {
			require.Equal(t, decision, step.Decision)
			return step
		}
	}
	t.Fatalf("missing %s", ruleGroupAddress)
	return runtime.PlanStep{}
}

func applyRuleGroup(t *testing.T, executor *runtime.Executor, decision runtime.Decision) {
	t.Helper()
	saved := saveIPSetPlan(t, executor)
	ruleGroupPlanStep(t, saved, decision)
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
}

func labelRule(name string) map[string]any {
	return map[string]any{
		"name":     name,
		"priority": int64(0),
		"action":   map[string]any{"count": map[string]any{}},
		"statement": map[string]any{"label-match-statement": map[string]any{
			"scope": "LABEL", "key": "test:label",
		}},
		"visibility-config": ruleGroupVisibility(name),
	}
}

func labelRuleSDK(name string, priority float64) map[string]any {
	return map[string]any{
		"Name": name, "Priority": priority,
		"Action":        map[string]any{"Count": map[string]any{}},
		"CaptchaConfig": map[string]any{},
		"Statement": map[string]any{"LabelMatchStatement": map[string]any{
			"Scope": "LABEL", "Key": "test:label",
		}},
		"VisibilityConfig": ruleGroupVisibilitySDK(name),
	}
}

func TestRuleGroupRuntimeReplacementAndRegion(t *testing.T) {
	libraries := ruleGroupRuntimeLibraries(t)
	cloud := newRuleGroupRuntimeCloud(t)
	oldURL := cloud.endpoint(t, "old")
	newURL := cloud.endpoint(t, "new")
	executor := newRuleGroupExecutor(t, "evaluated", libraries, oldURL)
	executor.Inputs["aws-config"] = map[string]any{"endpoint-url": oldURL, "region": "us-west-2"}
	applyRuleGroup(t, executor, runtime.DecisionCreate)
	read := ruleGroupReadCalls("old", "us-west-2", "REGIONAL", "example", "id-1")
	create := ipSetRequest{"CreateRuleGroup", "old", "us-west-2", map[string]any{
		"Capacity": float64(20), "Name": "example", "Scope": "REGIONAL",
		"VisibilityConfig": ruleGroupVisibilitySDK("group-metric"),
	}}
	require.Equal(t, append([]ipSetRequest{create}, read...), cloud.takeCalls())

	initial := maps.Clone(executor.Inputs)
	for _, tc := range []struct {
		field  string
		value  any
		reason string
	}{
		{"capacity", int64(50), "capacity"},
		{"name", "renamed", "name"},
		{"name-prefix", "examples-", "name-prefix"},
		{"scope", "CLOUDFRONT", "scope"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			executor.Inputs = maps.Clone(initial)
			executor.Inputs[tc.field] = tc.value
			if tc.field == "name-prefix" {
				executor.Inputs["name"] = nil
			}
			step := ruleGroupPlanStep(t, saveIPSetPlan(t, executor), runtime.DecisionReplace)
			require.Contains(t, step.ReplacementReasons, tc.reason)
			require.Equal(t, read, cloud.takeCalls())
		})
	}
	executor.Inputs = maps.Clone(initial)
	executor.Inputs["description"] = "updated in place"
	ruleGroupPlanStep(t, saveIPSetPlan(t, executor), runtime.DecisionUpdate)
	require.Equal(t, read, cloud.takeCalls())

	executor.Inputs = maps.Clone(initial)
	executor.Inputs["aws-config"] = map[string]any{"endpoint-url": newURL, "region": "us-east-2"}
	applyRuleGroup(t, executor, runtime.DecisionReplace)
	next := create
	next.endpoint, next.region = "new", "us-east-2"
	want := append(slices.Clone(read), read...)
	want = append(want,
		ipSetRequest{"DeleteRuleGroup", "old", "us-west-2",
			ipSetDeleteBody("id-1", "example", "REGIONAL")},
		next,
	)
	want = append(want, ruleGroupReadCalls("new", "us-east-2", "REGIONAL", "example", "id-2")...)
	require.Equal(t, want, cloud.takeCalls())

	executor.Inputs["scope"] = "CLOUDFRONT"
	applyRuleGroup(t, executor, runtime.DecisionReplace)
	regional := ruleGroupReadCalls("new", "us-east-2", "REGIONAL", "example", "id-2")
	want = append(slices.Clone(regional), regional...)
	next.body = maps.Clone(next.body)
	next.body["Scope"] = "CLOUDFRONT"
	next.region = "us-east-1"
	want = append(want,
		ipSetRequest{"DeleteRuleGroup", "new", "us-east-2",
			ipSetDeleteBody("id-2", "example", "REGIONAL")},
		next,
	)
	want = append(want, ruleGroupReadCalls("new", "us-east-1", "CLOUDFRONT", "example", "id-3")...)
	require.Equal(t, want, cloud.takeCalls())
}

func TestRuleGroupNativePlanAndSavedNoOp(t *testing.T) {
	cloud := newRuleGroupRuntimeCloud(t)
	executor := newRuleGroupExecutor(
		t, "evaluated", ruleGroupRuntimeLibraries(t), cloud.endpoint(t, "aws"),
	)
	rule := labelRule("alpha")
	executor.Inputs["rules"] = []any{rule}
	applyRuleGroup(t, executor, runtime.DecisionCreate)
	cloud.takeCalls()
	read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")

	applyRuleGroup(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())
	saved := saveIPSetPlan(t, executor)
	ruleGroupPlanStep(t, saved, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())

	invalid := labelRule("alpha")
	invalid["visibility-config"] = ruleGroupVisibility("invalid!")
	executor.Inputs["rules"] = []any{invalid}
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, ruleGroupAddress)
	require.ErrorContains(t, err, "rule visibility metric-name")
	require.Empty(t, cloud.takeCalls())

	historical := ipSetSnapshot(t, executor)
	historical.Find(ruleGroupAddress).Inputs["rules"] = []any{invalid}
	ipSetPutSnapshot(t, executor, historical)
	_, err = executor.Plan(t.Context())
	require.ErrorContains(t, err, ruleGroupAddress)
	require.ErrorContains(t, err, "rule visibility metric-name")
	require.Equal(t, read, cloud.takeCalls())

	executor.Destroy = true
	applyRuleGroup(t, executor, runtime.DecisionDestroy)
	want := append(slices.Clone(read), read...)
	want = append(want, ipSetRequest{"DeleteRuleGroup", "aws", "us-west-2",
		ipSetDeleteBody("id-1", "example", "REGIONAL")})
	require.Equal(t, want, cloud.takeCalls())
}

func TestRuleGroupPendingKnownSiblingAndReplacement(t *testing.T) {
	libraries := ruleGroupRuntimeLibraries(t)
	for _, metric := range []string{"invalid!", "known-metric"} {
		t.Run(metric, func(t *testing.T) {
			cloud := newRuleGroupRuntimeCloud(t)
			url := cloud.endpoint(t, "aws")
			initial := newRuleGroupExecutor(t, "evaluated", libraries, url)
			applyRuleGroup(t, initial, runtime.DecisionCreate)
			cloud.takeCalls()

			executor := newRuleGroupExecutor(t, "deferred", libraries, url)
			executor.Store = initial.Store
			executor.Inputs["capacity"] = int64(21)
			executor.Inputs["metric"] = metric
			executor.Inputs["pending-rule"] = labelRule("resolved")
			plan := saveIPSetPlan(t, executor)
			step := ruleGroupPlanStep(t, plan, runtime.DecisionReplace)
			require.Equal(t, map[string][]string{"rules": {"action.seed.value"}}, step.UnresolvedInputs)
			read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
			require.Equal(t, read, cloud.takeCalls())

			_, err := executor.ApplyPlan(t.Context(), plan)
			if metric == "invalid!" {
				require.ErrorContains(t, err, ruleGroupAddress)
				require.ErrorContains(t, err, "rule visibility metric-name")
				require.Empty(t, cloud.takeCalls())
				require.Equal(t, "id-1", ipSetSnapshot(t, executor).Find(ruleGroupAddress).Outputs["id"])
				return
			}
			require.NoError(t, err)
			known := labelRuleSDK("known", 3)
			known["VisibilityConfig"] = ruleGroupVisibilitySDK(metric)
			want := append(slices.Clone(read),
				ipSetRequest{"DeleteRuleGroup", "aws", "us-west-2",
					ipSetDeleteBody("id-1", "example", "REGIONAL")},
				ipSetRequest{"CreateRuleGroup", "aws", "us-west-2", map[string]any{
					"Capacity": float64(21), "Name": "example", "Scope": "REGIONAL",
					"VisibilityConfig": ruleGroupVisibilitySDK("group-metric"),
					"Rules":            []any{known, labelRuleSDK("resolved", 0)},
				}},
			)
			want = append(want, ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-2")...)
			require.Equal(t, want, cloud.takeCalls())
			entry := ipSetSnapshot(t, executor).Find(ruleGroupAddress)
			require.Equal(t, "id-2", entry.Outputs["id"])
			require.Equal(t, "observed-token", entry.Outputs["lock-token"])
		})
	}
}

func TestRuleGroupNativeRequiredValues(t *testing.T) {
	libraries := ruleGroupRuntimeLibraries(t)
	for _, tc := range []struct {
		object, field, diagnostic string
	}{
		{"rule", "priority", "rule priority must be set"},
		{"rule visibility", "cloudwatch-metrics-enabled",
			"rule visibility cloudwatch-metrics-enabled must be set"},
		{"rule visibility", "sampled-requests-enabled",
			"rule visibility sampled-requests-enabled must be set"},
		{"group visibility", "cloudwatch-metrics-enabled",
			"visibility-config cloudwatch-metrics-enabled must be set"},
		{"group visibility", "sampled-requests-enabled",
			"visibility-config sampled-requests-enabled must be set"},
	} {
		for _, mode := range []string{"missing", "null"} {
			t.Run(tc.object+"/"+tc.field+"/"+mode, func(t *testing.T) {
				cloud := newRuleGroupRuntimeCloud(t)
				endpoint := cloud.endpoint(t, "aws")
				executor := newRuleGroupExecutor(t, "evaluated", libraries, endpoint)
				executor.Inputs["rules"] = []any{labelRule("resolved")}
				applyRuleGroup(t, executor, runtime.DecisionCreate)
				read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
				create := ipSetRequest{"CreateRuleGroup", "aws", "us-west-2", map[string]any{
					"Capacity": float64(20), "Name": "example", "Scope": "REGIONAL",
					"VisibilityConfig": ruleGroupVisibilitySDK("group-metric"),
					"Rules":            []any{labelRuleSDK("resolved", 0)},
				}}
				require.Equal(t, append([]ipSetRequest{create}, read...), cloud.takeCalls())

				unchanged := saveIPSetPlan(t, executor)
				ruleGroupPlanStep(t, unchanged, runtime.DecisionNoOp)
				require.Equal(t, read, cloud.takeCalls())

				rule := labelRule("resolved")
				visibility := ruleGroupVisibility("group-metric")
				object := rule
				switch tc.object {
				case "rule visibility":
					object = rule["visibility-config"].(map[string]any)
				case "group visibility":
					object = visibility
				}
				if mode == "null" {
					object[tc.field] = nil
				} else {
					delete(object, tc.field)
				}
				inputs := []any{rule}
				executor.Inputs["rules"] = inputs
				executor.Inputs["visibility"] = visibility
				_, err := executor.Plan(t.Context())
				require.ErrorContains(t, err, ruleGroupAddress)
				require.ErrorContains(t, err, tc.diagnostic)
				require.Equal(t, read, cloud.takeCalls())
				_, err = executor.ApplyPlan(t.Context(), unchanged)
				require.ErrorContains(t, err, ruleGroupAddress)
				require.ErrorContains(t, err, tc.diagnostic)
				require.Empty(t, cloud.takeCalls())

				deferred := newRuleGroupExecutor(t, "deferred-required", libraries, endpoint)
				deferred.Store = executor.Store
				deferred.Inputs["capacity"] = int64(21)
				deferred.Inputs["rules"] = inputs
				deferred.Inputs["visibility"] = visibility
				replacement := saveIPSetPlan(t, deferred)
				step := ruleGroupPlanStep(t, replacement, runtime.DecisionReplace)
				require.Equal(t, map[string][]string{
					"rules": {"action.rules.value"}, "visibility-config": {"action.visibility.value"},
				}, step.UnresolvedInputs)
				require.Equal(t, read, cloud.takeCalls())
				_, err = deferred.ApplyPlan(t.Context(), replacement)
				require.ErrorContains(t, err, ruleGroupAddress)
				require.ErrorContains(t, err, tc.diagnostic)
				require.Empty(t, cloud.takeCalls())

				historical := ipSetSnapshot(t, executor)
				entry := historical.Find(ruleGroupAddress)
				require.Equal(t, "id-1", entry.Outputs["id"])
				entry.Inputs["rules"] = inputs
				entry.Inputs["visibility-config"] = visibility
				ipSetPutSnapshot(t, executor, historical)
				executor.Destroy = true
				applyRuleGroup(t, executor, runtime.DecisionDestroy)
				want := append(slices.Clone(read), read...)
				want = append(want, ipSetRequest{"DeleteRuleGroup", "aws", "us-west-2",
					ipSetDeleteBody("id-1", "example", "REGIONAL")})
				require.Equal(t, want, cloud.takeCalls())
			})
		}
	}
}

func TestRuleGroupRuntimeRepairsCoreDrift(t *testing.T) {
	cloud := newRuleGroupRuntimeCloud(t)
	executor := newRuleGroupExecutor(
		t, "evaluated", ruleGroupRuntimeLibraries(t), cloud.endpoint(t, "aws"),
	)
	executor.Inputs["description"] = "desired"
	executor.Inputs["rules"] = []any{labelRule("expected")}
	applyRuleGroup(t, executor, runtime.DecisionCreate)
	cloud.takeCalls()

	cloud.mu.Lock()
	cloud.groups["us-west-2/REGIONAL/example"]["Description"] = "out-of-band"
	cloud.token = "drift-token"
	cloud.mu.Unlock()
	plan := saveIPSetPlan(t, executor)
	step := ruleGroupPlanStep(t, plan, runtime.DecisionUpdate)
	require.Equal(t, step.PriorInputs, step.Inputs)
	require.Equal(t, "observed-token", step.PriorOutputs["lock-token"])
	require.Equal(t, "drift-token", step.ObservedOutputs["lock-token"])
	require.Equal(t, "out-of-band", step.ObservedOutputs["description"])
	read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
	require.Equal(t, read, cloud.takeCalls())
	_, err := executor.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	update := ipSetDeleteBody("id-1", "example", "REGIONAL")
	update["LockToken"] = "drift-token"
	update["Description"] = "desired"
	update["VisibilityConfig"] = ruleGroupVisibilitySDK("group-metric")
	update["Rules"] = []any{labelRuleSDK("expected", 0)}
	want := append([]ipSetRequest{{"UpdateRuleGroup", "aws", "us-west-2", update}}, read...)
	require.Equal(t, want, cloud.takeCalls())
	entry := ipSetSnapshot(t, executor).Find(ruleGroupAddress)
	require.Equal(t, "desired", entry.Outputs["description"])
	applyRuleGroup(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())
}

func TestRuleGroupRuntimeDeduplicatesTypedRulesBeforeConversion(t *testing.T) {
	cloud := newRuleGroupRuntimeCloud(t)
	executor := newRuleGroupExecutor(
		t, "evaluated", ruleGroupRuntimeLibraries(t), cloud.endpoint(t, "aws"),
	)
	rule := labelRule("collision")
	large := labelRule("collision")
	large["priority"] = int64(1) << 32
	executor.Inputs["rules"] = []any{large, rule, large, rule}
	applyRuleGroup(t, executor, runtime.DecisionCreate)

	read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
	converted := labelRuleSDK("collision", 0)
	create := ipSetRequest{"CreateRuleGroup", "aws", "us-west-2", map[string]any{
		"Capacity": float64(20), "Name": "example", "Scope": "REGIONAL",
		"VisibilityConfig": ruleGroupVisibilitySDK("group-metric"),
		"Rules":            []any{converted, converted},
	}}
	require.Equal(t, append([]ipSetRequest{create}, read...), cloud.takeCalls())

	executor.Inputs["rules"] = []any{rule, large}
	applyRuleGroup(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())

	executor.Inputs["rules"] = []any{rule}
	applyRuleGroup(t, executor, runtime.DecisionUpdate)
	update := ipSetDeleteBody("id-1", "example", "REGIONAL")
	update["VisibilityConfig"] = ruleGroupVisibilitySDK("group-metric")
	update["Rules"] = []any{converted}
	want := append(slices.Clone(read), ipSetRequest{"UpdateRuleGroup", "aws", "us-west-2", update})
	require.Equal(t, append(want, read...), cloud.takeCalls())
}

func TestRuleGroupRuntimeStructuredDefaultsEqual(t *testing.T) {
	cloud := newRuleGroupRuntimeCloud(t)
	executor := newRuleGroupExecutor(
		t, "evaluated", ruleGroupRuntimeLibraries(t), cloud.endpoint(t, "aws"),
	)
	rule := func(explicit bool) map[string]any {
		body := map[string]any{}
		jsonBody := map[string]any{
			"match-scope": "ALL", "match-pattern": map[string]any{"all": map[string]any{}},
		}
		xss := func(field string, value map[string]any) map[string]any {
			return map[string]any{"xss-match-statement": map[string]any{
				"field-to-match": map[string]any{field: value},
				"text-transformations": []any{
					map[string]any{"priority": int64(0), "type": "NONE"},
				},
			}}
		}
		rate := map[string]any{
			"limit": int64(100),
			"scope-down-statement": map[string]any{"and-statement": map[string]any{
				"statements": []any{xss("body", body), xss("json-body", jsonBody)},
			}},
		}
		if explicit {
			rate["aggregate-key-type"] = "IP"
			rate["evaluation-window-sec"] = int64(300)
			body["oversize-handling"] = "CONTINUE"
			jsonBody["oversize-handling"] = "CONTINUE"
		}
		value := labelRule("defaults")
		value["statement"] = map[string]any{"rate-based-statement": rate}
		return value
	}
	xssSDK := func(field string, value map[string]any) map[string]any {
		return map[string]any{"XssMatchStatement": map[string]any{
			"FieldToMatch":        map[string]any{field: value},
			"TextTransformations": []any{map[string]any{"Priority": float64(0), "Type": "NONE"}},
		}}
	}
	converted := labelRuleSDK("defaults", 0)
	converted["Statement"] = map[string]any{"RateBasedStatement": map[string]any{
		"AggregateKeyType": "IP", "EvaluationWindowSec": float64(300), "Limit": float64(100),
		"ScopeDownStatement": map[string]any{"AndStatement": map[string]any{"Statements": []any{
			xssSDK("Body", map[string]any{"OversizeHandling": "CONTINUE"}),
			xssSDK("JsonBody", map[string]any{
				"MatchScope": "ALL", "MatchPattern": map[string]any{"All": map[string]any{}},
				"OversizeHandling": "CONTINUE",
			}),
		}}},
	}}
	read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
	create := ipSetRequest{"CreateRuleGroup", "aws", "us-west-2", map[string]any{
		"Capacity": float64(20), "Name": "example", "Scope": "REGIONAL",
		"VisibilityConfig": ruleGroupVisibilitySDK("group-metric"),
		"Rules":            []any{converted},
	}}

	executor.Inputs["rules"] = []any{rule(false)}
	applyRuleGroup(t, executor, runtime.DecisionCreate)
	require.Equal(t, append([]ipSetRequest{create}, read...), cloud.takeCalls())

	executor.Inputs["rules"] = []any{rule(true)}
	applyRuleGroup(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())

	executor.Inputs["rules"] = []any{rule(true), rule(false)}
	executor.Inputs["description"] = "updated"
	applyRuleGroup(t, executor, runtime.DecisionUpdate)
	update := ipSetDeleteBody("id-1", "example", "REGIONAL")
	update["VisibilityConfig"] = ruleGroupVisibilitySDK("group-metric")
	update["Description"] = "updated"
	update["Rules"] = []any{converted}
	want := append(slices.Clone(read), ipSetRequest{"UpdateRuleGroup", "aws", "us-west-2", update})
	require.Equal(t, append(want, read...), cloud.takeCalls())
}

func TestRuleGroupRuntimeCaptchaImmunityTimeEquality(t *testing.T) {
	cloud := newRuleGroupRuntimeCloud(t)
	executor := newRuleGroupExecutor(
		t, "evaluated", ruleGroupRuntimeLibraries(t), cloud.endpoint(t, "aws"),
	)
	rule := func(property map[string]any) map[string]any {
		value := labelRule("captcha")
		value["action"] = map[string]any{"captcha": map[string]any{}}
		value["captcha-config"] = map[string]any{"immunity-time-property": property}
		return value
	}
	omitted := rule(map[string]any{})
	zero := rule(map[string]any{"immunity-time": int64(0)})
	executor.Inputs["rules"] = []any{omitted, zero}
	applyRuleGroup(t, executor, runtime.DecisionCreate)

	converted := labelRuleSDK("captcha", 0)
	converted["Action"] = map[string]any{"Captcha": map[string]any{}}
	converted["CaptchaConfig"] = map[string]any{
		"ImmunityTimeProperty": map[string]any{"ImmunityTime": float64(0)},
	}
	read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
	create := ipSetRequest{"CreateRuleGroup", "aws", "us-west-2", map[string]any{
		"Capacity": float64(20), "Name": "example", "Scope": "REGIONAL",
		"VisibilityConfig": ruleGroupVisibilitySDK("group-metric"),
		"Rules":            []any{converted},
	}}
	assert.Equal(t, append([]ipSetRequest{create}, read...), cloud.takeCalls())

	executor.Inputs["rules"] = []any{omitted}
	applyRuleGroup(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())

	executor.Inputs["rules"] = []any{rule(map[string]any{"immunity-time": int64(60)})}
	applyRuleGroup(t, executor, runtime.DecisionUpdate)
	converted["CaptchaConfig"] = map[string]any{
		"ImmunityTimeProperty": map[string]any{"ImmunityTime": float64(60)},
	}
	update := ipSetDeleteBody("id-1", "example", "REGIONAL")
	update["VisibilityConfig"] = ruleGroupVisibilitySDK("group-metric")
	update["Rules"] = []any{converted}
	want := append(slices.Clone(read), ipSetRequest{"UpdateRuleGroup", "aws", "us-west-2", update})
	require.Equal(t, append(want, read...), cloud.takeCalls())
}

func ruleGroupMatchPatternRule(field string, pattern map[string]any) map[string]any {
	rule := labelRule("pattern")
	rule["statement"] = map[string]any{"xss-match-statement": map[string]any{
		"field-to-match": map[string]any{field: map[string]any{
			"match-scope": "KEY", "oversize-handling": "NO_MATCH", "match-pattern": pattern,
		}},
		"text-transformations": []any{map[string]any{"priority": int64(0), "type": "NONE"}},
	}}
	return rule
}

func ruleGroupMatchPatternRuleSDK(field string, pattern map[string]any) map[string]any {
	rule := labelRuleSDK("pattern", 0)
	rule["Statement"] = map[string]any{"XssMatchStatement": map[string]any{
		"FieldToMatch": map[string]any{field: map[string]any{
			"MatchScope": "KEY", "OversizeHandling": "NO_MATCH", "MatchPattern": pattern,
		}},
		"TextTransformations": []any{map[string]any{"Priority": float64(0), "Type": "NONE"}},
	}}
	return rule
}

func TestRuleGroupRuntimeCookiePatternEquality(t *testing.T) {
	for _, tc := range []struct{ inputKey, requestKey string }{
		{"included-cookies", "IncludedCookies"},
		{"excluded-cookies", "ExcludedCookies"},
	} {
		t.Run(tc.inputKey, func(t *testing.T) {
			cloud := newRuleGroupRuntimeCloud(t)
			executor := newRuleGroupExecutor(
				t, "evaluated", ruleGroupRuntimeLibraries(t), cloud.endpoint(t, "aws"),
			)
			all := ruleGroupMatchPatternRule("cookies", map[string]any{"all": map[string]any{}})
			empty := ruleGroupMatchPatternRule("cookies", map[string]any{
				"all": map[string]any{}, tc.inputKey: []any{},
			})
			executor.Inputs["rules"] = []any{all, empty}
			applyRuleGroup(t, executor, runtime.DecisionCreate)

			read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
			converted := ruleGroupMatchPatternRuleSDK(
				"Cookies", map[string]any{"All": map[string]any{}},
			)
			create := ipSetRequest{"CreateRuleGroup", "aws", "us-west-2", map[string]any{
				"Capacity": float64(20), "Name": "example", "Scope": "REGIONAL",
				"VisibilityConfig": ruleGroupVisibilitySDK("group-metric"),
				"Rules":            []any{converted},
			}}
			assert.Equal(t, append([]ipSetRequest{create}, read...), cloud.takeCalls())

			executor.Inputs["rules"] = []any{all}
			applyRuleGroup(t, executor, runtime.DecisionNoOp)
			require.Equal(t, read, cloud.takeCalls())

			for _, names := range [][]any{{"b", "a", "b"}, {"a", "b", "b"}} {
				executor.Inputs["rules"] = []any{ruleGroupMatchPatternRule(
					"cookies", map[string]any{tc.inputKey: names},
				)}
				applyRuleGroup(t, executor, runtime.DecisionUpdate)
				converted := ruleGroupMatchPatternRuleSDK(
					"Cookies", map[string]any{tc.requestKey: names},
				)
				update := ipSetDeleteBody("id-1", "example", "REGIONAL")
				update["VisibilityConfig"] = ruleGroupVisibilitySDK("group-metric")
				update["Rules"] = []any{converted}
				want := append(slices.Clone(read),
					ipSetRequest{"UpdateRuleGroup", "aws", "us-west-2", update})
				require.Equal(t, append(want, read...), cloud.takeCalls())
			}
		})
	}
}

func TestRuleGroupRuntimeHeaderPatternRequests(t *testing.T) {
	names := []any{"b", "a", "b"}
	for _, tc := range []struct {
		inputKey, requestKey string
		value                any
	}{
		{"all", "All", map[string]any{}},
		{"included-headers", "IncludedHeaders", names},
		{"excluded-headers", "ExcludedHeaders", names},
	} {
		t.Run(tc.inputKey, func(t *testing.T) {
			cloud := newRuleGroupRuntimeCloud(t)
			executor := newRuleGroupExecutor(
				t, "evaluated", ruleGroupRuntimeLibraries(t), cloud.endpoint(t, "aws"),
			)
			executor.Inputs["rules"] = []any{ruleGroupMatchPatternRule(
				"headers", map[string]any{tc.inputKey: tc.value},
			)}
			applyRuleGroup(t, executor, runtime.DecisionCreate)

			converted := ruleGroupMatchPatternRuleSDK(
				"Headers", map[string]any{tc.requestKey: tc.value},
			)
			read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
			create := ipSetRequest{"CreateRuleGroup", "aws", "us-west-2", map[string]any{
				"Capacity": float64(20), "Name": "example", "Scope": "REGIONAL",
				"VisibilityConfig": ruleGroupVisibilitySDK("group-metric"),
				"Rules":            []any{converted},
			}}
			assert.Equal(t, append([]ipSetRequest{create}, read...), cloud.takeCalls())

			executor.Inputs["description"] = "updated"
			applyRuleGroup(t, executor, runtime.DecisionUpdate)
			update := ipSetDeleteBody("id-1", "example", "REGIONAL")
			update["Description"] = "updated"
			update["VisibilityConfig"] = ruleGroupVisibilitySDK("group-metric")
			update["Rules"] = []any{converted}
			want := append(slices.Clone(read),
				ipSetRequest{"UpdateRuleGroup", "aws", "us-west-2", update})
			require.Equal(t, append(want, read...), cloud.takeCalls())

			entry := ipSetSnapshot(t, executor).Find(ruleGroupAddress)
			rules := entry.Outputs["rules"].([]any)
			require.Len(t, rules, 1)
			statement := rules[0].(map[string]any)["statement"].(map[string]any)
			xss := statement["xss-match-statement"].(map[string]any)
			fields := xss["field-to-match"].(map[string]any)
			headers := fields["headers"].(map[string]any)
			wantPattern := map[string]any{
				"all": nil, "included-headers": nil, "excluded-headers": nil,
			}
			wantPattern[tc.inputKey] = tc.value
			require.Equal(t, wantPattern, headers["match-pattern"])
		})
	}
}

func TestRuleGroupRuntimeSetEqualityAndFullUpdate(t *testing.T) {
	cloud := newRuleGroupRuntimeCloud(t)
	executor := newRuleGroupExecutor(
		t, "evaluated", ruleGroupRuntimeLibraries(t), cloud.endpoint(t, "aws"),
	)
	count := labelRule("alpha")
	block := labelRule("beta")
	headerA := map[string]any{"name": "X-Test", "value": "alpha"}
	headerB := map[string]any{"name": "X-Test", "value": "beta"}
	count["action"] = map[string]any{"count": map[string]any{
		"custom-request-handling": map[string]any{"insert-headers": []any{headerB, headerA}},
	}}
	block["action"] = map[string]any{"block": map[string]any{
		"custom-response": map[string]any{"response-code": int64(403),
			"custom-response-body-key": "blocked", "response-headers": []any{headerB, headerA}},
	}}
	labelA := map[string]any{"name": "id:alpha"}
	labelB := map[string]any{"name": "id:beta"}
	block["rule-labels"] = []any{labelB, labelA}
	transformA := map[string]any{"priority": int64(0), "type": "LOWERCASE"}
	transformB := map[string]any{"priority": int64(0), "type": "NONE"}
	byteMatch := map[string]any{
		"field-to-match":        map[string]any{"body": map[string]any{}},
		"positional-constraint": "EXACTLY", "search-string": "Zm9v",
		"text-transformations": []any{transformB, transformA},
	}
	block["statement"] = map[string]any{"byte-match-statement": byteMatch}
	executor.Inputs["rules"] = []any{block, count}
	executor.Inputs["description"] = "initial"
	executor.Inputs["custom-response-bodies"] = map[string]any{
		"blocked": map[string]any{"content": "initial", "content-type": "TEXT_PLAIN"},
	}
	executor.Inputs["tags"] = map[string]any{"remove": "a", "keep": "b", "aws:omit": "c"}
	applyRuleGroup(t, executor, runtime.DecisionCreate)

	read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
	wantCount := labelRuleSDK("alpha", 0)
	wantBlock := labelRuleSDK("beta", 0)
	headers := []any{
		map[string]any{"Name": "X-Test", "Value": "alpha"},
		map[string]any{"Name": "X-Test", "Value": "beta"},
	}
	wantCount["Action"] = map[string]any{"Count": map[string]any{
		"CustomRequestHandling": map[string]any{"InsertHeaders": headers},
	}}
	wantBlock["Action"] = map[string]any{"Block": map[string]any{
		"CustomResponse": map[string]any{"ResponseCode": float64(403),
			"CustomResponseBodyKey": "blocked", "ResponseHeaders": headers},
	}}
	wantBlock["RuleLabels"] = []any{
		map[string]any{"Name": "id:alpha"}, map[string]any{"Name": "id:beta"},
	}
	wantBlock["Statement"] = map[string]any{"ByteMatchStatement": map[string]any{
		"FieldToMatch":         map[string]any{"Body": map[string]any{"OversizeHandling": "CONTINUE"}},
		"PositionalConstraint": "EXACTLY", "SearchString": "Wm05dg==",
		"TextTransformations": []any{
			map[string]any{"Priority": float64(0), "Type": "LOWERCASE"},
			map[string]any{"Priority": float64(0), "Type": "NONE"},
		},
	}}
	create := map[string]any{
		"Capacity": float64(20), "Name": "example", "Scope": "REGIONAL",
		"VisibilityConfig": ruleGroupVisibilitySDK("group-metric"),
		"Description":      "initial", "Rules": []any{wantCount, wantBlock},
		"CustomResponseBodies": map[string]any{
			"blocked": map[string]any{"Content": "initial", "ContentType": "TEXT_PLAIN"},
		},
		"Tags": []any{map[string]any{"Key": "keep", "Value": "b"},
			map[string]any{"Key": "remove", "Value": "a"}},
	}
	want := append([]ipSetRequest{{"CreateRuleGroup", "aws", "us-west-2", create}}, read...)
	require.Equal(t, want, cloud.takeCalls())

	// Reverse every list-shaped set and repeat an exact element.
	executor.Inputs["rules"] = []any{count, block, count}
	count["action"].(map[string]any)["count"].(map[string]any)["custom-request-handling"] =
		map[string]any{"insert-headers": []any{headerA, headerB, headerA}}
	block["action"].(map[string]any)["block"].(map[string]any)["custom-response"] =
		map[string]any{"response-code": int64(403), "custom-response-body-key": "blocked",
			"response-headers": []any{headerA, headerB, headerA}}
	block["rule-labels"] = []any{labelA, labelB, labelA}
	byteMatch["text-transformations"] = []any{transformA, transformB, transformA}
	applyRuleGroup(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())

	executor.Inputs["tags"] = map[string]any{"keep": "b", "add": ""}
	applyRuleGroup(t, executor, runtime.DecisionUpdate)
	want = slices.Clone(read)
	want = append(want,
		ipSetRequest{"UntagResource", "aws", "us-west-2", map[string]any{
			"ResourceARN": read[1].body["ResourceARN"], "TagKeys": []any{"remove"},
		}},
		ipSetRequest{"TagResource", "aws", "us-west-2", map[string]any{
			"ResourceARN": read[1].body["ResourceARN"],
			"Tags":        []any{map[string]any{"Key": "add", "Value": ""}},
		}},
	)
	require.Equal(t, append(want, read...), cloud.takeCalls())

	executor.Inputs["description"], executor.Inputs["rules"] = nil, nil
	executor.Inputs["custom-response-bodies"] = map[string]any{}
	executor.Inputs["visibility"] = ruleGroupVisibility("next-metric")
	applyRuleGroup(t, executor, runtime.DecisionUpdate)
	update := ipSetDeleteBody("id-1", "example", "REGIONAL")
	update["VisibilityConfig"] = ruleGroupVisibilitySDK("next-metric")
	want = slices.Clone(read)
	want = append(want, ipSetRequest{"UpdateRuleGroup", "aws", "us-west-2", update})
	require.Equal(t, append(want, read...), cloud.takeCalls())
	entry := ipSetSnapshot(t, executor).Find(ruleGroupAddress)
	require.Nil(t, entry.Outputs["description"])
	require.Equal(t, []any{}, entry.Outputs["rules"])
	require.Equal(t, map[string]any{}, entry.Outputs["custom-response-bodies"])
	require.Equal(t, ruleGroupVisibility("next-metric"), entry.Outputs["visibility-config"])
	applyRuleGroup(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())
}

func TestRuleGroupRuntimeJSONModesAndArrayOrder(t *testing.T) {
	cloud := newRuleGroupRuntimeCloud(t)
	executor := newRuleGroupExecutor(
		t, "evaluated", ruleGroupRuntimeLibraries(t), cloud.endpoint(t, "aws"),
	)
	executor.Inputs["rules"] = []any{labelRule("alpha")}
	applyRuleGroup(t, executor, runtime.DecisionCreate)
	cloud.takeCalls()

	read := ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
	wantA := labelRuleSDK("alpha", 4)
	delete(wantA, "CaptchaConfig")
	wantA["ChallengeConfig"] = map[string]any{
		"ImmunityTimeProperty": map[string]any{"ImmunityTime": float64(9)},
	}
	wantB := labelRuleSDK("beta", 0)
	delete(wantB, "CaptchaConfig")
	wantB["Statement"] = map[string]any{"ByteMatchStatement": map[string]any{
		"SearchString": "WVdKag==", "FieldToMatch": map[string]any{"Body": map[string]any{}},
		"PositionalConstraint": "EXACTLY",
		"TextTransformations":  []any{map[string]any{"Priority": float64(0), "Type": "NONE"}},
	}}
	rawA, err := json.Marshal(wantA)
	require.NoError(t, err)
	rawB, err := json.Marshal(wantB)
	require.NoError(t, err)
	a := string(rawA)
	b := strings.Replace(string(rawB), "WVdKag==", "YWJj", 1)
	executor.Inputs["rules"] = nil
	decimal := strings.Replace(a, `"Priority":4,`, `"Priority":4.0,`, 1)
	executor.Inputs["rules-json"] = "[" + decimal + "," + b + "]"
	applyRuleGroup(t, executor, runtime.DecisionUpdate)
	calls := cloud.takeCalls()
	require.Equal(t, read, calls[:2])
	require.Equal(t, read, calls[3:])
	require.Equal(t, "UpdateRuleGroup", calls[2].operation)
	update := ipSetDeleteBody("id-1", "example", "REGIONAL")
	update["VisibilityConfig"] = ruleGroupVisibilitySDK("group-metric")
	update["Rules"] = []any{wantA, wantB}
	require.Equal(t, update, calls[2].body)
	entry := ipSetSnapshot(t, executor).Find(ruleGroupAddress)
	require.Nil(t, entry.Outputs["rules"])
	require.JSONEq(t, "["+a+","+b+"]", entry.Outputs["rules-json"].(string))

	reordered := `{ "Priority": 4e0, ` + strings.Replace(a[1:], `"Priority":4,`, "", 1)
	executor.Inputs["rules-json"] = "[ " + reordered + ", " + b + " ]"
	applyRuleGroup(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())

	for _, spelling := range []string{"-0", "-0.0", "-0e0"} {
		t.Run(spelling, func(t *testing.T) {
			signedZero := strings.Replace(b, `"Priority":0,`, `"Priority":`+spelling+",", 1)
			require.NotEqual(t, b, signedZero)
			executor.Inputs["rules-json"] = "[" + a + "," + signedZero + "]"
			applyRuleGroup(t, executor, runtime.DecisionNoOp)
			require.Equal(t, read, cloud.takeCalls())
		})
	}
	executor.Inputs["rules-json"] = "[" + a + "," + b + "]"
	applyRuleGroup(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())

	signedZero := strings.Replace(b, `"Priority":0,`, `"Priority":-0.0,`, 1)
	executor.Inputs["rules-json"] = "[" + signedZero + "," + a + "]"
	applyRuleGroup(t, executor, runtime.DecisionUpdate)
	update["Rules"] = []any{wantB, wantA}
	want := append(slices.Clone(read), ipSetRequest{"UpdateRuleGroup", "aws", "us-west-2", update})
	require.Equal(t, append(want, read...), cloud.takeCalls())
	entry = ipSetSnapshot(t, executor).Find(ruleGroupAddress)
	normalized := strings.Replace(b, `"Priority":0,`, `"Priority":-0,`, 1)
	require.Equal(t, "["+normalized+","+a+"]", entry.Outputs["rules-json"])

	executor.Inputs["rules-json"] = "[" + b + "," + a + "]"
	applyRuleGroup(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())

	executor.Inputs["rules-json"] = "[]"
	applyRuleGroup(t, executor, runtime.DecisionUpdate)
	update["Rules"] = []any{}
	want = append(slices.Clone(read), ipSetRequest{"UpdateRuleGroup", "aws", "us-west-2", update})
	require.Equal(t, append(want, read...), cloud.takeCalls())

	executor.Inputs["rules-json"] = nil
	executor.Inputs["rules"] = []any{labelRule("alpha")}
	applyRuleGroup(t, executor, runtime.DecisionUpdate)
	update["Rules"] = []any{labelRuleSDK("alpha", 0)}
	want = append(slices.Clone(read), ipSetRequest{"UpdateRuleGroup", "aws", "us-west-2", update})
	require.Equal(t, append(want, read...), cloud.takeCalls())
	entry = ipSetSnapshot(t, executor).Find(ruleGroupAddress)
	require.Nil(t, entry.Outputs["rules-json"])
	rules := entry.Outputs["rules"].([]any)
	statement := rules[0].(map[string]any)["statement"].(map[string]any)
	maps.DeleteFunc(statement, func(_ string, value any) bool { return value == nil })
	require.Equal(t, labelRule("alpha")["statement"], statement)
}

func TestRuleGroupWiderInputsAndReferencesFailBeforeReplacement(t *testing.T) {
	libraries := ruleGroupRuntimeLibraries(t)
	for _, tc := range []struct {
		name       string
		edit       func(map[string]any)
		diagnostic string
	}{
		{"SDK-only rule fields", func(rule map[string]any) {
			rule["challenge-config"] = map[string]any{}
			rule["override-action"] = map[string]any{"none": map[string]any{}}
		}, "challenge-config"},
		{"group references", func(rule map[string]any) {
			statement := rule["statement"].(map[string]any)
			statement["managed-rule-group-statement"] = map[string]any{}
			statement["rule-group-reference-statement"] = map[string]any{}
		}, "managed-rule-group-statement"},
		{"nested rate", func(rule map[string]any) {
			statement := rule["statement"].(map[string]any)
			statement["rate-based-statement"] = map[string]any{"limit": int64(10)}
			rule["statement"] = map[string]any{"not-statement": map[string]any{
				"statement": statement,
			}}
		}, "rate-based-statement"},
		{"invalid reference ARN", func(rule map[string]any) {
			rule["statement"] = map[string]any{"ip-set-reference-statement": map[string]any{
				"arn": "malformed",
			}}
		}, "invalid reference ARN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := newRuleGroupRuntimeCloud(t)
			url := cloud.endpoint(t, "aws")
			initial := newRuleGroupExecutor(t, "evaluated", libraries, url)
			applyRuleGroup(t, initial, runtime.DecisionCreate)
			cloud.takeCalls()

			executor := newRuleGroupExecutor(t, "deferred", libraries, url)
			executor.Store = initial.Store
			executor.Inputs["capacity"] = int64(21)
			executor.Inputs["metric"] = "known-metric"
			rule := labelRule("resolved")
			tc.edit(rule)
			executor.Inputs["pending-rule"] = rule
			saved := saveIPSetPlan(t, executor)
			ruleGroupPlanStep(t, saved, runtime.DecisionReplace)
			require.Equal(t, ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1"),
				cloud.takeCalls())
			_, err := executor.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, ruleGroupAddress)
			require.ErrorContains(t, err, tc.diagnostic)
			require.Empty(t, cloud.takeCalls())
			require.Equal(t, "id-1", ipSetSnapshot(t, executor).Find(ruleGroupAddress).Outputs["id"])
		})
	}
}

func TestRuleGroupPendingEquivalentRulesKeepNoOp(t *testing.T) {
	libraries := ruleGroupRuntimeLibraries(t)
	cloud := newRuleGroupRuntimeCloud(t)
	endpoint := cloud.endpoint(t, "aws")
	initial := newRuleGroupExecutor(t, "evaluated", libraries, endpoint)
	known := labelRule("known")
	known["priority"] = int64(3)
	known["visibility-config"] = ruleGroupVisibility("known-metric")
	resolved := labelRule("resolved")
	initial.Inputs["rules"] = []any{resolved, known, resolved}
	applyRuleGroup(t, initial, runtime.DecisionCreate)
	cloud.takeCalls()

	executor := newRuleGroupExecutor(t, "deferred", libraries, endpoint)
	executor.Store = initial.Store
	executor.Inputs["metric"] = "known-metric"
	executor.Inputs["pending-rule"] = resolved
	saved := saveIPSetPlan(t, executor)
	step := ruleGroupPlanStep(t, saved, runtime.DecisionUpdate)
	require.Equal(t, map[string][]string{"rules": {"action.seed.value"}}, step.UnresolvedInputs)
	require.Equal(t, []runtime.Decision{runtime.DecisionNoOp, runtime.DecisionUpdate},
		step.AllowedDecisions)
	require.Equal(t, ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1"),
		cloud.takeCalls())
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	require.Empty(t, cloud.takeCalls())

	entry := ipSetSnapshot(t, executor).Find(ruleGroupAddress)
	require.Equal(t, "id-1", entry.Outputs["id"])
	observed := entry.Outputs["rules"].([]any)
	wantNames := []string{"known", "resolved"}
	var names []string
	for _, item := range observed {
		names = append(names, item.(map[string]any)["name"].(string))
	}
	require.Equal(t, wantNames, names)
}

func TestRuleGroupPendingCustomKeyDefersKnownLength(t *testing.T) {
	libraries := ruleGroupRuntimeLibraries(t)
	for _, decision := range []runtime.Decision{runtime.DecisionCreate, runtime.DecisionReplace} {
		t.Run(string(decision), func(t *testing.T) {
			cloud := newRuleGroupRuntimeCloud(t)
			endpoint := cloud.endpoint(t, "aws")
			executor := newRuleGroupExecutor(t, "deferred-rate-length", libraries, endpoint)
			var read []ipSetRequest
			if decision == runtime.DecisionReplace {
				initial := newRuleGroupExecutor(t, "evaluated", libraries, endpoint)
				applyRuleGroup(t, initial, runtime.DecisionCreate)
				cloud.takeCalls()
				executor.Store = initial.Store
				executor.Inputs["capacity"] = int64(21)
				read = ruleGroupReadCalls("aws", "us-west-2", "REGIONAL", "example", "id-1")
			}
			executor.Inputs["pending-key"] = map[string]any{
				"ja4-fingerprint": map[string]any{"fallback-behavior": "NO_MATCH"},
			}
			saved := saveIPSetPlan(t, executor)
			step := ruleGroupPlanStep(t, saved, decision)
			require.Equal(t, map[string][]string{"rules": {"action.seed.value"}}, step.UnresolvedInputs)
			require.Equal(t, read, cloud.takeCalls())
			_, err := executor.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, ruleGroupAddress)
			require.ErrorContains(t, err,
				"rate-based statement contains an invalid limit or custom-keys length")
			require.Empty(t, cloud.takeCalls())
			if decision == runtime.DecisionReplace {
				entry := ipSetSnapshot(t, executor).Find(ruleGroupAddress)
				require.Equal(t, "id-1", entry.Outputs["id"])
			}
		})
	}
}
