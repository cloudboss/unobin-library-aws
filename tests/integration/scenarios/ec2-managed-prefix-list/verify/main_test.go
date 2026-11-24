package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestUnchangedPlan(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "update", "duplicate", "unexpected"} {
		t.Run(mode, func(t *testing.T) {
			steps := []*runtime.PlanStep{
				{Address: "resource.managed", Kind: runtime.NodeResource, Decision: runtime.DecisionNoOp},
				{Address: "resource.cleared", Kind: runtime.NodeResource, Decision: runtime.DecisionNoOp},
				{Address: "resource.unmanaged", Kind: runtime.NodeResource, Decision: runtime.DecisionNoOp},
				{Address: "library-config.ec2", Kind: runtime.NodeLibraryConfig,
					Decision: runtime.DecisionEval},
				{Address: "output.managed", Kind: runtime.NodeOutput, Decision: runtime.DecisionEval},
			}
			switch mode {
			case "missing":
				steps = steps[1:]
			case "update":
				steps[0].Decision = runtime.DecisionUpdate
			case "duplicate":
				steps = append(steps, steps[0])
			case "unexpected":
				steps = append(steps, &runtime.PlanStep{Address: "resource.other",
					Kind: runtime.NodeResource, Decision: runtime.DecisionNoOp})
			}
			body, err := runtime.SealPlan(&runtime.Plan{Steps: steps}, encrypters.Noop{})
			require.NoError(t, err)
			if mode == "valid" {
				require.NoError(t, unchangedPlan(body))
			} else {
				require.Error(t, unchangedPlan(body))
			}
		})
	}
	require.Error(t, unchangedPlan([]byte("invalid")))
}

func TestReapplyUsesSavedPlanAndStopsOnFailure(t *testing.T) {
	for _, failure := range []string{"", "plan", "apply"} {
		t.Run(failure, func(t *testing.T) {
			expected := [][]string{
				{"plan", "-c", "update/config.ub", "-o", "prefix-repair.ubp"},
				{"apply", "prefix-repair.ubp"},
			}
			var calls [][]string
			injected := errors.New("injected " + failure + " failure")
			err := reapply(func(args ...string) error {
				calls = append(calls, args)
				if args[0] == "apply" && len(args) != 2 {
					return fmt.Errorf("apply accepts one saved plan file: %v", args)
				}
				if args[0] == failure {
					return injected
				}
				return nil
			})
			if failure == "" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, injected)
			}
			if failure == "plan" {
				expected = expected[:1]
			}
			assert.Equal(t, expected, calls)
		})
	}
}
