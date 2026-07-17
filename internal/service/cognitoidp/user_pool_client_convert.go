package cognitoidp

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

func (r *UserPoolClientResource) createInput(
	name string,
) *cognitoidentityprovider.CreateUserPoolClientInput {
	return &cognitoidentityprovider.CreateUserPoolClientInput{
		UserPoolId:                               aws.String(r.UserPoolID),
		ClientName:                               aws.String(name),
		GenerateSecret:                           aws.ToBool(r.GenerateSecret),
		AccessTokenValidity:                      r.AccessTokenValidity,
		IdTokenValidity:                          r.IDTokenValidity,
		RefreshTokenValidity:                     aws.ToInt32(r.RefreshTokenValidity),
		AuthSessionValidity:                      r.AuthSessionValidity,
		TokenValidityUnits:                       userPoolClientValidityUnits(r.TokenValidityUnits),
		AllowedOAuthFlows:                        userPoolClientOAuthFlows(r.AllowedOAuthFlows),
		AllowedOAuthScopes:                       copyUserPoolClientStrings(r.AllowedOAuthScopes),
		CallbackURLs:                             copyUserPoolClientStrings(r.CallbackURLs),
		LogoutURLs:                               copyUserPoolClientStrings(r.LogoutURLs),
		ExplicitAuthFlows:                        userPoolClientAuthFlows(r.ExplicitAuthFlows),
		ReadAttributes:                           copyUserPoolClientStrings(r.ReadAttributes),
		WriteAttributes:                          copyUserPoolClientStrings(r.WriteAttributes),
		SupportedIdentityProviders:               copyUserPoolClientStrings(r.SupportedIdentityProviders),
		AllowedOAuthFlowsUserPoolClient:          aws.ToBool(r.AllowedOAuthFlowsUserPoolClient),
		EnablePropagateAdditionalUserContextData: r.EnablePropagateAdditionalUserContextData,
		EnableTokenRevocation:                    r.EnableTokenRevocation,
		DefaultRedirectURI:                       r.DefaultRedirectURI,
		PreventUserExistenceErrors: userPoolClientPreventErrors(
			r.PreventUserExistenceErrors,
		),
		AnalyticsConfiguration: userPoolClientAnalytics(r.AnalyticsConfiguration),
		RefreshTokenRotation:   userPoolClientRotation(r.RefreshTokenRotation),
	}
}

func (r *UserPoolClientResource) updateInput(
	clientID string,
	name string,
	prior UserPoolClientResource,
) *cognitoidentityprovider.UpdateUserPoolClientInput {
	return &cognitoidentityprovider.UpdateUserPoolClientInput{
		UserPoolId:                               aws.String(r.UserPoolID),
		ClientId:                                 aws.String(clientID),
		ClientName:                               aws.String(name),
		AccessTokenValidity:                      r.AccessTokenValidity,
		IdTokenValidity:                          r.IDTokenValidity,
		RefreshTokenValidity:                     aws.ToInt32(r.RefreshTokenValidity),
		AuthSessionValidity:                      r.AuthSessionValidity,
		TokenValidityUnits:                       updateUserPoolClientValidityUnits(r, prior),
		AllowedOAuthFlows:                        userPoolClientOAuthFlows(r.AllowedOAuthFlows),
		AllowedOAuthScopes:                       copyUserPoolClientStrings(r.AllowedOAuthScopes),
		CallbackURLs:                             copyUserPoolClientStrings(r.CallbackURLs),
		LogoutURLs:                               copyUserPoolClientStrings(r.LogoutURLs),
		ExplicitAuthFlows:                        userPoolClientAuthFlows(r.ExplicitAuthFlows),
		ReadAttributes:                           copyUserPoolClientStrings(r.ReadAttributes),
		WriteAttributes:                          copyUserPoolClientStrings(r.WriteAttributes),
		SupportedIdentityProviders:               copyUserPoolClientStrings(r.SupportedIdentityProviders),
		AllowedOAuthFlowsUserPoolClient:          aws.ToBool(r.AllowedOAuthFlowsUserPoolClient),
		EnablePropagateAdditionalUserContextData: r.EnablePropagateAdditionalUserContextData,
		EnableTokenRevocation:                    r.EnableTokenRevocation,
		DefaultRedirectURI:                       r.DefaultRedirectURI,
		PreventUserExistenceErrors: userPoolClientPreventErrors(
			r.PreventUserExistenceErrors,
		),
		AnalyticsConfiguration: userPoolClientAnalytics(r.AnalyticsConfiguration),
		RefreshTokenRotation:   userPoolClientRotation(r.RefreshTokenRotation),
	}
}

