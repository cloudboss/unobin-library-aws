package cognitoidp

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPoolCreateInputOmitsOptionalCollections(t *testing.T) {
	in, err := (&UserPoolResource{Name: "unobin-pool"}).createInput()
	require.NoError(t, err)

	assert.Nil(t, in.AliasAttributes)
	assert.Nil(t, in.AutoVerifiedAttributes)
	assert.Nil(t, in.Schema)
	assert.Nil(t, in.UsernameAttributes)
	assert.Nil(t, in.UserPoolTags)
	assert.Nil(t, in.AccountRecoverySetting)
	assert.Nil(t, in.AdminCreateUserConfig)
	assert.Nil(t, in.LambdaConfig)
	assert.Nil(t, in.Policies)
	assert.Nil(t, in.VerificationMessageTemplate)
	assert.Empty(t, in.DeletionProtection)
	assert.Empty(t, in.UserPoolTier)
}

func TestUserPoolCreateInputMapsNestedProperties(t *testing.T) {
	r := completeUserPoolResource()
	in, err := r.createInput()
	require.NoError(t, err)

	require.Len(t, in.AccountRecoverySetting.RecoveryMechanisms, 2)
	assert.Equal(t, cognitotypes.RecoveryOptionNameTypeVerifiedEmail,
		in.AccountRecoverySetting.RecoveryMechanisms[0].Name)
	assert.Equal(t, int32(1),
		aws.ToInt32(in.AccountRecoverySetting.RecoveryMechanisms[0].Priority))
	require.NotNil(t, in.AdminCreateUserConfig.InviteMessageTemplate)
	assert.Equal(t, "Welcome {username}; code {####}",
		aws.ToString(in.AdminCreateUserConfig.InviteMessageTemplate.EmailMessage))
	require.NotNil(t, in.DeviceConfiguration)
	assert.True(t, in.DeviceConfiguration.ChallengeRequiredOnNewDevice)
	require.NotNil(t, in.LambdaConfig)
	require.NotNil(t, in.LambdaConfig.InboundFederation)
	assert.Equal(t, "V1_0", string(in.LambdaConfig.InboundFederation.LambdaVersion))
	require.NotNil(t, in.LambdaConfig.PreTokenGenerationConfig)
	assert.Equal(t, "V2_0", string(in.LambdaConfig.PreTokenGenerationConfig.LambdaVersion))
	require.NotNil(t, in.Policies)
	require.NotNil(t, in.Policies.SignInPolicy)
	assert.Equal(t, []cognitotypes.AuthFactorType{
		cognitotypes.AuthFactorTypePassword,
		cognitotypes.AuthFactorTypeWebAuthn,
	}, in.Policies.SignInPolicy.AllowedFirstAuthFactors)
	require.Len(t, in.Schema, 2)
	assert.Equal(t, "team", aws.ToString(in.Schema[0].Name))
	require.NotNil(t, in.SmsConfiguration)
	assert.Equal(t, "arn:aws:iam::123456789012:role/cognito-sns",
		aws.ToString(in.SmsConfiguration.SnsCallerArn))
	require.NotNil(t, in.UserAttributeUpdateSettings)
	assert.Equal(t, []cognitotypes.VerifiedAttributeType{
		cognitotypes.VerifiedAttributeTypeEmail,
	}, in.UserAttributeUpdateSettings.AttributesRequireVerificationBeforeUpdate)
	require.NotNil(t, in.UserPoolAddOns)
	assert.Equal(t, cognitotypes.AdvancedSecurityModeTypeEnforced,
		in.UserPoolAddOns.AdvancedSecurityMode)
	assert.Equal(t, map[string]string{"env": "test"}, in.UserPoolTags)
	assert.Empty(t, in.MfaConfiguration)
}

func TestUserPoolUpdateInputUsesCompleteDesiredConfiguration(t *testing.T) {
	r := completeUserPoolResource()
	prior := UserPoolResource{
		Name: "old-name",
		UserAttributeUpdateSettings: &UserPoolAttributeUpdateSettings{
			AttributesRequireVerificationBeforeUpdate: []string{"phone_number"},
		},
	}
	in, err := r.updateInput("pool-id", prior)
	require.NoError(t, err)

	assert.Equal(t, "pool-id", aws.ToString(in.UserPoolId))
	assert.Equal(t, r.Name, aws.ToString(in.PoolName))
	require.NotNil(t, in.AccountRecoverySetting)
	require.NotNil(t, in.AdminCreateUserConfig)
	require.NotNil(t, in.DeviceConfiguration)
	require.NotNil(t, in.EmailConfiguration)
	require.NotNil(t, in.LambdaConfig)
	require.NotNil(t, in.Policies)
	require.NotNil(t, in.SmsConfiguration)
	require.NotNil(t, in.UserAttributeUpdateSettings)
	require.NotNil(t, in.UserPoolAddOns)
	require.NotNil(t, in.VerificationMessageTemplate)
	assert.Equal(t, cognitotypes.UserPoolMfaTypeOptional, in.MfaConfiguration)
	assert.Equal(t, cognitotypes.UserPoolTierTypePlus, in.UserPoolTier)
	assert.Equal(t, r.EmailVerificationMessage, in.EmailVerificationMessage)
	assert.Equal(t, r.EmailVerificationSubject, in.EmailVerificationSubject)
	assert.Equal(t, r.SMSAuthenticationMessage, in.SmsAuthenticationMessage)
	assert.Equal(t, r.SMSVerificationMessage, in.SmsVerificationMessage)
}

