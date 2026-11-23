package wafv2

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

func TestRuleGroupActionsAndCaptcha(t *testing.T) {
	for _, tc := range []struct {
		goAction  WebACLRuleAction
		awsAction awstypes.RuleAction
	}{
		{WebACLRuleAction{Allow: &WebACLAllowAction{}},
			awstypes.RuleAction{Allow: &awstypes.AllowAction{}}},
		{WebACLRuleAction{Block: &WebACLBlockAction{}},
			awstypes.RuleAction{Block: &awstypes.BlockAction{}}},
		{WebACLRuleAction{Captcha: &WebACLCaptchaAction{}},
			awstypes.RuleAction{Captcha: &awstypes.CaptchaAction{}}},
		{WebACLRuleAction{Challenge: &WebACLChallengeAction{}},
			awstypes.RuleAction{Challenge: &awstypes.ChallengeAction{}}},
		{WebACLRuleAction{Count: &WebACLCountAction{}},
			awstypes.RuleAction{Count: &awstypes.CountAction{}}},
	} {
		require.Equal(t, &tc.awsAction, expandRuleGroupAction(tc.goAction))
		require.Equal(t, tc.goAction, flattenRuleGroupAction(&tc.awsAction))
	}
	require.Equal(t, &awstypes.CustomResponse{ResponseCode: aws.Int32(200)},
		expandRuleGroupResponse(&WebACLCustomResponse{
			ResponseCode: 200, ResponseHeaders: ptr.To([]WebACLCustomHTTPHeader{}),
		}))
	for _, in := range []*RuleGroupCaptchaConfig{nil, {}} {
		require.Equal(t, &awstypes.CaptchaConfig{}, expandRuleGroupCaptcha(in))
	}
	for _, awsConfig := range []*awstypes.CaptchaConfig{nil, {}} {
		require.Nil(t, flattenRuleGroupCaptcha(awsConfig))
	}
	for _, time := range []*int64{nil, aws.Int64(0), aws.Int64(9)} {
		input := &RuleGroupCaptchaConfig{
			ImmunityTimeProperty: &RuleGroupImmunityTimeProperty{ImmunityTime: time},
		}
		want := &awstypes.CaptchaConfig{
			ImmunityTimeProperty: &awstypes.ImmunityTimeProperty{
				ImmunityTime: aws.Int64(aws.ToInt64(time)),
			},
		}
		require.Equal(t, want, expandRuleGroupCaptcha(input))
		require.Equal(t, want.ImmunityTimeProperty.ImmunityTime,
			flattenRuleGroupCaptcha(want).ImmunityTimeProperty.ImmunityTime)
	}
}

