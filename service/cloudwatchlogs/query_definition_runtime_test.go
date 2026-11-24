package cloudwatchlogs

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/cloudwatchlogs"
)

type querySeed struct {
	Value map[string]any `ub:"value"`
}
type querySeedOutput struct {
	Name   any `ub:"name"`
	Query  any `ub:"query-string"`
	Groups any `ub:"log-group-names"`
}

func (s *querySeed) Run(context.Context, any) (*querySeedOutput, error) {
	return &querySeedOutput{s.Value["name"], s.Value["query-string"], s.Value["log-group-names"]}, nil
}

type queryCall struct {
	Operation string
	Body      map[string]any
}

type queryAPI struct {
	mu         sync.Mutex
	endpoint   string
	entries    map[string]map[string]any
	calls      []queryCall
	next       int
	fail, code string
	message    string
	account    string
	failTimes  int
	failDelete bool
	missingID  bool
	emptyRead  bool
	onRead     func(context.Context)
	pages      []map[string]any
}

func newQueryAPI(t *testing.T) *queryAPI {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "fake")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_ACCOUNT_ID", "123456789012")
	api := &queryAPI{entries: map[string]map[string]any{}, account: "123456789012"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		if r.Header.Get("X-Amz-Target") == "" {
			api.calls = append(api.calls, queryCall{"GetCallerIdentity", map[string]any{}})
			w.Header().Set("Content-Type", "text/xml")
			if api.fail == "GetCallerIdentity" {
				w.WriteHeader(403)
				fmt.Fprint(w, `<ErrorResponse><Error><Code>AccessDenied</Code>`+
					`<Message>identity failed</Message></Error></ErrorResponse>`)
				return
			}
			fmt.Fprintf(w, `<GetCallerIdentityResponse><GetCallerIdentityResult>`+
				`<Account>%s</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`, api.account)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		op := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "Logs_20140328.")
		if op == "PutQueryDefinition" {
			token, ok := body["clientToken"].(string)
			if !ok || token == "" {
				t.Error("missing SDK idempotency token")
			}
			delete(body, "clientToken")
		}
		api.calls = append(api.calls, queryCall{op, body})
		fail := func(code string) {
			status := 400
			if code == "ServiceUnavailableException" {
				status = 503
			}
			w.WriteHeader(status)
			message := api.message
			if message == "" {
				message = "operation failed"
			}
			fmt.Fprintf(w, `{"__type":%q,"message":%q}`, code, message)
		}
		if op == api.fail || op == "DeleteQueryDefinition" && api.failDelete {
			if op == "DescribeQueryDefinitions" && api.onRead != nil {
				api.onRead(r.Context())
				return
			}
			code := api.code
			if op == "DeleteQueryDefinition" && api.failDelete {
				code = "AccessDeniedException"
			}
			if code == "" {
				code = "AccessDeniedException"
			}
			if api.failTimes > 0 {
				api.failTimes--
				if api.failTimes == 0 {
					api.fail = ""
				}
			}
			fail(code)
			return
		}
		switch op {
		case "PutQueryDefinition":
			if api.missingID {
				fmt.Fprint(w, `{}`)
				return
			}
			id, _ := body["queryDefinitionId"].(string)
			if id == "" {
				api.next++
				id = fmt.Sprintf("id-%d", api.next)
			}
			entry := maps.Clone(body)
			entry["queryDefinitionId"] = id
			api.entries[id] = entry
			fmt.Fprintf(w, `{"queryDefinitionId":%q}`, id)
		case "DescribeQueryDefinitions":
			if api.pages != nil {
				page := api.pages[0]
				api.pages = api.pages[1:]
				_ = json.NewEncoder(w).Encode(page)
				return
			}
			entries := []map[string]any{}
			if !api.emptyRead {
				for _, entry := range api.entries {
					prefix, _ := body["queryDefinitionNamePrefix"].(string)
					if strings.HasPrefix(entry["name"].(string), prefix) {
						entries = append(entries, entry)
					}
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"queryDefinitions": entries})
		case "DeleteQueryDefinition":
			delete(api.entries, body["queryDefinitionId"].(string))
			fmt.Fprint(w, `{"success":false}`)
		default:
			t.Errorf("unexpected operation %s", op)
			fail("UnknownOperation")
		}
	}))
	t.Cleanup(server.Close)
	api.endpoint = server.URL
	return api
}

