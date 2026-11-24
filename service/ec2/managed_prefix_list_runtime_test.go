package ec2

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const prefixAddress = "resource.prefix"

type prefixCloud struct {
	describeCount, failDescribe int
	mu                          sync.Mutex
	requests                    []stopRuntimeRequest
	exists                      bool
	family, name, status        string
	capacity                    int64
	version                     int64
	entries                     map[string]string
	tags                        map[string]string
	fail, cleanupFail           string
	cancel                      context.CancelFunc
}

func (c *prefixCloud) endpoint(t *testing.T) string {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		c.mu.Lock()
		defer c.mu.Unlock()
		region := ""
		if match := stopSigningRegion.FindStringSubmatch(r.Header.Get("Authorization")); match != nil {
			region = match[1]
		}
		form := r.PostForm
		action := form.Get("Action")
		c.requests = append(c.requests, stopRuntimeRequest{form, region})
		w.Header().Set("Content-Type", "text/xml")
		if action == c.fail || action == c.cleanupFail {
			w.WriteHeader(400)
			fmt.Fprintf(w, "<Response><Errors><Error><Code>%sFailure</Code>"+
				"<Message>injected</Message></Error></Errors></Response>", action)
			return
		}
		switch action {
		case "CreateManagedPrefixList":
			c.exists = true
			c.family, c.name = form.Get("AddressFamily"), form.Get("PrefixListName")
			c.capacity, _ = strconv.ParseInt(form.Get("MaxEntries"), 10, 64)
			c.version = 1
			c.status = "create-complete"
			c.entries, c.tags = map[string]string{}, map[string]string{}
			for i := 1; form.Has(fmt.Sprintf("Entry.%d.Cidr", i)); i++ {
				c.entries[form.Get(fmt.Sprintf("Entry.%d.Cidr", i))] =
					form.Get(fmt.Sprintf("Entry.%d.Description", i))
			}
			for i := 1; form.Has(fmt.Sprintf("TagSpecification.1.Tag.%d.Key", i)); i++ {
				c.tags[form.Get(fmt.Sprintf("TagSpecification.1.Tag.%d.Key", i))] =
					form.Get(fmt.Sprintf("TagSpecification.1.Tag.%d.Value", i))
			}
			fmt.Fprint(w, "<CreateManagedPrefixListResponse><prefixList><prefixListId>pl-one"+
				"</prefixListId></prefixList></CreateManagedPrefixListResponse>")

		case "DescribeManagedPrefixLists":
			c.describeCount++
			if c.describeCount == c.failDescribe {
				w.WriteHeader(400)
				fmt.Fprint(w, "<Response><Errors><Error><Code>DescribeFailure</Code>"+
					"</Error></Errors></Response>")
				return
			}
			if c.cancel != nil {
				c.cancel()
				c.cancel = nil
			}
			fmt.Fprint(w, "<DescribeManagedPrefixListsResponse><prefixListSet>")
			if c.exists {
				fmt.Fprintf(w, "<item><prefixListId>pl-one</prefixListId><state>%s</state>"+
					"<prefixListName>%s</prefixListName><addressFamily>%s</addressFamily>"+
					"<maxEntries>%d</maxEntries><version>%d</version><ownerId>owner</ownerId>"+
					"<prefixListArn>api-arn</prefixListArn><tagSet>",
					c.status, c.name, c.family, c.capacity, c.version)
				for key, value := range c.tags {
					fmt.Fprintf(w, "<item><key>%s</key><value>%s</value></item>", key, value)
				}
				fmt.Fprint(w, "</tagSet></item>")
			}
			fmt.Fprint(w, "</prefixListSet></DescribeManagedPrefixListsResponse>")
		case "GetManagedPrefixListEntries":
			fmt.Fprint(w, "<GetManagedPrefixListEntriesResponse><entrySet>")
			for cidr, description := range c.entries {
				fmt.Fprintf(w, "<item><cidr>%s</cidr><description>%s</description></item>", cidr, description)
			}
			fmt.Fprint(w, "</entrySet></GetManagedPrefixListEntriesResponse>")
		case "ModifyManagedPrefixList":
			if form.Has("PrefixListName") {
				c.name = form.Get("PrefixListName")
			}
			if form.Has("MaxEntries") {
				c.capacity, _ = strconv.ParseInt(form.Get("MaxEntries"), 10, 64)
			}
			for i := 1; form.Has(fmt.Sprintf("RemoveEntry.%d.Cidr", i)); i++ {
				delete(c.entries, form.Get(fmt.Sprintf("RemoveEntry.%d.Cidr", i)))
			}
			for i := 1; form.Has(fmt.Sprintf("AddEntry.%d.Cidr", i)); i++ {
				c.entries[form.Get(fmt.Sprintf("AddEntry.%d.Cidr", i))] =
					form.Get(fmt.Sprintf("AddEntry.%d.Description", i))
			}
			c.version++
			c.status = "modify-complete"
			fmt.Fprint(w, "<ModifyManagedPrefixListResponse/>")
		case "DeleteTags", "CreateTags":
			for i := 1; form.Has(fmt.Sprintf("Tag.%d.Key", i)); i++ {
				key := form.Get(fmt.Sprintf("Tag.%d.Key", i))
				if action == "DeleteTags" {
					delete(c.tags, key)
				} else {
					c.tags[key] = form.Get(fmt.Sprintf("Tag.%d.Value", i))
				}
			}
			fmt.Fprintf(w, "<%sResponse/>", action)
		case "DeleteManagedPrefixList":
			assert.Equal(t, "pl-one", form.Get("PrefixListId"))
			c.exists = false
			fmt.Fprint(w, "<DeleteManagedPrefixListResponse/>")
		default:
			t.Errorf("unexpected prefix API %s", action)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func (c *prefixCloud) mutations() []stopRuntimeRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	var result []stopRuntimeRequest
	for _, request := range c.requests {
		if !strings.HasPrefix(request.Form.Get("Action"), "Describe") &&
			request.Form.Get("Action") != "GetManagedPrefixListEntries" {
			result = append(result, request)
		}
	}
	c.requests = nil
	return result
}

func prefixExecutor(t *testing.T, fixture string, schema *runtime.LibrarySchema) *runtime.Executor {
	t.Helper()
	lib := runtime.LibraryWithPath(Library(),
		"github.com/cloudboss/unobin-library-aws//service/ec2")
	for _, fields := range schema.Resources {
		fields.EvaluatedInputs = fields.Inputs
	}
	lib.Schema = schema
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.managed-prefix-list": schema.Resources["managed-prefix-list"].Constraints,
	}
	libs := map[string]*runtime.Library{"ec2": lib, "test": {
		Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[stopSeed, map[string]any, *awscfg.Configuration](),
		},
	}}
	path := filepath.Join("testdata/ub/managed-prefix-list", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	store, err := local.NewStore(t.TempDir(), "test-prefix", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{DAG: runtime.BuildSyntaxDAG(body, libs), SyntaxSource: &body,
		Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "test-prefix", Version: "v0", ContentRevision: "test"}}
}

