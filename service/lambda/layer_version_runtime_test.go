package lambda

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/sourcecheck"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	layerAddress    = "resource.layer"
	layerNameRule   = "layer-name must not be empty"
	layerSourceRule = "content needs a nonempty zip-file-path or both s3-bucket and s3-key"
)

type layerCall struct {
	method, region, path string
	body                 map[string]any
}

type layerAPI struct {
	t        *testing.T
	endpoint string
	mu       sync.Mutex
	calls    []layerCall
	latest   int64
	versions map[string]bool
	desc     string
	seedCode int
}

func newLayerAPI(t *testing.T) *layerAPI {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing-credentials"))
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	api := &layerAPI{
		t: t, versions: map[string]bool{}, desc: "observed description", seedCode: http.StatusOK,
	}
	server := httptest.NewServer(http.HandlerFunc(api.serve))
	t.Cleanup(server.Close)
	api.endpoint = server.URL
	return api
}

func (a *layerAPI) takeCalls() []layerCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.calls
	a.calls = nil
	return out
}

func (a *layerAPI) setDescription(value string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.desc = value
}

func (a *layerAPI) forget(region, name string, version int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.versions, region+getLayerCall(region, name, version).path)
}

func (a *layerAPI) serve(w http.ResponseWriter, req *http.Request) {
	scope := strings.SplitN(req.Header.Get("Authorization"), "/", 4)
	if !assert.Len(a.t, scope, 4, "SigV4 credential scope") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	call := layerCall{method: req.Method, path: req.URL.Path, region: scope[2]}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		a.t.Errorf("read request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(data) != 0 {
		if err := json.Unmarshal(data, &call.body); err != nil {
			a.t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, call)
	w.Header().Set("Content-Type", "application/json")
	if call.path == "/2015-03-31/functions/seed/invocations" {
		w.WriteHeader(a.seedCode)
		return
	}
	name, number, _ := strings.Cut(
		strings.TrimPrefix(call.path, "/2018-10-31/layers/"), "/versions",
	)
	baseArn := "arn:aws:lambda:" + call.region + ":123456789012:layer:" + name
	switch call.method {
	case http.MethodPost:
		a.latest++
		get := getLayerCall(call.region, name, a.latest)
		a.versions[call.region+get.path] = true
		payload := map[string]any{
			"LayerVersionArn": baseArn + ":" + strconv.FormatInt(a.latest, 10),
			"Version":         999,
		}
		a.writeResponse(w, payload)
	case http.MethodGet:
		if !a.versions[call.region+call.path] {
			w.Header().Set("X-Amzn-Errortype", "ResourceNotFoundException")
			w.WriteHeader(http.StatusNotFound)
			a.writeResponse(w, map[string]string{"message": "gone"})
			return
		}
		a.writeResponse(w, map[string]any{
			"LayerVersionArn":         baseArn + ":" + strings.TrimPrefix(number, "/"),
			"LayerArn":                baseArn,
			"Version":                 999,
			"CreatedDate":             "2026-10-01T01:02:03.456+0000",
			"Description":             a.desc,
			"LicenseInfo":             "observed license",
			"CompatibleArchitectures": []string{"x86_64", "arm64", "x86_64"},
			"CompatibleRuntimes":      []string{"python3.14", "go1.x", "python3.14"},
			"Content": map[string]any{
				"CodeSha256":               "observed-checksum",
				"CodeSize":                 123,
				"SigningJobArn":            "signing-job",
				"SigningProfileVersionArn": "signing-profile-version",
				"Location":                 "https://example.test/download-canary",
			},
		})
	case http.MethodDelete:
		delete(a.versions, call.region+call.path)
		w.WriteHeader(http.StatusNoContent)
	default:
		a.t.Errorf("unexpected request: %s %s", call.method, call.path)
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (a *layerAPI) writeResponse(w http.ResponseWriter, value any) {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		a.t.Errorf("write response: %v", err)
	}
}

func layerPath(name string) string {
	return "/2018-10-31/layers/" + name + "/versions"
}

func publishLayerCall(region, name string, content map[string]any) layerCall {
	return layerCall{
		method: http.MethodPost, region: region, path: layerPath(name),
		body: map[string]any{"Content": content, "Description": "", "LicenseInfo": ""},
	}
}

func getLayerCall(region, name string, version int64) layerCall {
	return layerCall{
		method: http.MethodGet, region: region, path: fmt.Sprintf("%s/%d", layerPath(name), version),
	}
}

func deleteLayerCall(region, name string, version int64) layerCall {
	call := getLayerCall(region, name, version)
	call.method = http.MethodDelete
	return call
}

func layerExecutor(
	t *testing.T, fixture string, api *layerAPI, store state.Backend,
) *runtime.Executor {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata/ub/layer-version", fixture))
	require.NoError(t, err)
	src, err := os.ReadFile(filepath.Join(dir, "factory.ub"))
	require.NoError(t, err)
	file, err := syntax.ParseSource("factory.ub", src)
	require.NoError(t, err)
	body := &file.Factory.Body
	module := libraryModuleRoot(t)
	checked, err := sourcecheck.AnalyzeFactoryBody(*body, sourcecheck.Options{
		ProjectDir:  dir,
		Versions:    map[string]string{module.Path: "v0.0.0"},
		Resolver:    eventInvokeFixtureResolver{module.Dir, module.Path},
		SchemaCache: sourcecheck.NewSchemaCache(module, unobinModuleRoot(t)),
	})
	require.NoError(t, err)
	if store == nil {
		store, err = local.NewStore(t.TempDir(), "test", "test", encrypters.Noop{})
		require.NoError(t, err)
	}
	lib := Library()
	lib.Schema = checked.Imports.Libraries["lambda"].Schema
	lib.Constraints = map[string][]lang.ConstraintSpec{}
	lib.Defaults = map[string][]lang.DefaultSpec{}
	for name, schema := range lib.Schema.Resources {
		lib.Constraints["resource."+name] = schema.Constraints
		lib.Defaults["resource."+name] = schema.Defaults
	}
	for name, schema := range lib.Schema.Actions {
		lib.Constraints["action."+name] = schema.Constraints
		lib.Defaults["action."+name] = schema.Defaults
	}
	inputs := map[string]any{"endpoint": api.endpoint}
	if fixture == "deferred" {
		inputs["status"] = int64(200)
		inputs["source-status"] = int64(200)
	} else {
		inputs["region"] = "us-east-1"
		inputs["name"] = "layer"
		inputs["bucket"] = "bucket"
		inputs["key"] = "layer.zip"
		inputs["retain"] = false
		for _, name := range []string{
			"zip", "object-version", "hash", "architectures", "runtimes", "description", "license",
		} {
			inputs[name] = nil
		}
	}
	return &runtime.Executor{
		DAG: checked.DAG, SyntaxSource: body, Store: store,
		Libraries: map[string]*runtime.Library{"lambda": lib}, Inputs: inputs,
	}
}

func layerStep(t *testing.T, plan *runtime.PlanFile) *runtime.PlanStep {
	t.Helper()
	for i := range plan.Steps {
		if plan.Steps[i].Address == layerAddress {
			return &plan.Steps[i]
		}
	}
	t.Fatalf("no plan step for %s", layerAddress)
	return nil
}

func planLayer(t *testing.T, exec *runtime.Executor, decision runtime.Decision) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	raw, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(raw)
	require.NoError(t, err)
	require.Equal(t, decision, layerStep(t, saved).Decision)
	return saved
}

func applyLayer(t *testing.T, exec *runtime.Executor, decision runtime.Decision) {
	t.Helper()
	_, err := exec.ApplyPlan(t.Context(), planLayer(t, exec, decision))
	require.NoError(t, err)
}

func TestLayerVersionEvaluatedConstraints(t *testing.T) {
	api := newLayerAPI(t)
	exec := layerExecutor(t, "runtime", api, nil)
	limit := []any{
		"nodejs", "nodejs4.3", "nodejs6.10", "nodejs8.10", "nodejs10.x", "nodejs12.x", "nodejs14.x",
		"nodejs16.x", "nodejs18.x", "nodejs20.x", "nodejs22.x", "nodejs24.x", "java8", "java8.al2",
		"java11",
	}
	for _, tc := range []struct {
		name, field string
		input       any
		diagnostic  string
	}{
		{"empty name", "name", "", layerNameRule},
		{"null name", "name", nil, layerNameRule},
		{"missing bucket", "bucket", nil, layerSourceRule},
		{"empty key", "key", "", layerSourceRule},
		{"mixed sources", "zip", "layer.zip", "(forbidden-with): \"content.zip-file-path\""},
		{"case", "architectures", []any{"ARM64"}, "must be valid Lambda architectures"},
		{"unknown runtime", "runtimes", []any{"fake"}, "must be valid Lambda runtimes"},
		{"null architecture", "architectures", []any{nil}, "must be valid Lambda architectures"},
		{"null runtime", "runtimes", []any{nil}, "must be valid Lambda runtimes"},
		{"too many", "runtimes", append(slices.Clone(limit), "java17"), "at most 15 distinct"},
		{"license bytes", "license", strings.Repeat("é", 257), "at most 512 bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := exec.Inputs[tc.field]
			exec.Inputs[tc.field] = tc.input
			t.Cleanup(func() { exec.Inputs[tc.field] = original })
			_, err := exec.Plan(t.Context())
			require.ErrorContains(t, err, layerAddress+": schema: constraints[")
			require.ErrorContains(t, err, tc.diagnostic)
			assert.Empty(t, api.takeCalls())
		})
	}
	exec.Inputs["architectures"] = []any{"x86_64", "arm64", "arm64"}
	exec.Inputs["runtimes"] = append(slices.Clone(limit), "nodejs")
	exec.Inputs["license"] = strings.Repeat("é", 256)
	applyLayer(t, exec, runtime.DecisionCreate)
	post := publishLayerCall("us-east-1", "layer", map[string]any{
		"S3Bucket": "bucket", "S3Key": "layer.zip",
	})
	post.body["CompatibleArchitectures"] = []any{"arm64", "x86_64"}
	normalized := slices.Clone(limit)
	slices.SortFunc(normalized, func(a, b any) int { return strings.Compare(a.(string), b.(string)) })
	post.body["CompatibleRuntimes"] = normalized
	post.body["LicenseInfo"] = strings.Repeat("é", 256)
	assert.Equal(t, []layerCall{post, getLayerCall("us-east-1", "layer", 1)}, api.takeCalls())
}

