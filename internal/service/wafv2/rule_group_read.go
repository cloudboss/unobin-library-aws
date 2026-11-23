package wafv2

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

func flattenRuleGroupRules(in []awstypes.Rule) []RuleGroupRule {
	out := make([]RuleGroupRule, 0, len(in))
	for _, rule := range in {
		out = append(out, RuleGroupRule{
			Action:           flattenRuleGroupAction(rule.Action),
			CaptchaConfig:    flattenRuleGroupCaptcha(rule.CaptchaConfig),
			Name:             aws.ToString(rule.Name),
			Priority:         int64(rule.Priority),
			RuleLabels:       flattenRuleGroupLabels(rule.RuleLabels),
			Statement:        flattenRuleGroupLevel3(rule.Statement),
			VisibilityConfig: flattenRuleGroupVisibility(rule.VisibilityConfig),
		})
	}
	return sortRuleGroupSet(out, ruleGroupRuleKey)
}

func flattenRuleGroupVisibility(in *awstypes.VisibilityConfig) WebACLVisibilityConfig {
	if in == nil {
		return WebACLVisibilityConfig{}
	}
	return WebACLVisibilityConfig{
		CloudWatchMetricsEnabled: in.CloudWatchMetricsEnabled,
		MetricName:               aws.ToString(in.MetricName),
		SampledRequestsEnabled:   in.SampledRequestsEnabled,
	}
}

func flattenRuleGroupBodies(
	in map[string]awstypes.CustomResponseBody,
) map[string]WebACLCustomResponseBody {
	out := make(map[string]WebACLCustomResponseBody, len(in))
	for name, value := range in {
		out[name] = WebACLCustomResponseBody{
			Content: aws.ToString(value.Content), ContentType: string(value.ContentType),
		}
	}
	return out
}

func flattenRuleGroupCaptcha(in *awstypes.CaptchaConfig) *RuleGroupCaptchaConfig {
	if in == nil || in.ImmunityTimeProperty == nil {
		return nil
	}
	return &RuleGroupCaptchaConfig{ImmunityTimeProperty: &RuleGroupImmunityTimeProperty{
		ImmunityTime: in.ImmunityTimeProperty.ImmunityTime,
	}}
}

func flattenRuleGroupLabels(in []awstypes.Label) *[]WebACLRuleLabel {
	labels := make([]WebACLRuleLabel, 0, len(in))
	for _, label := range in {
		labels = append(labels, WebACLRuleLabel{Name: aws.ToString(label.Name)})
	}
	return optionalRuleGroupList(sortRuleGroupSet(labels, func(label WebACLRuleLabel) string {
		return label.Name
	}))
}

func flattenRuleGroupAction(in *awstypes.RuleAction) WebACLRuleAction {
	out := WebACLRuleAction{}
	if in == nil {
		return out
	}
	if in.Allow != nil {
		out.Allow = &WebACLAllowAction{
			CustomRequestHandling: flattenRuleGroupRequestHandling(in.Allow.CustomRequestHandling),
		}
	}
	if in.Captcha != nil {
		out.Captcha = &WebACLCaptchaAction{
			CustomRequestHandling: flattenRuleGroupRequestHandling(in.Captcha.CustomRequestHandling),
		}
	}
	if in.Challenge != nil {
		out.Challenge = &WebACLChallengeAction{
			CustomRequestHandling: flattenRuleGroupRequestHandling(in.Challenge.CustomRequestHandling),
		}
	}
	if in.Count != nil {
		out.Count = &WebACLCountAction{
			CustomRequestHandling: flattenRuleGroupRequestHandling(in.Count.CustomRequestHandling),
		}
	}
	if in.Block != nil {
		out.Block = &WebACLBlockAction{}
		if response := in.Block.CustomResponse; response != nil {
			out.Block.CustomResponse = &WebACLCustomResponse{
				ResponseCode:          int64(aws.ToInt32(response.ResponseCode)),
				CustomResponseBodyKey: response.CustomResponseBodyKey,
				ResponseHeaders:       optionalRuleGroupList(flattenRuleGroupHeaders(response.ResponseHeaders)),
			}
		}
	}
	return out
}

func flattenRuleGroupRequestHandling(
	in *awstypes.CustomRequestHandling,
) *WebACLCustomRequestHandling {
	if in == nil {
		return nil
	}
	return &WebACLCustomRequestHandling{InsertHeaders: flattenRuleGroupHeaders(in.InsertHeaders)}
}

