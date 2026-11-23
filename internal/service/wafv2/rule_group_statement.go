package wafv2

import (
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

func expandRuleGroupLeaf(in RuleGroupStatementLevel0) *awstypes.Statement {
	out := &awstypes.Statement{}
	switch {
	case in.ASNMatchStatement != nil:
		out.AsnMatchStatement = expandASNMatchStatement(in.ASNMatchStatement)
	case in.GeoMatchStatement != nil:
		out.GeoMatchStatement = expandGeoMatchStatement(in.GeoMatchStatement)
	case in.IPSetReferenceStatement != nil:
		out.IPSetReferenceStatement = expandIPSetReferenceStatement(in.IPSetReferenceStatement)
	case in.LabelMatchStatement != nil:
		out.LabelMatchStatement = expandLabelMatchStatement(in.LabelMatchStatement)
	case in.ByteMatchStatement != nil:
		match := in.ByteMatchStatement
		out.ByteMatchStatement = &awstypes.ByteMatchStatement{
			FieldToMatch:         expandRuleGroupField(match.FieldToMatch),
			PositionalConstraint: awstypes.PositionalConstraint(match.PositionalConstraint),
			SearchString:         []byte(match.SearchString),
			TextTransformations:  ruleGroupTransforms(match.TextTransformations),
		}
	case in.RegexMatchStatement != nil:
		match := in.RegexMatchStatement
		out.RegexMatchStatement = &awstypes.RegexMatchStatement{
			FieldToMatch:        expandRuleGroupField(match.FieldToMatch),
			RegexString:         aws.String(match.RegexString),
			TextTransformations: ruleGroupTransforms(match.TextTransformations),
		}
	case in.RegexPatternSet != nil:
		match := in.RegexPatternSet
		out.RegexPatternSetReferenceStatement = &awstypes.RegexPatternSetReferenceStatement{
			ARN:                 aws.String(match.ARN),
			FieldToMatch:        expandRuleGroupField(match.FieldToMatch),
			TextTransformations: ruleGroupTransforms(match.TextTransformations),
		}
	case in.SizeConstraintStatement != nil:
		match := in.SizeConstraintStatement
		out.SizeConstraintStatement = &awstypes.SizeConstraintStatement{
			ComparisonOperator:  awstypes.ComparisonOperator(match.ComparisonOperator),
			FieldToMatch:        expandRuleGroupField(match.FieldToMatch),
			Size:                match.Size,
			TextTransformations: ruleGroupTransforms(match.TextTransformations),
		}
	case in.SQLiMatchStatement != nil:
		match := in.SQLiMatchStatement
		out.SqliMatchStatement = &awstypes.SqliMatchStatement{
			FieldToMatch:        expandRuleGroupField(match.FieldToMatch),
			SensitivityLevel:    awstypes.SensitivityLevel(aws.ToString(match.SensitivityLevel)),
			TextTransformations: ruleGroupTransforms(match.TextTransformations),
		}
	case in.XSSMatchStatement != nil:
		match := in.XSSMatchStatement
		out.XssMatchStatement = &awstypes.XssMatchStatement{
			FieldToMatch:        expandRuleGroupField(match.FieldToMatch),
			TextTransformations: ruleGroupTransforms(match.TextTransformations),
		}
	}
	return out
}

func expandRuleGroupChildren[T any](
	children []T, expand func(T) *awstypes.Statement,
) []awstypes.Statement {
	var out []awstypes.Statement
	for _, child := range children {
		out = append(out, *expand(child))
	}
	return out
}

func expandRuleGroupLevel1(in RuleGroupStatementLevel1) *awstypes.Statement {
	switch {
	case in.AndStatement != nil:
		return &awstypes.Statement{AndStatement: &awstypes.AndStatement{
			Statements: expandRuleGroupChildren(in.AndStatement.Statements, expandRuleGroupLeaf),
		}}
	case in.OrStatement != nil:
		return &awstypes.Statement{OrStatement: &awstypes.OrStatement{
			Statements: expandRuleGroupChildren(in.OrStatement.Statements, expandRuleGroupLeaf),
		}}
	case in.NotStatement != nil:
		return &awstypes.Statement{NotStatement: &awstypes.NotStatement{
			Statement: expandRuleGroupLeaf(in.NotStatement.Statement),
		}}
	default:
		return expandRuleGroupLeaf(RuleGroupStatementLevel0{
			ASNMatchStatement:       in.ASNMatchStatement,
			ByteMatchStatement:      in.ByteMatchStatement,
			GeoMatchStatement:       in.GeoMatchStatement,
			IPSetReferenceStatement: in.IPSetReferenceStatement,
			LabelMatchStatement:     in.LabelMatchStatement,
			RegexMatchStatement:     in.RegexMatchStatement,
			RegexPatternSet:         in.RegexPatternSet,
			SizeConstraintStatement: in.SizeConstraintStatement,
			SQLiMatchStatement:      in.SQLiMatchStatement,
			XSSMatchStatement:       in.XSSMatchStatement,
		})
	}
}

func expandRuleGroupLevel2(in RuleGroupStatementLevel2) *awstypes.Statement {
	switch {
	case in.AndStatement != nil:
		return &awstypes.Statement{AndStatement: &awstypes.AndStatement{
			Statements: expandRuleGroupChildren(in.AndStatement.Statements, expandRuleGroupLevel1),
		}}
	case in.OrStatement != nil:
		return &awstypes.Statement{OrStatement: &awstypes.OrStatement{
			Statements: expandRuleGroupChildren(in.OrStatement.Statements, expandRuleGroupLevel1),
		}}
	case in.NotStatement != nil:
		return &awstypes.Statement{NotStatement: &awstypes.NotStatement{
			Statement: expandRuleGroupLevel1(in.NotStatement.Statement),
		}}
	default:
		return expandRuleGroupLeaf(RuleGroupStatementLevel0{
			ASNMatchStatement:       in.ASNMatchStatement,
			ByteMatchStatement:      in.ByteMatchStatement,
			GeoMatchStatement:       in.GeoMatchStatement,
			IPSetReferenceStatement: in.IPSetReferenceStatement,
			LabelMatchStatement:     in.LabelMatchStatement,
			RegexMatchStatement:     in.RegexMatchStatement,
			RegexPatternSet:         in.RegexPatternSet,
			SizeConstraintStatement: in.SizeConstraintStatement,
			SQLiMatchStatement:      in.SQLiMatchStatement,
			XSSMatchStatement:       in.XSSMatchStatement,
		})
	}
}

func expandRuleGroupLevel3(in RuleGroupStatementLevel3) *awstypes.Statement {
	switch {
	case in.AndStatement != nil:
		return &awstypes.Statement{AndStatement: &awstypes.AndStatement{
			Statements: expandRuleGroupChildren(in.AndStatement.Statements, expandRuleGroupLevel2),
		}}
	case in.OrStatement != nil:
		return &awstypes.Statement{OrStatement: &awstypes.OrStatement{
			Statements: expandRuleGroupChildren(in.OrStatement.Statements, expandRuleGroupLevel2),
		}}
	case in.NotStatement != nil:
		return &awstypes.Statement{NotStatement: &awstypes.NotStatement{
			Statement: expandRuleGroupLevel2(in.NotStatement.Statement),
		}}
	case in.RateBasedStatement != nil:
		return &awstypes.Statement{
			RateBasedStatement: expandRuleGroupRate(in.RateBasedStatement),
		}
	default:
		return expandRuleGroupLeaf(RuleGroupStatementLevel0{
			ASNMatchStatement:       in.ASNMatchStatement,
			ByteMatchStatement:      in.ByteMatchStatement,
			GeoMatchStatement:       in.GeoMatchStatement,
			IPSetReferenceStatement: in.IPSetReferenceStatement,
			LabelMatchStatement:     in.LabelMatchStatement,
			RegexMatchStatement:     in.RegexMatchStatement,
			RegexPatternSet:         in.RegexPatternSet,
			SizeConstraintStatement: in.SizeConstraintStatement,
			SQLiMatchStatement:      in.SQLiMatchStatement,
			XSSMatchStatement:       in.XSSMatchStatement,
		})
	}
}

const ruleGroupDefaultRateWindow int64 = 300

func expandRuleGroupRate(in *RuleGroupRateBasedStatement) *awstypes.RateBasedStatement {
	out := &awstypes.RateBasedStatement{
		AggregateKeyType:    awstypes.RateBasedStatementAggregateKeyTypeIp,
		EvaluationWindowSec: ruleGroupDefaultRateWindow,
		Limit:               aws.Int64(in.Limit),
	}
	if in.AggregateKeyType != nil {
		out.AggregateKeyType = awstypes.RateBasedStatementAggregateKeyType(*in.AggregateKeyType)
	}
	if in.EvaluationWindowSec != nil {
		out.EvaluationWindowSec = *in.EvaluationWindowSec
	}
	if in.ForwardedIPConfig != nil {
		out.ForwardedIPConfig = expandForwardedIPConfig(in.ForwardedIPConfig)
	}
	if in.ScopeDownStatement != nil {
		out.ScopeDownStatement = expandRuleGroupLevel2(*in.ScopeDownStatement)
	}
	for _, item := range ptr.Value(in.CustomKeys) {
		key := expandRateCustomKey(item)
		switch {
		case key.Cookie != nil:
			key.Cookie.TextTransformations = ruleGroupTransforms(item.Cookie.TextTransformations)
		case key.Header != nil:
			key.Header.TextTransformations = ruleGroupTransforms(item.Header.TextTransformations)
		case key.QueryArgument != nil:
			key.QueryArgument.TextTransformations = ruleGroupTransforms(
				item.QueryArgument.TextTransformations,
			)
		case key.QueryString != nil:
			key.QueryString.TextTransformations = ruleGroupTransforms(
				item.QueryString.TextTransformations,
			)
		case key.UriPath != nil:
			key.UriPath.TextTransformations = ruleGroupTransforms(item.URIPath.TextTransformations)
		}
		out.CustomKeys = append(out.CustomKeys, key)
	}
	return out
}

func expandRuleGroupField(in RuleGroupFieldToMatch) *awstypes.FieldToMatch {
	out := &awstypes.FieldToMatch{}
	switch {
	case in.AllQueryArguments != nil:
		out.AllQueryArguments = &awstypes.AllQueryArguments{}
	case in.Body != nil:
		out.Body = &awstypes.Body{
			OversizeHandling: ruleGroupOversize(in.Body.OversizeHandling),
		}
	case in.Cookies != nil:
		cookies := in.Cookies
		out.Cookies = &awstypes.Cookies{
			MatchScope:       awstypes.MapMatchScope(cookies.MatchScope),
			OversizeHandling: awstypes.OversizeHandling(cookies.OversizeHandling),
			MatchPattern: &awstypes.CookieMatchPattern{
				ExcludedCookies: ruleGroupCookies(ptr.Value(cookies.MatchPattern.ExcludedCookies)),
				IncludedCookies: ruleGroupCookies(ptr.Value(cookies.MatchPattern.IncludedCookies)),
			},
		}
		if cookies.MatchPattern.All != nil {
			out.Cookies.MatchPattern.All = &awstypes.All{}
		}
	case in.HeaderOrder != nil:
		out.HeaderOrder = &awstypes.HeaderOrder{
			OversizeHandling: awstypes.OversizeHandling(in.HeaderOrder.OversizeHandling),
		}
	case in.Headers != nil:
		out.Headers = expandHeaders(in.Headers)
	case in.JA3Fingerprint != nil:
		out.JA3Fingerprint = &awstypes.JA3Fingerprint{
			FallbackBehavior: awstypes.FallbackBehavior(in.JA3Fingerprint.FallbackBehavior),
		}
	case in.JA4Fingerprint != nil:
		out.JA4Fingerprint = &awstypes.JA4Fingerprint{
			FallbackBehavior: awstypes.FallbackBehavior(in.JA4Fingerprint.FallbackBehavior),
		}
	case in.JSONBody != nil:
		body := in.JSONBody
		out.JsonBody = &awstypes.JsonBody{
			InvalidFallbackBehavior: awstypes.BodyParsingFallbackBehavior(
				aws.ToString(body.InvalidFallbackBehavior),
			),
			MatchPattern:     expandJSONMatchPattern(body.MatchPattern),
			MatchScope:       awstypes.JsonMatchScope(body.MatchScope),
			OversizeHandling: ruleGroupOversize(body.OversizeHandling),
		}
	case in.Method != nil:
		out.Method = &awstypes.Method{}
	case in.QueryString != nil:
		out.QueryString = &awstypes.QueryString{}
	case in.SingleHeader != nil:
		out.SingleHeader = &awstypes.SingleHeader{Name: aws.String(in.SingleHeader.Name)}
	case in.SingleQueryArgument != nil:
		out.SingleQueryArgument = &awstypes.SingleQueryArgument{
			Name: aws.String(in.SingleQueryArgument.Name),
		}
	case in.URIFragment != nil:
		out.UriFragment = &awstypes.UriFragment{
			FallbackBehavior: awstypes.FallbackBehavior(aws.ToString(in.URIFragment.FallbackBehavior)),
		}
	case in.URIPath != nil:
		out.UriPath = &awstypes.UriPath{}
	}
	return out
}

func ruleGroupCookies(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return slices.DeleteFunc(append([]string{}, in...), func(s string) bool { return s == "" })
}

func ruleGroupOversize(value *string) awstypes.OversizeHandling {
	if value == nil {
		return awstypes.OversizeHandlingContinue
	}
	return awstypes.OversizeHandling(*value)
}
