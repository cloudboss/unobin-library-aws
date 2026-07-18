package wafv2

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

var (
	customHTTPHeaderNamePattern  = regexp.MustCompile(`^[0-9A-Za-z_$.-]+$`)
	customResponseBodyKeyPattern = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)
	excludedRuleNamePattern      = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)
)

func validateRuleAction(action WebACLRuleAction) error {
	if countSet(
		action.Allow != nil,
		action.Block != nil,
		action.Captcha != nil,
		action.Challenge != nil,
		action.Count != nil,
	) != 1 {
		return fmt.Errorf("rule action must contain exactly one member")
	}
	switch {
	case action.Allow != nil:
		return validateCustomRequestHandling(action.Allow.CustomRequestHandling)
	case action.Block != nil:
		return validateCustomResponse(action.Block.CustomResponse)
	case action.Captcha != nil:
		return validateCustomRequestHandling(action.Captcha.CustomRequestHandling)
	case action.Challenge != nil:
		return validateCustomRequestHandling(action.Challenge.CustomRequestHandling)
	default:
		return validateCustomRequestHandling(action.Count.CustomRequestHandling)
	}
}

func expandRuleAction(action WebACLRuleAction) (*awstypes.RuleAction, error) {
	if err := validateRuleAction(action); err != nil {
		return nil, err
	}
	out := &awstypes.RuleAction{}
	switch {
	case action.Allow != nil:
		out.Allow = &awstypes.AllowAction{
			CustomRequestHandling: expandCustomRequestHandling(
				action.Allow.CustomRequestHandling,
			),
		}
	case action.Block != nil:
		out.Block = &awstypes.BlockAction{
			CustomResponse: expandCustomResponse(action.Block.CustomResponse),
		}
	case action.Captcha != nil:
		out.Captcha = &awstypes.CaptchaAction{
			CustomRequestHandling: expandCustomRequestHandling(
				action.Captcha.CustomRequestHandling,
			),
		}
	case action.Challenge != nil:
		out.Challenge = &awstypes.ChallengeAction{
			CustomRequestHandling: expandCustomRequestHandling(
				action.Challenge.CustomRequestHandling,
			),
		}
	case action.Count != nil:
		out.Count = &awstypes.CountAction{
			CustomRequestHandling: expandCustomRequestHandling(
				action.Count.CustomRequestHandling,
			),
		}
	}
	return out, nil
}

func validateOverrideAction(action WebACLOverrideAction) error {
	if countSet(action.Count != nil, action.None != nil) != 1 {
		return fmt.Errorf("override action must contain exactly one member")
	}
	return nil
}

func expandOverrideAction(action WebACLOverrideAction) (*awstypes.OverrideAction, error) {
	if err := validateOverrideAction(action); err != nil {
		return nil, err
	}
	out := &awstypes.OverrideAction{}
	if action.Count != nil {
		out.Count = &awstypes.CountAction{}
	} else {
		out.None = &awstypes.NoneAction{}
	}
	return out, nil
}

func validateRuleGroupReferenceStatement(in *WebACLRuleGroupReferenceStatement) error {
	if in == nil {
		return fmt.Errorf("rule group reference statement is required")
	}
	if err := validateRuleGroupARN(in.ARN); err != nil {
		return err
	}
	if err := validateExcludedRules(in.ExcludedRules); err != nil {
		return err
	}
	return validateRuleActionOverrides(in.RuleActionOverrides)
}

func expandRuleGroupReferenceStatement(
	in *WebACLRuleGroupReferenceStatement,
) (*awstypes.RuleGroupReferenceStatement, error) {
	if err := validateRuleGroupReferenceStatement(in); err != nil {
		return nil, err
	}
	out := &awstypes.RuleGroupReferenceStatement{ARN: aws.String(in.ARN)}
	out.ExcludedRules = expandExcludedRules(in.ExcludedRules)
	overrides, err := expandRuleActionOverrides(in.RuleActionOverrides)
	if err != nil {
		return nil, err
	}
	out.RuleActionOverrides = overrides
	return out, nil
}

func validateCustomRequestHandling(in *WebACLCustomRequestHandling) error {
	if in == nil {
		return nil
	}
	if len(in.InsertHeaders) == 0 {
		return fmt.Errorf("insert-headers must contain at least one member")
	}
	return validateCustomHTTPHeaders("insert-headers", in.InsertHeaders, false)
}

func validateCustomResponse(in *WebACLCustomResponse) error {
	if in == nil {
		return nil
	}
	if in.ResponseCode < 200 || in.ResponseCode > 600 {
		return fmt.Errorf("custom response response-code must be 200..600")
	}
	if in.CustomResponseBodyKey != nil {
		key := *in.CustomResponseBodyKey
		if len(key) < 1 || len(key) > 128 || !customResponseBodyKeyPattern.MatchString(key) {
			return fmt.Errorf(
				"custom-response-body-key must be 1..128 letters, numbers, hyphens, or underscores",
			)
		}
	}
	if in.ResponseHeaders == nil {
		return nil
	}
	return validateCustomHTTPHeaders("response-headers", *in.ResponseHeaders, true)
}

