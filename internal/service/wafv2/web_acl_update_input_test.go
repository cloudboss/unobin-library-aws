package wafv2

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLUpdateInputIncludesEveryMutableField(t *testing.T) {
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
		"denied": {Content: "denied", ContentType: "TEXT_PLAIN"},
	}
	description := "complete update request"
	rules := []WebACLRule{{
		Action: &WebACLRuleAction{Block: &WebACLBlockAction{
			CustomResponse: &WebACLCustomResponse{
				ResponseCode:          403,
				CustomResponseBodyKey: aws.String("denied"),
			},
		}},
		Name:      "block-rule",
		Priority:  4,
		Statement: ruleLeafLevel3("request:blocked"),
		VisibilityConfig: WebACLVisibilityConfig{
			CloudWatchMetricsEnabled: true,
			MetricName:               "block-rule",
			SampledRequestsEnabled:   false,
		},
	}}
	tags := map[string]string{"ignored": "tag"}
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
		Description: aws.String(description),
		Name:        aws.String("desired-name-is-not-the-identity"),
		OnSourceDDoSConfig: &WebACLOnSourceDDoSConfig{
			ALBLowReputationMode: "ACTIVE_UNDER_DDOS",
		},
		Rules:        &rules,
		Scope:        "CLOUDFRONT",
		Tags:         &tags,
		TokenDomains: &tokenDomains,
		VisibilityConfig: WebACLVisibilityConfig{
			CloudWatchMetricsEnabled: false,
			MetricName:               "web-acl-metric",
			SampledRequestsEnabled:   true,
		},
	}
	prior := &WebACLResourceOutput{
		ARN:       testWebACLARN,
		ID:        "ignored-id",
		LockToken: "token-1",
	}

	expected := &awssvc.UpdateWebACLInput{
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
			"denied": {
				Content:     aws.String("denied"),
				ContentType: awstypes.ResponseContentTypeTextPlain,
			},
		},
		DataProtectionConfig: &awstypes.DataProtectionConfig{
			DataProtections: []awstypes.DataProtection{{
				Action: awstypes.DataProtectionActionSubstitution,
				Field: &awstypes.FieldToProtect{
					FieldType: awstypes.FieldToProtectTypeSingleHeader,
				},
			}},
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
		Description: aws.String(description),
		Id:          aws.String("id-1"),
		LockToken:   aws.String("token-1"),
		Name:        aws.String("example"),
		OnSourceDDoSProtectionConfig: &awstypes.OnSourceDDoSProtectionConfig{
			ALBLowReputationMode: awstypes.LowReputationModeActiveUnderDdos,
		},
		Rules: []awstypes.Rule{{
			Action: &awstypes.RuleAction{Block: &awstypes.BlockAction{
				CustomResponse: &awstypes.CustomResponse{
					ResponseCode:          aws.Int32(403),
					CustomResponseBodyKey: aws.String("denied"),
				},
			}},
			Name:     aws.String("block-rule"),
			Priority: 4,
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
		}},
		Scope:        awstypes.ScopeRegional,
		TokenDomains: []string{"Shop.Example.COM"},
		VisibilityConfig: &awstypes.VisibilityConfig{
			CloudWatchMetricsEnabled: false,
			MetricName:               aws.String("web-acl-metric"),
			SampledRequestsEnabled:   true,
		},
	}

	actual, err := resource.updateInput(prior)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
	assert.Nil(t, actual.ApplicationConfig)
	assert.Nil(t, actual.MonetizationConfig)
}

