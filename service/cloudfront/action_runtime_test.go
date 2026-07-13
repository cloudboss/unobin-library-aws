package cloudfront

import (
	"context"
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/cloudfront"
)

type countingInvalidationRegistration struct {
	runs           int
	distributionID string
}

func (r *countingInvalidationRegistration) NewReceiver() any {
	return &svc.CreateInvalidationAction{}
}

func (r *countingInvalidationRegistration) Run(
	_ context.Context,
	receiver any,
	_ any,
) (any, error) {
	action := receiver.(*svc.CreateInvalidationAction)
	r.runs++
	r.distributionID = action.DistributionId
	return &svc.CreateInvalidationActionOutput{
		Id:     "INV123",
		Status: "Completed",
	}, nil
}

func (r *countingInvalidationRegistration) OutputType() reflect.Type {
	return reflect.TypeFor[*svc.CreateInvalidationActionOutput]()
}

func TestCreateInvalidationRuntimeReusesOutputsWhenSkipped(t *testing.T) {
	source := `factory: {
  description: 'runtime action test'
  actions: {
    invalidate: aws-cloudfront.create-invalidation {
      distribution-id: 'EDIST'
      paths: ['/']
    }
  }
  outputs: {
    invalidation-id:     { value: action.invalidate.id }
    invalidation-status: { value: action.invalidate.status }
  }
}`
	registration := &countingInvalidationRegistration{}
	lib := Library()
	lib.Configuration = nil
	lib.Actions["create-invalidation"] = registration
	libraries := map[string]*runtime.Library{"aws-cloudfront": lib}
	store, err := local.NewStore(
		t.TempDir(), "cloudfront-runtime-test", "test", encrypters.Noop{},
	)
	require.NoError(t, err)
	factory := state.FactoryInfo{
		Name:            "cloudfront-runtime-test",
		Version:         "v0",
		ContentRevision: "test",
	}

	firstPlan, first := runInvalidationRuntime(t, source, libraries, store, factory, false)
	require.Equal(t, runtime.DecisionRerun, actionStep(t, firstPlan).Decision)
	assert.Equal(t, "INV123", first.Outputs["invalidation-id"])
	assert.Equal(t, "Completed", first.Outputs["invalidation-status"])

	secondPlan, second := runInvalidationRuntime(t, source, libraries, store, factory, false)
	secondAction := actionStep(t, secondPlan)
	require.Equal(t, runtime.DecisionSkip, secondAction.Decision)
	assert.Equal(t, map[string]any{
		"id":     "INV123",
		"status": "Completed",
	}, secondAction.PriorOutputs)
	assert.Equal(t, "INV123", second.Outputs["invalidation-id"])
	assert.Equal(t, "Completed", second.Outputs["invalidation-status"])
	assert.Equal(t, 1, registration.runs)
	assert.Equal(t, "EDIST", registration.distributionID)

	destroyPlan, _ := runInvalidationRuntime(t, source, libraries, store, factory, true)
	require.Equal(t, runtime.DecisionDestroy, actionStep(t, destroyPlan).Decision)
	assert.Equal(t, 1, registration.runs)
	snapshot, err := store.Current()
	require.NoError(t, err)
	assert.Empty(t, snapshot.Entries)
}

func runInvalidationRuntime(
	t *testing.T,
	source string,
	libraries map[string]*runtime.Library,
	store state.Backend,
	factory state.FactoryInfo,
	destroy bool,
) (*runtime.Plan, *runtime.ExecResult) {
	t.Helper()
	file, err := syntax.ParseSource("factory.ub", []byte(source))
	require.NoError(t, err)
	require.NotNil(t, file.Factory)
	body := file.Factory.Body
	executor := &runtime.Executor{
		DAG:          runtime.BuildSyntaxDAG(body, libraries),
		SyntaxSource: &body,
		Libraries:    libraries,
		Store:        store,
		Factory:      factory,
		Destroy:      destroy,
	}
	plan, err := executor.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	planFile, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	result, err := executor.ApplyPlan(context.Background(), planFile)
	require.NoError(t, err)
	return plan, result
}

func actionStep(t *testing.T, plan *runtime.Plan) *runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == "action.invalidate" {
			return step
		}
	}
	require.FailNow(t, "action step not found")
	return nil
}
