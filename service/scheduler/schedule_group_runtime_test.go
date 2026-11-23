package scheduler_test

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
	"github.com/stretchr/testify/require"

	awsscheduler "github.com/cloudboss/unobin-library-aws/service/scheduler"
)

const (
	groupAddress     = "resource.group"
	groupLibraryPath = "github.com/cloudboss/unobin-library-aws//service/scheduler"
	groupWest        = "us-west-2"
	groupEast        = "us-east-2"
	groupInitial     = "example"
	groupNameRule    = groupAddress + ": schema: constraints[1] (predicate)"
	groupPrefixRule  = groupAddress + ": schema: constraints[2] (predicate)"
	groupBothRule    = groupAddress + ": schema: constraints[0] (at-most-one-of [name, name-prefix])"
)

type runtimeGroupCall struct {
	endpoint string
	region   string
	method   string
	path     string
}

type runtimeGroup struct {
	tags         map[string]string
	deleting     bool
	deleteChecks int
}

type groupCloud struct {
	mu       sync.Mutex
	groups   map[string]*runtimeGroup
	calls    []runtimeGroupCall
	getError string
	badName  bool
}

func newGroupCloud(t *testing.T) *groupCloud {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", groupWest)
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing-credentials"))
	return &groupCloud{groups: map[string]*runtimeGroup{}}
}

func groupARN(region, name string) string {
	return fmt.Sprintf("arn:aws:scheduler:%s:123456789012:schedule-group/%s", region, name)
}

func (c *groupCloud) endpoint(t *testing.T, label string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signing := strings.SplitN(r.Header.Get("Authorization"), "/", 5)
		if len(signing) != 5 {
			t.Errorf("missing SigV4 credential scope: %s", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		call := runtimeGroupCall{label, signing[2], r.Method, r.URL.Path}
		w.Header().Set("Content-Type", "application/json")
		c.mu.Lock()
		defer c.mu.Unlock()
		c.calls = append(c.calls, call)
		reply, code := c.reply(t, call, r)
		if code != "" {
			w.Header().Set("X-Amzn-Errortype", code)
			status := http.StatusBadRequest
			if code == "ResourceNotFoundException" {
				status = http.StatusNotFound
			}
			w.WriteHeader(status)
			reply = map[string]any{"message": "test Scheduler response"}
		}
		if err := json.NewEncoder(w).Encode(reply); err != nil {
			t.Errorf("encode Scheduler response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func (c *groupCloud) reply(
	t *testing.T, call runtimeGroupCall, request *http.Request,
) (map[string]any, string) {
	if name, ok := strings.CutPrefix(call.path, "/schedule-groups/"); ok {
		return c.replyGroup(t, call, request, name)
	}
	arn, ok := strings.CutPrefix(call.path, "/tags/")
	group := c.groups[arn]
	if !ok || group == nil {
		return nil, "ResourceNotFoundException"
	}
	switch call.method {
	case http.MethodGet:
		tags := make([]map[string]string, 0, len(group.tags))
		for _, key := range slices.Sorted(maps.Keys(group.tags)) {
			tags = append(tags, map[string]string{"Key": key, "Value": group.tags[key]})
		}
		return map[string]any{"Tags": tags}, ""
	case http.MethodPost:
		maps.Copy(group.tags, readGroupRequestTags(t, request))
	case http.MethodDelete:
		for _, key := range request.URL.Query()["TagKeys"] {
			delete(group.tags, key)
		}
	}
	return map[string]any{}, ""
}

func (c *groupCloud) replyGroup(
	t *testing.T, call runtimeGroupCall, request *http.Request, name string,
) (map[string]any, string) {
	arn := groupARN(call.region, name)
	if call.method == http.MethodPost {
		if c.groups[arn] != nil {
			return nil, "ConflictException"
		}
		tags := readGroupRequestTags(t, request)
		tags["aws:system"] = "keep"
		c.groups[arn] = &runtimeGroup{tags: tags}
		return map[string]any{"ScheduleGroupArn": arn}, ""
	}
	if call.method == http.MethodGet && c.getError != "" {
		return nil, c.getError
	}
	group := c.groups[arn]
	if group == nil {
		return nil, "ResourceNotFoundException"
	}
	if call.method == http.MethodDelete {
		group.deleting = true
		return map[string]any{}, ""
	}

	status := "ACTIVE"
	if group.deleting {
		group.deleteChecks++
		switch group.deleteChecks {
		case 1:
		case 2:
			status = "DELETING"
		default:
			delete(c.groups, arn)
			return nil, "ResourceNotFoundException"
		}
	}
	if c.badName {
		name = "unexpected-name"
	}
	return map[string]any{
		"Arn": arn, "Name": name, "State": status,
		"CreationDate": 1700000000, "LastModificationDate": 1700000010,
	}, ""
}

func readGroupRequestTags(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	var body struct {
		Tags []struct{ Key, Value string }
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("decode Scheduler request: %v", err)
	}
	tags := make(map[string]string, len(body.Tags))
	for _, tag := range body.Tags {
		tags[tag.Key] = tag.Value
	}
	return tags
}

func (c *groupCloud) takeCalls() []runtimeGroupCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls = nil
	return calls
}

type groupSeed struct {
	NextName *string `ub:"next-name"`
	NextTag  string  `ub:"next-tag"`
}

type groupSeedOutput struct {
	Name *string `ub:"name"`
	Tag  string  `ub:"tag"`
}

func (s *groupSeed) Run(context.Context, runtime.NoConfig) (*groupSeedOutput, error) {
	return &groupSeedOutput{Name: s.NextName, Tag: s.NextTag}, nil
}

func groupRuntimeLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	library := runtime.LibraryWithPath(awsscheduler.Library(), groupLibraryPath)
	library.Schema = readLibrarySchema(t)
	library.Constraints = map[string][]lang.ConstraintSpec{
		"resource.schedule-group": library.Schema.Resources["schedule-group"].Constraints,
	}
	return map[string]*runtime.Library{
		"aws-scheduler": library,
		"fixture": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[groupSeed, *groupSeedOutput, runtime.NoConfig, *groupSeed](),
		}},
	}
}

