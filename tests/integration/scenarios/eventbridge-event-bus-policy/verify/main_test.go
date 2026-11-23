package main

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters/local"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyUnchangedPlan(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		decision runtime.Decision
		wantErr  string
	}{
		{
			name:    "CLI envelope with unchanged policy",
			address: "resource.policy['managed']", decision: runtime.DecisionNoOp,
		},
		{
			name: "changed policy", address: "resource.policy['managed']",
			decision: runtime.DecisionUpdate, wantErr: "unchanged policy decision: update",
		},
		{
			name: "missing policy", address: "resource.bus", decision: runtime.DecisionNoOp,
			wantErr: "unchanged plan has no policy resource",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contents, err := runtime.SealPlan(&runtime.Plan{
				Steps: []*runtime.PlanStep{
					{Address: "data-source.caller", Decision: runtime.DecisionRead},
					{Address: tt.address, Decision: tt.decision},
				},
			}, local.Noop{})
			require.NoError(t, err)
			err = verifyUnchangedPlan(contents)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}
