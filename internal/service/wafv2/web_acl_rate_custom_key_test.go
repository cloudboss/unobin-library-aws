package wafv2

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandRateCustomKeyVariants(t *testing.T) {
	tests := []struct {
		name     string
		input    WebACLRateCustomKey
		expected awstypes.RateBasedStatementCustomKey
	}{
		{
			name:     "ASN",
			input:    WebACLRateCustomKey{ASN: &WebACLEmpty{}},
			expected: awstypes.RateBasedStatementCustomKey{ASN: &awstypes.RateLimitAsn{}},
		},
		{
			name: "cookie",
			input: WebACLRateCustomKey{Cookie: &WebACLRateNamedKey{
				Name:                "session",
				TextTransformations: rateTransformations(),
			}},
			expected: awstypes.RateBasedStatementCustomKey{
				Cookie: &awstypes.RateLimitCookie{
					Name:                aws.String("session"),
					TextTransformations: sdkRateTransformations(),
				},
			},
		},
		{
			name:  "forwarded IP",
			input: WebACLRateCustomKey{ForwardedIP: &WebACLEmpty{}},
			expected: awstypes.RateBasedStatementCustomKey{
				ForwardedIP: &awstypes.RateLimitForwardedIP{},
			},
		},
		{
			name:  "HTTP method",
			input: WebACLRateCustomKey{HTTPMethod: &WebACLEmpty{}},
			expected: awstypes.RateBasedStatementCustomKey{
				HTTPMethod: &awstypes.RateLimitHTTPMethod{},
			},
		},
		{
			name: "header",
			input: WebACLRateCustomKey{Header: &WebACLRateNamedKey{
				Name:                "X-Tenant",
				TextTransformations: rateTransformations(),
			}},
			expected: awstypes.RateBasedStatementCustomKey{
				Header: &awstypes.RateLimitHeader{
					Name:                aws.String("X-Tenant"),
					TextTransformations: sdkRateTransformations(),
				},
			},
		},
		{
			name:     "IP",
			input:    WebACLRateCustomKey{IP: &WebACLEmpty{}},
			expected: awstypes.RateBasedStatementCustomKey{IP: &awstypes.RateLimitIP{}},
		},
		{
			name: "JA3",
			input: WebACLRateCustomKey{
				JA3Fingerprint: &WebACLJAFingerprint{FallbackBehavior: "MATCH"},
			},
			expected: awstypes.RateBasedStatementCustomKey{
				JA3Fingerprint: &awstypes.RateLimitJA3Fingerprint{
					FallbackBehavior: awstypes.FallbackBehaviorMatch,
				},
			},
		},
		{
			name: "JA4",
			input: WebACLRateCustomKey{
				JA4Fingerprint: &WebACLJAFingerprint{FallbackBehavior: "NO_MATCH"},
			},
			expected: awstypes.RateBasedStatementCustomKey{
				JA4Fingerprint: &awstypes.RateLimitJA4Fingerprint{
					FallbackBehavior: awstypes.FallbackBehaviorNoMatch,
				},
			},
		},
		{
			name: "label namespace",
			input: WebACLRateCustomKey{
				LabelNamespace: &WebACLRateLabelNamespace{Namespace: "awswaf:managed:test"},
			},
			expected: awstypes.RateBasedStatementCustomKey{
				LabelNamespace: &awstypes.RateLimitLabelNamespace{
					Namespace: aws.String("awswaf:managed:test"),
				},
			},
		},
		{
			name: "query argument",
			input: WebACLRateCustomKey{QueryArgument: &WebACLRateNamedKey{
				Name:                "tenant",
				TextTransformations: rateTransformations(),
			}},
			expected: awstypes.RateBasedStatementCustomKey{
				QueryArgument: &awstypes.RateLimitQueryArgument{
					Name:                aws.String("tenant"),
					TextTransformations: sdkRateTransformations(),
				},
			},
		},
		{
			name: "query string",
			input: WebACLRateCustomKey{QueryString: &WebACLRateTransformedKey{
				TextTransformations: rateTransformations(),
			}},
			expected: awstypes.RateBasedStatementCustomKey{
				QueryString: &awstypes.RateLimitQueryString{
					TextTransformations: sdkRateTransformations(),
				},
			},
		},
		{
			name: "URI path",
			input: WebACLRateCustomKey{URIPath: &WebACLRateTransformedKey{
				TextTransformations: rateTransformations(),
			}},
			expected: awstypes.RateBasedStatementCustomKey{
				UriPath: &awstypes.RateLimitUriPath{
					TextTransformations: sdkRateTransformations(),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, validateRateCustomKey(tt.input))
			assert.Equal(t, tt.expected, expandRateCustomKey(tt.input))
		})
	}
}

