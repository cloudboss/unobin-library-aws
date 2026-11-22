package wafv2_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/testutil"
)

func TestWebACLRuleUniqueness(t *testing.T) {
	schema := readLibrarySchema(t).Resources["web-acl"]
	inputs := map[string]any{}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["rules"] = []any{
		map[string]any{"name": "first", "priority": int64(1)},
		map[string]any{"name": "second", "priority": int64(1)},
		map[string]any{"name": "first", "priority": int64(2)},
	}
	errs := testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 2)
	require.Contains(t, errs[0], "rule names must be unique")
	require.Contains(t, errs[1], "rule priorities must be unique")
}

func TestWebACLCustomResponseReferences(t *testing.T) {
	schema := readLibrarySchema(t).Resources["web-acl"]
	for _, tt := range []struct {
		name   string
		inputs func(map[string]any) map[string]any
	}{
		{"default-action", func(action map[string]any) map[string]any {
			return map[string]any{"default-action": action}
		}},
		{"rule", func(action map[string]any) map[string]any {
			return map[string]any{"rules": []any{map[string]any{"action": action}}}
		}},
		{"managed rule override", func(action map[string]any) map[string]any {
			return webACLOverrideInputs("managed-rule-group-statement", action)
		}},
		{"rule group override", func(action map[string]any) map[string]any {
			return webACLOverrideInputs("rule-group-reference-statement", action)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := map[string]any{"response-code": int64(403)}
			inputs := tt.inputs(map[string]any{"block": map[string]any{"custom-response": response}})
			require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
			response["custom-response-body-key"] = "deny"
			errs := testutil.CheckConstraints(t, schema, inputs)
			require.Len(t, errs, 1)
			require.Contains(t, errs[0], tt.name+" custom-response-body-key must exist")
			inputs["custom-response-bodies"] = map[string]any{
				"deny": map[string]any{"content-type": "TEXT_PLAIN", "content": "Forbidden"},
			}
			require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
		})
	}
}

func webACLOverrideInputs(group string, action map[string]any) map[string]any {
	return map[string]any{"rules": []any{map[string]any{"statement": map[string]any{
		group: map[string]any{"rule-action-overrides": []any{map[string]any{"action-to-use": action}}},
	}}}}
}