func TestQueryDefinitionSerializedGroupsAndSTS(t *testing.T) {
	api := newQueryAPI(t)
	identityAPI := newQueryAPI(t)
	t.Setenv("AWS_ACCOUNT_ID", "")
	cfg := &awscfg.Configuration{Region: aws.String("us-gov-west-1"),
		EndpointURL: aws.String(api.endpoint), MaxAttempts: aws.Int64(1),
		Endpoints: &awscfg.Endpoints{STS: aws.String(identityAPI.endpoint)}}
	for i, groups := range []*[]string{nil, &[]string{}, &[]string{""},
		&[]string{"b", "", "a", "b"}} {
		r := svc.QueryDefinitionResource{Name: "query", QueryString: "", LogGroupNames: groups}
		out, err := r.Create(t.Context(), cfg)
		require.NoError(t, err)
		id := fmt.Sprintf("id-%d", i+1)
		want := map[string]any{"name": "query", "queryString": ""}
		public := []string{}
		if i == 2 {
			want["logGroupNames"] = []any{}
		}
		if i == 3 {
			want["logGroupNames"] = []any{"b", "a", "b"}
			public = []string{"b", "a", "b"}
		}
		require.Equal(t, []queryCall{
			{"PutQueryDefinition", want},
			{"DescribeQueryDefinitions", map[string]any{"queryDefinitionNamePrefix": "query"}},
		}, api.takeCalls())
		require.Equal(t, []queryCall{{"GetCallerIdentity", map[string]any{}}}, identityAPI.takeCalls())
		require.Equal(t, &svc.QueryDefinitionResourceOutput{QueryDefinitionID: id, Name: "query",
			QueryString: "", LogGroupNames: public, Region: "us-gov-west-1",
			Arn: "arn:aws-us-gov:logs:us-gov-west-1:123456789012:query-definition:" + id}, out)
	}
	identityAPI.account = ""
	r := svc.QueryDefinitionResource{Name: "query"}
	_, err := r.Create(t.Context(), cfg)
	require.ErrorContains(t, err, "returned no account")
	require.Equal(t, []queryCall{
		{"PutQueryDefinition", map[string]any{"name": "query", "queryString": ""}},
		{"DeleteQueryDefinition", map[string]any{"queryDefinitionId": "id-5"}},
	}, queryMutations(api.takeCalls()))
}

func TestQueryDefinitionConfiguredRetries(t *testing.T) {
	for _, quota := range []bool{false, true} {
		t.Run(fmt.Sprint(quota), func(t *testing.T) {
			api := newQueryAPI(t)
			api.fail, api.code, api.failTimes = "PutQueryDefinition", "ServiceUnavailableException", 1
			if quota {
				api.code, api.message = "LimitExceededException", "Resource limit exceeded: queries"
			}
			r := svc.QueryDefinitionResource{Name: "query", QueryString: ""}
			_, err := r.Create(t.Context(), &awscfg.Configuration{Region: aws.String("us-east-1"),
				EndpointURL: aws.String(api.endpoint), MaxAttempts: aws.Int64(3)})
			put := queryCall{"PutQueryDefinition", map[string]any{"name": "query", "queryString": ""}}
			if quota {
				require.ErrorContains(t, err, "Resource limit exceeded")
				require.Equal(t, []queryCall{put}, api.takeCalls())
			} else {
				require.NoError(t, err)
				require.Equal(t, []queryCall{put, put, {"DescribeQueryDefinitions",
					map[string]any{"queryDefinitionNamePrefix": "query"}}}, api.takeCalls())
			}
		})
	}
}

func (a *queryAPI) takeCalls() []queryCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	calls := a.calls
	a.calls = nil
	return calls
}

func queryLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.Schema = readLibrarySchema(t)
	for _, schema := range lib.Schema.Resources {
		schema.EvaluatedInputs = maps.Clone(schema.Inputs)
	}
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.query-definition": lib.Schema.Resources["query-definition"].Constraints,
	}
	return map[string]*runtime.Library{"logs": lib, "test": {
		Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[querySeed, *querySeedOutput, any](),
		},
	}}
}

func queryInputs() map[string]any {
	return map[string]any{"name": "query/path", "query-string": "fields @message\n| limit 5\n",
		"log-group-names": []any{"b", "", "a", "b"}}
}

func queryExecutor(t *testing.T, fixture string, api *queryAPI,
	libs map[string]*runtime.Library, store sdkstate.Backend, inputs map[string]any,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "query-definition", "runtime", fixture, "factory.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	if store == nil {
		store, err = local.NewStore(t.TempDir(), "query", "test", encrypters.Noop{})
		require.NoError(t, err)
	}
	body := &f.Factory.Body
	return &runtime.Executor{DAG: runtime.BuildSyntaxDAG(*body, libs), SyntaxSource: body,
		Libraries: libs, Store: store, Inputs: map[string]any{
			"endpoint": api.endpoint, "region": "us-east-1", "query": inputs}}
}

func querySaved(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	data, err := runtime.SealPlan(plan, encrypters.Noop{})
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(data, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	return saved
}

func queryStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == "resource.subject" {
			return step
		}
	}
	t.Fatal("no subject step")
	return runtime.PlanStep{}
}

func queryApply(t *testing.T, exec *runtime.Executor, decision runtime.Decision) {
	t.Helper()
	plan := querySaved(t, exec)
	require.Equal(t, decision, queryStep(t, plan).Decision)
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
}

func queryMutations(calls []queryCall) []queryCall {
	var out []queryCall
	for _, call := range calls {
		if call.Operation != "DescribeQueryDefinitions" && call.Operation != "GetCallerIdentity" {
			out = append(out, call)
		}
	}
	return out
}

func TestQueryDefinitionNativeCRUDDriftAndDestroy(t *testing.T) {
	api := newQueryAPI(t)
	libs := queryLibraries(t)
	exec := queryExecutor(t, "resolved", api, libs, nil, queryInputs())
	queryApply(t, exec, runtime.DecisionCreate)
	require.Equal(t, []queryCall{
		{"PutQueryDefinition", map[string]any{"name": "query/path",
			"queryString": queryInputs()["query-string"], "logGroupNames": []any{"b", "a", "b"}}},
		{"DescribeQueryDefinitions", map[string]any{"queryDefinitionNamePrefix": "query/path"}},
	}, api.takeCalls())
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	require.Equal(t, map[string]any{"arn": "arn:aws:logs:us-east-1:123456789012:query-definition:id-1",
		"query-definition-id": "id-1", "name": "query/path",
		"query-string":    queryInputs()["query-string"],
		"log-group-names": []any{"b", "a", "b"}, "region": "us-east-1"},
		snapshot.Find("resource.subject").Outputs)
	queryApply(t, exec, runtime.DecisionNoOp)
	require.Empty(t, queryMutations(api.takeCalls()))
	api.mu.Lock()
	api.entries["id-1"]["queryString"] = "drift"
	api.entries["id-1"]["logGroupNames"] = []any{"external"}
	api.entries["id-1"]["name"] = "query/path-still-visible"
	api.mu.Unlock()
	queryApply(t, exec, runtime.DecisionUpdate)
	require.Equal(t, []queryCall{{"PutQueryDefinition", map[string]any{
		"queryDefinitionId": "id-1", "name": "query/path",
		"queryString": queryInputs()["query-string"], "logGroupNames": []any{"b", "a", "b"}}}},
		queryMutations(api.takeCalls()))
	inputs := queryInputs()
	inputs["name"], inputs["query-string"], inputs["log-group-names"] = "renamed", "", nil
	exec.Inputs["query"] = inputs
	queryApply(t, exec, runtime.DecisionUpdate)
	require.Equal(t, []queryCall{{"PutQueryDefinition", map[string]any{
		"queryDefinitionId": "id-1", "name": "renamed", "queryString": ""}}},
		queryMutations(api.takeCalls()))
	for _, groups := range []any{[]any{"x"}, []any{}, []any{""}, []any{"a", "b"},
		[]any{"b", "a"}, []any{"b", "a", "b"}} {
		inputs["log-group-names"] = groups
		queryApply(t, exec, runtime.DecisionUpdate)
		calls := queryMutations(api.takeCalls())
		want := map[string]any{"queryDefinitionId": "id-1", "name": "renamed", "queryString": ""}
		if len(groups.([]any)) > 0 {
			want["logGroupNames"] = groups
			if groups.([]any)[0] == "" {
				want["logGroupNames"] = []any{}
			}
		}
		require.Equal(t, []queryCall{{"PutQueryDefinition", want}}, calls)
	}
	snapshot, err = exec.Store.Current()
	require.NoError(t, err)
	require.Equal(t, "id-1", snapshot.Find("resource.subject").Outputs["query-definition-id"])
	require.Equal(t, "arn:aws:logs:us-east-1:123456789012:query-definition:id-1",
		snapshot.Find("resource.subject").Outputs["arn"])
	entry := snapshot.Find("resource.subject")
	entry.Inputs["name"], entry.Inputs["query-string"] = nil, nil
	entry.Inputs["log-group-names"] = []any{"invalid:*"}
	revision, err := exec.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, exec.Store.SetCurrent(revision))
	exec.Destroy = true
	queryApply(t, exec, runtime.DecisionDestroy)
	require.Equal(t, []queryCall{{"DeleteQueryDefinition", map[string]any{
		"queryDefinitionId": "id-1"}}}, queryMutations(api.takeCalls()))
	snapshot, err = exec.Store.Current()
	require.NoError(t, err)
	require.Empty(t, snapshot.Entries)
}

