package ecr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/ecr"
)

type replicationSeed struct {
	Echo any `ub:"echo"`
}

func (r *replicationSeed) Run(context.Context, *awscfg.Configuration) (map[string]any, error) {
	return map[string]any{"value": r.Echo}, nil
}

type replicationCall struct {
	Operation string
	Body      map[string]any
	Region    string
}

type replicationAPI struct {
	sync.Mutex
	url           string
	calls         []replicationCall
	configuration map[string]any
	account       string
	failRead      bool
	failAfterPut  bool
	failPut       bool
	identityError bool
}

func replicationEndpoint(t *testing.T) *replicationAPI {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "replication-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "replication-secret")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("AWS_PROFILE", "")
	api := &replicationAPI{account: "123456789012"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.Lock()
		defer api.Unlock()
		require.Contains(t, r.Header.Get("Authorization"), "Credential=replication-access/")
		region := strings.Split(r.Header.Get("Authorization"), "/")[2]
		require.Equal(t, http.MethodPost, r.Method)
		operation := strings.TrimPrefix(r.Header.Get("X-Amz-Target"),
			"AmazonEC2ContainerRegistry_V20150921.")
		if operation == "" {
			require.Contains(t, r.Header.Get("Authorization"), "/sts/aws4_request")
			require.NoError(t, r.ParseForm())
			require.Equal(t, "GetCallerIdentity", r.Form.Get("Action"))
			api.calls = append(api.calls, replicationCall{"GetCallerIdentity", nil, region})
			w.Header().Set("Content-Type", "text/xml")
			if api.identityError {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `<ErrorResponse><Error><Code>AccessDenied</Code>`+
					`<Message>identity denied</Message></Error></ErrorResponse>`)
				return
			}
			_, _ = io.WriteString(w, `<GetCallerIdentityResponse><GetCallerIdentityResult><Account>`+
				api.account+`</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`)
			return
		}
		require.Contains(t, r.Header.Get("Authorization"), "/ecr/aws4_request")
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		api.calls = append(api.calls, replicationCall{operation, body, region})
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		switch operation {
		case "PutReplicationConfiguration":
			if api.failPut {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"__type":"InvalidParameterException","message":"put failed"}`)
				return
			}
			api.configuration = body["replicationConfiguration"].(map[string]any)
			if api.failAfterPut {
				api.failRead = true
			}
			_, _ = io.WriteString(w, `{}`)
		case "DescribeRegistry":
			require.Equal(t, map[string]any{}, body)
			if api.failRead {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"__type":"InvalidParameterException","message":"read failed"}`)
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"registryId": api.account, "replicationConfiguration": api.configuration,
			}))
		default:
			t.Errorf("unexpected operation %s", operation)
		}
	}))
	t.Cleanup(server.Close)
	api.url = server.URL
	return api
}

func (api *replicationAPI) takeCalls() []replicationCall {
	api.Lock()
	defer api.Unlock()
	calls := api.calls
	api.calls = nil
	return calls
}

func (api *replicationAPI) mutations() []replicationCall {
	var result []replicationCall
	for _, call := range api.takeCalls() {
		if call.Operation == "PutReplicationConfiguration" {
			result = append(result, call)
		}
	}
	return result
}

func replicationConfiguration(region string, filters bool) map[string]any {
	rule := map[string]any{"destinations": []any{map[string]any{
		"region": region, "registry-id": "000000000001",
	}}}
	if filters {
		rule["repository-filters"] = []any{map[string]any{
			"filter": " x ", "filter-type": "PREFIX_MATCH",
		}}
	}
	return map[string]any{"rules": []any{rule}}
}

func replicationHTTPConfiguration(region string, filters bool) map[string]any {
	rule := map[string]any{"destinations": []any{map[string]any{
		"region": region, "registryId": "000000000001",
	}}}
	if filters {
		rule["repositoryFilters"] = []any{map[string]any{
			"filter": " x ", "filterType": "PREFIX_MATCH",
		}}
	}
	return map[string]any{"rules": []any{rule}}
}

