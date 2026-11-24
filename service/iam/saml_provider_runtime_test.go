package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

type samlSeed struct{ NextName, NextMetadata *string }
type samlSeedOutput struct{ Name, Metadata *string }

func (s *samlSeed) Run(context.Context, any) (*samlSeedOutput, error) {
	return &samlSeedOutput{Name: s.NextName, Metadata: s.NextMetadata}, nil
}

type samlCloud struct {
	mu                       sync.Mutex
	endpoint, name, metadata string
	tags                     map[string]string
	exists                   bool
	requests                 []url.Values
	failure, createARN       string
	uuid                     string
	until                    *string
	cleanupFailure           bool
	cancel                   context.CancelFunc
}

func newSAMLCloud(t *testing.T) *samlCloud {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	until := "2030-01-02T03:04:05Z"
	cloud := &samlCloud{tags: map[string]string{}, uuid: "uuid", until: &until}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse IAM request: %v", err)
			return
		}
		cloud.mu.Lock()
		defer cloud.mu.Unlock()
		cloud.requests = append(cloud.requests, r.PostForm)
		action := r.PostForm.Get("Action")
		w.Header().Set("Content-Type", "text/xml")
		fail := action == cloud.failure || action == "DeleteSAMLProvider" && cloud.cleanupFailure
		if action == "CreateSAMLProvider" && cloud.failure == "TagSAMLProvider" &&
			r.PostForm.Has("Tags.member.1.Key") {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<ErrorResponse><Error><Code>AccessDenied</Code>`+
				`<Message>unsupported tags</Message></Error></ErrorResponse>`)
			return
		}
		if fail {
			if cloud.cancel != nil && action == cloud.failure {
				cloud.cancel()
				<-r.Context().Done()
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<ErrorResponse><Error><Code>InvalidInput</Code>`+
				`<Message>initialization or cleanup failed</Message></Error></ErrorResponse>`)
			return
		}
		if action == "GetSAMLProvider" && !cloud.exists {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `<ErrorResponse><Error><Code>NoSuchEntity</Code>`+
				`<Message>gone</Message></Error></ErrorResponse>`)
			return
		}
		fields := ""
		switch action {
		case "CreateSAMLProvider":
			cloud.exists = true
			cloud.name, cloud.metadata = r.PostForm.Get("Name"), r.PostForm.Get("SAMLMetadataDocument")
			cloud.tags = samlFormTags(r.PostForm)
			arn := cloud.arn()
			if cloud.createARN != "" {
				arn = cloud.createARN
			}
			fields = "<SAMLProviderArn>" + html.EscapeString(arn) + "</SAMLProviderArn>"
		case "UpdateSAMLProvider":
			cloud.metadata = r.PostForm.Get("SAMLMetadataDocument")
		case "TagSAMLProvider":
			maps.Copy(cloud.tags, samlFormTags(r.PostForm))
		case "UntagSAMLProvider":
			for i := 1; ; i++ {
				key := fmt.Sprintf("TagKeys.member.%d", i)
				if !r.PostForm.Has(key) {
					break
				}
				delete(cloud.tags, r.PostForm.Get(key))
			}
		case "DeleteSAMLProvider":
			cloud.exists = false
		case "GetSAMLProvider":
			fields = "<SAMLMetadataDocument>" + html.EscapeString(cloud.metadata) +
				"</SAMLMetadataDocument><SAMLProviderUUID>" + cloud.uuid + "</SAMLProviderUUID>"
			if cloud.until != nil {
				fields += "<ValidUntil>" + *cloud.until + "</ValidUntil>"
			}
			fields += "<Tags>"
			for k, v := range cloud.tags {
				fields += "<member><Key>" + html.EscapeString(k) + "</Key><Value>" +
					html.EscapeString(v) + "</Value></member>"
			}
			fields += "</Tags>"
		default:
			t.Errorf("unexpected IAM action %s", action)
		}
		fmt.Fprintf(w, "<%[1]sResponse><%[1]sResult>%s</%[1]sResult></%[1]sResponse>",
			action, fields)
	}))
	t.Cleanup(server.Close)
	cloud.endpoint = server.URL
	return cloud
}

