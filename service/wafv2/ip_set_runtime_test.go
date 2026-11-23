package wafv2_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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

	awswafv2 "github.com/cloudboss/unobin-library-aws/service/wafv2"
)

const (
	ipSetAddress     = "resource.addresses"
	ipSetLibraryPath = "github.com/cloudboss/unobin-library-aws//service/wafv2"
)

type ipSetRequest struct {
	operation string
	endpoint  string
	region    string
	body      map[string]any
}

type ipSetCloud struct {
	mu       sync.Mutex
	calls    []ipSetRequest
	sets     map[string]map[string]any
	nextID   int
	getError string
}

func newIPSetCloud(t *testing.T) *ipSetCloud {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "us-west-2")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing-credentials"))
	return &ipSetCloud{sets: map[string]map[string]any{}}
}

func (c *ipSetCloud) endpoint(t *testing.T, label string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode WAF request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		signing := strings.SplitN(r.Header.Get("Authorization"), "/", 5)
		if !assert.Len(t, signing, 5, "SigV4 credentials") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, operation, _ := strings.Cut(r.Header.Get("X-Amz-Target"), ".")
		call := ipSetRequest{operation: operation, endpoint: label, region: signing[2], body: body}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		c.mu.Lock()
		defer c.mu.Unlock()
		c.calls = append(c.calls, call)
		response, code := c.reply(call)
		if code != "" {
			w.WriteHeader(http.StatusBadRequest)
			response = map[string]any{"__type": code, "message": "test WAF response"}
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode WAF response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func (c *ipSetCloud) reply(call ipSetRequest) (map[string]any, string) {
	body := call.body
	key := fmt.Sprint(call.region, "/", body["Scope"], "/", body["Name"])
	set := c.sets[key]
	switch call.operation {
	case "CreateIPSet":
		c.nextID++
		id := fmt.Sprintf("id-%d", c.nextID)
		scopePart := "regional"
		if body["Scope"] == "CLOUDFRONT" {
			scopePart = "global"
		}
		arn := fmt.Sprintf("arn:aws:wafv2:%s:123456789012:%s/ipset/%s/%s",
			call.region, scopePart, body["Name"], id)
		c.sets[key] = map[string]any{
			"Id": id, "ARN": arn, "Name": body["Name"],
			"IPAddressVersion": body["IPAddressVersion"],
			"Addresses":        body["Addresses"], "Description": body["Description"],
		}
		return map[string]any{"Summary": map[string]any{"Id": id}}, ""
	case "GetIPSet":
		if c.getError != "" {
			return nil, c.getError
		}
		if set == nil || set["Id"] != body["Id"] {
			return nil, "WAFNonexistentItemException"
		}
		return map[string]any{"IPSet": set, "LockToken": "observed-token"}, ""
	case "UpdateIPSet":
		if set == nil || set["Id"] != body["Id"] {
			return nil, "WAFNonexistentItemException"
		}
		set["Addresses"] = body["Addresses"]
		set["Description"] = body["Description"]
		return map[string]any{"NextLockToken": "unused-token"}, ""
	case "DeleteIPSet":
		if set == nil || set["Id"] != body["Id"] {
			return nil, "WAFNonexistentItemException"
		}
		delete(c.sets, key)
		return map[string]any{}, ""
	case "ListTagsForResource":
		return map[string]any{}, ""
	default:
		return nil, "UnexpectedOperation"
	}
}

func (c *ipSetCloud) takeCalls() []ipSetRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls = nil
	return calls
}

type ipSetSeed struct {
	NextName *string
}

type ipSetSeedOutput struct {
	Name *string
}

func (s *ipSetSeed) Run(context.Context, runtime.NoConfig) (*ipSetSeedOutput, error) {
	return &ipSetSeedOutput{Name: s.NextName}, nil
}

func ipSetRuntimeLibrary(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	library := runtime.LibraryWithPath(awswafv2.Library(), ipSetLibraryPath)
	library.Schema = readLibrarySchema(t)
	library.Constraints = map[string][]lang.ConstraintSpec{
		"resource.ip-set": library.Schema.Resources["ip-set"].Constraints,
	}
	return map[string]*runtime.Library{
		"aws-wafv2": library,
		"fixture": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[ipSetSeed, *ipSetSeedOutput, runtime.NoConfig, *ipSetSeed](),
		}},
	}
}