func copyUserPoolClientStrings(values *[]string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, (*values)...)
}

func userPoolClientOAuthFlows(values *[]string) []cognitotypes.OAuthFlowType {
	if values == nil {
		return nil
	}
	out := make([]cognitotypes.OAuthFlowType, 0, len(*values))
	for _, value := range *values {
		out = append(out, cognitotypes.OAuthFlowType(value))
	}
	return out
}

func userPoolClientAuthFlows(values *[]string) []cognitotypes.ExplicitAuthFlowsType {
	if values == nil {
		return nil
	}
	out := make([]cognitotypes.ExplicitAuthFlowsType, 0, len(*values))
	for _, value := range *values {
		out = append(out, cognitotypes.ExplicitAuthFlowsType(value))
	}
	return out
}

func userPoolClientValidityUnits(
	value *UserPoolClientValidityUnits,
) *cognitotypes.TokenValidityUnitsType {
	if value == nil {
		return nil
	}
	return &cognitotypes.TokenValidityUnitsType{
		AccessToken:  userPoolClientTimeUnit(value.AccessToken),
		IdToken:      userPoolClientTimeUnit(value.IDToken),
		RefreshToken: userPoolClientTimeUnit(value.RefreshToken),
	}
}

func updateUserPoolClientValidityUnits(
	current *UserPoolClientResource,
	prior UserPoolClientResource,
) *cognitotypes.TokenValidityUnitsType {
	units := userPoolClientValidityUnits(current.TokenValidityUnits)
	priorUnits := prior.TokenValidityUnits
	if !hasUserPoolClientValidityUnit(priorUnits) {
		return units
	}
	if units == nil {
		units = &cognitotypes.TokenValidityUnitsType{}
	}
	if current.TokenValidityUnits == nil || current.TokenValidityUnits.AccessToken == nil {
		if priorUnits.AccessToken != nil {
			units.AccessToken = cognitotypes.TimeUnitsTypeHours
		}
	}
	if current.TokenValidityUnits == nil || current.TokenValidityUnits.IDToken == nil {
		if priorUnits.IDToken != nil {
			units.IdToken = cognitotypes.TimeUnitsTypeHours
		}
	}
	if current.TokenValidityUnits == nil || current.TokenValidityUnits.RefreshToken == nil {
		if priorUnits.RefreshToken != nil {
			units.RefreshToken = cognitotypes.TimeUnitsTypeDays
		}
	}
	return units
}

func hasUserPoolClientValidityUnit(value *UserPoolClientValidityUnits) bool {
	return value != nil &&
		(value.AccessToken != nil || value.IDToken != nil || value.RefreshToken != nil)
}

func userPoolClientTimeUnit(value *string) cognitotypes.TimeUnitsType {
	if value == nil {
		return ""
	}
	return cognitotypes.TimeUnitsType(*value)
}

func userPoolClientPreventErrors(
	value *string,
) cognitotypes.PreventUserExistenceErrorTypes {
	if value == nil {
		return ""
	}
	return cognitotypes.PreventUserExistenceErrorTypes(*value)
}

func userPoolClientAnalytics(
	value *UserPoolClientAnalytics,
) *cognitotypes.AnalyticsConfigurationType {
	if value == nil {
		return nil
	}
	return &cognitotypes.AnalyticsConfigurationType{
		ApplicationArn: value.ApplicationARN,
		ApplicationId:  value.ApplicationID,
		ExternalId:     value.ExternalID,
		RoleArn:        value.RoleARN,
		UserDataShared: aws.ToBool(value.UserDataShared),
	}
}

func userPoolClientRotation(
	value *UserPoolClientRotation,
) *cognitotypes.RefreshTokenRotationType {
	if value == nil {
		return nil
	}
	return &cognitotypes.RefreshTokenRotationType{
		Feature:                 cognitotypes.FeatureType(value.Feature),
		RetryGracePeriodSeconds: value.RetryGracePeriodSeconds,
	}
}
