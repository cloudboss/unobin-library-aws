package cloudwatch

import (
	"encoding/xml"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

const (
	compositeAddress = "resource.alarm"
	compositeExport  = "resource.composite-alarm"
	oldAlarm         = "us-east-1/old"
	nextAlarm        = "us-east-1/next"
	alarmActionA     = "arn:aws:sns:us-east-1:123456789012:a"
	alarmActionB     = "arn:aws:sns:us-east-1:123456789012:b"
)

type compositeCloudCall struct {
	action string
	target string
}

type compositeCloud struct {
	mu        sync.Mutex
	calls     []compositeCloudCall
	alarms    map[string]url.Values
	tags      map[string]map[string]string
	ambiguous bool
	fail      string
	failCode  string
}

func (cloud *compositeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cloud.mu.Lock()
	defer cloud.mu.Unlock()
	w.Header().Set("Content-Type", "text/xml")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	credentials := strings.Split(r.Header.Get("Authorization"), "/")
	region := credentials[2]
	form := r.PostForm
	action := form.Get("Action")
	name := form.Get("AlarmName")
	if name == "" {
		name = form.Get("AlarmNames.member.1")
	}
	if name == "" {
		_, name, _ = strings.Cut(form.Get("ResourceARN"), ":alarm:")
	}
	key := region + "/" + name
	cloud.calls = append(cloud.calls, compositeCloudCall{action, key})
	if action == cloud.fail {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "<ErrorResponse><Error><Code>%s</Code>"+
			"<Message>test error</Message></Error></ErrorResponse>", cloud.failCode)
		return
	}

	var result strings.Builder
	switch action {
	case "PutCompositeAlarm":
		cloud.alarms[key] = form
		if cloud.tags[key] == nil {
			cloud.tags[key] = map[string]string{}
			readCloudTags(form, cloud.tags[key])
		}
	case "DescribeAlarms":
		if item := cloud.alarms[key]; item != nil {
			result.WriteString("<CompositeAlarms>")
			writeCloudAlarm(&result, region, name, item)
			if cloud.ambiguous {
				writeCloudAlarm(&result, region, name, item)
			}
			result.WriteString("</CompositeAlarms>")
		}
	case "ListTagsForResource":
		result.WriteString("<Tags>")
		for tag, value := range cloud.tags[key] {
			result.WriteString("<member>")
			writeCloudField(&result, "Key", tag)
			writeCloudField(&result, "Value", value)
			result.WriteString("</member>")
		}
		result.WriteString("</Tags>")
	case "TagResource":
		readCloudTags(form, cloud.tags[key])
	case "UntagResource":
		for i := 1; form.Has(fmt.Sprintf("TagKeys.member.%d", i)); i++ {
			delete(cloud.tags[key], form.Get(fmt.Sprintf("TagKeys.member.%d", i)))
		}
	case "DeleteAlarms":
		delete(cloud.alarms, key)
		delete(cloud.tags, key)
	default:
		http.Error(w, "unexpected action "+action, http.StatusBadRequest)
		return
	}
	_, _ = fmt.Fprintf(w, "<%[1]sResponse><%[1]sResult>%[2]s</%[1]sResult></%[1]sResponse>",
		action, result.String())
}

func readCloudTags(form url.Values, tags map[string]string) {
	for i := 1; ; i++ {
		key := form.Get(fmt.Sprintf("Tags.member.%d.Key", i))
		if key == "" {
			return
		}
		tags[key] = form.Get(fmt.Sprintf("Tags.member.%d.Value", i))
	}
}

