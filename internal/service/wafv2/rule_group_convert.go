package wafv2

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

type preparedRuleGroup struct {
	rules     []awstypes.Rule
	rulesJSON *string
	bodies    map[string]awstypes.CustomResponseBody
	tags      map[string]string
}

func (r *RuleGroupResource) prepareRuleGroup() (preparedRuleGroup, error) {
	prepared := preparedRuleGroup{tags: wafUserTags(ptr.Value(r.Tags))}
	if len(ptr.Value(r.CustomResponseBodies)) > 0 {
		prepared.bodies = expandCustomResponseBodies(r.CustomResponseBodies)
	}
	if r.RulesJSON != nil {
		rules, normalized, err := parseRuleGroupJSON(*r.RulesJSON)
		if err != nil {
			return preparedRuleGroup{}, fmt.Errorf("rules-json: %w", err)
		}
		prepared.rules, prepared.rulesJSON = rules, aws.String(normalized)
		return prepared, nil
	}
	prepared.rules = expandRuleGroupRules(r.Rules)
	for _, rule := range prepared.rules {
		if err := parseRuleGroupReferences(rule.Statement); err != nil {
			return preparedRuleGroup{}, fmt.Errorf("rule %q: %w", aws.ToString(rule.Name), err)
		}
	}
	return prepared, nil
}

