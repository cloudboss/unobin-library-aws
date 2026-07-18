package wafv2

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandVisibilityConfigPreservesBooleans(t *testing.T) {
	tests := []struct {
		name   string
		input  WebACLVisibilityConfig
		expect *awstypes.VisibilityConfig
	}{
		{
			name: "metrics false sampling true",
			input: WebACLVisibilityConfig{
				CloudWatchMetricsEnabled: false,
				MetricName:               "first-metric",
				SampledRequestsEnabled:   true,
			},
			expect: &awstypes.VisibilityConfig{
				CloudWatchMetricsEnabled: false,
				MetricName:               aws.String("first-metric"),
				SampledRequestsEnabled:   true,
			},
		},
		{
			name: "metrics true sampling false",
			input: WebACLVisibilityConfig{
				CloudWatchMetricsEnabled: true,
				MetricName:               "second_metric",
				SampledRequestsEnabled:   false,
			},
			expect: &awstypes.VisibilityConfig{
				CloudWatchMetricsEnabled: true,
				MetricName:               aws.String("second_metric"),
				SampledRequestsEnabled:   false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, validateVisibilityConfig(tt.input))
			assert.Equal(t, tt.expect, expandVisibilityConfig(tt.input))
		})
	}
}

func TestValidateVisibilityConfig(t *testing.T) {
	tests := []struct {
		name    string
		metric  string
		wantErr string
	}{
		{name: "empty", wantErr: "metric-name must be 1..128 characters"},
		{
			name:    "long",
			metric:  strings.Repeat("m", 129),
			wantErr: "metric-name must be 1..128 characters",
		},
		{
			name:    "invalid characters",
			metric:  "bad metric",
			wantErr: "metric-name must match ^[0-9A-Za-z_-]+$",
		},
		{name: "reserved All", metric: "All", wantErr: "metric-name is reserved"},
		{
			name:    "reserved Default_Action",
			metric:  "Default_Action",
			wantErr: "metric-name is reserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := WebACLVisibilityConfig{MetricName: tt.metric}
			assert.ErrorContains(t, validateVisibilityConfig(config), tt.wantErr)
		})
	}
}

func TestValidateVisibilityConfigBoundaries(t *testing.T) {
	for _, metric := range []string{"x", strings.Repeat("m", 128)} {
		require.NoError(t, validateVisibilityConfig(WebACLVisibilityConfig{
			MetricName: metric,
		}))
	}
}

func TestExpandCaptchaAndChallengeConfigs(t *testing.T) {
	captcha := &WebACLCaptchaConfig{
		ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 60},
	}
	challenge := &WebACLChallengeConfig{
		ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 300},
	}

	actualCaptcha, err := expandCaptchaConfig(captcha)
	require.NoError(t, err)
	assert.Equal(t, &awstypes.CaptchaConfig{
		ImmunityTimeProperty: &awstypes.ImmunityTimeProperty{
			ImmunityTime: aws.Int64(60),
		},
	}, actualCaptcha)

	actualChallenge, err := expandChallengeConfig(challenge)
	require.NoError(t, err)
	assert.Equal(t, &awstypes.ChallengeConfig{
		ImmunityTimeProperty: &awstypes.ImmunityTimeProperty{
			ImmunityTime: aws.Int64(300),
		},
	}, actualChallenge)
}

func TestExpandCaptchaAndChallengeConfigsOmitNil(t *testing.T) {
	captcha, err := expandCaptchaConfig(nil)
	require.NoError(t, err)
	assert.Nil(t, captcha)

	challenge, err := expandChallengeConfig(nil)
	require.NoError(t, err)
	assert.Nil(t, challenge)
}

func TestExpandCaptchaAndChallengeConfigsPreserveEmpty(t *testing.T) {
	captcha, err := expandCaptchaConfig(&WebACLCaptchaConfig{})
	require.NoError(t, err)
	assert.Equal(t, &awstypes.CaptchaConfig{}, captcha)

	challenge, err := expandChallengeConfig(&WebACLChallengeConfig{})
	require.NoError(t, err)
	assert.Equal(t, &awstypes.ChallengeConfig{}, challenge)
}

func TestValidateCaptchaConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   *WebACLCaptchaConfig
		wantErr string
	}{
		{
			name: "below minimum",
			input: &WebACLCaptchaConfig{
				ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 59},
			},
			wantErr: "captcha immunity-time must be 60..259200",
		},
		{
			name: "above maximum",
			input: &WebACLCaptchaConfig{
				ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 259201},
			},
			wantErr: "captcha immunity-time must be 60..259200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateCaptchaConfig(tt.input), tt.wantErr)
		})
	}
	for _, value := range []int64{60, 259200} {
		require.NoError(t, validateCaptchaConfig(&WebACLCaptchaConfig{
			ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: value},
		}))
	}
}

func TestValidateChallengeConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   *WebACLChallengeConfig
		wantErr string
	}{
		{
			name: "below minimum",
			input: &WebACLChallengeConfig{
				ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 299},
			},
			wantErr: "challenge immunity-time must be 300..259200",
		},
		{
			name: "above maximum",
			input: &WebACLChallengeConfig{
				ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 259201},
			},
			wantErr: "challenge immunity-time must be 300..259200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateChallengeConfig(tt.input), tt.wantErr)
		})
	}
	for _, value := range []int64{300, 259200} {
		require.NoError(t, validateChallengeConfig(&WebACLChallengeConfig{
			ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: value},
		}))
	}
}

func TestValidateCaptchaAndChallengeConfigsAllowNilOrEmpty(t *testing.T) {
	require.NoError(t, validateCaptchaConfig(nil))
	require.NoError(t, validateCaptchaConfig(&WebACLCaptchaConfig{}))
	require.NoError(t, validateChallengeConfig(nil))
	require.NoError(t, validateChallengeConfig(&WebACLChallengeConfig{}))
}
