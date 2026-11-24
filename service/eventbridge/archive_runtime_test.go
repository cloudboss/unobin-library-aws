package eventbridge

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const archiveARN = "arn:aws:events:us-east-1:123456789012:event-bus/test"

type archiveAPI struct {
	mu       sync.Mutex
	calls    []busPolicyRequest
	archives map[string]map[string]any
	failRead bool
	regions  []string
}

func archiveEndpoint(t *testing.T) (string, *archiveAPI) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	api := &archiveAPI{archives: map[string]map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		operation := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "AWSEvents.")
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		api.calls = append(api.calls, busPolicyRequest{operation, body})
		api.regions = append(api.regions, strings.Split(r.Header.Get("Authorization"), "/")[2])
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		name, _ := body["ArchiveName"].(string)
		switch operation {
		case "CreateArchive":
			body = maps.Clone(body)
			body["ArchiveArn"] = "arn:aws:events:us-east-1:123456789012:archive/" + name
			api.archives[name] = body
			_, _ = w.Write([]byte(`{}`))
		case "UpdateArchive":
			maps.Copy(api.archives[name], body)
			_, _ = w.Write([]byte(`{}`))
		case "DescribeArchive":
			if api.failRead {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"__type":"InternalException","message":"read failed"}`))
				return
			}
			if api.archives[name] == nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException"}`))
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode(api.archives[name]))
		case "DeleteArchive":
			delete(api.archives, name)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected operation %s", operation)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL, api
}

func (api *archiveAPI) mutations() []busPolicyRequest {
	api.mu.Lock()
	defer api.mu.Unlock()
	var calls []busPolicyRequest
	for _, call := range api.calls {
		if call.Operation != "DescribeArchive" {
			calls = append(calls, call)
		}
	}
	api.calls = nil
	return calls
}

func archiveExecutor(t *testing.T, fixture string) *runtime.Executor {
	t.Helper()
	lib := runtime.LibraryWithPath(Library(),
		"github.com/cloudboss/unobin-library-aws//service/eventbridge")
	lib.Schema = readLibrarySchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.archive": lib.Schema.Resources["archive"].Constraints,
	}
	libs := map[string]*runtime.Library{"eventbridge": lib,
		"test": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[putEventsSeed, map[string]any, *awscfg.Configuration](),
		}}}
	path := filepath.Join("testdata", "ub", "archive", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	store, err := local.NewStore(t.TempDir(), "archive", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "archive", Version: "v0", ContentRevision: "test"}}
}

func archiveInputs(endpoint string) map[string]any {
	return map[string]any{"endpoint": endpoint, "name": "old", "source": archiveARN,
		"description": "initial", "pattern": `{ "x": 1 }`, "key": "alias/key",
		"days": 30, "trigger": "first", "region": "us-east-1"}
}

func archiveCreateCall(name, description string) busPolicyRequest {
	return busPolicyRequest{"CreateArchive", map[string]any{
		"ArchiveName": name, "EventSourceArn": archiveARN, "Description": description,
		"EventPattern": `{"x":1}`, "KmsKeyIdentifier": "alias/key", "RetentionDays": float64(30)}}
}

func archiveStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == "resource.archive" {
			return step
		}
	}
	t.Fatal("missing archive step")
	return runtime.PlanStep{}
}

func savedArchivePlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
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

