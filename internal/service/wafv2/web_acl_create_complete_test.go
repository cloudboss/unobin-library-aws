package wafv2

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLCreateInputIncludesEveryWritableField(t *testing.T) {
	application := &WebACLApplicationConfig{
		Attributes: []WebACLApplicationAttribute{
			{Name: "environment", Values: []string{"production"}},
		},
	}
	association := &WebACLAssociationConfig{
		RequestBody: WebACLAssociationRequestBody{
			APIGateway: associationBodyConfig("KB_32"),
		},
	}
	bodies := map[string]WebACLCustomResponseBody{
		"teapot": {Content: "short and stout", ContentType: "TEXT_PLAIN"},
	}
	description := "complete create request"
	labels := []WebACLRuleLabel{{Name: "result:blocked"}}
	responseHeaders := []WebACLCustomHTTPHeader{
		{Name: "X-Reason", Value: "policy"},
	}
	rules := []WebACLRule{
		{
			Action: &WebACLRuleAction{Block: &WebACLBlockAction{
				CustomResponse: &WebACLCustomResponse{
					ResponseCode:          418,
					CustomResponseBodyKey: aws.String("teapot"),
					ResponseHeaders:       &responseHeaders,
				},
			}},
			Name:       "block-rule",
			Priority:   9,
			RuleLabels: &labels,
			Statement:  ruleLeafLevel3("request:blocked"),
			VisibilityConfig: WebACLVisibilityConfig{
				CloudWatchMetricsEnabled: true,
				MetricName:               "block-rule",
				SampledRequestsEnabled:   false,
			},
		},
	}
	tags := map[string]string{"Environment": "production"}
	tokenDomains := []string{"Shop.Example.COM"}
	resource := &WebACLResource{
		ApplicationConfig: application,
		AssociationConfig: association,
		CaptchaConfig: &WebACLCaptchaConfig{
			ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 60},
		},
		ChallengeConfig: &WebACLChallengeConfig{
			ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 300},
		},
		CustomResponseBodies: &bodies,
		DataProtectionConfig: validDataProtectionConfig(),
		DefaultAction: WebACLDefaultAction{Allow: &WebACLAllowAction{
			CustomRequestHandling: &WebACLCustomRequestHandling{
				InsertHeaders: []WebACLCustomHTTPHeader{
					{Name: "X-Default", Value: "allowed"},
				},
			},
		}},
		Description: &description,
		Name:        aws.String("configured-name"),
		OnSourceDDoSConfig: &WebACLOnSourceDDoSConfig{
			ALBLowReputationMode: "ACTIVE_UNDER_DDOS",
		},
		Rules:        &rules,
		Scope:        "REGIONAL",
		Tags:         &tags,
		TokenDomains: &tokenDomains,
		VisibilityConfig: WebACLVisibilityConfig{
			CloudWatchMetricsEnabled: false,
			MetricName:               "web-acl-metric",
			SampledRequestsEnabled:   true,
		},
	}

	expected := &awssvc.CreateWebACLInput{
		ApplicationConfig: &awstypes.ApplicationConfig{
			Attributes: []awstypes.ApplicationAttribute{
				{Name: aws.String("environment"), Values: []string{"production"}},
			},
		},
		AssociationConfig: &awstypes.AssociationConfig{
			RequestBody: map[string]awstypes.RequestBodyAssociatedResourceTypeConfig{
				"API_GATEWAY": {
					DefaultSizeInspectionLimit: awstypes.SizeInspectionLimitKb32,
				},
			},
		},
		CaptchaConfig: &awstypes.CaptchaConfig{
			ImmunityTimeProperty: &awstypes.ImmunityTimeProperty{
				ImmunityTime: aws.Int64(60),
			},
		},
		ChallengeConfig: &awstypes.ChallengeConfig{
			ImmunityTimeProperty: &awstypes.ImmunityTimeProperty{
				ImmunityTime: aws.Int64(300),
			},
		},
		CustomResponseBodies: map[string]awstypes.CustomResponseBody{
			"teapot": {
				Content:     aws.String("short and stout"),
				ContentType: awstypes.ResponseContentTypeTextPlain,
			},
		},
		DataProtectionConfig: &awstypes.DataProtectionConfig{
			DataProtections: []awstypes.DataProtection{
				{
					Action: awstypes.DataProtectionActionSubstitution,
					Field: &awstypes.FieldToProtect{
						FieldType: awstypes.FieldToProtectTypeSingleHeader,
					},
				},
			},
		},
		DefaultAction: &awstypes.DefaultAction{
			Allow: &awstypes.AllowAction{
				CustomRequestHandling: &awstypes.CustomRequestHandling{
					InsertHeaders: []awstypes.CustomHTTPHeader{
						{Name: aws.String("X-Default"), Value: aws.String("allowed")},
					},
				},
			},
		},
		Description: aws.String("complete create request"),
		Name:        aws.String("resolved-name"),
		OnSourceDDoSProtectionConfig: &awstypes.OnSourceDDoSProtectionConfig{
			ALBLowReputationMode: awstypes.LowReputationModeActiveUnderDdos,
		},
		Rules: []awstypes.Rule{
			{
				Action: &awstypes.RuleAction{Block: &awstypes.BlockAction{
					CustomResponse: &awstypes.CustomResponse{
						ResponseCode:          aws.Int32(418),
						CustomResponseBodyKey: aws.String("teapot"),
						ResponseHeaders: []awstypes.CustomHTTPHeader{
							{Name: aws.String("X-Reason"), Value: aws.String("policy")},
						},
					},
				}},
				Name:     aws.String("block-rule"),
				Priority: 9,
				RuleLabels: []awstypes.Label{
					{Name: aws.String("result:blocked")},
				},
				Statement: &awstypes.Statement{
					LabelMatchStatement: &awstypes.LabelMatchStatement{
						Key:   aws.String("request:blocked"),
						Scope: awstypes.LabelMatchScopeLabel,
					},
				},
				VisibilityConfig: &awstypes.VisibilityConfig{
					CloudWatchMetricsEnabled: true,
					MetricName:               aws.String("block-rule"),
					SampledRequestsEnabled:   false,
				},
			},
		},
		Scope: awstypes.ScopeRegional,
		Tags: []awstypes.Tag{
			{Key: aws.String("Environment"), Value: aws.String("production")},
		},
		TokenDomains: []string{"Shop.Example.COM"},
		VisibilityConfig: &awstypes.VisibilityConfig{
			CloudWatchMetricsEnabled: false,
			MetricName:               aws.String("web-acl-metric"),
			SampledRequestsEnabled:   true,
		},
	}

	actual, err := resource.createInput("resolved-name")
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}
