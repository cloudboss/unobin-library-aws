package cognitoidp

type UserPoolRecoveryMechanism struct {
	Name     string `ub:"name"`
	Priority int64  `ub:"priority"`
}

type UserPoolAccountRecoverySetting struct {
	RecoveryMechanisms []UserPoolRecoveryMechanism `ub:"recovery-mechanisms"`
}

type UserPoolInviteMessageTemplate struct {
	EmailMessage *string `ub:"email-message"`
	EmailSubject *string `ub:"email-subject"`
	SMSMessage   *string `ub:"sms-message"`
}

type UserPoolAdminCreateUserConfig struct {
	AllowAdminCreateUserOnly  *bool                          `ub:"allow-admin-create-user-only"`
	InviteMessageTemplate     *UserPoolInviteMessageTemplate `ub:"invite-message-template"`
	UnusedAccountValidityDays *int64                         `ub:"unused-account-validity-days"`
}

type UserPoolDeviceConfiguration struct {
	ChallengeRequiredOnNewDevice     *bool `ub:"challenge-required-on-new-device"`
	DeviceOnlyRememberedOnUserPrompt *bool `ub:"device-only-remembered-on-user-prompt"`
}

type UserPoolEmailConfiguration struct {
	ConfigurationSet    *string `ub:"configuration-set"`
	EmailSendingAccount *string `ub:"email-sending-account"`
	FromEmailAddress    *string `ub:"from-email-address"`
	ReplyToEmailAddress *string `ub:"reply-to-email-address"`
	SourceARN           *string `ub:"source-arn"`
}

type UserPoolEmailMFAConfiguration struct {
	Message *string `ub:"message"`
	Subject *string `ub:"subject"`
}

type UserPoolLambdaVersionConfiguration struct {
	LambdaARN     string `ub:"lambda-arn"`
	LambdaVersion string `ub:"lambda-version"`
}

type UserPoolLambdaConfiguration struct {
	CreateAuthChallenge      *string                             `ub:"create-auth-challenge"`
	CustomEmailSender        *UserPoolLambdaVersionConfiguration `ub:"custom-email-sender"`
	CustomMessage            *string                             `ub:"custom-message"`
	CustomSMSSender          *UserPoolLambdaVersionConfiguration `ub:"custom-sms-sender"`
	DefineAuthChallenge      *string                             `ub:"define-auth-challenge"`
	InboundFederation        *UserPoolLambdaVersionConfiguration `ub:"inbound-federation"`
	KMSKeyID                 *string                             `ub:"kms-key-id"`
	PostAuthentication       *string                             `ub:"post-authentication"`
	PostConfirmation         *string                             `ub:"post-confirmation"`
	PreAuthentication        *string                             `ub:"pre-authentication"`
	PreSignUp                *string                             `ub:"pre-sign-up"`
	PreTokenGeneration       *string                             `ub:"pre-token-generation"`
	PreTokenGenerationConfig *UserPoolLambdaVersionConfiguration `ub:"pre-token-generation-config"`
	UserMigration            *string                             `ub:"user-migration"`
	VerifyAuthChallenge      *string                             `ub:"verify-auth-challenge-response"`
}

type UserPoolPasswordPolicy struct {
	MinimumLength                 *int64 `ub:"minimum-length"`
	PasswordHistorySize           *int64 `ub:"password-history-size"`
	RequireLowercase              *bool  `ub:"require-lowercase"`
	RequireNumbers                *bool  `ub:"require-numbers"`
	RequireSymbols                *bool  `ub:"require-symbols"`
	RequireUppercase              *bool  `ub:"require-uppercase"`
	TemporaryPasswordValidityDays *int64 `ub:"temporary-password-validity-days"`
}

type UserPoolSignInPolicy struct {
	AllowedFirstAuthFactors *[]string `ub:"allowed-first-auth-factors"`
}

type UserPoolNumberAttributeConstraints struct {
	MaxValue *string `ub:"max-value"`
	MinValue *string `ub:"min-value"`
}

type UserPoolStringAttributeConstraints struct {
	MaxLength *string `ub:"max-length"`
	MinLength *string `ub:"min-length"`
}

type UserPoolSchemaAttribute struct {
	AttributeDataType          string                              `ub:"attribute-data-type"`
	DeveloperOnlyAttribute     *bool                               `ub:"developer-only-attribute"`
	Mutable                    *bool                               `ub:"mutable"`
	Name                       string                              `ub:"name"`
	NumberAttributeConstraints *UserPoolNumberAttributeConstraints `ub:"number-attribute-constraints"`
	Required                   *bool                               `ub:"required"`
	StringAttributeConstraints *UserPoolStringAttributeConstraints `ub:"string-attribute-constraints"`
}

