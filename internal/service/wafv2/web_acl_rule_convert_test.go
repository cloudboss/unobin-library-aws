package wafv2

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandRules(t *testing.T) {
	labels := []WebACLRuleLabel{
		{Name: "tenant:paid"},
		{Name: "risk:review"},
	}
	rules := []WebACLRule{
		{
			Action: &WebACLRuleAction{Count: &WebACLCountAction{}},
			CaptchaConfig: &WebACLCaptchaConfig{
				ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 60},
			},
			ChallengeConfig: &WebACLChallengeConfig{
				ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 300},
			},
			Name:       "rule-second",
			Priority:   20,
			RuleLabels: &labels,
			Statement: WebACLStatementLevel3{
				LabelMatchStatement: &WebACLLabelMatchStatement{
					Key:   "source:label",
					Scope: "LABEL",
				},
			},
			VisibilityConfig: WebACLVisibilityConfig{
				CloudWatchMetricsEnabled: false,
				MetricName:               "rule-second-metric",
				SampledRequestsEnabled:   true,
			},
		},
		{
			Name:           "rule-first",
			OverrideAction: &WebACLOverrideAction{None: &WebACLNoneAction{}},
			Priority:       1,
			Statement: WebACLStatementLevel3{
				ManagedRuleGroupStatement: validRuleManagedGroup(),
			},
			VisibilityConfig: WebACLVisibilityConfig{
				CloudWatchMetricsEnabled: true,
				MetricName:               "rule-first-metric",
				SampledRequestsEnabled:   false,
			},
		},
	}
	expected := []awstypes.Rule{
		{
			Action: &awstypes.RuleAction{Count: &awstypes.CountAction{}},
			CaptchaConfig: &awstypes.CaptchaConfig{
				ImmunityTimeProperty: &awstypes.ImmunityTimeProperty{
					ImmunityTime: aws.Int64(60),
				},
			},
			ChallengeConfig: &awstypes.ChallengeConfig{
				ImmunityTimeProperty: &awstypes.ImmunityTimeProperty{
					ImmunityTime: aws.Int64(300),
				},
			},
			Name:     aws.String("rule-second"),
			Priority: 20,
			RuleLabels: []awstypes.Label{
				{Name: aws.String("tenant:paid")},
				{Name: aws.String("risk:review")},
			},
			Statement: &awstypes.Statement{
				LabelMatchStatement: &awstypes.LabelMatchStatement{
					Key:   aws.String("source:label"),
					Scope: awstypes.LabelMatchScopeLabel,
				},
			},
			VisibilityConfig: &awstypes.VisibilityConfig{
				CloudWatchMetricsEnabled: false,
				MetricName:               aws.String("rule-second-metric"),
				SampledRequestsEnabled:   true,
			},
		},
		{
			Name:           aws.String("rule-first"),
			OverrideAction: &awstypes.OverrideAction{None: &awstypes.NoneAction{}},
			Priority:       1,
			Statement: &awstypes.Statement{
				ManagedRuleGroupStatement: &awstypes.ManagedRuleGroupStatement{
					Name:       aws.String("AWSManagedRulesCommonRuleSet"),
					VendorName: aws.String("AWS"),
				},
			},
			VisibilityConfig: &awstypes.VisibilityConfig{
				CloudWatchMetricsEnabled: true,
				MetricName:               aws.String("rule-first-metric"),
				SampledRequestsEnabled:   false,
			},
		},
	}

	actual, err := expandRules(&rules)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

func TestExpandRulesPreservesEmptyCaptchaAndChallengeConfigs(t *testing.T) {
	rules := []WebACLRule{{
		Action:          &WebACLRuleAction{Count: &WebACLCountAction{}},
		CaptchaConfig:   &WebACLCaptchaConfig{},
		ChallengeConfig: &WebACLChallengeConfig{},
		Name:            "empty-configs",
		Priority:        1,
		Statement:       ruleLeafLevel3("request:empty-configs"),
		VisibilityConfig: WebACLVisibilityConfig{
			CloudWatchMetricsEnabled: true,
			MetricName:               "empty-configs",
			SampledRequestsEnabled:   true,
		},
	}}

	actual, err := expandRules(&rules)
	require.NoError(t, err)
	require.Len(t, actual, 1)
	assert.Equal(t, &awstypes.CaptchaConfig{}, actual[0].CaptchaConfig)
	assert.Equal(t, &awstypes.ChallengeConfig{}, actual[0].ChallengeConfig)
}

func validRuleManagedGroup() *WebACLManagedRuleGroupStatement {
	return &WebACLManagedRuleGroupStatement{
		Name:       "AWSManagedRulesCommonRuleSet",
		VendorName: "AWS",
	}
}
