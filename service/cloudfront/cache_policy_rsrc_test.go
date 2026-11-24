package cloudfront

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	cachePolicyAddress       = "resource.policy"
	cachePolicyKind          = "resource.cache-policy"
	cachePolicyParametersKey = "parameters-in-cache-key-and-forwarded-to-origin"
	cachePolicyLibraryPath   = "github.com/cloudboss/unobin-library-aws//service/cloudfront"
	cachePolicyFixtureRoot   = "testdata/ub/cache-policy"
)

func TestCompileCloudFrontCachePolicy(t *testing.T) {
	module := libraryModuleRoot(t)
	version, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}",
		unobinModulePath).Output()
	require.NoError(t, err)
	check := func(t *testing.T, path string) error {
		t.Helper()
		return compile.Run(compile.Options{
			FactoryPath:      path,
			OutDir:           "-",
			GoVersion:        "1.26.2",
			CLIVersion:       strings.TrimSpace(string(version)),
			ReplaceGoModules: map[string]string{module.Path: module.Dir},
			Stdout:           io.Discard,
			Stderr:           io.Discard,
		})
	}
	t.Run("resource and data source", func(t *testing.T) {
		path := filepath.Join(
			"..", "testdata", "ub", "valid", "compiled", "cloudfront-cache-policy", "src",
		)
		require.NoError(t, check(t, path))
		require.NoError(t, check(t, filepath.Join(cachePolicyFixtureRoot, "evaluated")))
	})
	t.Run("invalid literal mode beside a deferred expression", func(t *testing.T) {
		err := check(t, filepath.Join(cachePolicyFixtureRoot, "compile-pending"))
		assert.ErrorContains(t, err, "constraints[0] (predicate): cookie-behavior must be")
	})
	t.Run("literal enums", func(t *testing.T) {
		err := check(t, filepath.Join(cachePolicyFixtureRoot, "enum-invalid"))
		for i, field := range []string{"cookie-behavior", "header-behavior", "query-string-behavior"} {
			for _, kind := range []string{"empty", "invalid", "case"} {
				want := fmt.Sprintf("resource.%s-%s: constraints[%d] (predicate): %s must be",
					field, kind, i, field)
				assert.ErrorContains(t, err, want)
			}
		}
	})
	t.Run("types and required fields", func(t *testing.T) {
		err := check(t, filepath.Join(cachePolicyFixtureRoot, "schema-invalid"))
		for _, message := range []string{
			`missing required input "name"`,
			`missing required input "parameters-in-cache-key-and-forwarded-to-origin"`,
			`missing required field "cookies-config"`,
			`missing required field "headers-config"`,
			`missing required field "query-strings-config"`,
			`missing required field "cookie-behavior"`,
			`missing required field "header-behavior"`,
			`missing required field "query-string-behavior"`,
			`unknown field "region" on aws-cloudfront.cache-policy`,
			`unknown field "tags" on aws-cloudfront.cache-policy`,
			`expected string, got integer`,
			`expected string, got boolean`,
			`factory.ub:61:77: type: type mismatch: expected string, got boolean`,
			`got list(unknown)`,
			`got null`,
		} {
			assert.ErrorContains(t, err, message)
		}
		if assert.Error(t, err) {
			assert.Equal(t, 8, strings.Count(err.Error(), "got null"))
			assert.Equal(t, 4, strings.Count(err.Error(), "got list(unknown)"))
		}
	})
}

type cachePolicySeed struct {
	NextMode string
	NextGzip bool
}

type cachePolicySeedOutput struct {
	Mode string
	Gzip bool
}

func (s *cachePolicySeed) Run(
	_ context.Context, _ runtime.NoConfig,
) (*cachePolicySeedOutput, error) {
	return &cachePolicySeedOutput{Mode: s.NextMode, Gzip: s.NextGzip}, nil
}

func cachePolicyLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	schema := readLibrarySchema(t)
	definition := schema.Resources["cache-policy"]
	require.NotNil(t, definition)
	library := func(path string) *runtime.Library {
		lib := runtime.LibraryWithPath(Library(), path)
		lib.Schema = schema
		lib.Constraints = map[string][]lang.ConstraintSpec{
			cachePolicyKind: definition.Constraints,
		}
		lib.Defaults = map[string][]lang.DefaultSpec{
			cachePolicyKind: definition.Defaults,
		}
		return lib
	}
	return map[string]*runtime.Library{
		"aws-cloudfront":    library(cachePolicyLibraryPath),
		"aws-cloudfront-v2": library(cachePolicyLibraryPath + "-next"),
		"fixture": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[
				cachePolicySeed, *cachePolicySeedOutput, runtime.NoConfig, *cachePolicySeed](),
		}},
	}
}

func cachePolicyExecutor(
	t *testing.T, fixture, endpoint string, libraries map[string]*runtime.Library,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join(cachePolicyFixtureRoot, fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, err := syntax.ParseSource(path, source)
	require.NoError(t, err)
	body := &file.Factory.Body
	store, err := local.NewStore(t.TempDir(), "cache-policy", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libraries), SyntaxSource: body, Libraries: libraries,
		Store: store, Factory: state.FactoryInfo{Name: "cache-policy", ContentRevision: "test"},
		Inputs: map[string]any{
			"aws-config": map[string]any{"endpoint-url": endpoint, "max-attempts": int64(1)},
			"name":       "base", "cookie-mode": "none", "header-mode": "none", "query-mode": "none",
			"items": []any{}, "header-items": []any{}, "query-items": []any{},
			"comment": nil, "gzip": nil, "brotli": nil,
		},
	}
}

type cachePolicyHTTPRequest struct {
	Method, Path, IfMatch string
}

const cachePolicyAPIPath = "/2020-05-31/cache-policy"
const cachePolicyResourcePath = cachePolicyAPIPath + "/test-id"

var cachePolicyGet = cachePolicyHTTPRequest{Method: http.MethodGet, Path: cachePolicyResourcePath}

type cachePolicyCloud struct {
	mu     sync.Mutex
	url    string
	config string
	etag   string
	getErr string
	calls  []cachePolicyHTTPRequest
}

func newCachePolicyCloud(t *testing.T) *cachePolicyCloud {
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
	cloud := new(cachePolicyCloud)
	server := httptest.NewServer(http.HandlerFunc(cloud.handle))
	t.Cleanup(server.Close)
	cloud.url = server.URL
	t.Setenv("AWS_ENDPOINT_URL_CLOUDFRONT", server.URL)
	return cloud
}

func (c *cachePolicyCloud) handle(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, cachePolicyHTTPRequest{
		Method: r.Method, Path: r.URL.Path, IfMatch: r.Header.Get("If-Match"),
	})
	w.Header().Set("Content-Type", "application/xml")
	if r.Method == http.MethodGet {
		switch c.getErr {
		case "":
		case "empty":
			return
		case "NoSuchCachePolicy":
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, "<ErrorResponse><Error><Code>NoSuchCachePolicy</Code>"+
				"</Error></ErrorResponse>")
			return
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, "<ErrorResponse><Error><Code>AccessDenied</Code>"+
				"</Error></ErrorResponse>")
			return
		}
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		start := bytes.Index(body, []byte("<CachePolicyConfig"))
		if start == -1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.config = string(body[start:])
		if r.Method == http.MethodPut {
			c.etag = "updated"
			return
		}
		c.etag = "initial"
	}
	if r.Method == http.MethodDelete {
		c.config = ""
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("ETag", c.etag)
	_, _ = fmt.Fprintf(w, "<CachePolicy><Id>test-id</Id>%s</CachePolicy>", c.config)
}

func (c *cachePolicyCloud) takeCalls() []cachePolicyHTTPRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls = nil
	return calls
}

func saveCachePolicyPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func cachePolicyStep(
	t *testing.T, plan *runtime.PlanFile, decision runtime.Decision,
) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == cachePolicyAddress {
			require.Equal(t, decision, step.Decision)
			return step
		}
	}
	t.Fatalf("missing %s in plan", cachePolicyAddress)
	return runtime.PlanStep{}
}

func applyCachePolicyPlan(
	t *testing.T, executor *runtime.Executor, decision runtime.Decision,
) runtime.PlanStep {
	t.Helper()
	saved := saveCachePolicyPlan(t, executor)
	step := cachePolicyStep(t, saved, decision)
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	return step
}

func assertCachePolicyConstraint(t *testing.T, err error, rule int, field string) {
	t.Helper()
	require.ErrorContains(t, err, fmt.Sprintf(
		"%s: schema: constraints[%d] (predicate): %s must be", cachePolicyAddress, rule, field))
}

func TestCachePolicyPlansEffectiveValuesAndDrift(t *testing.T) {
	cloud := newCachePolicyCloud(t)
	executor := cachePolicyExecutor(t, "evaluated", cloud.url, cachePolicyLibraries(t))
	executor.Inputs["cookie-mode"] = "whitelist"
	executor.Inputs["header-mode"] = "whitelist"
	executor.Inputs["query-mode"] = "allExcept"
	executor.Inputs["items"] = []any{"z", "a", "z", ""}
	executor.Inputs["header-items"] = []any{"Z", "X", "Z", ""}
	executor.Inputs["query-items"] = []any{"b", "a", "b", ""}
	step := applyCachePolicyPlan(t, executor, runtime.DecisionCreate)
	assert.EqualValues(t, 86400, step.Inputs["default-ttl"])
	assert.EqualValues(t, 31536000, step.Inputs["max-ttl"])
	assert.EqualValues(t, 0, step.Inputs["min-ttl"])
	require.Equal(t, []cachePolicyHTTPRequest{
		{Method: http.MethodPost, Path: cachePolicyAPIPath},
	}, cloud.takeCalls())
	executor.Inputs["items"] = []any{"a", "z"}
	executor.Inputs["header-items"] = []any{"X", "Z"}
	executor.Inputs["query-items"] = []any{"a", "b"}
	executor.Inputs["comment"] = ""
	executor.Inputs["gzip"] = false
	executor.Inputs["brotli"] = false
	step = applyCachePolicyPlan(t, executor, runtime.DecisionNoOp)
	require.Equal(t, []cachePolicyHTTPRequest{cachePolicyGet}, cloud.takeCalls())
	assert.EqualValues(t, 86400, step.ObservedOutputs["default-ttl"])
	assert.EqualValues(t, 31536000, step.ObservedOutputs["max-ttl"])
	assert.EqualValues(t, 0, step.ObservedOutputs["min-ttl"])
	require.Equal(t, map[string]any{
		"cookies-config": map[string]any{
			"cookie-behavior": "whitelist",
			"cookies":         map[string]any{"items": []any{"a", "z"}},
		},
		"headers-config": map[string]any{
			"header-behavior": "whitelist",
			"headers":         map[string]any{"items": []any{"X", "Z"}},
		},
		"query-strings-config": map[string]any{
			"query-string-behavior": "allExcept",
			"query-strings":         map[string]any{"items": []any{"a", "b"}},
		},
		"enable-accept-encoding-gzip":   false,
		"enable-accept-encoding-brotli": false,
	}, step.ObservedOutputs[cachePolicyParametersKey])

	cloud.mu.Lock()
	cloud.config = strings.Replace(cloud.config, "<Name>base</Name>", "<Name>drift</Name>", 1)
	cloud.etag = "external-edit"
	cloud.mu.Unlock()
	step = applyCachePolicyPlan(t, executor, runtime.DecisionUpdate)
	require.True(t, step.Drift())
	require.Empty(t, step.ReplacementReasons)
	assert.Equal(t, "external-edit", step.ObservedOutputs["etag"])
	require.Equal(t, []cachePolicyHTTPRequest{
		cachePolicyGet,
		{Method: http.MethodPut, Path: cachePolicyResourcePath, IfMatch: "external-edit"},
		cachePolicyGet,
	}, cloud.takeCalls())
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	require.Equal(t, "test-id", snapshot.Find(cachePolicyAddress).Outputs["id"])
	require.Equal(t, "updated", snapshot.Find(cachePolicyAddress).Outputs["etag"])
	require.Equal(t, "base", snapshot.Find(cachePolicyAddress).Outputs["name"])
}

