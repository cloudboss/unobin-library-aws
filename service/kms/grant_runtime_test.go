package kms

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/kms"
)

type grantSeed struct {
	Value map[string]any `ub:"value"`
}

type grantSeedOutput struct {
	KeyID       any `ub:"key-id"`
	Grantee     any `ub:"grantee-principal"`
	Operations  any `ub:"operations"`
	Constraints any `ub:"constraints"`
	Tokens      any `ub:"grant-tokens"`
	Name        any `ub:"name"`
	Retiring    any `ub:"retiring-principal"`
	Retire      any `ub:"retire-on-delete"`
}

func (s *grantSeed) Run(context.Context, any) (*grantSeedOutput, error) {
	return &grantSeedOutput{KeyID: s.Value["key-id"], Grantee: s.Value["grantee-principal"],
		Operations: s.Value["operations"], Constraints: s.Value["constraints"],
		Tokens: s.Value["grant-tokens"], Name: s.Value["name"],
		Retiring: s.Value["retiring-principal"], Retire: s.Value["retire-on-delete"]}, nil
}

type grantCall struct {
	Target string
	Body   map[string]any
}
type grantAPI struct {
	endpoint   string
	mu         sync.Mutex
	calls      []grantCall
	grants     map[string]map[string]any
	nextID     int
	failRead   bool
	failDelete bool
	onRead     func(context.Context)
}

func newGrantAPI(t *testing.T) *grantAPI {
	t.Helper()
	temp := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(temp, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(temp, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "fake")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake")
	api := &grantAPI{grants: make(map[string]map[string]any)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		api.mu.Lock()
		defer api.mu.Unlock()
		target := r.Header.Get("X-Amz-Target")
		api.calls = append(api.calls, grantCall{target, body})
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		fail := func() { w.WriteHeader(400); fmt.Fprint(w, `{"__type":"AccessDeniedException"}`) }
		switch target {
		case "TrentService.CreateGrant":
			api.nextID++
			id := fmt.Sprintf("grant-%d", api.nextID)
			entry := maps.Clone(body)
			entry["GrantId"] = id
			delete(entry, "KeyId")
			delete(entry, "GrantTokens")
			api.grants[id] = entry
			fmt.Fprintf(w, `{"GrantId":%q,"GrantToken":"secret"}`, id)

		case "TrentService.ListGrants":
			if api.failRead {
				api.failRead = false
				if api.onRead != nil {
					api.onRead(r.Context())
					return
				}
				fail()
				return
			}
			grants := make([]map[string]any, 0, len(api.grants))
			for _, entry := range api.grants {
				grants = append(grants, entry)
			}
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Grants": grants}))
		case "TrentService.RevokeGrant", "TrentService.RetireGrant":
			if api.failDelete {
				fail()
				return
			}
			delete(api.grants, body["GrantId"].(string))
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected operation %s", target)
			fail()
		}
	}))
	t.Cleanup(server.Close)
	api.endpoint = server.URL
	return api
}

func (api *grantAPI) takeCalls() []grantCall {
	api.mu.Lock()
	defer api.mu.Unlock()
	calls := api.calls
	api.calls = nil
	return calls
}

func grantLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.Schema = readLibrarySchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.grant": lib.Schema.Resources["grant"].Constraints,
	}
	lib.Defaults = map[string][]lang.DefaultSpec{
		"resource.grant": lib.Schema.Resources["grant"].Defaults,
	}
	return map[string]*runtime.Library{"kms": lib, "test": {
		Actions: map[string]runtime.ActionRegistration{"seed": runtime.MakeAction[grantSeed,
			*grantSeedOutput, any]()},
	}}
}

func grantInputs() map[string]any {
	return map[string]any{"key-id": "arn:aws:kms:us-east-1:123456789012:key/original",
		"grantee-principal": "lambda.amazonaws.com", "operations": []any{"Encrypt", "Decrypt"},
		"constraints": nil, "grant-tokens": []any{"b", "a", "b"}, "name": nil,
		"retiring-principal": nil, "retire-on-delete": false}
}

func grantExecutor(t *testing.T, fixture string, api *grantAPI,
	libs map[string]*runtime.Library, store *local.Store, inputs map[string]any,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "grant", "runtime", fixture, "factory.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	body := &f.Factory.Body
	return &runtime.Executor{DAG: runtime.BuildSyntaxDAG(*body, libs), SyntaxSource: body,
		Libraries: libs, Store: store, Inputs: map[string]any{"endpoint": api.endpoint, "grant": inputs}}
}

