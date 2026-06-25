package apigatewayv2

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthorizerReplaceFields(t *testing.T) {
	var r Authorizer

	assert.Equal(t, []string{"api-id"}, r.ReplaceFields())
}

func TestAuthorizerCreateInputAppliesHTTPREQUESTTTLDefault(t *testing.T) {
	r := Authorizer{
		ApiId:           "api-123",
		AuthorizerType:  "REQUEST",
		IdentitySources: []string{"$request.header.Auth", "", "$request.header.Auth"},
		Name:            "auth",
	}

	in := r.createInput(apigatewayv2types.ProtocolTypeHttp)

	assert.Equal(t, []string{"$request.header.Auth"}, in.IdentitySource)
	assert.Equal(t, int32(authorizerCreateDefaultTTL), aws.ToInt32(in.AuthorizerResultTtlInSeconds))
}

func TestAuthorizerCreateInputOmitsTTLDefaultWhenConditionDoesNotMatch(t *testing.T) {
	tests := []struct {
		name     string
		resource Authorizer
		protocol apigatewayv2types.ProtocolType
	}{
		{
			name: "no identity sources",
			resource: Authorizer{
				ApiId:          "api-123",
				AuthorizerType: "REQUEST",
				Name:           "auth",
			},
			protocol: apigatewayv2types.ProtocolTypeHttp,
		},
		{
			name: "non HTTP API",
			resource: Authorizer{
				ApiId:           "api-123",
				AuthorizerType:  "REQUEST",
				IdentitySources: []string{"route.request.header.Auth"},
				Name:            "auth",
			},
			protocol: apigatewayv2types.ProtocolTypeWebsocket,
		},
		{
			name: "non REQUEST authorizer",
			resource: Authorizer{
				ApiId:           "api-123",
				AuthorizerType:  "JWT",
				IdentitySources: []string{"$request.header.Authorization"},
				Name:            "auth",
			},
			protocol: apigatewayv2types.ProtocolTypeHttp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.resource.createInput(tt.protocol)

			assert.Nil(t, in.AuthorizerResultTtlInSeconds)
		})
	}
}

func TestAuthorizerCreateInputSendsExplicitZeroTTL(t *testing.T) {
	r := Authorizer{
		ApiId:                        "api-123",
		AuthorizerType:               "REQUEST",
		AuthorizerResultTtlInSeconds: aws.Int64(0),
		IdentitySources:              []string{"$request.header.Auth"},
		Name:                         "auth",
	}

	in := r.createInput(apigatewayv2types.ProtocolTypeHttp)

	require.NotNil(t, in.AuthorizerResultTtlInSeconds)
	assert.Equal(t, int32(0), aws.ToInt32(in.AuthorizerResultTtlInSeconds))
}

func TestAuthorizerCreateInputOmitsFalseSimpleResponses(t *testing.T) {
	r := Authorizer{
		ApiId:                 "api-123",
		AuthorizerType:        "REQUEST",
		EnableSimpleResponses: aws.Bool(false),
		Name:                  "auth",
	}

	in := r.createInput(apigatewayv2types.ProtocolTypeHttp)

	assert.Nil(t, in.EnableSimpleResponses)
}

func TestAuthorizerCreateInputExpandsJWTConfiguration(t *testing.T) {
	audience := []string{"api", "", "api", "admin"}
	r := Authorizer{
		ApiId:          "api-123",
		AuthorizerType: "JWT",
		Name:           "auth",
		JwtConfiguration: &AuthorizerJwtConfiguration{
			Audience: &audience,
			Issuer:   aws.String("https://issuer.example.com"),
		},
	}

	in := r.createInput(apigatewayv2types.ProtocolTypeHttp)

	require.NotNil(t, in.JwtConfiguration)
	assert.Equal(t, []string{"admin", "api"}, in.JwtConfiguration.Audience)
	assert.Equal(t, "https://issuer.example.com", aws.ToString(in.JwtConfiguration.Issuer))
}

func TestAuthorizerUpdateInputUnchanged(t *testing.T) {
	inputs := Authorizer{
		ApiId:           "api-123",
		AuthorizerType:  "REQUEST",
		IdentitySources: []string{"b", "a"},
		Name:            "auth",
		JwtConfiguration: &AuthorizerJwtConfiguration{
			Audience: stringSlicePtr("z", "a"),
			Issuer:   aws.String("issuer"),
		},
	}
	r := Authorizer{
		ApiId:           "api-123",
		AuthorizerType:  "REQUEST",
		IdentitySources: []string{"a", "b", "", "a"},
		Name:            "auth",
		JwtConfiguration: &AuthorizerJwtConfiguration{
			Audience: stringSlicePtr("a", "z", "z", ""),
			Issuer:   aws.String("issuer"),
		},
	}

	in, changed := r.updateInput(authorizerPrior(inputs))

	assert.False(t, changed)
	assert.NotNil(t, in)
}