func groupExecutor(
	t *testing.T, fixture string, libs map[string]*runtime.Library, endpoint string,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join(scheduleGroupFixture(fixture), "factory.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	body := &parsed.Factory.Body
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libs), Libraries: libs, SyntaxSource: body,
		Store: store, Factory: state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"},
		Inputs: map[string]any{
			"aws-config": map[string]any{"endpoint-url": endpoint},
			"name":       groupInitial, "name-prefix": nil, "tags": nil, "tag-value": "",
		},
	}
}

func groupSavePlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func groupStep(t *testing.T, plan *runtime.PlanFile, decision runtime.Decision) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == groupAddress {
			require.Equal(t, decision, step.Decision)
			return step
		}
	}
	t.Fatalf("missing %s", groupAddress)
	return runtime.PlanStep{}
}

func groupApply(
	t *testing.T, executor *runtime.Executor, decision runtime.Decision,
) *runtime.ExecResult {
	t.Helper()
	saved := groupSavePlan(t, executor)
	groupStep(t, saved, decision)
	result, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	return result
}

func groupSnapshot(t *testing.T, executor *runtime.Executor) *state.Snapshot {
	t.Helper()
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	return snapshot
}

func groupPutSnapshot(t *testing.T, executor *runtime.Executor, snapshot *state.Snapshot) {
	t.Helper()
	rev, err := executor.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(rev))
}

func groupGet(endpoint, region, name string) runtimeGroupCall {
	return runtimeGroupCall{endpoint, region, http.MethodGet, "/schedule-groups/" + name}
}

func groupTags(endpoint, region, name, method string) runtimeGroupCall {
	return runtimeGroupCall{endpoint, region, method, "/tags/" + groupARN(region, name)}
}

func groupReads(endpoint, region, name string) []runtimeGroupCall {
	return []runtimeGroupCall{
		groupGet(endpoint, region, name), groupTags(endpoint, region, name, http.MethodGet),
	}
}

func groupCreates(endpoint, region, name string) []runtimeGroupCall {
	get := groupGet(endpoint, region, name)
	create := get
	create.method = http.MethodPost
	return []runtimeGroupCall{create, get, get, get, groupTags(endpoint, region, name, http.MethodGet)}
}

func groupDeletes(endpoint, region, name string) []runtimeGroupCall {
	get := groupGet(endpoint, region, name)
	remove := get
	remove.method = http.MethodDelete
	return []runtimeGroupCall{remove, get, get, get}
}

