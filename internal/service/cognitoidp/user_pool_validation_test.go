package cognitoidp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPoolValidateInputsAcceptsCompleteConfiguration(t *testing.T) {
	r := completeUserPoolResource()
	require.NoError(t, r.ValidateInputs(context.Background(), nil))
}

func TestUserPoolValidateInputsAcceptsInboundFederationWithoutKMS(t *testing.T) {
	r := UserPoolResource{
		Name: "unobin-pool",
		LambdaConfig: &UserPoolLambdaConfiguration{
			InboundFederation: &UserPoolLambdaVersionConfiguration{
				LambdaARN:     "arn:aws:lambda:us-east-1:123456789012:function:inbound",
				LambdaVersion: "V1_0",
			},
		},
	}
	require.NoError(t, r.ValidateInputs(context.Background(), nil))
}

func TestUserPoolARNValidation(t *testing.T) {
	tests := []struct {
		name      string
		valid     string
		invalid   []string
		wantError string
		setARN    func(*UserPoolResource, string)
	}{
		{
			name:  "Lambda trigger",
			valid: "arn:aws:lambda:us-east-1:123456789012:function:handler",
			invalid: []string{
				"arn:aws123:lambda:us-east-1:123456789012:function:handler",
				"arn:aws:lambda:useast1:123456789012:function:handler",
				"arn:aws:lambda:us-east-1:123:function:handler",
				"arn:aws:lambda:us-east-1:123456789012:",
				"arn:aws:sns:us-east-1:123456789012:function:handler",
			},
			wantError: "lambda-config.post-confirmation",
			setARN: func(r *UserPoolResource, value string) {
				r.LambdaConfig = &UserPoolLambdaConfiguration{PostConfirmation: &value}
			},
		},
		{
			name:  "SES source",
			valid: "arn:aws:ses:us-east-1:123456789012:identity/example.com",
			invalid: []string{
				"arn:aws123:ses:us-east-1:123456789012:identity/example.com",
				"arn:aws:ses:useast1:123456789012:identity/example.com",
				"arn:aws:ses:us-east-1:123:identity/example.com",
				"arn:aws:ses:us-east-1:123456789012:",
				"arn:aws:sns:us-east-1:123456789012:identity/example.com",
			},
			wantError: "source-arn",
			setARN: func(r *UserPoolResource, value string) {
				r.EmailConfiguration = &UserPoolEmailConfiguration{SourceARN: &value}
			},
		},
		{
			name:  "IAM caller",
			valid: "arn:aws:iam::123456789012:role/cognito-sns",
			invalid: []string{
				"arn:aws123:iam::123456789012:role/cognito-sns",
				"arn:aws:iam:useast1:123456789012:role/cognito-sns",
				"arn:aws:iam::123:role/cognito-sns",
				"arn:aws:iam::123456789012:",
				"arn:aws:sns::123456789012:role/cognito-sns",
			},
			wantError: "sms-configuration.sns-caller-arn",
			setARN: func(r *UserPoolResource, value string) {
				r.SMSConfiguration = &UserPoolSMSConfiguration{SNSCallerARN: value}
			},
		},
		{
			name:  "KMS key",
			valid: "arn:aws:kms:us-east-1:123456789012:key/1234abcd",
			invalid: []string{
				"arn:aws123:kms:us-east-1:123456789012:key/1234abcd",
				"arn:aws:kms:useast1:123456789012:key/1234abcd",
				"arn:aws:kms:us-east-1:123:key/1234abcd",
				"arn:aws:kms:us-east-1:123456789012:",
				"arn:aws:sns:us-east-1:123456789012:key/1234abcd",
			},
			wantError: "lambda-config.kms-key-id",
			setARN: func(r *UserPoolResource, value string) {
				r.LambdaConfig = &UserPoolLambdaConfiguration{KMSKeyID: &value}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("valid", func(t *testing.T) {
				resource := UserPoolResource{Name: "unobin-pool"}
				tt.setARN(&resource, tt.valid)
				require.NoError(t, resource.ValidateInputs(context.Background(), nil))
			})
			for index, value := range tt.invalid {
				t.Run(fmt.Sprintf("invalid %d", index), func(t *testing.T) {
					resource := UserPoolResource{Name: "unobin-pool"}
					tt.setARN(&resource, value)
					err := resource.ValidateInputs(context.Background(), nil)
					require.Error(t, err)
					assert.ErrorContains(t, err, tt.wantError)
				})
			}
		})
	}
}

