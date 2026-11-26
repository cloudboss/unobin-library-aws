package eks_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	awseks "github.com/cloudboss/unobin-library-aws/service/eks"
)

const authAddress = "action.auth"
const authRule = "action.auth: schema: constraints[0] (predicate): name is required"

type authSeed struct {
	Echo any `ub:"echo"`
}

func (r *authSeed) Run(context.Context, *awscfg.Configuration) (map[string]any, error) {
	return map[string]any{"value": r.Echo}, nil
}

type authInvocation struct {
	runtime.ActionRegistration
	calls int
}

func (r *authInvocation) Run(ctx context.Context, receiver, cfg any) (any, error) {
	r.calls++
	return r.ActionRegistration.Run(ctx, receiver, cfg)
}

func clusterAuthEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "example-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "example-secret")
	t.Setenv("AWS_SESSION_TOKEN", "session-token")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("UNOBIN_EKS_AUTH_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
}

func clusterAuthExecutor(t *testing.T, fixture string) (
	*runtime.Executor, *local.Store, *authInvocation,
) {
	t.Helper()
	clusterAuthEnvironment(t)
	lib := runtime.LibraryWithPath(awseks.Library(),
		"github.com/cloudboss/unobin-library-aws//service/eks")
	lib.Schema = readLibrarySchema(t)
	// Exercise the extracted native rule independently of structural null checking.
	lib.Schema.Actions["cluster-auth"].EvaluatedInputs = nil
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"action.cluster-auth": lib.Schema.Actions["cluster-auth"].Constraints,
	}
	invocation := &authInvocation{ActionRegistration: lib.Actions["cluster-auth"]}
	lib.Actions["cluster-auth"] = invocation
	libs := map[string]*runtime.Library{
		"eks": lib,
		"test": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[authSeed, map[string]any, *awscfg.Configuration](),
		}},
	}
	path := filepath.Join("testdata", "ub", "cluster-auth", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	enc, err := encrypters.NewEnvKey("UNOBIN_EKS_AUTH_KEY")
	require.NoError(t, err)
	store, err := local.NewStore(t.TempDir(), "cluster-auth", "test", enc)
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "cluster-auth", Version: "v0", ContentRevision: "test"},
	}, store, invocation
}

func savedAuthPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func authStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == authAddress {
			return step
		}
	}
	t.Fatal("cluster-auth action absent")
	return runtime.PlanStep{}
}

func authInputs(trigger string) map[string]any {
	return map[string]any{
		"endpoint": "https://sts.invalid", "name": "", "trigger": trigger, "region": nil,
	}
}

func TestClusterAuthNativeSavedSkipAndDestroy(t *testing.T) {
	executor, store, invocation := clusterAuthExecutor(t, "evaluated")
	executor.Inputs = authInputs("fixed")
	first := savedAuthPlan(t, executor)
	assert.Zero(t, invocation.calls)
	assert.Equal(t, []string{"token"}, authStep(t, first).SensitiveOutputs)
	executor.Inputs = nil
	out, err := executor.ApplyPlan(t.Context(), first)
	require.NoError(t, err)
	token := out.Outputs["token"].(string)
	assert.Equal(t, "", out.Outputs["name"])
	assert.Equal(t, 1, invocation.calls)
	executor.Inputs = authInputs("fixed")
	skip := savedAuthPlan(t, executor)
	assert.Equal(t, runtime.DecisionSkip, authStep(t, skip).Decision)
	executor.Inputs = nil
	unchanged, err := executor.ApplyPlan(t.Context(), skip)
	require.NoError(t, err)
	assert.Equal(t, out.Outputs, unchanged.Outputs)
	assert.Equal(t, 1, invocation.calls)
	executor.Inputs = authInputs("fixed")
	skip = savedAuthPlan(t, executor)
	executor.Inputs["name"] = nil
	_, err = executor.Plan(t.Context())
	require.ErrorContains(t, err, authRule)
	_, err = executor.ApplyPlan(t.Context(), skip)
	require.ErrorContains(t, err, authRule)
	assert.Equal(t, 1, invocation.calls)

	snapshot, err := store.Current()
	require.NoError(t, err)
	assert.Equal(t, token, snapshot.Find(authAddress).Outputs["token"])
	assert.Equal(t, []string{"token"}, snapshot.Find(authAddress).SensitiveOutputs)
	enc, err := encrypters.NewEnvKey("UNOBIN_EKS_AUTH_KEY")
	require.NoError(t, err)
	reopened, err := local.NewStore(store.Root, "cluster-auth", "test", enc)
	require.NoError(t, err)
	restored, err := reopened.Current()
	require.NoError(t, err)
	assert.Equal(t, token, restored.Find(authAddress).Outputs["token"])
	checkEncrypted := func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		assert.NotContains(t, string(data), token)
		if filepath.Ext(path) == ".enc" {
			var envelope state.Envelope
			require.NoError(t, json.Unmarshal(data, &envelope))
			assert.Equal(t, "env-key", envelope.Encrypter.Name)
			assert.NotContains(t, string(envelope.Ciphertext), token)
		}
		return nil
	}
	require.NoError(t, filepath.WalkDir(store.Root, checkEncrypted))

	snapshot.Find(authAddress).Inputs["name"] = nil
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	executor.Destroy = true
	destroy := savedAuthPlan(t, executor)
	assert.Equal(t, runtime.DecisionDestroy, authStep(t, destroy).Decision)
	_, err = executor.ApplyPlan(t.Context(), destroy)
	require.NoError(t, err)
	assert.Equal(t, 1, invocation.calls)
	snapshot, err = store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find(authAddress))
}

