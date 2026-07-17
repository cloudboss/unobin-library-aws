package cognitoidp

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
)

var (
	userPoolClientPoolIDPattern   = regexp.MustCompile(`^[\w-]+_[0-9A-Za-z]+$`)
	userPoolClientNamePattern     = regexp.MustCompile(`^[\w\s+=,.@-]+$`)
	userPoolClientRedirectPattern = regexp.MustCompile(
		`^[\p{L}\p{M}\p{S}\p{N}\p{P}]+$`,
	)
	userPoolClientScopePattern    = regexp.MustCompile(`^[\x21\x23-\x5B\x5D-\x7E]+$`)
	userPoolClientProviderPattern = regexp.MustCompile(
		`^[\p{L}\p{M}\p{S}\p{N}\p{P}\p{Z}]+$`,
	)
	userPoolClientARNPattern = regexp.MustCompile(
		`^arn:[\w+=/,.@-]+:[\w+=/,.@-]+:([\w+=/,.@-]*)?:[0-9]+:` +
			`[\w+=/,.@-]+(:[\w+=/,.@-]+)?(:[\w+=/,.@-]+)?$`,
	)
	userPoolClientHexPattern = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

func (r *UserPoolClientResource) validateInputs() error {
	if err := validateUserPoolClientIdentity(r); err != nil {
		return err
	}
	if err := validateUserPoolClientTokens(r); err != nil {
		return err
	}
	if err := validateUserPoolClientCollections(r); err != nil {
		return err
	}
	if err := validateUserPoolClientOAuth(r); err != nil {
		return err
	}
	if err := validateUserPoolClientAuth(r); err != nil {
		return err
	}
	return validateUserPoolClientAnalytics(r.AnalyticsConfiguration)
}

func validateUserPoolClientIdentity(r *UserPoolClientResource) error {
	length := utf8.RuneCountInString(r.UserPoolID)
	if length < 1 || length > 55 || !userPoolClientPoolIDPattern.MatchString(r.UserPoolID) {
		return fmt.Errorf("user-pool-id must be a valid 1 to 55 character user pool ID")
	}
	if r.Name == nil {
		return nil
	}
	length = utf8.RuneCountInString(*r.Name)
	if length < 1 || length > 128 || !userPoolClientNamePattern.MatchString(*r.Name) {
		return fmt.Errorf("name must be 1 to 128 characters using the supported pattern")
	}
	return nil
}

func validateUserPoolClientTokens(r *UserPoolClientResource) error {
	if err := validateInt32Range(
		"access-token-validity", r.AccessTokenValidity, 1, 86400,
	); err != nil {
		return err
	}
	if err := validateInt32Range("id-token-validity", r.IDTokenValidity, 1, 86400); err != nil {
		return err
	}
	if err := validateInt32Range(
		"refresh-token-validity", r.RefreshTokenValidity, 1, 315360000,
	); err != nil {
		return err
	}
	if err := validateInt32Range("auth-session-validity", r.AuthSessionValidity, 3, 15); err != nil {
		return err
	}
	if err := validateUserPoolClientUnits(r.TokenValidityUnits); err != nil {
		return err
	}
	access := effectiveTokenSeconds(r.AccessTokenValidity, clientAccessUnit(r), 1, "hours")
	idToken := effectiveTokenSeconds(r.IDTokenValidity, clientIDUnit(r), 1, "hours")
	refresh := effectiveTokenSeconds(
		r.RefreshTokenValidity,
		clientRefreshUnit(r),
		30,
		"days",
	)
	if access < 300 || access > 86400 {
		return fmt.Errorf("access-token-validity must resolve to 5 minutes through 24 hours")
	}
	if idToken < 300 || idToken > 86400 {
		return fmt.Errorf("id-token-validity must resolve to 5 minutes through 24 hours")
	}
	if refresh < 3600 || refresh > 315360000 {
		return fmt.Errorf("refresh-token-validity must resolve to 1 hour through 10 years")
	}
	if access > refresh {
		return fmt.Errorf("access-token-validity must not exceed refresh-token-validity")
	}
	if idToken > refresh {
		return fmt.Errorf("id-token-validity must not exceed refresh-token-validity")
	}
	return nil
}

func validateInt32Range(name string, value *int32, minimum, maximum int32) error {
	if value != nil && (*value < minimum || *value > maximum) {
		return fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return nil
}

func validateUserPoolClientUnits(units *UserPoolClientValidityUnits) error {
	if units == nil {
		return nil
	}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{name: "access-token", value: units.AccessToken},
		{name: "id-token", value: units.IDToken},
		{name: "refresh-token", value: units.RefreshToken},
	} {
		if field.value != nil && !slices.Contains(
			[]string{"seconds", "minutes", "hours", "days"},
			*field.value,
		) {
			return fmt.Errorf(
				"token-validity-units.%s must be seconds, minutes, hours, or days",
				field.name,
			)
		}
	}
	return nil
}

func effectiveTokenSeconds(
	value *int32,
	unit *string,
	defaultValue int32,
	defaultUnit string,
) int64 {
	if value == nil {
		return int64(defaultValue) * tokenUnitSeconds(defaultUnit)
	}
	selectedUnit := defaultUnit
	if unit != nil {
		selectedUnit = *unit
	}
	return int64(*value) * tokenUnitSeconds(selectedUnit)
}

func tokenUnitSeconds(unit string) int64 {
	switch unit {
	case "seconds":
		return 1
	case "minutes":
		return 60
	case "hours":
		return 60 * 60
	case "days":
		return 24 * 60 * 60
	default:
		return 0
	}
}

func clientAccessUnit(r *UserPoolClientResource) *string {
	if r.TokenValidityUnits == nil {
		return nil
	}
	return r.TokenValidityUnits.AccessToken
}

func clientIDUnit(r *UserPoolClientResource) *string {
	if r.TokenValidityUnits == nil {
		return nil
	}
	return r.TokenValidityUnits.IDToken
}

func clientRefreshUnit(r *UserPoolClientResource) *string {
	if r.TokenValidityUnits == nil {
		return nil
	}
	return r.TokenValidityUnits.RefreshToken
}

func validateUserPoolClientCollections(r *UserPoolClientResource) error {
	if err := validateOAuthFlows(r.AllowedOAuthFlows); err != nil {
		return err
	}
	if err := validateScopes(r.AllowedOAuthScopes); err != nil {
		return err
	}
	if err := validateRedirects("callback-urls", r.CallbackURLs, true); err != nil {
		return err
	}
	if err := validateRedirects("logout-urls", r.LogoutURLs, false); err != nil {
		return err
	}
	if err := validateStringItems("read-attributes", r.ReadAttributes, 1, 2048, nil); err != nil {
		return err
	}
	if err := validateStringItems("write-attributes", r.WriteAttributes, 1, 2048, nil); err != nil {
		return err
	}
	return validateStringItems(
		"supported-identity-providers",
		r.SupportedIdentityProviders,
		1,
		32,
		userPoolClientProviderPattern,
	)
}

func validateOAuthFlows(values *[]string) error {
	if values == nil {
		return nil
	}
	if len(*values) > 3 {
		return fmt.Errorf("allowed-oauth-flows must contain at most 3 entries")
	}
	for _, value := range *values {
		if !slices.Contains([]string{"code", "implicit", "client_credentials"}, value) {
			return fmt.Errorf("allowed-oauth-flows contains invalid value %q", value)
		}
	}
	return nil
}

func validateScopes(values *[]string) error {
	if values == nil {
		return nil
	}
	if len(*values) > 50 {
		return fmt.Errorf("allowed-oauth-scopes must contain at most 50 entries")
	}
	return validateStringItems(
		"allowed-oauth-scopes", values, 1, 256, userPoolClientScopePattern,
	)
}

func validateRedirects(name string, values *[]string, requireURL bool) error {
	if values == nil {
		return nil
	}
	if len(*values) > 100 {
		return fmt.Errorf("%s must contain at most 100 entries", name)
	}
	for index, value := range *values {
		length := utf8.RuneCountInString(value)
		if length < 1 || length > 1024 || !userPoolClientRedirectPattern.MatchString(value) {
			return fmt.Errorf("%s[%d] must use the supported 1 to 1024 character pattern",
				name, index)
		}
		if requireURL {
			if err := validateCallbackURL(value); err != nil {
				return fmt.Errorf("%s[%d] must be a valid callback URL: %w", name, index, err)
			}
		}
	}
	return nil
}

func validateCallbackURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Scheme == "" {
		return fmt.Errorf("URL must be absolute")
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("URL must not contain a fragment")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		if parsed.Host == "" {
			return fmt.Errorf("HTTPS URL must contain a host")
		}
	case "http":
		host := strings.ToLower(parsed.Hostname())
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return fmt.Errorf("HTTP URL must use a documented loopback host")
		}
	default:
		if parsed.Host == "" && parsed.Opaque == "" {
			return fmt.Errorf("custom-scheme URL must contain a destination")
		}
	}
	return nil
}