func initialGrant(t *testing.T, api *grantAPI, libs map[string]*runtime.Library,
	inputs map[string]any,
) (*runtime.Executor, *local.Store) {
	t.Helper()
	store, err := local.NewStore(t.TempDir(), "grant", "test", encrypters.Noop{})
	require.NoError(t, err)
	exec := grantExecutor(t, "resolved", api, libs, store, inputs)
	plan := saveKeyLookupPlan(t, exec)
	step := aliasLookupStep(t, plan, "resource.subject")
	require.Equal(t, runtime.DecisionCreate, step.Decision)
	require.Equal(t, []string{"grant-token"}, step.SensitiveOutputs)
	result, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"grant-id": "grant-1", "token": "secret"}, result.Outputs)
	require.Equal(t, []grantCall{
		{"TrentService.CreateGrant", map[string]any{"KeyId": inputs["key-id"],
			"GranteePrincipal": inputs["grantee-principal"], "Operations": []any{"Decrypt", "Encrypt"},
			"GrantTokens": []any{"a", "b"}}},
		{"TrentService.ListGrants", map[string]any{"KeyId": inputs["key-id"], "Limit": float64(100)}},
	}, api.takeCalls())
	return exec, store
}

func requireGrantNative(t *testing.T, err error, rule int, message string) {
	t.Helper()
	require.ErrorContains(t, err, fmt.Sprintf("constraints[%d] (predicate)", rule))
	require.ErrorContains(t, err, message)
	require.ErrorContains(t, err, "resource.subject")
}

func TestGrantNativeInputStages(t *testing.T) {
	api := newGrantAPI(t)
	libs := grantLibraries(t)
	for _, tc := range []struct {
		name, field string
		value       any
		rule        int
		message     string
	}{
		{"null key", "key-id", nil, 0, "key-id is required"},
		{"null grantee", "grantee-principal", nil, 1, "grantee-principal is required"},
		{"null operations", "operations", nil, 2, "operations is required"},
		{"empty name", "name", "", 3, "name must contain"},
		{"bad name", "name", "!", 3, "name must contain"},
		{"long name", "name", strings.Repeat("a", 257), 4, "at most 256 bytes"},
		{"operation", "operations", []any{"encrypt"}, 5, "invalid grant operation"},
		{"context maps", "constraints", map[string]any{
			"encryption-context-equals": map[string]any{"a": "b"},
			"encryption-context-subset": map[string]any{"c": "d"}}, 6, "cannot both be nonempty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api.grants = make(map[string]map[string]any)
			api.nextID = 0
			exec, store := initialGrant(t, api, libs, grantInputs())
			saved := saveKeyLookupPlan(t, exec)
			require.Equal(t, runtime.DecisionNoOp, aliasLookupStep(t, saved, "resource.subject").Decision)
			api.takeCalls()
			invalid := grantInputs()
			invalid[tc.field] = tc.value
			bad := grantExecutor(t, "resolved", api, libs, store, invalid)
			_, err := bad.Plan(t.Context())
			requireGrantNative(t, err, tc.rule, tc.message)
			for _, call := range api.takeCalls() {
				require.Equal(t, "TrentService.ListGrants", call.Target)
			}
			_, err = bad.ApplyPlan(t.Context(), saved)
			requireGrantNative(t, err, tc.rule, tc.message)
			require.Empty(t, api.takeCalls())
			deferred := grantExecutor(t, "deferred", api, libs, store, invalid)
			pending := saveKeyLookupPlan(t, deferred)
			require.NotEmpty(t, aliasLookupStep(t, pending, "resource.subject").UnresolvedInputs)
			api.takeCalls()
			_, err = deferred.ApplyPlan(t.Context(), pending)
			requireGrantNative(t, err, tc.rule, tc.message)
			require.Empty(t, api.takeCalls())
			snapshot, err := store.Current()
			require.NoError(t, err)
			require.Equal(t, "grant-1", snapshot.Find("resource.subject").Outputs["grant-id"])
		})
	}
}

