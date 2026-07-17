package cognitoidp

import (
	"context"
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

var (
	userPoolNamePattern         = regexp.MustCompile(`^[A-Za-z0-9_+=,.@ -]+$`)
	userPoolRegionPattern       = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	userPoolARNPartitionPattern = regexp.MustCompile(`^aws(?:-[a-z]+)*$`)
	userPoolARNAccountPattern   = regexp.MustCompile(`^[0-9]{12}$`)
	schemaAttributeNamePattern  = regexp.MustCompile(`^[\p{L}\p{M}\p{S}\p{N}\p{P}]+$`)
	emailMessagePattern         = regexp.MustCompile(`^[\p{L}\p{M}\p{S}\p{N}\p{P}\s]*$`)
	emailSubjectPattern         = regexp.MustCompile(`^[\p{L}\p{M}\p{S}\p{N}\p{P}\s]+$`)
	smsMessagePattern           = regexp.MustCompile(`^[^\n]*\z`)
	linkPlaceholder             = regexp.MustCompile(`\{##[^{}]*##\}`)
)

var standardUserPoolAttributeNames = []string{
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
}

func (r *UserPoolResource) ValidateInputs(context.Context, *awsCfg) error {
	if err := validateUserPoolName(r.Name); err != nil {
		return err
	}
	if err := validateStringList(
		"alias-attributes",
		ptr.Value(r.AliasAttributes),
		[]string{"email", "phone_number", "preferred_username"},
	); err != nil {
		return err
	}
	if err := validateStringList(
		"username-attributes",
		ptr.Value(r.UsernameAttributes),
		[]string{"email", "phone_number"},
	); err != nil {
		return err
	}
	if len(ptr.Value(r.AliasAttributes)) > 0 && len(ptr.Value(r.UsernameAttributes)) > 0 {
		return fmt.Errorf("alias-attributes conflicts with username-attributes")
	}
	if err := validateStringList(
		"auto-verified-attributes",
		ptr.Value(r.AutoVerifiedAttributes),
		[]string{"email", "phone_number"},
	); err != nil {
		return err
	}
	if r.DeletionProtection != nil &&
		!slices.Contains([]string{"ACTIVE", "INACTIVE"}, *r.DeletionProtection) {
		return fmt.Errorf("deletion-protection must be ACTIVE or INACTIVE")
	}
	if err := validateRecoverySetting(r.AccountRecoverySetting); err != nil {
		return err
	}
	if err := validateAdminCreateUserConfig(r.AdminCreateUserConfig); err != nil {
		return err
	}
	if r.AdminCreateUserConfig != nil &&
		r.AdminCreateUserConfig.UnusedAccountValidityDays != nil &&
		r.PasswordPolicy != nil &&
		r.PasswordPolicy.TemporaryPasswordValidityDays != nil {
		return fmt.Errorf(
			"unused-account-validity-days conflicts with temporary-password-validity-days",
		)
	}
	if err := validateEmailConfiguration(r.EmailConfiguration); err != nil {
		return err
	}
	if err := validateLambdaConfiguration(r.LambdaConfig); err != nil {
		return err
	}
	if err := r.validateMFA(); err != nil {
		return err
	}
	if err := r.validatePasswordAndSignInPolicies(); err != nil {
		return err
	}
	if err := validateUserPoolSchema(r.Schema); err != nil {
		return err
	}
	if err := validateSMSConfiguration(r.SMSConfiguration); err != nil {
		return err
	}
	if err := validateVerificationMessages(r); err != nil {
		return err
	}
	if err := validateUserAttributeUpdateSettings(r); err != nil {
		return err
	}
	if err := r.validateAddOnsAndTier(); err != nil {
		return err
	}
	if err := validateUserPoolTags(ptr.Value(r.Tags)); err != nil {
		return err
	}
	_, err := r.verificationTemplate()
	return err
}

func validateUserPoolName(name string) error {
	length := utf8.RuneCountInString(name)
	if length < 1 || length > 128 || !userPoolNamePattern.MatchString(name) {
		return fmt.Errorf(
			"name must be 1 to 128 characters using letters, numbers, spaces, and _+=,.@-",
		)
	}
	return nil
}

func validateStringList(field string, values, allowed []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("%s contains invalid value %q", field, value)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%s contains duplicate value %q", field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateRecoverySetting(setting *UserPoolAccountRecoverySetting) error {
	if setting == nil {
		return nil
	}
	mechanisms := setting.RecoveryMechanisms
	if len(mechanisms) < 1 || len(mechanisms) > 2 {
		return fmt.Errorf("recovery-mechanisms must contain 1 or 2 entries")
	}
	names := make(map[string]struct{}, len(mechanisms))
	priorities := make(map[int64]struct{}, len(mechanisms))
	for _, mechanism := range mechanisms {
		if !slices.Contains(
			[]string{"admin_only", "verified_email", "verified_phone_number"},
			mechanism.Name,
		) {
			return fmt.Errorf("recovery-mechanisms contains invalid name %q", mechanism.Name)
		}
		if _, ok := names[mechanism.Name]; ok {
			return fmt.Errorf("recovery-mechanisms contains duplicate name %q", mechanism.Name)
		}
		if mechanism.Priority < 1 || mechanism.Priority > 2 {
			return fmt.Errorf("recovery mechanism priority must be 1 or 2")
		}
		if _, ok := priorities[mechanism.Priority]; ok {
			return fmt.Errorf("recovery mechanism priority %d is duplicated", mechanism.Priority)
		}
		names[mechanism.Name] = struct{}{}
		priorities[mechanism.Priority] = struct{}{}
	}
	if _, adminOnly := names["admin_only"]; adminOnly && len(mechanisms) != 1 {
		return fmt.Errorf("admin_only must be the only recovery mechanism")
	}
	return nil
}

func validateAdminCreateUserConfig(config *UserPoolAdminCreateUserConfig) error {
	if config == nil {
		return nil
	}
	if days := config.UnusedAccountValidityDays; days != nil && (*days < 0 || *days > 365) {
		return fmt.Errorf("unused-account-validity-days must be between 0 and 365")
	}
	if config.InviteMessageTemplate == nil {
		return nil
	}
	template := config.InviteMessageTemplate
	if err := validateEmailMessage(
		"invite-message-template.email-message",
		template.EmailMessage,
	); err != nil {
		return err
	}
	if template.EmailMessage != nil &&
		(!strings.Contains(*template.EmailMessage, "{username}") ||
			!strings.Contains(*template.EmailMessage, "{####}")) {
		return fmt.Errorf(
			"invite-message-template.email-message must contain {username} and {####}",
		)
	}
	if err := validateEmailSubject(
		"invite-message-template.email-subject",
		template.EmailSubject,
	); err != nil {
		return err
	}
	if err := validateSMSMessage(
		"invite-message-template.sms-message",
		template.SMSMessage,
	); err != nil {
		return err
	}
	if template.SMSMessage != nil &&
		(!strings.Contains(*template.SMSMessage, "{username}") ||
			!strings.Contains(*template.SMSMessage, "{####}")) {
		return fmt.Errorf(
			"invite-message-template.sms-message must contain {username} and {####}",
		)
	}
	return nil
}

func validateEmailConfiguration(config *UserPoolEmailConfiguration) error {
	if config == nil {
		return nil
	}
	if config.EmailSendingAccount != nil && !slices.Contains(
		[]string{"COGNITO_DEFAULT", "DEVELOPER"},
		*config.EmailSendingAccount,
	) {
		return fmt.Errorf("email-sending-account must be COGNITO_DEFAULT or DEVELOPER")
	}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{name: "from-email-address", value: config.FromEmailAddress},
		{name: "reply-to-email-address", value: config.ReplyToEmailAddress},
	} {
		if field.value == nil {
			continue
		}
		if _, err := mail.ParseAddress(*field.value); err != nil {
			return fmt.Errorf("%s must be a valid email address", field.name)
		}
	}
	if config.SourceARN != nil && !validARN(*config.SourceARN, "ses") {
		return fmt.Errorf("source-arn must be a valid SES ARN")
	}
	return nil
}

