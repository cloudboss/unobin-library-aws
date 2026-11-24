package iam

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/iam"
)

type linkedSeed struct {
	NextService *string
	NextSuffix  *string
}
type linkedSeedOutput struct {
	Service *string
	Suffix  *string
}

func (s *linkedSeed) Run(context.Context, any) (*linkedSeedOutput, error) {
	return &linkedSeedOutput{Service: s.NextService, Suffix: s.NextSuffix}, nil
}

type linkedCloud struct {
	mu                           sync.Mutex
	endpoint                     string
	service, suffix, description string
	tags                         map[string]string
	requests                     []url.Values
	failureAction                string
	cleanupFailure               bool
	cancelFailure                func(context.Context)
	exists                       bool
}

func newLinkedCloud(t *testing.T) *linkedCloud {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	cloud := &linkedCloud{tags: map[string]string{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse IAM form: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		cloud.mu.Lock()
		defer cloud.mu.Unlock()
		cloud.requests = append(cloud.requests, r.PostForm)
		action := r.PostForm.Get("Action")
		w.Header().Set("Content-Type", "text/xml")
		if action == cloud.failureAction ||
			action == "DeleteServiceLinkedRole" && cloud.cleanupFailure {
			if action == cloud.failureAction && cloud.cancelFailure != nil {
				cloud.cancelFailure(r.Context())
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<ErrorResponse><Error><Code>InvalidInput</Code>
    <Message>initialization or cleanup failed</Message></Error></ErrorResponse>`)
			return
		}
		switch action {
		case "CreateServiceLinkedRole":
			cloud.exists = true
			cloud.service = r.PostForm.Get("AWSServiceName")
			cloud.suffix = r.PostForm.Get("CustomSuffix")
			if cloud.suffix == "" {
				cloud.suffix = "generated"
			}
			cloud.description = r.PostForm.Get("Description")
		case "UpdateRole":
			cloud.description = r.PostForm.Get("Description")
		case "TagRole":
			for i := 1; ; i++ {
				p := fmt.Sprintf("Tags.member.%d.", i)
				if _, ok := r.PostForm[p+"Key"]; !ok {
					break
				}
				cloud.tags[r.PostForm.Get(p+"Key")] = r.PostForm.Get(p + "Value")
			}
		case "UntagRole":
			for i := 1; ; i++ {
				p := fmt.Sprintf("TagKeys.member.%d", i)
				if _, ok := r.PostForm[p]; !ok {
					break
				}
				delete(cloud.tags, r.PostForm.Get(p))
			}
		case "DeleteServiceLinkedRole":
			cloud.exists = false
		case "GetRole":
		case "ListRoleTags":
			fmt.Fprint(w, "<ListRoleTagsResponse><ListRoleTagsResult><Tags>")
			for k, v := range cloud.tags {
				fmt.Fprintf(w, "<member><Key>%s</Key><Value>%s</Value></member>", k, v)
			}
			fmt.Fprint(w, "</Tags></ListRoleTagsResult></ListRoleTagsResponse>")
			return
		default:
			t.Errorf("unexpected IAM action %s", action)
		}
		fmt.Fprintf(w, "<%[1]sResponse><%[1]sResult>", action)
		if action == "CreateServiceLinkedRole" || action == "GetRole" {
			fmt.Fprintf(w, `<Role><Arn>arn:aws:iam::123:role/aws-service-role/%s/Assigned_%s</Arn>
    <RoleName>Assigned_%s</RoleName><RoleId>AROA123</RoleId><Path>/aws-service-role/%s/</Path>
    <Description>%s</Description><CreateDate>2026-01-02T03:04:05Z</CreateDate></Role>`,
				cloud.service, cloud.suffix, cloud.suffix, cloud.service, cloud.description)
		}
		fmt.Fprintf(w, "</%[1]sResult></%[1]sResponse>", action)
	}))
	t.Cleanup(server.Close)
	cloud.endpoint = server.URL
	return cloud
}

func (c *linkedCloud) takeRequests() []url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.requests
	c.requests = nil
	return out
}

func linkedLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.Schema = readLibrarySchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.service-linked-role": lib.Schema.Resources["service-linked-role"].Constraints,
	}
	return map[string]*runtime.Library{
		"iam": lib, "test": {Actions: map[string]runtime.ActionRegistration{
			"linked-seed": runtime.MakeAction[linkedSeed, *linkedSeedOutput, any](),
		}}}
}

func linkedExecutor(t *testing.T, fixture string, cloud *linkedCloud,
	libraries map[string]*runtime.Library) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "service-linked-role", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := syntax.ParseSource(path, source)
	require.NoError(t, err)
	store, err := local.NewStore(t.TempDir(), "linked", "test", encrypters.Noop{})
	require.NoError(t, err)
	body := &parsed.Factory.Body
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libraries), SyntaxSource: body,
		Libraries: libraries, Store: store,
		Inputs: map[string]any{"endpoint": cloud.endpoint, "service": "test.amazonaws.com",
			"suffix": nil, "description": nil, "tags": nil},
	}
}

func linkedApply(
	t *testing.T, exec *runtime.Executor, decision runtime.Decision,
) *runtime.PlanFile {
	t.Helper()
	saved := saveRoleLookupPlan(t, exec)
	require.Equal(t, decision, roleLookupStep(t, saved, "resource.linked").Decision)
	_, err := exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	return saved
}

func linkedActions(requests []url.Values) []string {
	result := []string{}
	for _, request := range requests {
		result = append(result, request.Get("Action"))
	}
	return result
}

func TestServiceLinkedRoleNativeRuntime(t *testing.T) {
	libraries := linkedLibraries(t)
	for _, invalid := range []any{nil, "invalid"} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			cloud := newLinkedCloud(t)
			exec := linkedExecutor(t, "evaluated", cloud, libraries)
			linkedApply(t, exec, runtime.DecisionCreate)
			cloud.takeRequests()
			linkedApply(t, exec, runtime.DecisionNoOp)
			require.Equal(t, []string{"GetRole", "ListRoleTags"},
				linkedActions(cloud.takeRequests()))
			saved := saveRoleLookupPlan(t, exec)
			cloud.takeRequests()
			exec.Inputs["service"] = invalid
			_, err := exec.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, "resource.linked: schema: constraints[")
			require.ErrorContains(t, err, "aws-service-name")
			require.Empty(t, cloud.takeRequests())
			snapshot, err := exec.Store.Current()
			require.NoError(t, err)
			snapshot.Find("resource.linked").Inputs["aws-service-name"] = invalid
			rev, writeErr := exec.Store.Write(snapshot)
			require.NoError(t, writeErr)
			require.NoError(t, exec.Store.SetCurrent(rev))
			_, err = exec.Plan(t.Context())
			require.ErrorContains(t, err, "resource.linked: schema: constraints[")
			require.Equal(t, []string{"GetRole", "ListRoleTags"}, linkedActions(cloud.takeRequests()))
			exec.Destroy = true
			linkedApply(t, exec, runtime.DecisionDestroy)
			require.Equal(t, []string{"GetRole", "ListRoleTags", "GetRole", "ListRoleTags",
				"DeleteServiceLinkedRole"}, linkedActions(cloud.takeRequests()))
		})
	}
}

func TestServiceLinkedRoleFailedCreateState(t *testing.T) {
	for _, mode := range []string{"TagRole", "GetRole", "ListRoleTags", "cancel TagRole",
		"cancel GetRole", "cancel ListRoleTags"} {
		action := strings.TrimPrefix(mode, "cancel ")
		for _, cleanupFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cleanup-fails=%t", mode, cleanupFails), func(t *testing.T) {
				cloud := newLinkedCloud(t)
				cloud.failureAction, cloud.cleanupFailure = action, cleanupFails
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if action != mode {
					cloud.cancelFailure = func(requestCtx context.Context) {
						cancel()
						<-requestCtx.Done()
					}
				}
				exec := linkedExecutor(t, "evaluated", cloud, linkedLibraries(t))
				exec.Inputs["tags"] = map[string]any{"owner": "test"}
				saved := saveRoleLookupPlan(t, exec)
				require.Equal(t, runtime.DecisionCreate, roleLookupStep(t, saved, "resource.linked").Decision)
				_, err := exec.ApplyPlan(ctx, saved)
				if action == mode {
					require.ErrorContains(t, err, "InvalidInput")
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
				if cleanupFails {
					require.ErrorContains(t, err, "compensating delete service-linked role")
				}
				snapshot, err := exec.Store.Current()
				assert.ErrorIs(t, err, state.ErrNoCurrent)
				assert.Nil(t, snapshot)
				want := []string{"CreateServiceLinkedRole", "TagRole"}
				if action != "TagRole" {
					want = append(want, "GetRole")
				}
				if action == "ListRoleTags" {
					want = append(want, "ListRoleTags")
				}
				want = append(want, "DeleteServiceLinkedRole")
				assert.Equal(t, want, linkedActions(cloud.takeRequests()))
				assert.Equal(t, cleanupFails, cloud.exists)
				if cleanupFails {
					cloud.failureAction, cloud.cleanupFailure = "", false
					region := "us-east-1"
					cfg := &awscfg.Configuration{Region: &region, EndpointURL: &cloud.endpoint}
					identity := "arn:aws:iam::123:role/aws-service-role/" + cloud.service +
						"/Assigned_" + cloud.suffix
					require.NoError(t, (&svc.ServiceLinkedRoleResource{}).Delete(t.Context(), cfg,
						runtime.Prior[svc.ServiceLinkedRoleResource,
							*svc.ServiceLinkedRoleResourceOutput, *awscfg.Configuration]{
							Outputs: &svc.ServiceLinkedRoleResourceOutput{Arn: identity},
						}))
					assert.False(t, cloud.exists)
					assert.Equal(t, []string{"DeleteServiceLinkedRole"},
						linkedActions(cloud.takeRequests()))
				}
			})
		}
	}
}

func TestServiceLinkedRoleReplacement(t *testing.T) {
	libraries := linkedLibraries(t)
	for _, tc := range []struct {
		name, service   string
		suffix, desired any
		decision        runtime.Decision
	}{
		{"service", "test.amazonaws.com", nil, nil, runtime.DecisionReplace},
		{"suffix", "test.amazonaws.com", "old", "new", runtime.DecisionReplace},
		{"empty normalization", "test.amazonaws.com", nil, "", runtime.DecisionNoOp},
		{"normal removal", "test.amazonaws.com", "old", nil, runtime.DecisionReplace},
		{"generated", "ecs.application-autoscaling.amazonaws.com", nil, nil, runtime.DecisionNoOp},
		{"autoscaling removal", "ecs.application-autoscaling.amazonaws.com",
			"old", nil, runtime.DecisionNoOp},
		{"autoscaling explicit", "ecs.application-autoscaling.amazonaws.com",
			"old", "new", runtime.DecisionReplace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := newLinkedCloud(t)
			exec := linkedExecutor(t, "evaluated", cloud, libraries)
			exec.Inputs["service"], exec.Inputs["suffix"] = tc.service, tc.suffix
			linkedApply(t, exec, runtime.DecisionCreate)
			cloud.takeRequests()
			exec.Inputs["suffix"] = tc.desired
			if tc.name == "service" {
				exec.Inputs["service"] = "other.amazonaws.com"
			}
			plan := linkedApply(t, exec, tc.decision)
			step := roleLookupStep(t, plan, "resource.linked")
			if tc.decision == runtime.DecisionReplace {
				field := "custom-suffix"
				if tc.name == "service" {
					field = "aws-service-name"
				}
				assert.Equal(t, []string{field}, step.ReplacementReasons)
				require.Contains(t, linkedActions(cloud.takeRequests()), "DeleteServiceLinkedRole")
			} else {
				require.Equal(t, []string{"GetRole", "ListRoleTags"},
					linkedActions(cloud.takeRequests()))
			}
		})
	}
}

func TestServiceLinkedRoleDeferred(t *testing.T) {
	libraries := linkedLibraries(t)
	for _, value := range []any{nil, "invalid", "other.amazonaws.com"} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			cloud := newLinkedCloud(t)
			initial := linkedExecutor(t, "evaluated", cloud, libraries)
			linkedApply(t, initial, runtime.DecisionCreate)
			cloud.takeRequests()
			exec := linkedExecutor(t, "deferred-service", cloud, libraries)
			exec.Store = initial.Store
			exec.Inputs["service"] = value
			saved := saveRoleLookupPlan(t, exec)
			step := roleLookupStep(t, saved, "resource.linked")
			assert.Equal(t, map[string][]string{"aws-service-name": {"action.seed.service"}},
				step.UnresolvedInputs)
			require.Equal(t, []string{"aws-service-name"}, step.PendingReplacementReasons)
			cloud.takeRequests()
			_, err := exec.ApplyPlan(t.Context(), saved)
			if value == nil || value == "invalid" {
				require.ErrorContains(t, err, "resource.linked: schema: constraints[")
				require.Empty(t, cloud.takeRequests())
			} else {
				require.NoError(t, err)
				require.Contains(t, linkedActions(cloud.takeRequests()), "DeleteServiceLinkedRole")
			}
		})
	}
	for _, suffix := range []any{nil, "new"} {
		t.Run("autoscaling "+fmt.Sprint(suffix), func(t *testing.T) {
			cloud := newLinkedCloud(t)
			initial := linkedExecutor(t, "evaluated", cloud, libraries)
			initial.Inputs["service"] = "ecs.application-autoscaling.amazonaws.com"
			initial.Inputs["suffix"] = "old"
			linkedApply(t, initial, runtime.DecisionCreate)
			cloud.takeRequests()
			exec := linkedExecutor(t, "deferred-suffix", cloud, libraries)
			exec.Store = initial.Store
			exec.Inputs["service"] = initial.Inputs["service"]
			exec.Inputs["suffix"] = suffix
			saved := saveRoleLookupPlan(t, exec)
			assert.Equal(t, map[string][]string{"custom-suffix": {"action.seed.suffix"}},
				roleLookupStep(t, saved, "resource.linked").UnresolvedInputs)
			cloud.takeRequests()
			_, err := exec.ApplyPlan(t.Context(), saved)
			require.NoError(t, err)
			actions := strings.Join(linkedActions(cloud.takeRequests()), ",")
			if suffix == nil {
				require.NotContains(t, actions, "DeleteServiceLinkedRole")
			} else {
				require.Contains(t, actions, "DeleteServiceLinkedRole")
			}
		})
	}
}

func TestServiceLinkedRoleRuntimeDrift(t *testing.T) {
	cloud := newLinkedCloud(t)
	exec := linkedExecutor(t, "evaluated", cloud, linkedLibraries(t))
	exec.Inputs["description"] = "initial"
	exec.Inputs["tags"] = map[string]any{"keep": "1", "change": "old", "drop": "yes"}
	linkedApply(t, exec, runtime.DecisionCreate)
	cloud.takeRequests()
	exec.Inputs["description"] = nil
	exec.Inputs["tags"] = map[string]any{"keep": "1", "change": "new", "add": ""}
	linkedApply(t, exec, runtime.DecisionUpdate)
	assert.Equal(t, []string{"GetRole", "ListRoleTags", "UntagRole",
		"TagRole", "UpdateRole", "GetRole", "ListRoleTags"}, linkedActions(cloud.takeRequests()))
	cloud.mu.Lock()
	cloud.description = "external"
	cloud.tags = map[string]string{"extra": "external"}
	cloud.mu.Unlock()
	linkedApply(t, exec, runtime.DecisionUpdate)
	assert.Equal(t, []string{"GetRole", "ListRoleTags", "UntagRole",
		"TagRole", "UpdateRole", "GetRole", "ListRoleTags"}, linkedActions(cloud.takeRequests()))
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"arn":       "arn:aws:iam::123:role/aws-service-role/test.amazonaws.com/Assigned_generated",
		"role-name": "Assigned_generated", "role-id": "AROA123",
		"path": "/aws-service-role/test.amazonaws.com/", "create-date": "2026-01-02T03:04:05Z",
		"description": "", "tags": map[string]any{"keep": "1", "change": "new", "add": ""},
		"aws-service-name": "test.amazonaws.com", "custom-suffix": "generated",
	}, snapshot.Find("resource.linked").Outputs)
}