func TestGrantSetsReplacementsDriftAndHistoricalDestroy(t *testing.T) {
	api := newGrantAPI(t)
	libs := grantLibraries(t)
	_, store := initialGrant(t, api, libs, grantInputs())
	inputs := grantInputs()
	inputs["operations"] = []any{"Decrypt", "Encrypt", "Decrypt"}
	inputs["grant-tokens"] = []any{"a", "b"}
	exec := grantExecutor(t, "resolved", api, libs, store, inputs)
	saved := saveKeyLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp, aliasLookupStep(t, saved, "resource.subject").Decision)
	_, err := exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	require.Equal(t, []grantCall{{"TrentService.ListGrants", map[string]any{
		"KeyId": inputs["key-id"], "Limit": float64(100)}}}, api.takeCalls())
	for field, value := range map[string]any{
		"key-id": "changed", "grantee-principal": "ec2.amazonaws.com",
		"operations": []any{"Sign"}, "grant-tokens": []any{"new"}, "name": "new",
		"constraints": map[string]any{}, "retiring-principal": "ec2.amazonaws.com",
		"retire-on-delete": true,
	} {
		changed := grantInputs()
		changed[field] = value
		plan := saveKeyLookupPlan(t, grantExecutor(t, "resolved", api, libs, store, changed))
		require.Equal(t, runtime.DecisionReplace, aliasLookupStep(t, plan, "resource.subject").Decision,
			field)
		api.takeCalls()
	}
	for field, value := range map[string]any{"Operations": []any{"Sign"},
		"GranteePrincipal": "ec2.amazonaws.com", "Name": "remote",
		"RetiringPrincipal": "ec2.amazonaws.com", "Constraints": map[string]any{
			"EncryptionContextEquals": map[string]any{"k": "v"}},
	} {
		api.grants["grant-1"][field] = value
		plan := saveKeyLookupPlan(t, exec)
		require.Equal(t, runtime.DecisionReplace, aliasLookupStep(t, plan, "resource.subject").Decision,
			field)
		delete(api.grants["grant-1"], field)
		api.grants["grant-1"]["Operations"] = []any{"Decrypt", "Encrypt"}
		api.grants["grant-1"]["GranteePrincipal"] = "lambda.amazonaws.com"
		api.takeCalls()
	}
	for _, retire := range []bool{false, true} {
		api.grants = make(map[string]map[string]any)
		api.nextID = 0
		old := grantInputs()
		old["retire-on-delete"] = retire
		_, oldStore := initialGrant(t, api, libs, old)
		desired := grantInputs()
		desired["operations"] = []any{"Sign"}
		desired["retire-on-delete"] = !retire
		replaced := grantExecutor(t, "deferred", api, libs, oldStore, desired)
		plan := saveKeyLookupPlan(t, replaced)
		api.takeCalls()
		_, err := replaced.ApplyPlan(t.Context(), plan)
		require.NoError(t, err)
		method := "TrentService.RevokeGrant"
		if retire {
			method = "TrentService.RetireGrant"
		}
		calls := api.takeCalls()
		require.Equal(t, []grantCall{
			{"TrentService.ListGrants", map[string]any{"KeyId": old["key-id"], "Limit": float64(100)}},
			{method, map[string]any{"KeyId": old["key-id"], "GrantId": "grant-1"}},
			{"TrentService.ListGrants", map[string]any{"KeyId": old["key-id"], "Limit": float64(100)}},
			{"TrentService.CreateGrant", map[string]any{"KeyId": old["key-id"],
				"GranteePrincipal": "lambda.amazonaws.com", "Operations": []any{"Sign"},
				"GrantTokens": []any{"a", "b"}}},
			{"TrentService.ListGrants", map[string]any{"KeyId": old["key-id"], "Limit": float64(100)}},
		}, calls)
		snapshot, err := oldStore.Current()
		require.NoError(t, err)
		snapshot.Find("resource.subject").Inputs["key-id"] = nil
		snapshot.Find("resource.subject").Inputs["operations"] = nil
		revision, err := oldStore.Write(snapshot)
		require.NoError(t, err)
		require.NoError(t, oldStore.SetCurrent(revision))
		replaced.Destroy = true
		destroy := saveKeyLookupPlan(t, replaced)
		api.takeCalls()
		_, err = replaced.ApplyPlan(t.Context(), destroy)
		require.NoError(t, err)
		method = "TrentService.RevokeGrant"
		if !retire {
			method = "TrentService.RetireGrant"
		}
		require.Equal(t, []grantCall{
			{"TrentService.ListGrants", map[string]any{"KeyId": old["key-id"], "Limit": float64(100)}},
			{method, map[string]any{"KeyId": old["key-id"], "GrantId": "grant-2"}},
			{"TrentService.ListGrants", map[string]any{"KeyId": old["key-id"], "Limit": float64(100)}},
		}, api.takeCalls())
		snapshot, err = oldStore.Current()
		require.NoError(t, err)
		require.Empty(t, snapshot.Entries)
	}
}

