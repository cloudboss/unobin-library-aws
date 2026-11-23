package cloudfront

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
)

const originRequestKind = "resource.origin-request-policy"
const originRequestAddress = "resource.policy"
const originRequestFixtureRoot = "testdata/ub/origin-request-policy"
const originRequestAPIPath = "/2020-05-31/origin-request-policy"
const originRequestResourcePath = originRequestAPIPath + "/test-id"

type originRequestSeed struct {
	NextMode  string
	NextItems []string
}

type originRequestSeedOutput struct {
	Mode  string
	Items []string
}

func (s *originRequestSeed) Run(
	_ context.Context, _ runtime.NoConfig,
) (*originRequestSeedOutput, error) {
	return &originRequestSeedOutput{Mode: s.NextMode, Items: s.NextItems}, nil
}

func originRequestLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	schema := readLibrarySchema(t)
	definition := schema.Resources["origin-request-policy"]
	require.NotNil(t, definition)
	library := func(path string) *runtime.Library {
		lib := runtime.LibraryWithPath(Library(), path)
		lib.Schema = schema
		lib.Constraints = map[string][]lang.ConstraintSpec{
			originRequestKind: definition.Constraints,
		}
		return lib
	}
	path := "github.com/cloudboss/unobin-library-aws//service/cloudfront"
	return map[string]*runtime.Library{
		"aws-cloudfront":    library(path),
		"aws-cloudfront-v2": library(path + "-next"),
		"fixture": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[
				originRequestSeed, *originRequestSeedOutput, runtime.NoConfig, *originRequestSeed](),
		}},
	}
}

func originRequestExecutor(
	t *testing.T, fixture, endpoint string, libraries map[string]*runtime.Library,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join(originRequestFixtureRoot, fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, err := syntax.ParseSource(path, source)
	require.NoError(t, err)
	body := &file.Factory.Body
	store, err := local.NewStore(t.TempDir(), "origin-request-policy", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libraries), SyntaxSource: body, Libraries: libraries,
		Store: store, Factory: state.FactoryInfo{Name: "origin-request-policy", ContentRevision: "test"},
		Inputs: map[string]any{
			"aws-config": map[string]any{"endpoint-url": endpoint, "max-attempts": int64(1)},
			"name":       "base", "cookie-mode": "none", "header-mode": "none", "query-mode": "none",
			"cookie-items": []any{}, "header-items": []any{}, "query-items": []any{},
			"comment": nil,
		},
	}
}

type originRequestHTTPRequest struct {
	Method, Path, IfMatch string
}

var originRequestPost = originRequestHTTPRequest{
	Method: http.MethodPost, Path: originRequestAPIPath,
}
var originRequestGet = originRequestHTTPRequest{
	Method: http.MethodGet, Path: originRequestResourcePath,
}

type originRequestCloud struct {
	mu         sync.Mutex
	url        string
	config     string
	etag       string
	createName string
	getErr     string
	calls      []originRequestHTTPRequest
}

func newOriginRequestCloud(t *testing.T) *originRequestCloud {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_ACCOUNT_ID", "123456789012")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent/credentials")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	cloud := new(originRequestCloud)
	server := httptest.NewServer(http.HandlerFunc(cloud.handle))
	t.Cleanup(server.Close)
	cloud.url = server.URL
	t.Setenv("AWS_ENDPOINT_URL_CLOUDFRONT", server.URL)
	return cloud
}