func ipSetExecutor(
	t *testing.T, fixture string, libraries map[string]*runtime.Library, endpoint string,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "ip-set", fixture, "factory.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	body := &parsed.Factory.Body
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libraries), Libraries: libraries, SyntaxSource: body,
		Store: store, Factory: state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"},
		Inputs: map[string]any{
			"aws-config": map[string]any{"endpoint-url": endpoint},
			"scope":      "REGIONAL", "ip-address-version": "IPV4", "name": "example",
			"name-prefix": nil, "description": nil, "addresses": nil,
		},
	}
}

func saveIPSetPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func ipSetStep(t *testing.T, plan *runtime.PlanFile, decision runtime.Decision) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == ipSetAddress {
			require.Equal(t, decision, step.Decision)
			return step
		}
	}
	t.Fatalf("missing %s", ipSetAddress)
	return runtime.PlanStep{}
}

func applyIPSet(t *testing.T, executor *runtime.Executor, decision runtime.Decision) {
	t.Helper()
	saved := saveIPSetPlan(t, executor)
	ipSetStep(t, saved, decision)
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
}

func ipSetSnapshot(t *testing.T, executor *runtime.Executor) *state.Snapshot {
	t.Helper()
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	return snapshot
}

func ipSetPutSnapshot(t *testing.T, executor *runtime.Executor, snapshot *state.Snapshot) {
	t.Helper()
	rev, err := executor.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(rev))
}

func ipSetGetBody(id, name, scope string) map[string]any {
	return map[string]any{"Id": id, "Scope": scope, "Name": name}
}

func ipSetDeleteBody(id, name, scope string) map[string]any {
	body := ipSetGetBody(id, name, scope)
	body["LockToken"] = "observed-token"
	return body
}

func ipSetCreateBody(name, scope, version string, addresses []any) map[string]any {
	return map[string]any{
		"Name": name, "Scope": scope, "IPAddressVersion": version, "Addresses": addresses,
	}
}

func ipSetTagBody(region, scopePart, name, id string) map[string]any {
	arn := fmt.Sprintf("arn:aws:wafv2:%s:123456789012:%s/ipset/%s/%s", region, scopePart, name, id)
	return map[string]any{"ResourceARN": arn}
}

func ipSetReadCalls(endpoint, region, scope, name, id string) []ipSetRequest {
	scopePart := "regional"
	if scope == "CLOUDFRONT" {
		scopePart = "global"
	}
	return []ipSetRequest{
		{"GetIPSet", endpoint, region, ipSetGetBody(id, name, scope)},
		{"ListTagsForResource", endpoint, region, ipSetTagBody(region, scopePart, name, id)},
	}
}

func TestIPSetRuntimeEmptyAddressSerialization(t *testing.T) {
	libraries := ipSetRuntimeLibrary(t)
	for _, addresses := range [][]any{nil, {}, {"", ""}} {
		t.Run(fmt.Sprintf("%#v", addresses), func(t *testing.T) {
			cloud := newIPSetCloud(t)
			executor := ipSetExecutor(t, "evaluated", libraries, cloud.endpoint(t, "primary"))
			if addresses != nil {
				executor.Inputs["addresses"] = addresses
			}
			applyIPSet(t, executor, runtime.DecisionCreate)
			expected := []ipSetRequest{{
				"CreateIPSet", "primary", "us-west-2",
				ipSetCreateBody("example", "REGIONAL", "IPV4", []any{}),
			}}
			expected = append(expected, ipSetReadCalls(
				"primary", "us-west-2", "REGIONAL", "example", "id-1",
			)...)
			require.Equal(t, expected, cloud.takeCalls())
		})
	}
}