func samlFormTags(form url.Values) map[string]string {
	tags := map[string]string{}
	for i := 1; ; i++ {
		prefix := fmt.Sprintf("Tags.member.%d.", i)
		if !form.Has(prefix + "Key") {
			return tags
		}
		tags[form.Get(prefix+"Key")] = form.Get(prefix + "Value")
	}
}

func (c *samlCloud) arn() string { return "arn:aws:iam::123456789012:saml-provider/" + c.name }

func (c *samlCloud) take() []url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	requests := c.requests
	c.requests = nil
	return requests
}

func samlActionNames(requests []url.Values) []string {
	out := []string{}
	for _, request := range requests {
		out = append(out, request.Get("Action"))
	}
	return out
}

func samlLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.Schema = readLibrarySchema(t)
	schema := lib.Schema.Resources["saml-provider"]
	schema.EvaluatedInputs = schema.Inputs
	lib.Constraints = map[string][]lang.ConstraintSpec{"resource.saml-provider": schema.Constraints}
	return map[string]*runtime.Library{"iam": lib,
		"test": {Actions: map[string]runtime.ActionRegistration{
			"saml-seed": runtime.MakeAction[samlSeed, *samlSeedOutput, any](),
		}}}
}

func samlExecutor(t *testing.T, fixture string, cloud *samlCloud,
	libraries map[string]*runtime.Library) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "saml-provider", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, err := syntax.ParseSource(path, source)
	require.NoError(t, err)
	store, err := local.NewStore(t.TempDir(), "saml", "test", encrypters.Noop{})
	require.NoError(t, err)
	body := &file.Factory.Body
	return &runtime.Executor{DAG: runtime.BuildSyntaxDAG(*body, libraries), SyntaxSource: body,
		Libraries: libraries, Store: store, Factory: state.FactoryInfo{Name: "saml", Version: "v0"},
		Inputs: map[string]any{"endpoint": cloud.endpoint, "name": "Example",
			"metadata": strings.Repeat("x", 1000), "tags": nil, "region": "us-east-1"}}
}

func samlSaved(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	body, err := runtime.SealPlan(plan, encrypters.Noop{})
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	return saved
}

func samlApply(t *testing.T, executor *runtime.Executor, decision runtime.Decision) {
	t.Helper()
	saved := samlSaved(t, executor)
	require.Equal(t, decision, roleLookupStep(t, saved, "resource.provider").Decision)
	_, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
}

func requireSAMLViolation(t *testing.T, err error, field string, null bool) {
	t.Helper()
	require.ErrorContains(t, err, "resource.provider")
	if null {
		require.ErrorContains(t, err, "required but is null")
		require.ErrorContains(t, err, `"`+field+`"`)
		return
	}
	require.ErrorContains(t, err, "schema: constraints[")
	require.ErrorContains(t, err, field+" must be")
}

func TestSAMLProviderNativeBounds(t *testing.T) {
	libraries := samlLibraries(t)
	cases := []struct {
		name, field string
		value       any
		valid       bool
	}{
		{"name-min", "name", "a", true},
		{"name-max", "name", strings.Repeat("é", 64), true},
		{"name-empty", "name", "", false},
		{"name-129", "name", strings.Repeat("x", 129), false},
		{"name-byte", "name", strings.Repeat("é", 65), false},
		{"name-null", "name", nil, false},
		{"metadata-min", "metadata", strings.Repeat("é", 500), true},
		{"metadata-max", "metadata", strings.Repeat("é", 5000000), true},
		{"metadata-999", "metadata", strings.Repeat("x", 999), false},
		{"metadata-10000001", "metadata", strings.Repeat("x", 10000001), false},
		{"metadata-byte-max", "metadata", strings.Repeat("é", 5000000) + "x", false},
		{"metadata-byte", "metadata", strings.Repeat("é", 499), false},
		{"metadata-null", "metadata", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloud := newSAMLCloud(t)
			executor := samlExecutor(t, "evaluated", cloud, libraries)
			executor.Inputs[tc.field] = tc.value
			_, err := executor.Plan(t.Context())
			if tc.valid {
				require.NoError(t, err)
			} else {
				field := tc.field
				if field == "metadata" {
					field = "saml-metadata-document"
				}
				requireSAMLViolation(t, err, field, tc.value == nil)
			}
			require.Empty(t, cloud.take())
		})
	}
}