func (c *originRequestCloud) handle(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	match := r.Header.Get("If-Match")
	c.calls = append(c.calls, originRequestHTTPRequest{r.Method, r.URL.Path, match})
	w.Header().Set("Content-Type", "application/xml")
	switch r.Method {
	case http.MethodGet:
		switch c.getErr {
		case "empty":
			return
		case "AccessDenied":
			writeOriginRequestTestError(w, http.StatusForbidden, c.getErr)
			return
		case "NoSuchDistribution", "NoSuchOriginRequestPolicy":
			writeOriginRequestTestError(w, http.StatusNotFound, c.getErr)
			return
		}
	case http.MethodPost, http.MethodPut:
		if r.Method == http.MethodPut && match != c.etag {
			writeOriginRequestTestError(w, http.StatusPreconditionFailed, "PreconditionFailed")
			return
		}
		body, err := io.ReadAll(r.Body)
		start := bytes.Index(body, []byte("<OriginRequestPolicyConfig"))
		if err != nil || start == -1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.config = string(body[start:])
		if r.Method == http.MethodPut {
			c.etag = "updated"
			return
		}
		c.etag = "initial"
		if c.createName != "" {
			c.config = strings.Replace(c.config, "<Name>base</Name>",
				"<Name>"+c.createName+"</Name>", 1)
		}
	case http.MethodDelete:
		if match != c.etag {
			writeOriginRequestTestError(w, http.StatusPreconditionFailed, "PreconditionFailed")
			return
		}
		c.config = ""
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("ETag", c.etag)
	_, _ = fmt.Fprintf(w, "<OriginRequestPolicy><Id>test-id</Id>%s</OriginRequestPolicy>",
		c.config)
}

func writeOriginRequestTestError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<ErrorResponse><Error><Code>%s</Code></Error></ErrorResponse>", code)
}

func (c *originRequestCloud) takeCalls() []originRequestHTTPRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls = nil
	return calls
}

func saveOriginRequestPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func originRequestStep(
	t *testing.T, plan *runtime.PlanFile, decision runtime.Decision,
) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == originRequestAddress {
			require.Equal(t, decision, step.Decision)
			return step
		}
	}
	t.Fatalf("missing %s in plan", originRequestAddress)
	return runtime.PlanStep{}
}

func applyOriginRequestPlan(
	t *testing.T, exec *runtime.Executor, decision runtime.Decision,
) runtime.PlanStep {
	t.Helper()
	plan := saveOriginRequestPlan(t, exec)
	step := originRequestStep(t, plan, decision)
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	return step
}

func requireOriginRequestConstraint(t *testing.T, err error, index int, field string) {
	t.Helper()
	require.ErrorContains(t, err, fmt.Sprintf(
		"%s: schema: constraints[%d] (predicate): %s must be", originRequestAddress, index, field))
}

func TestOriginRequestPolicyRuntimeNoOpAndDrift(t *testing.T) {
	cloud := newOriginRequestCloud(t)
	cloud.mu.Lock()
	cloud.createName = "observed"
	cloud.mu.Unlock()
	exec := originRequestExecutor(t, "evaluated", cloud.url, originRequestLibraries(t))
	exec.Inputs["cookie-mode"] = "whitelist"
	exec.Inputs["header-mode"] = "whitelist"
	exec.Inputs["query-mode"] = "allExcept"
	exec.Inputs["cookie-items"] = []any{"z", "a", "z", ""}
	exec.Inputs["header-items"] = []any{"Z", "A", "Z", ""}
	exec.Inputs["query-items"] = []any{"b", "a", "b", ""}
	applyOriginRequestPlan(t, exec, runtime.DecisionCreate)
	require.Equal(t, []originRequestHTTPRequest{originRequestPost}, cloud.takeCalls())

	exec.Inputs["comment"] = ""
	exec.Inputs["cookie-items"] = []any{"a", "z"}
	exec.Inputs["header-items"] = []any{"A", "Z"}
	exec.Inputs["query-items"] = []any{"a", "b"}
	step := applyOriginRequestPlan(t, exec, runtime.DecisionNoOp)
	assert.False(t, step.Drift())
	require.Equal(t, []originRequestHTTPRequest{originRequestGet}, cloud.takeCalls())
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	assert.Equal(t, "base", step.Inputs["name"])
	assert.Equal(t, map[string]any{
		"name":    "observed",
		"comment": "",
		"cookies-config": map[string]any{
			"cookie-behavior": "whitelist",
			"cookies":         map[string]any{"items": []any{"a", "z"}},
		},
		"headers-config": map[string]any{
			"header-behavior": "whitelist",
			"headers":         map[string]any{"items": []any{"A", "Z"}},
		},
		"query-strings-config": map[string]any{
			"query-string-behavior": "allExcept",
			"query-strings":         map[string]any{"items": []any{"a", "b"}},
		},
	}, snapshot.Outputs)

	cloud.mu.Lock()
	cloud.config = strings.Replace(cloud.config, "<Name>observed</Name>", "<Name>drift</Name>", 1)
	cloud.etag = "external-edit"
	cloud.mu.Unlock()
	step = applyOriginRequestPlan(t, exec, runtime.DecisionUpdate)
	assert.True(t, step.Drift())
	assert.Empty(t, step.ReplacementReasons)
	assert.Equal(t, "drift", step.ObservedOutputs["name"])
	require.Equal(t, []originRequestHTTPRequest{
		originRequestGet, {http.MethodPut, originRequestResourcePath, "external-edit"},
		originRequestGet,
	}, cloud.takeCalls())
	snapshot, err = exec.Store.Current()
	require.NoError(t, err)
	assert.Equal(t, "test-id", snapshot.Find(originRequestAddress).Outputs["id"])
	assert.Equal(t, "updated", snapshot.Find(originRequestAddress).Outputs["etag"])
	assert.Equal(t, "base", snapshot.Outputs["name"])
}