func TestCachePolicyRefreshOnlyRecreatesOnAbsence(t *testing.T) {
	cloud := newCachePolicyCloud(t)
	executor := cachePolicyExecutor(t, "evaluated", cloud.url, cachePolicyLibraries(t))
	applyCachePolicyPlan(t, executor, runtime.DecisionCreate)
	cloud.takeCalls()
	for _, code := range []string{"NoSuchCachePolicy", "empty", "AccessDenied"} {
		t.Run(code, func(t *testing.T) {
			cloud.mu.Lock()
			cloud.getErr = code
			cloud.mu.Unlock()
			if code == "AccessDenied" {
				_, err := executor.Plan(t.Context())
				require.ErrorContains(t, err, "get cache policy test-id:")
				require.ErrorContains(t, err, "AccessDenied")
			} else {
				saved := saveCachePolicyPlan(t, executor)
				step := cachePolicyStep(t, saved, runtime.DecisionCreate)
				require.True(t, step.Gone())
			}
			require.Equal(t, []cachePolicyHTTPRequest{cachePolicyGet}, cloud.takeCalls())
		})
	}
}

func TestCachePolicyPlanEnforcesEvaluatedModes(t *testing.T) {
	cloud := newCachePolicyCloud(t)
	executor := cachePolicyExecutor(t, "evaluated", cloud.url, cachePolicyLibraries(t))
	for i, mode := range []string{"cookie", "header", "query"} {
		key := mode + "-mode"
		field := mode + "-behavior"
		if mode == "query" {
			field = "query-string-behavior"
		}
		for _, value := range []any{nil, "None"} {
			t.Run(fmt.Sprintf("%s/%v", mode, value), func(t *testing.T) {
				executor.Inputs[key] = value
				_, err := executor.Plan(t.Context())
				assertCachePolicyConstraint(t, err, i, field)
				require.Empty(t, cloud.takeCalls())
				executor.Inputs[key] = "none"
			})
		}
	}
}

func TestCachePolicySavedNoOpAndHistoricalDestroy(t *testing.T) {
	cloud := newCachePolicyCloud(t)
	libraries := cachePolicyLibraries(t)
	executor := cachePolicyExecutor(t, "evaluated", cloud.url, libraries)
	lib := libraries["aws-cloudfront"]
	constraints := lib.Constraints[cachePolicyKind]
	executor.Inputs["cookie-mode"] = "None"
	delete(lib.Constraints, cachePolicyKind)
	applyCachePolicyPlan(t, executor, runtime.DecisionCreate)
	require.Equal(t, []cachePolicyHTTPRequest{
		{Method: http.MethodPost, Path: cachePolicyAPIPath},
	}, cloud.takeCalls())

	saved := saveCachePolicyPlan(t, executor)
	step := cachePolicyStep(t, saved, runtime.DecisionNoOp)
	require.False(t, step.Drift())
	require.Equal(t, []cachePolicyHTTPRequest{cachePolicyGet}, cloud.takeCalls())
	lib.Constraints[cachePolicyKind] = constraints
	_, err := executor.ApplyPlan(t.Context(), saved)
	assertCachePolicyConstraint(t, err, 0, "cookie-behavior")
	require.Empty(t, cloud.takeCalls())

	_, err = executor.Plan(t.Context())
	assertCachePolicyConstraint(t, err, 0, "cookie-behavior")
	require.Equal(t, []cachePolicyHTTPRequest{cachePolicyGet}, cloud.takeCalls())

	cloud.mu.Lock()
	cloud.etag = "external-edit"
	cloud.mu.Unlock()
	executor.Destroy = true
	applyCachePolicyPlan(t, executor, runtime.DecisionDestroy)
	require.Equal(t, []cachePolicyHTTPRequest{
		cachePolicyGet, cachePolicyGet,
		{Method: http.MethodDelete, Path: cachePolicyResourcePath, IfMatch: "external-edit"},
	}, cloud.takeCalls())
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find(cachePolicyAddress))
}