func TestSAMLProviderNativeLifecycle(t *testing.T) {
	cloud := newSAMLCloud(t)
	executor := samlExecutor(t, "evaluated", cloud, samlLibraries(t))
	executor.Inputs["tags"] = map[string]any{"keep": "1", "change": "old", "remove": "yes"}
	samlApply(t, executor, runtime.DecisionCreate)
	require.Equal(t, []string{"CreateSAMLProvider", "GetSAMLProvider"}, samlActionNames(cloud.take()))
	samlApply(t, executor, runtime.DecisionNoOp)
	require.Equal(t, []string{"GetSAMLProvider"}, samlActionNames(cloud.take()))
	executor.Inputs["metadata"] = strings.Repeat("raw & \n", 150)
	executor.Inputs["tags"] = map[string]any{"keep": "1", "change": "new", "add": ""}
	samlApply(t, executor, runtime.DecisionUpdate)
	require.Equal(t, []string{"GetSAMLProvider", "UntagSAMLProvider", "TagSAMLProvider",
		"UpdateSAMLProvider", "GetSAMLProvider"}, samlActionNames(cloud.take()))
	cloud.mu.Lock()
	cloud.metadata, cloud.tags = "external", map[string]string{"extra": "external"}
	cloud.mu.Unlock()
	samlApply(t, executor, runtime.DecisionUpdate)
	require.Equal(t, []string{"GetSAMLProvider", "UntagSAMLProvider", "TagSAMLProvider",
		"UpdateSAMLProvider", "GetSAMLProvider"}, samlActionNames(cloud.take()))
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	require.Equal(t, map[string]any{"arn": cloud.arn(), "name": "Example",
		"saml-metadata-document": executor.Inputs["metadata"], "saml-provider-uuid": "uuid",
		"valid-until": "2030-01-02T03:04:05Z", "tags": executor.Inputs["tags"]},
		snapshot.Find("resource.provider").Outputs)
	executor.Inputs["tags"] = nil
	samlApply(t, executor, runtime.DecisionUpdate)
	require.Equal(t, []string{"GetSAMLProvider", "UntagSAMLProvider", "GetSAMLProvider"},
		samlActionNames(cloud.take()))
	cloud.mu.Lock()
	cloud.uuid, cloud.until = "new-uuid", nil
	cloud.mu.Unlock()
	samlApply(t, executor, runtime.DecisionUpdate)
	require.Equal(t, []string{"GetSAMLProvider", "GetSAMLProvider"}, samlActionNames(cloud.take()))
	snapshot, err = executor.Store.Current()
	require.NoError(t, err)
	require.Equal(t, "new-uuid", snapshot.Find("resource.provider").Outputs["saml-provider-uuid"])
	require.Nil(t, snapshot.Find("resource.provider").Outputs["valid-until"])
	cloud.mu.Lock()
	cloud.tags["aws:system"] = "ignored"
	cloud.mu.Unlock()
	executor.Inputs["region"] = "us-west-2"
	samlApply(t, executor, runtime.DecisionNoOp)
	require.Equal(t, []string{"GetSAMLProvider"}, samlActionNames(cloud.take()))
	executor.Inputs["name"] = "Replacement"
	saved := samlSaved(t, executor)
	step := roleLookupStep(t, saved, "resource.provider")
	require.Equal(t, runtime.DecisionReplace, step.Decision)
	require.Equal(t, []string{"name"}, step.ReplacementReasons)
	cloud.take()
	_, err = executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	requests := cloud.take()
	require.Equal(t, []string{"GetSAMLProvider", "DeleteSAMLProvider",
		"CreateSAMLProvider", "GetSAMLProvider"},
		samlActionNames(requests))
	require.Equal(t, "arn:aws:iam::123456789012:saml-provider/Example",
		requests[1].Get("SAMLProviderArn"))
	cloud.mu.Lock()
	cloud.exists = false
	cloud.mu.Unlock()
	samlApply(t, executor, runtime.DecisionCreate)
	require.Equal(t, []string{"GetSAMLProvider", "CreateSAMLProvider", "GetSAMLProvider"},
		samlActionNames(cloud.take()))
}