func replicationExecutor(t *testing.T, fixture string) *runtime.Executor {
	t.Helper()
	lib := runtime.LibraryWithPath(Library(), "github.com/cloudboss/unobin-library-aws//service/ecr")
	lib.Schema = readLibrarySchema(t)
	// Match the metadata emitted by the production compiler.
	lib.Schema.Resources["replication-configuration"].EvaluatedInputs =
		maps.Clone(lib.Schema.Resources["replication-configuration"].Inputs)
	rules := lib.Schema.Resources["replication-configuration"].Constraints
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.replication-configuration": rules,
	}
	libs := map[string]*runtime.Library{
		"ecr": lib, "test": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[replicationSeed, map[string]any, *awscfg.Configuration](),
		}}}
	path := filepath.Join("testdata", "ub", "replication-runtime", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	store, err := local.NewStore(t.TempDir(), "replication", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{DAG: runtime.BuildSyntaxDAG(body, libs), SyntaxSource: &body,
		Libraries: libs, Store: store}
}

func replicationInputs(api *replicationAPI) map[string]any {
	return map[string]any{"endpoint": api.url, "region": "us-east-1",
		"configuration": replicationConfiguration("us-west-1", true), "trigger": "initial"}
}

func savedReplicationPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	data, err := runtime.SealPlan(plan, encrypters.Noop{})
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(data, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	return saved
}

func replicationStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == "resource.replication" {
			return step
		}
	}
	t.Fatal("replication step missing")
	return runtime.PlanStep{}
}

func replicationPut(region string, filters bool, source string) replicationCall {
	return replicationCall{"PutReplicationConfiguration", map[string]any{
		"replicationConfiguration": replicationHTTPConfiguration(region, filters),
	}, source}
}

func replicationClear(region string) replicationCall {
	return replicationCall{"PutReplicationConfiguration", map[string]any{
		"replicationConfiguration": map[string]any{"rules": []any{}},
	}, region}
}

func TestReplicationNativeLifecycle(t *testing.T) {
	api := replicationEndpoint(t)
	executor := replicationExecutor(t, "evaluated")
	executor.Inputs = replicationInputs(api)
	created := savedReplicationPlan(t, executor)
	require.Equal(t, runtime.DecisionCreate, replicationStep(t, created).Decision)
	_, err := executor.ApplyPlan(t.Context(), created)
	require.NoError(t, err)
	require.Equal(t, []replicationCall{replicationPut("us-west-1", true, "us-east-1")},
		api.mutations())
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	require.Equal(t, map[string]any{"registry-id": "123456789012", "region": "us-east-1",
		"replication-configuration": replicationConfiguration("us-west-1", true),
	}, snapshot.Find("resource.replication").Outputs)
	unchanged := savedReplicationPlan(t, executor)
	require.Equal(t, runtime.DecisionNoOp, replicationStep(t, unchanged).Decision)
	_, err = executor.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	require.Empty(t, api.mutations())
	api.configuration = replicationHTTPConfiguration("us-west-2", true)
	drift := savedReplicationPlan(t, executor)
	require.Equal(t, runtime.DecisionUpdate, replicationStep(t, drift).Decision)
	_, err = executor.ApplyPlan(t.Context(), drift)
	require.NoError(t, err)
	require.Equal(t, []replicationCall{replicationPut("us-west-1", true, "us-east-1")},
		api.mutations())
	executor.Inputs["configuration"] = replicationConfiguration("us-west-2", false)
	updated := savedReplicationPlan(t, executor)
	require.Equal(t, runtime.DecisionUpdate, replicationStep(t, updated).Decision)
	_, err = executor.ApplyPlan(t.Context(), updated)
	require.NoError(t, err)
	require.Equal(t, []replicationCall{replicationPut("us-west-2", false, "us-east-1")},
		api.mutations())
	executor.Inputs["region"] = "us-gov-west-1"
	replaced := savedReplicationPlan(t, executor)
	require.Equal(t, runtime.DecisionReplace, replicationStep(t, replaced).Decision)
	_, err = executor.ApplyPlan(t.Context(), replaced)
	require.NoError(t, err)
	require.Equal(t, []replicationCall{replicationClear("us-east-1"),
		replicationPut("us-west-2", false, "us-gov-west-1")}, api.mutations())
	executor.Destroy = true
	_, err = executor.ApplyPlan(t.Context(), savedReplicationPlan(t, executor))
	require.NoError(t, err)
	require.Equal(t, []replicationCall{replicationClear("us-gov-west-1")}, api.mutations())
}