func TestScheduleGroupRuntimeConstraints(t *testing.T) {
	libraries := groupRuntimeLibraries(t)
	for _, tc := range []struct {
		label      string
		field      string
		value      any
		diagnostic string
	}{
		{"conflict", "name-prefix", "prefix-", groupBothRule},
		{"empty name", "name", "", groupNameRule},
		{"invalid name", "name", "invalid!", groupNameRule},
		{"overflow name", "name", strings.Repeat("a", 65), groupNameRule},
		{"empty prefix", "name-prefix", "", groupPrefixRule},
		{"invalid prefix", "name-prefix", "préfix", groupPrefixRule},
		{"overflow prefix", "name-prefix", strings.Repeat("p", 39), groupPrefixRule},
	} {
		t.Run(tc.label, func(t *testing.T) {
			cloud := newGroupCloud(t)
			executor := groupExecutor(t, "evaluated", libraries, cloud.endpoint(t, "primary"))
			groupApply(t, executor, runtime.DecisionCreate)
			require.Equal(t, groupCreates("primary", groupWest, groupInitial), cloud.takeCalls())

			groupApply(t, executor, runtime.DecisionNoOp)
			reads := groupReads("primary", groupWest, groupInitial)
			require.Equal(t, reads, cloud.takeCalls())
			saved := groupSavePlan(t, executor)
			groupStep(t, saved, runtime.DecisionNoOp)
			require.Equal(t, reads, cloud.takeCalls())

			executor.Inputs[tc.field] = tc.value
			if tc.field == "name-prefix" && tc.label != "conflict" {
				executor.Inputs["name"] = nil
			}
			_, err := executor.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, tc.diagnostic)
			require.Empty(t, cloud.takeCalls())

			historical := groupSnapshot(t, executor)
			historical.Find(groupAddress).Inputs[tc.field] = tc.value
			historical.Find(groupAddress).Inputs["name"] = executor.Inputs["name"]
			groupPutSnapshot(t, executor, historical)
			_, err = executor.Plan(t.Context())
			require.ErrorContains(t, err, tc.diagnostic)
			require.Equal(t, reads, cloud.takeCalls())

			executor.Destroy = true
			groupApply(t, executor, runtime.DecisionDestroy)
			want := slices.Concat(reads, reads, groupDeletes("primary", groupWest, groupInitial))
			require.Equal(t, want, cloud.takeCalls())
		})
	}
}

func TestScheduleGroupRuntimeDeferredReplacement(t *testing.T) {
	libraries := groupRuntimeLibraries(t)
	for _, next := range []string{"valid-next", "invalid!"} {
		t.Run(next, func(t *testing.T) {
			cloud := newGroupCloud(t)
			url := cloud.endpoint(t, "primary")
			initial := groupExecutor(t, "evaluated", libraries, url)
			groupApply(t, initial, runtime.DecisionCreate)
			require.Equal(t, groupCreates("primary", groupWest, groupInitial), cloud.takeCalls())

			executor := groupExecutor(t, "deferred", libraries, url)
			executor.Store = initial.Store
			executor.Inputs["name"] = next
			saved := groupSavePlan(t, executor)
			step := groupStep(t, saved, runtime.DecisionReplace)
			require.Equal(t, map[string][]string{"name": {"action.seed.name"}}, step.UnresolvedInputs)
			require.Equal(t, []string{"name"}, step.PendingReplacementReasons)
			reads := groupReads("primary", groupWest, groupInitial)
			require.Equal(t, reads, cloud.takeCalls())

			_, err := executor.ApplyPlan(t.Context(), saved)
			if next == "invalid!" {
				require.ErrorContains(t, err, groupNameRule)
				require.Empty(t, cloud.takeCalls())
				require.Equal(t, groupARN(groupWest, groupInitial),
					groupSnapshot(t, executor).Find(groupAddress).Outputs["arn"])
				return
			}
			require.NoError(t, err)
			want := slices.Concat(reads, groupDeletes("primary", groupWest, groupInitial),
				groupCreates("primary", groupWest, next))
			require.Equal(t, want, cloud.takeCalls())
			require.Equal(t, groupARN(groupWest, next),
				groupSnapshot(t, executor).Find(groupAddress).Outputs["arn"])
		})
	}
}

