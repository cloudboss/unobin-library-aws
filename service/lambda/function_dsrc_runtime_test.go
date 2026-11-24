package lambda

import (
	"context"
	"encoding/json"
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

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

type functionLookupRequest struct{ Method, URI, Region, Retry string }
type functionLookupEndpoint struct {
	mu          sync.Mutex
	endpoint    string
	calls       []functionLookupRequest
	description string
	mutations   []string
	image       bool
	fail        bool
	reverse     bool
}

func newFunctionLookupEndpoint(t *testing.T) *functionLookupEndpoint {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing"))
	a := &functionLookupEndpoint{description: "initial"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		scope := strings.Split(req.Header.Get("Authorization"), "/")
		region := ""
		if len(scope) >= 3 {
			region = scope[2]
		}
		a.calls = append(a.calls, functionLookupRequest{
			req.Method, req.URL.RequestURI(), region, req.Header.Get("Amz-Sdk-Request")})
		w.Header().Set("Content-Type", "application/json")
		if a.fail {
			w.Header().Set("X-Amzn-Errortype", "ServiceException")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"message":"failure"}`)
			return
		}
		var response any
		switch {
		case strings.HasSuffix(req.URL.Path, "/versions"):
			response = map[string]any{"Versions": []any{map[string]any{"Version": "7"}}}
		case strings.HasSuffix(req.URL.Path, "/concurrency"):
			response = map[string]any{"ReservedConcurrentExecutions": 12}
		case strings.HasSuffix(req.URL.Path, "/code-signing-config"):
			response = map[string]any{"CodeSigningConfigArn": "signing-config"}
		default:
			pkg := "Zip"
			if a.image {
				pkg = "Image"
			}
			subnets := []string{"b", "a", "b"}
			if a.reverse {
				subnets = []string{"a", "b", "a"}
			}
			response = map[string]any{
				"Code": map[string]any{"ImageUri": "image:tag", "ResolvedImageUri": "image@digest"},
				"Configuration": map[string]any{
					"FunctionArn": "arn:aws:lambda:eu-west-3:123456789012:function:remote:7",
					"Version":     "7", "PackageType": pkg, "Description": a.description,
					"CodeSha256": "checksum", "CodeSize": int64(1 << 40),
					"VpcConfig": map[string]any{"SubnetIds": subnets, "SecurityGroupIds": []string{"z", "x", "z"}},
				},
				"Tags": map[string]string{"ordinary": "value", "aws:hidden": "x"},
			}
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	a.endpoint = server.URL
	return a
}

func (a *functionLookupEndpoint) takeCalls() []functionLookupRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := a.calls
	a.calls = nil
	return result
}

func requireFunctionReads(t *testing.T, a *functionLookupEndpoint, selector string, count int) {
	t.Helper()
	var want []functionLookupRequest
	for range count {
		escaped := strings.ReplaceAll(url.PathEscape(selector), ":", "%3A")
		root := "/2015-03-31/functions/" + escaped
		want = append(want,
			functionLookupRequest{"GET", root + "/versions?MaxItems=10000", "us-gov-west-1",
				"attempt=1; max=1"},
			functionLookupRequest{"GET", root + "?Qualifier=7", "us-gov-west-1", "attempt=1; max=1"},
			functionLookupRequest{"GET", "/2019-09-30/functions/" + escaped + "/concurrency",
				"us-gov-west-1", "attempt=1; max=1"},
		)
		if !a.image {
			want = append(want, functionLookupRequest{"GET",
				"/2020-06-30/functions/" + escaped + "/code-signing-config",
				"us-gov-west-1", "attempt=1; max=1"})
		}
	}
	require.Equal(t, want, a.takeCalls())
}

type functionLookupValue struct {
	Name   *string `ub:"name"`
	Mutate bool    `ub:"mutate"`
	api    *functionLookupEndpoint
}

func (v *functionLookupValue) Create(context.Context, any) (*functionLookupValue, error) {
	v.api.mu.Lock()
	defer v.api.mu.Unlock()
	v.api.mutations = append(v.api.mutations, "create:"+fmt.Sprint(v.Name))
	if v.Mutate && v.Name != nil {
		v.api.description = *v.Name
	}
	return v, nil
}
func (v *functionLookupValue) Read(
	_ context.Context, _ any, prior runtime.Prior[functionLookupValue, *functionLookupValue, any],
) (*functionLookupValue, error) {
	return prior.Outputs, nil
}
func (v *functionLookupValue) Update(
	context.Context, any, runtime.Prior[functionLookupValue, *functionLookupValue, any],
) (*functionLookupValue, error) {
	v.api.mu.Lock()
	defer v.api.mu.Unlock()
	v.api.mutations = append(v.api.mutations, "update:"+fmt.Sprint(v.Name))
	if v.Mutate && v.Name != nil {
		v.api.description = *v.Name
	}
	return v, nil
}
func (*functionLookupValue) Delete(
	context.Context, any, runtime.Prior[functionLookupValue, *functionLookupValue, any],
) error {
	return nil
}

type functionLookupSeed struct {
	Name      *string `ub:"name"`
	Qualifier *string `ub:"qualifier"`
}

type functionLookupSeedOutput struct {
	Value           *string `ub:"value"`
	OutputQualifier *string `ub:"output-qualifier"`
}

func (v *functionLookupSeed) Run(context.Context, any) (*functionLookupSeedOutput, error) {
	return &functionLookupSeedOutput{Value: v.Name, OutputQualifier: v.Qualifier}, nil
}

func functionLookupLibraries(t *testing.T, a *functionLookupEndpoint) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.Schema = readLibrarySchema(t)
	schema := lib.Schema.DataSources["function"]
	schema.EvaluatedInputs = maps.Clone(schema.Inputs)
	return map[string]*runtime.Library{"lambda": lib, "test": {
		Resources: map[string]runtime.ResourceRegistration{
			"value": runtime.MakeResourceWith(
				runtime.ResourceDefinition[functionLookupValue, *functionLookupValue, any]{SchemaVersion: 1},
				func() *functionLookupValue { return &functionLookupValue{api: a} }),
		},
		Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[functionLookupSeed, *functionLookupSeedOutput, any](),
		},
	}}
}

func functionLookupExecutor(
	t *testing.T, fixture string, a *functionLookupEndpoint, store *local.Store,
	libraries map[string]*runtime.Library,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata/ub/function-lookup", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := syntax.ParseSource(path, source)
	require.NoError(t, err)
	if store == nil {
		store, err = local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
		require.NoError(t, err)
	}
	body := &f.Factory.Body
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libraries), SyntaxSource: body, Libraries: libraries,
		Store: store,
		Inputs: map[string]any{"endpoint": a.endpoint, "name": "selector", "qualifier": nil,
			"description": "initial"},
	}
}

func functionLookupPlan(t *testing.T, e *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := e.Plan(t.Context())
	require.NoError(t, err)
	body, err := runtime.SealPlan(plan, encrypters.Noop{})
	require.NoError(t, err)
	opened, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	return opened
}
func functionLookupStep(t *testing.T, p *runtime.PlanFile, address string) *runtime.PlanStep {
	t.Helper()
	for i := range p.Steps {
		if p.Steps[i].Address == address {
			return &p.Steps[i]
		}
	}
	t.Fatalf("missing step %s", address)
	return nil
}

func functionLookupExpected() map[string]any {
	arn := "arn:aws:lambda:eu-west-3:123456789012:function:remote"
	prefix := "arn:aws-us-gov:apigateway:us-gov-west-1:lambda:path/"
	return map[string]any{
		"arn": arn, "qualified-arn": arn + ":7",
		"invoke-arn":           prefix + "2015-03-31/functions/" + arn + "/invocations",
		"qualified-invoke-arn": prefix + "2015-03-31/functions/" + arn + ":7/invocations",
		"response-streaming-invoke-arn": prefix + "2021-11-15/functions/" +
			arn + ":7/response-streaming-invocations",
		"code-sha256": "checksum", "source-code-hash": "checksum",
		"code-signing-config-arn": "signing-config",
		"description":             "initial", "handler": "", "image-uri": "image:tag", "kms-key-arn": "",
		"last-modified": "", "memory-size": int64(0), "reserved-concurrent-executions": int64(12),
		"role": "", "runtime": "", "signing-job-arn": "", "signing-profile-version-arn": "",
		"source-code-size": int64(1 << 40), "source-kms-key-arn": "", "timeout": int64(0), "version": "7",
		"architectures": []any{}, "layers": []any{}, "tags": map[string]any{"ordinary": "value"},
		"capacity-provider-config": nil, "dead-letter-config": nil, "durable-config": nil,
		"environment": nil, "ephemeral-storage": nil, "file-system-configs": []any{},
		"logging-config": nil, "tenancy-config": nil,
		"tracing-config": map[string]any{"mode": "PassThrough"},
		"vpc-config": map[string]any{"ipv6-allowed-for-dual-stack": false,
			"security-group-ids": []any{"x", "z"}, "subnet-ids": []any{"a", "b"}, "vpc-id": ""},
	}
}

func TestFunctionLookupNativeSavedAndNoOp(t *testing.T) {
	a := newFunctionLookupEndpoint(t)
	libraries := functionLookupLibraries(t, a)
	e := functionLookupExecutor(t, "runtime", a, nil, libraries)
	saved := functionLookupPlan(t, e)
	require.Equal(t, functionLookupExpected(), functionLookupStep(t, saved,
		"data-source.lookup").ObservedOutputs)
	_, err := e.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	requireFunctionReads(t, a, "selector", 2)
	snapshot, err := e.Store.Current()
	require.NoError(t, err)
	require.Equal(t, map[string]any{"function-name": "selector", "qualifier": nil},
		snapshot.Find("data-source.lookup").Inputs)
	require.Equal(t, functionLookupExpected(), snapshot.Find("data-source.lookup").Outputs)
	require.Equal(t, map[string]any{"name": "initial", "mutate": false},
		snapshot.Find("resource.consumer").Inputs)
	a.mu.Lock()
	a.reverse = true
	a.mu.Unlock()
	saved = functionLookupPlan(t, e)
	require.Equal(t, runtime.DecisionNoOp, functionLookupStep(t, saved, "resource.consumer").Decision)
	_, err = e.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	requireFunctionReads(t, a, "selector", 2)
	before := slices.Clone(a.mutations)
	e.Inputs["name"] = nil
	_, err = e.Plan(t.Context())
	require.ErrorContains(t, err, `data-source.lookup: field "function-name": required but is null`)
	requireFunctionReads(t, a, "", 0)
	e.Inputs["name"] = "selector"
	saved = functionLookupPlan(t, e)
	requireFunctionReads(t, a, "selector", 1)
	missing := functionLookupExecutor(t, "missing", a, e.Store.(*local.Store), libraries)
	_, err = missing.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err,
		`data-source.lookup: field "function-name": required but not provided`)
	requireFunctionReads(t, a, "", 0)
	e.Inputs["name"] = nil
	_, err = e.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, `data-source.lookup: field "function-name": required but is null`)
	requireFunctionReads(t, a, "", 0)
	require.Equal(t, before, a.mutations)
	e.Inputs["name"] = "selector"
	saved = functionLookupPlan(t, e)
	a.mu.Lock()
	a.description = "drift"
	a.mu.Unlock()
	_, err = e.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "data source data-source.lookup changed since the plan was computed")
	requireFunctionReads(t, a, "selector", 2)
	require.Equal(t, before, a.mutations)
}

func TestFunctionLookupNativeInitialNullAndEmpty(t *testing.T) {
	a := newFunctionLookupEndpoint(t)
	libraries := functionLookupLibraries(t, a)
	for _, value := range []any{nil, ""} {
		e := functionLookupExecutor(t, "runtime", a, nil, libraries)
		e.Inputs["name"] = value
		_, err := e.Plan(t.Context())
		if value == nil {
			require.ErrorContains(t, err, `data-source.lookup: field "function-name": required but is null`)
		} else {
			require.ErrorContains(t, err, "input member FunctionName must not be empty")
		}
		requireFunctionReads(t, a, "", 0)
		require.Empty(t, a.mutations)
	}
	e := functionLookupExecutor(t, "missing", a, nil, libraries)
	_, err := e.Plan(t.Context())
	require.ErrorContains(t, err,
		`data-source.lookup: field "function-name": required but not provided`)
	requireFunctionReads(t, a, "", 0)
}

func TestFunctionLookupNativeDeferred(t *testing.T) {
	a := newFunctionLookupEndpoint(t)
	libraries := functionLookupLibraries(t, a)
	for _, name := range []any{"selector", nil} {
		for _, qualifier := range []any{nil, "", "live"} {
			e := functionLookupExecutor(t, "deferred", a, nil, libraries)
			e.Inputs["name"], e.Inputs["qualifier"] = name, qualifier
			saved := functionLookupPlan(t, e)
			step := functionLookupStep(t, saved, "data-source.lookup")
			require.Equal(t, map[string][]string{
				"function-name": {"action.seed.value"}, "qualifier": {"action.seed.output-qualifier"}},
				step.UnresolvedInputs)
			require.Nil(t, step.ObservedOutputs)
			requireFunctionReads(t, a, "", 0)
			before := slices.Clone(a.mutations)
			_, err := e.ApplyPlan(t.Context(), saved)
			if name == nil {
				require.ErrorContains(t, err, `data-source.lookup: field "function-name": required but is null`)
				requireFunctionReads(t, a, "", 0)
				require.Equal(t, before, a.mutations)
			} else {
				require.NoError(t, err)
				if qualifier == "live" {
					calls := a.takeCalls()
					require.Equal(t, []functionLookupRequest{
						{"GET", "/2015-03-31/functions/selector?Qualifier=live", "us-gov-west-1", "attempt=1; max=1"},
						{"GET", "/2019-09-30/functions/selector/concurrency", "us-gov-west-1", "attempt=1; max=1"},
						{"GET", "/2020-06-30/functions/selector/code-signing-config", "us-gov-west-1",
							"attempt=1; max=1"},
					}, calls)
				} else {
					requireFunctionReads(t, a, "selector", 1)
				}
			}
		}
	}
}

func TestFunctionLookupNativeDependencyAndDestroy(t *testing.T) {
	a := newFunctionLookupEndpoint(t)
	libraries := functionLookupLibraries(t, a)
	e := functionLookupExecutor(t, "dependent", a, nil, libraries)
	saved := functionLookupPlan(t, e)
	require.Nil(t, functionLookupStep(t, saved, "data-source.lookup").ObservedOutputs)
	_, err := e.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	requireFunctionReads(t, a, "selector", 1)
	e.Inputs["description"] = "updated"
	saved = functionLookupPlan(t, e)
	require.Equal(t, runtime.DecisionUpdate, functionLookupStep(t, saved,
		"resource.producer").Decision)
	require.Nil(t, functionLookupStep(t, saved, "data-source.lookup").ObservedOutputs)
	require.Equal(t, map[string]any{"function-name": "selector", "qualifier": nil},
		functionLookupStep(t, saved, "data-source.lookup").Inputs)
	requireFunctionReads(t, a, "", 0)
	_, err = e.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	requireFunctionReads(t, a, "selector", 1)
	snapshot, err := e.Store.Current()
	require.NoError(t, err)
	require.Equal(t, "updated", snapshot.Find("resource.consumer").Inputs["name"])
	for _, invalid := range []map[string]any{{"function-name": nil}, {}} {
		snapshot.Find("data-source.lookup").Inputs = invalid
		revision, err := e.Store.Write(snapshot)
		require.NoError(t, err)
		require.NoError(t, e.Store.SetCurrent(revision))
		e.Destroy = true
		e.Inputs["name"] = nil
		saved = functionLookupPlan(t, e)
		require.Equal(t, runtime.DecisionDestroy,
			functionLookupStep(t, saved, "data-source.lookup").Decision)
		_, err = e.ApplyPlan(t.Context(), saved)
		require.NoError(t, err)
		requireFunctionReads(t, a, "", 0)
		destroyed, err := e.Store.Current()
		require.NoError(t, err)
		require.Empty(t, destroyed.Entries)
	}

}
