package cognitoidp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

func accountRecoverySetting(
	in *UserPoolAccountRecoverySetting,
) *cognitotypes.AccountRecoverySettingType {
	if in == nil {
		return nil
	}
	mechanisms := make([]cognitotypes.RecoveryOptionType, 0, len(in.RecoveryMechanisms))
	for _, mechanism := range in.RecoveryMechanisms {
		mechanisms = append(mechanisms, cognitotypes.RecoveryOptionType{
			Name:     cognitotypes.RecoveryOptionNameType(mechanism.Name),
			Priority: ptr.Int32(&mechanism.Priority),
		})
	}
	return &cognitotypes.AccountRecoverySettingType{RecoveryMechanisms: mechanisms}
}

func adminCreateUserConfig(
	in *UserPoolAdminCreateUserConfig,
) *cognitotypes.AdminCreateUserConfigType {
	if in == nil {
		return nil
	}
	out := &cognitotypes.AdminCreateUserConfigType{
		AllowAdminCreateUserOnly: aws.ToBool(in.AllowAdminCreateUserOnly),
		InviteMessageTemplate:    inviteMessageTemplate(in.InviteMessageTemplate),
	}
	if in.UnusedAccountValidityDays != nil {
		out.UnusedAccountValidityDays = int32(*in.UnusedAccountValidityDays)
	}
	return out
}

func inviteMessageTemplate(
	in *UserPoolInviteMessageTemplate,
) *cognitotypes.MessageTemplateType {
	if in == nil {
		return nil
	}
	return &cognitotypes.MessageTemplateType{
		EmailMessage: in.EmailMessage,
		EmailSubject: in.EmailSubject,
		SMSMessage:   in.SMSMessage,
	}
}

func aliasAttributes(values []string) []cognitotypes.AliasAttributeType {
	if values == nil {
		return nil
	}
	out := make([]cognitotypes.AliasAttributeType, 0, len(values))
	for _, value := range values {
		out = append(out, cognitotypes.AliasAttributeType(value))
	}
	return out
}

func verifiedAttributes(values []string) []cognitotypes.VerifiedAttributeType {
	if values == nil {
		return nil
	}
	out := make([]cognitotypes.VerifiedAttributeType, 0, len(values))
	for _, value := range values {
		out = append(out, cognitotypes.VerifiedAttributeType(value))
	}
	return out
}

func usernameAttributes(values []string) []cognitotypes.UsernameAttributeType {
	if values == nil {
		return nil
	}
	out := make([]cognitotypes.UsernameAttributeType, 0, len(values))
	for _, value := range values {
		out = append(out, cognitotypes.UsernameAttributeType(value))
	}
	return out
}

func deletionProtection(value *string) cognitotypes.DeletionProtectionType {
	if value == nil {
		return ""
	}
	return cognitotypes.DeletionProtectionType(*value)
}

func mfaConfiguration(value *string) cognitotypes.UserPoolMfaType {
	if value == nil {
		return ""
	}
	return cognitotypes.UserPoolMfaType(*value)
}

func userPoolTier(value *string) cognitotypes.UserPoolTierType {
	if value == nil {
		return ""
	}
	return cognitotypes.UserPoolTierType(*value)
}

func deviceConfiguration(
	in *UserPoolDeviceConfiguration,
) *cognitotypes.DeviceConfigurationType {
	if in == nil {
		return nil
	}
	return &cognitotypes.DeviceConfigurationType{
		ChallengeRequiredOnNewDevice:     aws.ToBool(in.ChallengeRequiredOnNewDevice),
		DeviceOnlyRememberedOnUserPrompt: aws.ToBool(in.DeviceOnlyRememberedOnUserPrompt),
	}
}

func emailConfiguration(
	in *UserPoolEmailConfiguration,
) *cognitotypes.EmailConfigurationType {
	if in == nil {
		return nil
	}
	out := &cognitotypes.EmailConfigurationType{
		ConfigurationSet:    in.ConfigurationSet,
		From:                in.FromEmailAddress,
		ReplyToEmailAddress: in.ReplyToEmailAddress,
		SourceArn:           in.SourceARN,
	}
	if in.EmailSendingAccount != nil {
		out.EmailSendingAccount = cognitotypes.EmailSendingAccountType(*in.EmailSendingAccount)
	}
	return out
}