func validateStringItems(
	name string,
	values *[]string,
	minimum int,
	maximum int,
	pattern *regexp.Regexp,
) error {
	if values == nil {
		return nil
	}
	for index, value := range *values {
		length := utf8.RuneCountInString(value)
		if length < minimum || length > maximum || pattern != nil && !pattern.MatchString(value) {
			return fmt.Errorf("%s[%d] must use the supported %d to %d character pattern",
				name, index, minimum, maximum)
		}
	}
	return nil
}

func validateUserPoolClientOAuth(r *UserPoolClientResource) error {
	oauthConfigured := listHasItems(r.AllowedOAuthFlows) ||
		listHasItems(r.AllowedOAuthScopes) ||
		listHasItems(r.CallbackURLs) ||
		listHasItems(r.LogoutURLs) ||
		r.DefaultRedirectURI != nil
	if oauthConfigured && !aws.ToBool(r.AllowedOAuthFlowsUserPoolClient) {
		return fmt.Errorf("OAuth settings require allowed-oauth-flows-user-pool-client true")
	}
	if r.DefaultRedirectURI != nil &&
		(r.CallbackURLs == nil || !slices.Contains(*r.CallbackURLs, *r.DefaultRedirectURI)) {
		return fmt.Errorf("default-redirect-uri must exactly match a callback-urls entry")
	}
	flows := []string(nil)
	if r.AllowedOAuthFlows != nil {
		flows = *r.AllowedOAuthFlows
	}
	if slices.Contains(flows, "client_credentials") {
		if len(flows) != 1 {
			return fmt.Errorf("client_credentials must be the only allowed OAuth flow")
		}
		if !aws.ToBool(r.GenerateSecret) {
			return fmt.Errorf("client_credentials requires generate-secret true")
		}
	}
	if aws.ToBool(r.EnablePropagateAdditionalUserContextData) &&
		!aws.ToBool(r.GenerateSecret) {
		return fmt.Errorf(
			"enable-propagate-additional-user-context-data requires generate-secret true",
		)
	}
	return nil
}

