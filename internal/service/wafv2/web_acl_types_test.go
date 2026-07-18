package wafv2

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLStatementLevelBoundary(t *testing.T) {
	level0 := reflect.TypeFor[WebACLStatementLevel0]()
	for _, field := range []string{"AndStatement", "NotStatement", "OrStatement"} {
		_, ok := level0.FieldByName(field)
		assert.False(t, ok, "Level0 unexpectedly contains %s", field)
	}

	level3 := reflect.TypeFor[WebACLStatementLevel3]()
	for _, field := range []string{
		"ManagedRuleGroupStatement",
		"RateBasedStatement",
		"RuleGroupReferenceStatement",
	} {
		_, ok := level3.FieldByName(field)
		require.True(t, ok, "Level3 is missing %s", field)
		for _, lower := range []reflect.Type{
			reflect.TypeFor[WebACLStatementLevel0](),
			reflect.TypeFor[WebACLStatementLevel1](),
			reflect.TypeFor[WebACLStatementLevel2](),
		} {
			_, ok := lower.FieldByName(field)
			assert.False(t, ok, "%s unexpectedly contains %s", lower.Name(), field)
		}
	}
}
