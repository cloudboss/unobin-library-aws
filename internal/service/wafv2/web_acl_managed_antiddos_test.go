package wafv2

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandManagedAntiDDoSRuleSet(t *testing.T) {
	challengeSensitivity := "MEDIUM"
	blockSensitivity := "HIGH"
	exemptions := []WebACLManagedRuleGroupRegex{
		{RegexString: `\/api\/`},
		{RegexString: `\.png$`},
	}
	input := &WebACLAWSManagedRulesAntiDDoSRuleSet{
		ClientSideActionConfig: &WebACLClientSideActionConfig{
			Challenge: &WebACLClientSideAction{
				ExemptURIRegularExpressions: &exemptions,
				Sensitivity:                 &challengeSensitivity,
				UsageOfAction:               "ENABLED",
			},
		},
		SensitivityToBlock: &blockSensitivity,
	}
	expected := &awstypes.AWSManagedRulesAntiDDoSRuleSet{
		ClientSideActionConfig: &awstypes.ClientSideActionConfig{
			Challenge: &awstypes.ClientSideAction{
				ExemptUriRegularExpressions: []awstypes.Regex{
					{RegexString: aws.String(`\/api\/`)},
					{RegexString: aws.String(`\.png$`)},
				},
				Sensitivity:   awstypes.SensitivityToActMedium,
				UsageOfAction: awstypes.UsageOfActionEnabled,
			},
		},
		SensitivityToBlock: awstypes.SensitivityToActHigh,
	}

	actual, err := expandManagedAntiDDoSRuleSet(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

func TestExpandManagedAntiDDoSOmitsOptionalMembers(t *testing.T) {
	input := validManagedAntiDDoS("DISABLED")
	expected := &awstypes.AWSManagedRulesAntiDDoSRuleSet{
		ClientSideActionConfig: &awstypes.ClientSideActionConfig{
			Challenge: &awstypes.ClientSideAction{
				UsageOfAction: awstypes.UsageOfActionDisabled,
			},
		},
	}

	actual, err := expandManagedAntiDDoSRuleSet(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
	assert.Empty(t, actual.ClientSideActionConfig.Challenge.Sensitivity)
	assert.Empty(t, actual.SensitivityToBlock)
	assert.Nil(t, actual.ClientSideActionConfig.Challenge.ExemptUriRegularExpressions)
}

func TestExpandManagedAntiDDoSPreservesExplicitEmptyExemptions(t *testing.T) {
	empty := []WebACLManagedRuleGroupRegex{}
	input := validManagedAntiDDoS("DISABLED")
	input.ClientSideActionConfig.Challenge.ExemptURIRegularExpressions = &empty

	actual, err := expandManagedAntiDDoSRuleSet(input)
	require.NoError(t, err)
	require.NotNil(t, actual.ClientSideActionConfig.Challenge.ExemptUriRegularExpressions)
	assert.Empty(t, actual.ClientSideActionConfig.Challenge.ExemptUriRegularExpressions)
}

func TestValidateManagedAntiDDoSRuleSet(t *testing.T) {
	longRegex := strings.Repeat("r", 513)
	tests := []struct {
		name    string
		mutate  func(*WebACLAWSManagedRulesAntiDDoSRuleSet)
		wantErr string
	}{
		{
			name: "missing client-side action config",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.ClientSideActionConfig = nil
			},
			wantErr: "client-side-action-config is required",
		},
		{
			name: "missing challenge",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.ClientSideActionConfig.Challenge = nil
			},
			wantErr: "challenge is required",
		},
		{
			name: "missing usage",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.ClientSideActionConfig.Challenge.UsageOfAction = ""
			},
			wantErr: "usage-of-action must be ENABLED or DISABLED",
		},
		{
			name: "invalid usage",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.ClientSideActionConfig.Challenge.UsageOfAction = "ACTIVE"
			},
			wantErr: "usage-of-action must be ENABLED or DISABLED",
		},
		{
			name: "invalid challenge sensitivity",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.ClientSideActionConfig.Challenge.Sensitivity = stringPointer("EXTREME")
			},
			wantErr: "sensitivity must be LOW, MEDIUM, or HIGH",
		},
		{
			name: "invalid block sensitivity",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.SensitivityToBlock = stringPointer("EXTREME")
			},
			wantErr: "sensitivity-to-block must be LOW, MEDIUM, or HIGH",
		},
		{
			name: "too many exemptions",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.ClientSideActionConfig.Challenge.ExemptURIRegularExpressions =
					managedAntiDDoSRegexes("a", "b", "c", "d", "e", "f")
			},
			wantErr: "exempt-uri-regular-expressions must contain at most 5 members",
		},
		{
			name: "empty regex string",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.ClientSideActionConfig.Challenge.ExemptURIRegularExpressions =
					managedAntiDDoSRegexes("")
			},
			wantErr: "regex-string at index 0 must be 1..512 characters",
		},
		{
			name: "long regex string",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.ClientSideActionConfig.Challenge.ExemptURIRegularExpressions =
					managedAntiDDoSRegexes(longRegex)
			},
			wantErr: "regex-string at index 0 must be 1..512 characters",
		},
		{
			name: "enabled without exemptions",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.ClientSideActionConfig.Challenge.UsageOfAction = "ENABLED"
			},
			wantErr: "usage-of-action ENABLED requires 1..5 exempt-uri-regular-expressions",
		},
		{
			name: "enabled with empty exemptions",
			mutate: func(in *WebACLAWSManagedRulesAntiDDoSRuleSet) {
				in.ClientSideActionConfig.Challenge.UsageOfAction = "ENABLED"
				in.ClientSideActionConfig.Challenge.ExemptURIRegularExpressions =
					managedAntiDDoSRegexes()
			},
			wantErr: "usage-of-action ENABLED requires 1..5 exempt-uri-regular-expressions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validManagedAntiDDoS("DISABLED")
			tt.mutate(input)
			assert.ErrorContains(t, validateManagedAntiDDoSRuleSet(input), tt.wantErr)
		})
	}
}