func TestSAMLProviderNativeSavedNoOpAndHistoricalDestroy(t *testing.T) {
	libraries := samlLibraries(t)
	for _, tc := range []struct {
		field, rule string
		value       any
	}{
		{"name", "name", ""}, {"name", "name", nil},
		{"metadata", "saml-metadata-document", "short"},
		{"metadata", "saml-metadata-document", nil},
	} {
		t.Run(tc.field+fmt.Sprint(tc.value), func(t *testing.T) {
			cloud := newSAMLCloud(t)
			executor := samlExecutor(t, "evaluated", cloud, libraries)
			samlApply(t, executor, runtime.DecisionCreate)
			saved := samlSaved(t, executor)
			require.Equal(t, runtime.DecisionNoOp, roleLookupStep(t, saved, "resource.provider").Decision)
			cloud.take()
			executor.Inputs[tc.field] = tc.value
			_, err := executor.ApplyPlan(t.Context(), saved)
			requireSAMLViolation(t, err, tc.rule, tc.value == nil)
			require.Empty(t, cloud.take())
			snapshot, err := executor.Store.Current()
			require.NoError(t, err)
			snapshot.Find("resource.provider").Inputs[tc.rule] = tc.value
			revision, err := executor.Store.Write(snapshot)
			require.NoError(t, err)
			require.NoError(t, executor.Store.SetCurrent(revision))
			_, err = executor.Plan(t.Context())
			requireSAMLViolation(t, err, tc.rule, tc.value == nil)
			if tc.value == nil {
				require.Empty(t, cloud.take())
			} else {
				require.Equal(t, []string{"GetSAMLProvider"}, samlActionNames(cloud.take()))
			}
			executor.Destroy = true
			samlApply(t, executor, runtime.DecisionDestroy)
			require.Equal(t, []string{"GetSAMLProvider", "GetSAMLProvider", "DeleteSAMLProvider"},
				samlActionNames(cloud.take()))
		})
	}
}

func TestSAMLProviderNativeDeferred(t *testing.T) {
	libraries := samlLibraries(t)
	for _, field := range []string{"name", "metadata"} {
		for _, value := range []any{nil, "", "valid"} {
			t.Run(field+fmt.Sprint(value), func(t *testing.T) {
				cloud := newSAMLCloud(t)
				initial := samlExecutor(t, "evaluated", cloud, libraries)
				samlApply(t, initial, runtime.DecisionCreate)
				cloud.take()
				executor := samlExecutor(t, "deferred-"+field, cloud, libraries)
				executor.Store = initial.Store
				resolved := value
				if value == "valid" && field == "metadata" {
					resolved = strings.Repeat("new", 400)
				}
				executor.Inputs[field] = resolved
				saved := samlSaved(t, executor)
				step := roleLookupStep(t, saved, "resource.provider")
				rule := field
				if field == "metadata" {
					rule = "saml-metadata-document"
				}
				require.Equal(t, map[string][]string{rule: {"action.seed." + field}}, step.UnresolvedInputs)
				if field == "name" {
					require.Equal(t, []string{"name"}, step.PendingReplacementReasons)
				}
				cloud.take()
				_, err := executor.ApplyPlan(t.Context(), saved)
				if value != "valid" {
					requireSAMLViolation(t, err, rule, value == nil)
					require.Empty(t, cloud.take())
				} else {
					require.NoError(t, err)
					want := []string{"UpdateSAMLProvider", "GetSAMLProvider"}
					if field == "name" {
						want = []string{"GetSAMLProvider", "DeleteSAMLProvider",
							"CreateSAMLProvider", "GetSAMLProvider"}
					}
					require.Equal(t, want, samlActionNames(cloud.take()))
				}
			})
		}
	}
}