func TestOriginRequestPolicyRuntimeRefresh(t *testing.T) {
	cloud := newOriginRequestCloud(t)
	exec := originRequestExecutor(t, "evaluated", cloud.url, originRequestLibraries(t))
	applyOriginRequestPlan(t, exec, runtime.DecisionCreate)
	require.Equal(t, []originRequestHTTPRequest{originRequestPost}, cloud.takeCalls())
	for _, tt := range []struct {
		code    string
		etag    string
		wantErr string
	}{
		{code: "NoSuchOriginRequestPolicy"},
		{code: "empty"},
		{code: "AccessDenied", wantErr: "AccessDenied"},
		{code: "NoSuchDistribution", wantErr: "NoSuchDistribution"},
		{wantErr: "missing ETag"},
	} {
		t.Run(tt.wantErr+tt.code, func(t *testing.T) {
			cloud.mu.Lock()
			cloud.getErr = tt.code
			cloud.etag = tt.etag
			cloud.mu.Unlock()
			if tt.wantErr == "" {
				plan := saveOriginRequestPlan(t, exec)
				step := originRequestStep(t, plan, runtime.DecisionCreate)
				require.True(t, step.Gone())
			} else {
				_, err := exec.Plan(t.Context())
				require.ErrorContains(t, err, "get origin request policy test-id:")
				require.ErrorContains(t, err, tt.wantErr)
			}
			require.Equal(t, []originRequestHTTPRequest{originRequestGet}, cloud.takeCalls())
		})
	}
}

func TestOriginRequestPolicyRuntimeEvaluatedModes(t *testing.T) {
	cloud := newOriginRequestCloud(t)
	exec := originRequestExecutor(t, "evaluated", cloud.url, originRequestLibraries(t))
	for i, tt := range []struct {
		input string
		field string
	}{
		{"cookie-mode", "cookie-behavior"},
		{"header-mode", "header-behavior"},
		{"query-mode", "query-string-behavior"},
		{"name", "name"},
	} {
		valid := exec.Inputs[tt.input]
		for _, value := range []any{nil, "", "None", "unsupported"} {
			if tt.input == "name" && value != nil {
				continue
			}
			t.Run(fmt.Sprintf("%s/%v", tt.field, value), func(t *testing.T) {
				exec.Inputs[tt.input] = value
				_, err := exec.Plan(t.Context())
				requireOriginRequestConstraint(t, err, i, tt.field)
				require.Empty(t, cloud.takeCalls())
			})
		}
		exec.Inputs[tt.input] = valid
	}
}