func writeCloudAlarm(out *strings.Builder, region, name string, fields url.Values) {
	out.WriteString("<member>")
	arn := "arn:" + partition.Of(region) + ":cloudwatch:" + region + ":123456789012:alarm:" + name
	writeCloudField(out, "AlarmArn", arn)
	for _, key := range []string{
		"AlarmName", "AlarmRule", "AlarmDescription", "ActionsEnabled", "ActionsSuppressor",
		"ActionsSuppressorExtensionPeriod", "ActionsSuppressorWaitPeriod",
	} {
		if fields.Has(key) {
			writeCloudField(out, key, fields.Get(key))
		}
	}
	for _, key := range []string{"AlarmActions", "OKActions", "InsufficientDataActions"} {
		out.WriteString("<" + key + ">")
		for i := 1; ; i++ {
			member := fmt.Sprintf("%s.member.%d", key, i)
			if !fields.Has(member) {
				break
			}
			writeCloudField(out, "member", fields.Get(member))
		}
		out.WriteString("</" + key + ">")
	}
	out.WriteString("</member>")
}

func writeCloudField(out *strings.Builder, key, value string) {
	out.WriteString("<" + key + ">")
	_ = xml.EscapeText(out, []byte(value))
	out.WriteString("</" + key + ">")
}

func (cloud *compositeCloud) takeCalls() []compositeCloudCall {
	cloud.mu.Lock()
	defer cloud.mu.Unlock()
	calls := cloud.calls
	cloud.calls = nil
	return calls
}

func (cloud *compositeCloud) changeAlarm(key, field, value string) {
	cloud.mu.Lock()
	defer cloud.mu.Unlock()
	cloud.alarms[key].Set(field, value)
}

func (cloud *compositeCloud) contents(key string) (url.Values, map[string]string) {
	cloud.mu.Lock()
	defer cloud.mu.Unlock()
	return maps.Clone(cloud.alarms[key]), maps.Clone(cloud.tags[key])
}

func newCompositeCloud(t *testing.T) (*compositeCloud, string) {
	t.Helper()
	cloud := &compositeCloud{alarms: map[string]url.Values{}, tags: map[string]map[string]string{}}
	server := httptest.NewServer(cloud)
	t.Cleanup(server.Close)
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	return cloud, server.URL
}

func compositeLibrary(t *testing.T) *runtime.Library {
	t.Helper()
	library := runtime.LibraryWithPath(Library(),
		"github.com/cloudboss/unobin-library-aws//service/cloudwatch")
	library.Schema = readLibrarySchema(t)
	library.Constraints = make(map[string][]lang.ConstraintSpec)
	for export, schema := range library.Schema.Resources {
		library.Constraints["resource."+export] = schema.Constraints
	}
	return library
}

func compositeExecutor(
	t *testing.T, library *runtime.Library, fixture, endpoint string, store state.Backend,
) *runtime.Executor {
	t.Helper()
	if store == nil {
		var err error
		store, err = local.NewStore(t.TempDir(), "composite-test", "test", encrypters.Noop{})
		require.NoError(t, err)
	}
	path := filepath.Join("testdata", "ub", "composite-alarm", fixture, "factory.ub")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	source, _, err := compile.ParseFactorySyntaxSource(path, data)
	require.NoError(t, err)
	body := source.Factory.Body
	libraries := map[string]*runtime.Library{"cloudwatch": library}
	syntax := check.NewSyntax(body, libraries, nil, "")
	require.NoError(t, syntax.References(nil).Err())
	require.NoError(t, syntax.LiteralConstraints().Err())
	inputs := map[string]any{
		"endpoint": endpoint, "region": "us-east-1",
	}
	if fixture == "evaluated" {
		inputs["name-key"], inputs["rule-key"] = "configured", "configured"
		inputs["enabled"], inputs["suppressor"], inputs["actions"], inputs["tags"] = nil, nil, nil, nil
		inputs["ok-actions"], inputs["insufficient-data-actions"] = nil, nil
	}
	return &runtime.Executor{
		DAG: syntax.DAG(), SyntaxSource: &body, Libraries: libraries, Store: store,
		Factory: state.FactoryInfo{Name: "composite-test", Version: "v0", ContentRevision: "c0"},
		Inputs:  inputs,
	}
}

func saveCompositePlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	data, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(data)
	require.NoError(t, err)
	return saved
}

func compositeStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == compositeAddress {
			return step
		}
	}
	t.Fatalf("plan has no step for %s", compositeAddress)
	return runtime.PlanStep{}
}

func applyCompositePlan(t *testing.T, executor *runtime.Executor, plan *runtime.PlanFile) {
	t.Helper()
	_, err := executor.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
}

func compositeObserve(target string) []compositeCloudCall {
	return []compositeCloudCall{{"DescribeAlarms", target}, {"ListTagsForResource", target}}
}

func compositeCreate(target string) []compositeCloudCall {
	return append([]compositeCloudCall{{"PutCompositeAlarm", target}}, compositeObserve(target)...)
}

func requireCompositeConstraint(t *testing.T, err error, rule, message string) {
	t.Helper()
	require.ErrorContains(t, err, compositeAddress+": schema: "+rule)
	require.ErrorContains(t, err, message)
}

func TestCompositeAlarmEvaluatedRules(t *testing.T) {
	library := compositeLibrary(t)
	for _, tc := range []struct {
		name, field, rule, message string
		input                      any
	}{
		{"null name", "name-key", "constraints[0]", "alarm-name must be a nonempty", "missing"},
		{"empty name", "name-key", "constraints[0]", "alarm-name must be a nonempty", "empty"},
		{"null rule", "rule-key", "constraints[1]", "alarm-rule must be a nonempty", "missing"},
		{"empty rule", "rule-key", "constraints[1]", "alarm-rule must be a nonempty", "empty"},
		{"null action", "actions", "constraints[7]", "alarm-actions entries", []any{nil}},
		{"invalid action", "actions", "constraints[7]", "alarm-actions entries", []any{"x"}},
		{"null suppressor alarm", "suppressor", "constraints[3]", "actions-suppressor requires",
			map[string]any{"alarm": nil, "extension-period": int64(0), "wait-period": int64(0)}},
		{"null suppressor period", "suppressor", "constraints[3]", "actions-suppressor requires",
			map[string]any{"alarm": "", "extension-period": nil, "wait-period": int64(0)}},
		{"narrowing", "suppressor", "constraints[3]", "signed 32-bit",
			map[string]any{"alarm": "", "extension-period": int64(0), "wait-period": int64(-2147483649)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud, endpoint := newCompositeCloud(t)
			exec := compositeExecutor(t, library, "evaluated", endpoint, nil)
			exec.Inputs[tc.field] = tc.input
			_, err := exec.Plan(t.Context())
			requireCompositeConstraint(t, err, tc.rule, tc.message)
			assert.Empty(t, cloud.takeCalls())
		})
	}
}

func TestCompositeAlarmNativeSavedNoOpAndHistoricalDestroy(t *testing.T) {
	cloud, endpoint := newCompositeCloud(t)
	library := compositeLibrary(t)
	exec := compositeExecutor(t, library, "evaluated", endpoint, nil)
	exec.Inputs["rule-key"] = "empty"

	rules := library.Constraints[compositeExport]
	delete(library.Constraints, compositeExport)
	applyCompositePlan(t, exec, saveCompositePlan(t, exec))
	assert.Equal(t, compositeCreate(oldAlarm), cloud.takeCalls())
	saved := saveCompositePlan(t, exec)
	assert.Equal(t, runtime.DecisionNoOp, compositeStep(t, saved).Decision)
	assert.Equal(t, compositeObserve(oldAlarm), cloud.takeCalls())

	library.Constraints[compositeExport] = rules
	_, err := exec.Plan(t.Context())
	requireCompositeConstraint(t, err, "constraints[1]", "alarm-rule must be a nonempty")
	assert.Equal(t, compositeObserve(oldAlarm), cloud.takeCalls())
	applyExec := compositeExecutor(t, library, "evaluated", endpoint, exec.Store)
	applyExec.Inputs = nil
	_, err = applyExec.ApplyPlan(t.Context(), saved)
	requireCompositeConstraint(t, err, "constraints[1]", "alarm-rule must be a nonempty")
	assert.Empty(t, cloud.takeCalls())

	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	snapshot.Find(compositeAddress).Inputs["alarm-name"] = ""
	revision, err := exec.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, exec.Store.SetCurrent(revision))
	exec.Destroy = true
	applyCompositePlan(t, exec, saveCompositePlan(t, exec))
	assert.Equal(t, slices.Concat(compositeObserve(oldAlarm), compositeObserve(oldAlarm),
		[]compositeCloudCall{{"DeleteAlarms", oldAlarm}}), cloud.takeCalls())
}