func TestQueryDefinitionNativeConstraints(t *testing.T) {
	api := newQueryAPI(t)
	libs := queryLibraries(t)
	for _, tc := range []struct {
		name, field string
		value       any
		rule        int
		message     string
	}{
		{"null name", "name", nil, -1, ""},
		{"null query", "query-string", nil, -1, ""},
		{"predicate null name", "name", nil, 0, "name is required"},
		{"predicate null query", "query-string", nil, 1, "query-string is required"},
		{"empty name", "name", "", 2, "1 to 255 bytes"},
		{"256 bytes", "name", strings.Repeat("a", 256), 2, "1 to 255 bytes"},
		{"multibyte", "name", strings.Repeat("é", 128), 2, "1 to 255 bytes"},
		{"colon", "name", "a:b", 3, "name pattern"},
		{"asterisk", "name", "a*b", 3, "name pattern"},
		{"leading slash", "name", "/a", 3, "name pattern"},
		{"trailing slash", "name", "a/", 3, "name pattern"},
		{"double slash", "name", "a//b", 3, "name pattern"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			libs := libs
			if strings.HasPrefix(tc.name, "predicate ") {
				libs = queryLibraries(t)
				libs["logs"].Schema.Resources["query-definition"].EvaluatedInputs = nil
			}
			api.entries, api.next = map[string]map[string]any{}, 0
			exec := queryExecutor(t, "resolved", api, libs, nil, queryInputs())
			queryApply(t, exec, runtime.DecisionCreate)
			api.takeCalls()
			saved := querySaved(t, exec)
			require.Equal(t, runtime.DecisionNoOp, queryStep(t, saved).Decision)
			api.takeCalls()
			invalid := queryInputs()
			invalid[tc.field] = tc.value
			bad := queryExecutor(t, "resolved", api, libs, exec.Store, invalid)
			check := func(err error) {
				t.Helper()
				if tc.rule < 0 {
					require.ErrorContains(t, err, fmt.Sprintf(
						`resource.subject: field %q: required but is null`, tc.field))
				} else {
					require.ErrorContains(t, err, "resource.subject: schema: constraints[")
					require.ErrorContains(t, err, fmt.Sprintf("constraints[%d] (predicate)", tc.rule))
					require.ErrorContains(t, err, tc.message)
				}
				require.Empty(t, queryMutations(api.takeCalls()))
			}
			_, err := bad.Plan(t.Context())
			check(err)
			_, err = bad.ApplyPlan(t.Context(), saved)
			check(err)
			deferred := queryExecutor(t, "deferred", api, libs, exec.Store, invalid)
			deferred.Inputs["region"] = "us-west-2"
			pending := querySaved(t, deferred)
			require.NotEmpty(t, queryStep(t, pending).UnresolvedInputs)
			api.takeCalls()
			_, err = deferred.ApplyPlan(t.Context(), pending)
			check(err)
			// The same invalid desired input is also an otherwise unchanged historical input.
			snapshot, err := exec.Store.Current()
			require.NoError(t, err)
			snapshot.Find("resource.subject").Inputs[tc.field] = tc.value
			rev, err := exec.Store.Write(snapshot)
			require.NoError(t, err)
			require.NoError(t, exec.Store.SetCurrent(rev))
			_, err = bad.Plan(t.Context())
			check(err)
		})
	}
}

