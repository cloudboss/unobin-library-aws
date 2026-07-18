package wafv2

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLCreateInputIncludesCompositeConfigs(t *testing.T) {
	resource := validWebACLResource()
	resource.ApplicationConfig = &WebACLApplicationConfig{
		Attributes: []WebACLApplicationAttribute{
			{Name: "environment", Values: []string{"production"}},
		},
	}
	resource.AssociationConfig = &WebACLAssociationConfig{
		RequestBody: WebACLAssociationRequestBody{
			APIGateway: associationBodyConfig("KB_32"),
		},
	}
	bodies := map[string]WebACLCustomResponseBody{
		"denied": {Content: "denied", ContentType: "TEXT_PLAIN"},
	}
	resource.CustomResponseBodies = &bodies

	actual, err := resource.createInput("example")
	require.NoError(t, err)
	assert.Equal(t, &awssvc.CreateWebACLInput{
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
		CustomResponseBodies: map[string]awstypes.CustomResponseBody{
			"denied": {
				Content:     aws.String("denied"),
				ContentType: awstypes.ResponseContentTypeTextPlain,
			},
		},
		DefaultAction: &awstypes.DefaultAction{Allow: &awstypes.AllowAction{}},
		Name:          aws.String("example"),
		Scope:         awstypes.ScopeRegional,
		VisibilityConfig: &awstypes.VisibilityConfig{
			CloudWatchMetricsEnabled: true,
			MetricName:               aws.String("example"),
			SampledRequestsEnabled:   true,
		},
	}, actual)
}

func TestWebACLValidateInputsIncludesCompositeConfigs(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*WebACLResource)
		wantErr string
	}{
		{
			name: "application config",
			mutate: func(resource *WebACLResource) {
				resource.ApplicationConfig = &WebACLApplicationConfig{}
			},
			wantErr: "attributes must contain 1..10 items",
		},
		{
			name: "association config",
			mutate: func(resource *WebACLResource) {
				resource.AssociationConfig = &WebACLAssociationConfig{
					RequestBody: WebACLAssociationRequestBody{
						CloudFront: associationBodyConfig("KB_8"),
					},
				}
			},
			wantErr: "cloudfront default-size-inspection-limit",
		},
		{
			name: "custom response bodies",
			mutate: func(resource *WebACLResource) {
				bodies := map[string]WebACLCustomResponseBody{
					"body": {Content: "", ContentType: "TEXT_PLAIN"},
				}
				resource.CustomResponseBodies = &bodies
			},
			wantErr: "custom response body content must be 1..10240 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validWebACLResource()
			tt.mutate(resource)
			assert.ErrorContains(
				t,
				resource.ValidateInputs(context.Background(), nil),
				tt.wantErr,
			)
		})
	}
}