func TestCompositeAlarmDeferredReplacement(t *testing.T) {
	library := compositeLibrary(t)
	for _, tc := range []struct {
		selector string
		valid    bool
	}{{"next", true}, {"empty", false}, {"missing", false}} {
		t.Run(tc.selector, func(t *testing.T) {
			cloud, endpoint := newCompositeCloud(t)
			initial := compositeExecutor(t, library, "evaluated", endpoint, nil)
			applyCompositePlan(t, initial, saveCompositePlan(t, initial))
			assert.Equal(t, compositeCreate(oldAlarm), cloud.takeCalls())

			exec := compositeExecutor(t, library, "deferred", endpoint, initial.Store)
			exec.Inputs["seed-name"] = "seed-" + tc.selector
			seedAlarm := "us-east-1/seed-" + tc.selector
			saved := saveCompositePlan(t, exec)
			step := compositeStep(t, saved)
			assert.Equal(t, runtime.DecisionReplace, step.Decision)
			assert.Equal(t, []string{"alarm-name"}, step.PendingReplacementReasons)
			assert.Equal(t, map[string][]string{"alarm-name": {"resource.seed.arn"}},
				step.UnresolvedInputs)
			assert.Equal(t, compositeObserve(oldAlarm), cloud.takeCalls())

			exec.Inputs = nil
			_, err := exec.ApplyPlan(t.Context(), saved)
			want := compositeCreate(seedAlarm)
			if tc.valid {
				require.NoError(t, err)
				want = slices.Concat(want, compositeObserve(oldAlarm),
					[]compositeCloudCall{{"DeleteAlarms", oldAlarm}}, compositeCreate(nextAlarm),
					compositeObserve(seedAlarm))
			} else {
				requireCompositeConstraint(t, err, "constraints[0]", "alarm-name must be a nonempty")
			}
			assert.Equal(t, want, cloud.takeCalls())
		})
	}
}

func TestCompositeAlarmReplacementIdentity(t *testing.T) {
	library := compositeLibrary(t)
	for _, tc := range []struct {
		field, input, value, target string
	}{
		{"alarm-name", "name-key", "next", nextAlarm},
		{"region", "region", "us-west-2", "us-west-2/old"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			cloud, endpoint := newCompositeCloud(t)
			exec := compositeExecutor(t, library, "evaluated", endpoint, nil)
			applyCompositePlan(t, exec, saveCompositePlan(t, exec))
			assert.Equal(t, compositeCreate(oldAlarm), cloud.takeCalls())

			exec.Inputs[tc.input] = tc.value
			saved := saveCompositePlan(t, exec)
			step := compositeStep(t, saved)
			assert.Equal(t, runtime.DecisionReplace, step.Decision)
			assert.Equal(t, []string{tc.field}, step.ReplacementReasons)
			assert.Equal(t, compositeObserve(oldAlarm), cloud.takeCalls())
			applyCompositePlan(t, exec, saved)
			assert.Equal(t, slices.Concat(compositeObserve(oldAlarm),
				[]compositeCloudCall{{"DeleteAlarms", oldAlarm}}, compositeCreate(tc.target)),
				cloud.takeCalls())
		})
	}
}

func TestCompositeAlarmPriorImplicitRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	cloud, endpoint := newCompositeCloud(t)
	exec := compositeExecutor(t, compositeLibrary(t), "evaluated", endpoint, nil)
	exec.Inputs["region"] = nil
	applyCompositePlan(t, exec, saveCompositePlan(t, exec))
	require.Equal(t, compositeCreate(oldAlarm), cloud.takeCalls())
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	require.Nil(t, snapshot.Find(compositeAddress).Configuration["region"])

	t.Setenv("AWS_REGION", "us-west-2")
	saved := saveCompositePlan(t, exec)
	require.Equal(t, compositeObserve(oldAlarm), cloud.takeCalls())
	require.Equal(t, runtime.DecisionNoOp, compositeStep(t, saved).Decision)
	applyCompositePlan(t, exec, saved)
	assert.Empty(t, cloud.takeCalls())

	exec.Inputs["enabled"] = false
	saved = saveCompositePlan(t, exec)
	require.Equal(t, runtime.DecisionUpdate, compositeStep(t, saved).Decision)
	applyCompositePlan(t, exec, saved)
	assert.Equal(t, slices.Concat(compositeObserve(oldAlarm), compositeCreate(oldAlarm)),
		cloud.takeCalls())
	assert.Equal(t, false, compositeOutputs(t, exec)["actions-enabled"])

	exec.Destroy = true
	saved = saveCompositePlan(t, exec)
	assert.False(t, compositeStep(t, saved).AlreadyGone)
	applyCompositePlan(t, exec, saved)
	assert.Equal(t, slices.Concat(compositeObserve(oldAlarm), compositeObserve(oldAlarm),
		[]compositeCloudCall{{"DeleteAlarms", oldAlarm}}), cloud.takeCalls())
	alarm, tags := cloud.contents(oldAlarm)
	assert.Nil(t, alarm)
	assert.Nil(t, tags)
}

func compositeOutputs(t *testing.T, exec *runtime.Executor) map[string]any {
	t.Helper()
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	return snapshot.Find(compositeAddress).Outputs
}

func TestCompositeAlarmActionPresenceUpdates(t *testing.T) {
	library := compositeLibrary(t)
	for _, tc := range []struct {
		input, output, query string
	}{
		{"actions", "alarm-actions", "AlarmActions"},
		{"ok-actions", "ok-actions", "OKActions"},
		{"insufficient-data-actions", "insufficient-data-actions", "InsufficientDataActions"},
	} {
		t.Run(tc.output, func(t *testing.T) {
			cloud, endpoint := newCompositeCloud(t)
			exec := compositeExecutor(t, library, "evaluated", endpoint, nil)
			applyCompositePlan(t, exec, saveCompositePlan(t, exec))
			require.Equal(t, compositeCreate(oldAlarm), cloud.takeCalls())

			for _, transition := range []struct {
				value    any
				decision runtime.Decision
				empty    bool
			}{
				{[]any{}, runtime.DecisionNoOp, false},
				{[]any{""}, runtime.DecisionUpdate, true},
				{[]any{"", ""}, runtime.DecisionNoOp, true},
				{nil, runtime.DecisionUpdate, false},
				{[]any{""}, runtime.DecisionUpdate, true},
				{[]any{}, runtime.DecisionUpdate, false},
			} {
				exec.Inputs[tc.input] = transition.value
				saved := saveCompositePlan(t, exec)
				require.Equal(t, transition.decision, compositeStep(t, saved).Decision)
				applyCompositePlan(t, exec, saved)
				calls := compositeObserve(oldAlarm)
				if transition.decision == runtime.DecisionUpdate {
					calls = slices.Concat(calls, compositeCreate(oldAlarm))
				}
				assert.Equal(t, calls, cloud.takeCalls())
				alarm, _ := cloud.contents(oldAlarm)
				if transition.empty {
					assert.Equal(t, []string{""}, alarm[tc.query])
				} else {
					assert.NotContains(t, alarm, tc.query)
				}
				assert.Equal(t, []any{}, compositeOutputs(t, exec)[tc.output])
			}
		})
	}
}