func flattenRuleGroupHeaders(in []awstypes.CustomHTTPHeader) []WebACLCustomHTTPHeader {
	out := make([]WebACLCustomHTTPHeader, 0, len(in))
	for _, header := range in {
		out = append(out, WebACLCustomHTTPHeader{
			Name: aws.ToString(header.Name), Value: aws.ToString(header.Value),
		})
	}
	return sortRuleGroupSet(out, ruleGroupHeaderKey)
}

func optionalRuleGroupList[T any](in []T) *[]T {
	if len(in) == 0 {
		return nil
	}
	return &in
}

func optionalRuleGroupString[T ~string](value T) *string {
	if value == "" {
		return nil
	}
	return aws.String(string(value))
}

func flattenRuleGroupForwarding(in *awstypes.ForwardedIPConfig) *WebACLForwardedIPConfig {
	if in == nil {
		return nil
	}
	return &WebACLForwardedIPConfig{
		FallbackBehavior: string(in.FallbackBehavior), HeaderName: aws.ToString(in.HeaderName),
	}
}

func flattenRuleGroupTransforms(in []awstypes.TextTransformation) []WebACLTextTransformation {
	out := make([]WebACLTextTransformation, 0, len(in))
	for _, value := range in {
		out = append(out, WebACLTextTransformation{
			Priority: int64(value.Priority), Type: string(value.Type),
		})
	}
	return sortedRuleGroupTransforms(out)
}

func flattenRuleGroupLeaf(in *awstypes.Statement) RuleGroupStatementLevel0 {
	out := RuleGroupStatementLevel0{}
	if in == nil {
		return out
	}
	if match := in.AsnMatchStatement; match != nil {
		out.ASNMatchStatement = &WebACLASNMatchStatement{
			ASNList: match.AsnList, ForwardedIPConfig: flattenRuleGroupForwarding(match.ForwardedIPConfig),
		}
	}
	if match := in.GeoMatchStatement; match != nil {
		countries := make([]string, 0, len(match.CountryCodes))
		for _, code := range match.CountryCodes {
			countries = append(countries, string(code))
		}
		out.GeoMatchStatement = &WebACLGeoMatchStatement{
			CountryCodes: countries, ForwardedIPConfig: flattenRuleGroupForwarding(match.ForwardedIPConfig),
		}
	}
	if match := in.IPSetReferenceStatement; match != nil {
		out.IPSetReferenceStatement = &WebACLIPSetReferenceStatement{ARN: aws.ToString(match.ARN)}
		if cfg := match.IPSetForwardedIPConfig; cfg != nil {
			out.IPSetReferenceStatement.IPSetForwardedIPConfig = &WebACLIPSetForwardedIPConfig{
				FallbackBehavior: string(cfg.FallbackBehavior),
				HeaderName:       aws.ToString(cfg.HeaderName),
				Position:         string(cfg.Position),
			}
		}
	}
	if match := in.LabelMatchStatement; match != nil {
		out.LabelMatchStatement = &WebACLLabelMatchStatement{
			Key: aws.ToString(match.Key), Scope: string(match.Scope),
		}
	}
	if match := in.ByteMatchStatement; match != nil {
		out.ByteMatchStatement = &RuleGroupByteMatchStatement{
			FieldToMatch:         flattenRuleGroupField(match.FieldToMatch),
			TextTransformations:  flattenRuleGroupTransforms(match.TextTransformations),
			PositionalConstraint: string(match.PositionalConstraint),
			SearchString:         string(match.SearchString),
		}
	}
	if match := in.RegexMatchStatement; match != nil {
		out.RegexMatchStatement = &RuleGroupRegexMatchStatement{
			FieldToMatch:        flattenRuleGroupField(match.FieldToMatch),
			TextTransformations: flattenRuleGroupTransforms(match.TextTransformations),
			RegexString:         aws.ToString(match.RegexString),
		}
	}
	if match := in.RegexPatternSetReferenceStatement; match != nil {
		out.RegexPatternSet = &RuleGroupRegexPatternSetReferenceStatement{
			FieldToMatch:        flattenRuleGroupField(match.FieldToMatch),
			TextTransformations: flattenRuleGroupTransforms(match.TextTransformations),
			ARN:                 aws.ToString(match.ARN),
		}
	}
	if match := in.SizeConstraintStatement; match != nil {
		out.SizeConstraintStatement = &RuleGroupSizeConstraintStatement{
			FieldToMatch:        flattenRuleGroupField(match.FieldToMatch),
			TextTransformations: flattenRuleGroupTransforms(match.TextTransformations),
			ComparisonOperator:  string(match.ComparisonOperator),
			Size:                match.Size,
		}
	}
	if match := in.SqliMatchStatement; match != nil {
		out.SQLiMatchStatement = &RuleGroupSQLiMatchStatement{
			FieldToMatch:        flattenRuleGroupField(match.FieldToMatch),
			TextTransformations: flattenRuleGroupTransforms(match.TextTransformations),
			SensitivityLevel:    optionalRuleGroupString(match.SensitivityLevel),
		}
	}
	if match := in.XssMatchStatement; match != nil {
		out.XSSMatchStatement = &RuleGroupXSSMatchStatement{
			FieldToMatch:        flattenRuleGroupField(match.FieldToMatch),
			TextTransformations: flattenRuleGroupTransforms(match.TextTransformations),
		}
	}
	return out
}