func TestIPSetRuntimeUnorderedAddressesAndDrift(t *testing.T) {
	cloud := newIPSetCloud(t)
	executor := ipSetExecutor(t, "evaluated", ipSetRuntimeLibrary(t), cloud.endpoint(t, "primary"))
	executor.Inputs["ip-address-version"] = "IPV6"
	executor.Inputs["description"] = "desired"
	executor.Inputs["addresses"] = []any{"2001:0db8::1/64", "2001:db8:1::/64"}
	applyIPSet(t, executor, runtime.DecisionCreate)
	cloud.takeCalls()

	executor.Inputs["addresses"] = []any{"2001:db8:1::/64", "2001:db8::1/64"}
	applyIPSet(t, executor, runtime.DecisionNoOp)
	read := ipSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1")
	require.Equal(t, read, cloud.takeCalls())

	for _, test := range []struct {
		name, address string
	}{
		{"host bits", "2001:db8::2/64"},
		{"mask", "2001:db8::2/65"},
	} {
		t.Run(test.name, func(t *testing.T) {
			addresses := []any{"2001:db8:1::/64", test.address}
			executor.Inputs["addresses"] = addresses
			applyIPSet(t, executor, runtime.DecisionUpdate)
			update := ipSetDeleteBody("id-1", "example", "REGIONAL")
			update["Addresses"] = addresses
			update["Description"] = "desired"
			expected := append(slices.Clone(read),
				ipSetRequest{"UpdateIPSet", "primary", "us-west-2", update})
			require.Equal(t, append(expected, read...), cloud.takeCalls())
		})
	}

	cloud.mu.Lock()
	set := cloud.sets["us-west-2/REGIONAL/example"]
	set["Description"] = "drifted description"
	set["Addresses"] = []any{"2001:db8:2::/64"}
	cloud.mu.Unlock()
	applyIPSet(t, executor, runtime.DecisionUpdate)
	update := ipSetDeleteBody("id-1", "example", "REGIONAL")
	update["Addresses"] = executor.Inputs["addresses"]
	update["Description"] = "desired"
	expected := append(slices.Clone(read), ipSetRequest{"UpdateIPSet", "primary", "us-west-2", update})
	require.Equal(t, append(expected, read...), cloud.takeCalls())

	executor.Inputs["addresses"] = nil
	executor.Inputs["description"] = nil
	applyIPSet(t, executor, runtime.DecisionUpdate)
	update = ipSetDeleteBody("id-1", "example", "REGIONAL")
	update["Addresses"] = []any{}
	expected = append(slices.Clone(read), ipSetRequest{"UpdateIPSet", "primary", "us-west-2", update})
	require.Equal(t, append(expected, read...), cloud.takeCalls())
	applyIPSet(t, executor, runtime.DecisionNoOp)
	require.Equal(t, read, cloud.takeCalls())
}

func TestIPSetRuntimeImmutableFields(t *testing.T) {
	libraries := ipSetRuntimeLibrary(t)
	for _, tc := range []struct {
		field         string
		initial, next any
	}{
		{"name", "example", "other"},
		{"name-prefix", "first-", "other-"},
		{"scope", "REGIONAL", "CLOUDFRONT"},
		{"ip-address-version", "IPV4", "IPV6"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			cloud := newIPSetCloud(t)
			executor := ipSetExecutor(t, "evaluated", libraries, cloud.endpoint(t, "primary"))
			if tc.field == "name-prefix" {
				executor.Inputs["name"] = nil
			}
			executor.Inputs[tc.field] = tc.initial
			applyIPSet(t, executor, runtime.DecisionCreate)
			cloud.takeCalls()
			executor.Inputs[tc.field] = tc.next
			step := ipSetStep(t, saveIPSetPlan(t, executor), runtime.DecisionReplace)
			require.Equal(t, []string{tc.field}, step.ReplacementReasons)
			previous := ipSetSnapshot(t, executor).Find(ipSetAddress)
			read := ipSetReadCalls("primary", "us-west-2", "REGIONAL",
				previous.Outputs["name"].(string), "id-1")
			require.Equal(t, read, cloud.takeCalls())
		})
	}
}

func TestIPSetRuntimeSavedReplacementRegions(t *testing.T) {
	libraries := ipSetRuntimeLibrary(t)
	for _, scope := range []string{"REGIONAL", "CLOUDFRONT"} {
		t.Run(scope, func(t *testing.T) {
			cloud := newIPSetCloud(t)
			old := cloud.endpoint(t, "old")
			executor := ipSetExecutor(t, "evaluated", libraries, old)
			executor.Inputs["scope"] = scope
			executor.Inputs["aws-config"] = map[string]any{"endpoint-url": old, "region": "us-west-2"}
			applyIPSet(t, executor, runtime.DecisionCreate)
			oldRegion, newRegion := "us-west-2", "us-east-2"
			if scope == "CLOUDFRONT" {
				oldRegion, newRegion = "us-east-1", "us-east-1"
			}
			oldReads := ipSetReadCalls("old", oldRegion, scope, "example", "id-1")
			expected := []ipSetRequest{{
				"CreateIPSet", "old", oldRegion, ipSetCreateBody("example", scope, "IPV4", []any{}),
			}}
			require.Equal(t, append(expected, oldReads...), cloud.takeCalls())

			executor.Inputs["aws-config"] = map[string]any{
				"endpoint-url": cloud.endpoint(t, "new"), "region": "us-east-2",
			}
			saved := saveIPSetPlan(t, executor)
			step := ipSetStep(t, saved, runtime.DecisionReplace)
			require.Equal(t, []string{"region"}, step.ReplacementReasons)
			require.Equal(t, oldReads, cloud.takeCalls())
			_, err := executor.ApplyPlan(t.Context(), saved)
			require.NoError(t, err)
			expected = append(slices.Clone(oldReads),
				ipSetRequest{"DeleteIPSet", "old", oldRegion, ipSetDeleteBody("id-1", "example", scope)},
				ipSetRequest{
					"CreateIPSet", "new", newRegion, ipSetCreateBody("example", scope, "IPV4", []any{}),
				},
			)
			newReads := ipSetReadCalls("new", newRegion, scope, "example", "id-2")
			require.Equal(t, append(expected, newReads...), cloud.takeCalls())
			result := ipSetSnapshot(t, executor).Find(ipSetAddress).Outputs
			require.Equal(t, "id-2", result["id"])
			require.Equal(t, newRegion, result["region"])
			require.Equal(t, scope, result["scope"])
		})
	}
}