func TestUserPoolUpdateInputExplicitlyClearsUserAttributeSettings(t *testing.T) {
	r := UserPoolResource{Name: "unobin-pool"}
	prior := UserPoolResource{
		UserAttributeUpdateSettings: &UserPoolAttributeUpdateSettings{
			AttributesRequireVerificationBeforeUpdate: []string{"email"},
		},
	}
	in, err := r.updateInput("pool-id", prior)
	require.NoError(t, err)
	require.NotNil(t, in.UserAttributeUpdateSettings)
	assert.Empty(t,
		in.UserAttributeUpdateSettings.AttributesRequireVerificationBeforeUpdate)
}

func TestUserPoolUpdateInputOmitsDesiredOptionals(t *testing.T) {
	in, err := (&UserPoolResource{Name: "unobin-pool"}).updateInput(
		"pool-id",
		UserPoolResource{},
	)
	require.NoError(t, err)

	assert.Nil(t, in.AccountRecoverySetting)
	assert.Nil(t, in.AdminCreateUserConfig)
	assert.Nil(t, in.AutoVerifiedAttributes)
	assert.Nil(t, in.DeviceConfiguration)
	assert.Nil(t, in.EmailConfiguration)
	assert.Nil(t, in.LambdaConfig)
	assert.Nil(t, in.Policies)
	assert.Nil(t, in.SmsConfiguration)
	assert.Nil(t, in.UserAttributeUpdateSettings)
	assert.Nil(t, in.UserPoolAddOns)
	assert.Nil(t, in.VerificationMessageTemplate)
	assert.Empty(t, in.DeletionProtection)
	assert.Empty(t, in.MfaConfiguration)
	assert.Empty(t, in.UserPoolTier)
}

func TestUserPoolUpdateInputClearsOptionalConfiguration(t *testing.T) {
	prior := completeUserPoolResource()
	current := UserPoolResource{Name: "unobin-pool"}
	in, err := current.updateInput("pool-id", prior)
	require.NoError(t, err)

	assert.Equal(t, "pool-id", aws.ToString(in.UserPoolId))
	assert.Equal(t, current.Name, aws.ToString(in.PoolName))
	assert.Nil(t, in.AccountRecoverySetting)
	assert.Nil(t, in.AdminCreateUserConfig)
	assert.Nil(t, in.AutoVerifiedAttributes)
	assert.Nil(t, in.DeviceConfiguration)
	assert.Nil(t, in.EmailConfiguration)
	assert.Nil(t, in.EmailVerificationMessage)
	assert.Nil(t, in.EmailVerificationSubject)
	assert.Nil(t, in.LambdaConfig)
	assert.Nil(t, in.Policies)
	assert.Nil(t, in.SmsAuthenticationMessage)
	assert.Nil(t, in.SmsConfiguration)
	assert.Nil(t, in.SmsVerificationMessage)
	require.NotNil(t, in.UserAttributeUpdateSettings)
	assert.Empty(t,
		in.UserAttributeUpdateSettings.AttributesRequireVerificationBeforeUpdate)
	assert.Nil(t, in.UserPoolAddOns)
	assert.Nil(t, in.VerificationMessageTemplate)
	assert.Empty(t, in.DeletionProtection)
	assert.Empty(t, in.MfaConfiguration)
	assert.Empty(t, in.UserPoolTier)
}

