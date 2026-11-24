package ec2

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const stopAddress = "action.stop"
const stopInstanceID = "i-0123456789abcdef0"

type stopSeed struct {
	Echo any `ub:"echo"`
}

func (r *stopSeed) Run(context.Context, *awscfg.Configuration) (map[string]any, error) {
	return map[string]any{"value": r.Echo}, nil
}

type stopRuntimeRequest struct {
	Form   url.Values
	Region string
}

type stopRuntimeCloud struct {
	mu         sync.Mutex
	history    []stopRuntimeRequest
	state      string
	fail       bool
	beforeStop func()
}

var stopSigningRegion = regexp.MustCompile(`Credential=[^/]+/[^/]+/([^/]+)/ec2/`)

func (c *stopRuntimeCloud) endpoint(t *testing.T) string {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.NoError(t, r.ParseForm()) {
			w.WriteHeader(400)
			return
		}
		region := ""
		if match := stopSigningRegion.FindStringSubmatch(r.Header.Get("Authorization")); match != nil {
			region = match[1]
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.history = append(c.history, stopRuntimeRequest{r.PostForm, region})
		w.Header().Set("Content-Type", "text/xml")
		switch r.PostForm.Get("Action") {
		case "RunInstances":
			c.state = "running"
			fmt.Fprintf(w, `<RunInstancesResponse><instancesSet><item><instanceId>%s</instanceId>`+
				`</item></instancesSet></RunInstancesResponse>`, stopInstanceID)
		case "TerminateInstances":
			c.state = "terminated"
			fmt.Fprint(w, `<TerminateInstancesResponse/>`)
		case "DescribeInstances":
			if c.fail && c.state == "stopped" {
				w.WriteHeader(400)
				fmt.Fprint(w, `<Response><Errors><Error><Code>AccessDenied</Code>`+
					`<Message>wait failed</Message></Error></Errors></Response>`)
				return
			}
			fmt.Fprintf(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item>`+
				`<instanceId>%s</instanceId><instanceState><name>%s</name></instanceState>`+
				`</item></instancesSet></item></reservationSet></DescribeInstancesResponse>`,
				r.PostForm.Get("InstanceId.1"), c.state)
		case "StopInstances":
			if c.beforeStop != nil {
				c.beforeStop()
			}
			c.state = "stopped"
			fmt.Fprint(w, `<StopInstancesResponse/>`)
		default:
			t.Errorf("unexpected operation %s", r.PostForm.Get("Action"))
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestStopInstanceManagedFailureCleanup(t *testing.T) {
	schema := readLibrarySchema(t)
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			cloud := &stopRuntimeCloud{fail: fail}
			executor, store := stopExecutor(t, "managed", schema)
			cloud.beforeStop = func() {
				snapshot, err := store.Current()
				if assert.NoError(t, err) {
					assert.NotNil(t, snapshot.Find("resource.instance"))
					assert.Nil(t, snapshot.Find(stopAddress))
				}
			}
			executor.Inputs = map[string]any{"endpoint": cloud.endpoint(t), "trigger": "initial"}
			events := make(chan runtime.ApplyEvent, 40)
			executor.Events = events
			_, err := executor.ApplyPlan(t.Context(), stopSavedPlan(t, executor))
			if fail {
				require.ErrorContains(t, err, "wait for instance "+stopInstanceID+" to stop")
			} else {
				require.NoError(t, err)
			}
			snapshot, err := store.Current()
			require.NoError(t, err)
			require.NotNil(t, snapshot.Find("resource.instance"))
			assert.Equal(t, stopInstanceID, snapshot.Find("resource.instance").Outputs["instance-id"])
			if fail {
				assert.Nil(t, snapshot.Find(stopAddress))
			} else {
				assert.Empty(t, snapshot.Find(stopAddress).Outputs)
			}
			var stages []runtime.ApplyStage
			for len(events) > 0 {
				if event := <-events; event.Address == stopAddress {
					stages = append(stages, event.Stage)
				}
			}
			wantStages := []runtime.ApplyStage{runtime.StageStart, runtime.StageDone}
			if fail {
				wantStages[1] = runtime.StageFail
			}
			assert.Equal(t, wantStages, stages)
			executor.Events = nil
			requests := cloud.requests()
			require.NotEmpty(t, requests)
			create := requests[0]
			assert.NotEmpty(t, create.Form.Get("ClientToken"))
			create.Form.Del("ClientToken")
			assert.Equal(t, stopRuntimeRequest{url.Values{
				"Action": {"RunInstances"}, "Version": {"2016-11-15"},
				"ImageId": {"ami-0123456789abcdef0"}, "InstanceType": {"t3.micro"},
				"MinCount": {"1"}, "MaxCount": {"1"},
			}, "us-east-1"}, create)
			describe := stopExpectedRequest("DescribeInstances", stopInstanceID, "", "us-east-1")
			want := []stopRuntimeRequest{
				create, describe, describe, describe,
				stopExpectedRequest("StopInstances", stopInstanceID, "false", "us-east-1"), describe,
			}
			if !fail {
				want = append(want, describe)
			}
			assert.Equal(t, want, requests)
			cloud.fail = false
			if !fail {
				skip := stopSavedPlan(t, executor)
				require.Equal(t, runtime.DecisionSkip, stopPlanStep(t, skip).Decision)
				_, err = executor.ApplyPlan(t.Context(), skip)
				require.NoError(t, err)
				assert.Equal(t, []stopRuntimeRequest{describe}, cloud.requests())
				executor.Inputs["trigger"] = "updated"
				rerun := stopSavedPlan(t, executor)
				require.Equal(t, runtime.DecisionRerun, stopPlanStep(t, rerun).Decision)
				_, err = executor.ApplyPlan(t.Context(), rerun)
				require.NoError(t, err)
				assert.Equal(t, []stopRuntimeRequest{describe, describe, describe}, cloud.requests())
			}
			executor.Destroy = true
			_, err = executor.ApplyPlan(t.Context(), stopSavedPlan(t, executor))
			require.NoError(t, err)
			assert.Equal(t, []stopRuntimeRequest{
				describe, describe,
				stopExpectedRequest("TerminateInstances", stopInstanceID, "", "us-east-1"), describe,
			}, cloud.requests())
			assert.Equal(t, "terminated", cloud.state)
			snapshot, err = store.Current()
			require.NoError(t, err)
			assert.Empty(t, snapshot.Entries)
		})
	}
}

func TestStopInstanceCompiledCLI(t *testing.T) {
	module, unobin := libraryModuleRoot(t), unobinModuleRoot(t)
	for _, fixture := range []string{"settled", "cli-deferred", "scenario"} {
		t.Run(fixture, func(t *testing.T) {
			out := t.TempDir()
			path := stopFixture(fixture)
			if fixture == "scenario" {
				path = "../../tests/integration/scenarios/ec2-stop-instance/factory.ub"
			}
			result, err := compile.RunResult(compile.Options{
				FactoryPath: path, OutDir: out, Build: true,
				GoVersion: "1.26.2", CLIVersion: "v0.13.0", ReplaceUnobin: unobin.Dir,
				ReplaceGoModules: map[string]string{module.Path: module.Dir},
				NewResolver: func(directory string) (resolve.Resolver, error) {
					return resolve.NewLocalResolver(directory), nil
				}, Stdout: io.Discard, Stderr: io.Discard,
			})
			require.NoError(t, err)
			if fixture == "scenario" {
				return
			}
			cloud := &stopRuntimeCloud{state: "running"}
			endpoint := cloud.endpoint(t)
			template, err := os.ReadFile(filepath.Join(filepath.Dir(stopFixture(fixture)), "config.ub"))
			require.NoError(t, err)
			configPath, planPath := filepath.Join(out, "config.ub"), filepath.Join(out, "saved.plan")
			config := strings.ReplaceAll(string(template), "ENDPOINT", endpoint)
			writeConfig := func(body string) {
				require.NoError(t, os.WriteFile(configPath, []byte(body), 0o600))
			}
			run := func(args ...string) (string, error) {
				cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
				cmd.Dir = out
				output, err := cmd.CombinedOutput()
				return string(output), err
			}
			writeConfig(config)
			planArgs := []string{"plan", "--config", configPath, "--out", planPath,
				"--allow-version-mismatch"}
			output, err := run(planArgs...)
			require.NoError(t, err, output)
			open := func() *runtime.PlanFile {
				encoded, err := os.ReadFile(planPath)
				require.NoError(t, err)
				plan, err := runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
					return encrypters.Noop{}, nil
				})
				require.NoError(t, err)
				return plan
			}
			plan := open()
			step := stopPlanStep(t, plan)
			require.Equal(t, runtime.DecisionRerun, step.Decision)
			assert.Empty(t, cloud.requests())
			applyArgs := []string{"apply", planPath}
			output, err = run(applyArgs...)
			if fixture == "cli-deferred" {
				assert.Equal(t, map[string][]string{"instance-id": {"action.seed.value"}},
					step.UnresolvedInputs)
				require.Error(t, err, output)
				assert.Contains(t, output, "constraints[1] (predicate): instance-id must match")
				assert.Empty(t, cloud.requests())
				return
			}
			require.NoError(t, err, output)
			assert.Equal(t, map[string]any{
				"instance-id": stopInstanceID, "force": nil, "timeout": nil, "region": nil,
			}, step.Inputs)
			assert.Equal(t, []stopRuntimeRequest{
				stopExpectedRequest("DescribeInstances", stopInstanceID, "", "us-east-1"),
				stopExpectedRequest("StopInstances", stopInstanceID, "false", "us-east-1"),
				stopExpectedRequest("DescribeInstances", stopInstanceID, "", "us-east-1"),
			}, cloud.requests())
			output, err = run(planArgs...)
			require.NoError(t, err, output)
			require.Equal(t, runtime.DecisionSkip, stopPlanStep(t, open()).Decision)
			output, err = run(applyArgs...)
			require.NoError(t, err, output)
			assert.Empty(t, cloud.requests())
			writeConfig(strings.ReplaceAll(config, stopInstanceID, "invalid"))
			output, err = run(planArgs...)
			require.Error(t, err, output)
			assert.Contains(t, output, "constraints[1] (predicate): instance-id must match")
			assert.Empty(t, cloud.requests())
		})
	}
}

func (c *stopRuntimeCloud) requests() []stopRuntimeRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.history
	c.history = nil
	return out
}

func stopExecutor(t *testing.T, fixture string, schema *runtime.LibrarySchema) (
	*runtime.Executor, *local.Store,
) {
	t.Helper()
	lib := runtime.LibraryWithPath(Library(),
		"github.com/cloudboss/unobin-library-aws//service/ec2")
	for _, types := range []map[string]*runtime.TypeSchema{
		schema.Resources, schema.DataSources, schema.Actions,
	} {
		for _, fields := range types {
			fields.EvaluatedInputs = fields.Inputs
		}
	}
	lib.Schema = schema
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"action.stop-instance": schema.Actions["stop-instance"].Constraints,
	}
	libs := map[string]*runtime.Library{
		"ec2": lib,
		"test": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[stopSeed, map[string]any, *awscfg.Configuration](),
		}},
	}
	path := stopFixture(fixture)
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	store, err := local.NewStore(t.TempDir(), "test-stop", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "test-stop", Version: "v0", ContentRevision: "test"},
	}, store
}

func stopSavedPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	sealed, err := runtime.SealPlan(plan, encrypters.Noop{})
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(sealed, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	return saved
}

func stopPlanStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == stopAddress {
			return step
		}
	}
	t.Fatal("stop action absent from plan")
	return runtime.PlanStep{}
}

func stopExpectedRequest(operation, id, force, region string) stopRuntimeRequest {
	form := url.Values{"Action": {operation}, "Version": {"2016-11-15"}, "InstanceId.1": {id}}
	if force != "" {
		form.Set("Force", force)
	}
	return stopRuntimeRequest{form, region}
}

func stopValidInputs(endpoint string) map[string]any {
	return map[string]any{
		"endpoint": endpoint, "id": stopInstanceID, "force": nil,
		"timeout": nil, "region": nil, "trigger": "fixed",
	}
}

func stopInvalidInputs() []struct {
	name, field string
	value       any
	rule        string
} {
	return []struct {
		name, field string
		value       any
		rule        string
	}{
		{"null id", "id", nil, `field "instance-id": required but is null`},
		{"short id", "id", "i-1234567", "schema: constraints[1] (predicate): instance-id must match"},
		{"long id", "id", "i-0123456789abcdef01", "schema: constraints[1] (predicate): instance-id"},
		{"upper id", "id", "i-ABCDEF01", "schema: constraints[1] (predicate): instance-id"},
		{"space id", "id", "i-abcdef01 ", "schema: constraints[1] (predicate): instance-id"},
		{"empty id", "id", "", "schema: constraints[1] (predicate): instance-id"},
		{"wrong prefix", "id", "x-abcdef01", "schema: constraints[1] (predicate): instance-id"},
		{"small timeout", "timeout", int64(29), "schema: constraints[2] (predicate): timeout must be"},
		{"large timeout", "timeout", int64(3601), "schema: constraints[2] (predicate): timeout must be"},
	}
}

func TestStopInstanceEvaluatedStructuralNull(t *testing.T) {
	cloud := &stopRuntimeCloud{state: "running"}
	executor, _ := stopExecutor(t, "settled", readLibrarySchema(t))
	executor.Inputs = stopValidInputs(cloud.endpoint(t))
	executor.Inputs["id"] = nil
	_, err := executor.Plan(t.Context())
	require.ErrorContains(t, err, `action.stop: field "instance-id": required but is null`)
	assert.Empty(t, cloud.requests())
}

func TestStopInstanceNativeNullPredicate(t *testing.T) {
	for _, fixture := range []string{"settled", "deferred"} {
		t.Run(fixture, func(t *testing.T) {
			schema := readLibrarySchema(t)
			fields := schema.Actions["stop-instance"].Inputs
			// Nullable metadata isolates the declared predicate from structural null checks.
			fields["instance-id"] = typecheck.TOptional(fields["instance-id"])
			cloud := &stopRuntimeCloud{state: "running"}
			executor, _ := stopExecutor(t, fixture, schema)
			executor.Inputs = stopValidInputs(cloud.endpoint(t))
			executor.Inputs["id"] = nil
			const diagnostic = "action.stop: schema: constraints[0] (predicate): instance-id is required"
			if fixture == "settled" {
				_, err := executor.Plan(t.Context())
				require.ErrorContains(t, err, diagnostic)
			} else {
				plan := stopSavedPlan(t, executor)
				assert.Equal(t, map[string][]string{
					"instance-id": {"action.id.value"}, "timeout": {"action.timeout.value"},
				}, stopPlanStep(t, plan).UnresolvedInputs)
				executor.Inputs = nil
				_, err := executor.ApplyPlan(t.Context(), plan)
				require.ErrorContains(t, err, diagnostic)
			}
			assert.Empty(t, cloud.requests())
		})
	}
}

func TestStopInstanceEvaluatedAndSavedSkip(t *testing.T) {
	cloud := &stopRuntimeCloud{state: "running"}
	endpoint := cloud.endpoint(t)
	executor, store := stopExecutor(t, "settled", readLibrarySchema(t))
	valid := stopValidInputs(endpoint)
	executor.Inputs = valid
	events := make(chan runtime.ApplyEvent, 20)
	executor.Events = events
	first := stopSavedPlan(t, executor)
	require.Equal(t, runtime.DecisionRerun, stopPlanStep(t, first).Decision)
	executor.Inputs = nil
	result, err := executor.ApplyPlan(t.Context(), first)
	require.NoError(t, err)
	assert.Empty(t, result.Outputs)
	assert.Equal(t, []stopRuntimeRequest{
		stopExpectedRequest("DescribeInstances", stopInstanceID, "", "us-east-1"),
		stopExpectedRequest("StopInstances", stopInstanceID, "false", "us-east-1"),
		stopExpectedRequest("DescribeInstances", stopInstanceID, "", "us-east-1"),
	}, cloud.requests())
	var stages []runtime.ApplyStage
	for len(events) > 0 {
		if event := <-events; event.Address == stopAddress {
			stages = append(stages, event.Stage)
		}
	}
	assert.Equal(t, []runtime.ApplyStage{runtime.StageStart, runtime.StageDone}, stages)
	executor.Events = nil
	executor.Inputs = maps.Clone(valid)
	skip := stopSavedPlan(t, executor)
	require.Equal(t, runtime.DecisionSkip, stopPlanStep(t, skip).Decision)
	executor.Inputs = nil
	_, err = executor.ApplyPlan(t.Context(), skip)
	require.NoError(t, err)
	assert.Empty(t, cloud.requests())
	for _, test := range stopInvalidInputs() {
		t.Run(test.name, func(t *testing.T) {
			executor.Inputs = maps.Clone(valid)
			saved := stopSavedPlan(t, executor)
			require.Equal(t, runtime.DecisionSkip, stopPlanStep(t, saved).Decision)
			executor.Inputs[test.field] = test.value
			_, err := executor.Plan(t.Context())
			require.ErrorContains(t, err, stopAddress+": "+test.rule)
			_, err = executor.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, stopAddress+": "+test.rule)
			assert.Empty(t, cloud.requests())
		})
	}
	executor.Inputs = maps.Clone(valid)
	executor.Inputs["trigger"] = "rerun"
	plan := stopSavedPlan(t, executor)
	require.Equal(t, runtime.DecisionRerun, stopPlanStep(t, plan).Decision)
	_, err = executor.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, []stopRuntimeRequest{
		stopExpectedRequest("DescribeInstances", stopInstanceID, "", "us-east-1"),
	}, cloud.requests())
	snapshot, err := store.Current()
	require.NoError(t, err)
	assert.Empty(t, snapshot.Find(stopAddress).Outputs)
	snapshot.Find(stopAddress).Inputs["instance-id"] = nil
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	executor.Destroy = true
	executor.Inputs["id"] = "historical-invalid"
	destroy := stopSavedPlan(t, executor)
	require.Equal(t, runtime.DecisionDestroy, stopPlanStep(t, destroy).Decision)
	_, err = executor.ApplyPlan(t.Context(), destroy)
	require.NoError(t, err)
	assert.Empty(t, cloud.requests())
	snapshot, err = store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find(stopAddress))
}

func TestStopInstanceDeferredSavedPlan(t *testing.T) {
	schema := readLibrarySchema(t)
	cases := append(stopInvalidInputs(), struct {
		name, field string
		value       any
		rule        string
	}{name: "valid"})
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cloud := &stopRuntimeCloud{state: "running"}
			executor, _ := stopExecutor(t, "deferred", schema)
			executor.Inputs = stopValidInputs(cloud.endpoint(t))
			if test.field != "" {
				executor.Inputs[test.field] = test.value
			}
			saved := stopSavedPlan(t, executor)
			assert.Equal(t, map[string][]string{
				"instance-id": {"action.id.value"}, "timeout": {"action.timeout.value"},
			}, stopPlanStep(t, saved).UnresolvedInputs)
			assert.Empty(t, cloud.requests())
			executor.Inputs = nil
			_, err := executor.ApplyPlan(t.Context(), saved)
			if test.rule != "" {
				require.ErrorContains(t, err, stopAddress+": "+test.rule)
				assert.Empty(t, cloud.requests())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []stopRuntimeRequest{
				stopExpectedRequest("DescribeInstances", stopInstanceID, "", "us-east-1"),
				stopExpectedRequest("StopInstances", stopInstanceID, "false", "us-east-1"),
				stopExpectedRequest("DescribeInstances", stopInstanceID, "", "us-east-1"),
			}, cloud.requests())
		})
	}
}

func TestStopInstanceRegionAndBoundaryRequests(t *testing.T) {
	schema := readLibrarySchema(t)
	for _, region := range []any{nil, "", "us-west-2", "unknown-region", "us-gov-west-1",
		"cn-north-1"} {
		for _, force := range []any{nil, false, true} {
			t.Run(fmt.Sprint(region, force), func(t *testing.T) {
				cloud := &stopRuntimeCloud{state: "running"}
				executor, _ := stopExecutor(t, "settled", schema)
				executor.Inputs = stopValidInputs(cloud.endpoint(t))
				executor.Inputs["region"], executor.Inputs["force"] = region, force
				executor.Inputs["timeout"] = int64(30)
				_, err := executor.ApplyPlan(t.Context(), stopSavedPlan(t, executor))
				if region == "us-gov-west-1" || region == "cn-north-1" {
					require.ErrorContains(t, err, "outside configured partition")
					assert.Empty(t, cloud.requests())
					return
				}
				require.NoError(t, err)
				signing, forceValue := "us-east-1", "false"
				if region == "us-west-2" {
					signing = "us-west-2"
				}
				if region == "unknown-region" {
					signing = "unknown-region"
				}
				if force == true {
					forceValue = "true"
				}
				assert.Equal(t, []stopRuntimeRequest{
					stopExpectedRequest("DescribeInstances", stopInstanceID, "", signing),
					stopExpectedRequest("StopInstances", stopInstanceID, forceValue, signing),
					stopExpectedRequest("DescribeInstances", stopInstanceID, "", signing),
				}, cloud.requests())
			})
		}
	}
	for n := 8; n <= 17; n++ {
		cloud := &stopRuntimeCloud{state: "stopped"}
		executor, _ := stopExecutor(t, "settled", schema)
		id := "i-" + strings.Repeat("a", n)
		executor.Inputs = stopValidInputs(cloud.endpoint(t))
		executor.Inputs["id"], executor.Inputs["timeout"] = id, int64(3600)
		_, err := executor.ApplyPlan(t.Context(), stopSavedPlan(t, executor))
		require.NoError(t, err)
		assert.Equal(t, []stopRuntimeRequest{
			stopExpectedRequest("DescribeInstances", id, "", "us-east-1"),
		}, cloud.requests())
	}
}

func TestStopInstanceLiteralRequests(t *testing.T) {
	cloud := &stopRuntimeCloud{state: "running"}
	executor, _ := stopExecutor(t, "literal-run", readLibrarySchema(t))
	executor.Inputs = map[string]any{"endpoint": cloud.endpoint(t)}
	saved := stopSavedPlan(t, executor)
	executor.Inputs = nil
	result, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Empty(t, result.Outputs)
	assert.Equal(t, []stopRuntimeRequest{
		stopExpectedRequest("DescribeInstances", stopInstanceID, "", "us-east-1"),
		stopExpectedRequest("StopInstances", stopInstanceID, "false", "us-east-1"),
		stopExpectedRequest("DescribeInstances", stopInstanceID, "", "us-east-1"),
	}, cloud.requests())
}