func TestAuthorizerUpdateInputReconcilesExplicitTTLDifference(t *testing.T) {
	tests := []struct {
		name       string
		configured int64
		observed   int64
	}{
		{name: "non-zero ttl", configured: 300, observed: 0},
		{name: "zero ttl", configured: 0, observed: 300},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := Authorizer{
				ApiId:                        "api-123",
				AuthorizerType:               "REQUEST",
				AuthorizerResultTtlInSeconds: aws.Int64(tt.configured),
				IdentitySources:              []string{"$request.header.Auth"},
				Name:                         "auth",
			}
			r := inputs
			prior := authorizerPrior(inputs)
			prior.Observed = &AuthorizerOutput{
				ApiId:                        "api-123",
				AuthorizerId:                 "auth-123",
				AuthorizerResultTtlInSeconds: tt.observed,
			}

			in, changed := r.updateInput(prior)

			require.True(t, changed)
			require.NotNil(t, in.AuthorizerResultTtlInSeconds)
			assert.Equal(t, int32(tt.configured), aws.ToInt32(in.AuthorizerResultTtlInSeconds))
		})
	}
}

func TestAuthorizerUpdateInputIgnoresUnconfiguredTTLDifference(t *testing.T) {
	inputs := Authorizer{
		ApiId:           "api-123",
		AuthorizerType:  "REQUEST",
		IdentitySources: []string{"$request.header.Auth"},
		Name:            "auth",
	}
	prior := authorizerPrior(inputs)
	prior.Observed = &AuthorizerOutput{
		ApiId:                        "api-123",
		AuthorizerId:                 "auth-123",
		AuthorizerResultTtlInSeconds: 300,
	}

	in, changed := inputs.updateInput(prior)

	assert.False(t, changed)
	assert.Nil(t, in.AuthorizerResultTtlInSeconds)
}

func TestAuthorizerUpdateInputClearsFields(t *testing.T) {
	prior := Authorizer{
		ApiId:                          "api-123",
		AuthorizerType:                 "REQUEST",
		AuthorizerCredentialsArn:       aws.String("arn:aws:iam::123456789012:role/auth"),
		AuthorizerPayloadFormatVersion: aws.String("2.0"),
		AuthorizerResultTtlInSeconds:   aws.Int64(300),
		AuthorizerUri:                  aws.String("arn:aws:apigateway:us-east-1:lambda:path/f"),
		EnableSimpleResponses:          aws.Bool(true),
		IdentitySources:                []string{"$request.header.Auth"},
		Name:                           "auth",
		JwtConfiguration: &AuthorizerJwtConfiguration{
			Audience: stringSlicePtr("api"),
			Issuer:   aws.String("issuer"),
		},
	}
	r := Authorizer{
		ApiId:          "api-123",
		AuthorizerType: "JWT",
		Name:           "auth-v2",
	}

	in, changed := r.updateInput(authorizerPrior(prior))

	require.True(t, changed)
	assert.Equal(t, "api-123", aws.ToString(in.ApiId))
	assert.Equal(t, "auth-123", aws.ToString(in.AuthorizerId))
	assert.Equal(t, "", aws.ToString(in.AuthorizerCredentialsArn))
	assert.Equal(t, "", aws.ToString(in.AuthorizerPayloadFormatVersion))
	assert.Equal(t, int32(0), aws.ToInt32(in.AuthorizerResultTtlInSeconds))
	assert.Equal(t, apigatewayv2types.AuthorizerTypeJwt, in.AuthorizerType)
	assert.Equal(t, "", aws.ToString(in.AuthorizerUri))
	assert.False(t, aws.ToBool(in.EnableSimpleResponses))
	assert.Equal(t, []string{}, in.IdentitySource)
	require.NotNil(t, in.JwtConfiguration)
	assert.Empty(t, in.JwtConfiguration.Audience)
	assert.Nil(t, in.JwtConfiguration.Issuer)
	assert.Equal(t, "auth-v2", aws.ToString(in.Name))
}