func validateLambdaConfiguration(config *UserPoolLambdaConfiguration) error {
	if config == nil {
		return nil
	}
	triggers := []struct {
		name  string
		value *string
	}{
		{name: "create-auth-challenge", value: config.CreateAuthChallenge},
		{name: "custom-message", value: config.CustomMessage},
		{name: "define-auth-challenge", value: config.DefineAuthChallenge},
		{name: "post-authentication", value: config.PostAuthentication},
		{name: "post-confirmation", value: config.PostConfirmation},
		{name: "pre-authentication", value: config.PreAuthentication},
		{name: "pre-sign-up", value: config.PreSignUp},
		{name: "pre-token-generation", value: config.PreTokenGeneration},
		{name: "user-migration", value: config.UserMigration},
		{name: "verify-auth-challenge-response", value: config.VerifyAuthChallenge},
	}
	for _, trigger := range triggers {
		if trigger.value != nil && !validARN(*trigger.value, "lambda") {
			return fmt.Errorf("lambda-config.%s must be a valid Lambda ARN", trigger.name)
		}
	}
	if config.KMSKeyID != nil && !validARN(*config.KMSKeyID, "kms") {
		return fmt.Errorf("lambda-config.kms-key-id must be a valid KMS ARN")
	}
	if err := validateLambdaVersion(
		"custom-email-sender", config.CustomEmailSender, []string{"V1_0"},
	); err != nil {
		return err
	}
	if err := validateLambdaVersion(
		"custom-sms-sender", config.CustomSMSSender, []string{"V1_0"},
	); err != nil {
		return err
	}
	if err := validateLambdaVersion(
		"pre-token-generation-config",
		config.PreTokenGenerationConfig,
		[]string{"V1_0", "V2_0", "V3_0"},
	); err != nil {
		return err
	}
	if err := validateLambdaVersion(
		"inbound-federation", config.InboundFederation, []string{"V1_0"},
	); err != nil {
		return err
	}
	if (config.CustomEmailSender != nil || config.CustomSMSSender != nil) &&
		(config.KMSKeyID == nil || *config.KMSKeyID == "") {
		return fmt.Errorf("lambda-config.kms-key-id is required with a custom sender")
	}
	if config.PreTokenGeneration != nil && config.PreTokenGenerationConfig != nil &&
		*config.PreTokenGeneration != config.PreTokenGenerationConfig.LambdaARN {
		return fmt.Errorf("pre-token-generation and pre-token-generation-config Lambda ARNs must match")
	}
	return nil
}