func listHasItems(values *[]string) bool {
	return values != nil && len(*values) > 0
}

func validateUserPoolClientAuth(r *UserPoolClientResource) error {
	if r.PreventUserExistenceErrors != nil &&
		!slices.Contains([]string{"LEGACY", "ENABLED"}, *r.PreventUserExistenceErrors) {
		return fmt.Errorf("prevent-user-existence-errors must be LEGACY or ENABLED")
	}
	if r.ExplicitAuthFlows != nil {
		legacy := false
		current := false
		allowed := []string{
			"ADMIN_NO_SRP_AUTH",
			"CUSTOM_AUTH_FLOW_ONLY",
			"USER_PASSWORD_AUTH",
			"ALLOW_ADMIN_USER_PASSWORD_AUTH",
			"ALLOW_CUSTOM_AUTH",
			"ALLOW_USER_PASSWORD_AUTH",
			"ALLOW_USER_SRP_AUTH",
			"ALLOW_REFRESH_TOKEN_AUTH",
			"ALLOW_USER_AUTH",
		}
		for _, value := range *r.ExplicitAuthFlows {
			if !slices.Contains(allowed, value) {
				return fmt.Errorf("explicit-auth-flows contains invalid value %q", value)
			}
			if strings.HasPrefix(value, "ALLOW_") {
				current = true
			} else {
				legacy = true
			}
		}
		if legacy && current {
			return fmt.Errorf("legacy explicit-auth-flows cannot mix with ALLOW_ values")
		}
	}
	rotation := r.RefreshTokenRotation
	if rotation == nil {
		return nil
	}
	if !slices.Contains([]string{"ENABLED", "DISABLED"}, rotation.Feature) {
		return fmt.Errorf("refresh-token-rotation.feature must be ENABLED or DISABLED")
	}
	if rotation.RetryGracePeriodSeconds != nil &&
		(*rotation.RetryGracePeriodSeconds < 0 || *rotation.RetryGracePeriodSeconds > 60) {
		return fmt.Errorf("refresh-token-rotation.retry-grace-period-seconds must be 0 to 60")
	}
	if rotation.Feature == "ENABLED" && r.ExplicitAuthFlows != nil &&
		slices.Contains(*r.ExplicitAuthFlows, "ALLOW_REFRESH_TOKEN_AUTH") {
		return fmt.Errorf(
			"refresh-token-rotation ENABLED conflicts with ALLOW_REFRESH_TOKEN_AUTH",
		)
	}
	return nil
}

func validateUserPoolClientAnalytics(config *UserPoolClientAnalytics) error {
	if config == nil {
		return nil
	}
	hasARN := config.ApplicationARN != nil
	hasTripletMember := config.ApplicationID != nil || config.ExternalID != nil ||
		config.RoleARN != nil
	if hasARN && hasTripletMember {
		return fmt.Errorf("analytics-configuration application-arn conflicts with the triplet")
	}
	if hasARN {
		return validateUserPoolClientARN(
			"analytics-configuration.application-arn", *config.ApplicationARN,
		)
	}
	if config.ApplicationID == nil || config.ExternalID == nil || config.RoleARN == nil {
		return fmt.Errorf(
			"analytics-configuration requires application-arn or application-id, external-id, and role-arn",
		)
	}
	if !userPoolClientHexPattern.MatchString(*config.ApplicationID) {
		return fmt.Errorf("analytics-configuration.application-id must be hexadecimal")
	}
	if utf8.RuneCountInString(*config.ExternalID) > 131072 {
		return fmt.Errorf("analytics-configuration.external-id must be at most 131072 characters")
	}
	return validateUserPoolClientARN("analytics-configuration.role-arn", *config.RoleARN)
}

func validateUserPoolClientARN(name, value string) error {
	length := utf8.RuneCountInString(value)
	if length < 20 || length > 2048 || !userPoolClientARNPattern.MatchString(value) {
		return fmt.Errorf("%s must be a valid 20 to 2048 character ARN", name)
	}
	return nil
}