type UserPoolSMSConfiguration struct {
	SNSCallerARN string  `ub:"sns-caller-arn"`
	ExternalID   *string `ub:"external-id"`
	SNSRegion    *string `ub:"sns-region"`
}

type list = []string

type UserPoolAttributeUpdateSettings struct {
	AttributesRequireVerificationBeforeUpdate list `ub:"attributes-require-verification-before-update"`
}

type UserPoolSecurityFlows struct {
	CustomAuthMode *string `ub:"custom-auth-mode"`
}

type UserPoolAddOns struct {
	AdvancedSecurityAdditionalFlows *UserPoolSecurityFlows `ub:"advanced-security-additional-flows"`
	AdvancedSecurityMode            string                 `ub:"advanced-security-mode"`
}

type UserPoolUsernameConfiguration struct {
	CaseSensitive bool `ub:"case-sensitive"`
}

type UserPoolVerificationTemplate struct {
	DefaultEmailOption *string `ub:"default-email-option"`
	EmailMessage       *string `ub:"email-message"`
	EmailMessageByLink *string `ub:"email-message-by-link"`
	EmailSubject       *string `ub:"email-subject"`
	EmailSubjectByLink *string `ub:"email-subject-by-link"`
	SMSMessage         *string `ub:"sms-message"`
}

type UserPoolWebAuthnConfiguration struct {
	FactorConfiguration *string `ub:"factor-configuration"`
	RelyingPartyID      *string `ub:"relying-party-id"`
	UserVerification    *string `ub:"user-verification"`
}

type UserPoolResource struct {
	Name                        string                           `ub:"name"`
	AccountRecoverySetting      *UserPoolAccountRecoverySetting  `ub:"account-recovery-setting"`
	AdminCreateUserConfig       *UserPoolAdminCreateUserConfig   `ub:"admin-create-user-config"`
	AliasAttributes             *[]string                        `ub:"alias-attributes"`
	AutoVerifiedAttributes      *[]string                        `ub:"auto-verified-attributes"`
	DeletionProtection          *string                          `ub:"deletion-protection"`
	DeviceConfiguration         *UserPoolDeviceConfiguration     `ub:"device-configuration"`
	EmailConfiguration          *UserPoolEmailConfiguration      `ub:"email-configuration"`
	EmailVerificationMessage    *string                          `ub:"email-verification-message"`
	EmailVerificationSubject    *string                          `ub:"email-verification-subject"`
	LambdaConfig                *UserPoolLambdaConfiguration     `ub:"lambda-config"`
	MFAConfiguration            *string                          `ub:"mfa-configuration"`
	EnabledMFAs                 *[]string                        `ub:"enabled-mfas"`
	EmailMFAConfiguration       *UserPoolEmailMFAConfiguration   `ub:"email-mfa-configuration"`
	PasswordPolicy              *UserPoolPasswordPolicy          `ub:"password-policy"`
	SignInPolicy                *UserPoolSignInPolicy            `ub:"sign-in-policy"`
	Schema                      *[]UserPoolSchemaAttribute       `ub:"schema"`
	SMSAuthenticationMessage    *string                          `ub:"sms-authentication-message"`
	SMSConfiguration            *UserPoolSMSConfiguration        `ub:"sms-configuration"`
	SMSVerificationMessage      *string                          `ub:"sms-verification-message"`
	Tags                        *map[string]string               `ub:"tags"`
	UserAttributeUpdateSettings *UserPoolAttributeUpdateSettings `ub:"user-attribute-update-settings"`
	UserPoolAddOns              *UserPoolAddOns                  `ub:"user-pool-add-ons"`
	UserPoolTier                *string                          `ub:"user-pool-tier"`
	UsernameAttributes          *[]string                        `ub:"username-attributes"`
	UsernameConfiguration       *UserPoolUsernameConfiguration   `ub:"username-configuration"`
	VerificationMessageTemplate *UserPoolVerificationTemplate    `ub:"verification-message-template"`
	WebAuthnConfiguration       *UserPoolWebAuthnConfiguration   `ub:"web-authn-configuration"`
}

type UserPoolResourceOutput struct {
	UserPoolID             string `ub:"user-pool-id"`
	ARN                    string `ub:"arn"`
	ProviderName           string `ub:"provider-name"`
	ProviderURL            string `ub:"provider-url"`
	Endpoint               string `ub:"endpoint"`
	CreationDate           string `ub:"creation-date"`
	LastModifiedDate       string `ub:"last-modified-date"`
	Domain                 string `ub:"domain"`
	CustomDomain           string `ub:"custom-domain"`
	EstimatedNumberOfUsers int64  `ub:"estimated-number-of-users"`
	UserPoolTierActual     string `ub:"user-pool-tier-actual"`
}