func TestQueryDefinitionDeferredValidAndReplacementPreflight(t *testing.T) {
	api := newQueryAPI(t)
	libs := queryLibraries(t)
	exec := queryExecutor(t, "deferred", api, libs, nil, queryInputs())
	queryApply(t, exec, runtime.DecisionCreate)
	api.takeCalls()
	pending := querySaved(t, exec)
	api.takeCalls()
	_, err := exec.ApplyPlan(t.Context(), pending)
	require.NoError(t, err)
	require.Empty(t, queryMutations(api.takeCalls()))
	exec.Inputs["region"] = "us-west-2"
	invalid := queryInputs()
	invalid["log-group-names"] = []any{"not:valid"}
	exec.Inputs["query"] = invalid
	pending = querySaved(t, exec)
	api.takeCalls()
	_, err = exec.ApplyPlan(t.Context(), pending)
	require.ErrorContains(t, err, "invalid log-group-names")
	require.Empty(t, queryMutations(api.takeCalls()))
	newAPI := newQueryAPI(t)
	exec.Inputs["endpoint"], exec.Inputs["query"] = newAPI.endpoint, queryInputs()
	pending = querySaved(t, exec)
	require.Equal(t, runtime.DecisionReplace, queryStep(t, pending).Decision)
	api.takeCalls()
	_, err = exec.ApplyPlan(t.Context(), pending)
	require.NoError(t, err)
	require.Equal(t, []queryCall{{"DeleteQueryDefinition", map[string]any{
		"queryDefinitionId": "id-1"}}}, queryMutations(api.takeCalls()))
	require.Equal(t, []queryCall{{"PutQueryDefinition", map[string]any{"name": "query/path",
		"queryString": queryInputs()["query-string"], "logGroupNames": []any{"b", "a", "b"}}}},
		queryMutations(newAPI.takeCalls()))
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	require.Equal(t, "us-west-2", snapshot.Find("resource.subject").Outputs["region"])
}