func TestCachePolicySavedDeferredModes(t *testing.T) {
	libraries := cachePolicyLibraries(t)
	for _, tt := range []struct {
		fixture  string
		input    string
		decision runtime.Decision
		pending  string
		valid    bool
	}{
		{"deferred", "none", runtime.DecisionCreate, "action.seed.mode", true},
		{"deferred", "None", runtime.DecisionCreate, "action.seed.mode", false},
		{"partially-pending", "none", runtime.DecisionCreate, "action.seed.gzip", false},
		{"replacement", "None", runtime.DecisionReplace, "action.seed.mode", false},
	} {
		t.Run(tt.fixture+"/"+tt.input, func(t *testing.T) {
			cloud := newCachePolicyCloud(t)
			executor := cachePolicyExecutor(t, tt.fixture, cloud.url, libraries)
			executor.Inputs["cookie-mode"] = tt.input
			if tt.fixture == "partially-pending" {
				_, err := executor.Plan(t.Context())
				assertCachePolicyConstraint(t, err, 0, "cookie-behavior")
				require.Empty(t, cloud.takeCalls())
				return
			}
			if tt.decision == runtime.DecisionReplace {
				initial := cachePolicyExecutor(t, "evaluated", cloud.url, libraries)
				initial.Store = executor.Store
				applyCachePolicyPlan(t, initial, runtime.DecisionCreate)
				require.Equal(t, []cachePolicyHTTPRequest{
					{Method: http.MethodPost, Path: cachePolicyAPIPath},
				}, cloud.takeCalls())
			}
			saved := saveCachePolicyPlan(t, executor)
			step := cachePolicyStep(t, saved, tt.decision)
			require.Equal(t, map[string][]string{cachePolicyParametersKey: {tt.pending}},
				step.UnresolvedInputs)
			if tt.decision == runtime.DecisionReplace {
				require.Equal(t, []string{"binding"}, step.ReplacementReasons)
				require.Equal(t, "test-id", *step.ExpectedStableID)
				require.Equal(t, []cachePolicyHTTPRequest{cachePolicyGet}, cloud.takeCalls())
			} else {
				require.Empty(t, cloud.takeCalls())
			}
			_, err := executor.ApplyPlan(t.Context(), saved)
			if tt.valid {
				require.NoError(t, err)
				require.Equal(t, []cachePolicyHTTPRequest{
					{Method: http.MethodPost, Path: cachePolicyAPIPath},
				}, cloud.takeCalls())
			} else {
				assertCachePolicyConstraint(t, err, 0, "cookie-behavior")
				require.Empty(t, cloud.takeCalls())
			}
		})
	}
}

func TestCachePolicyRejectsEvaluatedNullName(t *testing.T) {
	cloud := newCachePolicyCloud(t)
	executor := cachePolicyExecutor(t, "evaluated", cloud.url, cachePolicyLibraries(t))
	executor.Inputs["name"] = nil
	_, err := executor.Plan(t.Context())
	assertCachePolicyConstraint(t, err, 3, "name")
	require.Empty(t, cloud.takeCalls())
}