func TestGrantParserPreventsReplacementDeletion(t *testing.T) {
	api := newGrantAPI(t)
	libs := grantLibraries(t)
	_, store := initialGrant(t, api, libs, grantInputs())
	for _, field := range []string{"grantee-principal", "retiring-principal"} {
		desired := grantInputs()
		desired[field] = "bad principal"
		exec := grantExecutor(t, "deferred", api, libs, store, desired)
		saved := saveKeyLookupPlan(t, exec)
		api.takeCalls()
		_, err := exec.ApplyPlan(t.Context(), saved)
		require.ErrorContains(t, err, "invalid grant principal")
		require.Empty(t, api.takeCalls())
	}
}

func TestGrantNativeAcceptedCreateFailure(t *testing.T) {
	libs := grantLibraries(t)
	for _, retire := range []bool{false, true} {
		for _, canceled := range []bool{false, true} {
			for _, cleanupFails := range []bool{false, true} {
				t.Run(fmt.Sprint(retire, canceled, cleanupFails), func(t *testing.T) {
					api := newGrantAPI(t)
					api.failRead, api.failDelete = true, cleanupFails
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					if canceled {
						api.onRead = func(requestCtx context.Context) {
							cancel()
							<-requestCtx.Done()
						}
					}
					store, err := local.NewStore(t.TempDir(), "grant", "test", encrypters.Noop{})
					require.NoError(t, err)
					inputs := grantInputs()
					inputs["retire-on-delete"] = retire
					exec := grantExecutor(t, "resolved", api, libs, store, inputs)
					saved := saveKeyLookupPlan(t, exec)
					_, err = exec.ApplyPlan(ctx, saved)
					require.Error(t, err)
					if canceled {
						require.ErrorIs(t, err, context.Canceled)
					} else {
						require.ErrorContains(t, err, "list grants")
					}
					if cleanupFails {
						require.ErrorContains(t, err, "clean up grant after failed create")
					}
					calls := api.takeCalls()
					method := "TrentService.RevokeGrant"
					if retire {
						method = "TrentService.RetireGrant"
					}
					var mutations []grantCall
					for _, call := range calls {
						if call.Target != "TrentService.ListGrants" {
							mutations = append(mutations, call)
						}
					}
					require.Equal(t, []grantCall{
						{"TrentService.CreateGrant", map[string]any{"KeyId": inputs["key-id"],
							"GranteePrincipal": "lambda.amazonaws.com", "Operations": []any{"Decrypt", "Encrypt"},
							"GrantTokens": []any{"a", "b"}}},
						{method, map[string]any{"KeyId": inputs["key-id"], "GrantId": "grant-1"}},
					}, mutations)
					snapshot, readErr := store.Current()
					require.ErrorIs(t, readErr, sdkstate.ErrNoCurrent)
					require.Nil(t, snapshot)
					api.mu.Lock()
					if cleanupFails {
						require.Contains(t, api.grants, "grant-1")
					} else {
						require.Empty(t, api.grants)
					}
					api.mu.Unlock()
				})
			}
		}
	}
}