func TestQueryDefinitionNativeCreateFailures(t *testing.T) {
	libs := queryLibraries(t)
	for _, mode := range []string{"read", "notfound", "empty", "identity", "cancel", "put", "no ID"} {
		for _, cleanupFails := range []bool{false, true} {
			t.Run(fmt.Sprint(mode, cleanupFails), func(t *testing.T) {
				api := newQueryAPI(t)
				api.failDelete = cleanupFails
				api.fail = "DescribeQueryDefinitions"
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				switch mode {
				case "notfound":
					api.code = "ResourceNotFoundException"
				case "empty":
					api.fail, api.emptyRead = "", true
				case "identity":
					t.Setenv("AWS_ACCOUNT_ID", "")
					api.fail = "GetCallerIdentity"
				case "cancel":
					api.onRead = func(requestCtx context.Context) { cancel(); <-requestCtx.Done() }
				case "put":
					api.fail = "PutQueryDefinition"
				case "no ID":
					api.fail, api.missingID = "", true
				}
				exec := queryExecutor(t, "resolved", api, libs, nil, queryInputs())
				pending := querySaved(t, exec)
				_, err := exec.ApplyPlan(ctx, pending)
				require.Error(t, err)
				if mode == "cancel" {
					require.ErrorIs(t, err, context.Canceled)
				}
				if mode == "notfound" {
					require.ErrorContains(t, err, "ResourceNotFoundException")
				}
				if mode == "identity" {
					require.ErrorContains(t, err, "identity failed")
				}
				if cleanupFails && mode != "put" && mode != "no ID" {
					require.ErrorContains(t, err, "clean up query definition")
				}
				want := []queryCall{{"PutQueryDefinition", map[string]any{"name": "query/path",
					"queryString": queryInputs()["query-string"], "logGroupNames": []any{"b", "a", "b"}}}}
				if mode != "put" && mode != "no ID" {
					want = append(want, queryCall{"DeleteQueryDefinition",
						map[string]any{"queryDefinitionId": "id-1"}})
				}
				require.Equal(t, want, queryMutations(api.takeCalls()))
				snapshot, err := exec.Store.Current()
				require.ErrorIs(t, err, sdkstate.ErrNoCurrent)
				require.Nil(t, snapshot)
				api.mu.Lock()
				if cleanupFails && mode != "put" && mode != "no ID" {
					require.Contains(t, api.entries, "id-1")
				} else {
					require.Empty(t, api.entries)
				}
				api.mu.Unlock()
			})
		}
	}
}

func TestQueryDefinitionReadPaginationIdentityAndAbsence(t *testing.T) {
	api := newQueryAPI(t)
	cfg := &awscfg.Configuration{Region: aws.String("cn-north-1"),
		EndpointURL: aws.String(api.endpoint), MaxAttempts: aws.Int64(1)}
	r := svc.QueryDefinitionResource{Name: "desired"}
	prior := runtime.Prior[svc.QueryDefinitionResource,
		*svc.QueryDefinitionResourceOutput, *awscfg.Configuration]{
		Inputs: svc.QueryDefinitionResource{Name: "prefix"}, Outputs: &svc.QueryDefinitionResourceOutput{
			QueryDefinitionID: "assigned", Region: "cn-north-1"}}
	matched := map[string]any{"queryDefinitionId": "assigned", "name": "different name",
		"queryString": "\nexact\n", "logGroupNames": []any{"b", "a", "b"}}
	api.pages = []map[string]any{
		{"queryDefinitions": []any{map[string]any{"queryDefinitionId": "other", "name": "prefix"}},
			"nextToken": "second"},
		{"queryDefinitions": []any{}, "nextToken": "third"},
		{"queryDefinitions": []any{matched, matched}, "nextToken": "unrequested"},
	}
	out, err := r.Read(t.Context(), cfg, prior)
	require.NoError(t, err)
	require.Equal(t, &svc.QueryDefinitionResourceOutput{QueryDefinitionID: "assigned",
		Name: "different name", QueryString: "\nexact\n", LogGroupNames: []string{"b", "a", "b"},
		Region: "cn-north-1",
		Arn:    "arn:aws-cn:logs:cn-north-1:123456789012:query-definition:assigned"}, out)
	require.Equal(t, []queryCall{
		{"DescribeQueryDefinitions", map[string]any{"queryDefinitionNamePrefix": "prefix"}},
		{"DescribeQueryDefinitions", map[string]any{
			"queryDefinitionNamePrefix": "prefix", "nextToken": "second"}},
		{"DescribeQueryDefinitions", map[string]any{
			"queryDefinitionNamePrefix": "prefix", "nextToken": "third"}},
	}, api.takeCalls())
	prior.Inputs.Name = ""
	delete(matched, "logGroupNames")
	api.pages = []map[string]any{{"queryDefinitions": []any{matched}}}
	out, err = r.Read(t.Context(), cfg, prior)
	require.NoError(t, err)
	require.Equal(t, []string{}, out.LogGroupNames)
	require.Equal(t, []queryCall{{"DescribeQueryDefinitions", map[string]any{}}}, api.takeCalls())
	api.pages = []map[string]any{{"nextToken": "last"}, {}}
	_, err = r.Read(t.Context(), cfg, prior)
	require.ErrorIs(t, err, runtime.ErrNotFound)
	require.Equal(t, []queryCall{{"DescribeQueryDefinitions", map[string]any{}},
		{"DescribeQueryDefinitions", map[string]any{"nextToken": "last"}}}, api.takeCalls())
	api.pages = nil
	for _, code := range []string{"ResourceNotFoundException", "AccessDeniedException"} {
		api.fail, api.code = "DescribeQueryDefinitions", code
		_, err := r.Read(t.Context(), cfg, prior)
		require.ErrorContains(t, err, code)
		require.NotErrorIs(t, err, runtime.ErrNotFound)
	}
}