func prefixInputs(endpoint string) map[string]any {
	return map[string]any{"endpoint": endpoint, "region": "us-east-1", "values": map[string]any{
		"address-family": "IPv4", "name": "desired", "max-entries": int64(8),
		"entries": nil, "tags": nil,
	}}
}

func prefixSeed(t *testing.T, executor *runtime.Executor, cloud *prefixCloud) {
	t.Helper()
	values := executor.Inputs["values"].(map[string]any)
	cloud.exists, cloud.family, cloud.name = true, "IPv4", "desired"
	cloud.status = "modify-complete"
	cloud.capacity, cloud.version = 8, 42
	cloud.entries, cloud.tags = map[string]string{}, map[string]string{}
	snapshot := state.NewSnapshot(executor.Factory, executor.Store.Stack())
	snapshot.Entries = []*state.Entry{{Address: prefixAddress, Category: "resource", SchemaVersion: 1,
		Binding: &state.Binding{Alias: "ec2", Export: "managed-prefix-list",
			LibraryPath: "github.com/cloudboss/unobin-library-aws//service/ec2"},
		Inputs: maps.Clone(values), Configuration: map[string]any{
			"endpoint-url": executor.Inputs["endpoint"], "region": executor.Inputs["region"],
			"max-attempts": int64(1)},
		Outputs: map[string]any{"prefix-list-id": "pl-one", "address-family": "IPv4",
			"name": "desired", "max-entries": int64(8), "version": int64(42), "region": "us-east-1",
			"arn": "api-arn", "owner-id": "owner", "entries": []any{}, "tags": map[string]any{}},
	}}
	revision, err := executor.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(revision))
}

func prefixStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == prefixAddress {
			return step
		}
	}
	t.Fatal("prefix resource absent")
	return runtime.PlanStep{}
}

func TestManagedPrefixListNativeEvaluatedNoOpSavedAndDeferred(t *testing.T) {
	schema := readLibrarySchema(t)
	invalid := []struct {
		field   string
		value   any
		message string
	}{
		{"address-family", nil, `field "address-family": required but is null`},
		{"max-entries", nil, `field "max-entries": required but is null`},
		{"name", nil, `field "name": required but is null`},
		{"address-family", "ipv4", "address-family must be IPv4 or IPv6"},
		{"address-family", "", "address-family must be IPv4 or IPv6"},
		{"address-family", "IPv7", "address-family must be IPv4 or IPv6"},
		{"max-entries", int64(0), "max-entries must be at least 1"},
		{"max-entries", int64(-1), "max-entries must be at least 1"},
		{"name", "", "name must contain 1..255 bytes"},
		{"name", strings.Repeat("a", 256), "name must contain 1..255 bytes"},
		{"name", strings.Repeat("é", 128), "name must contain 1..255 bytes"},
		{"entries", []any{nil}, `field "entries[0]": required but is null`},
		{"entries", []any{map[string]any{}}, `field "entries[0].cidr": required but not provided`},
		{"entries", []any{map[string]any{"cidr": nil}},
			`field "entries[0].cidr": required but is null`},
		{"entries", []any{map[string]any{"cidr": "10.0.0.1/33"}}, "entries cidr must be a valid CIDR"},
		{"entries", []any{map[string]any{"cidr": "::1/128", "description": strings.Repeat("é", 128)}},
			"entries description must contain at most 255 bytes"},
		{"tags", map[string]any{"key": nil}, `field "tags[\"key\"]": required but is null`},
	}
	for _, mode := range []string{"create", "noop", "saved", "deferred", "replace"} {
		for i, tc := range invalid {
			t.Run(fmt.Sprintf("%s/%d/%s", mode, i, tc.field), func(t *testing.T) {
				cloud := &prefixCloud{}
				fixture := "evaluated"
				if mode == "deferred" || mode == "replace" {
					fixture = "deferred"
				}
				executor := prefixExecutor(t, fixture, schema)
				executor.Inputs = prefixInputs(cloud.endpoint(t))
				if mode != "create" && mode != "deferred" {
					prefixSeed(t, executor, cloud)
				}
				values := executor.Inputs["values"].(map[string]any)
				var saved *runtime.PlanFile
				if mode == "saved" {
					saved = stopSavedPlan(t, executor)
					require.Equal(t, runtime.DecisionNoOp, prefixStep(t, saved).Decision)
				}
				if mode == "replace" {
					values["address-family"] = "IPv6"
				}
				values[tc.field] = tc.value
				var err error
				if mode == "deferred" || mode == "replace" {
					saved = stopSavedPlan(t, executor)
					require.NotEmpty(t, prefixStep(t, saved).UnresolvedInputs)
				}
				if saved != nil {
					_, err = executor.ApplyPlan(t.Context(), saved)
				} else {
					_, err = executor.Plan(t.Context())
				}
				require.ErrorContains(t, err, prefixAddress)
				require.ErrorContains(t, err, tc.message)
				assert.Empty(t, cloud.mutations())
			})
		}
	}
}