func TestClusterAuthAlwaysAndFailure(t *testing.T) {
	executor, store, invocation := clusterAuthExecutor(t, "evaluated")
	for i := range 2 {
		executor.Inputs = authInputs("always")
		plan := savedAuthPlan(t, executor)
		assert.Equal(t, runtime.DecisionRerun, authStep(t, plan).Decision)
		assert.Equal(t, i, invocation.calls)
		executor.Inputs = nil
		_, err := executor.ApplyPlan(t.Context(), plan)
		require.NoError(t, err)
		assert.Equal(t, i+1, invocation.calls)
	}
	executor.Inputs = authInputs("always")
	executor.Inputs["region"] = "cn-north-1"
	plan := savedAuthPlan(t, executor)
	_, err := executor.ApplyPlan(t.Context(), plan)
	require.ErrorContains(t, err, "partition")
	assert.Equal(t, 3, invocation.calls)
	// Failed invocation retains the preceding successful token.
	snapshot, err := store.Current()
	require.NoError(t, err)
	assert.NotEmpty(t, snapshot.Find(authAddress).Outputs["token"])
}

func TestClusterAuthDeferredNativeName(t *testing.T) {
	for _, value := range []any{"nonexistent", "", nil} {
		t.Run("name="+fmtAuthName(value), func(t *testing.T) {
			executor, store, invocation := clusterAuthExecutor(t, "deferred")
			executor.Inputs = authInputs("fixed")
			executor.Inputs["name"] = value
			plan := savedAuthPlan(t, executor)
			assert.Equal(t, map[string][]string{"name": {"action.seed.value"}},
				authStep(t, plan).UnresolvedInputs)
			assert.Zero(t, invocation.calls)
			executor.Inputs = nil
			_, err := executor.ApplyPlan(t.Context(), plan)
			if value == nil {
				require.ErrorContains(t, err, authRule)
				assert.Zero(t, invocation.calls)
				snapshot, err := store.Current()
				if !errors.Is(err, state.ErrNoCurrent) {
					require.NoError(t, err)
					assert.Nil(t, snapshot.Find(authAddress))
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, invocation.calls)
		})
	}
}

func fmtAuthName(value any) string {
	if value == nil {
		return "null"
	}
	return value.(string)
}

func TestClusterAuthInitialFailureHasNoOutput(t *testing.T) {
	executor, store, invocation := clusterAuthExecutor(t, "evaluated")
	executor.Inputs = authInputs("always")
	executor.Inputs["region"] = "cn-north-1"
	plan := savedAuthPlan(t, executor)
	_, err := executor.ApplyPlan(t.Context(), plan)
	require.ErrorContains(t, err, "partition")
	assert.Equal(t, 1, invocation.calls)
	snapshot, err := store.Current()
	if !errors.Is(err, state.ErrNoCurrent) {
		require.NoError(t, err)
		assert.Nil(t, snapshot.Find(authAddress))
	}
}
