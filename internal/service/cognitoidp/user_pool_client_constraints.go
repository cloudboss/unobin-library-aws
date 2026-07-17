package cognitoidp

import "github.com/cloudboss/unobin/pkg/constraint"

func (r UserPoolClientResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.MinItems(r.UserPoolID, 1)).
			Message("user-pool-id must contain at least 1 character"),
		constraint.Must(constraint.MaxItems(r.UserPoolID, 55)).
			Message("user-pool-id must contain at most 55 characters"),
		constraint.Must(constraint.MinItems(r.Name, 1)).
			Message("name must contain at least 1 character"),
		constraint.Must(constraint.MaxItems(r.Name, 128)).
			Message("name must contain at most 128 characters"),
		constraint.When(constraint.Present(r.AccessTokenValidity)).Require(
			constraint.AtLeast(r.AccessTokenValidity, 1),
			constraint.AtMost(r.AccessTokenValidity, 86400),
		).Message("access-token-validity must be between 1 and 86400"),
		constraint.When(constraint.Present(r.IDTokenValidity)).Require(
			constraint.AtLeast(r.IDTokenValidity, 1),
			constraint.AtMost(r.IDTokenValidity, 86400),
		).Message("id-token-validity must be between 1 and 86400"),
		constraint.When(constraint.Present(r.RefreshTokenValidity)).Require(
			constraint.AtLeast(r.RefreshTokenValidity, 1),
			constraint.AtMost(r.RefreshTokenValidity, 315360000),
		).Message("refresh-token-validity must be between 1 and 315360000"),
		constraint.When(constraint.Present(r.AuthSessionValidity)).Require(
			constraint.AtLeast(r.AuthSessionValidity, 3),
			constraint.AtMost(r.AuthSessionValidity, 15),
		).Message("auth-session-validity must be between 3 and 15"),
		constraint.When(constraint.Present(r.TokenValidityUnits.AccessToken)).Require(
			constraint.OneOf(
				r.TokenValidityUnits.AccessToken,
				"seconds",
				"minutes",
				"hours",
				"days",
			),
		).Message("access-token unit is invalid"),
		constraint.When(constraint.Present(r.TokenValidityUnits.IDToken)).Require(
			constraint.OneOf(
				r.TokenValidityUnits.IDToken,
				"seconds",
				"minutes",
				"hours",
				"days",
			),
		).Message("id-token unit is invalid"),
		constraint.When(constraint.Present(r.TokenValidityUnits.RefreshToken)).Require(
			constraint.OneOf(
				r.TokenValidityUnits.RefreshToken,
				"seconds",
				"minutes",
				"hours",
				"days",
			),
		).Message("refresh-token unit is invalid"),
		constraint.Must(constraint.MaxItems(r.AllowedOAuthFlows, 3)).
			Message("allowed-oauth-flows holds at most 3 entries"),
		constraint.ForEach(r.AllowedOAuthFlows, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(
					value,
					"code",
					"implicit",
					"client_credentials",
				)).Message("allowed-oauth-flows contains an invalid value"),
			}
		}),
		constraint.Must(constraint.MaxItems(r.AllowedOAuthScopes, 50)).
			Message("allowed-oauth-scopes holds at most 50 entries"),
		constraint.ForEach(r.AllowedOAuthScopes, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(
					constraint.MinItems(value, 1),
					constraint.MaxItems(value, 256),
				).Message("allowed-oauth-scopes entries must contain 1 to 256 characters"),
			}
		}),
		constraint.Must(constraint.MaxItems(r.CallbackURLs, 100)).
			Message("callback-urls holds at most 100 entries"),
		constraint.ForEach(r.CallbackURLs, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(
					constraint.MinItems(value, 1),
					constraint.MaxItems(value, 1024),
				).Message("callback-urls entries must contain 1 to 1024 characters"),
			}
		}),
		constraint.Must(constraint.MaxItems(r.LogoutURLs, 100)).
			Message("logout-urls holds at most 100 entries"),
		constraint.ForEach(r.LogoutURLs, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(
					constraint.MinItems(value, 1),
					constraint.MaxItems(value, 1024),
				).Message("logout-urls entries must contain 1 to 1024 characters"),
			}
		}),
		constraint.ForEach(r.ExplicitAuthFlows, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(
					value,
					"ADMIN_NO_SRP_AUTH",
					"CUSTOM_AUTH_FLOW_ONLY",
					"USER_PASSWORD_AUTH",
					"ALLOW_ADMIN_USER_PASSWORD_AUTH",
					"ALLOW_CUSTOM_AUTH",
					"ALLOW_USER_PASSWORD_AUTH",
					"ALLOW_USER_SRP_AUTH",
					"ALLOW_REFRESH_TOKEN_AUTH",
					"ALLOW_USER_AUTH",
				)).Message("explicit-auth-flows contains an invalid value"),
			}
		}),
		constraint.ForEach(r.ReadAttributes, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(
					constraint.MinItems(value, 1),
					constraint.MaxItems(value, 2048),
				).Message("read-attributes entries must contain 1 to 2048 characters"),
			}
		}),
		constraint.ForEach(r.WriteAttributes, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(
					constraint.MinItems(value, 1),
					constraint.MaxItems(value, 2048),
				).Message("write-attributes entries must contain 1 to 2048 characters"),
			}
		}),
		constraint.ForEach(
			r.SupportedIdentityProviders,
			func(value string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(
						constraint.MinItems(value, 1),
						constraint.MaxItems(value, 32),
					).Message(
						"supported-identity-providers entries must contain 1 to 32 characters",
					),
				}
			},
		),
		constraint.When(constraint.Any(
			constraint.NotEmpty(r.AllowedOAuthFlows),
			constraint.NotEmpty(r.AllowedOAuthScopes),
			constraint.NotEmpty(r.CallbackURLs),
			constraint.NotEmpty(r.LogoutURLs),
			constraint.Present(r.DefaultRedirectURI),
		)).Require(constraint.IsTrue(r.AllowedOAuthFlowsUserPoolClient)).
			Message("OAuth settings require allowed-oauth-flows-user-pool-client true"),
		constraint.When(
			constraint.IsTrue(r.EnablePropagateAdditionalUserContextData),
		).Require(constraint.IsTrue(r.GenerateSecret)).
			Message("propagated user context data requires generate-secret true"),
		constraint.When(constraint.Present(r.PreventUserExistenceErrors)).Require(
			constraint.OneOf(r.PreventUserExistenceErrors, "LEGACY", "ENABLED"),
		).Message("prevent-user-existence-errors must be LEGACY or ENABLED"),
		constraint.When(constraint.Present(r.RefreshTokenRotation)).Require(
			constraint.OneOf(r.RefreshTokenRotation.Feature, "ENABLED", "DISABLED"),
		).Message("refresh-token-rotation feature must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(
			r.RefreshTokenRotation.RetryGracePeriodSeconds,
		)).Require(
			constraint.AtLeast(r.RefreshTokenRotation.RetryGracePeriodSeconds, 0),
			constraint.AtMost(r.RefreshTokenRotation.RetryGracePeriodSeconds, 60),
		).Message("refresh-token-rotation grace must be between 0 and 60"),
		constraint.When(constraint.Present(r.AnalyticsConfiguration)).Require(
			constraint.Any(
				constraint.Present(r.AnalyticsConfiguration.ApplicationARN),
				constraint.All(
					constraint.Present(r.AnalyticsConfiguration.ApplicationID),
					constraint.Present(r.AnalyticsConfiguration.ExternalID),
					constraint.Present(r.AnalyticsConfiguration.RoleARN),
				),
			),
		).Message("analytics-configuration requires an ARN or application triplet"),
		constraint.ForbiddenWith(
			r.AnalyticsConfiguration.ApplicationARN,
			r.AnalyticsConfiguration.ApplicationID,
			r.AnalyticsConfiguration.ExternalID,
			r.AnalyticsConfiguration.RoleARN,
		).Message("analytics application-arn conflicts with the application triplet"),
		constraint.RequiredTogether(
			r.AnalyticsConfiguration.ApplicationID,
			r.AnalyticsConfiguration.ExternalID,
			r.AnalyticsConfiguration.RoleARN,
		).Message("analytics application triplet fields must be set together"),
		constraint.Must(
			constraint.MinItems(r.AnalyticsConfiguration.ApplicationARN, 20),
			constraint.MaxItems(r.AnalyticsConfiguration.ApplicationARN, 2048),
		).Message("analytics application-arn must contain 20 to 2048 characters"),
		constraint.Must(
			constraint.MinItems(r.AnalyticsConfiguration.RoleARN, 20),
			constraint.MaxItems(r.AnalyticsConfiguration.RoleARN, 2048),
		).Message("analytics role-arn must contain 20 to 2048 characters"),
		constraint.Must(
			constraint.MaxItems(r.AnalyticsConfiguration.ExternalID, 131072),
		).Message("analytics external-id must contain at most 131072 characters"),
	}
}