func TestRuleGroupFieldRequests(t *testing.T) {
	empty := &WebACLEmpty{}
	for _, tc := range []struct {
		field RuleGroupFieldToMatch
		want  awstypes.FieldToMatch
	}{
		{RuleGroupFieldToMatch{AllQueryArguments: empty},
			awstypes.FieldToMatch{AllQueryArguments: &awstypes.AllQueryArguments{}}},
		{RuleGroupFieldToMatch{Method: empty},
			awstypes.FieldToMatch{Method: &awstypes.Method{}}},
		{RuleGroupFieldToMatch{QueryString: empty},
			awstypes.FieldToMatch{QueryString: &awstypes.QueryString{}}},
		{RuleGroupFieldToMatch{URIPath: empty},
			awstypes.FieldToMatch{UriPath: &awstypes.UriPath{}}},
		{RuleGroupFieldToMatch{Body: &WebACLBody{}},
			awstypes.FieldToMatch{Body: &awstypes.Body{OversizeHandling: "CONTINUE"}}},
		{RuleGroupFieldToMatch{Body: &WebACLBody{OversizeHandling: aws.String("MATCH")}},
			awstypes.FieldToMatch{Body: &awstypes.Body{OversizeHandling: "MATCH"}}},
		{RuleGroupFieldToMatch{HeaderOrder: &WebACLHeaderOrder{OversizeHandling: "NO_MATCH"}},
			awstypes.FieldToMatch{HeaderOrder: &awstypes.HeaderOrder{OversizeHandling: "NO_MATCH"}}},
		{RuleGroupFieldToMatch{JA3Fingerprint: &WebACLJAFingerprint{FallbackBehavior: "MATCH"}},
			awstypes.FieldToMatch{JA3Fingerprint: &awstypes.JA3Fingerprint{FallbackBehavior: "MATCH"}}},
		{RuleGroupFieldToMatch{JA4Fingerprint: &WebACLJAFingerprint{FallbackBehavior: "NO_MATCH"}},
			awstypes.FieldToMatch{JA4Fingerprint: &awstypes.JA4Fingerprint{FallbackBehavior: "NO_MATCH"}}},
		{RuleGroupFieldToMatch{URIFragment: &WebACLURIFragment{}},
			awstypes.FieldToMatch{UriFragment: &awstypes.UriFragment{}}},
		{RuleGroupFieldToMatch{URIFragment: &WebACLURIFragment{FallbackBehavior: aws.String("MATCH")}},
			awstypes.FieldToMatch{UriFragment: &awstypes.UriFragment{FallbackBehavior: "MATCH"}}},
		{RuleGroupFieldToMatch{SingleHeader: &WebACLSingleName{Name: "a_b-0"}},
			awstypes.FieldToMatch{SingleHeader: &awstypes.SingleHeader{Name: aws.String("a_b-0")}}},
		{RuleGroupFieldToMatch{SingleQueryArgument: &WebACLSingleName{Name: "a_b-0"}},
			awstypes.FieldToMatch{SingleQueryArgument: &awstypes.SingleQueryArgument{
				Name: aws.String("a_b-0"),
			}}},
		{RuleGroupFieldToMatch{JSONBody: &RuleGroupJSONBody{
			MatchScope: "ALL", MatchPattern: WebACLJSONMatchPattern{All: empty},
		}}, awstypes.FieldToMatch{JsonBody: &awstypes.JsonBody{
			MatchScope: "ALL", OversizeHandling: "CONTINUE",
			MatchPattern: &awstypes.JsonMatchPattern{All: &awstypes.All{}},
		}}},
		{RuleGroupFieldToMatch{JSONBody: &RuleGroupJSONBody{
			MatchScope: "KEY", OversizeHandling: aws.String("MATCH"),
			InvalidFallbackBehavior: aws.String("EVALUATE_AS_STRING"),
			MatchPattern: WebACLJSONMatchPattern{
				IncludedPaths: ptr.To([]string{"/b", "/a", "/b"}),
			},
		}}, awstypes.FieldToMatch{JsonBody: &awstypes.JsonBody{
			MatchScope: "KEY", OversizeHandling: "MATCH",
			InvalidFallbackBehavior: "EVALUATE_AS_STRING",
			MatchPattern:            &awstypes.JsonMatchPattern{IncludedPaths: []string{"/b", "/a", "/b"}},
		}}},
	} {
		require.Equal(t, &tc.want, expandRuleGroupField(tc.field))
		require.Equal(t, &tc.want, expandRuleGroupField(flattenRuleGroupField(&tc.want)))
	}
}

func TestRuleGroupCookieAndHeaderPatterns(t *testing.T) {
	empty := &WebACLEmpty{}
	for _, tc := range []struct {
		input WebACLCookieMatchPattern
		want  awstypes.CookieMatchPattern
	}{
		{WebACLCookieMatchPattern{All: empty}, awstypes.CookieMatchPattern{All: &awstypes.All{}}},
		{WebACLCookieMatchPattern{IncludedCookies: ptr.To([]string{"b", "", "a", "b"})},
			awstypes.CookieMatchPattern{IncludedCookies: []string{"b", "a", "b"}}},
		{WebACLCookieMatchPattern{ExcludedCookies: ptr.To([]string{"b", "a", "b"})},
			awstypes.CookieMatchPattern{ExcludedCookies: []string{"b", "a", "b"}}},
		{WebACLCookieMatchPattern{IncludedCookies: ptr.To([]string{""})},
			awstypes.CookieMatchPattern{IncludedCookies: []string{}}},
		{WebACLCookieMatchPattern{IncludedCookies: ptr.To([]string{}), All: empty},
			awstypes.CookieMatchPattern{All: &awstypes.All{}}},
	} {
		out := expandRuleGroupField(RuleGroupFieldToMatch{Cookies: &WebACLCookies{
			MatchPattern: tc.input, MatchScope: "KEY", OversizeHandling: "NO_MATCH",
		}})
		require.Equal(t, &awstypes.FieldToMatch{Cookies: &awstypes.Cookies{
			MatchPattern: &tc.want, MatchScope: "KEY", OversizeHandling: "NO_MATCH",
		}}, out)
	}

	for _, tc := range []struct {
		input WebACLHeaderMatchPattern
		want  awstypes.HeaderMatchPattern
	}{
		{WebACLHeaderMatchPattern{All: empty}, awstypes.HeaderMatchPattern{All: &awstypes.All{}}},
		{WebACLHeaderMatchPattern{IncludedHeaders: ptr.To([]string{"b", "a", "b"})},
			awstypes.HeaderMatchPattern{IncludedHeaders: []string{"b", "a", "b"}}},
		{WebACLHeaderMatchPattern{ExcludedHeaders: ptr.To([]string{"b", "a", "b"})},
			awstypes.HeaderMatchPattern{ExcludedHeaders: []string{"b", "a", "b"}}},
	} {
		field := RuleGroupFieldToMatch{Headers: &WebACLHeaders{
			MatchPattern: tc.input, MatchScope: "VALUE", OversizeHandling: "MATCH",
		}}
		out := expandRuleGroupField(field)
		require.Equal(t, &awstypes.FieldToMatch{Headers: &awstypes.Headers{
			MatchPattern: &tc.want, MatchScope: "VALUE", OversizeHandling: "MATCH",
		}}, out)
		readPattern := tc.want
		if readPattern.IncludedHeaders == nil {
			readPattern.IncludedHeaders = []string{}
		}
		if readPattern.ExcludedHeaders == nil {
			readPattern.ExcludedHeaders = []string{}
		}
		out.Headers.MatchPattern = &readPattern
		require.Equal(t, field, flattenRuleGroupField(out))
	}
}

