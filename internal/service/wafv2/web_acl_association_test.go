package wafv2

import (
	"testing"

	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandAssociationConfigUsesExactResourceKeys(t *testing.T) {
	input := &WebACLAssociationConfig{
		RequestBody: WebACLAssociationRequestBody{
			APIGateway:             associationBodyConfig("KB_16"),
			AppRunnerService:       associationBodyConfig("KB_32"),
			CloudFront:             associationBodyConfig("KB_48"),
			CognitoUserPool:        associationBodyConfig("KB_64"),
			VerifiedAccessInstance: associationBodyConfig("KB_16"),
		},
	}

	require.NoError(t, validateAssociationConfig(input))
	assert.Equal(t, &awstypes.AssociationConfig{
		RequestBody: map[string]awstypes.RequestBodyAssociatedResourceTypeConfig{
			"API_GATEWAY": {
				DefaultSizeInspectionLimit: awstypes.SizeInspectionLimitKb16,
			},
			"APP_RUNNER_SERVICE": {
				DefaultSizeInspectionLimit: awstypes.SizeInspectionLimitKb32,
			},
			"CLOUDFRONT": {
				DefaultSizeInspectionLimit: awstypes.SizeInspectionLimitKb48,
			},
			"COGNITO_USER_POOL": {
				DefaultSizeInspectionLimit: awstypes.SizeInspectionLimitKb64,
			},
			"VERIFIED_ACCESS_INSTANCE": {
				DefaultSizeInspectionLimit: awstypes.SizeInspectionLimitKb16,
			},
		},
	}, expandAssociationConfig(input))
}

func TestAssociationConfigNilAndEmpty(t *testing.T) {
	require.NoError(t, validateAssociationConfig(nil))
	assert.Nil(t, expandAssociationConfig(nil))

	empty := &WebACLAssociationConfig{}
	require.NoError(t, validateAssociationConfig(empty))
	assert.Equal(t, &awstypes.AssociationConfig{
		RequestBody: map[string]awstypes.RequestBodyAssociatedResourceTypeConfig{},
	}, expandAssociationConfig(empty))
}

func TestValidateAssociationConfigRequiresInspectionLimit(t *testing.T) {
	tests := []struct {
		name   string
		assign func(*WebACLAssociationRequestBody, *WebACLAssociationRequestBodyConfig)
	}{
		{
			name: "api-gateway",
			assign: func(body *WebACLAssociationRequestBody, config *WebACLAssociationRequestBodyConfig) {
				body.APIGateway = config
			},
		},
		{
			name: "app-runner-service",
			assign: func(body *WebACLAssociationRequestBody, config *WebACLAssociationRequestBodyConfig) {
				body.AppRunnerService = config
			},
		},
		{
			name: "cloudfront",
			assign: func(body *WebACLAssociationRequestBody, config *WebACLAssociationRequestBodyConfig) {
				body.CloudFront = config
			},
		},
		{
			name: "cognito-user-pool",
			assign: func(body *WebACLAssociationRequestBody, config *WebACLAssociationRequestBodyConfig) {
				body.CognitoUserPool = config
			},
		},
		{
			name: "verified-access-instance",
			assign: func(body *WebACLAssociationRequestBody, config *WebACLAssociationRequestBodyConfig) {
				body.VerifiedAccessInstance = config
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, value := range []string{"", "KB_8", "kb_16"} {
				input := &WebACLAssociationConfig{}
				tt.assign(
					&input.RequestBody,
					&WebACLAssociationRequestBodyConfig{
						DefaultSizeInspectionLimit: value,
					},
				)
				assert.ErrorContains(
					t,
					validateAssociationConfig(input),
					tt.name+" default-size-inspection-limit must be one of "+
						"KB_16, KB_32, KB_48, KB_64",
				)
			}
		})
	}
}

func associationBodyConfig(value string) *WebACLAssociationRequestBodyConfig {
	return &WebACLAssociationRequestBodyConfig{DefaultSizeInspectionLimit: value}
}
