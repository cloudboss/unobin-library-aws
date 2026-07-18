package wafv2

import (
	"context"
	"fmt"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

var shieldMitigationRuleNamePattern = regexp.MustCompile(
	`^ShieldMitigationRuleGroup_\d{12}_[0-9A-Fa-f]{8}-` +
		`[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-` +
		`[0-9A-Fa-f]{12}_.*`,
)

func isShieldMitigationRuleName(name string) bool {
	return shieldMitigationRuleNamePattern.MatchString(name)
}

func prepareWebACLUpdateInput(
	ctx context.Context,
	client wafClient,
	input *awssvc.UpdateWebACLInput,
) (*awssvc.UpdateWebACLInput, error) {
	if input == nil {
		return nil, fmt.Errorf("prepare web ACL update: input is required")
	}
	for _, rule := range input.Rules {
		if isShieldMitigationRuleName(aws.ToString(rule.Name)) {
			return input, nil
		}
	}
	id := aws.ToString(input.Id)
	name := aws.ToString(input.Name)
	if id == "" || name == "" {
		return nil, fmt.Errorf("prepare web ACL update: input identity is incomplete")
	}

	snapshot, err := client.GetWebACL(ctx, &awssvc.GetWebACLInput{
		Id:    aws.String(id),
		Name:  aws.String(name),
		Scope: input.Scope,
	})
	if err != nil {
		return nil, fmt.Errorf("get current web ACL %s: %w", id, err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("get current web ACL %s: empty response", id)
	}
	if snapshot.WebACL == nil {
		return nil, fmt.Errorf("get current web ACL %s: response has no web ACL", id)
	}
	if aws.ToString(snapshot.LockToken) == "" {
		return nil, fmt.Errorf("get current web ACL %s: response has no lock-token", id)
	}
	identity := webACLIdentity{ID: id, Name: name, Scope: input.Scope}
	if _, err := webACLOutput(identity, snapshot); err != nil {
		return nil, fmt.Errorf("get current web ACL %s: %w", id, err)
	}

	shieldRules := make([]awstypes.Rule, 0)
	for _, rule := range snapshot.WebACL.Rules {
		if isShieldMitigationRuleName(aws.ToString(rule.Name)) {
			shieldRules = append(shieldRules, rule)
		}
	}
	merged := input.Rules
	if len(shieldRules) > 0 {
		merged = make([]awstypes.Rule, len(input.Rules), len(input.Rules)+len(shieldRules))
		copy(merged, input.Rules)
		merged = append(merged, shieldRules...)
	}
	if err := validatePreparedWebACLRules(merged); err != nil {
		return nil, err
	}
	prepared := *input
	prepared.LockToken = aws.String(aws.ToString(snapshot.LockToken))
	prepared.Rules = merged
	return &prepared, nil
}

func validatePreparedWebACLRules(rules []awstypes.Rule) error {
	names := make(map[string]struct{}, len(rules))
	for index, rule := range rules {
		name := aws.ToString(rule.Name)
		if name == "" {
			return fmt.Errorf("rule item %d name must not be empty", index)
		}
		if _, exists := names[name]; exists {
			return fmt.Errorf("rule names must be unique")
		}
		names[name] = struct{}{}
	}
	priorities := make(map[int32]struct{}, len(rules))
	for _, rule := range rules {
		if _, exists := priorities[rule.Priority]; exists {
			return fmt.Errorf("rule priorities must be unique")
		}
		priorities[rule.Priority] = struct{}{}
	}
	return nil
}