func TestReplicationCompiledCLIPlan(t *testing.T) {
	module, core := libraryModuleRoot(t), unobinModuleRoot(t)
	out := t.TempDir()
	result, err := compile.RunResult(compile.Options{
		FactoryPath: "testdata/ub/replication-runtime/evaluated", OutDir: out, Build: true,
		GoVersion: "1.26.2", CLIVersion: "v0.13.0", ReplaceUnobin: core.Dir,
		ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(directory string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(directory), nil
		}, Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	api := replicationEndpoint(t)
	configPath := filepath.Join(out, "config.ub")
	config, err := os.ReadFile("testdata/ub/replication-runtime/evaluated/config.ub")
	require.NoError(t, err)
	config = []byte(strings.ReplaceAll(string(config), "ENDPOINT", api.url))
	require.NoError(t, os.WriteFile(configPath, config, 0o600))
	planPath := filepath.Join(out, "saved.plan")
	cmd := exec.CommandContext(t.Context(), result.BinaryPath, "plan", "--config", configPath,
		"--out", planPath, "--allow-version-mismatch")
	cmd.Dir = out
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	encoded, err := os.ReadFile(planPath)
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, runtime.DecisionCreate, replicationStep(t, saved).Decision)
	expected := replicationConfiguration("us-west-1", false)
	expected["rules"].([]any)[0].(map[string]any)["repository-filters"] = nil
	require.Equal(t, expected, replicationStep(t, saved).Inputs["replication-configuration"])
	require.Empty(t, api.takeCalls())
	run := func(args ...string) []byte {
		command := exec.CommandContext(t.Context(), result.BinaryPath, args...)
		command.Dir = out
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return output
	}
	run("apply", planPath)
	require.Equal(t, []replicationCall{replicationPut("us-west-1", false, "us-east-1")},
		api.mutations())
	run("plan", "--config", configPath, "--out", planPath, "--allow-version-mismatch")
	encoded, err = os.ReadFile(planPath)
	require.NoError(t, err)
	saved, err = runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, runtime.DecisionNoOp, replicationStep(t, saved).Decision)
	run("apply", planPath)
	require.Empty(t, api.mutations())
	invalid := strings.ReplaceAll(string(config), "us-west-1", "INVALID")
	require.NoError(t, os.WriteFile(configPath, []byte(invalid), 0o600))
	command := exec.CommandContext(t.Context(), result.BinaryPath, "plan", "--config", configPath,
		"--allow-version-mismatch")
	command.Dir = out
	output, err = command.CombinedOutput()
	require.Error(t, err, string(output))
	require.Contains(t, string(output), "resource.replication")
	require.Contains(t, string(output), "constraints[")
	require.Contains(t, string(output), "destination region must match")
	require.Empty(t, api.mutations())
}

type replicationInvalidCase struct {
	name       string
	value      map[string]any
	diagnostic string
	native     bool
}

func replicationInvalidInputs() []replicationInvalidCase {
	var cases []replicationInvalidCase
	add := func(name, diagnostic string, native bool, change func(map[string]any, map[string]any)) {
		value := replicationConfiguration("us-west-1", true)
		rule := value["rules"].([]any)[0].(map[string]any)
		change(value, rule)
		cases = append(cases, replicationInvalidCase{name, value, diagnostic, native})
	}
	add("null rules", "rules", false, func(c, r map[string]any) { c["rules"] = nil })
	add("missing rules", "rules", false, func(c, r map[string]any) { delete(c, "rules") })
	add("zero rules", "rules must contain", true, func(c, r map[string]any) { c["rules"] = []any{} })
	add("eleven duplicate rules", "rules must contain", true, func(c, r map[string]any) {
		rules := make([]any, 11)
		for i := range rules {
			rules[i] = r
		}
		c["rules"] = rules
	})
	add("null destinations", "destinations", false, func(c, r map[string]any) {
		r["destinations"] = nil
	})
	add("missing destinations", "destinations", false, func(c, r map[string]any) {
		delete(r, "destinations")
	})
	add("zero destinations", "destinations must contain", true, func(c, r map[string]any) {
		r["destinations"] = []any{}
	})
	add("26 destinations", "destinations must contain", true, func(c, r map[string]any) {
		destinations := make([]any, 26)
		for i := range destinations {
			destinations[i] = r["destinations"].([]any)[0]
		}
		r["destinations"] = destinations
	})
	add("101 filters", "repository-filters must contain", true, func(c, r map[string]any) {
		filters := make([]any, 101)
		for i := range filters {
			filters[i] = r["repository-filters"].([]any)[0]
		}
		r["repository-filters"] = filters
	})
	for _, field := range []string{"region", "registry-id"} {
		for _, mode := range []string{"null", "missing", "syntax"} {
			add(mode+" "+field, field, mode == "syntax", func(c, r map[string]any) {
				destination := r["destinations"].([]any)[0].(map[string]any)
				switch mode {
				case "null":
					destination[field] = nil
				case "missing":
					delete(destination, field)
				case "syntax":
					destination[field] = "INVALID"
				}
			})
		}
	}
	for _, field := range []string{"filter", "filter-type"} {
		for _, mode := range []string{"null", "missing"} {
			add(mode+" "+field, field, false, func(c, r map[string]any) {
				filter := r["repository-filters"].([]any)[0].(map[string]any)
				if mode == "null" {
					filter[field] = nil
				} else {
					delete(filter, field)
				}
			})
		}
	}
	for _, value := range []string{"", "prefix_match", "FUTURE"} {
		add("enum "+value, "filter-type must be PREFIX_MATCH", true, func(c, r map[string]any) {
			r["repository-filters"].([]any)[0].(map[string]any)["filter-type"] = value
		})
	}
	return cases
}