func TestValidateManagedAntiDDoSRuleSetPermissiveCases(t *testing.T) {
	tests := []struct {
		name  string
		input *WebACLAWSManagedRulesAntiDDoSRuleSet
	}{
		{name: "disabled without exemptions", input: validManagedAntiDDoS("DISABLED")},
		{
			name:  "disabled with source-permitted regex syntax",
			input: managedAntiDDoSWithExemptions("DISABLED", "["),
		},
		{
			name:  "enabled with five exemptions",
			input: managedAntiDDoSWithExemptions("ENABLED", "a", "b", "c", "d", "e"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, validateManagedAntiDDoSRuleSet(tt.input))
		})
	}
}

func validManagedAntiDDoS(usage string) *WebACLAWSManagedRulesAntiDDoSRuleSet {
	return &WebACLAWSManagedRulesAntiDDoSRuleSet{
		ClientSideActionConfig: &WebACLClientSideActionConfig{
			Challenge: &WebACLClientSideAction{UsageOfAction: usage},
		},
	}
}

func managedAntiDDoSWithExemptions(
	usage string,
	values ...string,
) *WebACLAWSManagedRulesAntiDDoSRuleSet {
	in := validManagedAntiDDoS(usage)
	in.ClientSideActionConfig.Challenge.ExemptURIRegularExpressions =
		managedAntiDDoSRegexes(values...)
	return in
}

func managedAntiDDoSRegexes(values ...string) *[]WebACLManagedRuleGroupRegex {
	out := make([]WebACLManagedRuleGroupRegex, len(values))
	for index, value := range values {
		out[index] = WebACLManagedRuleGroupRegex{RegexString: value}
	}
	return &out
}