func TestRuleGroupLeavesAndLogicalOrder(t *testing.T) {
	forward := &WebACLForwardedIPConfig{HeaderName: "custom header!", FallbackBehavior: "MATCH"}
	awsForward := &awstypes.ForwardedIPConfig{
		HeaderName: aws.String("custom header!"), FallbackBehavior: "MATCH",
	}
	asn := RuleGroupStatementLevel0{ASNMatchStatement: &WebACLASNMatchStatement{
		ASNList: []int64{42, -1, 42}, ForwardedIPConfig: forward,
	}}
	geo := RuleGroupStatementLevel0{GeoMatchStatement: &WebACLGeoMatchStatement{
		CountryCodes: []string{"ZZ", "US", "ZZ"}, ForwardedIPConfig: forward,
	}}
	awsASN := awstypes.Statement{AsnMatchStatement: &awstypes.AsnMatchStatement{
		AsnList: []int64{42, -1, 42}, ForwardedIPConfig: awsForward,
	}}
	awsGeo := awstypes.Statement{GeoMatchStatement: &awstypes.GeoMatchStatement{
		CountryCodes: []awstypes.CountryCode{"ZZ", "US", "ZZ"}, ForwardedIPConfig: awsForward,
	}}
	ordered := RuleGroupStatementLevel3{NotStatement: &RuleGroupNotStatementLevel2{
		Statement: RuleGroupStatementLevel2{OrStatement: &RuleGroupOrStatementLevel1{
			Statements: []RuleGroupStatementLevel1{{AndStatement: &RuleGroupAndStatementLevel0{
				Statements: []RuleGroupStatementLevel0{asn, geo, asn},
			}}},
		}},
	}}
	awsAnd := awstypes.Statement{AndStatement: &awstypes.AndStatement{
		Statements: []awstypes.Statement{awsASN, awsGeo, awsASN},
	}}
	want := &awstypes.Statement{NotStatement: &awstypes.NotStatement{
		Statement: &awstypes.Statement{OrStatement: &awstypes.OrStatement{
			Statements: []awstypes.Statement{awsAnd},
		}},
	}}
	require.Equal(t, want, expandRuleGroupLevel3(ordered))
	require.Equal(t, ordered, flattenRuleGroupLevel3(want))
	ordered.NotStatement.Statement.OrStatement.Statements[0].AndStatement.Statements =
		[]RuleGroupStatementLevel0{geo, asn, asn}
	want.NotStatement.Statement.OrStatement.Statements[0].AndStatement.Statements =
		[]awstypes.Statement{awsGeo, awsASN, awsASN}
	require.Equal(t, want, expandRuleGroupLevel3(ordered))
	require.Equal(t, ordered, flattenRuleGroupLevel3(want))
	require.Equal(t, &awstypes.Statement{AndStatement: &awstypes.AndStatement{}},
		expandRuleGroupLevel3(RuleGroupStatementLevel3{AndStatement: &RuleGroupAndStatementLevel2{
			Statements: []RuleGroupStatementLevel2{},
		}}))
}