func lambdaConfiguration(
	in *UserPoolLambdaConfiguration,
) *cognitotypes.LambdaConfigType {
	if in == nil {
		return nil
	}
	return &cognitotypes.LambdaConfigType{
		CreateAuthChallenge:         in.CreateAuthChallenge,
		CustomEmailSender:           customEmailSender(in.CustomEmailSender),
		CustomMessage:               in.CustomMessage,
		CustomSMSSender:             customSMSSender(in.CustomSMSSender),
		DefineAuthChallenge:         in.DefineAuthChallenge,
		InboundFederation:           inboundFederation(in.InboundFederation),
		KMSKeyID:                    in.KMSKeyID,
		PostAuthentication:          in.PostAuthentication,
		PostConfirmation:            in.PostConfirmation,
		PreAuthentication:           in.PreAuthentication,
		PreSignUp:                   in.PreSignUp,
		PreTokenGeneration:          in.PreTokenGeneration,
		PreTokenGenerationConfig:    preTokenGeneration(in.PreTokenGenerationConfig),
		UserMigration:               in.UserMigration,
		VerifyAuthChallengeResponse: in.VerifyAuthChallenge,
	}
}

func customEmailSender(
	in *UserPoolLambdaVersionConfiguration,
) *cognitotypes.CustomEmailLambdaVersionConfigType {
	if in == nil {
		return nil
	}
	return &cognitotypes.CustomEmailLambdaVersionConfigType{
		LambdaArn:     aws.String(in.LambdaARN),
		LambdaVersion: cognitotypes.CustomEmailSenderLambdaVersionType(in.LambdaVersion),
	}
}

func customSMSSender(
	in *UserPoolLambdaVersionConfiguration,
) *cognitotypes.CustomSMSLambdaVersionConfigType {
	if in == nil {
		return nil
	}
	return &cognitotypes.CustomSMSLambdaVersionConfigType{
		LambdaArn:     aws.String(in.LambdaARN),
		LambdaVersion: cognitotypes.CustomSMSSenderLambdaVersionType(in.LambdaVersion),
	}
}

func inboundFederation(
	in *UserPoolLambdaVersionConfiguration,
) *cognitotypes.InboundFederationLambdaType {
	if in == nil {
		return nil
	}
	return &cognitotypes.InboundFederationLambdaType{
		LambdaArn:     aws.String(in.LambdaARN),
		LambdaVersion: cognitotypes.InboundFederationLambdaVersionType(in.LambdaVersion),
	}
}

func preTokenGeneration(
	in *UserPoolLambdaVersionConfiguration,
) *cognitotypes.PreTokenGenerationVersionConfigType {
	if in == nil {
		return nil
	}
	return &cognitotypes.PreTokenGenerationVersionConfigType{
		LambdaArn:     aws.String(in.LambdaARN),
		LambdaVersion: cognitotypes.PreTokenGenerationLambdaVersionType(in.LambdaVersion),
	}
}

func userPoolPolicies(
	password *UserPoolPasswordPolicy,
	signIn *UserPoolSignInPolicy,
) *cognitotypes.UserPoolPolicyType {
	if password == nil && signIn == nil {
		return nil
	}
	return &cognitotypes.UserPoolPolicyType{
		PasswordPolicy: passwordPolicy(password),
		SignInPolicy:   signInPolicy(signIn),
	}
}

func passwordPolicy(in *UserPoolPasswordPolicy) *cognitotypes.PasswordPolicyType {
	if in == nil {
		return nil
	}
	out := &cognitotypes.PasswordPolicyType{
		MinimumLength:       ptr.Int32(in.MinimumLength),
		PasswordHistorySize: ptr.Int32(in.PasswordHistorySize),
		RequireLowercase:    aws.ToBool(in.RequireLowercase),
		RequireNumbers:      aws.ToBool(in.RequireNumbers),
		RequireSymbols:      aws.ToBool(in.RequireSymbols),
		RequireUppercase:    aws.ToBool(in.RequireUppercase),
	}
	if in.TemporaryPasswordValidityDays != nil {
		out.TemporaryPasswordValidityDays = int32(*in.TemporaryPasswordValidityDays)
	}
	return out
}

func signInPolicy(in *UserPoolSignInPolicy) *cognitotypes.SignInPolicyType {
	if in == nil {
		return nil
	}
	values := ptr.Value(in.AllowedFirstAuthFactors)
	factors := make([]cognitotypes.AuthFactorType, 0, len(values))
	for _, value := range values {
		factors = append(factors, cognitotypes.AuthFactorType(value))
	}
	return &cognitotypes.SignInPolicyType{AllowedFirstAuthFactors: factors}
}