func TestIPSetRuntimeConstraintPhases(t *testing.T) {
	libraries := ipSetRuntimeLibrary(t)
	for _, tc := range []struct {
		field      string
		value      any
		diagnostic string
	}{
		{"scope", nil, "constraints[1]"},
		{"ip-address-version", "ipv4", "constraints[2]"},
		{"name-prefix", "prefix-", "constraints[0] (at-most-one-of [name, name-prefix])"},
		{"name", "invalid!", "constraints[3]"},
		{"description", strings.Repeat("é", 128) + "a", "description must be 1 to 256 bytes"},
		{"addresses", []any{nil}, "address must not be null"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			cloud := newIPSetCloud(t)
			executor := ipSetExecutor(t, "evaluated", libraries, cloud.endpoint(t, "primary"))
			applyIPSet(t, executor, runtime.DecisionCreate)
			cloud.takeCalls()
			applyIPSet(t, executor, runtime.DecisionNoOp)
			require.Equal(t, ipSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1"),
				cloud.takeCalls())
			saved := saveIPSetPlan(t, executor)
			ipSetStep(t, saved, runtime.DecisionNoOp)
			cloud.takeCalls()

			executor.Inputs[tc.field] = tc.value
			_, err := executor.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, ipSetAddress)
			require.ErrorContains(t, err, tc.diagnostic)
			require.Empty(t, cloud.takeCalls())

			historical := ipSetSnapshot(t, executor)
			historical.Find(ipSetAddress).Inputs[tc.field] = tc.value
			ipSetPutSnapshot(t, executor, historical)
			_, err = executor.Plan(t.Context())
			require.ErrorContains(t, err, ipSetAddress)
			require.ErrorContains(t, err, tc.diagnostic)
			read := ipSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1")
			require.Equal(t, read, cloud.takeCalls())

			executor.Destroy = true
			applyIPSet(t, executor, runtime.DecisionDestroy)
			want := append(slices.Clone(read), read...)
			want = append(want, ipSetRequest{
				"DeleteIPSet", "primary", "us-west-2", ipSetDeleteBody("id-1", "example", "REGIONAL"),
			})
			require.Equal(t, want, cloud.takeCalls())
		})
	}
}

func TestIPSetRuntimeAddressSetLimit(t *testing.T) {
	cloud := newIPSetCloud(t)
	executor := ipSetExecutor(t, "evaluated", ipSetRuntimeLibrary(t), cloud.endpoint(t, "primary"))
	addresses := make([]any, 0, 10003)
	for i := range 10000 {
		addresses = append(addresses, fmt.Sprintf("2001:db8::%x/128", i))
	}
	addresses = append(addresses, addresses[0], addresses[9999])
	executor.Inputs["addresses"] = addresses
	plan := saveIPSetPlan(t, executor)
	planned := ipSetStep(t, plan, runtime.DecisionCreate)
	require.Equal(t, addresses, planned.Inputs["addresses"])
	executor.Inputs["addresses"] = append(addresses, "2001:db8::ffff/128")
	_, err := executor.Plan(t.Context())
	require.ErrorContains(t, err, ipSetAddress)
	require.ErrorContains(t, err, "addresses must contain at most 10000 distinct strings")
	require.Empty(t, cloud.takeCalls())
}