func validateLambdaVersion(
	name string,
	config *UserPoolLambdaVersionConfiguration,
	versions []string,
) error {
	if config == nil {
		return nil
	}
	if !validARN(config.LambdaARN, "lambda") {
		return fmt.Errorf("lambda-config.%s.lambda-arn must be a valid Lambda ARN", name)
	}
	if !slices.Contains(versions, config.LambdaVersion) {
		return fmt.Errorf(
			"lambda-config.%s.lambda-version must be one of %s",
			name,
			strings.Join(versions, ", "),
		)
	}
	return nil
}

func validARN(value, service string) bool {
	parsed, err := awsarn.Parse(value)
	if err != nil || !userPoolARNPartitionPattern.MatchString(parsed.Partition) ||
		parsed.Service != service ||
		!userPoolARNAccountPattern.MatchString(parsed.AccountID) ||
		parsed.Resource == "" {
		return false
	}
	if service == "iam" {
		return parsed.Region == ""
	}
	return userPoolRegionPattern.MatchString(parsed.Region)
}

func (r *UserPoolResource) validateMFA() error {
	if r.MFAConfiguration != nil &&
		!slices.Contains([]string{"OFF", "ON", "OPTIONAL"}, *r.MFAConfiguration) {
		return fmt.Errorf("mfa-configuration must be OFF, ON, or OPTIONAL")
	}
	factors := ptr.Value(r.EnabledMFAs)
	if err := validateStringList(
		"enabled-mfas",
		factors,
		[]string{"SMS_MFA", "EMAIL_OTP", "SOFTWARE_TOKEN_MFA"},
	); err != nil {
		return err
	}
	multiFactorWebAuthn := r.WebAuthnConfiguration != nil &&
		r.WebAuthnConfiguration.FactorConfiguration != nil &&
		*r.WebAuthnConfiguration.FactorConfiguration ==
			"MULTI_FACTOR_WITH_USER_VERIFICATION"
	mode := "OFF"
	if r.MFAConfiguration != nil {
		mode = *r.MFAConfiguration
	}
	if (mode == "ON" || mode == "OPTIONAL") && len(factors) == 0 && !multiFactorWebAuthn {
		return fmt.Errorf("mfa-configuration %s requires an enabled MFA or multi-factor WebAuthn", mode)
	}
	if mode == "OFF" && (len(factors) > 0 || multiFactorWebAuthn) {
		return fmt.Errorf("mfa-configuration OFF forbids enabled MFA factors")
	}
	if slices.Contains(factors, "SMS_MFA") && r.SMSConfiguration == nil {
		return fmt.Errorf("SMS_MFA requires sms-configuration")
	}
	if slices.Contains(factors, "EMAIL_OTP") {
		if r.UserPoolTier != nil && *r.UserPoolTier == "LITE" {
			return fmt.Errorf("EMAIL_OTP requires user-pool-tier ESSENTIALS or PLUS")
		}
		if r.EmailConfiguration == nil {
			return fmt.Errorf("EMAIL_OTP requires email-configuration")
		}
		if r.AccountRecoverySetting == nil ||
			len(r.AccountRecoverySetting.RecoveryMechanisms) < 2 {
			return fmt.Errorf("EMAIL_OTP requires at least two recovery mechanisms")
		}
	} else if r.EmailMFAConfiguration != nil {
		return fmt.Errorf("email-mfa-configuration requires EMAIL_OTP in enabled-mfas")
	}
	if err := r.validateWebAuthn(); err != nil {
		return err
	}
	return nil
}