func TestUserPoolRegionValidation(t *testing.T) {
	regions := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "one digit suffix", value: "us-east-1"},
		{name: "two digit suffix", value: "us-east-12"},
		{name: "digit in intermediate component", value: "us-east2-1", wantError: true},
		{name: "three digit suffix", value: "us-east-123", wantError: true},
	}
	paths := []struct {
		name      string
		wantError string
		resource  func(string) UserPoolResource
	}{
		{
			name:      "ARN",
			wantError: "lambda-config.post-confirmation",
			resource: func(region string) UserPoolResource {
				arn := fmt.Sprintf(
					"arn:aws:lambda:%s:123456789012:function:handler",
					region,
				)
				return UserPoolResource{
					Name: "unobin-pool",
					LambdaConfig: &UserPoolLambdaConfiguration{
						PostConfirmation: &arn,
					},
				}
			},
		},
		{
			name:      "SNS region",
			wantError: "sms-configuration.sns-region",
			resource: func(region string) UserPoolResource {
				return UserPoolResource{
					Name: "unobin-pool",
					SMSConfiguration: &UserPoolSMSConfiguration{
						SNSCallerARN: "arn:aws:iam::123456789012:role/cognito-sns",
						SNSRegion:    &region,
					},
				}
			},
		},
	}
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			for _, region := range regions {
				t.Run(region.name, func(t *testing.T) {
					resource := path.resource(region.value)
					err := resource.ValidateInputs(context.Background(), nil)
					if !region.wantError {
						require.NoError(t, err)
						return
					}
					require.Error(t, err)
					assert.ErrorContains(t, err, path.wantError)
				})
			}
		})
	}
}