func TestGrantCompiledSavedPlan(t *testing.T) {
	api := newGrantAPI(t)
	build := t.TempDir()
	opts := keyCompileOptions(t, "evaluated", build)
	opts.FactoryPath = filepath.Join("testdata", "ub", "grant", "compile", "evaluated", "factory.ub")
	opts.StackName, opts.Build, opts.Version = "kms-grant", true, "v0.0.0"
	result, err := compile.RunResult(opts)
	require.NoError(t, err)
	writeStack := func(fixture string) {
		t.Helper()
		path := filepath.Join("testdata", "ub", "grant", "compile", "evaluated", fixture+".ub")
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		text := strings.ReplaceAll(string(body), "__ENDPOINT__", api.endpoint)
		text = strings.ReplaceAll(text, "__REVISION__", result.ContentRevision)
		require.NoError(t, os.WriteFile(filepath.Join(build, "config.ub"), []byte(text), 0o600))
	}
	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
		cmd.Dir = build
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	writeStack("valid")
	out, err := run("plan", "-c", "config.ub", "-o", "saved.ubp")
	require.NoError(t, err, out)
	open := func() *runtime.PlanFile {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(build, "saved.ubp"))
		require.NoError(t, err)
		plan, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
			return encrypters.Noop{}, nil
		})
		require.NoError(t, err)
		return plan
	}
	step := aliasLookupStep(t, open(), "resource.subject")
	require.Equal(t, runtime.DecisionCreate, step.Decision)
	require.Equal(t, []string{"grant-token"}, step.SensitiveOutputs)
	out, err = run("apply", "saved.ubp")
	require.NoError(t, err, out)
	api.takeCalls()
	out, err = run("plan", "-c", "config.ub", "-o", "saved.ubp")
	require.NoError(t, err, out)
	require.Equal(t, runtime.DecisionNoOp, aliasLookupStep(t, open(), "resource.subject").Decision)
	api.takeCalls()
	out, err = run("apply", "saved.ubp")
	require.NoError(t, err, out)
	require.Empty(t, api.takeCalls())
	writeStack("null")
	out, err = run("plan", "-c", "config.ub", "-o", "invalid.ubp")
	require.Error(t, err)
	require.Contains(t, out, "null")
	for _, call := range api.takeCalls() {
		require.Equal(t, "TrentService.ListGrants", call.Target)
	}
}

func TestGrantDeferredUnchangedAndHistoricalInvalidPlan(t *testing.T) {
	api := newGrantAPI(t)
	libs := grantLibraries(t)
	_, store := initialGrant(t, api, libs, grantInputs())
	exec := grantExecutor(t, "deferred", api, libs, store, grantInputs())
	plan := saveKeyLookupPlan(t, exec)
	api.takeCalls()
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	require.Empty(t, api.takeCalls())
	snapshot, err := store.Current()
	require.NoError(t, err)
	entry := snapshot.Find("resource.subject")
	entry.Inputs["name"] = "!"
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	inputs := grantInputs()
	inputs["name"] = "!"
	invalid := grantExecutor(t, "resolved", api, libs, store, inputs)
	_, err = invalid.Plan(t.Context())
	requireGrantNative(t, err, 3, "name must contain")
	for _, call := range api.takeCalls() {
		require.Equal(t, "TrentService.ListGrants", call.Target)
	}
}

func TestGrantUpdateOnlyRefreshes(t *testing.T) {
	api := newGrantAPI(t)
	api.grants["grant-1"] = map[string]any{"GrantId": "grant-1", "Name": "observed",
		"GranteePrincipal": "lambda.amazonaws.com", "Operations": []any{"Sign"}}
	r := svc.GrantResource{KeyID: "new-desired-selector", GranteePrincipal: "ec2.amazonaws.com",
		Operations: []string{"Encrypt"}}
	prior := &svc.GrantResourceOutput{KeyID: grantInputs()["key-id"].(string), GrantID: "grant-1",
		GrantToken: aws.String("secret"), Operations: []string{"Encrypt"}}
	out, err := r.Update(t.Context(), &awscfg.Configuration{Region: aws.String("us-east-1"),
		EndpointURL: aws.String(api.endpoint), MaxAttempts: aws.Int64(1)},
		runtime.Prior[svc.GrantResource, *svc.GrantResourceOutput, *awscfg.Configuration]{
			Outputs: prior})
	require.NoError(t, err)
	require.Equal(t, &svc.GrantResourceOutput{KeyID: prior.KeyID, GrantID: prior.GrantID,
		GrantToken: aws.String("secret"), Name: aws.String("observed"),
		GranteePrincipal: "lambda.amazonaws.com", Operations: []string{"Sign"}}, out)
	require.Equal(t, []grantCall{{"TrentService.ListGrants", map[string]any{
		"KeyId": prior.KeyID, "Limit": float64(100)}}}, api.takeCalls())
}