func flattenRuleGroupChildren[T any](
	in []awstypes.Statement, flatten func(*awstypes.Statement) T,
) []T {
	out := make([]T, 0, len(in))
	for i := range in {
		out = append(out, flatten(&in[i]))
	}
	return out
}

func flattenRuleGroupLevel1(in *awstypes.Statement) RuleGroupStatementLevel1 {
	leaf := flattenRuleGroupLeaf(in)
	out := RuleGroupStatementLevel1{
		ASNMatchStatement:       leaf.ASNMatchStatement,
		ByteMatchStatement:      leaf.ByteMatchStatement,
		GeoMatchStatement:       leaf.GeoMatchStatement,
		IPSetReferenceStatement: leaf.IPSetReferenceStatement,
		LabelMatchStatement:     leaf.LabelMatchStatement,
		RegexMatchStatement:     leaf.RegexMatchStatement,
		RegexPatternSet:         leaf.RegexPatternSet,
		SizeConstraintStatement: leaf.SizeConstraintStatement,
		SQLiMatchStatement:      leaf.SQLiMatchStatement,
		XSSMatchStatement:       leaf.XSSMatchStatement,
	}
	if in == nil {
		return out
	}
	if in.AndStatement != nil {
		out.AndStatement = &RuleGroupAndStatementLevel0{
			Statements: flattenRuleGroupChildren(in.AndStatement.Statements, flattenRuleGroupLeaf),
		}
	}
	if in.OrStatement != nil {
		out.OrStatement = &RuleGroupOrStatementLevel0{
			Statements: flattenRuleGroupChildren(in.OrStatement.Statements, flattenRuleGroupLeaf),
		}
	}
	if in.NotStatement != nil {
		out.NotStatement = &RuleGroupNotStatementLevel0{
			Statement: flattenRuleGroupLeaf(in.NotStatement.Statement),
		}
	}
	return out
}

func flattenRuleGroupLevel2(in *awstypes.Statement) RuleGroupStatementLevel2 {
	leaf := flattenRuleGroupLeaf(in)
	out := RuleGroupStatementLevel2{
		ASNMatchStatement:       leaf.ASNMatchStatement,
		ByteMatchStatement:      leaf.ByteMatchStatement,
		GeoMatchStatement:       leaf.GeoMatchStatement,
		IPSetReferenceStatement: leaf.IPSetReferenceStatement,
		LabelMatchStatement:     leaf.LabelMatchStatement,
		RegexMatchStatement:     leaf.RegexMatchStatement,
		RegexPatternSet:         leaf.RegexPatternSet,
		SizeConstraintStatement: leaf.SizeConstraintStatement,
		SQLiMatchStatement:      leaf.SQLiMatchStatement,
		XSSMatchStatement:       leaf.XSSMatchStatement,
	}
	if in == nil {
		return out
	}
	if in.AndStatement != nil {
		out.AndStatement = &RuleGroupAndStatementLevel1{
			Statements: flattenRuleGroupChildren(in.AndStatement.Statements, flattenRuleGroupLevel1),
		}
	}
	if in.OrStatement != nil {
		out.OrStatement = &RuleGroupOrStatementLevel1{
			Statements: flattenRuleGroupChildren(in.OrStatement.Statements, flattenRuleGroupLevel1),
		}
	}
	if in.NotStatement != nil {
		out.NotStatement = &RuleGroupNotStatementLevel1{
			Statement: flattenRuleGroupLevel1(in.NotStatement.Statement),
		}
	}
	return out
}

