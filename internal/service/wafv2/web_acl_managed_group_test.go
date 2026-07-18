package wafv2

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandManagedRuleGroupStatement(t *testing.T) {
	version := "Version_1.0"
	excluded := []WebACLExcludedRule{{Name: "legacy-rule"}}
	overrides := []WebACLRuleActionOverride{{
		Name:        "override-rule",
		ActionToUse: WebACLRuleAction{Count: &WebACLCountAction{}},
	}}
	scopeDown := level2Leaf(logicalLeaf0("scope-down"))
	input := &WebACLManagedRuleGroupStatement{
		ExcludedRules:       &excluded,
		Name:                "AWSManagedRulesCommonRuleSet",
		RuleActionOverrides: &overrides,
		ScopeDownStatement:  &scopeDown,
		VendorName:          "AWS",
		Version:             &version,
	}
	expected := &awstypes.ManagedRuleGroupStatement{
		ExcludedRules: []awstypes.ExcludedRule{{Name: aws.String("legacy-rule")}},
		Name:          aws.String("AWSManagedRulesCommonRuleSet"),
		RuleActionOverrides: []awstypes.RuleActionOverride{{
			Name: aws.String("override-rule"),
			ActionToUse: &awstypes.RuleAction{
				Count: &awstypes.CountAction{},
			},
		}},
		ScopeDownStatement: logicalSDKLeaf("scope-down"),
		VendorName:         aws.String("AWS"),
		Version:            aws.String(version),
	}

	actual, err := expandManagedRuleGroupStatement(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)

	root, err := expandStatementLevel3(WebACLStatementLevel3{
		ManagedRuleGroupStatement: input,
	})
	require.NoError(t, err)
	assert.Equal(t, &awstypes.Statement{ManagedRuleGroupStatement: expected}, root)
}

func TestExpandManagedRuleGroupStatementOmitsOptionalMembers(t *testing.T) {
	input := validManagedRuleGroupStatement()
	expected := &awstypes.ManagedRuleGroupStatement{
		Name:       aws.String(input.Name),
		VendorName: aws.String(input.VendorName),
	}

	actual, err := expandManagedRuleGroupStatement(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

func TestValidateManagedRuleGroupStatement(t *testing.T) {
	empty := ""
	longValue := strings.Repeat("x", 129)
	invalidExcluded := []WebACLExcludedRule{{Name: "bad name"}}
	tooManyOverrides := testRuleActionOverrides(101)
	invalidOverrides := []WebACLRuleActionOverride{{Name: "missing-action"}}
	invalidScopeDown := WebACLStatementLevel2{}
	tests := []struct {
		name    string
		mutate  func(*WebACLManagedRuleGroupStatement)
		wantErr string
	}{
		{
			name:    "nil statement",
			wantErr: "managed rule group statement is required",
		},
		{
			name: "missing name",
			mutate: func(in *WebACLManagedRuleGroupStatement) {
				in.Name = ""
			},
			wantErr: "name must be 1..128",
		},
		{
			name: "long name",
			mutate: func(in *WebACLManagedRuleGroupStatement) {
				in.Name = longValue
			},
			wantErr: "name must be 1..128",
		},
		{
			name: "missing vendor name",
			mutate: func(in *WebACLManagedRuleGroupStatement) {
				in.VendorName = ""
			},
			wantErr: "vendor-name must be 1..128",
		},
		{
			name: "long vendor name",
			mutate: func(in *WebACLManagedRuleGroupStatement) {
				in.VendorName = longValue
			},
			wantErr: "vendor-name must be 1..128",
		},
		{
			name: "empty version",
			mutate: func(in *WebACLManagedRuleGroupStatement) {
				in.Version = &empty
			},
			wantErr: "version must be 1..128",
		},
		{
			name: "long version",
			mutate: func(in *WebACLManagedRuleGroupStatement) {
				in.Version = &longValue
			},
			wantErr: "version must be 1..128",
		},
		{
			name: "invalid excluded rule",
			mutate: func(in *WebACLManagedRuleGroupStatement) {
				in.ExcludedRules = &invalidExcluded
			},
			wantErr: "excluded rule name",
		},
		{
			name: "too many overrides",
			mutate: func(in *WebACLManagedRuleGroupStatement) {
				in.RuleActionOverrides = &tooManyOverrides
			},
			wantErr: "0..100",
		},
		{
			name: "invalid override",
			mutate: func(in *WebACLManagedRuleGroupStatement) {
				in.RuleActionOverrides = &invalidOverrides
			},
			wantErr: "exactly one",
		},
		{
			name: "invalid scope down",
			mutate: func(in *WebACLManagedRuleGroupStatement) {
				in.ScopeDownStatement = &invalidScopeDown
			},
			wantErr: "level-2 statement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input *WebACLManagedRuleGroupStatement
			if tt.mutate != nil {
				input = validManagedRuleGroupStatement()
				tt.mutate(input)
			}
			assert.ErrorContains(t, validateManagedRuleGroupStatement(input), tt.wantErr)
		})
	}
}

func TestValidateManagedRuleGroupStatementBoundaries(t *testing.T) {
	version := strings.Repeat("v", 128)
	emptyConfigs := []WebACLManagedRuleGroupConfig{}
	overrides := testRuleActionOverrides(100)
	input := &WebACLManagedRuleGroupStatement{
		ManagedRuleGroupConfigs: &emptyConfigs,
		Name:                    strings.Repeat("n", 128),
		RuleActionOverrides:     &overrides,
		VendorName:              strings.Repeat("v", 128),
		Version:                 &version,
	}

	require.NoError(t, validateManagedRuleGroupStatement(input))
}

func validManagedRuleGroupStatement() *WebACLManagedRuleGroupStatement {
	return &WebACLManagedRuleGroupStatement{
		Name:       "AWSManagedRulesCommonRuleSet",
		VendorName: "AWS",
	}
}