func TestCompositeAlarmInPlaceUpdatesAndDrift(t *testing.T) {
	library := compositeLibrary(t)
	cloud, endpoint := newCompositeCloud(t)
	exec := compositeExecutor(t, library, "evaluated", endpoint, nil)
	exec.Inputs["actions"] = []any{alarmActionB, alarmActionA, alarmActionB}
	exec.Inputs["tags"] = map[string]any{"env": "initial", "remove": "yes"}
	applyCompositePlan(t, exec, saveCompositePlan(t, exec))
	assert.Equal(t, compositeCreate(oldAlarm), cloud.takeCalls())
	initial := compositeOutputs(t, exec)
	assert.Equal(t, map[string]any{
		"arn":                       "arn:aws:cloudwatch:us-east-1:123456789012:alarm:old",
		"alarm-name":                "old",
		"alarm-rule":                "TRUE",
		"alarm-description":         "",
		"actions-enabled":           true,
		"actions-suppressor":        nil,
		"alarm-actions":             []any{alarmActionA, alarmActionB},
		"ok-actions":                []any{},
		"insufficient-data-actions": []any{},
		"tags":                      map[string]any{"env": "initial", "remove": "yes"},
	}, initial)

	exec.Inputs["enabled"] = true
	exec.Inputs["actions"] = []any{alarmActionA, alarmActionB}
	saved := saveCompositePlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp, compositeStep(t, saved).Decision)
	applyCompositePlan(t, exec, saved)
	assert.Equal(t, compositeObserve(oldAlarm), cloud.takeCalls())

	for _, enabled := range []bool{false, true} {
		exec.Inputs["enabled"] = enabled
		saved = saveCompositePlan(t, exec)
		require.Equal(t, runtime.DecisionUpdate, compositeStep(t, saved).Decision)
		applyCompositePlan(t, exec, saved)
		assert.Equal(t, slices.Concat(compositeObserve(oldAlarm), compositeCreate(oldAlarm)),
			cloud.takeCalls())
		out := compositeOutputs(t, exec)
		assert.Equal(t, initial["arn"], out["arn"])
		assert.Equal(t, enabled, out["actions-enabled"])
	}

	suppressor := map[string]any{
		"alarm": "mute", "extension-period": int64(0), "wait-period": int64(30),
	}
	exec.Inputs["suppressor"] = suppressor
	applyCompositePlan(t, exec, saveCompositePlan(t, exec))
	assert.Equal(t, slices.Concat(compositeObserve(oldAlarm), compositeCreate(oldAlarm)),
		cloud.takeCalls())
	assert.Equal(t, map[string]any{
		"alarm": "mute", "extension-period": float64(0), "wait-period": float64(30),
	}, compositeOutputs(t, exec)["actions-suppressor"])

	exec.Inputs["suppressor"] = nil
	exec.Inputs["actions"] = nil
	applyCompositePlan(t, exec, saveCompositePlan(t, exec))
	assert.Equal(t, slices.Concat(compositeObserve(oldAlarm), compositeCreate(oldAlarm)),
		cloud.takeCalls())
	out := compositeOutputs(t, exec)
	assert.Nil(t, out["actions-suppressor"])
	assert.Equal(t, []any{}, out["alarm-actions"])

	cloud.changeAlarm(oldAlarm, "AlarmRule", "FALSE")
	cloud.changeAlarm(oldAlarm, "AlarmActions.member.1", alarmActionA)
	applyCompositePlan(t, exec, saveCompositePlan(t, exec))
	assert.Equal(t, slices.Concat(compositeObserve(oldAlarm), compositeCreate(oldAlarm)),
		cloud.takeCalls())
	out = compositeOutputs(t, exec)
	assert.Equal(t, "TRUE", out["alarm-rule"])
	assert.Equal(t, []any{}, out["alarm-actions"])

	exec.Inputs["tags"] = map[string]any{"env": "updated", "empty": ""}
	applyCompositePlan(t, exec, saveCompositePlan(t, exec))
	assert.Equal(t, slices.Concat(compositeObserve(oldAlarm), []compositeCloudCall{
		{"ListTagsForResource", oldAlarm}, {"UntagResource", oldAlarm}, {"TagResource", oldAlarm},
	}, compositeObserve(oldAlarm)), cloud.takeCalls())
	out = compositeOutputs(t, exec)
	assert.Equal(t, map[string]any{"env": "updated", "empty": ""}, out["tags"])

	cloud.mu.Lock()
	cloud.tags[oldAlarm]["env"] = "drifted"
	cloud.mu.Unlock()
	applyCompositePlan(t, exec, saveCompositePlan(t, exec))
	assert.Equal(t, slices.Concat(compositeObserve(oldAlarm), []compositeCloudCall{
		{"ListTagsForResource", oldAlarm}, {"TagResource", oldAlarm},
	}, compositeObserve(oldAlarm)), cloud.takeCalls())
	out = compositeOutputs(t, exec)
	assert.Equal(t, map[string]any{"env": "updated", "empty": ""}, out["tags"])
}

