package apigatewayv2

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainNameValidate(t *testing.T) {
	base := DomainName{
		DomainName: "api.example.com",
		DomainNameConfigurations: []DomainNameConfiguration{{
			CertificateArn: "arn:aws:acm:us-east-1:123456789012:certificate/abc",
			EndpointType:   "regional",
			SecurityPolicy: "tls_1_2",
		}},
		RoutingMode: aws.String("routing_rule_then_api_mapping"),
	}

	tests := []struct {
		name    string
		mutate  func(*DomainName)
		wantErr string
	}{
		{
			name: "valid lower-case enums",
		},
		{
			name: "domain name is required",
			mutate: func(r *DomainName) {
				r.DomainName = ""
			},
			wantErr: "domain-name must be between 1 and 512 characters",
		},
		{
			name: "one configuration is required",
			mutate: func(r *DomainName) {
				r.DomainNameConfigurations = nil
			},
			wantErr: "domain-name-configurations must have exactly one item",
		},
		{
			name: "certificate ARN is checked",
			mutate: func(r *DomainName) {
				r.DomainNameConfigurations[0].CertificateArn = "not-an-arn"
			},
			wantErr: "certificate-arn must be a valid ARN",
		},
		{
			name: "endpoint type is checked case-insensitively",
			mutate: func(r *DomainName) {
				r.DomainNameConfigurations[0].EndpointType = "EDGE"
			},
			wantErr: "endpoint-type must be REGIONAL",
		},
		{
			name: "security policy is checked case-insensitively",
			mutate: func(r *DomainName) {
				r.DomainNameConfigurations[0].SecurityPolicy = "TLS_1_0"
			},
			wantErr: "security-policy must be TLS_1_2",
		},
		{
			name: "ownership verification ARN is checked",
			mutate: func(r *DomainName) {
				r.DomainNameConfigurations[0].OwnershipVerificationCertificateArn =
					aws.String("bad")
			},
			wantErr: "ownership-verification-certificate-arn must be a valid ARN",
		},
		{
			name: "routing mode is checked case-insensitively",
			mutate: func(r *DomainName) {
				r.RoutingMode = aws.String("bad")
			},
			wantErr: "routing-mode must be API_MAPPING_ONLY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := base
			r.DomainNameConfigurations = append([]DomainNameConfiguration(nil),
				base.DomainNameConfigurations...)
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			err := r.validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestDomainNameUpdateInput(t *testing.T) {
	prior := DomainName{
		DomainName: "api.example.com",
		DomainNameConfigurations: []DomainNameConfiguration{{
			CertificateArn: "arn:aws:acm:us-east-1:123456789012:certificate/old",
			EndpointType:   "REGIONAL",
			SecurityPolicy: "TLS_1_2",
		}},
		MutualTlsAuthentication: &DomainNameMutualTlsAuthentication{
			TruststoreUri:     "s3://bucket/old.pem",
			TruststoreVersion: aws.String("1"),
		},
		RoutingMode: aws.String("ROUTING_RULE_ONLY"),
	}
	r := DomainName{
		DomainName: "api.example.com",
		DomainNameConfigurations: []DomainNameConfiguration{{
			CertificateArn: "arn:aws:acm:us-east-1:123456789012:certificate/new",
			EndpointType:   "regional",
			SecurityPolicy: "tls_1_2",
		}},
		MutualTlsAuthentication: &DomainNameMutualTlsAuthentication{
			TruststoreUri: "s3://bucket/new.pem",
		},
	}

	in, changed := r.updateDomainNameInput(domainNameUpdatePrior(prior), "api.example.com")
	require.True(t, changed)
	require.Len(t, in.DomainNameConfigurations, 1)
	assert.Equal(t, "api.example.com", aws.ToString(in.DomainName))
	assert.Equal(t, "arn:aws:acm:us-east-1:123456789012:certificate/new",
		aws.ToString(in.DomainNameConfigurations[0].CertificateArn))
	assert.Equal(t, apigatewayv2types.EndpointType("REGIONAL"),
		in.DomainNameConfigurations[0].EndpointType)
	assert.Equal(t, apigatewayv2types.SecurityPolicy("TLS_1_2"),
		in.DomainNameConfigurations[0].SecurityPolicy)
	assert.Equal(t, "s3://bucket/new.pem",
		aws.ToString(in.MutualTlsAuthentication.TruststoreUri))
	assert.Equal(t, "", aws.ToString(in.MutualTlsAuthentication.TruststoreVersion))
	assert.Equal(t, apigatewayv2types.RoutingMode("API_MAPPING_ONLY"), in.RoutingMode)
}

func TestDomainNameUpdateInputDisablesMutualTLS(t *testing.T) {
	prior := DomainName{
		DomainNameConfigurations: []DomainNameConfiguration{{
			CertificateArn: "arn:aws:acm:us-east-1:123456789012:certificate/abc",
			EndpointType:   "REGIONAL",
			SecurityPolicy: "TLS_1_2",
		}},
		MutualTlsAuthentication: &DomainNameMutualTlsAuthentication{
			TruststoreUri: "s3://bucket/trust.pem",
		},
	}
	r := DomainName{DomainNameConfigurations: prior.DomainNameConfigurations}

	in, changed := r.updateDomainNameInput(domainNameUpdatePrior(prior), "api.example.com")
	require.True(t, changed)
	require.NotNil(t, in.MutualTlsAuthentication)
	assert.Equal(t, "", aws.ToString(in.MutualTlsAuthentication.TruststoreUri))
}

func TestDomainNameUpdateInputUnchanged(t *testing.T) {
	prior := DomainName{
		DomainNameConfigurations: []DomainNameConfiguration{{
			CertificateArn: "arn:aws:acm:us-east-1:123456789012:certificate/abc",
			EndpointType:   "REGIONAL",
			SecurityPolicy: "TLS_1_2",
		}},
	}
	r := prior

	in, changed := r.updateDomainNameInput(domainNameUpdatePrior(prior), "api.example.com")
	assert.False(t, changed)
	assert.Nil(t, in)
}

func TestDomainNameUpdateInputUsesObservedDrift(t *testing.T) {
	cert := "arn:aws:acm:us-east-1:123456789012:certificate/abc"
	ownership := "arn:aws:acm:us-east-1:123456789012:certificate/ownership-new"
	oldOwnership := "arn:aws:acm:us-east-1:123456789012:certificate/ownership-old"
	inputs := DomainName{
		DomainNameConfigurations: []DomainNameConfiguration{{
			CertificateArn:                      cert,
			EndpointType:                        "REGIONAL",
			IpAddressType:                       aws.String("ipv4"),
			OwnershipVerificationCertificateArn: aws.String(ownership),
			SecurityPolicy:                      "TLS_1_2",
		}},
		RoutingMode: aws.String("API_MAPPING_ONLY"),
	}
	observed := &DomainNameOutput{
		IpAddressType:                       "dualstack",
		OwnershipVerificationCertificateArn: oldOwnership,
		RoutingMode:                         "ROUTING_RULE_ONLY",
	}

	in, changed := inputs.updateDomainNameInput(
		domainNameUpdatePriorObserved(inputs, observed), "api.example.com")
	require.True(t, changed)
	require.Len(t, in.DomainNameConfigurations, 1)
	assert.Equal(t, apigatewayv2types.IpAddressType("ipv4"),
		in.DomainNameConfigurations[0].IpAddressType)
	assert.Equal(t, ownership,
		aws.ToString(in.DomainNameConfigurations[0].OwnershipVerificationCertificateArn))
	assert.Equal(t, apigatewayv2types.RoutingMode("API_MAPPING_ONLY"), in.RoutingMode)
}

func TestDomainNameUpdateInputIgnoresUnconfiguredOutputDrift(t *testing.T) {
	cert := "arn:aws:acm:us-east-1:123456789012:certificate/abc"
	oldOwnership := "arn:aws:acm:us-east-1:123456789012:certificate/old"
	inputs := DomainName{
		DomainNameConfigurations: []DomainNameConfiguration{{
			CertificateArn: cert,
			EndpointType:   "REGIONAL",
			SecurityPolicy: "TLS_1_2",
		}},
	}
	observed := &DomainNameOutput{
		ApiGatewayDomainName:                "d-new.execute-api.us-east-1.amazonaws.com",
		HostedZoneId:                        "Z2FDTNDATAQYW2",
		IpAddressType:                       "dualstack",
		OwnershipVerificationCertificateArn: oldOwnership,
		RoutingMode:                         "ROUTING_RULE_ONLY",
		TargetDomainName:                    "d-new.execute-api.us-east-1.amazonaws.com",
	}

	in, changed := inputs.updateDomainNameInput(
		domainNameUpdatePriorObserved(inputs, observed), "api.example.com")
	assert.False(t, changed)
	assert.Nil(t, in)
}

func TestWaitDomainNameAvailableResetsMissingStreakAfterFound(t *testing.T) {
	missing := &apigatewayv2.GetDomainNameOutput{}
	updating := &apigatewayv2.GetDomainNameOutput{
		DomainNameConfigurations: []apigatewayv2types.DomainNameConfiguration{{
			DomainNameStatus: apigatewayv2types.DomainNameStatus("UPDATING"),
		}},
	}
	available := &apigatewayv2.GetDomainNameOutput{
		DomainNameConfigurations: []apigatewayv2types.DomainNameConfiguration{{
			DomainNameStatus: apigatewayv2types.DomainNameStatus("AVAILABLE"),
		}},
	}
	responses := make([]*apigatewayv2.GetDomainNameOutput, 0,
		domainNameNotFoundLimit*2+2)
	for range domainNameNotFoundLimit {
		responses = append(responses, missing)
	}
	responses = append(responses, updating)
	for range domainNameNotFoundLimit {
		responses = append(responses, missing)
	}
	responses = append(responses, available)
	calls := 0

	err := waitDomainNameAvailableWithGetter(
		context.Background(), "api.example.com", time.Second, 0,
		func(context.Context) (*apigatewayv2.GetDomainNameOutput, error) {
			resp := responses[calls]
			calls++
			return resp, nil
		})
	require.NoError(t, err)
	assert.Equal(t, len(responses), calls)
}

func domainNameUpdatePrior(inputs DomainName) runtime.Prior[DomainName, *DomainNameOutput] {
	return domainNameUpdatePriorObserved(inputs, nil)
}

func domainNameUpdatePriorObserved(
	inputs DomainName, observed *DomainNameOutput,
) runtime.Prior[DomainName, *DomainNameOutput] {
	return runtime.Prior[DomainName, *DomainNameOutput]{Inputs: inputs, Observed: observed}
}

func TestDomainNameUserTags(t *testing.T) {
	tags := domainNameUserTags(map[string]string{
		"aws:cloudformation:stack-name": "owned",
		"empty":                         "",
		"team":                          "platform",
	})

	assert.Equal(t, map[string]string{"empty": "", "team": "platform"}, tags)
	assert.Nil(t, domainNameUserTags(map[string]string{"aws:tag": "value"}))
	assert.Equal(t, map[string]string{}, domainNameOutputTags(nil))
}

func TestDomainNameTagsNeedSyncWhenObservedDiffers(t *testing.T) {
	r := DomainName{Tags: new(map[string]string{"team": "platform"})}
	prior := runtime.Prior[DomainName, *DomainNameOutput]{
		Inputs: DomainName{Tags: new(map[string]string{"team": "platform"})},
		Observed: &DomainNameOutput{
			Tags: map[string]string{"team": "security"},
		},
	}

	assert.True(t, r.tagsNeedSync(prior))
}

func TestDomainNameTagsNeedSyncUsesUserTags(t *testing.T) {
	r := DomainName{Tags: new(map[string]string{"aws:system": "new", "team": "platform"})}
	prior := runtime.Prior[DomainName, *DomainNameOutput]{
		Inputs: DomainName{Tags: new(map[string]string{"team": "platform"})},
		Observed: &DomainNameOutput{
			Tags: map[string]string{"team": "platform"},
		},
	}

	assert.False(t, r.tagsNeedSync(prior))
}

func TestDomainNameARN(t *testing.T) {
	assert.Equal(t,
		"arn:aws:apigateway:us-east-1::/domainnames/api.example.com",
		domainNameARN("us-east-1", "api.example.com"))
	assert.Equal(t,
		"arn:aws-us-gov:apigateway:us-gov-west-1::/domainnames/api.example.com",
		domainNameARN("us-gov-west-1", "api.example.com"))
}

func TestDomainNameReadOutput(t *testing.T) {
	resp := &apigatewayv2.GetDomainNameOutput{
		ApiMappingSelectionExpression: aws.String("$request.basepath"),
		DomainName:                    aws.String("api.example.com"),
		DomainNameConfigurations: []apigatewayv2types.DomainNameConfiguration{{
			ApiGatewayDomainName: aws.String("d-123.execute-api.us-east-1.amazonaws.com"),
			DomainNameStatus:     apigatewayv2types.DomainNameStatus("AVAILABLE"),
			HostedZoneId:         aws.String("Z2FDTNDATAQYW2"),
			IpAddressType:        apigatewayv2types.IpAddressType("ipv4"),
		}},
		RoutingMode: apigatewayv2types.RoutingMode("API_MAPPING_ONLY"),
		Tags: map[string]string{
			"aws:cloudformation:stack-name": "owned",
			"empty":                         "",
			"team":                          "platform",
		},
	}

	out := domainNameOutput("us-east-1", "api.example.com", resp)
	assert.Equal(t, &DomainNameOutput{
		DomainName:                    "api.example.com",
		Arn:                           "arn:aws:apigateway:us-east-1::/domainnames/api.example.com",
		ApiGatewayDomainName:          "d-123.execute-api.us-east-1.amazonaws.com",
		TargetDomainName:              "d-123.execute-api.us-east-1.amazonaws.com",
		HostedZoneId:                  "Z2FDTNDATAQYW2",
		ApiMappingSelectionExpression: "$request.basepath",
		DomainNameStatus:              "AVAILABLE",
		IpAddressType:                 "ipv4",
		RoutingMode:                   "API_MAPPING_ONLY",
		Tags:                          map[string]string{"empty": "", "team": "platform"},
	}, out)
}