func TestRuleGroupMatchLeafSDKValues(t *testing.T) {
	field := RuleGroupFieldToMatch{Method: &WebACLEmpty{}}
	transforms := []WebACLTextTransformation{{Priority: 0, Type: "NONE"}}
	awsTransforms := []awstypes.TextTransformation{{Priority: 0, Type: "NONE"}}
	awsField := &awstypes.FieldToMatch{Method: &awstypes.Method{}}
	arn := "arn:aws:arbitrary:::a:b"
	for _, tc := range []struct {
		input RuleGroupStatementLevel0
		want  awstypes.Statement
	}{
		{RuleGroupStatementLevel0{ByteMatchStatement: &RuleGroupByteMatchStatement{
			FieldToMatch: field, PositionalConstraint: "CONTAINS", SearchString: "YWJj",
			TextTransformations: transforms,
		}}, awstypes.Statement{ByteMatchStatement: &awstypes.ByteMatchStatement{
			FieldToMatch: awsField, PositionalConstraint: "CONTAINS", SearchString: []byte("YWJj"),
			TextTransformations: awsTransforms,
		}}},
		{RuleGroupStatementLevel0{IPSetReferenceStatement: &WebACLIPSetReferenceStatement{
			ARN: arn, IPSetForwardedIPConfig: &WebACLIPSetForwardedIPConfig{
				HeaderName: "X-IP", FallbackBehavior: "NO_MATCH", Position: "ANY",
			},
		}}, awstypes.Statement{IPSetReferenceStatement: &awstypes.IPSetReferenceStatement{
			ARN: aws.String(arn), IPSetForwardedIPConfig: &awstypes.IPSetForwardedIPConfig{
				HeaderName: aws.String("X-IP"), FallbackBehavior: "NO_MATCH", Position: "ANY",
			},
		}}},
		{RuleGroupStatementLevel0{RegexMatchStatement: &RuleGroupRegexMatchStatement{
			FieldToMatch: field, RegexString: "a+", TextTransformations: transforms,
		}}, awstypes.Statement{RegexMatchStatement: &awstypes.RegexMatchStatement{
			FieldToMatch: awsField, RegexString: aws.String("a+"), TextTransformations: awsTransforms,
		}}},
		{RuleGroupStatementLevel0{RegexPatternSet: &RuleGroupRegexPatternSetReferenceStatement{
			FieldToMatch: field, ARN: arn, TextTransformations: transforms,
		}}, awstypes.Statement{
			RegexPatternSetReferenceStatement: &awstypes.RegexPatternSetReferenceStatement{
				FieldToMatch: awsField, ARN: aws.String(arn), TextTransformations: awsTransforms,
			},
		}},
		{RuleGroupStatementLevel0{SizeConstraintStatement: &RuleGroupSizeConstraintStatement{
			FieldToMatch: field, Size: 0, ComparisonOperator: "GE", TextTransformations: transforms,
		}}, awstypes.Statement{SizeConstraintStatement: &awstypes.SizeConstraintStatement{
			FieldToMatch: awsField, Size: 0, ComparisonOperator: "GE", TextTransformations: awsTransforms,
		}}},
		{RuleGroupStatementLevel0{SQLiMatchStatement: &RuleGroupSQLiMatchStatement{
			FieldToMatch: field, TextTransformations: transforms,
		}}, awstypes.Statement{SqliMatchStatement: &awstypes.SqliMatchStatement{
			FieldToMatch: awsField, TextTransformations: awsTransforms,
		}}},
		{RuleGroupStatementLevel0{SQLiMatchStatement: &RuleGroupSQLiMatchStatement{
			FieldToMatch: field, TextTransformations: transforms, SensitivityLevel: aws.String("LOW"),
		}}, awstypes.Statement{SqliMatchStatement: &awstypes.SqliMatchStatement{
			FieldToMatch: awsField, TextTransformations: awsTransforms, SensitivityLevel: "LOW",
		}}},
		{RuleGroupStatementLevel0{XSSMatchStatement: &RuleGroupXSSMatchStatement{
			FieldToMatch: field, TextTransformations: transforms,
		}}, awstypes.Statement{XssMatchStatement: &awstypes.XssMatchStatement{
			FieldToMatch: awsField, TextTransformations: awsTransforms,
		}}},
	} {
		require.Equal(t, &tc.want, expandRuleGroupLeaf(tc.input))
		require.Equal(t, tc.input, flattenRuleGroupLeaf(&tc.want))
	}
}