func TestExpandRateBasedStatementCustomKeys(t *testing.T) {
	keys := []WebACLRateCustomKey{
		{ASN: &WebACLEmpty{}},
		{Cookie: &WebACLRateNamedKey{
			Name:                "session",
			TextTransformations: rateTransformations(),
		}},
		{HTTPMethod: &WebACLEmpty{}},
		{JA3Fingerprint: &WebACLJAFingerprint{FallbackBehavior: "MATCH"}},
		{QueryString: &WebACLRateTransformedKey{
			TextTransformations: rateTransformations(),
		}},
	}
	input := validRateBasedStatement("CUSTOM_KEYS")
	input.CustomKeys = &keys
	input.ForwardedIPConfig = validRateForwardedIPConfig()
	input.ScopeDownStatement = rateScopeDown()
	expected := &awstypes.RateBasedStatement{
		AggregateKeyType: awstypes.RateBasedStatementAggregateKeyTypeCustomKeys,
		CustomKeys: []awstypes.RateBasedStatementCustomKey{
			{ASN: &awstypes.RateLimitAsn{}},
			{Cookie: &awstypes.RateLimitCookie{
				Name:                aws.String("session"),
				TextTransformations: sdkRateTransformations(),
			}},
			{HTTPMethod: &awstypes.RateLimitHTTPMethod{}},
			{JA3Fingerprint: &awstypes.RateLimitJA3Fingerprint{
				FallbackBehavior: awstypes.FallbackBehaviorMatch,
			}},
			{QueryString: &awstypes.RateLimitQueryString{
				TextTransformations: sdkRateTransformations(),
			}},
		},
		ForwardedIPConfig: &awstypes.ForwardedIPConfig{
			FallbackBehavior: awstypes.FallbackBehaviorMatch,
			HeaderName:       aws.String("X-Forwarded-For"),
		},
		Limit: aws.Int64(10),
		ScopeDownStatement: &awstypes.Statement{
			LabelMatchStatement: &awstypes.LabelMatchStatement{
				Key:   aws.String("rate-scope"),
				Scope: awstypes.LabelMatchScopeLabel,
			},
		},
	}

	actual, err := expandRateBasedStatement(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
	assert.Zero(t, actual.EvaluationWindowSec)
}

func TestValidateRateCustomKeyExactlyOne(t *testing.T) {
	tests := []struct {
		name  string
		input WebACLRateCustomKey
	}{
		{name: "empty"},
		{
			name: "multiple",
			input: WebACLRateCustomKey{
				ASN:        &WebACLEmpty{},
				HTTPMethod: &WebACLEmpty{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(
				t,
				validateRateCustomKey(tt.input),
				"custom-key must contain exactly one member",
			)
		})
	}
}

func TestValidateRateCustomKeyFields(t *testing.T) {
	longName := strings.Repeat("n", 65)
	longNamespace := strings.Repeat("n", 1025)
	duplicateTransformations := []WebACLTextTransformation{
		{Priority: 1, Type: "NONE"},
		{Priority: 1, Type: "LOWERCASE"},
	}
	tests := []struct {
		name    string
		input   WebACLRateCustomKey
		wantErr string
	}{
		{
			name: "empty cookie name",
			input: WebACLRateCustomKey{Cookie: &WebACLRateNamedKey{
				TextTransformations: rateTransformations(),
			}},
			wantErr: "cookie name must be 1..64 characters",
		},
		{
			name: "long header name",
			input: WebACLRateCustomKey{Header: &WebACLRateNamedKey{
				Name:                longName,
				TextTransformations: rateTransformations(),
			}},
			wantErr: "header name must be 1..64 characters",
		},
		{
			name: "empty query argument name",
			input: WebACLRateCustomKey{QueryArgument: &WebACLRateNamedKey{
				TextTransformations: rateTransformations(),
			}},
			wantErr: "query-argument name must be 1..64 characters",
		},
		{
			name: "cookie without transformations",
			input: WebACLRateCustomKey{Cookie: &WebACLRateNamedKey{
				Name: "session",
			}},
			wantErr: "text-transformations must contain at least one member",
		},
		{
			name: "header duplicate priorities",
			input: WebACLRateCustomKey{Header: &WebACLRateNamedKey{
				Name:                "X-Tenant",
				TextTransformations: duplicateTransformations,
			}},
			wantErr: "text-transformations must use distinct priorities",
		},
		{
			name:    "query string without transformations",
			input:   WebACLRateCustomKey{QueryString: &WebACLRateTransformedKey{}},
			wantErr: "text-transformations must contain at least one member",
		},
		{
			name: "URI path duplicate priorities",
			input: WebACLRateCustomKey{URIPath: &WebACLRateTransformedKey{
				TextTransformations: duplicateTransformations,
			}},
			wantErr: "text-transformations must use distinct priorities",
		},
		{
			name: "invalid JA3 fallback",
			input: WebACLRateCustomKey{
				JA3Fingerprint: &WebACLJAFingerprint{FallbackBehavior: "SKIP"},
			},
			wantErr: "JA3 fallback-behavior must be MATCH or NO_MATCH",
		},
		{
			name: "invalid JA4 fallback",
			input: WebACLRateCustomKey{
				JA4Fingerprint: &WebACLJAFingerprint{FallbackBehavior: "SKIP"},
			},
			wantErr: "JA4 fallback-behavior must be MATCH or NO_MATCH",
		},
		{
			name: "empty label namespace",
			input: WebACLRateCustomKey{
				LabelNamespace: &WebACLRateLabelNamespace{},
			},
			wantErr: "label-namespace must be 1..1024",
		},
		{
			name: "long label namespace",
			input: WebACLRateCustomKey{
				LabelNamespace: &WebACLRateLabelNamespace{Namespace: longNamespace},
			},
			wantErr: "label-namespace must be 1..1024",
		},
		{
			name: "invalid label namespace characters",
			input: WebACLRateCustomKey{
				LabelNamespace: &WebACLRateLabelNamespace{Namespace: "bad/value"},
			},
			wantErr: "label-namespace must match ^[0-9A-Za-z_:-]+$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateRateCustomKey(tt.input), tt.wantErr)
		})
	}
}

func TestValidateRateCustomKeyBoundaries(t *testing.T) {
	for _, key := range []WebACLRateCustomKey{
		{Cookie: &WebACLRateNamedKey{
			Name:                strings.Repeat("c", 64),
			TextTransformations: rateTransformations(),
		}},
		{Header: &WebACLRateNamedKey{
			Name:                strings.Repeat("h", 64),
			TextTransformations: rateTransformations(),
		}},
		{QueryArgument: &WebACLRateNamedKey{
			Name:                strings.Repeat("q", 64),
			TextTransformations: rateTransformations(),
		}},
		{LabelNamespace: &WebACLRateLabelNamespace{
			Namespace: strings.Repeat("n", 1024),
		}},
	} {
		require.NoError(t, validateRateCustomKey(key))
	}
}

func TestValidateRateBasedStatementCustomKeyDependencies(t *testing.T) {
	tests := []struct {
		name    string
		input   *WebACLRateBasedStatement
		wantErr string
	}{
		{
			name:    "custom keys omitted",
			input:   validRateBasedStatement("CUSTOM_KEYS"),
			wantErr: "CUSTOM_KEYS requires 1..5 custom-keys",
		},
		{
			name:    "custom keys empty",
			input:   rateBasedWithKeys("CUSTOM_KEYS"),
			wantErr: "CUSTOM_KEYS requires 1..5 custom-keys",
		},
		{
			name: "custom keys above maximum",
			input: rateBasedWithKeys(
				"CUSTOM_KEYS",
				rateHTTPMethodKey(),
				rateHTTPMethodKey(),
				rateHTTPMethodKey(),
				rateHTTPMethodKey(),
				rateHTTPMethodKey(),
				rateHTTPMethodKey(),
			),
			wantErr: "CUSTOM_KEYS requires 1..5 custom-keys",
		},
		{
			name:    "single IP key",
			input:   rateBasedWithKeys("CUSTOM_KEYS", rateIPKey()),
			wantErr: "IP or forwarded-IP custom key requires at least one additional key",
		},
		{
			name: "single forwarded IP key",
			input: func() *WebACLRateBasedStatement {
				in := rateBasedWithKeys("CUSTOM_KEYS", rateForwardedIPKey())
				in.ForwardedIPConfig = validRateForwardedIPConfig()
				return in
			}(),
			wantErr: "IP or forwarded-IP custom key requires at least one additional key",
		},
		{
			name: "forwarded IP key without config",
			input: rateBasedWithKeys(
				"CUSTOM_KEYS",
				rateForwardedIPKey(),
				rateHTTPMethodKey(),
			),
			wantErr: "forwarded-IP custom key requires forwarded-ip-config",
		},
		{
			name:    "IP aggregate with keys",
			input:   rateBasedWithKeys("IP", rateHTTPMethodKey()),
			wantErr: "IP must not specify custom-keys",
		},
		{
			name: "forwarded IP aggregate with keys",
			input: func() *WebACLRateBasedStatement {
				in := rateBasedWithKeys("FORWARDED_IP", rateHTTPMethodKey())
				in.ForwardedIPConfig = validRateForwardedIPConfig()
				return in
			}(),
			wantErr: "FORWARDED_IP must not specify custom-keys",
		},
		{
			name: "constant aggregate with keys",
			input: func() *WebACLRateBasedStatement {
				in := rateBasedWithKeys("CONSTANT", rateHTTPMethodKey())
				in.ScopeDownStatement = rateScopeDown()
				return in
			}(),
			wantErr: "CONSTANT must not specify custom-keys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateRateBasedStatement(tt.input), tt.wantErr)
		})
	}
}