func normalizedRuleGroupJSON(source string) ([]byte, error) {
	var value any
	if err := json.Unmarshal([]byte(source), &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func parseRuleGroupJSON(source string) ([]awstypes.Rule, string, error) {
	normalized, err := normalizedRuleGroupJSON(source)
	if err != nil {
		return nil, "", err
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(normalized, &entries); err != nil {
		return nil, "", fmt.Errorf("expected an array of rules: %w", err)
	}
	if entries == nil {
		return nil, string(normalized), nil
	}
	rules := make([]awstypes.Rule, len(entries))
	for i, entry := range entries {
		processed, err := rewriteRuleGroupJSONField(entry, "Statement", literalRuleGroupSearch)
		if err != nil {
			return nil, "", fmt.Errorf("rule %d: %w", i, err)
		}
		if err := json.Unmarshal(processed, &rules[i]); err != nil {
			return nil, "", fmt.Errorf("rule %d: %w", i, err)
		}
		if reflect.ValueOf(rules[i]).IsZero() {
			return nil, "", fmt.Errorf("rule %d has no recognized rule members", i)
		}
	}
	return rules, string(normalized), nil
}

func literalRuleGroupSearch(raw json.RawMessage) (json.RawMessage, error) {
	if raw[0] != '{' {
		return raw, nil
	}
	var statement map[string]json.RawMessage
	if err := json.Unmarshal(raw, &statement); err != nil {
		return nil, err
	}
	for key, value := range statement {
		var converted json.RawMessage
		var err error
		switch {
		case key == "ByteMatchStatement":
			converted, err = rewriteRuleGroupJSONField(value, "SearchString", encodeRuleGroupSearch)
		case strings.EqualFold(key, "AndStatement") || strings.EqualFold(key, "OrStatement"):
			converted, err = rewriteRuleGroupJSONField(value, "Statements", literalRuleGroupChildren)
		case strings.EqualFold(key, "NotStatement"):
			converted, err = rewriteRuleGroupJSONField(value, "Statement", literalRuleGroupSearch)
		case strings.EqualFold(key, "RateBasedStatement") ||
			strings.EqualFold(key, "ManagedRuleGroupStatement"):
			converted, err = rewriteRuleGroupJSONField(value, "ScopeDownStatement",
				literalRuleGroupSearch)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		statement[key] = converted
	}
	return json.Marshal(statement)
}

func rewriteRuleGroupJSONField(
	raw json.RawMessage, field string, rewrite func(json.RawMessage) (json.RawMessage, error),
) (json.RawMessage, error) {
	if raw[0] != '{' {
		return raw, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	for key, value := range object {
		if key != field && (field == "SearchString" || !strings.EqualFold(key, field)) {
			continue
		}
		converted, err := rewrite(value)
		if err != nil {
			return nil, err
		}
		object[key] = converted
	}
	return json.Marshal(object)
}

func literalRuleGroupChildren(raw json.RawMessage) (json.RawMessage, error) {
	if raw[0] != '[' {
		return raw, nil
	}
	var children []json.RawMessage
	if err := json.Unmarshal(raw, &children); err != nil {
		return nil, err
	}
	for i, child := range children {
		converted, err := literalRuleGroupSearch(child)
		if err != nil {
			return nil, err
		}
		children[i] = converted
	}
	return json.Marshal(children)
}

func encodeRuleGroupSearch(raw json.RawMessage) (json.RawMessage, error) {
	var text string
	if raw[0] != '"' || json.Unmarshal(raw, &text) != nil {
		return nil, fmt.Errorf("ByteMatchStatement.SearchString must be a string")
	}
	// The SDK's JSON byte slice decoder expects base64.
	return json.Marshal(base64.StdEncoding.EncodeToString([]byte(text)))
}

var (
	ruleGroupARNPartition = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	ruleGroupARNRegion    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-[0-9]{1,2}$`)
	ruleGroupARNAccount   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|partner-managed|[0-9]{12}|cw.{10})$`,
	)
)

func parseRuleGroupARN(value string) error {
	if value == "" {
		return nil
	}
	arn, err := awsarn.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid reference ARN %q: %w", value, err)
	}
	switch {
	case !ruleGroupARNPartition.MatchString(arn.Partition):
		return fmt.Errorf("invalid reference ARN %q: invalid partition", value)
	case arn.Region != "" && !ruleGroupARNRegion.MatchString(arn.Region):
		return fmt.Errorf("invalid reference ARN %q: invalid region", value)
	case arn.AccountID != "" && !ruleGroupARNAccount.MatchString(arn.AccountID):
		return fmt.Errorf("invalid reference ARN %q: invalid account", value)
	case arn.Resource == "":
		return fmt.Errorf("invalid reference ARN %q: empty resource", value)
	default:
		return nil
	}
}

func parseRuleGroupReferences(statement *awstypes.Statement) error {
	switch {
	case statement == nil:
		return nil
	case statement.IPSetReferenceStatement != nil:
		return parseRuleGroupARN(aws.ToString(statement.IPSetReferenceStatement.ARN))
	case statement.RegexPatternSetReferenceStatement != nil:
		return parseRuleGroupARN(aws.ToString(statement.RegexPatternSetReferenceStatement.ARN))
	case statement.NotStatement != nil:
		return parseRuleGroupReferences(statement.NotStatement.Statement)
	case statement.RateBasedStatement != nil:
		return parseRuleGroupReferences(statement.RateBasedStatement.ScopeDownStatement)
	}
	var children []awstypes.Statement
	switch {
	case statement.AndStatement != nil:
		children = statement.AndStatement.Statements
	case statement.OrStatement != nil:
		children = statement.OrStatement.Statements
	}
	for i := range children {
		if err := parseRuleGroupReferences(&children[i]); err != nil {
			return err
		}
	}
	return nil
}

func ruleGroupJSONEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	var left, right any
	if err := json.Unmarshal([]byte(*a), &left); err != nil {
		return *a == *b
	}
	if err := json.Unmarshal([]byte(*b), &right); err != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func (r RuleGroupResource) RuleSetEqual(prior RuleGroupResource) bool {
	return ruleGroupRulesEqual(r.Rules, prior.Rules)
}

func ruleGroupRulesEqual(a, b *[]RuleGroupRule) bool {
	return slices.Equal(
		slices.Sorted(maps.Keys(ruleGroupRuleSet(ptr.Value(a)))),
		slices.Sorted(maps.Keys(ruleGroupRuleSet(ptr.Value(b)))),
	)
}

func ruleGroupBodiesEqual(a, b *map[string]WebACLCustomResponseBody) bool {
	return maps.Equal(ptr.Value(a), ptr.Value(b))
}

func ruleGroupTagsEqual(a, b *map[string]string) bool {
	return maps.Equal(wafUserTags(ptr.Value(a)), wafUserTags(ptr.Value(b)))
}

func sortRuleGroupSet[T any](values []T, key func(T) string) []T {
	if len(values) < 2 {
		return slices.Clone(values)
	}
	unique := make(map[string]T, len(values))
	for _, value := range values {
		unique[key(value)] = value
	}
	keys := slices.Sorted(maps.Keys(unique))
	out := make([]T, 0, len(keys))
	for _, key := range keys {
		out = append(out, unique[key])
	}
	return out
}

func ruleGroupRuleSet(rules []RuleGroupRule) map[string]RuleGroupRule {
	out := make(map[string]RuleGroupRule, len(rules))
	for _, rule := range rules {
		out[ruleGroupRuleKey(rule)] = rule
	}
	return out
}

func ruleGroupRuleKey(rule RuleGroupRule) string {
	data, _ := json.Marshal(rule)
	decoder := json.NewDecoder(bytes.NewReader(data))
	// Keep typed integers exact, including priorities that the AWS request narrows to int32.
	decoder.UseNumber()
	var value map[string]any
	_ = decoder.Decode(&value)
	canonicalizeRuleGroupKey(value)
	return ruleGroupJSONKey(value)
}

func canonicalizeRuleGroupKey(value any) {
	switch value := value.(type) {
	case map[string]any:
		for field, member := range value {
			switch field {
			case "InsertHeaders", "ResponseHeaders", "RuleLabels", "TextTransformations":
				items, _ := member.([]any)
				if len(items) == 0 {
					value[field] = nil
				} else {
					value[field] = sortRuleGroupSet(items, ruleGroupJSONKey)
				}
				continue
			case "RateBasedStatement":
				if rate, ok := member.(map[string]any); ok {
					if rate["AggregateKeyType"] == nil {
						rate["AggregateKeyType"] = awstypes.RateBasedStatementAggregateKeyTypeIp
					}
					if rate["EvaluationWindowSec"] == nil {
						rate["EvaluationWindowSec"] = ruleGroupDefaultRateWindow
					}
				}
			case "Body", "JSONBody":
				if body, ok := member.(map[string]any); ok && body["OversizeHandling"] == nil {
					body["OversizeHandling"] = awstypes.OversizeHandlingContinue
				}
			case "ImmunityTimeProperty":
				if property, ok := member.(map[string]any); ok && property["ImmunityTime"] == nil {
					property["ImmunityTime"] = int64(0)
				}
			case "ExcludedCookies", "IncludedCookies":
				items, _ := member.([]any)
				if len(items) == 0 {
					value[field] = nil
				}
			}
			canonicalizeRuleGroupKey(member)
		}
	case []any:
		for _, member := range value {
			canonicalizeRuleGroupKey(member)
		}
	}
}

func ruleGroupJSONKey(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func ruleGroupHeaderKey(header WebACLCustomHTTPHeader) string {
	data, _ := json.Marshal(header)
	return string(data)
}

func sortedRuleGroupTransforms(in []WebACLTextTransformation) []WebACLTextTransformation {
	values := slices.Clone(in)
	slices.SortFunc(values, func(a, b WebACLTextTransformation) int {
		if a.Priority != b.Priority {
			return cmp.Compare(a.Priority, b.Priority)
		}
		return cmp.Compare(a.Type, b.Type)
	})
	return slices.Compact(values)
}

func ruleGroupTransforms(in []WebACLTextTransformation) []awstypes.TextTransformation {
	return expandTextTransformations(sortedRuleGroupTransforms(in))
}

func expandRuleGroupRules(input *[]RuleGroupRule) []awstypes.Rule {
	rules := ruleGroupRuleSet(ptr.Value(input))
	var out []awstypes.Rule
	for _, key := range slices.Sorted(maps.Keys(rules)) {
		rule := rules[key]
		out = append(out, awstypes.Rule{
			Action:           expandRuleGroupAction(rule.Action),
			CaptchaConfig:    expandRuleGroupCaptcha(rule.CaptchaConfig),
			Name:             aws.String(rule.Name),
			Priority:         int32(rule.Priority),
			RuleLabels:       expandRuleGroupLabels(rule.RuleLabels),
			Statement:        expandRuleGroupLevel3(rule.Statement),
			VisibilityConfig: expandVisibilityConfig(rule.VisibilityConfig),
		})
	}
	return out
}

func expandRuleGroupLabels(input *[]WebACLRuleLabel) []awstypes.Label {
	if len(ptr.Value(input)) == 0 {
		return nil
	}
	labels := sortRuleGroupSet(*input, func(label WebACLRuleLabel) string { return label.Name })
	return expandRuleLabels(&labels)
}

func expandRuleGroupCaptcha(input *RuleGroupCaptchaConfig) *awstypes.CaptchaConfig {
	result := &awstypes.CaptchaConfig{}
	if input != nil && input.ImmunityTimeProperty != nil {
		result.ImmunityTimeProperty = &awstypes.ImmunityTimeProperty{
			ImmunityTime: aws.Int64(ptr.Value(input.ImmunityTimeProperty.ImmunityTime)),
		}
	}
	return result
}

func expandRuleGroupAction(action WebACLRuleAction) *awstypes.RuleAction {
	out := &awstypes.RuleAction{}
	switch {
	case action.Allow != nil:
		out.Allow = &awstypes.AllowAction{
			CustomRequestHandling: expandRuleGroupRequestHandling(action.Allow.CustomRequestHandling),
		}
	case action.Block != nil:
		out.Block = &awstypes.BlockAction{
			CustomResponse: expandRuleGroupResponse(action.Block.CustomResponse),
		}
	case action.Captcha != nil:
		out.Captcha = &awstypes.CaptchaAction{
			CustomRequestHandling: expandRuleGroupRequestHandling(action.Captcha.CustomRequestHandling),
		}
	case action.Challenge != nil:
		out.Challenge = &awstypes.ChallengeAction{
			CustomRequestHandling: expandRuleGroupRequestHandling(
				action.Challenge.CustomRequestHandling,
			),
		}
	case action.Count != nil:
		out.Count = &awstypes.CountAction{
			CustomRequestHandling: expandRuleGroupRequestHandling(action.Count.CustomRequestHandling),
		}
	}
	return out
}

func expandRuleGroupRequestHandling(
	input *WebACLCustomRequestHandling,
) *awstypes.CustomRequestHandling {
	if input == nil {
		return nil
	}
	return expandCustomRequestHandling(&WebACLCustomRequestHandling{
		InsertHeaders: sortRuleGroupSet(input.InsertHeaders, ruleGroupHeaderKey),
	})
}

func expandRuleGroupResponse(input *WebACLCustomResponse) *awstypes.CustomResponse {
	if input == nil {
		return nil
	}
	value := *input
	value.ResponseHeaders = nil
	if len(ptr.Value(input.ResponseHeaders)) > 0 {
		value.ResponseHeaders = ptr.To(sortRuleGroupSet(*input.ResponseHeaders, ruleGroupHeaderKey))
	}
	return expandCustomResponse(&value)
}