func TestLayerVersionNativeNoOpAndHistoricalDestroy(t *testing.T) {
	for _, tc := range []struct {
		name, rule string
		inputs     func(entry *state.Entry, desired map[string]any)
	}{
		{
			name: "layer name", rule: layerNameRule,
			inputs: func(entry *state.Entry, desired map[string]any) {
				entry.Inputs["layer-name"] = nil
				desired["name"] = nil
			},
		},
		{
			name: "membership", rule: "must be valid Lambda runtimes",
			inputs: func(entry *state.Entry, desired map[string]any) {
				entry.Inputs["compatible-runtimes"] = []any{nil}
				desired["runtimes"] = []any{nil}
			},
		},
		{
			name: "source presence", rule: layerSourceRule,
			inputs: func(entry *state.Entry, desired map[string]any) {
				entry.Inputs["content"].(map[string]any)["s3-bucket"] = nil
				desired["bucket"] = nil
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newLayerAPI(t)
			exec := layerExecutor(t, "runtime", api, nil)
			applyLayer(t, exec, runtime.DecisionCreate)
			api.takeCalls()
			get := getLayerCall("us-east-1", "layer", 1)

			snapshot, err := exec.Store.Current()
			require.NoError(t, err)
			tc.inputs(snapshot.Find(layerAddress), exec.Inputs)
			rev, err := exec.Store.Write(snapshot)
			require.NoError(t, err)
			require.NoError(t, exec.Store.SetCurrent(rev))

			lib := exec.Libraries["lambda"]
			constraints := lib.Constraints
			lib.Constraints = nil
			historical := planLayer(t, exec, runtime.DecisionNoOp)
			lib.Constraints = constraints
			_, err = exec.Plan(t.Context())
			require.ErrorContains(t, err, layerAddress+": schema: constraints[")
			require.ErrorContains(t, err, tc.rule)
			assert.Equal(t, []layerCall{get, get}, api.takeCalls())

			_, err = exec.ApplyPlan(t.Context(), historical)
			require.ErrorContains(t, err, layerAddress)
			require.ErrorContains(t, err, "schema: constraints[")
			require.ErrorContains(t, err, tc.rule)
			assert.Empty(t, api.takeCalls())

			exec.Destroy = true
			applyLayer(t, exec, runtime.DecisionDestroy)
			assert.Equal(t, []layerCall{
				get, get, deleteLayerCall("us-east-1", "layer", 1),
			}, api.takeCalls())
		})
	}
}