func validateCustomHTTPHeaders(
	field string,
	headers []WebACLCustomHTTPHeader,
	forbidContentType bool,
) error {
	seen := make(map[string]struct{}, len(headers))
	for index, header := range headers {
		if len(header.Name) < 1 || len(header.Name) > 64 ||
			!customHTTPHeaderNamePattern.MatchString(header.Name) {
			return fmt.Errorf(
				"%s header %d name must be 1..64 allowed characters",
				field,
				index,
			)
		}
		name := strings.ToLower(header.Name)
		if forbidContentType && name == "content-type" {
			return fmt.Errorf("%s must not contain content-type", field)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%s must use unique names", field)
		}
		seen[name] = struct{}{}
		valueLength := utf8.RuneCountInString(header.Value)
		if valueLength < 1 || valueLength > 255 {
			return fmt.Errorf("%s header %d value must be 1..255 characters", field, index)
		}
	}
	return nil
}

func expandCustomRequestHandling(
	in *WebACLCustomRequestHandling,
) *awstypes.CustomRequestHandling {
	if in == nil {
		return nil
	}
	return &awstypes.CustomRequestHandling{
		InsertHeaders: expandCustomHTTPHeaders(in.InsertHeaders),
	}
}

func expandCustomResponse(in *WebACLCustomResponse) *awstypes.CustomResponse {
	if in == nil {
		return nil
	}
	out := &awstypes.CustomResponse{ResponseCode: aws.Int32(int32(in.ResponseCode))}
	if in.CustomResponseBodyKey != nil {
		out.CustomResponseBodyKey = aws.String(*in.CustomResponseBodyKey)
	}
	if in.ResponseHeaders != nil {
		out.ResponseHeaders = expandCustomHTTPHeaders(*in.ResponseHeaders)
	}
	return out
}

func expandCustomHTTPHeaders(headers []WebACLCustomHTTPHeader) []awstypes.CustomHTTPHeader {
	out := make([]awstypes.CustomHTTPHeader, len(headers))
	for index, header := range headers {
		out[index] = awstypes.CustomHTTPHeader{
			Name:  aws.String(header.Name),
			Value: aws.String(header.Value),
		}
	}
	return out
}

func validateRuleGroupARN(value string) error {
	if value == "" {
		return fmt.Errorf("rule group ARN must not be empty")
	}
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return fmt.Errorf("rule group ARN is invalid: %w", err)
	}
	if parsed.Service != "wafv2" {
		return fmt.Errorf("rule group ARN must use the wafv2 service")
	}
	parts := strings.Split(parsed.Resource, "/")
	if len(parts) != 4 || parts[1] != "rulegroup" || parts[2] == "" || parts[3] == "" {
		return fmt.Errorf("rule group ARN has invalid resource %q", parsed.Resource)
	}
	if parts[0] != "regional" && parts[0] != "global" {
		return fmt.Errorf("rule group ARN has invalid scope %q", parts[0])
	}
	return nil
}

func validateExcludedRules(rules *[]WebACLExcludedRule) error {
	if rules == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(*rules))
	for index, rule := range *rules {
		if len(rule.Name) < 1 || len(rule.Name) > 128 ||
			!excludedRuleNamePattern.MatchString(rule.Name) {
			return fmt.Errorf(
				"excluded rule name at index %d must be 1..128 letters, numbers, underscores, or hyphens",
				index,
			)
		}
		if _, exists := seen[rule.Name]; exists {
			return fmt.Errorf("excluded rule names must be unique")
		}
		seen[rule.Name] = struct{}{}
	}
	return nil
}

func validateRuleActionOverrides(overrides *[]WebACLRuleActionOverride) error {
	if overrides == nil {
		return nil
	}
	if len(*overrides) > 100 {
		return fmt.Errorf("rule-action-overrides must contain 0..100 members")
	}
	seen := make(map[string]struct{}, len(*overrides))
	for index, override := range *overrides {
		nameLength := utf8.RuneCountInString(override.Name)
		if nameLength < 1 || nameLength > 128 {
			return fmt.Errorf("rule action override name at index %d must be 1..128 characters", index)
		}
		if _, exists := seen[override.Name]; exists {
			return fmt.Errorf("rule action override names must be unique")
		}
		seen[override.Name] = struct{}{}
		if err := validateRuleAction(override.ActionToUse); err != nil {
			return fmt.Errorf("rule action override %q: %w", override.Name, err)
		}
	}
	return nil
}

func expandExcludedRules(rules *[]WebACLExcludedRule) []awstypes.ExcludedRule {
	if rules == nil {
		return nil
	}
	out := make([]awstypes.ExcludedRule, len(*rules))
	for index, rule := range *rules {
		out[index] = awstypes.ExcludedRule{Name: aws.String(rule.Name)}
	}
	return out
}

func expandRuleActionOverrides(
	overrides *[]WebACLRuleActionOverride,
) ([]awstypes.RuleActionOverride, error) {
	if overrides == nil {
		return nil, nil
	}
	out := make([]awstypes.RuleActionOverride, len(*overrides))
	for index, override := range *overrides {
		action, err := expandRuleAction(override.ActionToUse)
		if err != nil {
			return nil, fmt.Errorf("convert rule action override %d: %w", index, err)
		}
		out[index] = awstypes.RuleActionOverride{
			ActionToUse: action,
			Name:        aws.String(override.Name),
		}
	}
	return out, nil
}