func TestIPSetRuntimeDeferredReplacement(t *testing.T) {
	libraries := ipSetRuntimeLibrary(t)
	for _, next := range []string{"valid-next", "invalid!"} {
		t.Run(next, func(t *testing.T) {
			cloud := newIPSetCloud(t)
			url := cloud.endpoint(t, "primary")
			initial := ipSetExecutor(t, "evaluated", libraries, url)
			applyIPSet(t, initial, runtime.DecisionCreate)
			cloud.takeCalls()

			executor := ipSetExecutor(t, "deferred", libraries, url)
			executor.Store = initial.Store
			executor.Inputs = maps.Clone(initial.Inputs)
			executor.Inputs["name"] = next
			saved := saveIPSetPlan(t, executor)
			step := ipSetStep(t, saved, runtime.DecisionReplace)
			require.Equal(t, map[string][]string{"name": {"action.seed.name"}}, step.UnresolvedInputs)
			require.Equal(t, []string{"name"}, step.PendingReplacementReasons)
			read := ipSetReadCalls("primary", "us-west-2", "REGIONAL", "example", "id-1")
			require.Equal(t, read, cloud.takeCalls())
			_, err := executor.ApplyPlan(t.Context(), saved)
			if next == "invalid!" {
				require.ErrorContains(t, err, ipSetAddress)
				require.ErrorContains(t, err, "constraints[3]")
				require.Empty(t, cloud.takeCalls())
				require.Equal(t, "id-1", ipSetSnapshot(t, executor).Find(ipSetAddress).Outputs["id"])
				return
			}
			require.NoError(t, err)
			want := append(slices.Clone(read),
				ipSetRequest{
					"DeleteIPSet", "primary", "us-west-2",
					ipSetDeleteBody("id-1", "example", "REGIONAL"),
				},
				ipSetRequest{
					"CreateIPSet", "primary", "us-west-2",
					ipSetCreateBody(next, "REGIONAL", "IPV4", []any{}),
				},
			)
			newReads := ipSetReadCalls("primary", "us-west-2", "REGIONAL", next, "id-2")
			require.Equal(t, append(want, newReads...), cloud.takeCalls())
		})
	}
}

func TestIPSetRuntimeUpdateKeepsEffectiveRegion(t *testing.T) {
	cloud := newIPSetCloud(t)
	executor := ipSetExecutor(t, "evaluated", ipSetRuntimeLibrary(t), cloud.endpoint(t, "prior"))
	applyIPSet(t, executor, runtime.DecisionCreate)
	cloud.takeCalls()

	t.Setenv("AWS_REGION", "us-east-2")
	executor.Inputs["aws-config"] = map[string]any{"endpoint-url": cloud.endpoint(t, "current")}
	executor.Inputs["description"] = "updated"
	applyIPSet(t, executor, runtime.DecisionUpdate)
	update := ipSetDeleteBody("id-1", "example", "REGIONAL")
	update["Description"] = "updated"
	update["Addresses"] = []any{}
	want := ipSetReadCalls("prior", "us-west-2", "REGIONAL", "example", "id-1")
	want = append(want, ipSetRequest{"UpdateIPSet", "current", "us-west-2", update})
	want = append(want, ipSetReadCalls("current", "us-west-2", "REGIONAL", "example", "id-1")...)
	require.Equal(t, want, cloud.takeCalls())
}

func TestIPSetRuntimeRefreshOnlyRecreatesMissingSet(t *testing.T) {
	libraries := ipSetRuntimeLibrary(t)
	for _, tc := range []struct {
		name        string
		serviceCode string
		wrongName   bool
		diagnostic  string
	}{
		{name: "missing", serviceCode: "WAFNonexistentItemException"},
		{name: "permission", serviceCode: "AccessDeniedException", diagnostic: "AccessDeniedException"},
		{name: "conflicting identity", wrongName: true, diagnostic: "identity does not match"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := newIPSetCloud(t)
			executor := ipSetExecutor(t, "evaluated", libraries, cloud.endpoint(t, "primary"))
			applyIPSet(t, executor, runtime.DecisionCreate)
			cloud.takeCalls()
			cloud.mu.Lock()
			cloud.getError = tc.serviceCode
			if tc.wrongName {
				cloud.sets["us-west-2/REGIONAL/example"]["Name"] = "different"
			}
			cloud.mu.Unlock()
			if tc.diagnostic == "" {
				step := ipSetStep(t, saveIPSetPlan(t, executor), runtime.DecisionCreate)
				require.True(t, step.Gone())
			} else {
				_, err := executor.Plan(t.Context())
				require.ErrorContains(t, err, ipSetAddress)
				require.ErrorContains(t, err, tc.diagnostic)
			}
			require.Equal(t, []ipSetRequest{{
				"GetIPSet", "primary", "us-west-2", ipSetGetBody("id-1", "example", "REGIONAL"),
			}}, cloud.takeCalls())
		})
	}
}