func TestSAMLProviderNativeFailedCreate(t *testing.T) {
	libraries := samlLibraries(t)
	for _, mode := range []string{"GetSAMLProvider", "TagSAMLProvider", "malformed",
		"cancel-read", "cancel-tag"} {
		for _, cleanupFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cleanup=%t", mode, cleanupFails), func(t *testing.T) {
				cloud := newSAMLCloud(t)
				cloud.cleanupFailure = cleanupFails
				cloud.failure = mode
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if mode == "malformed" {
					cloud.createARN = "malformed"
				}
				if strings.HasPrefix(mode, "cancel-") {
					cloud.failure, cloud.cancel = "GetSAMLProvider", cancel
					if mode == "cancel-tag" {
						cloud.failure = "TagSAMLProvider"
					}
				}
				executor := samlExecutor(t, "evaluated", cloud, libraries)
				executor.Inputs["tags"] = map[string]any{"owner": "test"}
				executor.Inputs["region"] = "us-iso-east-1"
				saved := samlSaved(t, executor)
				_, err := executor.ApplyPlan(ctx, saved)
				require.Error(t, err)
				if mode == "malformed" {
					require.ErrorContains(t, err, "parse SAML provider ARN")
				} else if strings.HasPrefix(mode, "cancel-") {
					require.ErrorContains(t, err, "canceled")
				} else {
					require.ErrorContains(t, err, "InvalidInput")
				}
				if cleanupFails {
					require.ErrorContains(t, err, "compensating delete")
				}
				want := []string{"CreateSAMLProvider", "GetSAMLProvider", "DeleteSAMLProvider"}
				if cloud.failure == "TagSAMLProvider" {
					want = []string{"CreateSAMLProvider", "CreateSAMLProvider",
						"TagSAMLProvider", "DeleteSAMLProvider"}
				}
				if mode == "malformed" {
					want = []string{"CreateSAMLProvider", "DeleteSAMLProvider"}
				}
				require.Equal(t, want, samlActionNames(cloud.take()))
				require.Equal(t, cleanupFails, cloud.exists)
				snapshot, err := executor.Store.Current()
				require.ErrorIs(t, err, state.ErrNoCurrent)
				require.Nil(t, snapshot)
			})
		}
	}
}

func TestSAMLProviderCompiledSavedEnvelope(t *testing.T) {
	cloud := newSAMLCloud(t)
	build := t.TempDir()
	module, unobin := libraryModuleRoot(t), unobinModuleRoot(t)
	result, err := compile.RunResult(compile.Options{
		FactoryPath: filepath.Join("testdata", "ub", "saml-provider", "compiled", "factory.ub"),
		OutDir:      build, CLIVersion: "v0.13.0", GoVersion: "1.26.2", Build: true,
		StackName: "saml", Version: "v0.0.0", ReplaceUnobin: unobin.Dir,
		ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(directory string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(directory), nil
		}, Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join("testdata", "ub", "saml-provider", "compiled", "config.ub"))
	require.NoError(t, err)
	config := strings.ReplaceAll(string(body), "__ENDPOINT__", cloud.endpoint)
	config = strings.ReplaceAll(config, "__REVISION__", result.ContentRevision)
	require.NoError(t, os.WriteFile(filepath.Join(build, "config.ub"), []byte(config), 0o600))
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
		cmd.Dir = build
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	openPlan := func() *runtime.PlanFile {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(build, "saved.ubp"))
		require.NoError(t, err)
		saved, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
			return encrypters.Noop{}, nil
		})
		require.NoError(t, err)
		return saved
	}
	run("plan", "-c", "config.ub", "-o", "saved.ubp")
	require.Equal(t, runtime.DecisionCreate,
		roleLookupStep(t, openPlan(), "resource.provider").Decision)
	run("apply", "saved.ubp")
	require.Equal(t, []string{"CreateSAMLProvider", "GetSAMLProvider"}, samlActionNames(cloud.take()))
	run("plan", "-c", "config.ub", "-o", "saved.ubp")
	require.Equal(t, runtime.DecisionNoOp, roleLookupStep(t, openPlan(), "resource.provider").Decision)
	run("apply", "saved.ubp")
	require.Equal(t, []string{"GetSAMLProvider"}, samlActionNames(cloud.take()))
	run("plan", "-c", "config.ub", "-o", "saved.ubp")
	cloud.take()
	for _, tc := range []struct {
		field, input string
		value        any
	}{
		{"name", "name", ""}, {"name", "name", nil},
		{"saml-metadata-document", "metadata", "short"},
		{"saml-metadata-document", "metadata", nil},
	} {
		saved := openPlan()
		saved.Inputs[tc.input] = tc.value
		for i := range saved.Steps {
			if saved.Steps[i].Address == "resource.provider" {
				saved.Steps[i].Inputs[tc.field] = tc.value
			}
		}
		body, err := json.Marshal(saved)
		require.NoError(t, err)
		envelope, err := state.Seal(body, state.PayloadTypePlan, encrypters.Noop{})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(build, "invalid.ubp"), envelope, 0o600))
		cmd := exec.CommandContext(t.Context(), result.BinaryPath, "apply", "invalid.ubp")
		cmd.Dir = build
		out, err := cmd.CombinedOutput()
		require.Error(t, err, string(out))
		require.Contains(t, string(out), "resource.provider")
		require.Contains(t, string(out), tc.field)
		if tc.value == nil {
			require.Contains(t, string(out), "required but is null")
		} else {
			require.Contains(t, string(out), "schema: constraints[")
		}
		require.Empty(t, cloud.take())
	}
	invalid := strings.Replace(config, "'Example'", "''", 1)
	require.NoError(t, os.WriteFile(filepath.Join(build, "config.ub"), []byte(invalid), 0o600))
	cmd := exec.CommandContext(t.Context(), result.BinaryPath,
		"plan", "-c", "config.ub", "-o", "bad.ubp")
	cmd.Dir = build
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(out), "resource.provider")
	require.Contains(t, string(out), "name must be 1 to 128 bytes")
	require.NotContains(t, samlActionNames(cloud.take()), "DeleteSAMLProvider")
}

