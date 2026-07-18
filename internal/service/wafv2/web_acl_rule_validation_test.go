package wafv2

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRuleActionMatrixForAllLevel3Members(t *testing.T) {
	for _, tt := range ruleLevel3Cases() {
		t.Run(tt.name, func(t *testing.T) {
			rule := validWebACLRule(tt.statement)
			if tt.groupReference {
				rule.Action = nil
				rule.OverrideAction = validRuleOverride()
			}
			require.NoError(t, validateRule(rule))

			if tt.groupReference {
				withoutOverride := rule
				withoutOverride.OverrideAction = nil
				assert.ErrorContains(
					t,
					validateRule(withoutOverride),
					"group reference rule requires override-action",
				)

				withAction := rule
				withAction.Action = validRuleAction()
				assert.ErrorContains(
					t,
					validateRule(withAction),
					"group reference rule must not specify action",
				)

				labels := []WebACLRuleLabel{{Name: "group:label"}}
				withLabels := rule
				withLabels.RuleLabels = &labels
				assert.ErrorContains(
					t,
					validateRule(withLabels),
					"group reference rule must not specify rule-labels",
				)
				return
			}

			withoutAction := rule
			withoutAction.Action = nil
			assert.ErrorContains(
				t,
				validateRule(withoutAction),
				"non-group rule requires action",
			)

			withOverride := rule
			withOverride.OverrideAction = validRuleOverride()
			assert.ErrorContains(
				t,
				validateRule(withOverride),
				"non-group rule must not specify override-action",
			)
		})
	}
}

func TestValidateRuleRejectsRateAllow(t *testing.T) {
	rule := validWebACLRule(WebACLStatementLevel3{
		RateBasedStatement: validRateBasedStatement("IP"),
	})
	rule.Action = &WebACLRuleAction{Allow: &WebACLAllowAction{}}
	assert.ErrorContains(t, validateRule(rule), "rate-based rule must not use allow action")
}

func TestValidateRuleAcceptsPermittedRateActions(t *testing.T) {
	tests := []struct {
		name   string
		action *WebACLRuleAction
	}{
		{name: "block", action: &WebACLRuleAction{Block: &WebACLBlockAction{}}},
		{name: "captcha", action: &WebACLRuleAction{Captcha: &WebACLCaptchaAction{}}},
		{name: "challenge", action: &WebACLRuleAction{Challenge: &WebACLChallengeAction{}}},
		{name: "count", action: &WebACLRuleAction{Count: &WebACLCountAction{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := validWebACLRule(WebACLStatementLevel3{
				RateBasedStatement: validRateBasedStatement("IP"),
			})
			rule.Action = tt.action
			require.NoError(t, validateRule(rule))
		})
	}
}

func TestValidateRulePropagatesNestedValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		rule    WebACLRule
		wantErr string
	}{
		{
			name: "rate action union",
			rule: func() WebACLRule {
				rule := validWebACLRule(WebACLStatementLevel3{
					RateBasedStatement: validRateBasedStatement("IP"),
				})
				rule.Action = &WebACLRuleAction{
					Allow: &WebACLAllowAction{},
					Count: &WebACLCountAction{},
				}
				return rule
			}(),
			wantErr: "rule action must contain exactly one member",
		},
		{
			name: "override union",
			rule: func() WebACLRule {
				rule := validWebACLRule(WebACLStatementLevel3{
					ManagedRuleGroupStatement: validRuleManagedGroup(),
				})
				rule.Action = nil
				rule.OverrideAction = &WebACLOverrideAction{
					Count: &WebACLEmpty{},
					None:  &WebACLNoneAction{},
				}
				return rule
			}(),
			wantErr: "override action must contain exactly one member",
		},
		{
			name: "statement union",
			rule: validWebACLRule(WebACLStatementLevel3{
				GeoMatchStatement: &WebACLGeoMatchStatement{CountryCodes: []string{"US"}},
				LabelMatchStatement: &WebACLLabelMatchStatement{
					Key:   "test",
					Scope: "LABEL",
				},
			}),
			wantErr: "level-3 statement must contain exactly one member",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateRule(tt.rule), tt.wantErr)
		})
	}
}