func TestManagedPrefixListNativeValidAndHistoricalDestroy(t *testing.T) {
	schema := readLibrarySchema(t)
	for _, fixture := range []string{"evaluated", "deferred"} {
		cloud := &prefixCloud{}
		executor := prefixExecutor(t, fixture, schema)
		executor.Inputs = prefixInputs(cloud.endpoint(t))
		prefixSeed(t, executor, cloud)
		values := executor.Inputs["values"].(map[string]any)
		for _, name := range []string{strings.Repeat("a", 255), strings.Repeat("é", 127) + "x",
			"aws:allowed-locally"} {
			values["name"] = name
			values["entries"] = []any{map[string]any{"cidr": "10.0.0.1/08", "description": nil},
				map[string]any{"cidr": "2001:DB8::1/064", "description": strings.Repeat("é", 127) + "x"}}
			_, err := executor.ApplyPlan(t.Context(), stopSavedPlan(t, executor))
			require.NoError(t, err)
			assert.Equal(t, map[string]string{"10.0.0.1/08": "",
				"2001:DB8::1/064": strings.Repeat("é", 127) + "x"}, cloud.entries)
			plan := stopSavedPlan(t, executor)
			if fixture == "evaluated" {
				assert.Equal(t, runtime.DecisionNoOp, prefixStep(t, plan).Decision)
			}
			_, err = executor.ApplyPlan(t.Context(), plan)
			require.NoError(t, err)
		}
		values["name"], values["address-family"] = "", "historical"
		snapshot, err := executor.Store.Current()
		require.NoError(t, err)
		historical := snapshot.Find(prefixAddress)
		historical.Inputs["name"], historical.Inputs["address-family"] = "", "historical"
		historical.Inputs["max-entries"] = int64(0)
		historical.Inputs["entries"] = []any{nil}
		revision, err := executor.Store.Write(snapshot)
		require.NoError(t, err)
		require.NoError(t, executor.Store.SetCurrent(revision))
		executor.Destroy = true
		_, err = executor.ApplyPlan(t.Context(), stopSavedPlan(t, executor))
		require.NoError(t, err)
		assert.False(t, cloud.exists)
	}
}

func TestManagedPrefixListNativeReplacementAndSavedRegion(t *testing.T) {
	schema := readLibrarySchema(t)
	for _, changed := range []string{"address-family", "region"} {
		old, desired := &prefixCloud{}, &prefixCloud{}
		executor := prefixExecutor(t, "evaluated", schema)
		executor.Inputs = prefixInputs(old.endpoint(t))
		prefixSeed(t, executor, old)
		executor.Inputs["endpoint"] = desired.endpoint(t)
		if changed == "region" {
			executor.Inputs["region"] = "us-west-2"
		} else {
			executor.Inputs["values"].(map[string]any)["address-family"] = "IPv6"
		}
		plan := stopSavedPlan(t, executor)
		require.Equal(t, runtime.DecisionReplace, prefixStep(t, plan).Decision)
		assert.Contains(t, prefixStep(t, plan).ReplacementReasons, changed)
		_, err := executor.ApplyPlan(t.Context(), plan)
		require.NoError(t, err)
		assert.True(t, desired.exists)
		assert.False(t, old.exists)
		oldMutations := old.mutations()
		require.Equal(t, []stopRuntimeRequest{{Form: url.Values{
			"Action": {"DeleteManagedPrefixList"}, "Version": {"2016-11-15"},
			"PrefixListId": {"pl-one"}}, Region: "us-east-1"}}, oldMutations)
	}
}

