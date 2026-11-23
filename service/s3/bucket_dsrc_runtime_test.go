package s3_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"

	awss3 "github.com/cloudboss/unobin-library-aws/service/s3"
)

type bucketLookupValue struct {
	Name string
}

func (v *bucketLookupValue) Create(context.Context, any) (*bucketLookupValue, error) {
	return v, nil
}

func (v *bucketLookupValue) Read(
	_ context.Context, _ any, prior runtime.Prior[bucketLookupValue, *bucketLookupValue, any],
) (*bucketLookupValue, error) {
	return prior.Outputs, nil
}

func (v *bucketLookupValue) Update(
	context.Context, any, runtime.Prior[bucketLookupValue, *bucketLookupValue, any],
) (*bucketLookupValue, error) {
	return v, nil
}

func (*bucketLookupValue) Delete(
	context.Context, any, runtime.Prior[bucketLookupValue, *bucketLookupValue, any],
) error {
	return nil
}

type bucketLookupSeed struct {
	Name *string
}

type bucketLookupSeedOutput struct {
	Value *string
}

func (s *bucketLookupSeed) Run(context.Context, any) (*bucketLookupSeedOutput, error) {
	return &bucketLookupSeedOutput{Value: s.Name}, nil
}

func bucketLookupLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := awss3.Library()
	lib.Schema = readServiceSchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"data-source.bucket": lib.Schema.DataSources["bucket"].Constraints,
	}
	return map[string]*runtime.Library{
		"s3": lib,
		"test": {
			Resources: map[string]runtime.ResourceRegistration{
				"value": runtime.MakeResource(
					runtime.ResourceDefinition[bucketLookupValue, *bucketLookupValue, any]{
						SchemaVersion: 1,
					}),
			},
			Actions: map[string]runtime.ActionRegistration{
				"seed": runtime.MakeAction[bucketLookupSeed, *bucketLookupSeedOutput, any](),
			},
		},
	}
}

func bucketLookupExecutor(
	t *testing.T,
	fixture, endpoint string,
	libraries map[string]*runtime.Library,
	store state.Backend,
	lookup any,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join("testdata", "ub", "runtime", fixture, "factory.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	body := &f.Factory.Body
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, libraries), SyntaxSource: body, Libraries: libraries,
		Inputs: map[string]any{"endpoint": endpoint, "lookup": lookup},
		Store:  store,
	}
}

func saveBucketLookupPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func bucketLookupStep(t *testing.T, plan *runtime.PlanFile, address string) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == address {
			return step
		}
	}
	t.Fatalf("plan has no step %q", address)
	return runtime.PlanStep{}
}

func requireBucketConstraintViolation(t *testing.T, err error) {
	t.Helper()
	require.ErrorContains(t, err, "data-source.lookup: schema: constraints[0] (predicate)")
	require.ErrorContains(t, err, "bucket is required")
}

func TestBucketLookupNativePlan(t *testing.T) {
	api := newBucketLookupAPI(t, bucketLookupResponse{region: "us-east-1"})
	libraries := bucketLookupLibraries(t)
	store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
	require.NoError(t, err)
	exec := bucketLookupExecutor(t, "resolved", api.endpoint, libraries, store, "demo.bucket")
	want := map[string]any{"bucket": "demo.bucket", "domain": "demo.bucket.s3.amazonaws.com"}
	initial := saveBucketLookupPlan(t, exec)
	require.Equal(t, runtime.DecisionCreate,
		bucketLookupStep(t, initial, "resource.consumer").Decision)
	got, err := exec.ApplyPlan(t.Context(), initial)
	require.NoError(t, err)
	require.Equal(t, want, got.Outputs)
	api.requireCalls(t, bucketLookupCalls("demo.bucket", 2)...)

	unchanged := saveBucketLookupPlan(t, exec)
	consumer := bucketLookupStep(t, unchanged, "resource.consumer")
	require.Equal(t, runtime.DecisionNoOp, consumer.Decision)
	ds := bucketLookupStep(t, unchanged, "data-source.lookup")
	require.Equal(t, runtime.DecisionRead, ds.Decision)
	require.Equal(t, map[string]any{
		"arn":                         "arn:aws:s3:::demo.bucket",
		"bucket-region":               "us-east-1",
		"bucket-domain-name":          "demo.bucket.s3.amazonaws.com",
		"bucket-regional-domain-name": "demo.bucket.s3.us-east-1.amazonaws.com",
		"hosted-zone-id":              "Z3AQBSTGFYJSTF",
		"website-domain":              nil,
		"website-endpoint":            nil,
	}, ds.ObservedOutputs)
	got, err = exec.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	require.Equal(t, want, got.Outputs)
	api.requireCalls(t, bucketLookupCalls("demo.bucket", 2)...)

	invalid := bucketLookupExecutor(t, "resolved", api.endpoint, libraries, store, nil)
	_, err = invalid.Plan(t.Context())
	requireBucketConstraintViolation(t, err)
	api.requireCalls(t)

	changed := saveBucketLookupPlan(t, exec)
	api.setResponse(bucketLookupResponse{region: "us-west-2"})
	_, err = exec.ApplyPlan(t.Context(), changed)
	require.ErrorContains(t, err, "data source data-source.lookup changed since the plan was computed")
	api.requireCalls(t, bucketLookupCalls("demo.bucket", 2)...)
}

func TestBucketLookupNativeDeferredSavedApply(t *testing.T) {
	api := newBucketLookupAPI(t, bucketLookupResponse{region: "us-east-1"})
	libraries := bucketLookupLibraries(t)
	for _, tc := range []struct {
		name   string
		lookup any
	}{
		{name: "valid", lookup: "demo.bucket"},
		{name: "null", lookup: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := local.NewStore(t.TempDir(), "lookup", "test", encrypters.Noop{})
			require.NoError(t, err)
			initial := bucketLookupExecutor(
				t, "resolved", api.endpoint, libraries, store, "demo.bucket",
			)
			_, err = initial.ApplyPlan(t.Context(), saveBucketLookupPlan(t, initial))
			require.NoError(t, err)
			api.requireCalls(t, bucketLookupCalls("demo.bucket", 2)...)

			exec := bucketLookupExecutor(t, "deferred", api.endpoint, libraries, store, tc.lookup)
			saved := saveBucketLookupPlan(t, exec)
			ds := bucketLookupStep(t, saved, "data-source.lookup")
			require.Equal(t, runtime.DecisionRead, ds.Decision)
			require.Equal(t, map[string][]string{"bucket": {"action.seed.value"}},
				ds.UnresolvedInputs)
			require.Nil(t, ds.ObservedOutputs)
			api.requireCalls(t)

			got, err := exec.ApplyPlan(t.Context(), saved)
			if tc.lookup == nil {
				requireBucketConstraintViolation(t, err)
				api.requireCalls(t)
				snapshot, err := store.Current()
				require.NoError(t, err)
				consumer := snapshot.Find("resource.consumer")
				require.NotNil(t, consumer)
				require.Equal(t,
					map[string]any{"name": "demo.bucket.s3.amazonaws.com"}, consumer.Inputs)
				return
			}
			require.NoError(t, err)
			require.Equal(t, map[string]any{
				"bucket": "demo.bucket", "domain": "demo.bucket.s3.amazonaws.com",
			}, got.Outputs)
			api.requireCalls(t, bucketLookupCalls("demo.bucket", 1)...)
		})
	}
}
