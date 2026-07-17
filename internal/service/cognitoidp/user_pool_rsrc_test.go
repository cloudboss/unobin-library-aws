package cognitoidp

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPoolMetadata(t *testing.T) {
	r := UserPoolResource{}
	assert.Equal(t, 1, r.SchemaVersion())
	assert.Equal(t, []string{
		"alias-attributes",
		"username-attributes",
		"username-configuration",
	}, r.ReplaceFields())
}

func TestUserPoolCreateInputRequiresName(t *testing.T) {
	r := UserPoolResource{Name: "unobin-pool"}
	in, err := r.createInput()
	require.NoError(t, err)
	require.NotNil(t, in.PoolName)
	assert.Equal(t, "unobin-pool", *in.PoolName)
}

func TestUserPoolCreateInputMapsDirectProperties(t *testing.T) {
	r := UserPoolResource{
		Name:                   "unobin-pool",
		AliasAttributes:        &[]string{"email"},
		AutoVerifiedAttributes: &[]string{"email"},
		DeletionProtection:     aws.String("ACTIVE"),
		EmailConfiguration: &UserPoolEmailConfiguration{
			EmailSendingAccount: aws.String("DEVELOPER"),
			SourceARN: aws.String(
				"arn:aws:ses:us-east-1:123456789012:identity/example.com",
			),
		},
		EnabledMFAs:      &[]string{"EMAIL_OTP"},
		MFAConfiguration: aws.String("OPTIONAL"),
		PasswordPolicy: &UserPoolPasswordPolicy{
			MinimumLength:    aws.Int64(12),
			RequireLowercase: aws.Bool(true),
			RequireNumbers:   aws.Bool(true),
			RequireSymbols:   aws.Bool(true),
			RequireUppercase: aws.Bool(true),
		},
		Tags:         &map[string]string{"env": "test", "aws:reserved": "ignored"},
		UserPoolTier: aws.String("PLUS"),
	}
	in, err := r.createInput()
	require.NoError(t, err)
	assert.Equal(t, []cognitotypes.AliasAttributeType{"email"}, in.AliasAttributes)
	assert.Equal(t, []cognitotypes.VerifiedAttributeType{"email"},
		in.AutoVerifiedAttributes)
	assert.Equal(t, cognitotypes.DeletionProtectionTypeActive, in.DeletionProtection)
	require.NotNil(t, in.EmailConfiguration)
	assert.Equal(t, cognitotypes.EmailSendingAccountTypeDeveloper,
		in.EmailConfiguration.EmailSendingAccount)
	require.NotNil(t, in.Policies)
	require.NotNil(t, in.Policies.PasswordPolicy)
	assert.Equal(t, int32(12), aws.ToInt32(in.Policies.PasswordPolicy.MinimumLength))
	assert.Equal(t, map[string]string{"env": "test"}, in.UserPoolTags)
	assert.Equal(t, cognitotypes.UserPoolTierTypePlus, in.UserPoolTier)
	assert.Empty(t, in.MfaConfiguration)
}

func TestUserPoolCreateInputMirrorsLegacyVerificationFields(t *testing.T) {
	r := UserPoolResource{
		Name:                     "unobin-pool",
		EmailVerificationMessage: aws.String("Your code is {####}"),
		EmailVerificationSubject: aws.String("Verification code"),
		SMSVerificationMessage:   aws.String("Code: {####}"),
	}
	in, err := r.createInput()
	require.NoError(t, err)
	require.NotNil(t, in.VerificationMessageTemplate)
	assert.Equal(t, r.EmailVerificationMessage, in.VerificationMessageTemplate.EmailMessage)
	assert.Equal(t, r.EmailVerificationSubject, in.VerificationMessageTemplate.EmailSubject)
	assert.Equal(t, r.SMSVerificationMessage, in.VerificationMessageTemplate.SmsMessage)
}

func TestUserPoolCreateInputRejectsLegacyVerificationConflict(t *testing.T) {
	r := UserPoolResource{
		Name:                     "unobin-pool",
		EmailVerificationMessage: aws.String("Your code is {####}"),
		VerificationMessageTemplate: &UserPoolVerificationTemplate{
			EmailMessage: aws.String("Different code: {####}"),
		},
	}
	_, err := r.createInput()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email-verification-message")
}
