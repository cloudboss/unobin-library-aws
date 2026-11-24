package codebuildbuild

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	awscodebuild "github.com/cloudboss/unobin-library-aws/service/codebuild"
)

var teardownCalls chan string

type parentResource struct {
	Name       string  `ub:"name"`
	Dependency *string `ub:"dependency"`
}
type parentOutput struct {
	Name string `ub:"name"`
}

func (r *parentResource) Create(context.Context, *awscfg.Configuration) (*parentOutput, error) {
	return &parentOutput{Name: r.Name}, nil
}
func (r *parentResource) Read(
	_ context.Context, _ *awscfg.Configuration,
	p runtime.Prior[parentResource, *parentOutput, *awscfg.Configuration],
) (*parentOutput, error) {
	return p.Outputs, nil
}
func (r *parentResource) Update(
	ctx context.Context, cfg *awscfg.Configuration,
	_ runtime.Prior[parentResource, *parentOutput, *awscfg.Configuration],
) (*parentOutput, error) {
	return r.Create(ctx, cfg)
}
func (r *parentResource) Delete(
	_ context.Context, _ *awscfg.Configuration,
	p runtime.Prior[parentResource, *parentOutput, *awscfg.Configuration],
) error {
	teardownCalls <- "delete " + p.Outputs.Name
	return nil
}

func fixtureModule(t *testing.T, name string) goschema.ModuleRoot {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", name).Output()
	require.NoError(t, err)
	return goschema.ModuleRoot{Path: name, Dir: strings.TrimSpace(string(out))}
}

func fixtureSavedPlan(t *testing.T, plan *runtime.Plan) *runtime.PlanFile {
	t.Helper()
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func TestFailedActionCleansOwnedActiveBuilds(t *testing.T) {
	for _, startError := range []bool{false, true} {
		t.Run(fmt.Sprint(startError), func(t *testing.T) {
			t.Setenv("AWS_ACCESS_KEY_ID", "test")
			t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
			t.Setenv("AWS_SESSION_TOKEN", "")
			t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
			teardownCalls = make(chan string, 30)
			stopped := map[string]bool{}
			stopPolls := map[string]int{}
			var persisted *local.Store
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				op := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "CodeBuild_20161006.")
				teardownCalls <- op
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				switch op {
				case "StartBuild":
					snapshot, err := persisted.Current()
					require.NoError(t, err)
					require.NotNil(t, snapshot.Find("resource.cleanup"))
					assert.Equal(t, map[string]any{"projectName": "project"}, body)
					if startError {
						w.WriteHeader(400)
						_, _ = w.Write([]byte(`{"__type":"InvalidInputException","message":"late"}`))
					} else {
						_, _ = w.Write([]byte(`{"build":{"id":"project:unreturned"}}`))
					}
				case "ListBuildsForProject":
					if body["nextToken"] == nil {
						assert.Equal(t, map[string]any{"projectName": "project"}, body)
						_, _ = w.Write([]byte(`{"ids":["project:unreturned"],"nextToken":"next"}`))
					} else {
						assert.Equal(t, map[string]any{"projectName": "project", "nextToken": "next"}, body)
						_, _ = w.Write([]byte(`{"ids":["project:second","foreign:active","project:done"]}`))
					}
				case "BatchGetBuilds":
					ids := body["ids"].([]any)
					id := ids[0].(string)
					status, project := "IN_PROGRESS", "project"
					if stopped[id] {
						stopPolls[id]++
						if stopPolls[id] > 1 {
							status = "STOPPED"
						}
					}
					if id == "project:done" {
						status = "SUCCEEDED"
					}
					if id == "foreign:active" {
						project = "foreign"
					}
					response := map[string]any{"builds": []any{map[string]any{
						"id": id, "projectName": project, "buildStatus": status,
					}}}
					require.NoError(t, json.NewEncoder(w).Encode(response))
				case "StopBuild":
					id := body["id"].(string)
					assert.Contains(t, []string{"project:unreturned", "project:second"}, id)
					stopped[id] = true
					_, _ = w.Write([]byte(`{}`))
				default:
					t.Errorf("unexpected operation %s", op)
				}
			}))
			defer server.Close()
			module, dep := fixtureModule(t, "github.com/cloudboss/unobin-library-aws"),
				fixtureModule(t, "github.com/cloudboss/unobin")
			schema, warnings, err := goschema.Read(".", module, dep)
			require.NoError(t, err)
			require.Empty(t, warnings)
			lib := runtime.LibraryWithPath(Library(),
				"github.com/cloudboss/unobin-library-aws//tests/integration/fixtures/codebuildbuild")
			lib.Schema = schema
			actionSchema, warnings, err := goschema.Read("../../../../service/codebuild", module, dep)
			require.NoError(t, err)
			require.Empty(t, warnings)
			action := runtime.LibraryWithPath(awscodebuild.Library(),
				"github.com/cloudboss/unobin-library-aws//service/codebuild")
			action.Schema = actionSchema
			action.Constraints = map[string][]lang.ConstraintSpec{
				"action.start-build": actionSchema.Actions["start-build"].Constraints,
			}
			libs := map[string]*runtime.Library{"fixture": lib, "codebuild": action,
				"test": {Resources: map[string]runtime.ResourceRegistration{
					"parent": runtime.MakeResource[
						parentResource, *parentOutput, *awscfg.Configuration,
					](runtime.ResourceDefinition[parentResource, *parentOutput,
						*awscfg.Configuration]{SchemaVersion: 1}),
				}}}
			path := filepath.Join("testdata", "ub", "failure", "factory.ub")
			source, err := os.ReadFile(path)
			require.NoError(t, err)
			file, _, err := compile.ParseFactorySyntaxSource(path, source)
			require.NoError(t, err)
			body := file.Factory.Body
			checker := check.NewSyntax(body, libs, nil, "")
			require.NoError(t, checker.References(nil).Err())
			store, err := local.NewStore(t.TempDir(), "failed-build", "test", encrypters.Noop{})
			require.NoError(t, err)
			persisted = store
			e := &runtime.Executor{DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs,
				Store: store, Inputs: map[string]any{"endpoint": server.URL},
				Factory: state.FactoryInfo{Name: "failed-build", Version: "v0", ContentRevision: "test"}}
			plan, err := e.Plan(t.Context())
			require.NoError(t, err)
			_, err = e.ApplyPlan(t.Context(), fixtureSavedPlan(t, plan))
			if startError {
				require.ErrorContains(t, err, "starting build")
			} else {
				require.ErrorContains(t, err, "timeout must be positive")
			}
			snapshot, err := store.Current()
			require.NoError(t, err)
			require.NotNil(t, snapshot.Find("resource.cleanup"))
			assert.Nil(t, snapshot.Find("action.build"))
			e.Destroy = true
			plan, err = e.Plan(t.Context())
			require.NoError(t, err)
			_, err = e.ApplyPlan(t.Context(), fixtureSavedPlan(t, plan))
			require.NoError(t, err)
			assert.Equal(t, map[string]bool{"project:unreturned": true, "project:second": true}, stopped)
			var calls []string
			for len(teardownCalls) > 0 {
				calls = append(calls, <-teardownCalls)
			}
			assert.Equal(t, []string{"StartBuild", "ListBuildsForProject", "ListBuildsForProject",
				"BatchGetBuilds", "StopBuild", "BatchGetBuilds", "BatchGetBuilds",
				"BatchGetBuilds", "StopBuild", "BatchGetBuilds", "BatchGetBuilds",
				"BatchGetBuilds", "BatchGetBuilds", "delete project", "delete role"}, calls)
		})
	}
}