func requireReplicationDiagnostic(t *testing.T, err error, tc replicationInvalidCase) {
	t.Helper()
	require.ErrorContains(t, err, "resource.replication")
	require.ErrorContains(t, err, tc.diagnostic)
	if tc.native {
		require.ErrorContains(t, err, "constraints[")
	}
}

func TestReplicationNativeInvalidSavedNoOpAndHistoricalDestroy(t *testing.T) {
	api := replicationEndpoint(t)
	executor := replicationExecutor(t, "evaluated")
	executor.Inputs = replicationInputs(api)
	_, err := executor.ApplyPlan(t.Context(), savedReplicationPlan(t, executor))
	require.NoError(t, err)
	api.takeCalls()
	for _, tc := range replicationInvalidInputs() {
		t.Run(tc.name, func(t *testing.T) {
			executor.Inputs = replicationInputs(api)
			saved := savedReplicationPlan(t, executor)
			require.Equal(t, runtime.DecisionNoOp, replicationStep(t, saved).Decision)
			api.takeCalls()
			executor.Inputs["configuration"] = tc.value
			_, err := executor.Plan(t.Context())
			requireReplicationDiagnostic(t, err, tc)
			require.Empty(t, api.mutations())
			_, err = executor.ApplyPlan(t.Context(), saved)
			requireReplicationDiagnostic(t, err, tc)
			require.Empty(t, api.mutations())
		})
	}
	executor.Inputs = replicationInputs(api)
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	snapshot.Find("resource.replication").Inputs["replication-configuration"] =
		replicationInvalidInputs()[10].value
	revision, err := executor.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(revision))
	executor.Destroy = true
	_, err = executor.ApplyPlan(t.Context(), savedReplicationPlan(t, executor))
	require.NoError(t, err)
	require.Equal(t, []replicationCall{replicationClear("us-east-1")}, api.mutations())
}

func TestReplicationNativeDeferred(t *testing.T) {
	for _, stage := range []string{"create", "unchanged", "update", "replace"} {
		t.Run(stage, func(t *testing.T) {
			api := replicationEndpoint(t)
			executor := replicationExecutor(t, "deferred")
			executor.Inputs = replicationInputs(api)
			if stage != "create" {
				_, err := executor.ApplyPlan(t.Context(), savedReplicationPlan(t, executor))
				require.NoError(t, err)
				api.takeCalls()
			}
			cases := append(replicationInvalidInputs(), replicationInvalidCase{name: "valid"})
			for index, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					executor.Inputs = replicationInputs(api)
					executor.Inputs["trigger"] = fmt.Sprintf("changed-%d", index)
					if stage == "replace" {
						executor.Inputs["region"] = "us-gov-west-1"
					}
					if stage == "update" {
						executor.Inputs["configuration"] = replicationConfiguration("us-west-2", false)
					}
					if tc.value != nil {
						executor.Inputs["configuration"] = tc.value
					}
					saved := savedReplicationPlan(t, executor)
					require.Equal(t, map[string][]string{
						"replication-configuration": {"action.seed.value"},
					}, replicationStep(t, saved).UnresolvedInputs)
					api.takeCalls()
					executor.Inputs = nil
					_, err := executor.ApplyPlan(t.Context(), saved)
					if tc.value != nil {
						requireReplicationDiagnostic(t, err, tc)
						require.Empty(t, api.mutations())
						return
					}
					require.NoError(t, err)
					var want []replicationCall
					switch stage {
					case "create":
						want = []replicationCall{replicationPut("us-west-1", true, "us-east-1")}
					case "update":
						want = []replicationCall{replicationPut("us-west-2", false, "us-east-1")}
					case "replace":
						want = []replicationCall{replicationClear("us-east-1"),
							replicationPut("us-west-1", true, "us-gov-west-1")}
					}
					require.Equal(t, want, api.mutations())
				})
			}
		})
	}
}