func TestManagedPrefixListNativeFailedCreateCleanup(t *testing.T) {
	schema := readLibrarySchema(t)
	for _, failure := range []string{"wait", "describe", "entries", "cleanup", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			cloud := &prefixCloud{}
			executor := prefixExecutor(t, "evaluated", schema)
			executor.Inputs = prefixInputs(cloud.endpoint(t))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if failure == "entries" || failure == "cleanup" {
				cloud.fail = "GetManagedPrefixListEntries"
			}
			if failure == "wait" {
				cloud.fail = "DescribeManagedPrefixLists"
			}
			if failure == "describe" {
				cloud.failDescribe = 2
			}
			if failure == "cleanup" {
				cloud.cleanupFail = "DeleteManagedPrefixList"
			}
			if failure == "cancel" {
				cloud.cancel = cancel
			}
			_, err := executor.ApplyPlan(ctx, stopSavedPlan(t, executor))
			require.Error(t, err)
			snapshot, readErr := executor.Store.Current()
			if readErr != nil {
				require.ErrorIs(t, readErr, state.ErrNoCurrent)
			} else {
				assert.Nil(t, snapshot.Find(prefixAddress))
			}
			assert.Equal(t, failure == "cleanup", cloud.exists)
			if failure == "cleanup" {
				require.ErrorContains(t, err, "compensating delete")
			}
			actions := []string{}
			for _, request := range cloud.mutations() {
				actions = append(actions, request.Form.Get("Action"))
			}
			assert.True(t, slices.Contains(actions, "DeleteManagedPrefixList"))
		})
	}
}

func TestManagedPrefixListNativeDriftAndEntryManagement(t *testing.T) {
	cloud := &prefixCloud{}
	executor := prefixExecutor(t, "evaluated", readLibrarySchema(t))
	executor.Inputs = prefixInputs(cloud.endpoint(t))
	prefixSeed(t, executor, cloud)
	values := executor.Inputs["values"].(map[string]any)
	values["entries"] = []any{map[string]any{"cidr": "10.0.0.1/8", "description": "desired"}}
	values["tags"] = map[string]any{"blank": ""}
	_, err := executor.ApplyPlan(t.Context(), stopSavedPlan(t, executor))
	require.NoError(t, err)
	cloud.mutations()
	cloud.name, cloud.capacity = "drift", 2
	cloud.entries = map[string]string{"10.0.0.1/8": "drift", "192.0.2.0/24": "foreign"}
	cloud.tags = map[string]string{"unwanted": "remove", "blank": "drift", "aws:reserved": "preserve"}
	plan := stopSavedPlan(t, executor)
	assert.Equal(t, runtime.DecisionUpdate, prefixStep(t, plan).Decision)
	_, err = executor.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, "desired", cloud.name)
	assert.Equal(t, int64(8), cloud.capacity)
	assert.Equal(t, map[string]string{"10.0.0.1/8": "desired"}, cloud.entries)
	assert.Equal(t, map[string]string{"blank": "", "aws:reserved": "preserve"}, cloud.tags)
	var actions []string
	for _, request := range cloud.mutations() {
		actions = append(actions, request.Form.Get("Action"))
	}
	assert.Equal(t, []string{"DeleteTags", "CreateTags", "ModifyManagedPrefixList",
		"ModifyManagedPrefixList", "ModifyManagedPrefixList"}, actions)
	cloud.version++
	plan = stopSavedPlan(t, executor)
	assert.Equal(t, runtime.DecisionUpdate, prefixStep(t, plan).Decision)
	_, err = executor.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	assert.Empty(t, cloud.mutations())
	values["entries"] = nil
	plan = stopSavedPlan(t, executor)
	assert.Equal(t, runtime.DecisionNoOp, prefixStep(t, plan).Decision)
	_, err = executor.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"10.0.0.1/8": "desired"}, cloud.entries)
	values["entries"] = []any{}
	_, err = executor.ApplyPlan(t.Context(), stopSavedPlan(t, executor))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{}, cloud.entries)
	executor.Destroy = true
	_, err = executor.ApplyPlan(t.Context(), stopSavedPlan(t, executor))
	require.NoError(t, err)
	assert.False(t, cloud.exists)
}