func TestWebACLUpdateInputOptionalRemovals(t *testing.T) {
	t.Run("nil source values", func(t *testing.T) {
		resource := validWebACLResource()
		actual, err := resource.updateInput(validWebACLUpdatePriorOutput())
		require.NoError(t, err)
		assert.Equal(t, requiredWebACLUpdateInput(), actual)
	})

	t.Run("explicit empty source collections", func(t *testing.T) {
		emptyBodies := map[string]WebACLCustomResponseBody{}
		emptyRules := []WebACLRule{}
		emptyTokenDomains := []string{}
		resource := validWebACLResource()
		resource.AssociationConfig = &WebACLAssociationConfig{}
		resource.CustomResponseBodies = &emptyBodies
		resource.Rules = &emptyRules
		resource.TokenDomains = &emptyTokenDomains

		expected := requiredWebACLUpdateInput()
		expected.AssociationConfig = &awstypes.AssociationConfig{
			RequestBody: map[string]awstypes.RequestBodyAssociatedResourceTypeConfig{},
		}
		expected.CustomResponseBodies = map[string]awstypes.CustomResponseBody{}
		expected.Rules = []awstypes.Rule{}
		expected.TokenDomains = []string{}

		actual, err := resource.updateInput(validWebACLUpdatePriorOutput())
		require.NoError(t, err)
		assert.Equal(t, expected, actual)
		assert.NotNil(t, actual.AssociationConfig.RequestBody)
		assert.NotNil(t, actual.CustomResponseBodies)
		assert.NotNil(t, actual.Rules)
		assert.NotNil(t, actual.TokenDomains)
	})

	t.Run("empty captcha and challenge configs", func(t *testing.T) {
		resource := validWebACLResource()
		resource.CaptchaConfig = &WebACLCaptchaConfig{}
		resource.ChallengeConfig = &WebACLChallengeConfig{}

		expected := requiredWebACLUpdateInput()
		expected.CaptchaConfig = &awstypes.CaptchaConfig{}
		expected.ChallengeConfig = &awstypes.ChallengeConfig{}

		actual, err := resource.updateInput(validWebACLUpdatePriorOutput())
		require.NoError(t, err)
		assert.Equal(t, expected, actual)
	})
}

func TestWebACLUpdateInputRequiresPriorIdentityAndLockToken(t *testing.T) {
	tests := []struct {
		name    string
		prior   *WebACLResourceOutput
		wantErr string
	}{
		{name: "nil prior", wantErr: "prior web ACL output has no ARN"},
		{
			name:    "empty ARN",
			prior:   &WebACLResourceOutput{LockToken: "token-1"},
			wantErr: "prior web ACL output has no ARN",
		},
		{
			name:    "invalid ARN",
			prior:   &WebACLResourceOutput{ARN: "invalid", LockToken: "token-1"},
			wantErr: "parse web ACL ARN",
		},
		{
			name:    "empty lock token",
			prior:   &WebACLResourceOutput{ARN: testWebACLARN},
			wantErr: "prior web ACL output has no lock-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := validWebACLResource().updateInput(tt.prior)
			assert.Nil(t, actual)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestWebACLUpdateInputValidatesDesiredValues(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*WebACLResource)
		wantErr string
	}{
		{
			name: "top-level input",
			mutate: func(resource *WebACLResource) {
				resource.Scope = "INVALID"
			},
			wantErr: "scope must be REGIONAL or CLOUDFRONT",
		},
		{
			name: "captcha converter",
			mutate: func(resource *WebACLResource) {
				resource.CaptchaConfig = &WebACLCaptchaConfig{
					ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 59},
				}
			},
			wantErr: "captcha immunity-time must be 60..259200",
		},
		{
			name: "challenge converter",
			mutate: func(resource *WebACLResource) {
				resource.ChallengeConfig = &WebACLChallengeConfig{
					ImmunityTimeProperty: &WebACLImmunityTimeProperty{ImmunityTime: 299},
				}
			},
			wantErr: "challenge immunity-time must be 300..259200",
		},
		{
			name: "default action converter",
			mutate: func(resource *WebACLResource) {
				resource.DefaultAction.Allow.CustomRequestHandling =
					&WebACLCustomRequestHandling{}
			},
			wantErr: "insert-headers must contain at least one member",
		},
		{
			name: "rules converter",
			mutate: func(resource *WebACLResource) {
				rules := []WebACLRule{{}}
				resource.Rules = &rules
			},
			wantErr: "rule item 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validWebACLResource()
			tt.mutate(resource)
			actual, err := resource.updateInput(validWebACLUpdatePriorOutput())
			assert.Nil(t, actual)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func requiredWebACLUpdateInput() *awssvc.UpdateWebACLInput {
	return &awssvc.UpdateWebACLInput{
		DefaultAction: &awstypes.DefaultAction{Allow: &awstypes.AllowAction{}},
		Id:            aws.String("id-1"),
		LockToken:     aws.String("token-1"),
		Name:          aws.String("example"),
		Scope:         awstypes.ScopeRegional,
		VisibilityConfig: &awstypes.VisibilityConfig{
			CloudWatchMetricsEnabled: true,
			MetricName:               aws.String("example"),
			SampledRequestsEnabled:   true,
		},
	}
}

func validWebACLUpdatePriorOutput() *WebACLResourceOutput {
	return &WebACLResourceOutput{
		ARN:       testWebACLARN,
		ID:        "ignored-id",
		LockToken: "token-1",
	}
}