func TestReplicationHTTPIdentityAndDelete(t *testing.T) {
	api := replicationEndpoint(t)
	cfg := &awscfg.Configuration{Region: aws.String("us-east-1"), EndpointURL: aws.String(api.url),
		MaxAttempts: aws.Int64(1)}
	resource := svc.ReplicationConfigurationResource{}
	output := &svc.ReplicationConfigurationResourceOutput{
		RegistryID: "123456789012", Region: "us-east-1",
	}
	prior := runtime.Prior[svc.ReplicationConfigurationResource,
		*svc.ReplicationConfigurationResourceOutput, *awscfg.Configuration]{
		Outputs: output, Configuration: cfg,
	}
	ambient := &awscfg.Configuration{Region: aws.String("us-west-2"),
		EndpointURL: aws.String("http://invalid.test")}
	require.NoError(t, resource.Delete(t.Context(), ambient, prior))
	require.Equal(t, []replicationCall{{"GetCallerIdentity", nil, "us-east-1"},
		replicationClear("us-east-1")}, api.takeCalls())
	api.failPut = true
	require.ErrorContains(t, resource.Delete(t.Context(), ambient, prior), "put failed")
	require.Equal(t, []replicationCall{{"GetCallerIdentity", nil, "us-east-1"},
		replicationClear("us-east-1")}, api.takeCalls())
	api.failPut = false
	api.account = "999999999999"
	for _, operation := range []string{"read", "update", "delete"} {
		var err error
		switch operation {
		case "read":
			_, err = resource.Read(t.Context(), ambient, prior)
		case "update":
			_, err = resource.Update(t.Context(), ambient, prior)
		case "delete":
			err = resource.Delete(t.Context(), ambient, prior)
		}
		require.ErrorContains(t, err, "caller account differs")
		require.Equal(t, []replicationCall{{"GetCallerIdentity", nil, "us-east-1"}}, api.takeCalls())
	}
	api.account = ""
	_, err := resource.Create(t.Context(), cfg)
	require.ErrorContains(t, err, "returned no account")
	require.Equal(t, []replicationCall{{"GetCallerIdentity", nil, "us-east-1"}}, api.takeCalls())
	api.account = "123456789012"
	_, err = resource.Create(t.Context(), cfg)
	require.ErrorContains(t, err, "ReplicationConfiguration")
	require.Equal(t, []replicationCall{{"GetCallerIdentity", nil, "us-east-1"},
		{"DescribeRegistry", map[string]any{}, "us-east-1"}}, api.takeCalls())
	prior.Configuration.Region = nil
	require.NoError(t, resource.Delete(t.Context(), ambient, prior))
	require.Equal(t, []replicationCall{{"GetCallerIdentity", nil, "us-east-1"},
		replicationClear("us-east-1")}, api.takeCalls())
}

func TestReplicationNativeFailedCreateAndUpdateOwnership(t *testing.T) {
	api := replicationEndpoint(t)
	executor := replicationExecutor(t, "evaluated")
	executor.Inputs = replicationInputs(api)
	saved := savedReplicationPlan(t, executor)
	api.failAfterPut = true
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "read failed")
	require.Equal(t, []replicationCall{replicationPut("us-west-1", true, "us-east-1"),
		replicationClear("us-east-1")}, api.mutations())
	snapshot, err := executor.Store.Current()
	if err == nil {
		require.Nil(t, snapshot.Find("resource.replication"))
	} else {
		require.ErrorContains(t, err, "no current snapshot")
	}
	api.failAfterPut, api.failRead = false, false
	_, err = executor.ApplyPlan(t.Context(), savedReplicationPlan(t, executor))
	require.NoError(t, err)
	api.takeCalls()
	snapshot, err = executor.Store.Current()
	require.NoError(t, err)
	prior := snapshot.Find("resource.replication").Outputs
	executor.Inputs["configuration"] = replicationConfiguration("us-west-2", false)
	saved = savedReplicationPlan(t, executor)
	api.failAfterPut = true
	_, err = executor.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "read failed")
	require.Equal(t, []replicationCall{replicationPut("us-west-2", false, "us-east-1")},
		api.mutations())
	snapshot, err = executor.Store.Current()
	require.NoError(t, err)
	require.Equal(t, prior, snapshot.Find("resource.replication").Outputs)
	api.failAfterPut, api.failRead = false, false
	executor.Destroy = true
	_, err = executor.ApplyPlan(t.Context(), savedReplicationPlan(t, executor))
	require.NoError(t, err)
	require.Equal(t, []replicationCall{replicationClear("us-east-1")}, api.mutations())
}