func (r *UserPoolResource) validateWebAuthn() error {
	config := r.WebAuthnConfiguration
	if config == nil {
		return nil
	}
	if config.FactorConfiguration != nil && !slices.Contains(
		[]string{"SINGLE_FACTOR", "MULTI_FACTOR_WITH_USER_VERIFICATION"},
		*config.FactorConfiguration,
	) {
		return fmt.Errorf("web-authn-configuration.factor-configuration is invalid")
	}
	if config.UserVerification != nil &&
		!slices.Contains([]string{"required", "preferred"}, *config.UserVerification) {
		return fmt.Errorf("web-authn-configuration.user-verification must be required or preferred")
	}
	if config.RelyingPartyID != nil {
		length := utf8.RuneCountInString(*config.RelyingPartyID)
		if length < 1 || length > 63 {
			return fmt.Errorf("web-authn-configuration.relying-party-id must be 1 to 63 characters")
		}
	}
	if config.FactorConfiguration != nil &&
		*config.FactorConfiguration == "MULTI_FACTOR_WITH_USER_VERIFICATION" {
		if r.UserPoolTier != nil && *r.UserPoolTier == "LITE" {
			return fmt.Errorf("multi-factor WebAuthn requires user-pool-tier ESSENTIALS or PLUS")
		}
		if config.UserVerification == nil || *config.UserVerification != "required" {
			return fmt.Errorf("multi-factor WebAuthn requires user-verification required")
		}
	}
	return nil
}