func TestScheduleGroupRuntimeRegionalReplacement(t *testing.T) {
	libraries := groupRuntimeLibraries(t)
	for _, region := range []string{groupEast, groupWest} {
		t.Run(region, func(t *testing.T) {
			cloud := newGroupCloud(t)
			old := cloud.endpoint(t, "old")
			executor := groupExecutor(t, "evaluated", libraries, old)
			groupApply(t, executor, runtime.DecisionCreate)
			require.Equal(t, groupCreates("old", groupWest, groupInitial), cloud.takeCalls())

			executor.Inputs["aws-config"] = map[string]any{
				"endpoint-url": cloud.endpoint(t, "new"), "region": region,
			}
			saved := groupSavePlan(t, executor)
			step := groupStep(t, saved, runtime.DecisionReplace)
			require.Equal(t, []string{"region"}, step.ReplacementReasons)
			require.Equal(t, groupARN(groupWest, groupInitial), *step.ExpectedStableID)
			reads := groupReads("old", groupWest, groupInitial)
			require.Equal(t, reads, cloud.takeCalls())

			_, err := executor.ApplyPlan(t.Context(), saved)
			require.NoError(t, err)
			want := slices.Concat(reads, groupDeletes("old", groupWest, groupInitial),
				groupCreates("new", region, groupInitial))
			require.Equal(t, want, cloud.takeCalls())
			output := groupSnapshot(t, executor).Find(groupAddress).Outputs
			require.Equal(t, groupARN(region, groupInitial), output["arn"])
			require.Equal(t, region, output["region"])
		})
	}
}

func TestScheduleGroupRuntimePrefixReplacement(t *testing.T) {
	cloud := newGroupCloud(t)
	executor := groupExecutor(t, "evaluated", groupRuntimeLibraries(t), cloud.endpoint(t, "primary"))
	executor.Inputs["name"] = nil
	executor.Inputs["name-prefix"] = strings.Repeat("p", 38)
	groupApply(t, executor, runtime.DecisionCreate)
	initial := groupSnapshot(t, executor).Find(groupAddress)
	name := initial.Outputs["name"].(string)
	require.Regexp(t, "^p{38}[0-9a-f]{26}$", name)
	require.Equal(t, map[string]any{
		"arn": groupARN(groupWest, name), "name": name, "name-prefix": strings.Repeat("p", 38),
		"creation-date": "2023-11-14T22:13:20Z", "last-modification-date": "2023-11-14T22:13:30Z",
		"state": "ACTIVE", "region": groupWest, "tags": map[string]any{},
	}, initial.Outputs)
	require.Equal(t, groupCreates("primary", groupWest, name), cloud.takeCalls())

	executor.Inputs["tags"] = map[string]any{"empty": ""}
	result := groupApply(t, executor, runtime.DecisionUpdate)
	reads := groupReads("primary", groupWest, name)
	want := slices.Concat(reads,
		[]runtimeGroupCall{groupTags("primary", groupWest, name, http.MethodPost)}, reads)
	require.Equal(t, want, cloud.takeCalls())
	require.Equal(t, map[string]any{"name": name, "tags": map[string]any{"empty": ""}},
		result.Outputs)
	require.Equal(t, initial.Outputs["arn"],
		groupSnapshot(t, executor).Find(groupAddress).Outputs["arn"])

	executor.Inputs["name-prefix"] = "next-"
	saved := groupSavePlan(t, executor)
	step := groupStep(t, saved, runtime.DecisionReplace)
	require.Equal(t, []string{"name-prefix"}, step.ReplacementReasons)
	require.Equal(t, reads, cloud.takeCalls())
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	nextName := groupSnapshot(t, executor).Find(groupAddress).Outputs["name"].(string)
	require.Regexp(t, "^next-[0-9a-f]{26}$", nextName)
	require.Equal(t, slices.Concat(reads, groupDeletes("primary", groupWest, name),
		groupCreates("primary", groupWest, nextName)), cloud.takeCalls())
}

