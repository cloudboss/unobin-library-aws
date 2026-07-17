package cognitoidp

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPoolClientValidateInputsRejectsInvalidConfigurations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*UserPoolClientResource)
		want   string
	}{
		{
			name: "user pool id",
			mutate: func(r *UserPoolClientResource) {
				r.UserPoolID = "invalid"
			},
			want: "user-pool-id",
		},
		{
			name: "empty name",
			mutate: func(r *UserPoolClientResource) {
				r.Name = aws.String("")
			},
			want: "name",
		},
		{
			name: "access token raw zero",
			mutate: func(r *UserPoolClientResource) {
				r.AccessTokenValidity = aws.Int32(0)
			},
			want: "access-token-validity",
		},
		{
			name: "access token below five minutes",
			mutate: func(r *UserPoolClientResource) {
				r.AccessTokenValidity = aws.Int32(299)
				r.TokenValidityUnits = &UserPoolClientValidityUnits{
					AccessToken: aws.String("seconds"),
				}
			},
			want: "access-token-validity",
		},
		{
			name: "id token above one day",
			mutate: func(r *UserPoolClientResource) {
				r.IDTokenValidity = aws.Int32(25)
				r.TokenValidityUnits = &UserPoolClientValidityUnits{
					IDToken: aws.String("hours"),
				}
			},
			want: "id-token-validity",
		},
		{
			name: "refresh token explicit zero",
			mutate: func(r *UserPoolClientResource) {
				r.RefreshTokenValidity = aws.Int32(0)
			},
			want: "refresh-token-validity",
		},
		{
			name: "refresh token below one hour",
			mutate: func(r *UserPoolClientResource) {
				r.RefreshTokenValidity = aws.Int32(59)
				r.TokenValidityUnits = &UserPoolClientValidityUnits{
					RefreshToken: aws.String("minutes"),
				}
			},
			want: "refresh-token-validity",
		},
		{
			name: "access token exceeds refresh token",
			mutate: func(r *UserPoolClientResource) {
				r.AccessTokenValidity = aws.Int32(2)
				r.RefreshTokenValidity = aws.Int32(1)
				r.TokenValidityUnits = &UserPoolClientValidityUnits{
					AccessToken:  aws.String("hours"),
					RefreshToken: aws.String("hours"),
				}
			},
			want: "must not exceed refresh-token-validity",
		},
		{
			name: "auth session lower bound",
			mutate: func(r *UserPoolClientResource) {
				r.AuthSessionValidity = aws.Int32(2)
			},
			want: "auth-session-validity",
		},
		{
			name: "token unit enum",
			mutate: func(r *UserPoolClientResource) {
				r.TokenValidityUnits = &UserPoolClientValidityUnits{
					AccessToken: aws.String("weeks"),
				}
			},
			want: "token-validity-units.access-token",
		},
		{
			name: "oauth collection requires flag",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthScopes = userPoolClientStrings("openid")
			},
			want: "allowed-oauth-flows-user-pool-client",
		},
		{
			name: "default redirect requires callback membership",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
				r.CallbackURLs = userPoolClientStrings("https://example.com/callback")
				r.DefaultRedirectURI = aws.String("https://example.com/other")
			},
			want: "default-redirect-uri",
		},
		{
			name: "client credentials is exclusive",
			mutate: func(r *UserPoolClientResource) {
				r.GenerateSecret = aws.Bool(true)
				r.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
				r.AllowedOAuthFlows = userPoolClientStrings("client_credentials", "code")
			},
			want: "client_credentials must be the only",
		},
		{
			name: "client credentials requires secret",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
				r.AllowedOAuthFlows = userPoolClientStrings("client_credentials")
			},
			want: "client_credentials requires generate-secret true",
		},
		{
			name: "context data requires secret",
			mutate: func(r *UserPoolClientResource) {
				r.EnablePropagateAdditionalUserContextData = aws.Bool(true)
			},
			want: "enable-propagate-additional-user-context-data",
		},
		{
			name: "relative callback",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
				r.CallbackURLs = userPoolClientStrings("/callback")
			},
			want: "callback-urls[0]",
		},
		{
			name: "callback fragment",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
				r.CallbackURLs = userPoolClientStrings("https://example.com/callback#fragment")
			},
			want: "callback-urls[0]",
		},
		{
			name: "non-loopback http callback",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
				r.CallbackURLs = userPoolClientStrings("http://example.com/callback")
			},
			want: "callback-urls[0]",
		},
		{
			name: "empty logout url",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
				r.LogoutURLs = userPoolClientStrings("")
			},
			want: "logout-urls[0]",
		},
		{
			name: "scope pattern",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
				r.AllowedOAuthScopes = userPoolClientStrings("bad scope")
			},
			want: "allowed-oauth-scopes[0]",
		},
		{
			name: "explicit auth enum",
			mutate: func(r *UserPoolClientResource) {
				r.ExplicitAuthFlows = userPoolClientStrings("INVALID")
			},
			want: "explicit-auth-flows",
		},
		{
			name: "legacy and current explicit auth",
			mutate: func(r *UserPoolClientResource) {
				r.ExplicitAuthFlows = userPoolClientStrings(
					"USER_PASSWORD_AUTH",
					"ALLOW_USER_PASSWORD_AUTH",
				)
			},
			want: "legacy explicit-auth-flows",
		},
		{
			name: "prevent user existence enum",
			mutate: func(r *UserPoolClientResource) {
				r.PreventUserExistenceErrors = aws.String("UNKNOWN")
			},
			want: "prevent-user-existence-errors",
		},
		{
			name: "rotation feature required",
			mutate: func(r *UserPoolClientResource) {
				r.RefreshTokenRotation = &UserPoolClientRotation{}
			},
			want: "refresh-token-rotation.feature",
		},
		{
			name: "rotation grace upper bound",
			mutate: func(r *UserPoolClientResource) {
				r.RefreshTokenRotation = &UserPoolClientRotation{
					Feature: "ENABLED", RetryGracePeriodSeconds: aws.Int32(61),
				}
			},
			want: "retry-grace-period-seconds",
		},
		{
			name: "rotation conflicts with refresh auth",
			mutate: func(r *UserPoolClientResource) {
				r.RefreshTokenRotation = &UserPoolClientRotation{Feature: "ENABLED"}
				r.ExplicitAuthFlows = userPoolClientStrings("ALLOW_REFRESH_TOKEN_AUTH")
			},
			want: "ALLOW_REFRESH_TOKEN_AUTH",
		},
		{
			name: "empty analytics",
			mutate: func(r *UserPoolClientResource) {
				r.AnalyticsConfiguration = &UserPoolClientAnalytics{}
			},
			want: "analytics-configuration",
		},
		{
			name: "analytics triplet incomplete",
			mutate: func(r *UserPoolClientResource) {
				r.AnalyticsConfiguration = &UserPoolClientAnalytics{
					ApplicationID: aws.String("abcdef"),
				}
			},
			want: "application-id, external-id, and role-arn",
		},
		{
			name: "analytics forms conflict",
			mutate: func(r *UserPoolClientResource) {
				r.AnalyticsConfiguration = validUserPoolClientAnalytics()
				r.AnalyticsConfiguration.ApplicationARN = aws.String(
					"arn:aws:mobiletargeting:us-east-1:123456789012:apps/abcdef",
				)
			},
			want: "conflicts",
		},
		{
			name: "analytics application id pattern",
			mutate: func(r *UserPoolClientResource) {
				r.AnalyticsConfiguration = validUserPoolClientAnalytics()
				r.AnalyticsConfiguration.ApplicationID = aws.String("not-hex")
			},
			want: "application-id",
		},
		{
			name: "attribute item empty",
			mutate: func(r *UserPoolClientResource) {
				r.ReadAttributes = userPoolClientStrings("")
			},
			want: "read-attributes[0]",
		},
		{
			name: "provider item too long",
			mutate: func(r *UserPoolClientResource) {
				r.SupportedIdentityProviders = userPoolClientStrings(strings.Repeat("a", 33))
			},
			want: "supported-identity-providers[0]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validUserPoolClientResource()
			tt.mutate(&resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestUserPoolClientValidateInputsAcceptsDocumentedConfigurations(t *testing.T) {
	resource := validUserPoolClientResource()
	resource.GenerateSecret = aws.Bool(true)
	resource.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
	resource.AllowedOAuthFlows = userPoolClientStrings("code", "implicit")
	resource.AllowedOAuthScopes = userPoolClientStrings("openid", "api/read")
	resource.CallbackURLs = userPoolClientStrings(
		"https://example.com/callback",
		"myapp://callback",
		"http://localhost:8080/callback",
		"http://127.0.0.1:8080/callback",
		"http://[::1]:8080/callback",
	)
	resource.DefaultRedirectURI = aws.String("https://example.com/callback")
	resource.LogoutURLs = userPoolClientStrings("http://example.com/logout")
	resource.EnablePropagateAdditionalUserContextData = aws.Bool(true)
	resource.ExplicitAuthFlows = userPoolClientStrings("ALLOW_USER_AUTH")
	resource.RefreshTokenRotation = &UserPoolClientRotation{
		Feature: "DISABLED", RetryGracePeriodSeconds: aws.Int32(0),
	}
	resource.AnalyticsConfiguration = validUserPoolClientAnalytics()

	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestUserPoolClientValidateInputsAcceptsConfidentialClientCredentials(t *testing.T) {
	resource := validUserPoolClientResource()
	resource.GenerateSecret = aws.Bool(true)
	resource.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
	resource.AllowedOAuthFlows = userPoolClientStrings("client_credentials")

	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestUserPoolClientTokenValidityUnitBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value int32
		unit  string
	}{
		{name: "access seconds lower", field: "access", value: 300, unit: "seconds"},
		{name: "access seconds upper", field: "access", value: 86400, unit: "seconds"},
		{name: "access minutes lower", field: "access", value: 5, unit: "minutes"},
		{name: "access minutes upper", field: "access", value: 1440, unit: "minutes"},
		{name: "access hours lower", field: "access", value: 1, unit: "hours"},
		{name: "access hours upper", field: "access", value: 24, unit: "hours"},
		{name: "access days boundary", field: "access", value: 1, unit: "days"},
		{name: "id seconds lower", field: "id", value: 300, unit: "seconds"},
		{name: "id seconds upper", field: "id", value: 86400, unit: "seconds"},
		{name: "id minutes lower", field: "id", value: 5, unit: "minutes"},
		{name: "id minutes upper", field: "id", value: 1440, unit: "minutes"},
		{name: "id hours lower", field: "id", value: 1, unit: "hours"},
		{name: "id hours upper", field: "id", value: 24, unit: "hours"},
		{name: "id days boundary", field: "id", value: 1, unit: "days"},
		{name: "refresh seconds lower", field: "refresh", value: 3600, unit: "seconds"},
		{
			name: "refresh seconds upper", field: "refresh",
			value: 315360000, unit: "seconds",
		},
		{name: "refresh minutes lower", field: "refresh", value: 60, unit: "minutes"},
		{
			name: "refresh minutes upper", field: "refresh",
			value: 5256000, unit: "minutes",
		},
		{name: "refresh hours lower", field: "refresh", value: 1, unit: "hours"},
		{name: "refresh hours upper", field: "refresh", value: 87600, unit: "hours"},
		{name: "refresh days lower", field: "refresh", value: 1, unit: "days"},
		{name: "refresh days upper", field: "refresh", value: 3650, unit: "days"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validUserPoolClientResource()
			setUserPoolClientToken(&resource, tt.field, tt.value, tt.unit)
			require.NoError(t, resource.ValidateInputs(context.Background(), nil))
		})
	}
}

func TestUserPoolClientTokenValidityDefaultsAndEquality(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*UserPoolClientResource)
	}{
		{name: "all defaults", mutate: func(*UserPoolClientResource) {}},
		{
			name: "access omitted unit lower",
			mutate: func(r *UserPoolClientResource) {
				r.AccessTokenValidity = aws.Int32(1)
			},
		},
		{
			name: "access omitted unit upper",
			mutate: func(r *UserPoolClientResource) {
				r.AccessTokenValidity = aws.Int32(24)
			},
		},
		{
			name: "id omitted unit lower",
			mutate: func(r *UserPoolClientResource) {
				r.IDTokenValidity = aws.Int32(1)
			},
		},
		{
			name: "id omitted unit upper",
			mutate: func(r *UserPoolClientResource) {
				r.IDTokenValidity = aws.Int32(24)
			},
		},
		{
			name: "refresh omitted unit lower",
			mutate: func(r *UserPoolClientResource) {
				r.RefreshTokenValidity = aws.Int32(1)
			},
		},
		{
			name: "refresh omitted unit upper",
			mutate: func(r *UserPoolClientResource) {
				r.RefreshTokenValidity = aws.Int32(3650)
			},
		},
		{
			name: "access equals refresh",
			mutate: func(r *UserPoolClientResource) {
				r.AccessTokenValidity = aws.Int32(1)
				r.RefreshTokenValidity = aws.Int32(1)
				r.TokenValidityUnits = &UserPoolClientValidityUnits{
					AccessToken: aws.String("hours"), RefreshToken: aws.String("hours"),
				}
			},
		},
		{
			name: "id equals refresh",
			mutate: func(r *UserPoolClientResource) {
				r.IDTokenValidity = aws.Int32(60)
				r.RefreshTokenValidity = aws.Int32(1)
				r.TokenValidityUnits = &UserPoolClientValidityUnits{
					IDToken: aws.String("minutes"), RefreshToken: aws.String("hours"),
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validUserPoolClientResource()
			tt.mutate(&resource)
			require.NoError(t, resource.ValidateInputs(context.Background(), nil))
		})
	}
}