func (r *UserPoolResource) validatePasswordAndSignInPolicies() error {
	if policy := r.PasswordPolicy; policy != nil {
		if policy.MinimumLength != nil && (*policy.MinimumLength < 6 || *policy.MinimumLength > 99) {
			return fmt.Errorf("password-policy.minimum-length must be between 6 and 99")
		}
		if policy.PasswordHistorySize != nil &&
			(*policy.PasswordHistorySize < 0 || *policy.PasswordHistorySize > 24) {
			return fmt.Errorf("password-policy.password-history-size must be between 0 and 24")
		}
		if policy.TemporaryPasswordValidityDays != nil &&
			(*policy.TemporaryPasswordValidityDays < 0 ||
				*policy.TemporaryPasswordValidityDays > 365) {
			return fmt.Errorf(
				"password-policy.temporary-password-validity-days must be between 0 and 365",
			)
		}
		if policy.PasswordHistorySize != nil && *policy.PasswordHistorySize > 0 &&
			(r.UserPoolAddOns == nil || r.UserPoolAddOns.AdvancedSecurityMode == "OFF") {
			return fmt.Errorf("password history requires advanced security")
		}
	}
	if r.SignInPolicy != nil {
		factors := ptr.Value(r.SignInPolicy.AllowedFirstAuthFactors)
		if len(factors) == 0 {
			return fmt.Errorf("allowed-first-auth-factors must not be empty")
		}
		if err := validateStringList(
			"allowed-first-auth-factors",
			factors,
			[]string{"PASSWORD", "EMAIL_OTP", "SMS_OTP", "WEB_AUTHN"},
		); err != nil {
			return err
		}
		if r.UserPoolTier != nil && *r.UserPoolTier == "LITE" {
			return fmt.Errorf("sign-in-policy requires user-pool-tier ESSENTIALS or PLUS")
		}
	}
	return nil
}

func validateUserPoolSchema(schema *[]UserPoolSchemaAttribute) error {
	if schema == nil {
		return nil
	}
	if len(*schema) < 1 || len(*schema) > 50 {
		return fmt.Errorf("schema must contain 1 to 50 attributes")
	}
	seen := make(map[string]struct{}, len(*schema))
	for index, attribute := range *schema {
		if _, ok := seen[attribute.Name]; ok {
			return fmt.Errorf("schema contains duplicate attribute %q", attribute.Name)
		}
		seen[attribute.Name] = struct{}{}
		length := utf8.RuneCountInString(attribute.Name)
		if length < 1 || length > 20 ||
			!schemaAttributeNamePattern.MatchString(attribute.Name) ||
			strings.HasPrefix(attribute.Name, "custom:") ||
			strings.HasPrefix(attribute.Name, "dev:") {
			return fmt.Errorf(
				"schema[%d].name must be an unprefixed 1 to 20 character attribute name",
				index,
			)
		}
		if !slices.Contains(
			[]string{"String", "Number", "DateTime", "Boolean"},
			attribute.AttributeDataType,
		) {
			return fmt.Errorf("schema[%d].attribute-data-type is invalid", index)
		}
		if attribute.StringAttributeConstraints != nil &&
			attribute.AttributeDataType != "String" {
			return fmt.Errorf(
				"schema[%d].string-attribute-constraints requires String attribute-data-type",
				index,
			)
		}
		if attribute.NumberAttributeConstraints != nil &&
			attribute.AttributeDataType != "Number" {
			return fmt.Errorf(
				"schema[%d].number-attribute-constraints requires Number attribute-data-type",
				index,
			)
		}
		if err := validateNumberConstraints(index, attribute.NumberAttributeConstraints); err != nil {
			return err
		}
		if err := validateStringConstraints(index, attribute.StringAttributeConstraints); err != nil {
			return err
		}
	}
	return nil
}

func isStandardUserPoolAttribute(name string) bool {
	return slices.Contains(standardUserPoolAttributeNames, name)
}

func validateNumberConstraints(
	index int,
	constraints *UserPoolNumberAttributeConstraints,
) error {
	if constraints == nil {
		return nil
	}
	minValue, err := parseDecimal(constraints.MinValue)
	if err != nil {
		return fmt.Errorf("schema[%d].number-attribute-constraints.min-value is invalid", index)
	}
	maxValue, err := parseDecimal(constraints.MaxValue)
	if err != nil {
		return fmt.Errorf("schema[%d].number-attribute-constraints.max-value is invalid", index)
	}
	if minValue != nil && maxValue != nil && minValue.Cmp(maxValue) > 0 {
		return fmt.Errorf(
			"schema[%d].number-attribute-constraints.min-value must not exceed max-value",
			index,
		)
	}
	return nil
}

func parseDecimal(value *string) (*big.Rat, error) {
	if value == nil {
		return nil, nil
	}
	number := new(big.Rat)
	if _, ok := number.SetString(*value); !ok {
		return nil, fmt.Errorf("invalid decimal")
	}
	return number, nil
}

