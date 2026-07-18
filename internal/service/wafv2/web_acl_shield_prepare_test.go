package wafv2

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testShieldRuleName = "ShieldMitigationRuleGroup_123456789012_" +
	"5e665b1c-1641-4b7a-8db1-567871a18b2a_managed"

func TestPrepareWebACLUpdateInputSkipsGetForDesiredShieldRule(t *testing.T) {
	input := requiredWebACLUpdateInput()
	input.Rules = []awstypes.Rule{
		{Name: aws.String("desired-before"), Priority: 1},
		{Name: aws.String(testShieldRuleName), Priority: 2},
		{Name: aws.String("desired-after"), Priority: 3},
	}
	client := &fakeWAFClient{get: func(
		context.Context,
		*awssvc.GetWebACLInput,
	) (*awssvc.GetWebACLOutput, error) {
		t.Fatal("unexpected GetWebACL call")
		return nil, nil
	}}

	prepared, err := prepareWebACLUpdateInput(context.Background(), client, input)
	require.NoError(t, err)
	assert.Same(t, input, prepared)
}

func TestShieldMitigationRuleNameMatcher(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{name: "lowercase hex", value: testShieldRuleName, expected: true},
		{
			name: "uppercase hex",
			value: "ShieldMitigationRuleGroup_123456789012_" +
				"5E665B1C-1641-4B7A-8DB1-567871A18B2A_MANAGED",
			expected: true,
		},
		{
			name: "mixed case hex",
			value: "ShieldMitigationRuleGroup_123456789012_" +
				"5e665B1c-1641-4b7A-8dB1-567871A18b2A_suffix",
			expected: true,
		},
		{
			name: "empty suffix",
			value: "ShieldMitigationRuleGroup_123456789012_" +
				"5e665b1c-1641-4b7a-8db1-567871a18b2a_",
			expected: true,
		},
		{name: "unrelated", value: "ordinary-rule"},
		{
			name: "short account",
			value: "ShieldMitigationRuleGroup_12345678901_" +
				"5e665b1c-1641-4b7a-8db1-567871a18b2a_suffix",
		},
		{
			name: "long account",
			value: "ShieldMitigationRuleGroup_1234567890123_" +
				"5e665b1c-1641-4b7a-8db1-567871a18b2a_suffix",
		},
		{
			name: "non-hex UUID",
			value: "ShieldMitigationRuleGroup_123456789012_" +
				"5g665b1c-1641-4b7a-8db1-567871a18b2a_suffix",
		},
		{
			name: "short UUID group",
			value: "ShieldMitigationRuleGroup_123456789012_" +
				"5e665b1-1641-4b7a-8db1-567871a18b2a_suffix",
		},
		{
			name: "missing suffix separator",
			value: "ShieldMitigationRuleGroup_123456789012_" +
				"5e665b1c-1641-4b7a-8db1-567871a18b2a",
		},
		{name: "wrong prefix case", value: "shieldMitigationRuleGroup_123"},
		{name: "prefixed", value: "x" + testShieldRuleName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isShieldMitigationRuleName(tt.value))
		})
	}
}