func TestSAMLProviderNativeSavedReplacementSafety(t *testing.T) {
	cloud := newSAMLCloud(t)
	executor := samlExecutor(t, "evaluated", cloud, samlLibraries(t))
	samlApply(t, executor, runtime.DecisionCreate)
	executor.Inputs["name"] = "replacement"
	saved := samlSaved(t, executor)
	require.Equal(t, runtime.DecisionReplace, roleLookupStep(t, saved, "resource.provider").Decision)
	cloud.take()
	executor.Inputs["metadata"] = "short"
	_, err := executor.ApplyPlan(t.Context(), saved)
	requireSAMLViolation(t, err, "saml-metadata-document", false)
	require.Empty(t, cloud.take())
	require.True(t, cloud.exists)
}

func TestSAMLProviderNativeDeferredCreate(t *testing.T) {
	libraries := samlLibraries(t)
	for _, value := range []any{nil, "", "Example"} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			cloud := newSAMLCloud(t)
			executor := samlExecutor(t, "deferred-name", cloud, libraries)
			executor.Inputs["name"] = value
			saved := samlSaved(t, executor)
			step := roleLookupStep(t, saved, "resource.provider")
			require.Equal(t, runtime.DecisionCreate, step.Decision)
			require.Equal(t, map[string][]string{"name": {"action.seed.name"}}, step.UnresolvedInputs)
			require.Empty(t, cloud.take())
			_, err := executor.ApplyPlan(t.Context(), saved)
			if value == "Example" {
				require.NoError(t, err)
				require.Equal(t, []string{"CreateSAMLProvider", "GetSAMLProvider"},
					samlActionNames(cloud.take()))
			} else {
				requireSAMLViolation(t, err, "name", value == nil)
				require.Empty(t, cloud.take())
			}
		})
	}
}

func TestSAMLProviderNativeNullTagValue(t *testing.T) {
	cloud := newSAMLCloud(t)
	executor := samlExecutor(t, "evaluated", cloud, samlLibraries(t))
	executor.Inputs["tags"] = map[string]any{"bad": nil}
	_, err := executor.Plan(t.Context())
	require.ErrorContains(t, err, "resource.provider")
	require.ErrorContains(t, err, `tags[\"bad\"]`)
	require.ErrorContains(t, err, "required but is null")
	require.Empty(t, cloud.take())
}
