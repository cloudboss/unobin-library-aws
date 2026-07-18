package wafv2

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRuleLabel(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "empty", wantErr: "rule-label must be 1..1024 characters"},
		{
			name:    "long",
			value:   strings.Repeat("a", 1025),
			wantErr: "rule-label must be 1..1024 characters",
		},
		{
			name:    "invalid characters",
			value:   "namespace/bad",
			wantErr: "rule-label must match ^[0-9A-Za-z_:-]+$",
		},
		{
			name:    "empty component",
			value:   "namespace::name",
			wantErr: "rule-label components must be 1..128 characters",
		},
		{
			name:    "long component",
			value:   strings.Repeat("a", 129),
			wantErr: "rule-label components must be 1..128 characters",
		},
		{
			name:    "too many namespaces",
			value:   "one:two:three:four:five:six:name",
			wantErr: "rule-label must contain at most 5 namespaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateRuleLabel(tt.value), tt.wantErr)
		})
	}
}

func TestValidateRuleLabelRejectsReservedComponents(t *testing.T) {
	for _, reserved := range []string{
		"aws",
		"waf",
		"managed",
		"rulegroup",
		"webacl",
		"regexpatternset",
		"ipset",
	} {
		t.Run(reserved, func(t *testing.T) {
			assert.ErrorContains(
				t,
				validateRuleLabel("tenant:"+reserved+":name"),
				"rule-label component is reserved",
			)
		})
	}
}

func TestValidateRuleLabelBoundaries(t *testing.T) {
	component := strings.Repeat("a", 128)
	for _, value := range []string{
		"x",
		component,
		strings.Join([]string{
			component,
			component,
			component,
			component,
			component,
			component,
		}, ":"),
	} {
		require.NoError(t, validateRuleLabel(value))
	}
}

func TestValidateRuleLabelsPreservesCase(t *testing.T) {
	labels := []WebACLRuleLabel{
		{Name: "Tenant:Name"},
		{Name: "tenant:name"},
	}
	require.NoError(t, validateRuleLabels(&labels))
}