func TestUserPoolMFAInputMapsCompleteFactorConfiguration(t *testing.T) {
	tests := map[string]struct {
		resource UserPoolResource
		check    func(*testing.T, *cognitoidentityprovider.SetUserPoolMfaConfigInput)
	}{
		"email default template": {
			resource: UserPoolResource{EnabledMFAs: &[]string{"EMAIL_OTP"}},
			check: func(t *testing.T, in *cognitoidentityprovider.SetUserPoolMfaConfigInput) {
				require.NotNil(t, in.EmailMfaConfiguration)
				assert.Nil(t, in.EmailMfaConfiguration.Message)
				assert.Nil(t, in.EmailMfaConfiguration.Subject)
				assert.Nil(t, in.SmsMfaConfiguration)
				assert.Nil(t, in.SoftwareTokenMfaConfiguration)
			},
		},
		"all mfa factors": {
			resource: UserPoolResource{
				EnabledMFAs: &[]string{"SOFTWARE_TOKEN_MFA", "EMAIL_OTP", "SMS_MFA"},
				EmailMFAConfiguration: &UserPoolEmailMFAConfiguration{
					Message: aws.String("Email code {####}"),
					Subject: aws.String("Email code"),
				},
				SMSAuthenticationMessage: aws.String("SMS code {####}"),
				SMSConfiguration: &UserPoolSMSConfiguration{
					SNSCallerARN: "arn:aws:iam::123456789012:role/cognito-sns",
				},
			},
			check: func(t *testing.T, in *cognitoidentityprovider.SetUserPoolMfaConfigInput) {
				require.NotNil(t, in.EmailMfaConfiguration)
				assert.Equal(t, "Email code {####}",
					aws.ToString(in.EmailMfaConfiguration.Message))
				require.NotNil(t, in.SmsMfaConfiguration)
				assert.Equal(t, "SMS code {####}",
					aws.ToString(in.SmsMfaConfiguration.SmsAuthenticationMessage))
				require.NotNil(t, in.SoftwareTokenMfaConfiguration)
				assert.True(t, in.SoftwareTokenMfaConfiguration.Enabled)
			},
		},
		"web authn only": {
			resource: UserPoolResource{
				WebAuthnConfiguration: &UserPoolWebAuthnConfiguration{
					FactorConfiguration: aws.String("SINGLE_FACTOR"),
					RelyingPartyID:      aws.String("login.example.com"),
					UserVerification:    aws.String("preferred"),
				},
			},
			check: func(t *testing.T, in *cognitoidentityprovider.SetUserPoolMfaConfigInput) {
				require.NotNil(t, in.WebAuthnConfiguration)
				assert.Equal(t, "SINGLE_FACTOR",
					string(in.WebAuthnConfiguration.FactorConfiguration))
				assert.Nil(t, in.EmailMfaConfiguration)
				assert.Nil(t, in.SmsMfaConfiguration)
				assert.Nil(t, in.SoftwareTokenMfaConfiguration)
			},
		},
		"removed factors": {
			resource: UserPoolResource{EnabledMFAs: &[]string{}},
			check: func(t *testing.T, in *cognitoidentityprovider.SetUserPoolMfaConfigInput) {
				assert.Nil(t, in.EmailMfaConfiguration)
				assert.Nil(t, in.SmsMfaConfiguration)
				assert.Nil(t, in.SoftwareTokenMfaConfiguration)
				assert.Nil(t, in.WebAuthnConfiguration)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			in := tt.resource.mfaInput("pool-id")
			assert.Equal(t, "pool-id", aws.ToString(in.UserPoolId))
			tt.check(t, in)
		})
	}
}

func TestUserPoolCreateInputUsesUnprefixedSchemaNames(t *testing.T) {
	attributes := []UserPoolSchemaAttribute{
		{Name: "email", AttributeDataType: "String"},
		{
			Name:                   "team",
			AttributeDataType:      "String",
			DeveloperOnlyAttribute: aws.Bool(false),
		},
		{
			Name:                   "owner",
			AttributeDataType:      "String",
			DeveloperOnlyAttribute: aws.Bool(true),
		},
	}
	in, err := (&UserPoolResource{Name: "unobin-pool", Schema: &attributes}).createInput()
	require.NoError(t, err)
	require.Len(t, in.Schema, 3)
	assert.Equal(t, "email", aws.ToString(in.Schema[0].Name))
	assert.Nil(t, in.Schema[0].DeveloperOnlyAttribute)
	assert.Equal(t, "team", aws.ToString(in.Schema[1].Name))
	assert.False(t, aws.ToBool(in.Schema[1].DeveloperOnlyAttribute))
	assert.Equal(t, "owner", aws.ToString(in.Schema[2].Name))
	assert.True(t, aws.ToBool(in.Schema[2].DeveloperOnlyAttribute))
}

