package cognitoidp

import "github.com/cloudboss/unobin/pkg/constraint"

func (r UserPoolResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.MinItems(r.Name, 1)).
			Message("name must contain at least 1 character"),
		constraint.Must(constraint.MaxItems(r.Name, 128)).
			Message("name must contain at most 128 characters"),
		constraint.Must(constraint.MaxItems(r.AliasAttributes, 3)).
			Message("alias-attributes holds at most 3 entries"),
		constraint.ForEach(r.AliasAttributes, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(
					value, "email", "phone_number", "preferred_username",
				)).Message("alias-attributes values must be valid alias attributes"),
			}
		}),
		constraint.Must(constraint.MaxItems(r.UsernameAttributes, 2)).
			Message("username-attributes holds at most 2 entries"),
		constraint.ForEach(r.UsernameAttributes, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(value, "email", "phone_number")).
					Message("username-attributes values must be email or phone_number"),
			}
		}),
		constraint.ForbiddenWith(r.AliasAttributes, r.UsernameAttributes).
			Message("alias-attributes conflicts with username-attributes"),
		constraint.Must(constraint.MaxItems(r.AutoVerifiedAttributes, 2)).
			Message("auto-verified-attributes holds at most 2 entries"),
		constraint.ForEach(r.AutoVerifiedAttributes, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(value, "email", "phone_number")).
					Message("auto-verified-attributes values must be email or phone_number"),
			}
		}),
		constraint.When(constraint.Present(r.DeletionProtection)).
			Require(constraint.OneOf(r.DeletionProtection, "ACTIVE", "INACTIVE")).
			Message("deletion-protection must be ACTIVE or INACTIVE"),
		constraint.When(constraint.Present(r.AccountRecoverySetting)).Require(
			constraint.MinItems(r.AccountRecoverySetting.RecoveryMechanisms, 1),
			constraint.MaxItems(r.AccountRecoverySetting.RecoveryMechanisms, 2),
		).Message("recovery-mechanisms must contain 1 or 2 entries"),
		constraint.ForEach(
			r.AccountRecoverySetting.RecoveryMechanisms,
			func(value UserPoolRecoveryMechanism) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(
						value.Name,
						"admin_only",
						"verified_email",
						"verified_phone_number",
					)).Message("recovery mechanism name is invalid"),
					constraint.Must(
						constraint.AtLeast(value.Priority, 1),
						constraint.AtMost(value.Priority, 2),
					).Message("recovery mechanism priority must be 1 or 2"),
				}
			},
		),
		constraint.ForbiddenWith(
			r.AdminCreateUserConfig.UnusedAccountValidityDays,
			r.PasswordPolicy.TemporaryPasswordValidityDays,
		).Message(
			"unused-account-validity-days conflicts with temporary-password-validity-days",
		),
		constraint.When(constraint.Present(
			r.AdminCreateUserConfig.UnusedAccountValidityDays,
		)).Require(
			constraint.AtLeast(r.AdminCreateUserConfig.UnusedAccountValidityDays, 0),
			constraint.AtMost(r.AdminCreateUserConfig.UnusedAccountValidityDays, 365),
		).Message("unused-account-validity-days must be between 0 and 365"),
		constraint.When(constraint.Present(r.EmailConfiguration.EmailSendingAccount)).
			Require(constraint.OneOf(
				r.EmailConfiguration.EmailSendingAccount,
				"COGNITO_DEFAULT",
				"DEVELOPER",
			)).Message("email-sending-account must be COGNITO_DEFAULT or DEVELOPER"),
		constraint.When(constraint.Present(r.LambdaConfig.CustomEmailSender)).
			Require(constraint.Equals(
				r.LambdaConfig.CustomEmailSender.LambdaVersion, "V1_0",
			)).Message("custom-email-sender lambda-version must be V1_0"),
		constraint.When(constraint.Present(r.LambdaConfig.CustomSMSSender)).
			Require(constraint.Equals(
				r.LambdaConfig.CustomSMSSender.LambdaVersion, "V1_0",
			)).Message("custom-sms-sender lambda-version must be V1_0"),
		constraint.When(constraint.Present(r.LambdaConfig.PreTokenGenerationConfig)).
			Require(constraint.OneOf(
				r.LambdaConfig.PreTokenGenerationConfig.LambdaVersion,
				"V1_0",
				"V2_0",
				"V3_0",
			)).Message("pre-token-generation-config lambda-version is invalid"),
		constraint.When(constraint.Present(r.LambdaConfig.InboundFederation)).
			Require(constraint.Equals(
				r.LambdaConfig.InboundFederation.LambdaVersion, "V1_0",
			)).Message("inbound-federation lambda-version must be V1_0"),
		constraint.When(constraint.Any(
			constraint.Present(r.LambdaConfig.CustomEmailSender),
			constraint.Present(r.LambdaConfig.CustomSMSSender),
		)).Require(constraint.Present(r.LambdaConfig.KMSKeyID)).
			Message("custom senders require kms-key-id"),
		constraint.When(constraint.All(
			constraint.Present(r.LambdaConfig.PreTokenGeneration),
			constraint.Present(r.LambdaConfig.PreTokenGenerationConfig),
		)).Require(constraint.Equals(
			r.LambdaConfig.PreTokenGenerationConfig.LambdaARN,
			r.LambdaConfig.PreTokenGeneration,
		)).Message("pre-token-generation Lambda ARNs must match"),
		constraint.When(constraint.Present(r.MFAConfiguration)).
			Require(constraint.OneOf(r.MFAConfiguration, "OFF", "ON", "OPTIONAL")).
			Message("mfa-configuration must be OFF, ON, or OPTIONAL"),
		constraint.Must(constraint.MaxItems(r.EnabledMFAs, 3)).
			Message("enabled-mfas holds at most 3 entries"),
		constraint.ForEach(r.EnabledMFAs, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(
					value, "SMS_MFA", "EMAIL_OTP", "SOFTWARE_TOKEN_MFA",
				)).Message("enabled-mfas contains an invalid factor"),
			}
		}),
		constraint.When(constraint.OneOf(r.MFAConfiguration, "ON", "OPTIONAL")).
			Require(constraint.Any(
				constraint.NotEmpty(r.EnabledMFAs),
				constraint.Equals(
					r.WebAuthnConfiguration.FactorConfiguration,
					"MULTI_FACTOR_WITH_USER_VERIFICATION",
				),
			)).Message("ON or OPTIONAL MFA requires an enabled factor"),
		constraint.When(constraint.Any(
			constraint.Absent(r.MFAConfiguration),
			constraint.Equals(r.MFAConfiguration, "OFF"),
		)).Require(
			constraint.MaxItems(r.EnabledMFAs, 0),
			constraint.Not(constraint.Equals(
				r.WebAuthnConfiguration.FactorConfiguration,
				"MULTI_FACTOR_WITH_USER_VERIFICATION",
			)),
		).Message("OFF MFA forbids enabled factors"),
		constraint.When(constraint.Present(r.WebAuthnConfiguration.FactorConfiguration)).
			Require(constraint.OneOf(
				r.WebAuthnConfiguration.FactorConfiguration,
				"SINGLE_FACTOR",
				"MULTI_FACTOR_WITH_USER_VERIFICATION",
			)).Message("web-authn factor-configuration is invalid"),
		constraint.When(constraint.Present(r.WebAuthnConfiguration.UserVerification)).
			Require(constraint.OneOf(
				r.WebAuthnConfiguration.UserVerification, "required", "preferred",
			)).Message("web-authn user-verification must be required or preferred"),
		constraint.When(constraint.Present(r.WebAuthnConfiguration.RelyingPartyID)).
			Require(
				constraint.MinItems(r.WebAuthnConfiguration.RelyingPartyID, 1),
				constraint.MaxItems(r.WebAuthnConfiguration.RelyingPartyID, 63),
			).Message("web-authn relying-party-id must be 1 to 63 characters"),
		constraint.When(constraint.Equals(
			r.WebAuthnConfiguration.FactorConfiguration,
			"MULTI_FACTOR_WITH_USER_VERIFICATION",
		)).Require(
			constraint.Equals(r.WebAuthnConfiguration.UserVerification, "required"),
			constraint.Any(
				constraint.Absent(r.UserPoolTier),
				constraint.OneOf(r.UserPoolTier, "ESSENTIALS", "PLUS"),
			),
		).Message("multi-factor WebAuthn requires verification and an eligible tier"),
		constraint.When(constraint.Present(r.PasswordPolicy.MinimumLength)).Require(
			constraint.AtLeast(r.PasswordPolicy.MinimumLength, 6),
			constraint.AtMost(r.PasswordPolicy.MinimumLength, 99),
		).Message("password minimum-length must be between 6 and 99"),
		constraint.When(constraint.Present(r.PasswordPolicy.PasswordHistorySize)).Require(
			constraint.AtLeast(r.PasswordPolicy.PasswordHistorySize, 0),
			constraint.AtMost(r.PasswordPolicy.PasswordHistorySize, 24),
		).Message("password-history-size must be between 0 and 24"),
		constraint.When(constraint.Present(
			r.PasswordPolicy.TemporaryPasswordValidityDays,
		)).Require(
			constraint.AtLeast(r.PasswordPolicy.TemporaryPasswordValidityDays, 0),
			constraint.AtMost(r.PasswordPolicy.TemporaryPasswordValidityDays, 365),
		).Message("temporary-password-validity-days must be between 0 and 365"),
		constraint.When(constraint.All(
			constraint.Present(r.PasswordPolicy.PasswordHistorySize),
			constraint.Above(r.PasswordPolicy.PasswordHistorySize, 0),
		)).
			Require(constraint.OneOf(
				r.UserPoolAddOns.AdvancedSecurityMode, "AUDIT", "ENFORCED",
			)).Message("password history requires advanced security"),
		constraint.When(constraint.Present(r.SignInPolicy)).Require(
			constraint.MinItems(r.SignInPolicy.AllowedFirstAuthFactors, 1),
			constraint.MaxItems(r.SignInPolicy.AllowedFirstAuthFactors, 4),
		).Message("allowed-first-auth-factors must contain 1 to 4 entries"),
		constraint.ForEach(
			r.SignInPolicy.AllowedFirstAuthFactors,
			func(value string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(
						value, "PASSWORD", "EMAIL_OTP", "SMS_OTP", "WEB_AUTHN",
					)).Message("allowed-first-auth-factors contains an invalid factor"),
				}
			},
		),
		constraint.When(constraint.Present(r.SignInPolicy)).Require(constraint.Any(
			constraint.Absent(r.UserPoolTier),
			constraint.OneOf(r.UserPoolTier, "ESSENTIALS", "PLUS"),
		)).Message("sign-in-policy requires user-pool-tier ESSENTIALS or PLUS"),
		constraint.When(constraint.Present(r.Schema)).Require(
			constraint.MinItems(r.Schema, 1),
			constraint.MaxItems(r.Schema, 50),
		).Message("schema must contain 1 to 50 attributes"),
		constraint.ForEach(r.Schema, func(value UserPoolSchemaAttribute) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(
					value.AttributeDataType, "String", "Number", "DateTime", "Boolean",
				)).Message("schema attribute-data-type is invalid"),
				constraint.When(constraint.Present(value.StringAttributeConstraints)).
					Require(constraint.Equals(value.AttributeDataType, "String")).
					Message("string-attribute-constraints requires String"),
				constraint.When(constraint.Present(value.NumberAttributeConstraints)).
					Require(constraint.Equals(value.AttributeDataType, "Number")).
					Message("number-attribute-constraints requires Number"),
			}
		}),
		constraint.When(constraint.Present(
			r.VerificationMessageTemplate.DefaultEmailOption,
		)).Require(constraint.OneOf(
			r.VerificationMessageTemplate.DefaultEmailOption,
			"CONFIRM_WITH_CODE",
			"CONFIRM_WITH_LINK",
		)).Message("default-email-option must be CONFIRM_WITH_CODE or CONFIRM_WITH_LINK"),
		constraint.Must(constraint.MaxItems(
			r.UserAttributeUpdateSettings.AttributesRequireVerificationBeforeUpdate,
			2,
		)).Message("attributes-require-verification-before-update holds at most 2 entries"),
		constraint.ForEach(
			r.UserAttributeUpdateSettings.AttributesRequireVerificationBeforeUpdate,
			func(value string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(value, "email", "phone_number")).
						Message("verification attributes must be email or phone_number"),
				}
			},
		),
		constraint.When(constraint.Present(r.UserPoolAddOns)).Require(constraint.OneOf(
			r.UserPoolAddOns.AdvancedSecurityMode, "OFF", "AUDIT", "ENFORCED",
		)).Message("advanced-security-mode must be OFF, AUDIT, or ENFORCED"),
		constraint.When(constraint.Present(
			r.UserPoolAddOns.AdvancedSecurityAdditionalFlows.CustomAuthMode,
		)).Require(constraint.OneOf(
			r.UserPoolAddOns.AdvancedSecurityAdditionalFlows.CustomAuthMode,
			"AUDIT",
			"ENFORCED",
		)).Message("custom-auth-mode must be AUDIT or ENFORCED"),
		constraint.When(constraint.All(
			constraint.Present(r.UserPoolAddOns),
			constraint.NotEquals(r.UserPoolAddOns.AdvancedSecurityMode, "OFF"),
		)).Require(constraint.Equals(r.UserPoolTier, "PLUS")).
			Message("advanced security requires user-pool-tier PLUS"),
		constraint.When(constraint.Present(r.UserPoolTier)).Require(constraint.OneOf(
			r.UserPoolTier, "LITE", "ESSENTIALS", "PLUS",
		)).Message("user-pool-tier must be LITE, ESSENTIALS, or PLUS"),
		constraint.Must(constraint.MaxItems(r.Tags, 50)).
			Message("tags holds at most 50 entries"),
	}
}
