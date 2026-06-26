package cloudfront

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOriginRequestPolicyDataARN(t *testing.T) {
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
			want: "arn:aws:cloudfront::123456789012:" +
				"origin-request-policy/policy-id",
		},
		{
			name:      "china partition",
			region:    "cn-north-1",
			accountID: "210987654321",
			id:        "policy-id",
			want: "arn:aws-cn:cloudfront::210987654321:" +
				"origin-request-policy/policy-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want,
				originRequestPolicyDataARN(tt.region, tt.accountID, tt.id))
		})
	}
}

func TestFlattenOriginRequestPolicyConfigs(t *testing.T) {
	assert.Nil(t, flattenOriginRequestPolicyCookiesConfig(nil))
	assert.Nil(t, flattenOriginRequestPolicyHeadersConfig(nil))
	assert.Nil(t, flattenOriginRequestPolicyQueryStringsConfig(nil))

	emptyCookies := flattenOriginRequestPolicyCookiesConfig(
		&cloudfronttypes.OriginRequestPolicyCookiesConfig{
			CookieBehavior: cloudfronttypes.OriginRequestPolicyCookieBehaviorWhitelist,
			Cookies:        &cloudfronttypes.CookieNames{},
		})
	require.NotNil(t, emptyCookies)
	assert.Equal(t, "whitelist", emptyCookies.CookieBehavior)
	assert.Nil(t, emptyCookies.Cookies)

	emptyHeaders := flattenOriginRequestPolicyHeadersConfig(
		&cloudfronttypes.OriginRequestPolicyHeadersConfig{
			HeaderBehavior: cloudfronttypes.OriginRequestPolicyHeaderBehaviorWhitelist,
			Headers:        &cloudfronttypes.Headers{Items: []string{}},
		})
	require.NotNil(t, emptyHeaders)
	assert.Equal(t, "whitelist", emptyHeaders.HeaderBehavior)
	assert.Nil(t, emptyHeaders.Headers)

	emptyQueryStrings := flattenOriginRequestPolicyQueryStringsConfig(
		&cloudfronttypes.OriginRequestPolicyQueryStringsConfig{
			QueryStringBehavior: cloudfronttypes.OriginRequestPolicyQueryStringBehaviorAllExcept,
			QueryStrings:        &cloudfronttypes.QueryStringNames{},
		})
	require.NotNil(t, emptyQueryStrings)
	assert.Equal(t, "allExcept", emptyQueryStrings.QueryStringBehavior)
	assert.Nil(t, emptyQueryStrings.QueryStrings)

	cookies := flattenOriginRequestPolicyCookiesConfig(
		&cloudfronttypes.OriginRequestPolicyCookiesConfig{
			CookieBehavior: cloudfronttypes.OriginRequestPolicyCookieBehaviorWhitelist,
			Cookies: &cloudfronttypes.CookieNames{
				Quantity: aws.Int32(99),
				Items:    []string{"theme", "session", "theme"},
			},
		})
	require.NotNil(t, cookies)
	assert.Equal(t, "whitelist", cookies.CookieBehavior)
	assert.Equal(t, []string{"session", "theme"}, cookies.Cookies.Items)

	headers := flattenOriginRequestPolicyHeadersConfig(
		&cloudfronttypes.OriginRequestPolicyHeadersConfig{
			HeaderBehavior: cloudfronttypes.OriginRequestPolicyHeaderBehaviorWhitelist,
			Headers: &cloudfronttypes.Headers{
				Quantity: aws.Int32(99),
				Items:    []string{"x-beta", "x-alpha", "x-beta"},
			},
		})
	require.NotNil(t, headers)
	assert.Equal(t, "whitelist", headers.HeaderBehavior)
	assert.Equal(t, []string{"x-alpha", "x-beta"}, headers.Headers.Items)

	queryStrings := flattenOriginRequestPolicyQueryStringsConfig(
		&cloudfronttypes.OriginRequestPolicyQueryStringsConfig{
			QueryStringBehavior: cloudfronttypes.OriginRequestPolicyQueryStringBehaviorAllExcept,
			QueryStrings: &cloudfronttypes.QueryStringNames{
				Quantity: aws.Int32(99),
				Items:    []string{"z", "a", "z"},
			},
		})
	require.NotNil(t, queryStrings)
	assert.Equal(t, "allExcept", queryStrings.QueryStringBehavior)
	assert.Equal(t, []string{"a", "z"}, queryStrings.QueryStrings.Items)
}

func TestOriginRequestPolicyDataOutputUsesResolvedIDFallback(t *testing.T) {
	out := originRequestPolicyDataOutput("us-east-1", "123456789012", "resolved-id", "etag",
		&cloudfronttypes.OriginRequestPolicy{
			OriginRequestPolicyConfig: &cloudfronttypes.OriginRequestPolicyConfig{
				Name: aws.String("policy-name"),
			},
		})

	require.NotNil(t, out)
	assert.Equal(t, "resolved-id", out.Id)
	assert.Equal(t, "policy-name", out.Name)
	assert.Equal(t, "etag", out.ETag)
}