func TestCompositeAlarmAmbiguityStopsPlan(t *testing.T) {
	cloud, endpoint := newCompositeCloud(t)
	exec := compositeExecutor(t, compositeLibrary(t), "evaluated", endpoint, nil)
	applyCompositePlan(t, exec, saveCompositePlan(t, exec))
	assert.Equal(t, compositeCreate(oldAlarm), cloud.takeCalls())

	cloud.mu.Lock()
	cloud.ambiguous = true
	cloud.mu.Unlock()
	plan, err := exec.Plan(t.Context())
	require.ErrorContains(t, err, "multiple composite alarms")
	assert.Nil(t, plan)
	assert.Equal(t, []compositeCloudCall{{"DescribeAlarms", oldAlarm}}, cloud.takeCalls())
}

func TestCompositeAlarmMixedUpdate(t *testing.T) {
	library := compositeLibrary(t)
	for _, tc := range []struct {
		name, region, failedAction, failCode string
		actions                              []string
		alarmRule                            string
		tags                                 map[string]string
	}{
		{
			name: "success", region: "us-east-1",
			actions: []string{"ListTagsForResource", "UntagResource", "TagResource",
				"PutCompositeAlarm", "DescribeAlarms", "ListTagsForResource"},
			alarmRule: "FALSE", tags: map[string]string{"env": "", "keep": "same"},
		},
		{
			name: "removal failed", region: "us-east-1",
			failedAction: "UntagResource", failCode: "AccessDenied",
			actions:   []string{"ListTagsForResource", "UntagResource"},
			alarmRule: "TRUE", tags: map[string]string{"env": "before", "keep": "same", "drop": "yes"},
		},
		{
			name: "tag write failed", region: "us-east-1",
			failedAction: "TagResource", failCode: "AccessDenied",
			actions:   []string{"ListTagsForResource", "UntagResource", "TagResource"},
			alarmRule: "TRUE", tags: map[string]string{"env": "before", "keep": "same"},
		},
		{
			name: "settings write failed", region: "us-east-1",
			failedAction: "PutCompositeAlarm", failCode: "ValidationError",
			actions: []string{"ListTagsForResource", "UntagResource", "TagResource",
				"PutCompositeAlarm"},
			alarmRule: "TRUE", tags: map[string]string{"env": "", "keep": "same"},
		},
		{
			name: "readback failed", region: "us-east-1",
			failedAction: "DescribeAlarms", failCode: "AccessDenied",
			actions: []string{"ListTagsForResource", "UntagResource", "TagResource",
				"PutCompositeAlarm", "DescribeAlarms"},
			alarmRule: "FALSE", tags: map[string]string{"env": "", "keep": "same"},
		},
		{
			name: "suppressed removal", region: "us-gov-west-1",
			failedAction: "UntagResource", failCode: "UnsupportedOperation",
			actions: []string{"ListTagsForResource", "UntagResource", "PutCompositeAlarm",
				"DescribeAlarms", "ListTagsForResource"},
			alarmRule: "FALSE", tags: map[string]string{"env": "before", "keep": "same", "drop": "yes"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud, endpoint := newCompositeCloud(t)
			exec := compositeExecutor(t, library, "evaluated", endpoint, nil)
			exec.Inputs["region"] = tc.region
			exec.Inputs["tags"] = map[string]any{"env": "before", "keep": "same", "drop": "yes"}
			target := tc.region + "/old"
			applyCompositePlan(t, exec, saveCompositePlan(t, exec))
			require.Equal(t, compositeCreate(target), cloud.takeCalls())

			exec.Inputs["rule-key"] = "changed"
			exec.Inputs["tags"] = map[string]any{"env": "", "keep": "same"}
			saved := saveCompositePlan(t, exec)
			require.Equal(t, runtime.DecisionUpdate, compositeStep(t, saved).Decision)
			require.Equal(t, compositeObserve(target), cloud.takeCalls())
			cloud.mu.Lock()
			cloud.fail, cloud.failCode = tc.failedAction, tc.failCode
			cloud.mu.Unlock()

			_, err := exec.ApplyPlan(t.Context(), saved)
			if tc.failCode == "" || tc.failCode == "UnsupportedOperation" {
				require.NoError(t, err)
				output := compositeOutputs(t, exec)
				assert.Equal(t, tc.alarmRule, output["alarm-rule"])
				want := make(map[string]any, len(tc.tags))
				for key, value := range tc.tags {
					want[key] = value
				}
				assert.Equal(t, want, output["tags"])
			} else {
				require.ErrorContains(t, err, tc.failCode)
			}
			var calls []compositeCloudCall
			for _, action := range tc.actions {
				calls = append(calls, compositeCloudCall{action, target})
			}
			assert.Equal(t, calls, cloud.takeCalls())
			alarm, tags := cloud.contents(target)
			assert.Equal(t, tc.alarmRule, alarm.Get("AlarmRule"))
			assert.Equal(t, tc.tags, tags)
		})
	}
}