func TestAuthorizerOutputDefaultsNilTTLToZero(t *testing.T) {
	out := authorizerOutput("api-123", "auth-123", &apigatewayv2.GetAuthorizerOutput{})

	assert.Equal(t, &AuthorizerOutput{
		ApiId:                        "api-123",
		AuthorizerId:                 "auth-123",
		AuthorizerResultTtlInSeconds: 0,
	}, out)
}

func TestAuthorizerValidate(t *testing.T) {
	base := Authorizer{
		ApiId:                    "api-123",
		AuthorizerType:           "REQUEST",
		AuthorizerCredentialsArn: aws.String("arn:aws:iam::123456789012:role/auth"),
		AuthorizerUri:            aws.String("arn:aws:apigateway:us-east-1:lambda:path/f"),
		Name:                     "auth",
	}
	tests := []struct {
		name    string
		mutate  func(*Authorizer)
		wantErr string
	}{
		{name: "valid"},
		{
			name: "empty credentials arn is accepted",
			mutate: func(r *Authorizer) {
				r.AuthorizerCredentialsArn = aws.String("")
			},
		},
		{
			name: "name is required",
			mutate: func(r *Authorizer) {
				r.Name = ""
			},
			wantErr: "name must be between 1 and 128 bytes",
		},
		{
			name: "name is at most 128 bytes",
			mutate: func(r *Authorizer) {
				r.Name = strings.Repeat("a", 129)
			},
			wantErr: "name must be between 1 and 128 bytes",
		},
		{
			name: "name counts bytes",
			mutate: func(r *Authorizer) {
				r.Name = strings.Repeat("é", 65)
			},
			wantErr: "name must be between 1 and 128 bytes",
		},
		{
			name: "uri is not empty",
			mutate: func(r *Authorizer) {
				r.AuthorizerUri = aws.String("")
			},
			wantErr: "authorizer-uri must be between 1 and 2048 bytes",
		},
		{
			name: "uri is at most 2048 bytes",
			mutate: func(r *Authorizer) {
				r.AuthorizerUri = aws.String(strings.Repeat("a", 2049))
			},
			wantErr: "authorizer-uri must be between 1 and 2048 bytes",
		},
		{
			name: "uri counts bytes",
			mutate: func(r *Authorizer) {
				r.AuthorizerUri = aws.String(strings.Repeat("é", 1025))
			},
			wantErr: "authorizer-uri must be between 1 and 2048 bytes",
		},
		{
			name: "credentials arn is checked",
			mutate: func(r *Authorizer) {
				r.AuthorizerCredentialsArn = aws.String("not-an-arn")
			},
			wantErr: "authorizer-credentials-arn must be a valid ARN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := base
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

func TestValidAuthorizerARN(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "explicit empty string",
			value: "",
			want:  true,
		},
		{
			name:  "role arn",
			value: "arn:aws:iam::123456789012:role/auth",
			want:  true,
		},
		{
			name:  "gov partition",
			value: "arn:aws-us-gov:iam::aws:policy/AdministratorAccess",
			want:  true,
		},
		{
			name:  "empty region and account",
			value: "arn:aws:s3:::bucket/example",
			want:  true,
		},
		{
			name:  "cw account",
			value: "arn:aws:logs:us-east-1:cw1234567890:log-group/example",
			want:  true,
		},
		{
			name:  "empty service",
			value: "arn:aws::us-east-1:123456789012:thing",
			want:  true,
		},
		{
			name:  "missing arn prefix",
			value: "iam::123456789012:role/auth",
			want:  false,
		},
		{
			name:  "missing partition",
			value: "arn::iam::123456789012:role/auth",
			want:  false,
		},
		{
			name:  "invalid partition",
			value: "arn:aws123:iam::123456789012:role/auth",
			want:  false,
		},
		{
			name:  "invalid region",
			value: "arn:aws:iam:useast1:123456789012:role/auth",
			want:  false,
		},
		{
			name:  "invalid account",
			value: "arn:aws:iam::123:role/auth",
			want:  false,
		},
		{
			name:  "missing resource",
			value: "arn:aws:iam::123456789012:",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validAuthorizerARN(tt.value))
		})
	}
}

func authorizerPrior(inputs Authorizer) runtime.Prior[Authorizer, *AuthorizerOutput] {
	return runtime.Prior[Authorizer, *AuthorizerOutput]{
		Inputs: inputs,
		Outputs: &AuthorizerOutput{
			ApiId:        "api-123",
			AuthorizerId: "auth-123",
		},
	}
}

func stringSlicePtr(values ...string) *[]string {
	out := append([]string(nil), values...)
	return &out
}