func TestLayerVersionDeferredConstraints(t *testing.T) {
	for _, tc := range []struct {
		name, rule           string
		status, sourceStatus int64
		seedCode             int
	}{
		{"valid", "", 200, 200, http.StatusOK},
		{"invalid runtime", "must be valid Lambda runtimes", 201, 200, http.StatusOK},
		{"missing source", layerSourceRule, 200, 201, http.StatusOK},
		{"missing layer name", layerNameRule, 201, 201, http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newLayerAPI(t)
			api.mu.Lock()
			api.seedCode = tc.seedCode
			api.mu.Unlock()
			initial := layerExecutor(t, "runtime", api, nil)
			applyLayer(t, initial, runtime.DecisionCreate)
			api.takeCalls()
			prior, err := initial.Store.Current()
			require.NoError(t, err)

			exec := layerExecutor(t, "deferred", api, initial.Store)
			exec.Inputs["status"] = tc.status
			exec.Inputs["source-status"] = tc.sourceStatus
			plan := planLayer(t, exec, runtime.DecisionReplace)
			step := layerStep(t, plan)
			assert.Equal(t, []string{
				"compatible-runtimes", "content.s3-bucket", "content.s3-key",
				"content.s3-object-version", "content.zip-file-path", "layer-name",
			}, step.PendingReplacementReasons)
			assert.Equal(t, map[string][]string{
				"compatible-runtimes": {"action.seed.status-code"},
				"content":             {"action.seed.status-code"},
				"layer-name":          {"action.seed.status-code"},
			}, step.UnresolvedInputs)
			getOld := getLayerCall("us-east-1", "layer", 1)
			assert.Equal(t, []layerCall{getOld}, api.takeCalls())

			_, err = exec.ApplyPlan(t.Context(), plan)
			invoke := layerCall{
				method: http.MethodPost, region: "us-east-1",
				path: "/2015-03-31/functions/seed/invocations", body: map[string]any{},
			}
			if tc.rule != "" {
				assert.Equal(t, []layerCall{invoke}, api.takeCalls())
				require.ErrorContains(t, err, layerAddress)
				require.ErrorContains(t, err, "schema: constraints[")
				require.ErrorContains(t, err, tc.rule)
				current, err := initial.Store.Current()
				require.NoError(t, err)
				assert.Equal(t, prior.Find(layerAddress), current.Find(layerAddress))
				planLayer(t, initial, runtime.DecisionNoOp)
				assert.Equal(t, []layerCall{getOld}, api.takeCalls())
				return
			}
			require.NoError(t, err)
			post := publishLayerCall("us-east-1", "replacement", map[string]any{
				"S3Bucket": "bucket", "S3Key": "layer.zip",
			})
			post.body["CompatibleRuntimes"] = []any{"python3.14"}
			assert.Equal(t, []layerCall{
				invoke, getOld, deleteLayerCall("us-east-1", "layer", 1), post,
				getLayerCall("us-east-1", "replacement", 2),
			}, api.takeCalls())
		})
	}
}