func validateStringConstraints(
	index int,
	constraints *UserPoolStringAttributeConstraints,
) error {
	if constraints == nil {
		return nil
	}
	minLength, err := parseAttributeLength(constraints.MinLength)
	if err != nil {
		return fmt.Errorf("schema[%d].string-attribute-constraints.min-length is invalid", index)
	}
	maxLength, err := parseAttributeLength(constraints.MaxLength)
	if err != nil {
		return fmt.Errorf("schema[%d].string-attribute-constraints.max-length is invalid", index)
	}
	if minLength != nil && maxLength != nil && *minLength > *maxLength {
		return fmt.Errorf(
			"schema[%d].string-attribute-constraints.min-length must not exceed max-length",
			index,
		)
	}
	return nil
}

func parseAttributeLength(value *string) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	length, err := strconv.ParseInt(*value, 10, 64)
	if err != nil || length < 0 || length > 2048 {
		return nil, fmt.Errorf("invalid length")
	}
	return &length, nil
}

func validateSMSConfiguration(config *UserPoolSMSConfiguration) error {
	if config == nil {
		return nil
	}
	if !validARN(config.SNSCallerARN, "iam") {
		return fmt.Errorf("sms-configuration.sns-caller-arn must be a valid IAM ARN")
	}
	if config.SNSRegion != nil && !userPoolRegionPattern.MatchString(*config.SNSRegion) {
		return fmt.Errorf("sms-configuration.sns-region must be a valid AWS Region")
	}
	return nil
}

func validateVerificationMessages(r *UserPoolResource) error {
	emailCodeMessages := []struct {
		name  string
		value *string
	}{
		{name: "email-verification-message", value: r.EmailVerificationMessage},
	}
	smsCodeMessages := []struct {
		name  string
		value *string
	}{
		{name: "sms-authentication-message", value: r.SMSAuthenticationMessage},
		{name: "sms-verification-message", value: r.SMSVerificationMessage},
	}
	subjects := []struct {
		name  string
		value *string
	}{
		{name: "email-verification-subject", value: r.EmailVerificationSubject},
	}
	if r.EmailMFAConfiguration != nil {
		emailCodeMessages = append(emailCodeMessages, struct {
			name  string
			value *string
		}{name: "email-mfa-configuration.message", value: r.EmailMFAConfiguration.Message})
		subjects = append(subjects, struct {
			name  string
			value *string
		}{name: "email-mfa-configuration.subject", value: r.EmailMFAConfiguration.Subject})
	}
	if r.VerificationMessageTemplate != nil {
		template := r.VerificationMessageTemplate
		emailCodeMessages = append(emailCodeMessages,
			struct {
				name  string
				value *string
			}{name: "verification-message-template.email-message", value: template.EmailMessage},
		)
		smsCodeMessages = append(smsCodeMessages,
			struct {
				name  string
				value *string
			}{name: "verification-message-template.sms-message", value: template.SMSMessage},
		)
		if err := validateEmailMessage(
			"verification-message-template.email-message-by-link",
			template.EmailMessageByLink,
		); err != nil {
			return err
		}
		if template.EmailMessageByLink != nil &&
			!linkPlaceholder.MatchString(*template.EmailMessageByLink) {
			return fmt.Errorf(
				"verification-message-template.email-message-by-link must contain {##link text##}",
			)
		}
		if template.DefaultEmailOption != nil && !slices.Contains(
			[]string{"CONFIRM_WITH_CODE", "CONFIRM_WITH_LINK"},
			*template.DefaultEmailOption,
		) {
			return fmt.Errorf(
				"verification-message-template.default-email-option must be " +
					"CONFIRM_WITH_CODE or CONFIRM_WITH_LINK",
			)
		}
		subjects = append(subjects,
			struct {
				name  string
				value *string
			}{name: "verification-message-template.email-subject", value: template.EmailSubject},
			struct {
				name  string
				value *string
			}{
				name:  "verification-message-template.email-subject-by-link",
				value: template.EmailSubjectByLink,
			},
		)
	}
	for _, message := range emailCodeMessages {
		if err := validateEmailMessage(message.name, message.value); err != nil {
			return err
		}
		if message.value != nil && !strings.Contains(*message.value, "{####}") {
			return fmt.Errorf("%s must contain {####}", message.name)
		}
	}
	for _, message := range smsCodeMessages {
		if err := validateSMSMessage(message.name, message.value); err != nil {
			return err
		}
		if message.value != nil && !strings.Contains(*message.value, "{####}") {
			return fmt.Errorf("%s must contain {####}", message.name)
		}
	}
	for _, subject := range subjects {
		if err := validateEmailSubject(subject.name, subject.value); err != nil {
			return err
		}
	}
	return nil
}

