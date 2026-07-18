package wafv2

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

var (
	ruleNamePattern             = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)
	ruleLabelPattern            = regexp.MustCompile(`^[0-9A-Za-z_:-]+$`)
	ruleLabelReservedComponents = map[string]struct{}{
		"aws":             {},
		"ipset":           {},
		"managed":         {},
		"regexpatternset": {},
		"rulegroup":       {},
		"waf":             {},
		"webacl":          {},
	}
)

func validateRules(rules *[]WebACLRule) error {
	if rules == nil {
		return nil
	}
	names := make(map[string]struct{}, len(*rules))
	priorities := make(map[int64]struct{}, len(*rules))
	for index, rule := range *rules {
		if err := validateRule(rule); err != nil {
			return fmt.Errorf("rule item %d: %w", index, err)
		}
		if _, exists := names[rule.Name]; exists {
			return fmt.Errorf("rule names must be unique")
		}
		names[rule.Name] = struct{}{}
		if _, exists := priorities[rule.Priority]; exists {
			return fmt.Errorf("rule priorities must be unique")
		}
		priorities[rule.Priority] = struct{}{}
	}
	return nil
}

func expandRules(rules *[]WebACLRule) ([]awstypes.Rule, error) {
	if err := validateRules(rules); err != nil {
		return nil, err
	}
	if rules == nil || len(*rules) == 0 {
		return nil, nil
	}
	out := make([]awstypes.Rule, len(*rules))
	for index, rule := range *rules {
		converted, err := expandRule(rule)
		if err != nil {
			return nil, fmt.Errorf("convert rule item %d: %w", index, err)
		}
		out[index] = converted
	}
	return out, nil
}

func validateRule(rule WebACLRule) error {
	nameLength := utf8.RuneCountInString(rule.Name)
	if nameLength < 1 || nameLength > 128 {
		return fmt.Errorf("rule name must be 1..128 characters")
	}
	if !ruleNamePattern.MatchString(rule.Name) {
		return fmt.Errorf("rule name must match ^[0-9A-Za-z_-]+$")
	}
	if rule.Priority < 0 || rule.Priority > math.MaxInt32 {
		return fmt.Errorf("priority must be 0..%d", math.MaxInt32)
	}
	if err := validateStatementLevel3(rule.Statement); err != nil {
		return err
	}
	if err := validateVisibilityConfig(rule.VisibilityConfig); err != nil {
		return err
	}
	if err := validateCaptchaConfig(rule.CaptchaConfig); err != nil {
		return err
	}
	if err := validateChallengeConfig(rule.ChallengeConfig); err != nil {
		return err
	}

	groupReference := rule.Statement.ManagedRuleGroupStatement != nil ||
		rule.Statement.RuleGroupReferenceStatement != nil
	if groupReference {
		if rule.Action != nil {
			return fmt.Errorf("group reference rule must not specify action")
		}
		if rule.OverrideAction == nil {
			return fmt.Errorf("group reference rule requires override-action")
		}
		if rule.RuleLabels != nil && len(*rule.RuleLabels) > 0 {
			return fmt.Errorf("group reference rule must not specify rule-labels")
		}
		return validateOverrideAction(*rule.OverrideAction)
	}
	if rule.Action == nil {
		return fmt.Errorf("non-group rule requires action")
	}
	if rule.OverrideAction != nil {
		return fmt.Errorf("non-group rule must not specify override-action")
	}
	if err := validateRuleAction(*rule.Action); err != nil {
		return err
	}
	if rule.Statement.RateBasedStatement != nil && rule.Action.Allow != nil {
		return fmt.Errorf("rate-based rule must not use allow action")
	}
	return validateRuleLabels(rule.RuleLabels)
}

func expandRule(rule WebACLRule) (awstypes.Rule, error) {
	if err := validateRule(rule); err != nil {
		return awstypes.Rule{}, err
	}
	statement, err := expandStatementLevel3(rule.Statement)
	if err != nil {
		return awstypes.Rule{}, err
	}
	captchaConfig, err := expandCaptchaConfig(rule.CaptchaConfig)
	if err != nil {
		return awstypes.Rule{}, err
	}
	challengeConfig, err := expandChallengeConfig(rule.ChallengeConfig)
	if err != nil {
		return awstypes.Rule{}, err
	}
	out := awstypes.Rule{
		CaptchaConfig:    captchaConfig,
		ChallengeConfig:  challengeConfig,
		Name:             aws.String(rule.Name),
		Priority:         int32(rule.Priority),
		RuleLabels:       expandRuleLabels(rule.RuleLabels),
		Statement:        statement,
		VisibilityConfig: expandVisibilityConfig(rule.VisibilityConfig),
	}
	if rule.Action != nil {
		out.Action, err = expandRuleAction(*rule.Action)
		if err != nil {
			return awstypes.Rule{}, err
		}
	}
	if rule.OverrideAction != nil {
		out.OverrideAction, err = expandOverrideAction(*rule.OverrideAction)
		if err != nil {
			return awstypes.Rule{}, err
		}
	}
	return out, nil
}

func validateRuleLabels(labels *[]WebACLRuleLabel) error {
	if labels == nil {
		return nil
	}
	for index, label := range *labels {
		if err := validateRuleLabel(label.Name); err != nil {
			return fmt.Errorf("rule-label item %d: %w", index, err)
		}
	}
	return nil
}

func validateRuleLabel(value string) error {
	length := utf8.RuneCountInString(value)
	if length < 1 || length > 1024 {
		return fmt.Errorf("rule-label must be 1..1024 characters")
	}
	if !ruleLabelPattern.MatchString(value) {
		return fmt.Errorf("rule-label must match ^[0-9A-Za-z_:-]+$")
	}
	components := strings.Split(value, ":")
	if len(components) > 6 {
		return fmt.Errorf("rule-label must contain at most 5 namespaces")
	}
	for _, component := range components {
		componentLength := utf8.RuneCountInString(component)
		if componentLength < 1 || componentLength > 128 {
			return fmt.Errorf("rule-label components must be 1..128 characters")
		}
		if _, reserved := ruleLabelReservedComponents[component]; reserved {
			return fmt.Errorf("rule-label component is reserved")
		}
	}
	return nil
}

func expandRuleLabels(labels *[]WebACLRuleLabel) []awstypes.Label {
	if labels == nil || len(*labels) == 0 {
		return nil
	}
	out := make([]awstypes.Label, len(*labels))
	for index, label := range *labels {
		out[index] = awstypes.Label{Name: aws.String(label.Name)}
	}
	return out
}
