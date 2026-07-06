package cloudfront

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCachePolicyDataSourceARN(t *testing.T) {
	tests := []struct {
		name      string
		region    string
		accountID string
		id        string
		want      string
	}{
		{
			name:      "standard partition",
			region:    "us-east-1",
			accountID: "123456789012",
			id:        "policy-id",
			want:      "arn:aws:cloudfront::123456789012:cache-policy/policy-id",
		},
		{
			name:      "china partition",
			region:    "cn-north-1",
			accountID: "210987654321",
			id:        "policy-id",
			want:      "arn:aws-cn:cloudfront::210987654321:cache-policy/policy-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cachePolicyDataARN(tt.region, tt.accountID, tt.id))
		})
	}
}

func TestFlattenCachePolicyParameters(t *testing.T) {
	assert.Nil(t, flattenCachePolicyParameters(nil))

	got := flattenCachePolicyParameters(
		&cloudfronttypes.ParametersInCacheKeyAndForwardedToOrigin{
			CookiesConfig: &cloudfronttypes.CachePolicyCookiesConfig{
				CookieBehavior: cloudfronttypes.CachePolicyCookieBehaviorWhitelist,
				Cookies: &cloudfronttypes.CookieNames{
					Quantity: aws.Int32(99),
					Items:    []string{"session", "theme"},
				},
			},
			EnableAcceptEncodingGzip: aws.Bool(true),
			HeadersConfig: &cloudfronttypes.CachePolicyHeadersConfig{
				HeaderBehavior: cloudfronttypes.CachePolicyHeaderBehaviorWhitelist,
				Headers: &cloudfronttypes.Headers{
					Quantity: aws.Int32(99),
					Items:    []string{"accept-language"},
				},
			},
			QueryStringsConfig: &cloudfronttypes.CachePolicyQueryStringsConfig{
				QueryStringBehavior: cloudfronttypes.CachePolicyQueryStringBehaviorAllExcept,
				QueryStrings: &cloudfronttypes.QueryStringNames{
					Quantity: aws.Int32(99),
					Items:    []string{"debug"},
				},
			},
			EnableAcceptEncodingBrotli: aws.Bool(true),
		})

	require.NotNil(t, got)
	assert.Equal(t, "whitelist", got.CookiesConfig.CookieBehavior)
	assert.Equal(t, []string{"session", "theme"}, got.CookiesConfig.Cookies.Items)
	assert.True(t, got.EnableAcceptEncodingGzip)
	assert.Equal(t, "whitelist", got.HeadersConfig.HeaderBehavior)
	assert.Equal(t, []string{"accept-language"}, got.HeadersConfig.Headers.Items)
	assert.Equal(t, "allExcept", got.QueryStringsConfig.QueryStringBehavior)
	assert.Equal(t, []string{"debug"}, got.QueryStringsConfig.QueryStrings.Items)
	assert.True(t, got.EnableAcceptEncodingBrotli)
}