func completeUserPoolResource() UserPoolResource {
	return UserPoolResource{
		Name: "unobin-pool",
		AccountRecoverySetting: &UserPoolAccountRecoverySetting{
			RecoveryMechanisms: []UserPoolRecoveryMechanism{
				{Name: "verified_email", Priority: 1},
				{Name: "verified_phone_number", Priority: 2},
			},
		},
		AdminCreateUserConfig: &UserPoolAdminCreateUserConfig{
			AllowAdminCreateUserOnly: aws.Bool(true),
			InviteMessageTemplate: &UserPoolInviteMessageTemplate{
				EmailMessage: aws.String("Welcome {username}; code {####}"),
				EmailSubject: aws.String("Welcome"),
				SMSMessage:   aws.String("Hi {username}: {####}"),
			},
		},
		AliasAttributes:        &[]string{"email"},
		AutoVerifiedAttributes: &[]string{"email"},
		DeletionProtection:     aws.String("ACTIVE"),
		DeviceConfiguration: &UserPoolDeviceConfiguration{
			ChallengeRequiredOnNewDevice: aws.Bool(true),
		},
		EmailConfiguration: &UserPoolEmailConfiguration{
			EmailSendingAccount: aws.String("DEVELOPER"),
			FromEmailAddress:    aws.String("Example <no-reply@example.com>"),
			ReplyToEmailAddress: aws.String("support@example.com"),
			SourceARN: aws.String(
				"arn:aws:ses:us-east-1:123456789012:identity/example.com",
			),
		},
		EmailVerificationMessage: aws.String("Verification code {####}"),
		EmailVerificationSubject: aws.String("Verification code"),
		LambdaConfig: &UserPoolLambdaConfiguration{
			CreateAuthChallenge: aws.String(
				"arn:aws:lambda:us-east-1:123456789012:function:create-auth",
			),
			InboundFederation: &UserPoolLambdaVersionConfiguration{
				LambdaARN:     "arn:aws:lambda:us-east-1:123456789012:function:inbound",
				LambdaVersion: "V1_0",
			},
			PreTokenGeneration: aws.String(
				"arn:aws:lambda:us-east-1:123456789012:function:pre-token",
			),
			PreTokenGenerationConfig: &UserPoolLambdaVersionConfiguration{
				LambdaARN:     "arn:aws:lambda:us-east-1:123456789012:function:pre-token",
				LambdaVersion: "V2_0",
			},
		},
		MFAConfiguration:      aws.String("OPTIONAL"),
		EnabledMFAs:           &[]string{"SOFTWARE_TOKEN_MFA"},
		EmailMFAConfiguration: nil,
		PasswordPolicy: &UserPoolPasswordPolicy{
			MinimumLength:                 aws.Int64(12),
			PasswordHistorySize:           aws.Int64(12),
			RequireLowercase:              aws.Bool(true),
			RequireNumbers:                aws.Bool(true),
			RequireSymbols:                aws.Bool(true),
			RequireUppercase:              aws.Bool(true),
			TemporaryPasswordValidityDays: aws.Int64(7),
		},
		SignInPolicy: &UserPoolSignInPolicy{
			AllowedFirstAuthFactors: &[]string{"PASSWORD", "WEB_AUTHN"},
		},
		Schema: &[]UserPoolSchemaAttribute{
			{
				Name:              "team",
				AttributeDataType: "String",
				StringAttributeConstraints: &UserPoolStringAttributeConstraints{
					MinLength: aws.String("1"),
					MaxLength: aws.String("64"),
				},
			},
			{
				Name:              "score",
				AttributeDataType: "Number",
				NumberAttributeConstraints: &UserPoolNumberAttributeConstraints{
					MinValue: aws.String("0"),
					MaxValue: aws.String("100"),
				},
			},
		},
		SMSAuthenticationMessage: aws.String("Authentication code {####}"),
		SMSConfiguration: &UserPoolSMSConfiguration{
			SNSCallerARN: "arn:aws:iam::123456789012:role/cognito-sns",
			ExternalID:   aws.String("external-id"),
			SNSRegion:    aws.String("us-east-1"),
		},
		SMSVerificationMessage: aws.String("Verification code {####}"),
		Tags:                   &map[string]string{"env": "test", "aws:skip": "value"},
		UserAttributeUpdateSettings: &UserPoolAttributeUpdateSettings{
			AttributesRequireVerificationBeforeUpdate: []string{"email"},
		},
		UserPoolAddOns: &UserPoolAddOns{
			AdvancedSecurityMode: "ENFORCED",
			AdvancedSecurityAdditionalFlows: &UserPoolSecurityFlows{
				CustomAuthMode: aws.String("ENFORCED"),
			},
		},
		UserPoolTier: aws.String("PLUS"),
		UsernameConfiguration: &UserPoolUsernameConfiguration{
			CaseSensitive: true,
		},
		VerificationMessageTemplate: &UserPoolVerificationTemplate{
			DefaultEmailOption: aws.String("CONFIRM_WITH_CODE"),
			EmailMessage:       aws.String("Verification code {####}"),
			EmailSubject:       aws.String("Verification code"),
			SMSMessage:         aws.String("Verification code {####}"),
		},
	}
}
