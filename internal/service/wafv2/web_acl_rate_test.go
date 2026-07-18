package wafv2

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandRateBasedStatement(t *testing.T) {
	evaluationWindow := int64(120)
	input := &WebACLRateBasedStatement{
		AggregateKeyType:    "FORWARDED_IP",
		EvaluationWindowSec: &evaluationWindow,
		ForwardedIPConfig: &WebACLForwardedIPConfig{
			FallbackBehavior: "MATCH",
			HeaderName:       "X-Forwarded-For",
		},
		Limit:              500,
		ScopeDownStatement: rateScopeDown(),
	}
	expected := &awstypes.RateBasedStatement{
		AggregateKeyType:    awstypes.RateBasedStatementAggregateKeyTypeForwardedIp,
		EvaluationWindowSec: 120,
		ForwardedIPConfig: &awstypes.ForwardedIPConfig{
			FallbackBehavior: awstypes.FallbackBehaviorMatch,
			HeaderName:       aws.String("X-Forwarded-For"),
		},
		Limit: aws.Int64(500),
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

	root, err := expandStatementLevel3(WebACLStatementLevel3{RateBasedStatement: input})
	require.NoError(t, err)
	assert.Equal(t, &awstypes.Statement{RateBasedStatement: expected}, root)
}

func TestExpandRateBasedStatementPreservesOmittedEvaluationWindow(t *testing.T) {
	input := validRateBasedStatement("IP")
	expected := &awstypes.RateBasedStatement{
		AggregateKeyType: awstypes.RateBasedStatementAggregateKeyTypeIp,
		Limit:            aws.Int64(10),
	}

	actual, err := expandRateBasedStatement(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
	assert.Zero(t, actual.EvaluationWindowSec)
	assert.Nil(t, actual.CustomKeys)
	assert.Nil(t, actual.ForwardedIPConfig)
	assert.Nil(t, actual.ScopeDownStatement)
}

func TestValidateRateBasedStatementBoundsAndEnums(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*WebACLRateBasedStatement)
		wantErr string
	}{
		{
			name: "missing aggregate key type",
			mutate: func(in *WebACLRateBasedStatement) {
				in.AggregateKeyType = ""
			},
			wantErr: "aggregate-key-type must be IP, FORWARDED_IP, CUSTOM_KEYS, or CONSTANT",
		},
		{
			name: "invalid aggregate key type",
			mutate: func(in *WebACLRateBasedStatement) {
				in.AggregateKeyType = "HEADER"
			},
			wantErr: "aggregate-key-type must be IP, FORWARDED_IP, CUSTOM_KEYS, or CONSTANT",
		},
		{
			name: "limit below minimum",
			mutate: func(in *WebACLRateBasedStatement) {
				in.Limit = 9
			},
			wantErr: "limit must be 10..2000000000",
		},
		{
			name: "limit above maximum",
			mutate: func(in *WebACLRateBasedStatement) {
				in.Limit = 2_000_000_001
			},
			wantErr: "limit must be 10..2000000000",
		},
		{
			name: "explicit zero evaluation window",
			mutate: func(in *WebACLRateBasedStatement) {
				in.EvaluationWindowSec = aws.Int64(0)
			},
			wantErr: "evaluation-window-sec must be 60, 120, 300, or 600",
		},
		{
			name: "invalid evaluation window",
			mutate: func(in *WebACLRateBasedStatement) {
				in.EvaluationWindowSec = aws.Int64(240)
			},
			wantErr: "evaluation-window-sec must be 60, 120, 300, or 600",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validRateBasedStatement("IP")
			tt.mutate(input)
			assert.ErrorContains(t, validateRateBasedStatement(input), tt.wantErr)
		})
	}
}

func TestValidateRateBasedStatementBoundaries(t *testing.T) {
	for _, limit := range []int64{10, 2_000_000_000} {
		t.Run("limit", func(t *testing.T) {
			input := validRateBasedStatement("IP")
			input.Limit = limit
			require.NoError(t, validateRateBasedStatement(input))
		})
	}
	for _, window := range []int64{60, 120, 300, 600} {
		t.Run("evaluation window", func(t *testing.T) {
			input := validRateBasedStatement("IP")
			input.EvaluationWindowSec = &window
			require.NoError(t, validateRateBasedStatement(input))
		})
	}
}