func TestQueryDefinitionNativeAbsenceUpdateFailureAndDelete(t *testing.T) {
	api := newQueryAPI(t)
	libs := queryLibraries(t)
	exec := queryExecutor(t, "resolved", api, libs, nil, queryInputs())
	queryApply(t, exec, runtime.DecisionCreate)
	api.takeCalls()
	api.entries["id-1"]["name"] = "outside-saved-prefix"
	require.Equal(t, runtime.DecisionCreate, queryStep(t, querySaved(t, exec)).Decision)
	api.entries["id-1"]["name"] = "query/path"
	for _, code := range []string{"ResourceNotFoundException", "AccessDeniedException"} {
		api.fail, api.code = "DescribeQueryDefinitions", code
		_, err := exec.Plan(t.Context())
		require.ErrorContains(t, err, code)
		require.NotErrorIs(t, err, runtime.ErrNotFound)
		require.Empty(t, queryMutations(api.takeCalls()))
	}
	api.fail, api.code = "", ""
	for _, op := range []string{"PutQueryDefinition", "DescribeQueryDefinitions"} {
		inputs := queryInputs()
		inputs["query-string"] = "changed"
		exec.Inputs["query"] = inputs
		pending := querySaved(t, exec)
		api.takeCalls()
		api.fail = op
		_, err := exec.ApplyPlan(t.Context(), pending)
		require.ErrorContains(t, err, "AccessDeniedException")
		calls := queryMutations(api.takeCalls())
		require.Equal(t, []queryCall{{"PutQueryDefinition", map[string]any{
			"name": "query/path", "queryString": "changed", "queryDefinitionId": "id-1",
			"logGroupNames": []any{"b", "a", "b"}}}}, calls)
		snapshot, err := exec.Store.Current()
		require.NoError(t, err)
		require.Equal(t, "id-1", snapshot.Find("resource.subject").Outputs["query-definition-id"])
		api.fail = ""
		api.entries["id-1"]["queryString"] = queryInputs()["query-string"]
	}
	r := svc.QueryDefinitionResource{Name: "invalid:*", LogGroupNames: &[]string{"bad:*"}}
	prior := runtime.Prior[svc.QueryDefinitionResource,
		*svc.QueryDefinitionResourceOutput, *awscfg.Configuration]{
		Outputs: &svc.QueryDefinitionResourceOutput{QueryDefinitionID: "id-1"}}
	for _, code := range []string{"", "ResourceNotFoundException", "AccessDeniedException"} {
		api.fail, api.code = "", code
		if code != "" {
			api.fail = "DeleteQueryDefinition"
		}
		err := r.Delete(t.Context(), &awscfg.Configuration{Region: aws.String("us-east-1"),
			EndpointURL: aws.String(api.endpoint), MaxAttempts: aws.Int64(1)}, prior)
		if code == "AccessDeniedException" {
			require.ErrorContains(t, err, code)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, []queryCall{{"DeleteQueryDefinition", map[string]any{
			"queryDefinitionId": "id-1"}}}, api.takeCalls())
	}
}