func TestArchiveNativeLifecycle(t *testing.T) {
	endpoint, api := archiveEndpoint(t)
	executor := archiveExecutor(t, "evaluated")
	executor.Inputs = archiveInputs(endpoint)
	created := savedArchivePlan(t, executor)
	assert.Equal(t, runtime.DecisionCreate, archiveStep(t, created).Decision)
	_, err := executor.ApplyPlan(t.Context(), created)
	require.NoError(t, err)
	assert.Equal(t, []busPolicyRequest{{"CreateArchive", map[string]any{
		"ArchiveName": "old", "EventSourceArn": archiveARN, "Description": "initial",
		"EventPattern": `{"x":1}`, "KmsKeyIdentifier": "alias/key", "RetentionDays": float64(30)}}},
		api.mutations())
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"archive-name": "old",
		"archive-arn":      "arn:aws:events:us-east-1:123456789012:archive/old",
		"event-source-arn": archiveARN,
		"description":      "initial", "event-pattern": `{"x":1}`, "kms-key-identifier": "alias/key",
		"retention-days": int64(30)}, snapshot.Find("resource.archive").Outputs)
	unchanged := savedArchivePlan(t, executor)
	assert.Equal(t, runtime.DecisionNoOp, archiveStep(t, unchanged).Decision)
	executor.Inputs = nil
	_, err = executor.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	assert.Empty(t, api.mutations())
	executor.Inputs = archiveInputs(endpoint)
	executor.Inputs["pattern"] = `{"x":1.0}`
	equivalent := savedArchivePlan(t, executor)
	assert.Equal(t, runtime.DecisionNoOp, archiveStep(t, equivalent).Decision)
	executor.Inputs = archiveInputs(endpoint)
	api.mu.Lock()
	api.archives["old"]["Description"] = "drift"
	api.mu.Unlock()
	update := savedArchivePlan(t, executor)
	assert.Equal(t, runtime.DecisionUpdate, archiveStep(t, update).Decision)
	_, err = executor.ApplyPlan(t.Context(), update)
	require.NoError(t, err)
	assert.Equal(t, []busPolicyRequest{{"UpdateArchive", map[string]any{
		"ArchiveName": "old", "Description": "initial", "EventPattern": `{"x":1}`,
		"KmsKeyIdentifier": "alias/key", "RetentionDays": float64(30)}}}, api.mutations())
	for _, field := range []string{"name", "source", "region"} {
		old := executor.Inputs[field]
		if field == "name" {
			executor.Inputs[field] = "new"
		}
		if field == "source" {
			executor.Inputs[field] = archiveARN + "-new"
		}
		if field == "region" {
			executor.Inputs[field] = "us-west-2"
		}
		plan := savedArchivePlan(t, executor)
		assert.Equal(t, runtime.DecisionReplace, archiveStep(t, plan).Decision)
		executor.Inputs[field] = old
	}
	executor.Inputs["name"] = "new"
	replaced := savedArchivePlan(t, executor)
	_, err = executor.ApplyPlan(t.Context(), replaced)
	require.NoError(t, err)
	mutations := api.mutations()
	assert.Equal(t, []busPolicyRequest{
		{"DeleteArchive", map[string]any{"ArchiveName": "old"}},
		archiveCreateCall("new", "initial"),
	}, mutations)
	executor.Inputs["source"] = archiveARN + "-new"
	sourceReplacement := savedArchivePlan(t, executor)
	require.Equal(t, runtime.DecisionReplace, archiveStep(t, sourceReplacement).Decision)
	_, err = executor.ApplyPlan(t.Context(), sourceReplacement)
	require.NoError(t, err)
	sourceCreate := archiveCreateCall("new", "initial")
	sourceCreate.Body["EventSourceArn"] = archiveARN + "-new"
	assert.Equal(t, []busPolicyRequest{
		{"DeleteArchive", map[string]any{"ArchiveName": "new"}}, sourceCreate,
	}, api.mutations())
	executor.Destroy = true
	_, err = executor.ApplyPlan(t.Context(), savedArchivePlan(t, executor))
	require.NoError(t, err)
	assert.Equal(t, []busPolicyRequest{{"DeleteArchive", map[string]any{"ArchiveName": "new"}}},
		api.mutations())
}

var archiveInvalid = []struct {
	name, field string
	value       any
	diagnostic  string
}{
	{"null name", "name", nil, "name is required"},
	{"empty name", "name", "", "name must be 1-48"},
	{"long name", "name", strings.Repeat("x", 49), "name must be 1-48"},
	{"invalid name", "name", "é", "name must be 1-48"},
	{"null source", "source", nil, "event-source-arn is required"},
	{"description bytes", "description", strings.Repeat("é", 257), "512 bytes"},
	{"key bytes", "key", strings.Repeat("a", 2049), "2048 bytes"},
	{"key characters", "key", "key!", "kms-key-identifier contains invalid"},
	{"pattern JSON", "pattern", "broken", "event-pattern must be valid JSON"},
}

