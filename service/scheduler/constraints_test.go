package scheduler_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/testutil"
)

func TestScheduleDatesAndCapacityProviderCounts(t *testing.T) {
	schema := readLibrarySchema(t).Resources["schedule"]
	ecs := map[string]any{"task-definition-arn": "arn:aws:ecs:us-east-1:123456789012:task-definition/job"}
	inputs := map[string]any{
		"flexible-time-window": map[string]any{"mode": "OFF"},
		"schedule-expression":  "rate(1 hour)",
		"target": map[string]any{
			"arn":            "arn:aws:ecs:us-east-1:123456789012:cluster/jobs",
			"role-arn":       "arn:aws:iam::123456789012:role/scheduler",
			"ecs-parameters": ecs,
		},
	}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	ecs["capacity-provider-strategy"] = []any{}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["start-date"], inputs["end-date"] = "2026-01-02T03:04:05Z", "2026-01-02T04:05:06+00:00"
	first := map[string]any{"capacity-provider": "first", "base": int64(1), "weight": int64(1)}
	second := map[string]any{"capacity-provider": "second"}
	ecs["capacity-provider-strategy"] = []any{first, second}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["start-date"], inputs["end-date"] = "2026-01-02", ""
	first["weight"], second["base"] = int64(0), int64(2)
	errs := testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 4)
	require.Contains(t, errs[0], "start-date must be RFC3339")
	require.Contains(t, errs[1], "end-date must be RFC3339")
	require.Contains(t, errs[2], "at most one positive base")
	require.Contains(t, errs[3], "at least one positive weight")
}
