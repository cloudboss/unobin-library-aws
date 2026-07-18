package wafv2

import (
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

var rateLabelNamespacePattern = regexp.MustCompile(`^[0-9A-Za-z_:-]+$`)

func validateRateBasedStatement(in *WebACLRateBasedStatement) error {
	if in == nil {
		return fmt.Errorf("rate-based-statement is required")
	}
	if !validRateAggregateKeyType(in.AggregateKeyType) {
		return fmt.Errorf(
			"aggregate-key-type must be IP, FORWARDED_IP, CUSTOM_KEYS, or CONSTANT",
		)
	}
	if in.Limit < 10 || in.Limit > 2_000_000_000 {
		return fmt.Errorf("limit must be 10..2000000000")
	}
	if in.EvaluationWindowSec != nil &&
		!validRateEvaluationWindow(*in.EvaluationWindowSec) {
		return fmt.Errorf("evaluation-window-sec must be 60, 120, 300, or 600")
	}
	customKeyCount := 0
	if in.CustomKeys != nil {
		customKeyCount = len(*in.CustomKeys)
	}
	if in.AggregateKeyType == string(
		awstypes.RateBasedStatementAggregateKeyTypeCustomKeys,
	) {
		if customKeyCount < 1 || customKeyCount > 5 {
			return fmt.Errorf("CUSTOM_KEYS requires 1..5 custom-keys")
		}
		if err := validateRateCustomKeys(*in.CustomKeys, in.ForwardedIPConfig); err != nil {
			return err
		}
	} else if customKeyCount > 0 {
		return fmt.Errorf("%s must not specify custom-keys", in.AggregateKeyType)
	}
	if in.AggregateKeyType == string(
		awstypes.RateBasedStatementAggregateKeyTypeForwardedIp,
	) && in.ForwardedIPConfig == nil {
		return fmt.Errorf("FORWARDED_IP requires forwarded-ip-config")
	}
	if in.AggregateKeyType == string(
		awstypes.RateBasedStatementAggregateKeyTypeConstant,
	) && in.ScopeDownStatement == nil {
		return fmt.Errorf("CONSTANT requires scope-down-statement")
	}
	if in.ScopeDownStatement != nil {
		if err := validateStatementLevel2(*in.ScopeDownStatement); err != nil {
			return fmt.Errorf("scope-down-statement: %w", err)
		}
	}
	return nil
}

func expandRateBasedStatement(
	in *WebACLRateBasedStatement,
) (*awstypes.RateBasedStatement, error) {
	if err := validateRateBasedStatement(in); err != nil {
		return nil, err
	}
	out := &awstypes.RateBasedStatement{
		AggregateKeyType: awstypes.RateBasedStatementAggregateKeyType(
			in.AggregateKeyType,
		),
		Limit: aws.Int64(in.Limit),
	}
	if in.CustomKeys != nil && len(*in.CustomKeys) > 0 {
		out.CustomKeys = make(
			[]awstypes.RateBasedStatementCustomKey,
			len(*in.CustomKeys),
		)
		for index, key := range *in.CustomKeys {
			out.CustomKeys[index] = expandRateCustomKey(key)
		}
	}
	if in.EvaluationWindowSec != nil {
		out.EvaluationWindowSec = *in.EvaluationWindowSec
	}
	if in.ForwardedIPConfig != nil {
		out.ForwardedIPConfig = expandForwardedIPConfig(in.ForwardedIPConfig)
	}
	if in.ScopeDownStatement != nil {
		converted, err := expandStatementLevel2(*in.ScopeDownStatement)
		if err != nil {
			return nil, fmt.Errorf("convert scope-down-statement: %w", err)
		}
		out.ScopeDownStatement = converted
	}
	return out, nil
}

func validRateAggregateKeyType(value string) bool {
	return value == string(awstypes.RateBasedStatementAggregateKeyTypeIp) ||
		value == string(awstypes.RateBasedStatementAggregateKeyTypeForwardedIp) ||
		value == string(awstypes.RateBasedStatementAggregateKeyTypeCustomKeys) ||
		value == string(awstypes.RateBasedStatementAggregateKeyTypeConstant)
}

func validRateEvaluationWindow(value int64) bool {
	return value == 60 || value == 120 || value == 300 || value == 600
}

func validateRateCustomKeys(
	keys []WebACLRateCustomKey,
	forwardedIPConfig *WebACLForwardedIPConfig,
) error {
	hasIPKey := false
	hasForwardedIPKey := false
	for index, key := range keys {
		if err := validateRateCustomKey(key); err != nil {
			return fmt.Errorf("custom-key item %d: %w", index, err)
		}
		hasIPKey = hasIPKey || key.IP != nil
		hasForwardedIPKey = hasForwardedIPKey || key.ForwardedIP != nil
	}
	if hasForwardedIPKey && forwardedIPConfig == nil {
		return fmt.Errorf("forwarded-IP custom key requires forwarded-ip-config")
	}
	if (hasIPKey || hasForwardedIPKey) && len(keys) < 2 {
		return fmt.Errorf(
			"IP or forwarded-IP custom key requires at least one additional key",
		)
	}
	return nil
}

func validateRateCustomKey(in WebACLRateCustomKey) error {
	if countSet(
		in.ASN != nil,
		in.Cookie != nil,
		in.ForwardedIP != nil,
		in.HTTPMethod != nil,
		in.Header != nil,
		in.IP != nil,
		in.JA3Fingerprint != nil,
		in.JA4Fingerprint != nil,
		in.LabelNamespace != nil,
		in.QueryArgument != nil,
		in.QueryString != nil,
		in.URIPath != nil,
	) != 1 {
		return fmt.Errorf("custom-key must contain exactly one member")
	}
	switch {
	case in.Cookie != nil:
		return validateRateNamedKey("cookie", in.Cookie)
	case in.Header != nil:
		return validateRateNamedKey("header", in.Header)
	case in.QueryArgument != nil:
		return validateRateNamedKey("query-argument", in.QueryArgument)
	case in.QueryString != nil:
		return validateTextTransformations(in.QueryString.TextTransformations)
	case in.URIPath != nil:
		return validateTextTransformations(in.URIPath.TextTransformations)
	case in.JA3Fingerprint != nil:
		return validateRateFingerprint("JA3", in.JA3Fingerprint)
	case in.JA4Fingerprint != nil:
		return validateRateFingerprint("JA4", in.JA4Fingerprint)
	case in.LabelNamespace != nil:
		return validateRateLabelNamespace(in.LabelNamespace.Namespace)
	default:
		return nil
	}
}

func validateRateNamedKey(name string, in *WebACLRateNamedKey) error {
	length := utf8.RuneCountInString(in.Name)
	if length < 1 || length > 64 {
		return fmt.Errorf("%s name must be 1..64 characters", name)
	}
	return validateTextTransformations(in.TextTransformations)
}

func validateRateFingerprint(name string, in *WebACLJAFingerprint) error {
	if in.FallbackBehavior != string(awstypes.FallbackBehaviorMatch) &&
		in.FallbackBehavior != string(awstypes.FallbackBehaviorNoMatch) {
		return fmt.Errorf("%s fallback-behavior must be MATCH or NO_MATCH", name)
	}
	return nil
}

func validateRateLabelNamespace(value string) error {
	length := utf8.RuneCountInString(value)
	if length < 1 || length > 1024 {
		return fmt.Errorf("label-namespace must be 1..1024 characters")
	}
	if !rateLabelNamespacePattern.MatchString(value) {
		return fmt.Errorf("label-namespace must match ^[0-9A-Za-z_:-]+$")
	}
	return nil
}

func expandRateCustomKey(in WebACLRateCustomKey) awstypes.RateBasedStatementCustomKey {
	out := awstypes.RateBasedStatementCustomKey{}
	switch {
	case in.ASN != nil:
		out.ASN = &awstypes.RateLimitAsn{}
	case in.Cookie != nil:
		out.Cookie = &awstypes.RateLimitCookie{
			Name:                aws.String(in.Cookie.Name),
			TextTransformations: expandTextTransformations(in.Cookie.TextTransformations),
		}
	case in.ForwardedIP != nil:
		out.ForwardedIP = &awstypes.RateLimitForwardedIP{}
	case in.HTTPMethod != nil:
		out.HTTPMethod = &awstypes.RateLimitHTTPMethod{}
	case in.Header != nil:
		out.Header = &awstypes.RateLimitHeader{
			Name:                aws.String(in.Header.Name),
			TextTransformations: expandTextTransformations(in.Header.TextTransformations),
		}
	case in.IP != nil:
		out.IP = &awstypes.RateLimitIP{}
	case in.JA3Fingerprint != nil:
		out.JA3Fingerprint = &awstypes.RateLimitJA3Fingerprint{
			FallbackBehavior: awstypes.FallbackBehavior(
				in.JA3Fingerprint.FallbackBehavior,
			),
		}
	case in.JA4Fingerprint != nil:
		out.JA4Fingerprint = &awstypes.RateLimitJA4Fingerprint{
			FallbackBehavior: awstypes.FallbackBehavior(
				in.JA4Fingerprint.FallbackBehavior,
			),
		}
	case in.LabelNamespace != nil:
		out.LabelNamespace = &awstypes.RateLimitLabelNamespace{
			Namespace: aws.String(in.LabelNamespace.Namespace),
		}
	case in.QueryArgument != nil:
		out.QueryArgument = &awstypes.RateLimitQueryArgument{
			Name: aws.String(in.QueryArgument.Name),
			TextTransformations: expandTextTransformations(
				in.QueryArgument.TextTransformations,
			),
		}
	case in.QueryString != nil:
		out.QueryString = &awstypes.RateLimitQueryString{
			TextTransformations: expandTextTransformations(
				in.QueryString.TextTransformations,
			),
		}
	case in.URIPath != nil:
		out.UriPath = &awstypes.RateLimitUriPath{
			TextTransformations: expandTextTransformations(
				in.URIPath.TextTransformations,
			),
		}
	}
	return out
}