func TestCompositeAlarmDeferredTagsUpdate(t *testing.T) {
	library := compositeLibrary(t)
	cloud, endpoint := newCompositeCloud(t)
	initial := compositeExecutor(t, library, "evaluated", endpoint, nil)
	initial.Inputs["tags"] = map[string]any{"env": "before", "drop": "yes"}
	applyCompositePlan(t, initial, saveCompositePlan(t, initial))
	require.Equal(t, compositeCreate(oldAlarm), cloud.takeCalls())

	exec := compositeExecutor(t, library, "deferred-tags", endpoint, initial.Store)
	saved := saveCompositePlan(t, exec)
	step := compositeStep(t, saved)
	require.Equal(t, runtime.DecisionUpdate, step.Decision)
	assert.Equal(t, map[string][]string{"tags": {"resource.seed.arn"}}, step.UnresolvedInputs)
	assert.Equal(t, compositeObserve(oldAlarm), cloud.takeCalls())

	exec.Inputs = nil
	applyCompositePlan(t, exec, saved)
	seed := "us-east-1/seed"
	assert.Equal(t, slices.Concat(compositeCreate(seed), []compositeCloudCall{
		{"ListTagsForResource", oldAlarm}, {"UntagResource", oldAlarm}, {"TagResource", oldAlarm},
	}, compositeCreate(oldAlarm), compositeObserve(seed)), cloud.takeCalls())
	output := compositeOutputs(t, exec)
	assert.Equal(t, "FALSE", output["alarm-rule"])
	assert.Equal(t, map[string]any{
		"env": "", "seed": "arn:aws:cloudwatch:us-east-1:123456789012:alarm:seed",
	}, output["tags"])
}