func TestLayerVersionZipHashAndOutputDrift(t *testing.T) {
	api := newLayerAPI(t)
	exec := layerExecutor(t, "runtime", api, nil)
	path := filepath.Join(t.TempDir(), "layer.zip")
	require.NoError(t, os.WriteFile(path, []byte("zip-canary"), 0o600))
	exec.Inputs["zip"], exec.Inputs["bucket"], exec.Inputs["key"] = path, nil, nil
	exec.Inputs["hash"] = "configured-token"

	applyLayer(t, exec, runtime.DecisionCreate)
	get := getLayerCall("us-east-1", "layer", 1)
	assert.Equal(t, []layerCall{
		publishLayerCall("us-east-1", "layer", map[string]any{
			"ZipFile": base64.StdEncoding.EncodeToString([]byte("zip-canary")),
		}),
		get,
	}, api.takeCalls())

	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	entry := snapshot.Find(layerAddress)
	require.NotNil(t, entry)
	wantARN := "arn:aws:lambda:us-east-1:123456789012:layer:layer:1"
	assert.Equal(t, "configured-token", entry.Inputs["source-code-hash"])
	assert.Equal(t, map[string]any{
		"layer-version-arn":           wantARN,
		"layer-arn":                   "arn:aws:lambda:us-east-1:123456789012:layer:layer",
		"layer-name":                  "layer",
		"version":                     int64(1),
		"region":                      "us-east-1",
		"created-date":                "2026-10-01T01:02:03.456+0000",
		"description":                 "observed description",
		"license-info":                "observed license",
		"compatible-architectures":    []any{"arm64", "x86_64"},
		"compatible-runtimes":         []any{"go1.x", "python3.14"},
		"code-sha256":                 "observed-checksum",
		"code-size":                   int64(123),
		"signing-job-arn":             "signing-job",
		"signing-profile-version-arn": "signing-profile-version",
	}, entry.Outputs)
	stateJSON, err := json.Marshal(snapshot)
	require.NoError(t, err)
	assert.NotContains(t, string(stateJSON), "download-canary")
	assert.NotContains(t, string(stateJSON), "zip-canary")

	require.NoError(t, os.WriteFile(path, []byte("new bytes"), 0o600))
	exec.Inputs["description"], exec.Inputs["license"] = "", ""
	exec.Inputs["architectures"], exec.Inputs["runtimes"] = []any{}, []any{}
	applyLayer(t, exec, runtime.DecisionNoOp)
	assert.Equal(t, []layerCall{get}, api.takeCalls())

	exec.Inputs["hash"] = "Configured-token"
	plan := planLayer(t, exec, runtime.DecisionReplace)
	step := layerStep(t, plan)
	assert.Equal(t, []string{"source-code-hash"}, step.ReplacementReasons)
	require.NotNil(t, step.ExpectedStableID)
	assert.Equal(t, wantARN, *step.ExpectedStableID)
	assert.Equal(t, []layerCall{get}, api.takeCalls())

	exec.Inputs["hash"] = nil
	applyLayer(t, exec, runtime.DecisionNoOp)
	assert.Equal(t, []layerCall{get}, api.takeCalls())
	snapshot, err = exec.Store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find(layerAddress).Inputs["source-code-hash"])

	api.setDescription("drift")
	applyLayer(t, exec, runtime.DecisionUpdate)
	assert.Equal(t, []layerCall{get}, api.takeCalls())
	snapshot, err = exec.Store.Current()
	require.NoError(t, err)
	entry = snapshot.Find(layerAddress)
	require.NotNil(t, entry)
	assert.Equal(t, "drift", entry.Outputs["description"])
	priorInputs, wantOutputs := entry.Inputs, maps.Clone(entry.Outputs)

	exec.Inputs["zip"] = filepath.Join(t.TempDir(), "missing.zip")
	plan = planLayer(t, exec, runtime.DecisionReplace)
	assert.Equal(t, []layerCall{get}, api.takeCalls())
	_, err = exec.ApplyPlan(t.Context(), plan)
	require.ErrorContains(t, err, "read ZIP file")
	assert.NotContains(t, err.Error(), "download-canary")
	assert.NotContains(t, err.Error(), "zip-canary")
	assert.Empty(t, api.takeCalls())

	require.NoError(t, os.Remove(path))
	exec.Inputs["zip"] = path
	api.setDescription("drift without original ZIP")
	plan = planLayer(t, exec, runtime.DecisionUpdate)
	step = layerStep(t, plan)
	assert.Equal(t, "drift", step.PriorOutputs["description"])
	assert.Equal(t, "drift without original ZIP", step.ObservedOutputs["description"])
	assert.Equal(t, []layerCall{get}, api.takeCalls())

	_, err = exec.ApplyPlan(t.Context(), plan)
	require.ErrorContains(t, err, "read ZIP file")
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Empty(t, api.takeCalls())
	snapshot, err = exec.Store.Current()
	require.NoError(t, err)
	entry = snapshot.Find(layerAddress)
	require.NotNil(t, entry)
	assert.Equal(t, wantOutputs, entry.Outputs)

	refreshed, err := exec.Refresh(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, refreshed.WrittenRev)
	assert.Equal(t, []layerCall{get}, api.takeCalls())
	snapshot, err = exec.Store.Current()
	require.NoError(t, err)
	entry = snapshot.Find(layerAddress)
	require.NotNil(t, entry)
	wantOutputs["description"] = "drift without original ZIP"
	assert.Equal(t, wantOutputs, entry.Outputs)
	assert.Equal(t, priorInputs, entry.Inputs)

	applyLayer(t, exec, runtime.DecisionNoOp)
	assert.Equal(t, []layerCall{get}, api.takeCalls())
	snapshot, err = exec.Store.Current()
	require.NoError(t, err)
	entry = snapshot.Find(layerAddress)
	require.NotNil(t, entry)
	assert.Equal(t, wantOutputs, entry.Outputs)
}