func flattenRuleGroupLevel3(in *awstypes.Statement) RuleGroupStatementLevel3 {
	leaf := flattenRuleGroupLeaf(in)
	out := RuleGroupStatementLevel3{
		ASNMatchStatement:       leaf.ASNMatchStatement,
		ByteMatchStatement:      leaf.ByteMatchStatement,
		GeoMatchStatement:       leaf.GeoMatchStatement,
		IPSetReferenceStatement: leaf.IPSetReferenceStatement,
		LabelMatchStatement:     leaf.LabelMatchStatement,
		RegexMatchStatement:     leaf.RegexMatchStatement,
		RegexPatternSet:         leaf.RegexPatternSet,
		SizeConstraintStatement: leaf.SizeConstraintStatement,
		SQLiMatchStatement:      leaf.SQLiMatchStatement,
		XSSMatchStatement:       leaf.XSSMatchStatement,
	}
	if in == nil {
		return out
	}
	if in.AndStatement != nil {
		out.AndStatement = &RuleGroupAndStatementLevel2{
			Statements: flattenRuleGroupChildren(in.AndStatement.Statements, flattenRuleGroupLevel2),
		}
	}
	if in.OrStatement != nil {
		out.OrStatement = &RuleGroupOrStatementLevel2{
			Statements: flattenRuleGroupChildren(in.OrStatement.Statements, flattenRuleGroupLevel2),
		}
	}
	if in.NotStatement != nil {
		out.NotStatement = &RuleGroupNotStatementLevel2{
			Statement: flattenRuleGroupLevel2(in.NotStatement.Statement),
		}
	}
	if in.RateBasedStatement != nil {
		out.RateBasedStatement = flattenRuleGroupRate(in.RateBasedStatement)
	}
	return out
}

func flattenRuleGroupRate(in *awstypes.RateBasedStatement) *RuleGroupRateBasedStatement {
	out := &RuleGroupRateBasedStatement{
		AggregateKeyType:  optionalRuleGroupString(in.AggregateKeyType),
		Limit:             aws.ToInt64(in.Limit),
		ForwardedIPConfig: flattenRuleGroupForwarding(in.ForwardedIPConfig),
	}
	if in.EvaluationWindowSec != 0 {
		out.EvaluationWindowSec = aws.Int64(in.EvaluationWindowSec)
	}
	if in.ScopeDownStatement != nil {
		out.ScopeDownStatement = ptr.To(flattenRuleGroupLevel2(in.ScopeDownStatement))
	}
	var keys []WebACLRateCustomKey
	for _, key := range in.CustomKeys {
		keys = append(keys, flattenRuleGroupRateKey(key))
	}
	out.CustomKeys = optionalRuleGroupList(keys)
	return out
}

func flattenRuleGroupRateKey(in awstypes.RateBasedStatementCustomKey) WebACLRateCustomKey {
	out := WebACLRateCustomKey{
		ASN:         ruleGroupMarker(in.ASN),
		ForwardedIP: ruleGroupMarker(in.ForwardedIP),
		HTTPMethod:  ruleGroupMarker(in.HTTPMethod),
		IP:          ruleGroupMarker(in.IP),
	}
	if key := in.Cookie; key != nil {
		out.Cookie = &WebACLRateNamedKey{
			Name:                aws.ToString(key.Name),
			TextTransformations: flattenRuleGroupTransforms(key.TextTransformations),
		}
	}
	if key := in.Header; key != nil {
		out.Header = &WebACLRateNamedKey{
			Name:                aws.ToString(key.Name),
			TextTransformations: flattenRuleGroupTransforms(key.TextTransformations),
		}
	}
	if key := in.QueryArgument; key != nil {
		out.QueryArgument = &WebACLRateNamedKey{
			Name:                aws.ToString(key.Name),
			TextTransformations: flattenRuleGroupTransforms(key.TextTransformations),
		}
	}
	if key := in.QueryString; key != nil {
		out.QueryString = &WebACLRateTransformedKey{
			TextTransformations: flattenRuleGroupTransforms(key.TextTransformations),
		}
	}
	if key := in.UriPath; key != nil {
		out.URIPath = &WebACLRateTransformedKey{
			TextTransformations: flattenRuleGroupTransforms(key.TextTransformations),
		}
	}
	if key := in.JA3Fingerprint; key != nil {
		out.JA3Fingerprint = &WebACLJAFingerprint{FallbackBehavior: string(key.FallbackBehavior)}
	}
	if key := in.JA4Fingerprint; key != nil {
		out.JA4Fingerprint = &WebACLJAFingerprint{FallbackBehavior: string(key.FallbackBehavior)}
	}
	if key := in.LabelNamespace; key != nil {
		out.LabelNamespace = &WebACLRateLabelNamespace{Namespace: aws.ToString(key.Namespace)}
	}
	return out
}