func TestReplicationKnownInvalidBesidePending(t *testing.T) {
	api := replicationEndpoint(t)
	executor := replicationExecutor(t, "partial")
	executor.Inputs = replicationInputs(api)
	configuration := replicationConfiguration("us-west-1", false)
	rule := configuration["rules"].([]any)[0].(map[string]any)
	destination := rule["destinations"].([]any)[0].(map[string]any)
	destination["registry-id"] = "INVALID"
	executor.Inputs["configuration"] = configuration
	_, err := executor.Plan(t.Context())
	require.ErrorContains(t, err, "resource.replication")
	require.ErrorContains(t, err, "constraints[")
	require.ErrorContains(t, err, "registry-id must contain 12 digits")
	require.Empty(t, api.takeCalls())
}

func TestReplicationNativeBoundsAndOmission(t *testing.T) {
	api := replicationEndpoint(t)
	executor := replicationExecutor(t, "evaluated")
	executor.Inputs = replicationInputs(api)
	maximum := replicationConfiguration("eusc-de-east-1", true)
	rule := maximum["rules"].([]any)[0].(map[string]any)
	destinations := make([]any, 25)
	for i := range destinations {
		destinations[i] = rule["destinations"].([]any)[0]
	}
	filters := make([]any, 100)
	for i := range filters {
		filters[i] = rule["repository-filters"].([]any)[0]
	}
	rule["destinations"], rule["repository-filters"] = destinations, filters
	rules := make([]any, 10)
	for i := range rules {
		rules[i] = rule
	}
	maximum["rules"] = rules
	executor.Inputs["configuration"] = maximum
	require.Equal(t, runtime.DecisionCreate,
		replicationStep(t, savedReplicationPlan(t, executor)).Decision)
	require.Empty(t, api.takeCalls())
	for _, empty := range []any{nil, []any{}} {
		configuration := replicationConfiguration("zz-future-99", false)
		configuration["rules"].([]any)[0].(map[string]any)["repository-filters"] = empty
		executor.Inputs["configuration"] = configuration
		require.Equal(t, runtime.DecisionCreate,
			replicationStep(t, savedReplicationPlan(t, executor)).Decision)
		require.Empty(t, api.takeCalls())
	}
	executor.Inputs["configuration"] = nil
	saved := savedReplicationPlan(t, executor)
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "ReplicationConfiguration")
	require.Equal(t, []replicationCall{{"GetCallerIdentity", nil, "us-east-1"},
		{"DescribeRegistry", map[string]any{}, "us-east-1"}}, api.takeCalls())
}

func TestReplicationSTSEndpointAndLookupFailure(t *testing.T) {
	ecrAPI, stsAPI := replicationEndpoint(t), replicationEndpoint(t)
	resource := svc.ReplicationConfigurationResource{}
	cfg := &awscfg.Configuration{Region: aws.String("us-gov-west-1"),
		EndpointURL: aws.String(ecrAPI.url), Endpoints: &awscfg.Endpoints{STS: aws.String(stsAPI.url)},
		MaxAttempts: aws.Int64(1)}
	_, err := resource.Create(t.Context(), cfg)
	require.ErrorContains(t, err, "ReplicationConfiguration")
	require.Equal(t, []replicationCall{{"GetCallerIdentity", nil, "us-gov-west-1"}},
		stsAPI.takeCalls())
	require.Equal(t, []replicationCall{{"DescribeRegistry", map[string]any{}, "us-gov-west-1"}},
		ecrAPI.takeCalls())
	stsAPI.identityError = true
	_, err = resource.Create(t.Context(), cfg)
	require.ErrorContains(t, err, "identity denied")
	require.Empty(t, ecrAPI.takeCalls())
	require.Equal(t, []replicationCall{{"GetCallerIdentity", nil, "us-gov-west-1"}},
		stsAPI.takeCalls())
}