func TestLayerVersionEmptyHashPreservesConcreteEquality(t *testing.T) {
	api := newLayerAPI(t)
	exec := layerExecutor(t, "runtime", api, nil)
	exec.Inputs["hash"] = "configured"
	applyLayer(t, exec, runtime.DecisionCreate)
	api.takeCalls()

	exec.Inputs["hash"] = ""
	applyLayer(t, exec, runtime.DecisionUpdate)
	assert.Equal(t, []layerCall{getLayerCall("us-east-1", "layer", 1)}, api.takeCalls())
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	assert.Equal(t, "", snapshot.Find(layerAddress).Inputs["source-code-hash"])
}

func TestLayerVersionS3SetEqualityAndRegionalRetention(t *testing.T) {
	api := newLayerAPI(t)
	exec := layerExecutor(t, "runtime", api, nil)
	const east, west = "us-east-1", "us-west-2"
	unpinned := map[string]any{"S3Bucket": "bucket", "S3Key": "layer.zip"}
	pinned := map[string]any{"S3Bucket": "bucket", "S3Key": "layer.zip", "S3ObjectVersion": "v1"}
	first := publishLayerCall(east, "layer", unpinned)
	first.body["CompatibleArchitectures"] = []any{"arm64", "x86_64"}
	first.body["CompatibleRuntimes"] = []any{"go1.x", "python3.14"}
	exec.Inputs["architectures"] = []any{"x86_64", "arm64"}
	exec.Inputs["runtimes"] = []any{"python3.14", "go1.x"}
	applyLayer(t, exec, runtime.DecisionCreate)
	getFirst := getLayerCall(east, "layer", 1)
	assert.Equal(t, []layerCall{first, getFirst}, api.takeCalls())

	exec.Inputs["architectures"] = []any{"arm64", "x86_64", "arm64"}
	exec.Inputs["runtimes"] = []any{"go1.x", "go1.x", "python3.14"}
	exec.Inputs["object-version"] = ""
	applyLayer(t, exec, runtime.DecisionNoOp)
	assert.Equal(t, []layerCall{getFirst}, api.takeCalls())

	exec.Inputs["object-version"] = "v1"
	plan := planLayer(t, exec, runtime.DecisionReplace)
	assert.Equal(t, []string{"content.s3-object-version"}, layerStep(t, plan).ReplacementReasons)
	assert.Equal(t, []layerCall{getFirst}, api.takeCalls())

	exec.Inputs["retain"], exec.Inputs["region"], exec.Inputs["name"] = true, west, "retained"
	plan = planLayer(t, exec, runtime.DecisionReplace)
	assert.ElementsMatch(t, []string{
		"content.s3-object-version", "skip-destroy", "region", "layer-name",
	}, layerStep(t, plan).ReplacementReasons)
	assert.Equal(t, []layerCall{getFirst}, api.takeCalls())
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	second := publishLayerCall(west, "retained", pinned)
	second.body["CompatibleArchitectures"] = first.body["CompatibleArchitectures"]
	second.body["CompatibleRuntimes"] = first.body["CompatibleRuntimes"]
	getSecond := getLayerCall(west, "retained", 2)
	assert.Equal(t, []layerCall{
		getFirst, deleteLayerCall(east, "layer", 1), second, getSecond,
	}, api.takeCalls())

	exec.Inputs["retain"], exec.Inputs["region"], exec.Inputs["name"] = false, east, "managed"
	applyLayer(t, exec, runtime.DecisionReplace)
	third := publishLayerCall(east, "managed", pinned)
	third.body["CompatibleArchitectures"] = first.body["CompatibleArchitectures"]
	third.body["CompatibleRuntimes"] = first.body["CompatibleRuntimes"]
	getThird := getLayerCall(east, "managed", 3)
	assert.Equal(t, []layerCall{getSecond, getSecond, third, getThird}, api.takeCalls())

	exec.Destroy = true
	applyLayer(t, exec, runtime.DecisionDestroy)
	assert.Equal(t, []layerCall{
		getThird, getThird, deleteLayerCall(east, "managed", 3),
	}, api.takeCalls())
	exec.Destroy = false
	exec.Inputs["retain"], exec.Inputs["name"] = true, "retained-on-destroy"
	applyLayer(t, exec, runtime.DecisionCreate)
	fourth := publishLayerCall(east, "retained-on-destroy", pinned)
	fourth.body["CompatibleArchitectures"] = first.body["CompatibleArchitectures"]
	fourth.body["CompatibleRuntimes"] = first.body["CompatibleRuntimes"]
	getFourth := getLayerCall(east, "retained-on-destroy", 4)
	assert.Equal(t, []layerCall{fourth, getFourth}, api.takeCalls())

	exec.Destroy = true
	applyLayer(t, exec, runtime.DecisionDestroy)
	assert.Equal(t, []layerCall{getFourth, getFourth}, api.takeCalls())
	api.mu.Lock()
	defer api.mu.Unlock()
	assert.Equal(t, map[string]bool{
		west + getSecond.path: true,
		east + getFourth.path: true,
	}, api.versions)
}

func TestLayerVersionTrackedNotFoundPlansCreate(t *testing.T) {
	api := newLayerAPI(t)
	exec := layerExecutor(t, "runtime", api, nil)
	applyLayer(t, exec, runtime.DecisionCreate)
	api.takeCalls()
	api.forget("us-east-1", "layer", 1)
	applyLayer(t, exec, runtime.DecisionCreate)
	assert.Equal(t, []layerCall{
		getLayerCall("us-east-1", "layer", 1),
		publishLayerCall("us-east-1", "layer", map[string]any{
			"S3Bucket": "bucket", "S3Key": "layer.zip",
		}),
		getLayerCall("us-east-1", "layer", 2),
	}, api.takeCalls())
}
