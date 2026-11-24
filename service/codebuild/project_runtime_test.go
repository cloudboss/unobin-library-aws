package codebuild

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

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type projectSeed struct {
	NextName  *string
	NextImage *string
	NextCache *map[string]any
	NextSets  map[string]any
}

type projectSeedOutput struct {
	Name  *string
	Image *string
	Cache *map[string]any
	Sets  map[string]any
}

func (s *projectSeed) Run(context.Context, any) (*projectSeedOutput, error) {
	return &projectSeedOutput{
		Name: s.NextName, Image: s.NextImage, Cache: s.NextCache, Sets: s.NextSets,
	}, nil
}

type projectCloud struct {
	mu           sync.Mutex
	endpoint     string
	project      map[string]any
	calls        []string
	requests     []map[string]any
	failure      string
	cleanupFails bool
	cancel       context.CancelFunc
	malformed    bool
}

func newProjectCloud(t *testing.T) *projectCloud {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	c := &projectCloud{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		action := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "CodeBuild_20161006.")
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
			return
		}
		c.calls, c.requests = append(c.calls, action), append(c.requests, in)
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		if action == c.failure || action == "DeleteProject" && c.cleanupFails {
			if c.cancel != nil && action == c.failure {
				c.cancel()
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"__type":"InvalidInputException","message":"injected failure"}`)
			return
		}
		response := map[string]any{}
		switch action {
		case "CreateProject":
			c.project = maps.Clone(in)
			c.project["arn"] = "arn:aws:codebuild:us-east-1:123456789012:project/" + in["name"].(string)
			c.project["projectVisibility"] = "PRIVATE"
			response["project"] = c.project
		case "BatchGetProjects":
			projects := []any{}
			if c.project != nil {
				p := maps.Clone(c.project)
				if c.malformed {
					delete(p, "environment")
				}
				projects = append(projects, p)
			}
			response["projects"] = projects
		case "UpdateProject":
			assert.Equal(t, c.project["name"], in["name"])
			for k, v := range in {
				if k != "name" {
					c.project[k] = v
				}
			}
			response["project"] = c.project
		case "UpdateProjectVisibility":
			c.project["projectVisibility"] = in["projectVisibility"]
		case "DeleteProject":
			c.project = nil
		default:
			t.Errorf("unexpected CodeBuild action %s", action)
		}
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	t.Cleanup(server.Close)
	c.endpoint = server.URL
	return c
}

func (c *projectCloud) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls, c.requests = nil, nil
	return calls
}

func projectLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.Schema = projectSchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.project": lib.Schema.Resources["project"].Constraints,
	}
	lib.Defaults = map[string][]lang.DefaultSpec{
		"resource.project": lib.Schema.Resources["project"].Defaults,
	}
	return map[string]*runtime.Library{"codebuild": lib,
		"test": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[projectSeed, *projectSeedOutput, any](),
		}}}
}

func projectExecutor(t *testing.T, fixture string, c *projectCloud,
	libs map[string]*runtime.Library) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata/ub/runtime", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	doc, err := syntax.ParseSource(path, source)
	require.NoError(t, err)
	store, err := local.NewStore(t.TempDir(), "project", "test", encrypters.Noop{})
	require.NoError(t, err)
	body := &doc.Factory.Body
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libs), Libraries: libs, SyntaxSource: body, Store: store,
		Inputs: map[string]any{
			"endpoint": c.endpoint, "name": "test", "image": "image",
			"compute": "BUILD_GENERAL1_SMALL", "certificate": nil, "description": nil,
			"timeout": int64(60), "clone-depth": nil, "source-type": "NO_SOURCE",
			"source-location": nil, "buildspec": "version: 0.2", "visibility": nil, "cache": nil,
			"docker-security-groups": nil, "vpc": nil,
			"secondary-artifacts": nil, "secondary-sources": nil,
			"secondary-source-versions": nil, "batch-compute-types": nil,
		},
	}
}

func projectPlan(t *testing.T, e *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := e.Plan(t.Context())
	require.NoError(t, err)
	bytes, err := runtime.SealPlan(plan, encrypters.Noop{})
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(bytes, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	return saved
}

func projectStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == "resource.project" {
			return step
		}
	}
	t.Fatal("missing project plan step")
	return runtime.PlanStep{}
}

func projectApply(t *testing.T, e *runtime.Executor, want runtime.Decision) {
	t.Helper()
	plan := projectPlan(t, e)
	require.Equal(t, want, projectStep(t, plan).Decision)
	_, err := e.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
}

func requireProjectConstraint(t *testing.T, err error, message string) {
	t.Helper()
	require.ErrorContains(t, err, "resource.project: schema: constraints[")
	require.ErrorContains(t, err, message)
}

func TestProjectNativeEnforcement(t *testing.T) {
	libs := projectLibraries(t)
	for _, tc := range []struct {
		field   string
		invalid any
		message string
	}{
		{"name", nil, "Name is required"}, {"name", "-bad", "name must start"},
		{"image", nil, "Environment.Image is required"}, {"compute", "small", "compute"},
		{"certificate", "CERT.PEM", "certificate must end"},
		{"description", strings.Repeat("é", 128), "255 bytes"},
		{"timeout", int64(4), "timeout-in-minutes"}, {"clone-depth", int64(-1), "git-clone-depth"},
		{"source-type", nil, "Source.Type is required"},
		{"cache", map[string]any{"type": "S3", "location": ""}, "S3 cache requires"},
	} {
		t.Run(tc.field+"/"+tc.message, func(t *testing.T) {
			c := newProjectCloud(t)
			e := projectExecutor(t, "evaluated", c, libs)
			e.Inputs[tc.field] = tc.invalid
			_, err := e.Plan(t.Context())
			requireProjectConstraint(t, err, tc.message)
			assert.Empty(t, c.take())
			e = projectExecutor(t, "evaluated", c, libs)
			projectApply(t, e, runtime.DecisionCreate)
			c.take()
			saved := projectPlan(t, e)
			assert.Equal(t, runtime.DecisionNoOp, projectStep(t, saved).Decision)
			c.take()
			e.Inputs[tc.field] = tc.invalid
			_, err = e.ApplyPlan(t.Context(), saved)
			requireProjectConstraint(t, err, tc.message)
			assert.Empty(t, c.take())
			_, err = e.Plan(t.Context())
			requireProjectConstraint(t, err, tc.message)
			assert.Equal(t, []string{"BatchGetProjects"}, c.take())
		})
	}
}

func TestProjectRuntimeReplacementAndDeferred(t *testing.T) {
	libs := projectLibraries(t)
	for _, fixture := range []string{"evaluated", "deferred"} {
		for _, invalid := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/invalid=%t", fixture, invalid), func(t *testing.T) {
				c := newProjectCloud(t)
				initial := projectExecutor(t, "evaluated", c, libs)
				projectApply(t, initial, runtime.DecisionCreate)
				c.take()
				e := projectExecutor(t, fixture, c, libs)
				e.Store, e.Inputs["name"] = initial.Store, "replacement"
				if invalid {
					e.Inputs["buildspec"] = ""
				}
				plan, err := e.Plan(t.Context())
				if invalid && fixture == "evaluated" {
					require.ErrorContains(t, err, "NO_SOURCE requires")
					assert.Equal(t, []string{"BatchGetProjects"}, c.take())
					return
				}
				require.NoError(t, err)
				encoded, err := runtime.EncodePlan(plan)
				require.NoError(t, err)
				saved, err := runtime.DecodePlan(encoded)
				require.NoError(t, err)
				c.take()
				_, err = e.ApplyPlan(t.Context(), saved)
				if invalid {
					require.ErrorContains(t, err, "NO_SOURCE requires")
					assert.Empty(t, c.take())
				} else {
					require.NoError(t, err)
					assert.Equal(t, []string{"BatchGetProjects", "DeleteProject",
						"CreateProject", "BatchGetProjects"}, c.take())
				}
			})
		}
	}
}

func TestProjectDeferredNativeCache(t *testing.T) {
	libs := projectLibraries(t)
	for _, location := range []string{"", "bucket/key"} {
		c := newProjectCloud(t)
		e := projectExecutor(t, "deferred", c, libs)
		e.Inputs["cache"] = map[string]any{"type": "S3", "location": location}
		saved := projectPlan(t, e)
		require.Contains(t, projectStep(t, saved).UnresolvedInputs, "cache")
		assert.Empty(t, c.take())
		_, err := e.ApplyPlan(t.Context(), saved)
		if location == "" {
			requireProjectConstraint(t, err, "S3 cache requires")
			assert.Empty(t, c.take())
		} else {
			require.NoError(t, err)
			assert.Equal(t, []string{"CreateProject", "BatchGetProjects"}, c.take())
		}
	}
}

func TestProjectNativeBatchComputeTypes(t *testing.T) {
	valid := []any{
		"BUILD_GENERAL1_SMALL", "BUILD_GENERAL1_MEDIUM", "BUILD_GENERAL1_LARGE",
		"BUILD_GENERAL1_XLARGE", "BUILD_GENERAL1_2XLARGE", "BUILD_LAMBDA_1GB", "BUILD_LAMBDA_2GB",
		"BUILD_LAMBDA_4GB", "BUILD_LAMBDA_8GB", "BUILD_LAMBDA_10GB", "ATTRIBUTE_BASED_COMPUTE",
		"CUSTOM_INSTANCE_TYPE", "BUILD_GENERAL1_SMALL",
	}
	invalid := append(append([]any{}, valid...), "custom")
	c := newProjectCloud(t)
	libs := projectLibraries(t)
	e := projectExecutor(t, "evaluated", c, libs)
	e.Inputs["batch-compute-types"] = invalid
	_, err := e.Plan(t.Context())
	requireProjectConstraint(t, err, "batch compute type must be a supported value")
	assert.Empty(t, c.take())
	e.Inputs["batch-compute-types"] = valid
	projectApply(t, e, runtime.DecisionCreate)
	assert.Equal(t, []string{"CreateProject", "BatchGetProjects"}, c.take())
	assert.Equal(t, map[string]any{
		"serviceRole": "arn:aws:iam::123456789012:role/build", "combineArtifacts": false,
		"restrictions": map[string]any{"computeTypesAllowed": valid},
	}, c.project["buildBatchConfig"])
	saved := projectPlan(t, e)
	require.Equal(t, runtime.DecisionNoOp, projectStep(t, saved).Decision)
	c.take()
	_, err = e.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Empty(t, c.take())
	saved = projectPlan(t, e)
	require.Equal(t, runtime.DecisionNoOp, projectStep(t, saved).Decision)
	c.take()
	e.Inputs["batch-compute-types"] = invalid
	_, err = e.ApplyPlan(t.Context(), saved)
	requireProjectConstraint(t, err, "batch compute type must be a supported value")
	assert.Empty(t, c.take())
	_, err = e.Plan(t.Context())
	requireProjectConstraint(t, err, "batch compute type must be a supported value")
	assert.Equal(t, []string{"BatchGetProjects"}, c.take())
	for _, accepted := range []bool{false, true} {
		deferred := projectExecutor(t, "deferred", c, libs)
		deferred.Store, deferred.Inputs["name"] = e.Store, "replacement"
		deferred.Inputs["batch-compute-types"] = invalid
		if accepted {
			deferred.Inputs["batch-compute-types"] = valid
		}
		saved := projectPlan(t, deferred)
		require.Contains(t, projectStep(t, saved).UnresolvedInputs, "build-batch-config")
		c.take()
		_, err = deferred.ApplyPlan(t.Context(), saved)
		if accepted {
			require.NoError(t, err)
			assert.Equal(t, []string{"BatchGetProjects", "DeleteProject", "CreateProject",
				"BatchGetProjects"}, c.take())
			batch := c.project["buildBatchConfig"].(map[string]any)
			restrictions := batch["restrictions"].(map[string]any)
			assert.Equal(t, valid, restrictions["computeTypesAllowed"])
		} else {
			requireProjectConstraint(t, err, "batch compute type must be a supported value")
			assert.Empty(t, c.take())
		}
	}
}

func TestProjectNativeSetCardinality(t *testing.T) {
	c := newProjectCloud(t)
	e := projectExecutor(t, "evaluated", c, projectLibraries(t))
	subnets, groups := []any{}, []any{}
	for range 20 {
		subnets = append(subnets, "subnet")
	}
	for range 8 {
		groups = append(groups, "sg")
	}
	e.Inputs["vpc"] = map[string]any{
		"vpc-id": "", "subnets": subnets, "security-group-ids": groups,
	}
	saved := projectPlan(t, e)
	_, err := e.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"vpcId": "", "subnets": []any{"subnet"},
		"securityGroupIds": []any{"sg"}}, c.project["vpcConfig"])
	c.take()
	for i := range groups {
		groups[i] = fmt.Sprint(i)
	}
	_, err = e.Plan(t.Context())
	requireProjectConstraint(t, err, "security groups or subnets")
	assert.Equal(t, []string{"BatchGetProjects"}, c.take())
}

func TestProjectNativeObjectSetCardinality(t *testing.T) {
	libs := projectLibraries(t)
	for _, field := range []string{"secondary-artifacts", "secondary-sources",
		"secondary-source-versions"} {
		t.Run(field, func(t *testing.T) {
			c := newProjectCloud(t)
			e := projectExecutor(t, "evaluated", c, libs)
			item := projectCollectionItem(field, "same")
			items := make([]any, 13)
			for i := range items {
				items[i] = maps.Clone(item)
			}
			e.Inputs[field] = items
			projectApply(t, e, runtime.DecisionCreate)
			assert.Equal(t, []string{"CreateProject", "BatchGetProjects"}, c.take())
			key, expected := "secondarySourceVersions", map[string]any{
				"sourceIdentifier": "same", "sourceVersion": "same",
			}
			switch field {
			case "secondary-artifacts":
				key, expected = "secondaryArtifacts", map[string]any{
					"type": "S3", "artifactIdentifier": "same", "namespaceType": "NONE",
					"packaging": "NONE", "encryptionDisabled": false, "overrideArtifactName": false,
				}
			case "secondary-sources":
				key, expected = "secondarySources", map[string]any{
					"type": "S3", "sourceIdentifier": "same", "buildspec": "",
					"gitCloneDepth": float64(0), "insecureSsl": false,
				}
			}
			assert.Equal(t, []any{expected}, c.project[key])
			for i := range items {
				items[i] = projectCollectionItem(field, fmt.Sprint(i))
			}
			_, err := e.Plan(t.Context())
			requireProjectConstraint(t, err, "secondary collections accept")
			assert.Equal(t, []string{"BatchGetProjects"}, c.take())
		})
	}
}

func projectCollectionItem(field, id string) map[string]any {
	switch field {
	case "secondary-artifacts":
		return map[string]any{"type": "S3", "artifact-identifier": id}
	case "secondary-sources":
		return map[string]any{"type": "S3", "source-identifier": id}
	default:
		return map[string]any{"source-identifier": "same", "source-version": id}
	}
}

func TestProjectConfigurationCardinality(t *testing.T) {
	libs := projectLibraries(t)
	for _, tc := range []struct {
		field, member string
		first, second any
	}{
		{"secondary-artifacts", "name", "first", "second"},
		{"secondary-artifacts", "namespace-type", nil, "NONE"},
		{"secondary-artifacts", "packaging", nil, "NONE"},
		{"secondary-artifacts", "encryption-disabled", nil, false},
		{"secondary-artifacts", "override-artifact-name", nil, false},
		{"secondary-artifacts", "bucket-owner-access", nil, "NONE"},
		{"secondary-artifacts", "location", nil, ""},
		{"secondary-artifacts", "path", nil, ""},
		{"secondary-sources", "buildspec", nil, ""},
		{"secondary-sources", "git-clone-depth", nil, int64(0)},
		{"secondary-sources", "insecure-ssl", nil, false},
		{"secondary-sources", "report-build-status", nil, false},
		{"secondary-sources", "location", nil, ""},
		{"secondary-sources", "build-status-config", nil, map[string]any{}},
		{"secondary-sources", "build-status-config", map[string]any{},
			map[string]any{"context": ""}},
		{"secondary-sources", "build-status-config", map[string]any{},
			map[string]any{"target-url": ""}},
		{"secondary-sources", "git-submodules-config", nil,
			map[string]any{"fetch-submodules": false}},
		{"secondary-sources", "git-submodules-config", map[string]any{"fetch-submodules": false},
			map[string]any{"fetch-submodules": true}},
		{"secondary-sources", "auth", map[string]any{"type": "OAUTH", "resource": ""},
			map[string]any{"type": "BASIC_AUTH", "resource": ""}},
		{"secondary-sources", "auth", map[string]any{"type": "OAUTH", "resource": ""},
			map[string]any{"type": "OAUTH", "resource": "arn:aws:other:::x"}},
	} {
		t.Run(tc.field+"/"+tc.member+fmt.Sprint(tc.first), func(t *testing.T) {
			c := newProjectCloud(t)
			e := projectExecutor(t, "evaluated", c, libs)
			items := []any{}
			for i := range 11 {
				items = append(items, projectCollectionItem(tc.field, fmt.Sprint(i)))
			}
			first, second := projectCollectionItem(tc.field, "pair"),
				projectCollectionItem(tc.field, "pair")
			first[tc.member], second[tc.member] = tc.first, tc.second
			duplicate := maps.Clone(first)
			if tc.first == nil {
				delete(first, tc.member)
				duplicate[tc.member] = nil
			}
			e.Inputs[tc.field] = append(items, first, duplicate)
			_, err := e.Plan(t.Context())
			require.NoError(t, err, "exact duplicates count once")
			e.Inputs[tc.field] = append(items, first, second)
			_, err = e.Plan(t.Context())
			requireProjectConstraint(t, err, "secondary collections accept")
			assert.Empty(t, c.take())
		})
	}
}

func TestProjectDeferredNativeNullBeforeReplacement(t *testing.T) {
	c := newProjectCloud(t)
	libs := projectLibraries(t)
	initial := projectExecutor(t, "evaluated", c, libs)
	projectApply(t, initial, runtime.DecisionCreate)
	c.take()
	e := projectExecutor(t, "deferred", c, libs)
	e.Store = initial.Store
	e.Inputs["name"], e.Inputs["image"] = "new-name", nil
	saved := projectPlan(t, e)
	require.Contains(t, projectStep(t, saved).UnresolvedInputs, "environment")
	c.take()
	_, err := e.ApplyPlan(t.Context(), saved)
	requireProjectConstraint(t, err, "Environment.Image is required")
	assert.Empty(t, c.take())
}

func TestProjectRuntimeManagedDriftAndCreateOnlyValidity(t *testing.T) {
	c := newProjectCloud(t)
	e := projectExecutor(t, "evaluated", c, projectLibraries(t))
	e.Inputs["description"] = "managed"
	projectApply(t, e, runtime.DecisionCreate)
	c.take()
	c.mu.Lock()
	c.project["description"] = "external"
	c.mu.Unlock()
	projectApply(t, e, runtime.DecisionUpdate)
	assert.Equal(t, []string{"BatchGetProjects", "UpdateProject", "BatchGetProjects"}, c.take())
	assert.Equal(t, "managed", c.project["description"])
	e.Inputs["buildspec"] = ""
	projectApply(t, e, runtime.DecisionUpdate)
	assert.Equal(t, []string{"BatchGetProjects", "UpdateProject", "BatchGetProjects"}, c.take())
	projectApply(t, e, runtime.DecisionNoOp)
	assert.Equal(t, []string{"BatchGetProjects"}, c.take())
}

func TestProjectHistoricalDestroy(t *testing.T) {
	c := newProjectCloud(t)
	e := projectExecutor(t, "evaluated", c, projectLibraries(t))
	projectApply(t, e, runtime.DecisionCreate)
	c.take()
	snapshot, err := e.Store.Current()
	require.NoError(t, err)
	entry := snapshot.Find("resource.project")
	entry.Inputs["name"], entry.Inputs["service-role"] = "-old-invalid", "invalid-arn"
	rev, err := e.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, e.Store.SetCurrent(rev))
	e.Inputs["name"], e.Destroy = nil, true
	projectApply(t, e, runtime.DecisionDestroy)
	assert.Equal(t, []string{"BatchGetProjects", "BatchGetProjects", "DeleteProject"}, c.take())
}

func TestProjectRuntimeFailedCreate(t *testing.T) {
	libs := projectLibraries(t)
	for _, mode := range []string{"UpdateProjectVisibility", "BatchGetProjects", "malformed"} {
		for _, cancelRequest := range []bool{false, true} {
			c := newProjectCloud(t)
			c.failure = mode
			c.malformed = mode == "malformed"
			e := projectExecutor(t, "evaluated", c, libs)
			e.Inputs["visibility"] = "PUBLIC_READ"
			saved := projectPlan(t, e)
			ctx, cancel := context.WithCancel(t.Context())
			if cancelRequest {
				c.cancel = cancel
			}
			_, err := e.ApplyPlan(ctx, saved)
			cancel()
			require.Error(t, err)
			calls := c.take()
			assert.Equal(t, "CreateProject", calls[0])
			assert.Equal(t, "DeleteProject", calls[len(calls)-1])
			assert.Nil(t, c.project)
			snapshot, err := e.Store.Current()
			assert.ErrorIs(t, err, state.ErrNoCurrent)
			assert.Nil(t, snapshot)
		}
	}
}

func TestProjectCardinalitySavedAndDeferred(t *testing.T) {
	libs := projectLibraries(t)
	for _, field := range []string{"secondary-artifacts", "secondary-sources",
		"secondary-source-versions"} {
		t.Run(field, func(t *testing.T) {
			c := newProjectCloud(t)
			e := projectExecutor(t, "evaluated", c, libs)
			duplicates, distinct := []any{}, []any{}
			for i := range 13 {
				duplicates = append(duplicates, projectCollectionItem(field, "same"))
				item := projectCollectionItem(field, fmt.Sprint(i))
				if field == "secondary-artifacts" {
					item["artifact-identifier"], item["name"] = "same", fmt.Sprint(i)
				}
				distinct = append(distinct, item)
			}
			e.Inputs[field] = duplicates
			projectApply(t, e, runtime.DecisionCreate)
			c.take()
			saved := projectPlan(t, e)
			require.Equal(t, runtime.DecisionNoOp, projectStep(t, saved).Decision)
			c.take()
			e.Inputs[field] = distinct
			_, err := e.ApplyPlan(t.Context(), saved)
			requireProjectConstraint(t, err, "secondary collections accept")
			assert.Empty(t, c.take())
			_, err = e.Plan(t.Context())
			requireProjectConstraint(t, err, "secondary collections accept")
			assert.Equal(t, []string{"BatchGetProjects"}, c.take())
			for _, valid := range []bool{false, true} {
				deferred := projectExecutor(t, "deferred", c, libs)
				deferred.Store, deferred.Inputs["name"] = e.Store, "replacement"
				deferred.Inputs[field] = distinct
				if valid {
					deferred.Inputs[field] = duplicates
				}
				saved := projectPlan(t, deferred)
				require.Contains(t, projectStep(t, saved).UnresolvedInputs, field)
				c.take()
				_, err := deferred.ApplyPlan(t.Context(), saved)
				if valid {
					require.NoError(t, err)
					assert.Equal(t, []string{"BatchGetProjects", "DeleteProject",
						"CreateProject", "BatchGetProjects"}, c.take())
				} else {
					requireProjectConstraint(t, err, "secondary collections accept")
					assert.Empty(t, c.take())
				}
			}
		})
	}
}

func TestProjectNativeNetworkCardinality(t *testing.T) {
	libs := projectLibraries(t)
	for _, tc := range []struct {
		field string
		max   int
	}{
		{"subnets", 16}, {"security-group-ids", 5}, {"docker-security-groups", 5},
	} {
		t.Run(tc.field, func(t *testing.T) {
			c := newProjectCloud(t)
			e := projectExecutor(t, "evaluated", c, libs)
			items := []any{}
			for i := range tc.max {
				items = append(items, fmt.Sprint(i))
			}
			set := func(items []any) {
				if tc.field == "docker-security-groups" {
					e.Inputs[tc.field] = items
				} else {
					vpc := map[string]any{"vpc-id": "v", "subnets": []any{},
						"security-group-ids": []any{}}
					vpc[tc.field] = items
					e.Inputs["vpc"] = vpc
				}
			}
			set(items)
			_, err := e.Plan(t.Context())
			require.NoError(t, err)
			set(append(items, items[0]))
			_, err = e.Plan(t.Context())
			if tc.field == "docker-security-groups" {
				requireProjectConstraint(t, err, "security groups or subnets")
				set([]any{"same", "same", "same", "same", "same", "same"})
				_, err = e.Plan(t.Context())
				requireProjectConstraint(t, err, "security groups or subnets")
			} else {
				require.NoError(t, err)
				set(append(items, "new"))
				_, err = e.Plan(t.Context())
				requireProjectConstraint(t, err, "security groups or subnets")
			}
			assert.Empty(t, c.take())
		})
	}
}

func TestProjectDomainParserBeforeMutation(t *testing.T) {
	libs := projectLibraries(t)
	for _, fixture := range []string{"evaluated", "deferred"} {
		t.Run(fixture, func(t *testing.T) {
			c := newProjectCloud(t)
			initial := projectExecutor(t, "evaluated", c, libs)
			projectApply(t, initial, runtime.DecisionCreate)
			c.take()
			e := projectExecutor(t, fixture, c, libs)
			e.Store, e.Inputs["name"] = initial.Store, "replacement"
			e.Inputs["secondary-sources"] = []any{map[string]any{
				"type": "S3", "source-identifier": "s",
				"auth": map[string]any{"type": "OAUTH", "resource": "invalid-arn"},
			}}
			plan, err := e.Plan(t.Context())
			if fixture == "evaluated" {
				require.ErrorContains(t, err, "invalid ARN")
				assert.Equal(t, []string{"BatchGetProjects"}, c.take())
			} else {
				require.NoError(t, err)
				c.take()
				encoded, err := runtime.EncodePlan(plan)
				require.NoError(t, err)
				saved, err := runtime.DecodePlan(encoded)
				require.NoError(t, err)
				_, err = e.ApplyPlan(t.Context(), saved)
				require.ErrorContains(t, err, "invalid ARN")
				assert.Empty(t, c.take())
			}
		})
	}
}

func TestProjectNativeFailedCreateCleanupError(t *testing.T) {
	c := newProjectCloud(t)
	c.failure, c.cleanupFails = "BatchGetProjects", true
	e := projectExecutor(t, "evaluated", c, projectLibraries(t))
	saved := projectPlan(t, e)
	_, err := e.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "get project")
	require.ErrorContains(t, err, "compensating delete project")
	assert.Equal(t, []string{"CreateProject", "BatchGetProjects", "DeleteProject"}, c.take())
	snapshot, err := e.Store.Current()
	assert.ErrorIs(t, err, state.ErrNoCurrent)
	assert.Nil(t, snapshot)
}