func schemaAttributes(in []UserPoolSchemaAttribute) []cognitotypes.SchemaAttributeType {
	if in == nil {
		return nil
	}
	out := make([]cognitotypes.SchemaAttributeType, 0, len(in))
	for _, attribute := range in {
		apiAttribute := cognitotypes.SchemaAttributeType{
			AttributeDataType:      cognitotypes.AttributeDataType(attribute.AttributeDataType),
			DeveloperOnlyAttribute: attribute.DeveloperOnlyAttribute,
			Mutable:                attribute.Mutable,
			Name:                   aws.String(attribute.Name),
			NumberAttributeConstraints: numberAttributeConstraints(
				attribute.NumberAttributeConstraints,
			),
			Required: attribute.Required,
			StringAttributeConstraints: stringAttributeConstraints(
				attribute.StringAttributeConstraints,
			),
		}
		out = append(out, apiAttribute)
	}
	return out
}

func numberAttributeConstraints(
	in *UserPoolNumberAttributeConstraints,
) *cognitotypes.NumberAttributeConstraintsType {
	if in == nil {
		return nil
	}
	return &cognitotypes.NumberAttributeConstraintsType{
		MaxValue: in.MaxValue,
		MinValue: in.MinValue,
	}
}

func stringAttributeConstraints(
	in *UserPoolStringAttributeConstraints,
) *cognitotypes.StringAttributeConstraintsType {
	if in == nil {
		return nil
	}
	if in.MaxLength == nil && in.MinLength == nil {
		return nil
	}
	return &cognitotypes.StringAttributeConstraintsType{
		MaxLength: in.MaxLength,
		MinLength: in.MinLength,
	}
}

func smsConfiguration(in *UserPoolSMSConfiguration) *cognitotypes.SmsConfigurationType {
	if in == nil {
		return nil
	}
	return &cognitotypes.SmsConfigurationType{
		SnsCallerArn: aws.String(in.SNSCallerARN),
		ExternalId:   in.ExternalID,
		SnsRegion:    in.SNSRegion,
	}
}

func userAttributeUpdateSettings(
	in *UserPoolAttributeUpdateSettings,
) *cognitotypes.UserAttributeUpdateSettingsType {
	if in == nil {
		return nil
	}
	return &cognitotypes.UserAttributeUpdateSettingsType{
		AttributesRequireVerificationBeforeUpdate: verifiedAttributes(
			in.AttributesRequireVerificationBeforeUpdate,
		),
	}
}

func emptyUserAttributeUpdateSettings() *cognitotypes.UserAttributeUpdateSettingsType {
	return &cognitotypes.UserAttributeUpdateSettingsType{
		AttributesRequireVerificationBeforeUpdate: []cognitotypes.VerifiedAttributeType{},
	}
}

func userPoolAddOns(in *UserPoolAddOns) *cognitotypes.UserPoolAddOnsType {
	if in == nil {
		return nil
	}
	out := &cognitotypes.UserPoolAddOnsType{
		AdvancedSecurityMode: cognitotypes.AdvancedSecurityModeType(in.AdvancedSecurityMode),
	}
	if in.AdvancedSecurityAdditionalFlows != nil {
		out.AdvancedSecurityAdditionalFlows =
			&cognitotypes.AdvancedSecurityAdditionalFlowsType{}
		if in.AdvancedSecurityAdditionalFlows.CustomAuthMode != nil {
			out.AdvancedSecurityAdditionalFlows.CustomAuthMode =
				cognitotypes.AdvancedSecurityEnabledModeType(
					*in.AdvancedSecurityAdditionalFlows.CustomAuthMode,
				)
		}
	}
	return out
}

func usernameConfiguration(
	in *UserPoolUsernameConfiguration,
) *cognitotypes.UsernameConfigurationType {
	if in == nil {
		return nil
	}
	return &cognitotypes.UsernameConfigurationType{CaseSensitive: aws.Bool(in.CaseSensitive)}
}

func (r *UserPoolResource) verificationTemplate() (
	*cognitotypes.VerificationMessageTemplateType,
	error,
) {
	var out *cognitotypes.VerificationMessageTemplateType
	if r.VerificationMessageTemplate != nil {
		in := r.VerificationMessageTemplate
		out = &cognitotypes.VerificationMessageTemplateType{
			EmailMessage:       in.EmailMessage,
			EmailMessageByLink: in.EmailMessageByLink,
			EmailSubject:       in.EmailSubject,
			EmailSubjectByLink: in.EmailSubjectByLink,
			SmsMessage:         in.SMSMessage,
		}
		if in.DefaultEmailOption != nil {
			out.DefaultEmailOption = cognitotypes.DefaultEmailOptionType(*in.DefaultEmailOption)
		}
	}
	legacy := []struct {
		name  string
		value *string
		get   func(*cognitotypes.VerificationMessageTemplateType) *string
		set   func(*cognitotypes.VerificationMessageTemplateType, *string)
	}{
		{
			name:  "email-verification-message",
			value: r.EmailVerificationMessage,
			get:   func(v *cognitotypes.VerificationMessageTemplateType) *string { return v.EmailMessage },
			set: func(v *cognitotypes.VerificationMessageTemplateType, value *string) {
				v.EmailMessage = value
			},
		},
		{
			name:  "email-verification-subject",
			value: r.EmailVerificationSubject,
			get:   func(v *cognitotypes.VerificationMessageTemplateType) *string { return v.EmailSubject },
			set: func(v *cognitotypes.VerificationMessageTemplateType, value *string) {
				v.EmailSubject = value
			},
		},
		{
			name:  "sms-verification-message",
			value: r.SMSVerificationMessage,
			get:   func(v *cognitotypes.VerificationMessageTemplateType) *string { return v.SmsMessage },
			set: func(v *cognitotypes.VerificationMessageTemplateType, value *string) {
				v.SmsMessage = value
			},
		},
	}
	for _, field := range legacy {
		if field.value == nil {
			continue
		}
		if out == nil {
			out = &cognitotypes.VerificationMessageTemplateType{}
		}
		if current := field.get(out); current != nil && *current != *field.value {
			return nil, fmt.Errorf(
				"%s conflicts with verification-message-template",
				field.name,
			)
		}
		field.set(out, field.value)
	}
	return out, nil
}

