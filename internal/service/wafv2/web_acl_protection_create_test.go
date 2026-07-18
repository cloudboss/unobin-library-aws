package wafv2

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLCreateInputIncludesProtectionConfigs(t *testing.T) {
	fieldKeys := []string{"authorization", "x-api-key"}
	resource := validWebACLResource()
	resource.DataProtectionConfig = &WebACLDataProtectionConfig{
		DataProtections: []WebACLDataProtection{
			{
				Action:                  "HASH",
				ExcludeRateBasedDetails: true,
				Field: WebACLDataProtectionField{
					FieldKeys: &fieldKeys,
					FieldType: "SINGLE_HEADER",
				},
			},
		},
	}
	resource.OnSourceDDoSConfig = &WebACLOnSourceDDoSConfig{
		ALBLowReputationMode: "ALWAYS_ON",
	}
	tokenDomains := []string{"Shop.Example.COM"}
	resource.TokenDomains = &tokenDomains
	resource.Tags = &map[string]string{"Environment": "production"}

	actual, err := resource.createInput("example")
	require.NoError(t, err)
	assert.Equal(t, &awssvc.CreateWebACLInput{
		DataProtectionConfig: &awstypes.DataProtectionConfig{
			DataProtections: []awstypes.DataProtection{
				{
					Action:                  awstypes.DataProtectionActionHash,
					ExcludeRateBasedDetails: true,
					Field: &awstypes.FieldToProtect{
						FieldKeys: []string{"authorization", "x-api-key"},
						FieldType: awstypes.FieldToProtectTypeSingleHeader,
					},
				},
			},
		},
		DefaultAction: &awstypes.DefaultAction{Allow: &awstypes.AllowAction{}},
		Name:          aws.String("example"),
		OnSourceDDoSProtectionConfig: &awstypes.OnSourceDDoSProtectionConfig{
			ALBLowReputationMode: awstypes.LowReputationModeAlwaysOn,
		},
		Scope: awstypes.ScopeRegional,
		Tags: []awstypes.Tag{
			{Key: aws.String("Environment"), Value: aws.String("production")},
		},
		TokenDomains: []string{"Shop.Example.COM"},
		VisibilityConfig: &awstypes.VisibilityConfig{
			CloudWatchMetricsEnabled: true,
			MetricName:               aws.String("example"),
			SampledRequestsEnabled:   true,
		},
	}, actual)
}
