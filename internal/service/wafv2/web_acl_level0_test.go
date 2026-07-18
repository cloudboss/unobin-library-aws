package wafv2

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandStatementLevel0Leaves(t *testing.T) {
	search := "needle"
	sensitivity := "HIGH"
	tests := []struct {
		name     string
		input    WebACLStatementLevel0
		expected *awstypes.Statement
	}{
		{
			name: "ASN",
			input: WebACLStatementLevel0{
				ASNMatchStatement: &WebACLASNMatchStatement{
					ASNList: []int64{64512},
					ForwardedIPConfig: &WebACLForwardedIPConfig{
						FallbackBehavior: "MATCH",
						HeaderName:       "X-Forwarded-For",
					},
				},
			},
			expected: &awstypes.Statement{
				AsnMatchStatement: &awstypes.AsnMatchStatement{
					AsnList: []int64{64512},
					ForwardedIPConfig: &awstypes.ForwardedIPConfig{
						FallbackBehavior: awstypes.FallbackBehaviorMatch,
						HeaderName:       aws.String("X-Forwarded-For"),
					},
				},
			},
		},
		{
			name: "byte",
			input: WebACLStatementLevel0{
				ByteMatchStatement: &WebACLByteMatchStatement{
					FieldToMatch:         methodFieldToMatch(),
					PositionalConstraint: "CONTAINS",
					SearchString:         &search,
					TextTransformations:  testTextTransformations(),
				},
			},
			expected: &awstypes.Statement{
				ByteMatchStatement: &awstypes.ByteMatchStatement{
					FieldToMatch:         &awstypes.FieldToMatch{Method: &awstypes.Method{}},
					PositionalConstraint: awstypes.PositionalConstraintContains,
					SearchString:         []byte("needle"),
					TextTransformations:  testSDKTextTransformations(),
				},
			},
		},
		{
			name: "geo",
			input: WebACLStatementLevel0{
				GeoMatchStatement: &WebACLGeoMatchStatement{CountryCodes: []string{"US"}},
			},
			expected: &awstypes.Statement{
				GeoMatchStatement: &awstypes.GeoMatchStatement{
					CountryCodes: []awstypes.CountryCode{awstypes.CountryCodeUs},
				},
			},
		},
		{
			name: "IP set",
			input: WebACLStatementLevel0{
				IPSetReferenceStatement: &WebACLIPSetReferenceStatement{
					ARN: "arn:aws:wafv2:us-east-1:123456789012:regional/ipset/example/id",
					IPSetForwardedIPConfig: &WebACLIPSetForwardedIPConfig{
						FallbackBehavior: "NO_MATCH",
						HeaderName:       "X-Forwarded-For",
						Position:         "FIRST",
					},
				},
			},
			expected: &awstypes.Statement{
				IPSetReferenceStatement: &awstypes.IPSetReferenceStatement{
					ARN: aws.String(
						"arn:aws:wafv2:us-east-1:123456789012:regional/ipset/example/id",
					),
					IPSetForwardedIPConfig: &awstypes.IPSetForwardedIPConfig{
						FallbackBehavior: awstypes.FallbackBehaviorNoMatch,
						HeaderName:       aws.String("X-Forwarded-For"),
						Position:         awstypes.ForwardedIPPositionFirst,
					},
				},
			},
		},
		{
			name: "label",
			input: WebACLStatementLevel0{
				LabelMatchStatement: &WebACLLabelMatchStatement{
					Key:   "namespace:label",
					Scope: "LABEL",
				},
			},
			expected: &awstypes.Statement{
				LabelMatchStatement: &awstypes.LabelMatchStatement{
					Key:   aws.String("namespace:label"),
					Scope: awstypes.LabelMatchScopeLabel,
				},
			},
		},
		{
			name: "regex",
			input: WebACLStatementLevel0{
				RegexMatchStatement: &WebACLRegexMatchStatement{
					FieldToMatch:        queryStringFieldToMatch(),
					RegexString:         "^/api/",
					TextTransformations: testTextTransformations(),
				},
			},
			expected: &awstypes.Statement{
				RegexMatchStatement: &awstypes.RegexMatchStatement{
					FieldToMatch:        &awstypes.FieldToMatch{QueryString: &awstypes.QueryString{}},
					RegexString:         aws.String("^/api/"),
					TextTransformations: testSDKTextTransformations(),
				},
			},
		},
		{
			name: "regex pattern set",
			input: WebACLStatementLevel0{
				RegexPatternSetReference: &WebACLRegexPatternSetReferenceStatement{
					ARN:                 "arn:aws:wafv2:us-east-1:123:regional/regexpatternset/a/id",
					FieldToMatch:        uriPathFieldToMatch(),
					TextTransformations: testTextTransformations(),
				},
			},
			expected: &awstypes.Statement{
				RegexPatternSetReferenceStatement: &awstypes.RegexPatternSetReferenceStatement{
					ARN:                 aws.String("arn:aws:wafv2:us-east-1:123:regional/regexpatternset/a/id"),
					FieldToMatch:        &awstypes.FieldToMatch{UriPath: &awstypes.UriPath{}},
					TextTransformations: testSDKTextTransformations(),
				},
			},
		},
		{
			name: "size",
			input: WebACLStatementLevel0{
				SizeConstraintStatement: &WebACLSizeConstraintStatement{
					ComparisonOperator:  "GT",
					FieldToMatch:        bodyFieldToMatch(),
					Size:                1024,
					TextTransformations: testTextTransformations(),
				},
			},
			expected: &awstypes.Statement{
				SizeConstraintStatement: &awstypes.SizeConstraintStatement{
					ComparisonOperator:  awstypes.ComparisonOperatorGt,
					FieldToMatch:        &awstypes.FieldToMatch{Body: &awstypes.Body{}},
					Size:                1024,
					TextTransformations: testSDKTextTransformations(),
				},
			},
		},
		{
			name: "SQLi",
			input: WebACLStatementLevel0{
				SQLiMatchStatement: &WebACLSQLiMatchStatement{
					FieldToMatch:        allQueryArgumentsFieldToMatch(),
					SensitivityLevel:    &sensitivity,
					TextTransformations: testTextTransformations(),
				},
			},
			expected: &awstypes.Statement{
				SqliMatchStatement: &awstypes.SqliMatchStatement{
					FieldToMatch: &awstypes.FieldToMatch{
						AllQueryArguments: &awstypes.AllQueryArguments{},
					},
					SensitivityLevel:    awstypes.SensitivityLevelHigh,
					TextTransformations: testSDKTextTransformations(),
				},
			},
		},
		{
			name: "XSS",
			input: WebACLStatementLevel0{
				XSSMatchStatement: &WebACLXSSMatchStatement{
					FieldToMatch: WebACLFieldToMatch{
						SingleHeader: &WebACLSingleName{Name: "user-agent"},
					},
					TextTransformations: testTextTransformations(),
				},
			},
			expected: &awstypes.Statement{
				XssMatchStatement: &awstypes.XssMatchStatement{
					FieldToMatch: &awstypes.FieldToMatch{
						SingleHeader: &awstypes.SingleHeader{Name: aws.String("user-agent")},
					},
					TextTransformations: testSDKTextTransformations(),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := expandStatementLevel0(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestValidateStatementLevel0RequiresExactlyOneMember(t *testing.T) {
	tests := []struct {
		name  string
		input WebACLStatementLevel0
	}{
		{name: "zero", input: WebACLStatementLevel0{}},
		{
			name: "multiple",
			input: WebACLStatementLevel0{
				ASNMatchStatement: &WebACLASNMatchStatement{ASNList: []int64{1}},
				GeoMatchStatement: &WebACLGeoMatchStatement{CountryCodes: []string{"US"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStatementLevel0(tt.input)
			assert.ErrorContains(t, err, "exactly one")
		})
	}
}

func TestExpandByteMatchSearchString(t *testing.T) {
	raw := "plain"
	encoded := base64.StdEncoding.EncodeToString([]byte{0, 1, 2})
	invalid := "AAE"
	nonCanonical := "Zh=="
	empty := ""
	tooLong := strings.Repeat("a", 201)
	encodedTooLong := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 201)))
	tests := []struct {
		name     string
		raw      *string
		encoded  *string
		expected []byte
		wantErr  string
	}{
		{name: "raw", raw: &raw, expected: []byte("plain")},
		{name: "base64", encoded: &encoded, expected: []byte{0, 1, 2}},
		{name: "neither", wantErr: "exactly one"},
		{name: "both", raw: &raw, encoded: &encoded, wantErr: "exactly one"},
		{name: "invalid base64", encoded: &invalid, wantErr: "base64"},
		{name: "non-canonical base64", encoded: &nonCanonical, wantErr: "base64"},
		{name: "empty raw", raw: &empty, wantErr: "1..200"},
		{name: "empty base64", encoded: &empty, wantErr: "1..200"},
		{name: "raw too long", raw: &tooLong, wantErr: "1..200"},
		{name: "base64 too long", encoded: &encodedTooLong, wantErr: "1..200"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statement := WebACLStatementLevel0{
				ByteMatchStatement: &WebACLByteMatchStatement{
					FieldToMatch:         methodFieldToMatch(),
					PositionalConstraint: "EXACTLY",
					SearchString:         tt.raw,
					SearchStringBase64:   tt.encoded,
					TextTransformations:  testTextTransformations(),
				},
			}
			actual, err := expandStatementLevel0(statement)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual.ByteMatchStatement.SearchString)
		})
	}
}

func TestValidateLevel0TextTransformations(t *testing.T) {
	tests := []struct {
		name            string
		transformations []WebACLTextTransformation
		wantErr         string
	}{
		{name: "missing", wantErr: "at least one"},
		{
			name: "duplicate priority",
			transformations: []WebACLTextTransformation{
				{Priority: 1, Type: "NONE"},
				{Priority: 1, Type: "LOWERCASE"},
			},
			wantErr: "distinct priorities",
		},
		{
			name:            "valid",
			transformations: testTextTransformations(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := expandStatementLevel0(WebACLStatementLevel0{
				RegexMatchStatement: &WebACLRegexMatchStatement{
					FieldToMatch:        methodFieldToMatch(),
					RegexString:         "test",
					TextTransformations: tt.transformations,
				},
			})
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateByteMatchSpecialConstraints(t *testing.T) {
	validWord := "abc_123"
	invalidWord := "abc-123"
	search := "fingerprint"
	tests := []struct {
		name       string
		field      WebACLFieldToMatch
		constraint string
		search     *string
		wantErr    string
	}{
		{
			name: "JA3 requires exact",
			field: WebACLFieldToMatch{
				JA3Fingerprint: &WebACLJAFingerprint{FallbackBehavior: "NO_MATCH"},
			},
			constraint: "CONTAINS",
			search:     &search,
			wantErr:    "JA3 and JA4",
		},
		{
			name: "JA4 exact",
			field: WebACLFieldToMatch{
				JA4Fingerprint: &WebACLJAFingerprint{FallbackBehavior: "MATCH"},
			},
			constraint: "EXACTLY",
			search:     &search,
		},
		{
			name:       "contains word accepts ASCII word bytes",
			field:      methodFieldToMatch(),
			constraint: "CONTAINS_WORD",
			search:     &validWord,
		},
		{
			name:       "contains word rejects punctuation",
			field:      methodFieldToMatch(),
			constraint: "CONTAINS_WORD",
			search:     &invalidWord,
			wantErr:    "ASCII alphanumeric or underscore",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := expandStatementLevel0(WebACLStatementLevel0{
				ByteMatchStatement: &WebACLByteMatchStatement{
					FieldToMatch:         tt.field,
					PositionalConstraint: tt.constraint,
					SearchString:         tt.search,
					TextTransformations:  testTextTransformations(),
				},
			})
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateLevel0FieldToMatchRequiresExactlyOneMember(t *testing.T) {
	raw := "value"
	tests := []struct {
		name  string
		field WebACLFieldToMatch
	}{
		{name: "zero", field: WebACLFieldToMatch{}},
		{
			name: "multiple",
			field: WebACLFieldToMatch{
				Method:      &WebACLEmpty{},
				QueryString: &WebACLEmpty{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := expandStatementLevel0(WebACLStatementLevel0{
				ByteMatchStatement: &WebACLByteMatchStatement{
					FieldToMatch:         tt.field,
					PositionalConstraint: "EXACTLY",
					SearchString:         &raw,
					TextTransformations:  testTextTransformations(),
				},
			})
			assert.ErrorContains(t, err, "field-to-match must contain exactly one")
		})
	}
}

func testTextTransformations() []WebACLTextTransformation {
	return []WebACLTextTransformation{{Priority: 0, Type: "NONE"}}
}

func testSDKTextTransformations() []awstypes.TextTransformation {
	return []awstypes.TextTransformation{{
		Priority: 0,
		Type:     awstypes.TextTransformationTypeNone,
	}}
}

func methodFieldToMatch() WebACLFieldToMatch {
	return WebACLFieldToMatch{Method: &WebACLEmpty{}}
}

func queryStringFieldToMatch() WebACLFieldToMatch {
	return WebACLFieldToMatch{QueryString: &WebACLEmpty{}}
}

func uriPathFieldToMatch() WebACLFieldToMatch {
	return WebACLFieldToMatch{URIPath: &WebACLEmpty{}}
}

func bodyFieldToMatch() WebACLFieldToMatch {
	return WebACLFieldToMatch{Body: &WebACLBody{}}
}

func allQueryArgumentsFieldToMatch() WebACLFieldToMatch {
	return WebACLFieldToMatch{AllQueryArguments: &WebACLEmpty{}}
}