func TestValidateRateBasedStatementDependencies(t *testing.T) {
	tests := []struct {
		name    string
		input   *WebACLRateBasedStatement
		wantErr string
	}{
		{
			name:    "forwarded IP without config",
			input:   validRateBasedStatement("FORWARDED_IP"),
			wantErr: "FORWARDED_IP requires forwarded-ip-config",
		},
		{
			name:    "IP with custom keys",
			input:   rateBasedWithCustomKeys("IP"),
			wantErr: "IP must not specify custom-keys",
		},
		{
			name:    "forwarded IP with custom keys",
			input:   rateBasedWithCustomKeys("FORWARDED_IP"),
			wantErr: "FORWARDED_IP must not specify custom-keys",
		},
		{
			name:    "constant without scope down",
			input:   validRateBasedStatement("CONSTANT"),
			wantErr: "CONSTANT requires scope-down-statement",
		},
		{
			name: "constant with custom keys",
			input: func() *WebACLRateBasedStatement {
				in := rateBasedWithCustomKeys("CONSTANT")
				in.ScopeDownStatement = rateScopeDown()
				return in
			}(),
			wantErr: "CONSTANT must not specify custom-keys",
		},
		{
			name:    "custom keys type without keys",
			input:   validRateBasedStatement("CUSTOM_KEYS"),
			wantErr: "CUSTOM_KEYS requires 1..5 custom-keys",
		},
		{
			name:    "custom keys type with keys",
			input:   rateBasedWithCustomKeys("CUSTOM_KEYS"),
			wantErr: "IP or forwarded-IP custom key requires at least one additional key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateRateBasedStatement(tt.input), tt.wantErr)
		})
	}
}

func TestValidateRateBasedStatementScopeAndPermittedFields(t *testing.T) {
	emptyCustomKeys := []WebACLRateCustomKey{}
	tests := []struct {
		name  string
		input *WebACLRateBasedStatement
	}{
		{
			name: "constant with scope down",
			input: func() *WebACLRateBasedStatement {
				in := validRateBasedStatement("CONSTANT")
				in.ScopeDownStatement = rateScopeDown()
				return in
			}(),
		},
		{
			name: "IP with forwarded config",
			input: func() *WebACLRateBasedStatement {
				in := validRateBasedStatement("IP")
				in.ForwardedIPConfig = validRateForwardedIPConfig()
				return in
			}(),
		},
		{
			name: "IP with explicit empty custom keys",
			input: func() *WebACLRateBasedStatement {
				in := validRateBasedStatement("IP")
				in.CustomKeys = &emptyCustomKeys
				return in
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, validateRateBasedStatement(tt.input))
		})
	}

	invalidScope := validRateBasedStatement("IP")
	invalidScope.ScopeDownStatement = &WebACLStatementLevel2{}
	assert.ErrorContains(
		t,
		validateRateBasedStatement(invalidScope),
		"scope-down-statement: level-2 statement must contain exactly one member",
	)
}

func validRateBasedStatement(aggregateKeyType string) *WebACLRateBasedStatement {
	return &WebACLRateBasedStatement{
		AggregateKeyType: aggregateKeyType,
		Limit:            10,
	}
}

func validRateForwardedIPConfig() *WebACLForwardedIPConfig {
	return &WebACLForwardedIPConfig{
		FallbackBehavior: "MATCH",
		HeaderName:       "X-Forwarded-For",
	}
}

func rateScopeDown() *WebACLStatementLevel2 {
	return &WebACLStatementLevel2{
		LabelMatchStatement: &WebACLLabelMatchStatement{
			Key:   "rate-scope",
			Scope: "LABEL",
		},
	}
}

func rateBasedWithCustomKeys(aggregateKeyType string) *WebACLRateBasedStatement {
	in := validRateBasedStatement(aggregateKeyType)
	in.CustomKeys = &[]WebACLRateCustomKey{{IP: &WebACLEmpty{}}}
	if aggregateKeyType == "FORWARDED_IP" {
		in.ForwardedIPConfig = validRateForwardedIPConfig()
	}
	return in
}
