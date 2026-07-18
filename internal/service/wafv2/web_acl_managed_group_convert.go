package wafv2

import (
	"fmt"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

func validateManagedRuleGroupStatement(in *WebACLManagedRuleGroupStatement) error {
	if in == nil {
		return fmt.Errorf("managed rule group statement is required")
	}
	if err := validateManagedRuleGroupText("name", in.Name); err != nil {
		return err
	}
	if err := validateManagedRuleGroupText("vendor-name", in.VendorName); err != nil {
		return err
	}
	if in.Version != nil {
		if err := validateManagedRuleGroupText("version", *in.Version); err != nil {
			return err
		}
	}
	if err := validateManagedRuleGroupConfigs(in.ManagedRuleGroupConfigs); err != nil {
		return err
	}
	if err := validateExcludedRules(in.ExcludedRules); err != nil {
		return err
	}
	if err := validateRuleActionOverrides(in.RuleActionOverrides); err != nil {
		return err
	}
	if in.ScopeDownStatement != nil {
		if err := validateStatementLevel2(*in.ScopeDownStatement); err != nil {
			return fmt.Errorf("scope-down-statement: %w", err)
		}
	}
	return nil
}

func expandManagedRuleGroupStatement(
	in *WebACLManagedRuleGroupStatement,
) (*awstypes.ManagedRuleGroupStatement, error) {
	if err := validateManagedRuleGroupStatement(in); err != nil {
		return nil, err
	}
	overrides, err := expandRuleActionOverrides(in.RuleActionOverrides)
	if err != nil {
		return nil, err
	}
	configs, err := expandManagedRuleGroupConfigs(in.ManagedRuleGroupConfigs)
	if err != nil {
		return nil, err
	}
	out := &awstypes.ManagedRuleGroupStatement{
		ExcludedRules:           expandExcludedRules(in.ExcludedRules),
		ManagedRuleGroupConfigs: configs,
		Name:                    aws.String(in.Name),
		RuleActionOverrides:     overrides,
		VendorName:              aws.String(in.VendorName),
	}
	if in.ScopeDownStatement != nil {
		scopeDown, err := expandStatementLevel2(*in.ScopeDownStatement)
		if err != nil {
			return nil, fmt.Errorf("convert scope-down-statement: %w", err)
		}
		out.ScopeDownStatement = scopeDown
	}
	if in.Version != nil {
		out.Version = aws.String(*in.Version)
	}
	return out, nil
}

func validateManagedRuleGroupText(field, value string) error {
	length := utf8.RuneCountInString(value)
	if length < 1 || length > 128 {
		return fmt.Errorf("%s must be 1..128 characters", field)
	}
	return nil
}