func TestValidateRateBasedStatementCustomKeyValidMatrix(t *testing.T) {
	tests := []struct {
		name  string
		input *WebACLRateBasedStatement
	}{
		{
			name:  "single non-IP key",
			input: rateBasedWithKeys("CUSTOM_KEYS", rateHTTPMethodKey()),
		},
		{
			name: "IP with additional key",
			input: rateBasedWithKeys(
				"CUSTOM_KEYS",
				rateIPKey(),
				rateHTTPMethodKey(),
			),
		},
		{
			name: "forwarded IP with config and additional key",
			input: func() *WebACLRateBasedStatement {
				in := rateBasedWithKeys(
					"CUSTOM_KEYS",
					rateForwardedIPKey(),
					rateHTTPMethodKey(),
				)
				in.ForwardedIPConfig = validRateForwardedIPConfig()
				return in
			}(),
		},
		{
			name: "forwarded config without forwarded key",
			input: func() *WebACLRateBasedStatement {
				in := rateBasedWithKeys("CUSTOM_KEYS", rateHTTPMethodKey())
				in.ForwardedIPConfig = validRateForwardedIPConfig()
				return in
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, validateRateBasedStatement(tt.input))
		})
	}
}

func rateTransformations() []WebACLTextTransformation {
	return []WebACLTextTransformation{{Priority: 0, Type: "NONE"}}
}

func sdkRateTransformations() []awstypes.TextTransformation {
	return []awstypes.TextTransformation{{
		Priority: 0,
		Type:     awstypes.TextTransformationTypeNone,
	}}
}

func rateBasedWithKeys(
	aggregateKeyType string,
	keys ...WebACLRateCustomKey,
) *WebACLRateBasedStatement {
	in := validRateBasedStatement(aggregateKeyType)
	in.CustomKeys = &keys
	return in
}

func rateHTTPMethodKey() WebACLRateCustomKey {
	return WebACLRateCustomKey{HTTPMethod: &WebACLEmpty{}}
}

func rateIPKey() WebACLRateCustomKey {
	return WebACLRateCustomKey{IP: &WebACLEmpty{}}
}

func rateForwardedIPKey() WebACLRateCustomKey {
	return WebACLRateCustomKey{ForwardedIP: &WebACLEmpty{}}
}
