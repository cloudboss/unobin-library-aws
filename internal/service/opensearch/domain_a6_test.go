package opensearch

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainValidationRejectsMalformedARNFields(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*DomainResource)
		wantField string
	}{
		{
			name: "master user",
			configure: func(resource *DomainResource) {
				resource.AdvancedSecurityOptions = &DomainAdvancedSecurityOptions{
					Enabled: true,
					MasterUserOptions: &DomainMasterUserOptions{
						MasterUserARN: stringPointer("not-an-arn"),
					},
				}
			},
			wantField: "master-user-arn",
		},
		{
			name: "Cognito role",
			configure: func(resource *DomainResource) {
				resource.CognitoOptions = &DomainCognitoOptions{
					Enabled: true, IdentityPoolID: "identity", RoleARN: "not-an-arn",
					UserPoolID: "user",
				}
			},
			wantField: "role-arn",
		},
		{
			name: "custom certificate",
			configure: func(resource *DomainResource) {
				resource.DomainEndpointOptions = &DomainEndpointOptions{
					CustomEndpointEnabled:        true,
					CustomEndpointCertificateARN: stringPointer("not-an-arn"),
				}
			},
			wantField: "custom-endpoint-certificate-arn",
		},
		{
			name: "Identity Center instance",
			configure: func(resource *DomainResource) {
				resource.IdentityCenterOptions = &DomainIdentityCenterOptions{
					EnabledAPIAccess:          aws.Bool(true),
					IdentityCenterInstanceARN: stringPointer("not-an-arn"),
				}
			},
			wantField: "identity-center-instance-arn",
		},
		{
			name: "log group",
			configure: func(resource *DomainResource) {
				resource.LogPublishingOptions = &[]DomainLogPublishingOption{{
					CloudWatchLogGroupARN: "not-an-arn", LogType: "INDEX_SLOW_LOGS",
				}}
			},
			wantField: "cloudwatch-log-group-arn",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := DomainResource{DomainName: "example"}
			test.configure(&resource)

			err := resource.ValidateInputs(context.Background(), nil)

			require.Error(t, err)
			assert.ErrorContains(t, err, test.wantField)
		})
	}
}

func TestDomainValidationAcceptsGenericARNForms(t *testing.T) {
	resource := DomainResource{
		DomainName: "example",
		AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
			Enabled: true,
			MasterUserOptions: &DomainMasterUserOptions{
				MasterUserARN: stringPointer(
					"arn:aws:iam::123456789012:role/master",
				),
			},
		},
		CognitoOptions: &DomainCognitoOptions{
			Enabled: true, IdentityPoolID: "identity",
			RoleARN: "arn:aws:iam::123456789012:role/cognito", UserPoolID: "user",
		},
		DomainEndpointOptions: &DomainEndpointOptions{
			CustomEndpointEnabled: true,
			CustomEndpointCertificateARN: stringPointer(
				"arn:aws:acm:us-east-1:123456789012:certificate/example",
			),
		},
		IdentityCenterOptions: &DomainIdentityCenterOptions{
			EnabledAPIAccess: aws.Bool(true),
			IdentityCenterInstanceARN: stringPointer(
				"arn:aws:sso:::instance/ssoins-1234567890123456",
			),
		},
		LogPublishingOptions: &[]DomainLogPublishingOption{{
			CloudWatchLogGroupARN: "arn:aws:logs:us-east-1:123456789012:log-group:test",
			LogType:               "INDEX_SLOW_LOGS",
		}},
	}

	err := resource.ValidateInputs(context.Background(), nil)

	assert.NoError(t, err)
}

func TestDomainSAMLMetadataValidationBoundary(t *testing.T) {
	validate := func(length int) error {
		resource := DomainResource{
			DomainName: "example",
			AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
				Enabled: true,
				SAMLOptions: &DomainSAMLOptions{
					Enabled: true,
					IDP: &DomainSAMLIDP{
						EntityID: "entity", MetadataContent: strings.Repeat("x", length),
					},
					SessionTimeoutMinutes: 60,
				},
			},
		}
		return resource.ValidateInputs(context.Background(), nil)
	}

	assert.NoError(t, validate(1_048_576))
	assert.ErrorContains(t, validate(1_048_577), "metadata-content")
}

func TestDomainTagValidationBoundaries(t *testing.T) {
	tags := make(map[string]string, 10)
	for index := range 10 {
		tags[fmt.Sprintf("key-%d", index)] = "value"
	}
	tests := []struct {
		name      string
		tags      map[string]string
		wantError string
	}{
		{name: "ten tags", tags: tags},
		{
			name: "eleven tags",
			tags: func() map[string]string {
				result := copyStringMap(&tags)
				result["extra"] = "value"
				return result
			}(),
			wantError: "at most 10",
		},
		{name: "empty key", tags: map[string]string{"": "value"}},
		{name: "key maximum", tags: map[string]string{strings.Repeat("k", 128): "value"}},
		{
			name: "key too long", tags: map[string]string{strings.Repeat("k", 129): "value"},
			wantError: "tag key",
		},
		{name: "value maximum", tags: map[string]string{"key": strings.Repeat("v", 256)}},
		{
			name: "value too long", tags: map[string]string{"key": strings.Repeat("v", 257)},
			wantError: "tag value",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := DomainResource{DomainName: "example", Tags: &test.tags}

			err := resource.ValidateInputs(context.Background(), nil)

			if test.wantError == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestDomainCreateRejectsMalformedARNBeforeClientCalls(t *testing.T) {
	called := false
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			called = true
			return &awssdk.DescribeDomainOutput{}, nil
		},
	}
	resource := DomainResource{
		DomainName: "example",
		LogPublishingOptions: &[]DomainLogPublishingOption{{
			CloudWatchLogGroupARN: "not-an-arn", LogType: "INDEX_SLOW_LOGS",
		}},
	}

	output, err := resource.create(
		context.Background(), client, shortDomainOptions(newRecordingDomainClock()),
	)

	require.ErrorContains(t, err, "cloudwatch-log-group-arn")
	assert.Nil(t, output)
	assert.False(t, called)
}