func TestValidateRulesUniquenessAndCaseSensitivity(t *testing.T) {
	tests := []struct {
		name    string
		rules   []WebACLRule
		wantErr string
	}{
		{
			name: "duplicate name",
			rules: []WebACLRule{
				ruleWithIdentity("same", 1),
				ruleWithIdentity("same", 2),
			},
			wantErr: "rule names must be unique",
		},
		{
			name: "duplicate priority",
			rules: []WebACLRule{
				ruleWithIdentity("first", 1),
				ruleWithIdentity("second", 1),
			},
			wantErr: "rule priorities must be unique",
		},
		{
			name: "case-sensitive names",
			rules: []WebACLRule{
				ruleWithIdentity("same", 1),
				ruleWithIdentity("Same", 2),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRules(&tt.rules)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateRuleBoundsAndRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*WebACLRule)
		wantErr string
	}{
		{
			name: "missing name",
			mutate: func(rule *WebACLRule) {
				rule.Name = ""
			},
			wantErr: "rule name must be 1..128",
		},
		{
			name: "long name",
			mutate: func(rule *WebACLRule) {
				rule.Name = strings.Repeat("r", 129)
			},
			wantErr: "rule name must be 1..128",
		},
		{
			name: "invalid name",
			mutate: func(rule *WebACLRule) {
				rule.Name = "bad name"
			},
			wantErr: "rule name must match ^[0-9A-Za-z_-]+$",
		},
		{
			name: "negative priority",
			mutate: func(rule *WebACLRule) {
				rule.Priority = -1
			},
			wantErr: fmt.Sprintf("priority must be 0..%d", math.MaxInt32),
		},
		{
			name: "priority above maximum",
			mutate: func(rule *WebACLRule) {
				rule.Priority = math.MaxInt32 + 1
			},
			wantErr: fmt.Sprintf("priority must be 0..%d", math.MaxInt32),
		},
		{
			name: "missing statement",
			mutate: func(rule *WebACLRule) {
				rule.Statement = WebACLStatementLevel3{}
			},
			wantErr: "level-3 statement must contain exactly one member",
		},
		{
			name: "missing visibility",
			mutate: func(rule *WebACLRule) {
				rule.VisibilityConfig = WebACLVisibilityConfig{}
			},
			wantErr: "metric-name must be 1..128",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := validWebACLRule(ruleLeafLevel3("bounds"))
			tt.mutate(&rule)
			assert.ErrorContains(t, validateRule(rule), tt.wantErr)
		})
	}
}

func TestValidateRuleBoundaries(t *testing.T) {
	for _, rule := range []WebACLRule{
		ruleWithIdentity("x", 0),
		ruleWithIdentity(strings.Repeat("r", 128), math.MaxInt32),
	} {
		require.NoError(t, validateRule(rule))
	}
}

func TestExpandRulesHasNoArbitraryMaximum(t *testing.T) {
	rules := make([]WebACLRule, 101)
	for index := range rules {
		rules[index] = ruleWithIdentity(fmt.Sprintf("rule-%d", index), int64(index))
	}

	actual, err := expandRules(&rules)
	require.NoError(t, err)
	require.Len(t, actual, len(rules))
	assert.Equal(t, "rule-0", *actual[0].Name)
	assert.Equal(t, "rule-100", *actual[100].Name)
}

func TestExpandRulesOmitsNilAndEmpty(t *testing.T) {
	empty := []WebACLRule{}
	tests := []struct {
		name  string
		input *[]WebACLRule
	}{
		{name: "nil"},
		{name: "empty", input: &empty},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := expandRules(tt.input)
			require.NoError(t, err)
			assert.Nil(t, actual)
		})
	}
}

func TestValidateRuleConfigsDoNotDependOnAction(t *testing.T) {
	rule := validWebACLRule(WebACLStatementLevel3{
		ManagedRuleGroupStatement: validRuleManagedGroup(),
	})
	rule.Action = nil
	rule.OverrideAction = validRuleOverride()
	rule.CaptchaConfig = &WebACLCaptchaConfig{
		ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 60},
	}
	rule.ChallengeConfig = &WebACLChallengeConfig{
		ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 300},
	}
	require.NoError(t, validateRule(rule))
}

type ruleLevel3Case struct {
	name           string
	statement      WebACLStatementLevel3
	groupReference bool
}

