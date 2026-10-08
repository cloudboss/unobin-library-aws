package cloudtrail

import (
	"context"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trailPlanCounters struct {
	creates int
	updates int
	deletes int
}

type trailPlanProbe struct {
	Name         string `ub:"name"`
	S3BucketName string `ub:"s3-bucket-name"`

	counters *trailPlanCounters
}

func (r *trailPlanProbe) Create(context.Context, any) (*TrailResourceOutput, error) {
	r.counters.creates++
	return &TrailResourceOutput{Arn: "arn:new"}, nil
}

func (r *trailPlanProbe) Read(
	_ context.Context, _ any, prior runtime.Prior[trailPlanProbe, *TrailResourceOutput, any],
) (*TrailResourceOutput, error) {
	return prior.Outputs, nil
}

func (r *trailPlanProbe) Update(
	_ context.Context,
	_ any,
	prior runtime.Prior[trailPlanProbe, *TrailResourceOutput, any],
) (*TrailResourceOutput, error) {
	r.counters.updates++
	return prior.Outputs, nil
}

func (r *trailPlanProbe) Delete(
	context.Context, any, runtime.Prior[trailPlanProbe, *TrailResourceOutput, any],
) error {
	r.counters.deletes++
	return nil
}

func (r *trailPlanProbe) ResourceDefinition() runtime.ResourceDefinition[
	trailPlanProbe, *TrailResourceOutput, any,
] {
	return runtime.ResourceDefinition[trailPlanProbe, *TrailResourceOutput, any]{
		SchemaVersion: 1,
		Replace: runtime.Replacement[trailPlanProbe, *TrailResourceOutput, any]{
			Fields: []runtime.AnyInputField[trailPlanProbe]{
				runtime.InputField(func(input *trailPlanProbe) *string { return &input.Name }),
			},
		},
	}
}

func TestTrailNameChangePlansAndAppliesReplacement(t *testing.T) {
	const source = `factory: {
  resources: {
    trail: aws-cloudtrail.trail {
      name:           'new-name'
      s3-bucket-name: 'logs-bucket'
    }
  }
}`
	parsed, err := syntax.ParseSource("factory.ub", []byte(source))
	require.NoError(t, err)
	require.NotNil(t, parsed.Factory)
	body := parsed.Factory.Body

	counters := &trailPlanCounters{}
	libraries := map[string]*runtime.Library{
		"aws-cloudtrail": {
			Name: "aws-cloudtrail",
			Resources: map[string]runtime.ResourceRegistration{
				"trail": runtime.MakeResourceWith[trailPlanProbe, *TrailResourceOutput, any](
					(&trailPlanProbe{}).ResourceDefinition(),
					func() *trailPlanProbe { return &trailPlanProbe{counters: counters} },
				),
			},
		},
	}
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	factory := state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"}
	snapshot := state.NewSnapshot(factory, store.Stack())
	snapshot.Entries = []*state.Entry{{
		Address:       "resource.trail",
		Composite:     false,
		Category:      "resource",
		Binding:       &state.Binding{Alias: "aws-cloudtrail", Export: "trail"},
		SchemaVersion: 1,
		Inputs: map[string]any{
			"name": "old-name", "s3-bucket-name": "logs-bucket",
		},
		Outputs: map[string]any{"arn": "arn:old"},
	}}
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))

	executor := &runtime.Executor{
		DAG:          runtime.BuildSyntaxDAG(body, libraries),
		SyntaxSource: &body,
		Libraries:    libraries,
		Store:        store,
		Factory:      factory,
	}
	plan, err := executor.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Steps, 1)
	assert.Equal(t, runtime.DecisionReplace, plan.Steps[0].Decision)
	assert.Equal(t, []string{"name"}, plan.Steps[0].ReplacementReasons)

	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	planFile, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	_, err = executor.ApplyPlan(context.Background(), planFile)
	require.NoError(t, err)
	assert.Equal(t, 1, counters.creates)
	assert.Equal(t, 1, counters.deletes)
	assert.Zero(t, counters.updates)
}
