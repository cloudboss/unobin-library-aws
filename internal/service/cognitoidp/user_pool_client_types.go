package cognitoidp

type UserPoolClientValidityUnits struct {
	AccessToken  *string `ub:"access-token"`
	IDToken      *string `ub:"id-token"`
	RefreshToken *string `ub:"refresh-token"`
}

type UserPoolClientAnalytics struct {
	ApplicationARN *string `ub:"application-arn"`
	ApplicationID  *string `ub:"application-id"`
	ExternalID     *string `ub:"external-id"`
	RoleARN        *string `ub:"role-arn"`
	UserDataShared *bool   `ub:"user-data-shared"`
}

type UserPoolClientRotation struct {
	Feature                 string `ub:"feature"`
	RetryGracePeriodSeconds *int32 `ub:"retry-grace-period-seconds"`
}

type UserPoolClientResource struct {
	UserPoolID           string                       `ub:"user-pool-id"`
	Name                 *string                      `ub:"name"`
	GenerateSecret       *bool                        `ub:"generate-secret"`
	AccessTokenValidity  *int32                       `ub:"access-token-validity"`
	IDTokenValidity      *int32                       `ub:"id-token-validity"`
	RefreshTokenValidity *int32                       `ub:"refresh-token-validity"`
	AuthSessionValidity  *int32                       `ub:"auth-session-validity"`
	TokenValidityUnits   *UserPoolClientValidityUnits `ub:"token-validity-units"`
	AllowedOAuthFlows    *[]string                    `ub:"allowed-oauth-flows"`
	AllowedOAuthScopes   *[]string                    `ub:"allowed-oauth-scopes"`
	CallbackURLs         *[]string                    `ub:"callback-urls"`
	LogoutURLs           *[]string                    `ub:"logout-urls"`
	ExplicitAuthFlows    *[]string                    `ub:"explicit-auth-flows"`
	ReadAttributes       *[]string                    `ub:"read-attributes"`
	WriteAttributes      *[]string                    `ub:"write-attributes"`

	SupportedIdentityProviders *[]string `ub:"supported-identity-providers"`

	AllowedOAuthFlowsUserPoolClient *bool `ub:"allowed-oauth-flows-user-pool-client"`

	EnablePropagateAdditionalUserContextData *bool `ub:"enable-propagate-additional-user-context-data"`

	EnableTokenRevocation *bool   `ub:"enable-token-revocation"`
	DefaultRedirectURI    *string `ub:"default-redirect-uri"`

	PreventUserExistenceErrors *string `ub:"prevent-user-existence-errors"`

	AnalyticsConfiguration *UserPoolClientAnalytics `ub:"analytics-configuration"`
	RefreshTokenRotation   *UserPoolClientRotation  `ub:"refresh-token-rotation"`
}

type UserPoolClientResourceOutput struct {
	ID           string  `ub:"id"`
	Name         string  `ub:"name"`
	ClientSecret *string `ub:"client-secret,sensitive"`
}
