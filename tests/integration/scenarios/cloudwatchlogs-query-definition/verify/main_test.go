package main

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyUnchangedPlan(t *testing.T) {
	group := &runtime.PlanStep{
		Address: "resource.group", Kind: runtime.NodeResource, Decision: runtime.DecisionNoOp,
	}
	query := &runtime.PlanStep{
		Address: "resource.query", Kind: runtime.NodeResource, Decision: runtime.DecisionNoOp,
	}
	evaluations := []*runtime.PlanStep{
		{Address: "library-config.logs", Kind: runtime.NodeLibraryConfig,
			Decision: runtime.DecisionEval},
		{Address: "output.id", Kind: runtime.NodeOutput, Decision: runtime.DecisionEval},
		{Address: "output.arn", Kind: runtime.NodeOutput, Decision: runtime.DecisionEval},
		{Address: "output.name", Kind: runtime.NodeOutput, Decision: runtime.DecisionEval},
		{Address: "output.query", Kind: runtime.NodeOutput, Decision: runtime.DecisionEval},
		{Address: "output.groups", Kind: runtime.NodeOutput, Decision: runtime.DecisionEval},
	}
	tests := []struct {
		name    string
		steps   []*runtime.PlanStep
		wantErr string
	}{
		{name: "compiled configuration and output evaluations",
			steps: append(evaluations, group, query)},
		{name: "resources only", steps: []*runtime.PlanStep{query, group}},
		{name: "missing group", steps: []*runtime.PlanStep{query},
			wantErr: "unchanged plan has no resource.group"},
		{name: "missing query", steps: []*runtime.PlanStep{group},
			wantErr: "unchanged plan has no resource.query"},
		{name: "evaluations without resources", steps: evaluations,
			wantErr: "unchanged plan has no resource.group"},
		{name: "empty plan", wantErr: "unchanged plan has no resource.group"},
		{name: "unexpected resource", steps: []*runtime.PlanStep{group, query, {
			Address: "resource.other", Kind: runtime.NodeResource, Decision: runtime.DecisionNoOp,
		}}, wantErr: "unexpected resource resource.other in unchanged plan"},
		{name: "duplicate resource", steps: []*runtime.PlanStep{group, query, query},
			wantErr: "duplicate resource.query in unchanged plan"},
		{name: "output cannot replace managed resource", steps: []*runtime.PlanStep{group, {
			Address: "resource.query", Kind: runtime.NodeOutput, Decision: runtime.DecisionEval,
		}}, wantErr: "unchanged plan has no resource.query"},
		{name: "configuration mutation", steps: []*runtime.PlanStep{group, query, {
			Address: "library-config.logs", Kind: runtime.NodeLibraryConfig,
			Decision: runtime.DecisionUpdate,
		}}, wantErr: "unchanged library-config.logs planned update"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := runtime.SealPlan(&runtime.Plan{Steps: tt.steps}, encrypters.Noop{})
			require.NoError(t, err)
			err = verifyUnchangedPlan(body)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestVerifyUnchangedPlanRejectsInvalidEnvelope(t *testing.T) {
	assert.Error(t, verifyUnchangedPlan([]byte("invalid plan")))
}

func TestVerifyUnchangedPlanRejectsResourceChanges(t *testing.T) {
	for _, address := range []string{"resource.group", "resource.query"} {
		for _, decision := range []runtime.Decision{
			runtime.DecisionCreate, runtime.DecisionUpdate, runtime.DecisionReplace,
			runtime.DecisionDestroy, runtime.DecisionEval,
		} {
			t.Run(address+"/"+string(decision), func(t *testing.T) {
				steps := []*runtime.PlanStep{
					{Address: "resource.group", Kind: runtime.NodeResource,
						Decision: runtime.DecisionNoOp},
					{Address: "resource.query", Kind: runtime.NodeResource,
						Decision: runtime.DecisionNoOp},
				}
				for _, step := range steps {
					if step.Address == address {
						step.Decision = decision
					}
				}
				body, err := runtime.SealPlan(&runtime.Plan{Steps: steps}, encrypters.Noop{})
				require.NoError(t, err)
				assert.EqualError(t, verifyUnchangedPlan(body),
					"unchanged "+address+" planned "+string(decision))
			})
		}
	}
}
