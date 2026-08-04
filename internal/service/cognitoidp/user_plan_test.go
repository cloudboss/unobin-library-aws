package cognitoidp

import (
	"context"
	"fmt"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type userPlanProbe UserResource

func (*userPlanProbe) SchemaVersion() int { return 1 }

func (*userPlanProbe) Create(context.Context, any) (map[string]any, error) {
	return map[string]any{"user-pool-id": "us-east-1_example", "username": "alice"}, nil
}

func (*userPlanProbe) Read(
	_ context.Context,
	_ any,
	prior map[string]any,
) (map[string]any, error) {
	return prior, nil
}

func (*userPlanProbe) Update(
	_ context.Context,
	_ any,
	prior runtime.Prior[userPlanProbe, map[string]any],
) (map[string]any, error) {
	return prior.Outputs, nil
}

func (*userPlanProbe) Delete(context.Context, any, map[string]any) error {
	return nil
}

func (*userPlanProbe) ReplaceFields() []string {
	return (&UserResource{}).ReplaceFields()
}

func TestUserReplacementPlans(t *testing.T) {
	tests := []struct {
		name     string
		poolID   string
		username string
		current  string
		decision runtime.Decision
		triggers []string
	}{
		{
			name:     "user pool id replaces",
			poolID:   "us-east-1_other",
			username: "alice",
			decision: runtime.DecisionReplace,
			triggers: []string{"user-pool-id"},
		},
		{
			name:     "username replaces",
			poolID:   "us-east-1_example",
			username: "bob",
			decision: runtime.DecisionReplace,
			triggers: []string{"username"},
		},
		{
			name:     "attributes update",
			poolID:   "us-east-1_example",
			username: "alice",
			current: `attributes: {
        name: 'Alice Updated'
      }`,
			decision: runtime.DecisionUpdate,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := planUserChange(t, tt.poolID, tt.username, tt.current)
			assert.Equal(t, tt.decision, step.Decision)
			assert.Equal(t, tt.triggers, step.ReplaceTriggers)
		})
	}
}

func planUserChange(
	t *testing.T,
	poolID string,
	username string,
	current string,
) *runtime.PlanStep {
	t.Helper()
	source := fmt.Sprintf(`factory: {
  resources: {
    user: aws-cognitoidp.user {
      user-pool-id: '%s'
      username: '%s'
      %s
    }
  }
}`, poolID, username, current)
	parsed, err := syntax.ParseSource("factory.ub", []byte(source))
	require.NoError(t, err)
	require.NotNil(t, parsed.Factory)
	body := parsed.Factory.Body
	libraries := map[string]*runtime.Library{
		"aws-cognitoidp": {
			Name: "aws-cognitoidp",
			Resources: map[string]runtime.ResourceRegistration{
				"user": runtime.MakeResource[
					userPlanProbe,
					map[string]any,
					any,
				](),
			},
		},
	}
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	factory := state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"}
	snapshot := state.NewSnapshot(factory, store.Stack())
	inputs := map[string]any{
		"user-pool-id": "us-east-1_example",
		"username":     "alice",
		"attributes": map[string]any{
			"name": "Alice",
		},
	}
	snapshot.Entries = []*state.Entry{{
		Address:       "resource.user",
		Type:          state.EntryLeaf,
		Category:      "resource",
		Binding:       &state.Binding{Alias: "aws-cognitoidp", Export: "user"},
		SchemaVersion: 1,
		Inputs:        inputs,
		Outputs: map[string]any{
			"user-pool-id": "us-east-1_example",
			"username":     "alice",
			"attributes": map[string]any{
				"name": "Alice",
			},
		},
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
	return plan.Steps[0]
}