func (r *UserPoolResource) mfaInput(
	id string,
) *cognitoidentityprovider.SetUserPoolMfaConfigInput {
	factors := ptr.Value(r.EnabledMFAs)
	in := &cognitoidentityprovider.SetUserPoolMfaConfigInput{
		UserPoolId:            aws.String(id),
		MfaConfiguration:      mfaConfiguration(r.MFAConfiguration),
		WebAuthnConfiguration: webAuthnConfiguration(r.WebAuthnConfiguration),
	}
	if slices.Contains(factors, "EMAIL_OTP") {
		in.EmailMfaConfiguration = &cognitotypes.EmailMfaConfigType{}
		if r.EmailMFAConfiguration != nil {
			in.EmailMfaConfiguration.Message = r.EmailMFAConfiguration.Message
			in.EmailMfaConfiguration.Subject = r.EmailMFAConfiguration.Subject
		}
	}
	if slices.Contains(factors, "SMS_MFA") {
		in.SmsMfaConfiguration = &cognitotypes.SmsMfaConfigType{
			SmsAuthenticationMessage: r.SMSAuthenticationMessage,
			SmsConfiguration:         smsConfiguration(r.SMSConfiguration),
		}
	}
	if slices.Contains(factors, "SOFTWARE_TOKEN_MFA") {
		in.SoftwareTokenMfaConfiguration = &cognitotypes.SoftwareTokenMfaConfigType{
			Enabled: true,
		}
	}
	return in
}

func webAuthnConfiguration(
	in *UserPoolWebAuthnConfiguration,
) *cognitotypes.WebAuthnConfigurationType {
	if in == nil {
		return nil
	}
	out := &cognitotypes.WebAuthnConfigurationType{
		RelyingPartyId: in.RelyingPartyID,
	}
	if in.UserVerification != nil {
		out.UserVerification = cognitotypes.UserVerificationType(*in.UserVerification)
	}
	if in.FactorConfiguration != nil {
		out.FactorConfiguration = cognitotypes.WebAuthnFactorConfigurationType(
			*in.FactorConfiguration,
		)
	}
	return out
}

func desiredTags(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *UserPoolResource) syncTags(
	ctx context.Context,
	client cognitoClient,
	arn string,
) error {
	if arn == "" {
		return fmt.Errorf("sync user pool tags: prior output has no ARN")
	}
	return tagsync.Sync(
		ctx,
		desiredTags(ptr.Value(r.Tags)),
		func(ctx context.Context) (map[string]string, error) {
			out, err := client.ListTagsForResource(
				ctx,
				&cognitoidentityprovider.ListTagsForResourceInput{
					ResourceArn: aws.String(arn),
				},
			)
			if err != nil {
				return nil, fmt.Errorf("list user pool tags: %w", err)
			}
			if out == nil {
				return nil, fmt.Errorf("list user pool tags: empty response")
			}
			return out.Tags, nil
		},
		func(ctx context.Context, tags map[string]string) error {
			_, err := client.TagResource(ctx, &cognitoidentityprovider.TagResourceInput{
				ResourceArn: aws.String(arn),
				Tags:        tags,
			})
			if err != nil {
				return fmt.Errorf("tag user pool: %w", err)
			}
			return nil
		},
		func(ctx context.Context, keys []string) error {
			_, err := client.UntagResource(ctx, &cognitoidentityprovider.UntagResourceInput{
				ResourceArn: aws.String(arn),
				TagKeys:     keys,
			})
			if err != nil {
				return fmt.Errorf("untag user pool: %w", err)
			}
			return nil
		},
	)
}