func TestPrepareWebACLUpdateInputMergesCurrentShieldRules(t *testing.T) {
	upperName := "ShieldMitigationRuleGroup_123456789012_" +
		"5E665B1C-1641-4B7A-8DB1-567871A18B2A_UPPER"
	secondName := "ShieldMitigationRuleGroup_210987654321_" +
		"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee_second"
	malformed := "ShieldMitigationRuleGroup_123456789012_" +
		"5g665b1c-1641-4b7a-8db1-567871a18b2a_bad"
	tests := []struct {
		name         string
		currentRules []awstypes.Rule
		wantNames    []string
	}{
		{
			name: "zero",
			currentRules: []awstypes.Rule{
				{Name: aws.String("unrelated"), Priority: 9},
				{Name: aws.String(malformed), Priority: 10},
			},
			wantNames: []string{"desired"},
		},
		{
			name: "one uppercase hex",
			currentRules: []awstypes.Rule{
				{Name: aws.String("unrelated"), Priority: 9},
				{Name: aws.String(upperName), Priority: 10},
			},
			wantNames: []string{"desired", upperName},
		},
		{
			name: "multiple preserve response order",
			currentRules: []awstypes.Rule{
				{Name: aws.String(secondName), Priority: 10},
				{Name: aws.String("unrelated"), Priority: 11},
				{Name: aws.String(testShieldRuleName), Priority: 12},
				{Name: aws.String(malformed), Priority: 13},
			},
			wantNames: []string{"desired", secondName, testShieldRuleName},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := requiredWebACLUpdateInput()
			input.Rules = []awstypes.Rule{{Name: aws.String("desired"), Priority: 1}}
			calls := 0
			client := &fakeWAFClient{get: func(
				_ context.Context,
				in *awssvc.GetWebACLInput,
			) (*awssvc.GetWebACLOutput, error) {
				calls++
				assert.Equal(t, &awssvc.GetWebACLInput{
					Id:    aws.String("id-1"),
					Name:  aws.String("example"),
					Scope: awstypes.ScopeRegional,
				}, in)
				return shieldSnapshot("token-2", tt.currentRules), nil
			}}

			prepared, err := prepareWebACLUpdateInput(context.Background(), client, input)
			require.NoError(t, err)
			assert.Equal(t, 1, calls)
			assert.NotSame(t, input, prepared)
			assert.Equal(t, "token-2", aws.ToString(prepared.LockToken))
			assert.Equal(t, "token-1", aws.ToString(input.LockToken))
			assert.Equal(t, tt.wantNames, webACLRuleNames(prepared.Rules))
			assert.Equal(t, []string{"desired"}, webACLRuleNames(input.Rules))
		})
	}
}

func TestPrepareWebACLUpdateInputPreservesShieldRuleVerbatim(t *testing.T) {
	statement := &awstypes.Statement{
		RuleGroupReferenceStatement: &awstypes.RuleGroupReferenceStatement{
			ARN: aws.String("arn:aws:wafv2:us-east-1:123456789012:regional/" +
				"rulegroup/shield/id-1"),
		},
	}
	override := &awstypes.OverrideAction{None: &awstypes.NoneAction{}}
	visibility := &awstypes.VisibilityConfig{
		CloudWatchMetricsEnabled: true,
		MetricName:               aws.String(testShieldRuleName),
		SampledRequestsEnabled:   true,
	}
	shield := awstypes.Rule{
		Name:             aws.String(testShieldRuleName),
		OverrideAction:   override,
		Priority:         7,
		Statement:        statement,
		VisibilityConfig: visibility,
	}
	input := requiredWebACLUpdateInput()
	client := &fakeWAFClient{get: func(
		context.Context,
		*awssvc.GetWebACLInput,
	) (*awssvc.GetWebACLOutput, error) {
		return shieldSnapshot("token-2", []awstypes.Rule{shield}), nil
	}}

	prepared, err := prepareWebACLUpdateInput(context.Background(), client, input)
	require.NoError(t, err)
	require.Len(t, prepared.Rules, 1)
	assert.Equal(t, shield, prepared.Rules[0])
	assert.Same(t, statement, prepared.Rules[0].Statement)
	assert.Same(t, override, prepared.Rules[0].OverrideAction)
	assert.Same(t, visibility, prepared.Rules[0].VisibilityConfig)
}