func TestUserPoolClientTokenValidityRejectsInvalidDurations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*UserPoolClientResource)
		want   string
	}{
		{
			name: "access explicit zero",
			mutate: func(r *UserPoolClientResource) {
				r.AccessTokenValidity = aws.Int32(0)
			},
			want: "access-token-validity",
		},
		{
			name: "id explicit zero",
			mutate: func(r *UserPoolClientResource) {
				r.IDTokenValidity = aws.Int32(0)
			},
			want: "id-token-validity",
		},
		{
			name: "refresh explicit zero",
			mutate: func(r *UserPoolClientResource) {
				r.RefreshTokenValidity = aws.Int32(0)
			},
			want: "refresh-token-validity",
		},
	}
	unitCases := []struct {
		name  string
		field string
		value int32
		unit  string
	}{
		{name: "access seconds below", field: "access", value: 299, unit: "seconds"},
		{name: "access seconds above", field: "access", value: 86401, unit: "seconds"},
		{name: "access minutes below", field: "access", value: 4, unit: "minutes"},
		{name: "access minutes above", field: "access", value: 1441, unit: "minutes"},
		{name: "access hours above", field: "access", value: 25, unit: "hours"},
		{name: "access days above", field: "access", value: 2, unit: "days"},
		{name: "id seconds below", field: "id", value: 299, unit: "seconds"},
		{name: "id seconds above", field: "id", value: 86401, unit: "seconds"},
		{name: "id minutes below", field: "id", value: 4, unit: "minutes"},
		{name: "id minutes above", field: "id", value: 1441, unit: "minutes"},
		{name: "id hours above", field: "id", value: 25, unit: "hours"},
		{name: "id days above", field: "id", value: 2, unit: "days"},
		{name: "refresh seconds below", field: "refresh", value: 3599, unit: "seconds"},
		{
			name: "refresh seconds above", field: "refresh",
			value: 315360001, unit: "seconds",
		},
		{name: "refresh minutes below", field: "refresh", value: 59, unit: "minutes"},
		{
			name: "refresh minutes above", field: "refresh",
			value: 5256001, unit: "minutes",
		},
		{name: "refresh hours above", field: "refresh", value: 87601, unit: "hours"},
		{name: "refresh days above", field: "refresh", value: 3651, unit: "days"},
		{
			name: "access large day input", field: "access",
			value: 86400, unit: "days",
		},
		{name: "id large day input", field: "id", value: 86400, unit: "days"},
		{
			name: "refresh duration overflow scale", field: "refresh",
			value: 315360000, unit: "days",
		},
	}
	for _, tt := range unitCases {
		field := tt.field
		value := tt.value
		unit := tt.unit
		tests = append(tests, struct {
			name   string
			mutate func(*UserPoolClientResource)
			want   string
		}{
			name: tt.name,
			mutate: func(r *UserPoolClientResource) {
				setUserPoolClientToken(r, field, value, unit)
			},
			want: field + "-token-validity",
		})
	}
	tests = append(tests,
		struct {
			name   string
			mutate func(*UserPoolClientResource)
			want   string
		}{
			name: "access exceeds refresh",
			mutate: func(r *UserPoolClientResource) {
				r.AccessTokenValidity = aws.Int32(2)
				r.RefreshTokenValidity = aws.Int32(1)
				r.TokenValidityUnits = &UserPoolClientValidityUnits{
					AccessToken: aws.String("hours"), RefreshToken: aws.String("hours"),
				}
			},
			want: "access-token-validity must not exceed refresh-token-validity",
		},
		struct {
			name   string
			mutate func(*UserPoolClientResource)
			want   string
		}{
			name: "id exceeds refresh",
			mutate: func(r *UserPoolClientResource) {
				r.IDTokenValidity = aws.Int32(2)
				r.RefreshTokenValidity = aws.Int32(1)
				r.TokenValidityUnits = &UserPoolClientValidityUnits{
					IDToken: aws.String("hours"), RefreshToken: aws.String("hours"),
				}
			},
			want: "id-token-validity must not exceed refresh-token-validity",
		},
	)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validUserPoolClientResource()
			tt.mutate(&resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func setUserPoolClientToken(
	resource *UserPoolClientResource,
	field string,
	value int32,
	unit string,
) {
	if resource.TokenValidityUnits == nil {
		resource.TokenValidityUnits = &UserPoolClientValidityUnits{}
	}
	switch field {
	case "access":
		resource.AccessTokenValidity = aws.Int32(value)
		resource.TokenValidityUnits.AccessToken = aws.String(unit)
	case "id":
		resource.IDTokenValidity = aws.Int32(value)
		resource.TokenValidityUnits.IDToken = aws.String(unit)
	case "refresh":
		resource.RefreshTokenValidity = aws.Int32(value)
		resource.TokenValidityUnits.RefreshToken = aws.String(unit)
	}
}

func TestUserPoolClientCollectionBoundaries(t *testing.T) {
	callbackPrefix := "https://example.com/"
	resource := validUserPoolClientResource()
	resource.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
	resource.AllowedOAuthFlows = userPoolClientStrings("code", "implicit")
	resource.AllowedOAuthScopes = userPoolClientStrings(strings.Repeat("a", 256))
	resource.CallbackURLs = userPoolClientStrings(
		callbackPrefix + strings.Repeat("界", 1024-len(callbackPrefix)),
	)
	resource.LogoutURLs = userPoolClientStrings(strings.Repeat("x", 1024))
	resource.ReadAttributes = userPoolClientStrings(strings.Repeat("r", 2048))
	resource.WriteAttributes = userPoolClientStrings(strings.Repeat("w", 2048))
	resource.SupportedIdentityProviders = userPoolClientStrings(strings.Repeat("p", 32))

	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestUserPoolClientCollectionLimits(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*UserPoolClientResource)
		want   string
	}{
		{
			name: "oauth flow count",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthFlows = userPoolClientStrings("code", "code", "code", "code")
			},
			want: "allowed-oauth-flows",
		},
		{
			name: "scope count",
			mutate: func(r *UserPoolClientResource) {
				values := make([]string, 51)
				for index := range values {
					values[index] = "openid"
				}
				r.AllowedOAuthScopes = &values
			},
			want: "allowed-oauth-scopes",
		},
		{
			name: "callback count",
			mutate: func(r *UserPoolClientResource) {
				values := make([]string, 101)
				for index := range values {
					values[index] = "https://example.com/callback"
				}
				r.CallbackURLs = &values
			},
			want: "callback-urls",
		},
		{
			name: "logout count",
			mutate: func(r *UserPoolClientResource) {
				values := make([]string, 101)
				for index := range values {
					values[index] = "logout"
				}
				r.LogoutURLs = &values
			},
			want: "logout-urls",
		},
		{
			name: "scope quote",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthScopes = userPoolClientStrings("bad\"scope")
			},
			want: "allowed-oauth-scopes[0]",
		},
		{
			name: "scope backslash",
			mutate: func(r *UserPoolClientResource) {
				r.AllowedOAuthScopes = userPoolClientStrings(`bad\scope`)
			},
			want: "allowed-oauth-scopes[0]",
		},
		{
			name: "callback too long",
			mutate: func(r *UserPoolClientResource) {
				r.CallbackURLs = userPoolClientStrings(
					"https://example.com/" + strings.Repeat("a", 1025),
				)
			},
			want: "callback-urls[0]",
		},
		{
			name: "read attribute too long",
			mutate: func(r *UserPoolClientResource) {
				r.ReadAttributes = userPoolClientStrings(strings.Repeat("a", 2049))
			},
			want: "read-attributes[0]",
		},
		{
			name: "rotation grace negative",
			mutate: func(r *UserPoolClientResource) {
				r.RefreshTokenRotation = &UserPoolClientRotation{
					Feature: "ENABLED", RetryGracePeriodSeconds: aws.Int32(-1),
				}
			},
			want: "retry-grace-period-seconds",
		},
		{
			name: "analytics external id too long",
			mutate: func(r *UserPoolClientResource) {
				r.AnalyticsConfiguration = validUserPoolClientAnalytics()
				r.AnalyticsConfiguration.ExternalID = aws.String(strings.Repeat("x", 131073))
			},
			want: "external-id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validUserPoolClientResource()
			resource.AllowedOAuthFlowsUserPoolClient = aws.Bool(true)
			tt.mutate(&resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestUserPoolClientAnalyticsARNForm(t *testing.T) {
	resource := validUserPoolClientResource()
	resource.AnalyticsConfiguration = &UserPoolClientAnalytics{
		ApplicationARN: aws.String(
			"arn:aws:mobiletargeting:us-east-1:123456789012:apps/example",
		),
		UserDataShared: aws.Bool(false),
	}

	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestUserPoolClientAnalyticsRejectsInvalidARNs(t *testing.T) {
	applicationPrefix := "arn:aws:mobiletargeting:us-east-1:123456789012:apps/"
	rolePrefix := "arn:aws:iam::123456789012:role/"
	tests := []struct {
		name   string
		config func() *UserPoolClientAnalytics
		want   string
	}{
		{
			name: "application ARN below minimum",
			config: func() *UserPoolClientAnalytics {
				return &UserPoolClientAnalytics{ApplicationARN: aws.String("arn:a:b::1:x")}
			},
			want: "application-arn",
		},
		{
			name: "application ARN above maximum",
			config: func() *UserPoolClientAnalytics {
				return &UserPoolClientAnalytics{ApplicationARN: aws.String(
					applicationPrefix + strings.Repeat("a", 2049-len(applicationPrefix)),
				)}
			},
			want: "application-arn",
		},
		{
			name: "application ARN pattern",
			config: func() *UserPoolClientAnalytics {
				return &UserPoolClientAnalytics{
					ApplicationARN: aws.String(strings.Repeat("x", 20)),
				}
			},
			want: "application-arn",
		},
		{
			name: "role ARN below minimum",
			config: func() *UserPoolClientAnalytics {
				config := validUserPoolClientAnalytics()
				config.RoleARN = aws.String("arn:a:b::1:x")
				return config
			},
			want: "role-arn",
		},
		{
			name: "role ARN above maximum",
			config: func() *UserPoolClientAnalytics {
				config := validUserPoolClientAnalytics()
				config.RoleARN = aws.String(
					rolePrefix + strings.Repeat("a", 2049-len(rolePrefix)),
				)
				return config
			},
			want: "role-arn",
		},
		{
			name: "role ARN pattern",
			config: func() *UserPoolClientAnalytics {
				config := validUserPoolClientAnalytics()
				config.RoleARN = aws.String(strings.Repeat("x", 20))
				return config
			},
			want: "role-arn",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validUserPoolClientResource()
			resource.AnalyticsConfiguration = tt.config()

			err := resource.ValidateInputs(context.Background(), nil)

			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func validUserPoolClientResource() UserPoolClientResource {
	return UserPoolClientResource{UserPoolID: "us-east-1_example"}
}

func validUserPoolClientAnalytics() *UserPoolClientAnalytics {
	return &UserPoolClientAnalytics{
		ApplicationID: aws.String("abcdef"),
		ExternalID:    aws.String("external"),
		RoleARN:       aws.String("arn:aws:iam::123456789012:role/cognito-analytics"),
	}
}

func userPoolClientStrings(values ...string) *[]string {
	return &values
}