func ruleGroupMarker[T any](in *T) *WebACLEmpty {
	if in == nil {
		return nil
	}
	return &WebACLEmpty{}
}

func flattenRuleGroupField(in *awstypes.FieldToMatch) RuleGroupFieldToMatch {
	if in == nil {
		return RuleGroupFieldToMatch{}
	}
	out := RuleGroupFieldToMatch{
		AllQueryArguments: ruleGroupMarker(in.AllQueryArguments),
		Method:            ruleGroupMarker(in.Method),
		QueryString:       ruleGroupMarker(in.QueryString),
		URIPath:           ruleGroupMarker(in.UriPath),
	}
	if body := in.Body; body != nil {
		out.Body = &WebACLBody{OversizeHandling: optionalRuleGroupString(body.OversizeHandling)}
	}
	if cookies := in.Cookies; cookies != nil {
		out.Cookies = &WebACLCookies{
			MatchScope:       string(cookies.MatchScope),
			OversizeHandling: string(cookies.OversizeHandling),
		}
		if pattern := cookies.MatchPattern; pattern != nil {
			out.Cookies.MatchPattern = WebACLCookieMatchPattern{
				All:             ruleGroupMarker(pattern.All),
				ExcludedCookies: optionalRuleGroupList(pattern.ExcludedCookies),
				IncludedCookies: optionalRuleGroupList(pattern.IncludedCookies),
			}
		}
	}
	if order := in.HeaderOrder; order != nil {
		out.HeaderOrder = &WebACLHeaderOrder{OversizeHandling: string(order.OversizeHandling)}
	}
	if headers := in.Headers; headers != nil {
		out.Headers = &WebACLHeaders{
			MatchScope:       string(headers.MatchScope),
			OversizeHandling: string(headers.OversizeHandling),
		}
		if pattern := headers.MatchPattern; pattern != nil {
			out.Headers.MatchPattern = WebACLHeaderMatchPattern{
				All:             ruleGroupMarker(pattern.All),
				ExcludedHeaders: optionalRuleGroupList(pattern.ExcludedHeaders),
				IncludedHeaders: optionalRuleGroupList(pattern.IncludedHeaders),
			}
		}
	}
	if fingerprint := in.JA3Fingerprint; fingerprint != nil {
		out.JA3Fingerprint = &WebACLJAFingerprint{
			FallbackBehavior: string(fingerprint.FallbackBehavior),
		}
	}
	if fingerprint := in.JA4Fingerprint; fingerprint != nil {
		out.JA4Fingerprint = &WebACLJAFingerprint{
			FallbackBehavior: string(fingerprint.FallbackBehavior),
		}
	}
	if body := in.JsonBody; body != nil {
		out.JSONBody = &RuleGroupJSONBody{
			MatchScope:              string(body.MatchScope),
			OversizeHandling:        optionalRuleGroupString(body.OversizeHandling),
			InvalidFallbackBehavior: optionalRuleGroupString(body.InvalidFallbackBehavior),
		}
		if pattern := body.MatchPattern; pattern != nil {
			out.JSONBody.MatchPattern = WebACLJSONMatchPattern{
				All:           ruleGroupMarker(pattern.All),
				IncludedPaths: optionalRuleGroupList(pattern.IncludedPaths),
			}
		}
	}
	if header := in.SingleHeader; header != nil {
		out.SingleHeader = &WebACLSingleName{Name: aws.ToString(header.Name)}
	}
	if arg := in.SingleQueryArgument; arg != nil {
		out.SingleQueryArgument = &WebACLSingleName{Name: aws.ToString(arg.Name)}
	}
	if fragment := in.UriFragment; fragment != nil {
		out.URIFragment = &WebACLURIFragment{
			FallbackBehavior: optionalRuleGroupString(fragment.FallbackBehavior),
		}
	}
	return out
}