func TestScheduleGroupRuntimeDrift(t *testing.T) {
	libraries := groupRuntimeLibraries(t)
	for _, kind := range []string{"missing", "denied", "contradictory identity", "tags"} {
		t.Run(kind, func(t *testing.T) {
			cloud := newGroupCloud(t)
			executor := groupExecutor(t, "evaluated", libraries, cloud.endpoint(t, "primary"))
			desired := map[string]any{"empty": "", "managed": "expected"}
			executor.Inputs["tags"] = desired
			groupApply(t, executor, runtime.DecisionCreate)
			arn := groupARN(groupWest, groupInitial)
			require.Equal(t, groupCreates("primary", groupWest, groupInitial), cloud.takeCalls())

			cloud.mu.Lock()
			group := cloud.groups[arn]
			switch kind {
			case "missing":
				delete(cloud.groups, arn)
			case "denied":
				cloud.getError = "AccessDeniedException"
			case "contradictory identity":
				cloud.badName = true
			case "tags":
				group.tags["managed"] = "drifted"
				group.tags["manual"] = "added"
			}
			cloud.mu.Unlock()

			get := []runtimeGroupCall{groupGet("primary", groupWest, groupInitial)}
			switch kind {
			case "missing":
				step := groupStep(t, groupSavePlan(t, executor), runtime.DecisionCreate)
				require.True(t, step.Gone())
				require.Equal(t, get, cloud.takeCalls())
			case "denied", "contradictory identity":
				_, err := executor.Plan(t.Context())
				message := "AccessDeniedException"
				if kind == "contradictory identity" {
					message = "returned name"
				}
				require.ErrorContains(t, err, message)
				require.Equal(t, get, cloud.takeCalls())
			case "tags":
				result := groupApply(t, executor, runtime.DecisionUpdate)
				reads := groupReads("primary", groupWest, groupInitial)
				want := slices.Concat(reads, []runtimeGroupCall{
					groupTags("primary", groupWest, groupInitial, http.MethodDelete),
					groupTags("primary", groupWest, groupInitial, http.MethodPost),
				}, reads)
				require.Equal(t, want, cloud.takeCalls())
				require.Equal(t, map[string]any{"name": groupInitial, "tags": desired},
					result.Outputs)
				cloud.mu.Lock()
				got := maps.Clone(group.tags)
				cloud.mu.Unlock()
				require.Equal(t, map[string]string{
					"empty": "", "managed": "expected", "aws:system": "keep",
				}, got)
			}
		})
	}
}

func TestScheduleGroupRuntimeDeferredEmptyTagAndEffectiveEquality(t *testing.T) {
	cloud := newGroupCloud(t)
	libraries := groupRuntimeLibraries(t)
	url := cloud.endpoint(t, "primary")
	initial := groupExecutor(t, "evaluated", libraries, url)
	groupApply(t, initial, runtime.DecisionCreate)
	require.Equal(t, groupCreates("primary", groupWest, groupInitial), cloud.takeCalls())

	reads := groupReads("primary", groupWest, groupInitial)
	for _, tags := range []map[string]any{{}, {"aws:system": "ignored"}} {
		initial.Inputs["tags"] = tags
		groupApply(t, initial, runtime.DecisionNoOp)
		require.Equal(t, reads, cloud.takeCalls())
	}

	executor := groupExecutor(t, "deferred-tags", libraries, url)
	executor.Store = initial.Store
	saved := groupSavePlan(t, executor)
	step := groupStep(t, saved, runtime.DecisionUpdate)
	require.Equal(t, map[string][]string{"tags": {"action.seed.tag"}}, step.UnresolvedInputs)
	require.Equal(t, reads, cloud.takeCalls())

	result, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"name": groupInitial, "tags": map[string]any{"empty": ""},
	}, result.Outputs)
	require.Equal(t, slices.Concat([]runtimeGroupCall{
		groupTags("primary", groupWest, groupInitial, http.MethodPost),
	}, reads), cloud.takeCalls())
	require.Equal(t, groupARN(groupWest, groupInitial),
		groupSnapshot(t, executor).Find(groupAddress).Outputs["arn"])
}

func TestScheduleGroupRuntimeUpdateUsesSavedRegion(t *testing.T) {
	cloud := newGroupCloud(t)
	executor := groupExecutor(t, "evaluated", groupRuntimeLibraries(t), cloud.endpoint(t, "old"))
	name := "Group.1_-" + strings.Repeat("x", 55)
	executor.Inputs["name"] = name
	groupApply(t, executor, runtime.DecisionCreate)
	require.Equal(t, groupCreates("old", groupWest, name), cloud.takeCalls())

	t.Setenv("AWS_REGION", groupEast)
	executor.Inputs["aws-config"] = map[string]any{"endpoint-url": cloud.endpoint(t, "new")}
	executor.Inputs["tags"] = map[string]any{"changed": ""}
	result := groupApply(t, executor, runtime.DecisionUpdate)
	require.Equal(t, slices.Concat(groupReads("old", groupWest, name), []runtimeGroupCall{
		groupTags("new", groupWest, name, http.MethodPost),
	}, groupReads("new", groupWest, name)), cloud.takeCalls())
	require.Equal(t, map[string]any{"name": name, "tags": map[string]any{"changed": ""}},
		result.Outputs)
	require.Equal(t, groupARN(groupWest, name),
		groupSnapshot(t, executor).Find(groupAddress).Outputs["arn"])
}
