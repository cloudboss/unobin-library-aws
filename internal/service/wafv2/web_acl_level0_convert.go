package wafv2

import (
	"encoding/base64"
	"fmt"
	"math"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

func validateStatementLevel0(statement WebACLStatementLevel0) error {
	count := countSet(
		statement.ASNMatchStatement != nil,
		statement.ByteMatchStatement != nil,
		statement.GeoMatchStatement != nil,
		statement.IPSetReferenceStatement != nil,
		statement.LabelMatchStatement != nil,
		statement.RegexMatchStatement != nil,
		statement.RegexPatternSetReference != nil,
		statement.SizeConstraintStatement != nil,
		statement.SQLiMatchStatement != nil,
		statement.XSSMatchStatement != nil,
	)
	if count != 1 {
		return fmt.Errorf("level-0 statement must contain exactly one member")
	}
	if statement.ByteMatchStatement != nil {
		search, err := byteMatchSearchString(statement.ByteMatchStatement)
		if err != nil {
			return err
		}
		if err := validateByteMatchSpecialConstraints(
			statement.ByteMatchStatement,
			search,
		); err != nil {
			return err
		}
	}
	if field := level0FieldToMatch(statement); field != nil {
		if err := validateFieldToMatch(*field); err != nil {
			return err
		}
	}
	if err := validateLevel0TextTransformations(statement); err != nil {
		return err
	}
	return nil
}

func validateLevel0TextTransformations(
	statement WebACLStatementLevel0,
) error {
	switch {
	case statement.ByteMatchStatement != nil:
		return validateTextTransformations(statement.ByteMatchStatement.TextTransformations)
	case statement.RegexMatchStatement != nil:
		return validateTextTransformations(statement.RegexMatchStatement.TextTransformations)
	case statement.RegexPatternSetReference != nil:
		return validateTextTransformations(
			statement.RegexPatternSetReference.TextTransformations,
		)
	case statement.SizeConstraintStatement != nil:
		return validateTextTransformations(
			statement.SizeConstraintStatement.TextTransformations,
		)
	case statement.SQLiMatchStatement != nil:
		return validateTextTransformations(statement.SQLiMatchStatement.TextTransformations)
	case statement.XSSMatchStatement != nil:
		return validateTextTransformations(statement.XSSMatchStatement.TextTransformations)
	default:
		return nil
	}
}

func validateTextTransformations(transformations []WebACLTextTransformation) error {
	if len(transformations) == 0 {
		return fmt.Errorf("text-transformations must contain at least one member")
	}
	priorities := make(map[int64]struct{}, len(transformations))
	for _, transformation := range transformations {
		if transformation.Priority < 0 || transformation.Priority > math.MaxInt32 {
			return fmt.Errorf("text-transformation priority must be 0..%d", math.MaxInt32)
		}
		if _, exists := priorities[transformation.Priority]; exists {
			return fmt.Errorf("text-transformations must use distinct priorities")
		}
		priorities[transformation.Priority] = struct{}{}
	}
	return nil
}

func validateByteMatchSpecialConstraints(
	statement *WebACLByteMatchStatement,
	search []byte,
) error {
	field := statement.FieldToMatch
	if (field.JA3Fingerprint != nil || field.JA4Fingerprint != nil) &&
		statement.PositionalConstraint != "EXACTLY" {
		return fmt.Errorf("byte match with JA3 and JA4 must use EXACTLY")
	}
	if statement.PositionalConstraint != "CONTAINS_WORD" {
		return nil
	}
	for _, value := range search {
		if (value >= 'a' && value <= 'z') ||
			(value >= 'A' && value <= 'Z') ||
			(value >= '0' && value <= '9') || value == '_' {
			continue
		}
		return fmt.Errorf("CONTAINS_WORD search bytes must be ASCII alphanumeric or underscore")
	}
	return nil
}

func countSet(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func level0FieldToMatch(statement WebACLStatementLevel0) *WebACLFieldToMatch {
	switch {
	case statement.ByteMatchStatement != nil:
		return &statement.ByteMatchStatement.FieldToMatch
	case statement.RegexMatchStatement != nil:
		return &statement.RegexMatchStatement.FieldToMatch
	case statement.RegexPatternSetReference != nil:
		return &statement.RegexPatternSetReference.FieldToMatch
	case statement.SizeConstraintStatement != nil:
		return &statement.SizeConstraintStatement.FieldToMatch
	case statement.SQLiMatchStatement != nil:
		return &statement.SQLiMatchStatement.FieldToMatch
	case statement.XSSMatchStatement != nil:
		return &statement.XSSMatchStatement.FieldToMatch
	default:
		return nil
	}
}

func validateFieldToMatch(field WebACLFieldToMatch) error {
	count := countSet(
		field.AllQueryArguments != nil,
		field.Body != nil,
		field.Cookies != nil,
		field.HeaderOrder != nil,
		field.Headers != nil,
		field.JA3Fingerprint != nil,
		field.JA4Fingerprint != nil,
		field.JSONBody != nil,
		field.Method != nil,
		field.QueryString != nil,
		field.SingleHeader != nil,
		field.SingleQueryArgument != nil,
		field.URIFragment != nil,
		field.URIPath != nil,
	)
	if count != 1 {
		return fmt.Errorf("field-to-match must contain exactly one member")
	}
	if field.Cookies != nil {
		pattern := field.Cookies.MatchPattern
		if countSet(
			pattern.All != nil,
			pattern.ExcludedCookies != nil,
			pattern.IncludedCookies != nil,
		) != 1 {
			return fmt.Errorf("cookie match-pattern must contain exactly one member")
		}
	}
	if field.Headers != nil {
		pattern := field.Headers.MatchPattern
		if countSet(
			pattern.All != nil,
			pattern.ExcludedHeaders != nil,
			pattern.IncludedHeaders != nil,
		) != 1 {
			return fmt.Errorf("header match-pattern must contain exactly one member")
		}
	}
	if field.JSONBody != nil {
		pattern := field.JSONBody.MatchPattern
		if countSet(pattern.All != nil, pattern.IncludedPaths != nil) != 1 {
			return fmt.Errorf("JSON match-pattern must contain exactly one member")
		}
	}
	return nil
}

func expandStatementLevel0(statement WebACLStatementLevel0) (*awstypes.Statement, error) {
	if err := validateStatementLevel0(statement); err != nil {
		return nil, err
	}
	out := &awstypes.Statement{}
	switch {
	case statement.ASNMatchStatement != nil:
		out.AsnMatchStatement = expandASNMatchStatement(statement.ASNMatchStatement)
	case statement.ByteMatchStatement != nil:
		converted, err := expandByteMatchStatement(statement.ByteMatchStatement)
		if err != nil {
			return nil, err
		}
		out.ByteMatchStatement = converted
	case statement.GeoMatchStatement != nil:
		out.GeoMatchStatement = expandGeoMatchStatement(statement.GeoMatchStatement)
	case statement.IPSetReferenceStatement != nil:
		out.IPSetReferenceStatement = expandIPSetReferenceStatement(
			statement.IPSetReferenceStatement,
		)
	case statement.LabelMatchStatement != nil:
		out.LabelMatchStatement = expandLabelMatchStatement(statement.LabelMatchStatement)
	case statement.RegexMatchStatement != nil:
		out.RegexMatchStatement = expandRegexMatchStatement(statement.RegexMatchStatement)
	case statement.RegexPatternSetReference != nil:
		out.RegexPatternSetReferenceStatement = expandRegexPatternSetReferenceStatement(
			statement.RegexPatternSetReference,
		)
	case statement.SizeConstraintStatement != nil:
		out.SizeConstraintStatement = expandSizeConstraintStatement(
			statement.SizeConstraintStatement,
		)
	case statement.SQLiMatchStatement != nil:
		out.SqliMatchStatement = expandSQLiMatchStatement(statement.SQLiMatchStatement)
	case statement.XSSMatchStatement != nil:
		out.XssMatchStatement = expandXSSMatchStatement(statement.XSSMatchStatement)
	}
	return out, nil
}

func expandASNMatchStatement(in *WebACLASNMatchStatement) *awstypes.AsnMatchStatement {
	out := &awstypes.AsnMatchStatement{AsnList: append([]int64(nil), in.ASNList...)}
	if in.ForwardedIPConfig != nil {
		out.ForwardedIPConfig = expandForwardedIPConfig(in.ForwardedIPConfig)
	}
	return out
}

func expandForwardedIPConfig(in *WebACLForwardedIPConfig) *awstypes.ForwardedIPConfig {
	return &awstypes.ForwardedIPConfig{
		FallbackBehavior: awstypes.FallbackBehavior(in.FallbackBehavior),
		HeaderName:       aws.String(in.HeaderName),
	}
}

func expandByteMatchStatement(
	in *WebACLByteMatchStatement,
) (*awstypes.ByteMatchStatement, error) {
	search, err := byteMatchSearchString(in)
	if err != nil {
		return nil, err
	}
	field, err := expandFieldToMatch(in.FieldToMatch)
	if err != nil {
		return nil, err
	}
	return &awstypes.ByteMatchStatement{
		FieldToMatch:         field,
		PositionalConstraint: awstypes.PositionalConstraint(in.PositionalConstraint),
		SearchString:         search,
		TextTransformations:  expandTextTransformations(in.TextTransformations),
	}, nil
}

func byteMatchSearchString(in *WebACLByteMatchStatement) ([]byte, error) {
	if countSet(in.SearchString != nil, in.SearchStringBase64 != nil) != 1 {
		return nil, fmt.Errorf(
			"byte match must contain exactly one of search-string or search-string-base64",
		)
	}
	var value []byte
	if in.SearchString != nil {
		value = []byte(*in.SearchString)
	} else {
		encoded := *in.SearchStringBase64
		decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode byte match search-string-base64: %w", err)
		}
		if base64.StdEncoding.EncodeToString(decoded) != encoded {
			return nil, fmt.Errorf("byte match search-string-base64 must be canonical base64")
		}
		value = decoded
	}
	if len(value) < 1 || len(value) > 200 {
		return nil, fmt.Errorf("byte match search value must decode to 1..200 bytes")
	}
	return value, nil
}

func expandGeoMatchStatement(in *WebACLGeoMatchStatement) *awstypes.GeoMatchStatement {
	countries := make([]awstypes.CountryCode, len(in.CountryCodes))
	for index, country := range in.CountryCodes {
		countries[index] = awstypes.CountryCode(country)
	}
	out := &awstypes.GeoMatchStatement{CountryCodes: countries}
	if in.ForwardedIPConfig != nil {
		out.ForwardedIPConfig = expandForwardedIPConfig(in.ForwardedIPConfig)
	}
	return out
}

func expandIPSetReferenceStatement(
	in *WebACLIPSetReferenceStatement,
) *awstypes.IPSetReferenceStatement {
	out := &awstypes.IPSetReferenceStatement{ARN: aws.String(in.ARN)}
	if in.IPSetForwardedIPConfig != nil {
		config := in.IPSetForwardedIPConfig
		out.IPSetForwardedIPConfig = &awstypes.IPSetForwardedIPConfig{
			FallbackBehavior: awstypes.FallbackBehavior(config.FallbackBehavior),
			HeaderName:       aws.String(config.HeaderName),
			Position:         awstypes.ForwardedIPPosition(config.Position),
		}
	}
	return out
}

func expandLabelMatchStatement(in *WebACLLabelMatchStatement) *awstypes.LabelMatchStatement {
	return &awstypes.LabelMatchStatement{
		Key:   aws.String(in.Key),
		Scope: awstypes.LabelMatchScope(in.Scope),
	}
}

func expandRegexMatchStatement(in *WebACLRegexMatchStatement) *awstypes.RegexMatchStatement {
	field, _ := expandFieldToMatch(in.FieldToMatch)
	return &awstypes.RegexMatchStatement{
		FieldToMatch:        field,
		RegexString:         aws.String(in.RegexString),
		TextTransformations: expandTextTransformations(in.TextTransformations),
	}
}

func expandRegexPatternSetReferenceStatement(
	in *WebACLRegexPatternSetReferenceStatement,
) *awstypes.RegexPatternSetReferenceStatement {
	field, _ := expandFieldToMatch(in.FieldToMatch)
	return &awstypes.RegexPatternSetReferenceStatement{
		ARN:                 aws.String(in.ARN),
		FieldToMatch:        field,
		TextTransformations: expandTextTransformations(in.TextTransformations),
	}
}

func expandSizeConstraintStatement(
	in *WebACLSizeConstraintStatement,
) *awstypes.SizeConstraintStatement {
	field, _ := expandFieldToMatch(in.FieldToMatch)
	return &awstypes.SizeConstraintStatement{
		ComparisonOperator:  awstypes.ComparisonOperator(in.ComparisonOperator),
		FieldToMatch:        field,
		Size:                in.Size,
		TextTransformations: expandTextTransformations(in.TextTransformations),
	}
}

func expandSQLiMatchStatement(in *WebACLSQLiMatchStatement) *awstypes.SqliMatchStatement {
	field, _ := expandFieldToMatch(in.FieldToMatch)
	out := &awstypes.SqliMatchStatement{
		FieldToMatch:        field,
		TextTransformations: expandTextTransformations(in.TextTransformations),
	}
	if in.SensitivityLevel != nil {
		out.SensitivityLevel = awstypes.SensitivityLevel(*in.SensitivityLevel)
	}
	return out
}

func expandXSSMatchStatement(in *WebACLXSSMatchStatement) *awstypes.XssMatchStatement {
	field, _ := expandFieldToMatch(in.FieldToMatch)
	return &awstypes.XssMatchStatement{
		FieldToMatch:        field,
		TextTransformations: expandTextTransformations(in.TextTransformations),
	}
}

func expandTextTransformations(
	in []WebACLTextTransformation,
) []awstypes.TextTransformation {
	out := make([]awstypes.TextTransformation, len(in))
	for index, transformation := range in {
		out[index] = awstypes.TextTransformation{
			Priority: int32(transformation.Priority),
			Type:     awstypes.TextTransformationType(transformation.Type),
		}
	}
	return out
}

func expandFieldToMatch(in WebACLFieldToMatch) (*awstypes.FieldToMatch, error) {
	if err := validateFieldToMatch(in); err != nil {
		return nil, err
	}
	out := &awstypes.FieldToMatch{}
	switch {
	case in.AllQueryArguments != nil:
		out.AllQueryArguments = &awstypes.AllQueryArguments{}
	case in.Body != nil:
		out.Body = &awstypes.Body{}
		if in.Body.OversizeHandling != nil {
			out.Body.OversizeHandling = awstypes.OversizeHandling(*in.Body.OversizeHandling)
		}
	case in.Cookies != nil:
		out.Cookies = expandCookies(in.Cookies)
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
		out.JsonBody = expandJSONBody(in.JSONBody)
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
		out.UriFragment = &awstypes.UriFragment{}
		if in.URIFragment.FallbackBehavior != nil {
			out.UriFragment.FallbackBehavior = awstypes.FallbackBehavior(
				*in.URIFragment.FallbackBehavior,
			)
		}
	case in.URIPath != nil:
		out.UriPath = &awstypes.UriPath{}
	}
	return out, nil
}

func expandCookies(in *WebACLCookies) *awstypes.Cookies {
	return &awstypes.Cookies{
		MatchPattern:     expandCookieMatchPattern(in.MatchPattern),
		MatchScope:       awstypes.MapMatchScope(in.MatchScope),
		OversizeHandling: awstypes.OversizeHandling(in.OversizeHandling),
	}
}

func expandCookieMatchPattern(in WebACLCookieMatchPattern) *awstypes.CookieMatchPattern {
	out := &awstypes.CookieMatchPattern{}
	switch {
	case in.All != nil:
		out.All = &awstypes.All{}
	case in.ExcludedCookies != nil:
		out.ExcludedCookies = append([]string(nil), (*in.ExcludedCookies)...)
	case in.IncludedCookies != nil:
		out.IncludedCookies = append([]string(nil), (*in.IncludedCookies)...)
	}
	return out
}

func expandHeaders(in *WebACLHeaders) *awstypes.Headers {
	return &awstypes.Headers{
		MatchPattern:     expandHeaderMatchPattern(in.MatchPattern),
		MatchScope:       awstypes.MapMatchScope(in.MatchScope),
		OversizeHandling: awstypes.OversizeHandling(in.OversizeHandling),
	}
}

func expandHeaderMatchPattern(in WebACLHeaderMatchPattern) *awstypes.HeaderMatchPattern {
	out := &awstypes.HeaderMatchPattern{}
	switch {
	case in.All != nil:
		out.All = &awstypes.All{}
	case in.ExcludedHeaders != nil:
		out.ExcludedHeaders = append([]string(nil), (*in.ExcludedHeaders)...)
	case in.IncludedHeaders != nil:
		out.IncludedHeaders = append([]string(nil), (*in.IncludedHeaders)...)
	}
	return out
}

func expandJSONBody(in *WebACLJSONBody) *awstypes.JsonBody {
	out := &awstypes.JsonBody{
		MatchPattern:     expandJSONMatchPattern(in.MatchPattern),
		MatchScope:       awstypes.JsonMatchScope(in.MatchScope),
		OversizeHandling: awstypes.OversizeHandling(in.OversizeHandling),
	}
	if in.InvalidFallbackBehavior != nil {
		out.InvalidFallbackBehavior = awstypes.BodyParsingFallbackBehavior(
			*in.InvalidFallbackBehavior,
		)
	}
	return out
}

func expandJSONMatchPattern(in WebACLJSONMatchPattern) *awstypes.JsonMatchPattern {
	out := &awstypes.JsonMatchPattern{}
	if in.All != nil {
		out.All = &awstypes.All{}
	} else if in.IncludedPaths != nil {
		out.IncludedPaths = append([]string(nil), (*in.IncludedPaths)...)
	}
	return out
}