func TestArchiveNativeInvalidSavedNoOp(t *testing.T) {
	endpoint, api := archiveEndpoint(t)
	executor := archiveExecutor(t, "evaluated")
	executor.Inputs = archiveInputs(endpoint)
	_, err := executor.ApplyPlan(t.Context(), savedArchivePlan(t, executor))
	require.NoError(t, err)
	api.mutations()
	for _, tt := range archiveInvalid {
		t.Run(tt.name, func(t *testing.T) {
			executor.Inputs = archiveInputs(endpoint)
			saved := savedArchivePlan(t, executor)
			require.Equal(t, runtime.DecisionNoOp, archiveStep(t, saved).Decision)
			executor.Inputs[tt.field] = tt.value
			_, err := executor.Plan(t.Context())
			require.ErrorContains(t, err, tt.diagnostic)
			require.ErrorContains(t, err, "resource.archive")
			require.ErrorContains(t, err, "constraints[")
			_, err = executor.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, tt.diagnostic)
			require.ErrorContains(t, err, "constraints[")
			assert.Empty(t, api.mutations())
		})
	}
	executor.Inputs = archiveInputs(endpoint)
	executor.Inputs["description"] = strings.Repeat("a", 512)
	executor.Inputs["key"] = strings.Repeat("a", 2048)
	executor.Inputs["pattern"] = ""
	executor.Inputs["days"] = -1
	_, err = executor.ApplyPlan(t.Context(), savedArchivePlan(t, executor))
	require.NoError(t, err)
	api.mutations()
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	snapshot.Find("resource.archive").Inputs["name"] = "historical:invalid"
	revision, err := executor.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(revision))
	executor.Destroy = true
	_, err = executor.ApplyPlan(t.Context(), savedArchivePlan(t, executor))
	require.NoError(t, err)
	assert.Equal(t, []busPolicyRequest{{"DeleteArchive", map[string]any{"ArchiveName": "old"}}},
		api.mutations())
}

func TestArchiveNativeDeferred(t *testing.T) {
	for _, stage := range []string{"create", "unchanged", "update", "replace"} {
		for _, tt := range append(archiveInvalid, struct {
			name, field string
			value       any
			diagnostic  string
		}{name: "valid"}) {
			t.Run(stage+" "+tt.name, func(t *testing.T) {
				endpoint, api := archiveEndpoint(t)
				executor := archiveExecutor(t, "deferred")
				executor.Inputs = archiveInputs(endpoint)
				if stage != "create" {
					_, err := executor.ApplyPlan(t.Context(), savedArchivePlan(t, executor))
					require.NoError(t, err)
					api.mutations()
				}
				executor.Inputs["trigger"] = "second"
				if stage == "update" {
					executor.Inputs["description"] = "updated"
				}
				if stage == "replace" {
					executor.Inputs["name"] = "new"
				}
				if tt.field != "" {
					executor.Inputs[tt.field] = tt.value
				}
				saved := savedArchivePlan(t, executor)
				assert.Equal(t, map[string][]string{
					"name":               {"action.seed.value.name"},
					"event-source-arn":   {"action.seed.value.source"},
					"description":        {"action.seed.value.description"},
					"event-pattern":      {"action.seed.value.pattern"},
					"kms-key-identifier": {"action.seed.value.key"},
					"retention-days":     {"action.seed.value.days"},
				}, archiveStep(t, saved).UnresolvedInputs)
				executor.Inputs = nil
				_, err := executor.ApplyPlan(t.Context(), saved)
				if tt.diagnostic != "" {
					require.ErrorContains(t, err, tt.diagnostic)
					require.ErrorContains(t, err, "resource.archive")
					require.ErrorContains(t, err, "constraints[")
					assert.Empty(t, api.mutations())
				} else {
					require.NoError(t, err)
					var want []busPolicyRequest
					switch stage {
					case "create":
						want = []busPolicyRequest{archiveCreateCall("old", "initial")}
					case "replace":
						want = []busPolicyRequest{
							{"DeleteArchive", map[string]any{"ArchiveName": "old"}},
							archiveCreateCall("new", "initial")}
					case "update":
						want = []busPolicyRequest{{"UpdateArchive", map[string]any{
							"ArchiveName": "old", "Description": "updated", "EventPattern": `{"x":1}`,
							"KmsKeyIdentifier": "alias/key", "RetentionDays": float64(30)}}}
					}
					assert.Equal(t, want, api.mutations())
				}
			})
		}
	}
}

func TestArchiveNativeAcceptedCreateFailureCleanup(t *testing.T) {
	endpoint, api := archiveEndpoint(t)
	executor := archiveExecutor(t, "evaluated")
	executor.Inputs = archiveInputs(endpoint)
	saved := savedArchivePlan(t, executor)
	api.failRead = true
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "read failed")
	mutations := api.mutations()
	assert.Equal(t, []busPolicyRequest{archiveCreateCall("old", "initial"),
		{"DeleteArchive", map[string]any{"ArchiveName": "old"}}}, mutations)
	assert.Empty(t, api.archives)
	snapshot, err := executor.Store.Current()
	if err == nil {
		assert.Nil(t, snapshot.Find("resource.archive"))
	} else {
		require.ErrorContains(t, err, "no current snapshot")
	}
}