func TestOriginRequestPolicyRuntimeSavedNoOpAndHistoricalDestroy(t *testing.T) {
	cloud := newOriginRequestCloud(t)
	libraries := originRequestLibraries(t)
	exec := originRequestExecutor(t, "evaluated", cloud.url, libraries)
	lib := libraries["aws-cloudfront"]
	constraints := lib.Constraints[originRequestKind]
	delete(lib.Constraints, originRequestKind)
	exec.Inputs["cookie-mode"] = "None"
	applyOriginRequestPlan(t, exec, runtime.DecisionCreate)
	require.Equal(t, []originRequestHTTPRequest{originRequestPost}, cloud.takeCalls())

	saved := saveOriginRequestPlan(t, exec)
	step := originRequestStep(t, saved, runtime.DecisionNoOp)
	require.False(t, step.Drift())
	require.Equal(t, step.PriorInputs, step.Inputs)
	require.Equal(t, step.PriorOutputs, step.ObservedOutputs)
	require.Equal(t, []originRequestHTTPRequest{originRequestGet}, cloud.takeCalls())
	lib.Constraints[originRequestKind] = constraints
	_, err := exec.ApplyPlan(t.Context(), saved)
	requireOriginRequestConstraint(t, err, 0, "cookie-behavior")
	require.Empty(t, cloud.takeCalls())

	_, err = exec.Plan(t.Context())
	requireOriginRequestConstraint(t, err, 0, "cookie-behavior")
	require.Equal(t, []originRequestHTTPRequest{originRequestGet}, cloud.takeCalls())

	cloud.mu.Lock()
	cloud.etag = "external-edit"
	cloud.mu.Unlock()
	exec.Destroy = true
	applyOriginRequestPlan(t, exec, runtime.DecisionDestroy)
	require.Equal(t, []originRequestHTTPRequest{
		originRequestGet, originRequestGet,
		{http.MethodDelete, originRequestResourcePath, "external-edit"},
	}, cloud.takeCalls())
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	require.Nil(t, snapshot.Find(originRequestAddress))
}

func TestOriginRequestPolicyRuntimeSavedDeferredInputs(t *testing.T) {
	libraries := originRequestLibraries(t)
	for _, tt := range []struct {
		fixture  string
		mode     string
		pending  string
		decision runtime.Decision
		valid    bool
	}{
		{"deferred", "none", "action.seed.mode", runtime.DecisionCreate, true},
		{"deferred", "None", "action.seed.mode", runtime.DecisionCreate, false},
		{"pending-cookie-items", "none", "action.seed.items", runtime.DecisionCreate, false},
		{"replacement", "None", "action.seed.mode", runtime.DecisionReplace, false},
	} {
		t.Run(tt.fixture+"/"+tt.mode, func(t *testing.T) {
			cloud := newOriginRequestCloud(t)
			exec := originRequestExecutor(t, tt.fixture, cloud.url, libraries)
			exec.Inputs["cookie-mode"] = tt.mode
			if tt.decision == runtime.DecisionReplace {
				initial := originRequestExecutor(t, "evaluated", cloud.url, libraries)
				initial.Store = exec.Store
				applyOriginRequestPlan(t, initial, runtime.DecisionCreate)
				require.Equal(t, []originRequestHTTPRequest{originRequestPost}, cloud.takeCalls())
			}
			saved := saveOriginRequestPlan(t, exec)
			step := originRequestStep(t, saved, tt.decision)
			assert.Equal(t, map[string][]string{"cookies-config": {tt.pending}},
				step.UnresolvedInputs)
			if tt.fixture == "pending-cookie-items" {
				config := step.Inputs["cookies-config"].(map[string]any)
				assert.Equal(t, "None", config["cookie-behavior"])
			}
			if tt.decision == runtime.DecisionReplace {
				assert.Equal(t, []string{"binding"}, step.ReplacementReasons)
				require.NotNil(t, step.ExpectedStableID)
				assert.Equal(t, "test-id", *step.ExpectedStableID)
				require.Equal(t, []originRequestHTTPRequest{originRequestGet}, cloud.takeCalls())
			} else {
				require.Empty(t, cloud.takeCalls())
			}
			_, err := exec.ApplyPlan(t.Context(), saved)
			if tt.valid {
				require.NoError(t, err)
				require.Equal(t, []originRequestHTTPRequest{originRequestPost}, cloud.takeCalls())
			} else {
				requireOriginRequestConstraint(t, err, 0, "cookie-behavior")
				require.Empty(t, cloud.takeCalls())
			}
		})
	}

	t.Run("unrelated pending header items", func(t *testing.T) {
		cloud := newOriginRequestCloud(t)
		exec := originRequestExecutor(t, "pending-header-items", cloud.url, libraries)
		_, err := exec.Plan(t.Context())
		requireOriginRequestConstraint(t, err, 0, "cookie-behavior")
		require.Empty(t, cloud.takeCalls())
	})
}