func ruleLevel3Cases() []ruleLevel3Case {
	leafOne := ruleLeafLevel2("one")
	leafTwo := ruleLeafLevel2("two")
	search := "x"
	return []ruleLevel3Case{
		{
			name: "and",
			statement: WebACLStatementLevel3{
				AndStatement: &WebACLAndStatementLevel2{
					Statements: []WebACLStatementLevel2{leafOne, leafTwo},
				},
			},
		},
		{
			name: "not",
			statement: WebACLStatementLevel3{
				NotStatement: &WebACLNotStatementLevel2{Statement: leafOne},
			},
		},
		{
			name: "or",
			statement: WebACLStatementLevel3{
				OrStatement: &WebACLOrStatementLevel2{
					Statements: []WebACLStatementLevel2{leafOne, leafTwo},
				},
			},
		},
		{
			name: "ASN",
			statement: WebACLStatementLevel3{
				ASNMatchStatement: &WebACLASNMatchStatement{ASNList: []int64{64512}},
			},
		},
		{
			name: "byte",
			statement: WebACLStatementLevel3{
				ByteMatchStatement: &WebACLByteMatchStatement{
					FieldToMatch:         WebACLFieldToMatch{URIPath: &WebACLEmpty{}},
					PositionalConstraint: "EXACTLY",
					SearchString:         &search,
					TextTransformations:  ruleTransformations(),
				},
			},
		},
		{
			name: "geo",
			statement: WebACLStatementLevel3{
				GeoMatchStatement: &WebACLGeoMatchStatement{CountryCodes: []string{"US"}},
			},
		},
		{
			name: "IP set",
			statement: WebACLStatementLevel3{
				IPSetReferenceStatement: &WebACLIPSetReferenceStatement{
					ARN: "arn:aws:wafv2:us-east-1:123456789012:regional/ipset/test/id",
				},
			},
		},
		{name: "label", statement: ruleLeafLevel3("label")},
		{
			name: "managed group",
			statement: WebACLStatementLevel3{
				ManagedRuleGroupStatement: validRuleManagedGroup(),
			},
			groupReference: true,
		},
		{
			name: "rate",
			statement: WebACLStatementLevel3{
				RateBasedStatement: validRateBasedStatement("IP"),
			},
		},
		{
			name: "regex",
			statement: WebACLStatementLevel3{
				RegexMatchStatement: &WebACLRegexMatchStatement{
					FieldToMatch:        WebACLFieldToMatch{URIPath: &WebACLEmpty{}},
					RegexString:         "x",
					TextTransformations: ruleTransformations(),
				},
			},
		},
		{
			name: "regex pattern set",
			statement: WebACLStatementLevel3{
				RegexPatternSetReference: &WebACLRegexPatternSetReferenceStatement{
					ARN:                 "arn:aws:wafv2:us-east-1:123456789012:regional/regexpatternset/test/id",
					FieldToMatch:        WebACLFieldToMatch{URIPath: &WebACLEmpty{}},
					TextTransformations: ruleTransformations(),
				},
			},
		},
		{
			name: "rule group",
			statement: WebACLStatementLevel3{
				RuleGroupReferenceStatement: &WebACLRuleGroupReferenceStatement{
					ARN: "arn:aws:wafv2:us-east-1:123456789012:regional/rulegroup/test/id",
				},
			},
			groupReference: true,
		},
		{
			name: "size",
			statement: WebACLStatementLevel3{
				SizeConstraintStatement: &WebACLSizeConstraintStatement{
					ComparisonOperator:  "GE",
					FieldToMatch:        WebACLFieldToMatch{URIPath: &WebACLEmpty{}},
					Size:                1,
					TextTransformations: ruleTransformations(),
				},
			},
		},
		{
			name: "SQLi",
			statement: WebACLStatementLevel3{
				SQLiMatchStatement: &WebACLSQLiMatchStatement{
					FieldToMatch:        WebACLFieldToMatch{URIPath: &WebACLEmpty{}},
					TextTransformations: ruleTransformations(),
				},
			},
		},
		{
			name: "XSS",
			statement: WebACLStatementLevel3{
				XSSMatchStatement: &WebACLXSSMatchStatement{
					FieldToMatch:        WebACLFieldToMatch{URIPath: &WebACLEmpty{}},
					TextTransformations: ruleTransformations(),
				},
			},
		},
	}
}

func validWebACLRule(statement WebACLStatementLevel3) WebACLRule {
	return WebACLRule{
		Action:    validRuleAction(),
		Name:      "valid-rule",
		Priority:  0,
		Statement: statement,
		VisibilityConfig: WebACLVisibilityConfig{
			MetricName: "valid-rule-metric",
		},
	}
}

func ruleWithIdentity(name string, priority int64) WebACLRule {
	rule := validWebACLRule(ruleLeafLevel3(name))
	rule.Name = name
	rule.Priority = priority
	rule.VisibilityConfig.MetricName = name
	return rule
}

func validRuleAction() *WebACLRuleAction {
	return &WebACLRuleAction{Count: &WebACLCountAction{}}
}

func validRuleOverride() *WebACLOverrideAction {
	return &WebACLOverrideAction{None: &WebACLNoneAction{}}
}

func ruleLeafLevel3(key string) WebACLStatementLevel3 {
	return WebACLStatementLevel3{
		LabelMatchStatement: &WebACLLabelMatchStatement{Key: key, Scope: "LABEL"},
	}
}

func ruleLeafLevel2(key string) WebACLStatementLevel2 {
	return WebACLStatementLevel2{
		LabelMatchStatement: &WebACLLabelMatchStatement{Key: key, Scope: "LABEL"},
	}
}

func ruleTransformations() []WebACLTextTransformation {
	return []WebACLTextTransformation{{Priority: 0, Type: "NONE"}}
}