func TestPrepareWebACLUpdateInputSnapshotFailures(t *testing.T) {
	sentinel := errors.New("snapshot failed")
	valid := shieldSnapshot("token-2", nil)
	tests := []struct {
		name    string
		output  *awssvc.GetWebACLOutput
		err     error
		wantErr string
		wantIs  error
	}{
		{name: "get error", err: sentinel, wantErr: "get current web ACL", wantIs: sentinel},
		{name: "nil output", wantErr: "empty response"},
		{
			name: "nil web ACL",
			output: &awssvc.GetWebACLOutput{
				LockToken: aws.String("token-2"),
			},
			wantErr: "response has no web ACL",
		},
		{
			name:    "empty token",
			output:  shieldSnapshot("", nil),
			wantErr: "response has no lock-token",
		},
		{
			name: "incomplete identity",
			output: func() *awssvc.GetWebACLOutput {
				copy := *valid
				acl := *valid.WebACL
				acl.Id = nil
				copy.WebACL = &acl
				return &copy
			}(),
			wantErr: "response has incomplete identity",
		},
		{
			name: "mismatched identity",
			output: func() *awssvc.GetWebACLOutput {
				copy := *valid
				acl := *valid.WebACL
				acl.Name = aws.String("other")
				copy.WebACL = &acl
				return &copy
			}(),
			wantErr: "response identity does not match request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := &fakeWAFClient{get: func(
				context.Context,
				*awssvc.GetWebACLInput,
			) (*awssvc.GetWebACLOutput, error) {
				calls++
				return tt.output, tt.err
			}}

			prepared, err := prepareWebACLUpdateInput(
				context.Background(),
				client,
				requiredWebACLUpdateInput(),
			)
			assert.Nil(t, prepared)
			assert.ErrorContains(t, err, tt.wantErr)
			if tt.wantIs != nil {
				assert.ErrorIs(t, err, tt.wantIs)
			}
			assert.Equal(t, 1, calls)
		})
	}
}

func TestPrepareWebACLUpdateInputRejectsMergedRuleCollisions(t *testing.T) {
	secondShieldName := "ShieldMitigationRuleGroup_210987654321_" +
		"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee_second"
	tests := []struct {
		name         string
		desiredRules []awstypes.Rule
		currentRules []awstypes.Rule
		wantErr      string
	}{
		{
			name: "empty desired name",
			desiredRules: []awstypes.Rule{
				{Priority: 1},
			},
			wantErr: "rule item 0 name must not be empty",
		},
		{
			name: "duplicate desired names",
			desiredRules: []awstypes.Rule{
				{Name: aws.String("duplicate"), Priority: 1},
				{Name: aws.String("duplicate"), Priority: 2},
			},
			wantErr: "rule names must be unique",
		},
		{
			name: "duplicate current Shield names",
			currentRules: []awstypes.Rule{
				{Name: aws.String(testShieldRuleName), Priority: 2},
				{Name: aws.String(testShieldRuleName), Priority: 3},
			},
			wantErr: "rule names must be unique",
		},
		{
			name: "desired and Shield priority collision",
			desiredRules: []awstypes.Rule{
				{Name: aws.String("desired"), Priority: 7},
			},
			currentRules: []awstypes.Rule{
				{Name: aws.String(testShieldRuleName), Priority: 7},
			},
			wantErr: "rule priorities must be unique",
		},
		{
			name: "current Shield priority collision",
			currentRules: []awstypes.Rule{
				{Name: aws.String(testShieldRuleName), Priority: 7},
				{Name: aws.String(secondShieldName), Priority: 7},
			},
			wantErr: "rule priorities must be unique",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := requiredWebACLUpdateInput()
			input.Rules = tt.desiredRules
			client := &fakeWAFClient{get: func(
				context.Context,
				*awssvc.GetWebACLInput,
			) (*awssvc.GetWebACLOutput, error) {
				return shieldSnapshot("token-2", tt.currentRules), nil
			}}

			prepared, err := prepareWebACLUpdateInput(context.Background(), client, input)
			assert.Nil(t, prepared)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Equal(t, "token-1", aws.ToString(input.LockToken))
			assert.Equal(t, tt.desiredRules, input.Rules)
		})
	}
}

func shieldSnapshot(
	lockToken string,
	rules []awstypes.Rule,
) *awssvc.GetWebACLOutput {
	return &awssvc.GetWebACLOutput{
		LockToken: aws.String(lockToken),
		WebACL: &awstypes.WebACL{
			ARN:   aws.String(testWebACLARN),
			Id:    aws.String("id-1"),
			Name:  aws.String("example"),
			Rules: rules,
		},
	}
}

func webACLRuleNames(rules []awstypes.Rule) []string {
	names := make([]string, len(rules))
	for index, rule := range rules {
		names[index] = aws.ToString(rule.Name)
	}
	return names
}