func TestArchiveDomainPreflight(t *testing.T) {
	for _, fixture := range []string{"evaluated", "deferred"} {
		for _, tt := range []struct{ field, value, diagnostic string }{
			{"source", "invalid ARN", "event-source-arn must be a valid ARN"},
			{"pattern", `"` + strings.Repeat("x", 4095) + `"`, "4096 normalized bytes"},
		} {
			t.Run(fixture+" "+tt.field, func(t *testing.T) {
				endpoint, api := archiveEndpoint(t)
				executor := archiveExecutor(t, fixture)
				executor.Inputs = archiveInputs(endpoint)
				_, err := executor.ApplyPlan(t.Context(), savedArchivePlan(t, executor))
				require.NoError(t, err)
				api.mutations()
				executor.Inputs["name"] = "replacement"
				executor.Inputs["trigger"] = "second"
				executor.Inputs[tt.field] = tt.value
				saved := savedArchivePlan(t, executor)
				_, err = executor.ApplyPlan(t.Context(), saved)
				require.ErrorContains(t, err, tt.diagnostic)
				assert.Empty(t, api.mutations())
				assert.NotNil(t, api.archives["old"])
			})
		}
	}
}

func TestArchiveNativeRemoval(t *testing.T) {
	endpoint, api := archiveEndpoint(t)
	executor := archiveExecutor(t, "evaluated")
	executor.Inputs = archiveInputs(endpoint)
	_, err := executor.ApplyPlan(t.Context(), savedArchivePlan(t, executor))
	require.NoError(t, err)
	api.mutations()
	for _, field := range []string{"description", "pattern", "key", "days"} {
		executor.Inputs[field] = nil
	}
	saved := savedArchivePlan(t, executor)
	require.Equal(t, runtime.DecisionUpdate, archiveStep(t, saved).Decision)
	_, err = executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Equal(t, []busPolicyRequest{{"UpdateArchive", map[string]any{"ArchiveName": "old"}}},
		api.mutations())
	api.mu.Lock()
	assert.Equal(t, map[string]any{
		"ArchiveName": "old", "ArchiveArn": "arn:aws:events:us-east-1:123456789012:archive/old",
		"EventSourceArn": archiveARN, "Description": "initial", "EventPattern": `{"x":1}`,
		"KmsKeyIdentifier": "alias/key", "RetentionDays": float64(30),
	}, api.archives["old"])
	api.mu.Unlock()
	require.Equal(t, runtime.DecisionNoOp,
		archiveStep(t, savedArchivePlan(t, executor)).Decision)
	assert.Empty(t, api.mutations())
}

func TestArchiveNativeRegionalReplacement(t *testing.T) {
	for _, savedRegion := range []bool{true, false} {
		t.Run(map[bool]string{true: "saved configuration", false: "saved ARN"}[savedRegion],
			func(t *testing.T) { testArchiveRegionalReplacement(t, savedRegion) })
	}
}

func testArchiveRegionalReplacement(t *testing.T, savedRegion bool) {
	oldEndpoint, oldAPI := archiveEndpoint(t)
	newEndpoint, newAPI := archiveEndpoint(t)
	executor := archiveExecutor(t, "evaluated")
	executor.Inputs = archiveInputs(oldEndpoint)
	_, err := executor.ApplyPlan(t.Context(), savedArchivePlan(t, executor))
	require.NoError(t, err)
	oldAPI.mutations()
	if !savedRegion {
		snapshot, err := executor.Store.Current()
		require.NoError(t, err)
		snapshot.Find("resource.archive").Configuration["region"] = nil
		revision, err := executor.Store.Write(snapshot)
		require.NoError(t, err)
		require.NoError(t, executor.Store.SetCurrent(revision))
	}
	executor.Inputs["endpoint"], executor.Inputs["region"] = newEndpoint, "us-west-2"
	saved := savedArchivePlan(t, executor)
	require.Equal(t, runtime.DecisionReplace, archiveStep(t, saved).Decision)
	assert.Empty(t, oldAPI.mutations())
	assert.Empty(t, newAPI.mutations())
	_, err = executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Equal(t, []busPolicyRequest{{"DeleteArchive", map[string]any{"ArchiveName": "old"}}},
		oldAPI.mutations())
	assert.Equal(t, []busPolicyRequest{archiveCreateCall("old", "initial")}, newAPI.mutations())
	assert.Equal(t, []string{"us-east-1", "us-east-1", "us-east-1", "us-east-1", "us-east-1"},
		oldAPI.regions)
	assert.Equal(t, []string{"us-west-2", "us-west-2"}, newAPI.regions)
}