func TestUserPoolValidateSchemaNames(t *testing.T) {
	tests := []struct {
		name      string
		attribute string
		wantError bool
	}{
		{name: "one character", attribute: "a"},
		{name: "twenty characters", attribute: strings.Repeat("a", 20)},
		{name: "unicode categories", attribute: "équipe-東京!"},
		{name: "empty", attribute: "", wantError: true},
		{name: "twenty one characters", attribute: strings.Repeat("a", 21), wantError: true},
		{name: "space", attribute: "team name", wantError: true},
		{name: "control", attribute: "team\nname", wantError: true},
		{name: "custom prefix", attribute: "custom:team", wantError: true},
		{name: "developer prefix", attribute: "dev:team", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := UserPoolResource{
				Name: "unobin-pool",
				Schema: &[]UserPoolSchemaAttribute{
					{Name: tt.attribute, AttributeDataType: "String"},
				},
			}
			err := resource.ValidateInputs(context.Background(), nil)
			if tt.wantError {
				require.Error(t, err)
				assert.ErrorContains(t, err, "schema[0].name")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestStandardUserPoolAttributeNames(t *testing.T) {
	assert.Equal(t, []string{
		"address",
		"birthdate",
		"email",
		"email_verified",
		"family_name",
		"gender",
		"given_name",
		"locale",
		"middle_name",
		"name",
		"nickname",
		"phone_number",
		"phone_number_verified",
		"picture",
		"preferred_username",
		"profile",
		"sub",
		"updated_at",
		"website",
		"zoneinfo",
	}, standardUserPoolAttributeNames)
	for _, name := range []string{"Email", "email_address", "phone", "user_name"} {
		assert.False(t, isStandardUserPoolAttribute(name))
	}
}

func TestUserPoolValidateInputsRejectsInvalidConfigurations(t *testing.T) {
	tests := map[string]struct {
		resource UserPoolResource
		want     string
	}{
		"name pattern": {
			resource: UserPoolResource{Name: "bad/name"},
			want:     "name",
		},
		"alias enum": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.AliasAttributes = &[]string{"username"}
			}),
			want: "alias-attributes",
		},
		"alias duplicate": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.AliasAttributes = &[]string{"email", "email"}
			}),
			want: "alias-attributes",
		},
		"alias and username conflict": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.AliasAttributes = &[]string{"email"}
				r.UsernameAttributes = &[]string{"email"}
			}),
			want: "conflicts",
		},
		"auto verified enum": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.AutoVerifiedAttributes = &[]string{"preferred_username"}
			}),
			want: "auto-verified-attributes",
		},
		"deletion protection": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.DeletionProtection = aws.String("ENABLED")
			}),
			want: "deletion-protection",
		},
		"recovery count": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.AccountRecoverySetting = &UserPoolAccountRecoverySetting{}
			}),
			want: "recovery-mechanisms",
		},
		"recovery enum": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.AccountRecoverySetting = &UserPoolAccountRecoverySetting{
					RecoveryMechanisms: []UserPoolRecoveryMechanism{
						{Name: "sms", Priority: 1},
					},
				}
			}),
			want: "recovery-mechanisms",
		},
		"recovery duplicate priority": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.AccountRecoverySetting = &UserPoolAccountRecoverySetting{
					RecoveryMechanisms: []UserPoolRecoveryMechanism{
						{Name: "verified_email", Priority: 1},
						{Name: "verified_phone_number", Priority: 1},
					},
				}
			}),
			want: "priority",
		},
		"legacy password conflict": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.AdminCreateUserConfig = &UserPoolAdminCreateUserConfig{
					UnusedAccountValidityDays: aws.Int64(7),
				}
				r.PasswordPolicy = &UserPoolPasswordPolicy{
					TemporaryPasswordValidityDays: aws.Int64(7),
				}
			}),
			want: "conflicts",
		},
		"invite placeholders": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.AdminCreateUserConfig = &UserPoolAdminCreateUserConfig{
					InviteMessageTemplate: &UserPoolInviteMessageTemplate{
						EmailMessage: aws.String("No placeholders"),
					},
				}
			}),
			want: "invite-message-template.email-message",
		},
		"email sending account": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.EmailConfiguration = &UserPoolEmailConfiguration{
					EmailSendingAccount: aws.String("SES")}
			}),
			want: "email-sending-account",
		},
		"email reply address": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.EmailConfiguration = &UserPoolEmailConfiguration{
					ReplyToEmailAddress: aws.String("not-an-email")}
			}),
			want: "reply-to-email-address",
		},
		"email source arn": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.EmailConfiguration = &UserPoolEmailConfiguration{
					SourceARN: aws.String("not-an-arn")}
			}),
			want: "source-arn",
		},
		"lambda trigger arn": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.LambdaConfig = &UserPoolLambdaConfiguration{
					PostConfirmation: aws.String("not-an-arn")}
			}),
			want: "post-confirmation",
		},
		"custom sender requires kms": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.LambdaConfig = &UserPoolLambdaConfiguration{
					CustomEmailSender: validLambdaVersion("sender", "V1_0")}
			}),
			want: "kms-key-id",
		},
		"custom sender version": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.LambdaConfig = &UserPoolLambdaConfiguration{
					CustomSMSSender: validLambdaVersion("sender", "V2_0"),
					KMSKeyID: aws.String(
						"arn:aws:kms:us-east-1:123456789012:key/1234abcd"),
				}
			}),
			want: "lambda-version",
		},
		"pre token version": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.LambdaConfig = &UserPoolLambdaConfiguration{
					PreTokenGenerationConfig: validLambdaVersion("pre-token", "V4_0")}
			}),
			want: "lambda-version",
		},
		"pre token arns differ": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.LambdaConfig = &UserPoolLambdaConfiguration{
					PreTokenGeneration: aws.String(
						"arn:aws:lambda:us-east-1:123456789012:function:legacy"),
					PreTokenGenerationConfig: validLambdaVersion("config", "V2_0"),
				}
			}),
			want: "must match",
		},
		"inbound federation version": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.LambdaConfig = &UserPoolLambdaConfiguration{
					InboundFederation: validLambdaVersion("inbound", "V2_0")}
			}),
			want: "inbound-federation.lambda-version",
		},
		"mfa enum": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.MFAConfiguration = aws.String("REQUIRED")
			}),
			want: "mfa-configuration",
		},
		"mfa factor enum": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.EnabledMFAs = &[]string{"PUSH"}
			}),
			want: "enabled-mfas",
		},
		"mfa factor duplicate": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.EnabledMFAs = &[]string{"SOFTWARE_TOKEN_MFA", "SOFTWARE_TOKEN_MFA"}
			}),
			want: "enabled-mfas",
		},
		"mfa on needs factor": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.MFAConfiguration = aws.String("ON")
			}),
			want: "requires",
		},
		"mfa off rejects factor": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.MFAConfiguration = aws.String("OFF")
				r.EnabledMFAs = &[]string{"SOFTWARE_TOKEN_MFA"}
			}),
			want: "OFF",
		},
		"sms mfa needs sms configuration": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.MFAConfiguration = aws.String("OPTIONAL")
				r.EnabledMFAs = &[]string{"SMS_MFA"}
			}),
			want: "sms-configuration",
		},
		"email mfa needs essentials": {
			resource: validEmailMFAResource(func(r *UserPoolResource) {
				r.UserPoolTier = aws.String("LITE")
			}),
			want: "user-pool-tier",
		},
		"email mfa needs email configuration": {
			resource: validEmailMFAResource(func(r *UserPoolResource) {
				r.EmailConfiguration = nil
			}),
			want: "email-configuration",
		},
		"email mfa needs two recovery methods": {
			resource: validEmailMFAResource(func(r *UserPoolResource) {
				r.AccountRecoverySetting.RecoveryMechanisms =
					r.AccountRecoverySetting.RecoveryMechanisms[:1]
			}),
			want: "two recovery",
		},
		"email mfa config needs factor": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.EmailMFAConfiguration = &UserPoolEmailMFAConfiguration{}
			}),
			want: "EMAIL_OTP",
		},
		"web authn factor": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.WebAuthnConfiguration = &UserPoolWebAuthnConfiguration{
					FactorConfiguration: aws.String("PASSWORD")}
			}),
			want: "factor-configuration",
		},
		"web authn verification": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.WebAuthnConfiguration = &UserPoolWebAuthnConfiguration{
					UserVerification: aws.String("discouraged")}
			}),
			want: "user-verification",
		},
		"web authn relying party length": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.WebAuthnConfiguration = &UserPoolWebAuthnConfiguration{
					RelyingPartyID: aws.String(strings.Repeat("a", 64))}
			}),
			want: "relying-party-id",
		},
		"web authn multi needs verification": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.MFAConfiguration = aws.String("OPTIONAL")
				r.WebAuthnConfiguration = &UserPoolWebAuthnConfiguration{
					FactorConfiguration: aws.String("MULTI_FACTOR_WITH_USER_VERIFICATION"),
					UserVerification:    aws.String("preferred"),
				}
			}),
			want: "required",
		},
		"password minimum": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.PasswordPolicy = &UserPoolPasswordPolicy{MinimumLength: aws.Int64(5)}
			}),
			want: "minimum-length",
		},
		"password history needs security": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.PasswordPolicy = &UserPoolPasswordPolicy{
					PasswordHistorySize: aws.Int64(1)}
			}),
			want: "advanced security",
		},
		"sign in factor": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.SignInPolicy = &UserPoolSignInPolicy{
					AllowedFirstAuthFactors: &[]string{"MAGIC_LINK"}}
			}),
			want: "allowed-first-auth-factors",
		},
		"sign in policy tier": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.SignInPolicy = &UserPoolSignInPolicy{
					AllowedFirstAuthFactors: &[]string{"PASSWORD"}}
				r.UserPoolTier = aws.String("LITE")
			}),
			want: "user-pool-tier",
		},
		"schema count": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.Schema = &[]UserPoolSchemaAttribute{}
			}),
			want: "schema",
		},
		"schema duplicate": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.Schema = &[]UserPoolSchemaAttribute{
					{Name: "team", AttributeDataType: "String"},
					{Name: "team", AttributeDataType: "String"},
				}
			}),
			want: "duplicate",
		},
		"schema type": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.Schema = &[]UserPoolSchemaAttribute{
					{Name: "team", AttributeDataType: "Object"}}
			}),
			want: "attribute-data-type",
		},
		"schema constraint type": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.Schema = &[]UserPoolSchemaAttribute{{
					Name:                       "team",
					AttributeDataType:          "Number",
					StringAttributeConstraints: &UserPoolStringAttributeConstraints{},
				}}
			}),
			want: "string-attribute-constraints",
		},
		"schema number ordering": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.Schema = &[]UserPoolSchemaAttribute{{
					Name:              "score",
					AttributeDataType: "Number",
					NumberAttributeConstraints: &UserPoolNumberAttributeConstraints{
						MinValue: aws.String("10"), MaxValue: aws.String("1"),
					},
				}}
			}),
			want: "min-value",
		},
		"schema string length": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.Schema = &[]UserPoolSchemaAttribute{{
					Name:              "team",
					AttributeDataType: "String",
					StringAttributeConstraints: &UserPoolStringAttributeConstraints{
						MinLength: aws.String("20"), MaxLength: aws.String("10"),
					},
				}}
			}),
			want: "min-length",
		},
		"sms caller required": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.SMSConfiguration = &UserPoolSMSConfiguration{}
			}),
			want: "sns-caller-arn",
		},
		"sms region": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.SMSConfiguration = &UserPoolSMSConfiguration{
					SNSCallerARN: "arn:aws:iam::123456789012:role/cognito-sns",
					SNSRegion:    aws.String("US_EAST_1"),
				}
			}),
			want: "sns-region",
		},
		"verification placeholder": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.EmailVerificationMessage = aws.String("Missing code")
			}),
			want: "email-verification-message",
		},
		"link placeholder": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.VerificationMessageTemplate = &UserPoolVerificationTemplate{
					EmailMessageByLink: aws.String("Missing link")}
			}),
			want: "email-message-by-link",
		},
		"verification option": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.VerificationMessageTemplate = &UserPoolVerificationTemplate{
					DefaultEmailOption: aws.String("SEND_CODE")}
			}),
			want: "default-email-option",
		},
		"verification subset": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.AutoVerifiedAttributes = &[]string{"email"}
				r.UserAttributeUpdateSettings = &UserPoolAttributeUpdateSettings{
					AttributesRequireVerificationBeforeUpdate: []string{"phone_number"},
				}
			}),
			want: "subset",
		},
		"add ons mode": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.UserPoolAddOns = &UserPoolAddOns{AdvancedSecurityMode: "MONITOR"}
			}),
			want: "advanced-security-mode",
		},
		"tier": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.UserPoolTier = aws.String("STANDARD")
			}),
			want: "user-pool-tier",
		},
		"tag key": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.Tags = &map[string]string{"": "value"}
			}),
			want: "tag key",
		},
		"tag value": {
			resource: withUserPool(func(r *UserPoolResource) {
				r.Tags = &map[string]string{"key": strings.Repeat("v", 257)}
			}),
			want: "tag value",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func withUserPool(change func(*UserPoolResource)) UserPoolResource {
	r := UserPoolResource{Name: "unobin-pool"}
	change(&r)
	return r
}

func validEmailMFAResource(change func(*UserPoolResource)) UserPoolResource {
	r := UserPoolResource{
		Name:             "unobin-pool",
		MFAConfiguration: aws.String("OPTIONAL"),
		EnabledMFAs:      &[]string{"EMAIL_OTP"},
		AccountRecoverySetting: &UserPoolAccountRecoverySetting{
			RecoveryMechanisms: []UserPoolRecoveryMechanism{
				{Name: "verified_email", Priority: 1},
				{Name: "verified_phone_number", Priority: 2},
			},
		},
		EmailConfiguration: &UserPoolEmailConfiguration{},
	}
	change(&r)
	return r
}

func validLambdaVersion(name, version string) *UserPoolLambdaVersionConfiguration {
	return &UserPoolLambdaVersionConfiguration{
		LambdaARN:     "arn:aws:lambda:us-east-1:123456789012:function:" + name,
		LambdaVersion: version,
	}
}