func TestRuleGroupRateDefaultsAndOrderedKeys(t *testing.T) {
	require.Equal(t, &awstypes.RateBasedStatement{
		Limit: aws.Int64(10), AggregateKeyType: "IP", EvaluationWindowSec: 300,
	}, expandRuleGroupRate(&RuleGroupRateBasedStatement{Limit: 10}))
	empty := &WebACLEmpty{}
	transforms := []WebACLTextTransformation{{Priority: 0, Type: "NONE"}}
	awsTransforms := []awstypes.TextTransformation{{Priority: 0, Type: "NONE"}}
	named := &WebACLRateNamedKey{Name: "a_!", TextTransformations: transforms}
	transformed := &WebACLRateTransformedKey{TextTransformations: transforms}
	fallback := &WebACLJAFingerprint{FallbackBehavior: "NO_MATCH"}
	keys := []WebACLRateCustomKey{
		{ASN: empty}, {Cookie: named}, {ForwardedIP: empty}, {HTTPMethod: empty},
		{Header: named}, {IP: empty}, {JA3Fingerprint: fallback}, {JA4Fingerprint: fallback},
		{LabelNamespace: &WebACLRateLabelNamespace{Namespace: "a::"}},
		{QueryArgument: named}, {QueryString: transformed}, {URIPath: transformed},
	}
	awsKeys := []awstypes.RateBasedStatementCustomKey{
		{ASN: &awstypes.RateLimitAsn{}},
		{Cookie: &awstypes.RateLimitCookie{Name: aws.String("a_!"), TextTransformations: awsTransforms}},
		{ForwardedIP: &awstypes.RateLimitForwardedIP{}}, {HTTPMethod: &awstypes.RateLimitHTTPMethod{}},
		{Header: &awstypes.RateLimitHeader{Name: aws.String("a_!"), TextTransformations: awsTransforms}},
		{IP: &awstypes.RateLimitIP{}},
		{JA3Fingerprint: &awstypes.RateLimitJA3Fingerprint{FallbackBehavior: "NO_MATCH"}},
		{JA4Fingerprint: &awstypes.RateLimitJA4Fingerprint{FallbackBehavior: "NO_MATCH"}},
		{LabelNamespace: &awstypes.RateLimitLabelNamespace{Namespace: aws.String("a::")}},
		{QueryArgument: &awstypes.RateLimitQueryArgument{
			Name: aws.String("a_!"), TextTransformations: awsTransforms,
		}},
		{QueryString: &awstypes.RateLimitQueryString{TextTransformations: awsTransforms}},
		{UriPath: &awstypes.RateLimitUriPath{TextTransformations: awsTransforms}},
	}
	for start := 0; start < len(keys); start += 4 {
		input := RuleGroupRateBasedStatement{
			Limit: 2000000000, AggregateKeyType: aws.String("CUSTOM_KEYS"),
			EvaluationWindowSec: aws.Int64(600), CustomKeys: ptr.To(keys[start : start+4]),
			ForwardedIPConfig: &WebACLForwardedIPConfig{HeaderName: "x", FallbackBehavior: "MATCH"},
		}
		want := &awstypes.RateBasedStatement{
			Limit: aws.Int64(2000000000), AggregateKeyType: "CUSTOM_KEYS",
			EvaluationWindowSec: 600, CustomKeys: awsKeys[start : start+4],
			ForwardedIPConfig: &awstypes.ForwardedIPConfig{
				HeaderName: aws.String("x"), FallbackBehavior: "MATCH",
			},
		}
		require.Equal(t, want, expandRuleGroupRate(&input))
		require.Equal(t, &input, flattenRuleGroupRate(want))
	}

	leaf := &WebACLLabelMatchStatement{Key: ":label::", Scope: "NAMESPACE"}
	down := &RuleGroupStatementLevel2{NotStatement: &RuleGroupNotStatementLevel1{
		Statement: RuleGroupStatementLevel1{AndStatement: &RuleGroupAndStatementLevel0{
			Statements: []RuleGroupStatementLevel0{{LabelMatchStatement: leaf}},
		}},
	}}
	wantDown := &awstypes.Statement{NotStatement: &awstypes.NotStatement{
		Statement: &awstypes.Statement{AndStatement: &awstypes.AndStatement{
			Statements: []awstypes.Statement{{LabelMatchStatement: &awstypes.LabelMatchStatement{
				Key: aws.String(":label::"), Scope: "NAMESPACE",
			}}},
		}},
	}}
	input := &RuleGroupRateBasedStatement{
		Limit: 10, AggregateKeyType: aws.String("CONSTANT"), ScopeDownStatement: down,
		EvaluationWindowSec: aws.Int64(60),
	}
	want := &awstypes.RateBasedStatement{
		Limit: aws.Int64(10), AggregateKeyType: "CONSTANT", ScopeDownStatement: wantDown,
		EvaluationWindowSec: 60,
	}
	require.Equal(t, want, expandRuleGroupRate(input))
	require.Equal(t, input, flattenRuleGroupRate(want))
}