func validateEmailMessage(name string, value *string) error {
	return validateTextField(name, value, 6, 20000, emailMessagePattern)
}

func validateSMSMessage(name string, value *string) error {
	return validateTextField(name, value, 6, 140, smsMessagePattern)
}

func validateEmailSubject(name string, value *string) error {
	return validateTextField(name, value, 1, 140, emailSubjectPattern)
}

func validateTextField(
	name string,
	value *string,
	minimum int,
	maximum int,
	pattern *regexp.Regexp,
) error {
	if value == nil {
		return nil
	}
	length := utf8.RuneCountInString(*value)
	if length < minimum || length > maximum {
		return fmt.Errorf("%s must contain %d to %d characters", name, minimum, maximum)
	}
	if !pattern.MatchString(*value) {
		return fmt.Errorf("%s contains unsupported characters", name)
	}
	return nil
}

func validateUserAttributeUpdateSettings(r *UserPoolResource) error {
	if r.UserAttributeUpdateSettings == nil {
		return nil
	}
	values := r.UserAttributeUpdateSettings.AttributesRequireVerificationBeforeUpdate
	if err := validateStringList(
		"attributes-require-verification-before-update",
		values,
		[]string{"email", "phone_number"},
	); err != nil {
		return err
	}
	autoVerified := ptr.Value(r.AutoVerifiedAttributes)
	for _, value := range values {
		if !slices.Contains(autoVerified, value) {
			return fmt.Errorf(
				"attributes-require-verification-before-update must be a subset of auto-verified-attributes",
			)
		}
	}
	return nil
}

func (r *UserPoolResource) validateAddOnsAndTier() error {
	if r.UserPoolTier != nil &&
		!slices.Contains([]string{"LITE", "ESSENTIALS", "PLUS"}, *r.UserPoolTier) {
		return fmt.Errorf("user-pool-tier must be LITE, ESSENTIALS, or PLUS")
	}
	if r.UserPoolAddOns == nil {
		return nil
	}
	if !slices.Contains(
		[]string{"OFF", "AUDIT", "ENFORCED"},
		r.UserPoolAddOns.AdvancedSecurityMode,
	) {
		return fmt.Errorf("user-pool-add-ons.advanced-security-mode must be OFF, AUDIT, or ENFORCED")
	}
	if r.UserPoolAddOns.AdvancedSecurityAdditionalFlows != nil {
		mode := r.UserPoolAddOns.AdvancedSecurityAdditionalFlows.CustomAuthMode
		if mode != nil && !slices.Contains([]string{"AUDIT", "ENFORCED"}, *mode) {
			return fmt.Errorf(
				"user-pool-add-ons.advanced-security-additional-flows.custom-auth-mode is invalid",
			)
		}
	}
	if r.UserPoolTier != nil && *r.UserPoolTier != "PLUS" &&
		r.UserPoolAddOns.AdvancedSecurityMode != "OFF" {
		return fmt.Errorf("advanced security requires user-pool-tier PLUS")
	}
	return nil
}

func validateUserPoolTags(tags map[string]string) error {
	if len(tags) > 50 {
		return fmt.Errorf("tags must contain at most 50 entries")
	}
	for key, value := range tags {
		if length := utf8.RuneCountInString(key); length < 1 || length > 128 {
			return fmt.Errorf("tag key must be 1 to 128 characters")
		}
		if utf8.RuneCountInString(value) > 256 {
			return fmt.Errorf("tag value must be at most 256 characters")
		}
	}
	return nil
}
