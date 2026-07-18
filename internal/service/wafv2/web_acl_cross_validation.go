package wafv2

import "fmt"

func validateCustomResponseBodyReferences(resource *WebACLResource) error {
	if err := validateBlockBodyReference(
		"default-action block",
		resource.DefaultAction.Block,
		resource.CustomResponseBodies,
	); err != nil {
		return err
	}
	if resource.Rules == nil {
		return nil
	}
	for ruleIndex := range *resource.Rules {
		rule := &(*resource.Rules)[ruleIndex]
		if rule.Action != nil {
			if err := validateBlockBodyReference(
				fmt.Sprintf("rules item %d action block", ruleIndex),
				rule.Action.Block,
				resource.CustomResponseBodies,
			); err != nil {
				return err
			}
		}
		managed := rule.Statement.ManagedRuleGroupStatement
		if managed != nil {
			if err := validateOverrideBodyReferences(
				fmt.Sprintf("rules item %d managed-rule-group-statement", ruleIndex),
				managed.RuleActionOverrides,
				resource.CustomResponseBodies,
			); err != nil {
				return err
			}
		}
		group := rule.Statement.RuleGroupReferenceStatement
		if group != nil {
			if err := validateOverrideBodyReferences(
				fmt.Sprintf("rules item %d rule-group-reference-statement", ruleIndex),
				group.RuleActionOverrides,
				resource.CustomResponseBodies,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOverrideBodyReferences(
	location string,
	overrides *[]WebACLRuleActionOverride,
	bodies *map[string]WebACLCustomResponseBody,
) error {
	if overrides == nil {
		return nil
	}
	for overrideIndex := range *overrides {
		override := &(*overrides)[overrideIndex]
		if err := validateBlockBodyReference(
			fmt.Sprintf(
				"%s rule-action-overrides item %d action-to-use block",
				location,
				overrideIndex,
			),
			override.ActionToUse.Block,
			bodies,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateBlockBodyReference(
	location string,
	block *WebACLBlockAction,
	bodies *map[string]WebACLCustomResponseBody,
) error {
	if block == nil || block.CustomResponse == nil ||
		block.CustomResponse.CustomResponseBodyKey == nil {
		return nil
	}
	key := *block.CustomResponse.CustomResponseBodyKey
	if bodies != nil {
		if _, exists := (*bodies)[key]; exists {
			return nil
		}
	}
	return fmt.Errorf(
		"%s custom-response-body-key %q does not exist in custom-response-bodies",
		location,
		key,
	)
}

func validateManagedResponseInspectionScope(resource *WebACLResource) error {
	if resource.Scope == "CLOUDFRONT" || resource.Rules == nil {
		return nil
	}
	for ruleIndex := range *resource.Rules {
		managed := (*resource.Rules)[ruleIndex].Statement.ManagedRuleGroupStatement
		if managed == nil || managed.ManagedRuleGroupConfigs == nil {
			continue
		}
		for configIndex, config := range *managed.ManagedRuleGroupConfigs {
			if config.AWSManagedRulesACFPRuleSet != nil &&
				config.AWSManagedRulesACFPRuleSet.ResponseInspection != nil {
				return managedResponseInspectionScopeError(ruleIndex, configIndex, "ACFP")
			}
			if config.AWSManagedRulesATPRuleSet != nil &&
				config.AWSManagedRulesATPRuleSet.ResponseInspection != nil {
				return managedResponseInspectionScopeError(ruleIndex, configIndex, "ATP")
			}
		}
	}
	return nil
}

func managedResponseInspectionScopeError(ruleIndex, configIndex int, kind string) error {
	return fmt.Errorf(
		"rules item %d managed-rule-group-configs item %d %s response-inspection "+
			"requires CLOUDFRONT scope",
		ruleIndex,
		configIndex,
		kind,
	)
}
