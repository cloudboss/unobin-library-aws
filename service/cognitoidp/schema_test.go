package cognitoidp_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/cognitoidp"
)

const unobinModulePath = "github.com/cloudboss/unobin"

func TestUserPoolSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 1)
	require.Contains(t, schema.Resources, "user-pool")
	got := schema.Resources["user-pool"]

	assert.Equal(t, expectedUserPoolInputs(), got.Inputs)
	assert.Equal(t, map[string]typecheck.Type{
		"arn":                       typecheck.TString(),
		"creation-date":             typecheck.TString(),
		"custom-domain":             typecheck.TString(),
		"domain":                    typecheck.TString(),
		"endpoint":                  typecheck.TString(),
		"estimated-number-of-users": typecheck.TInteger(),
		"last-modified-date":        typecheck.TString(),
		"provider-name":             typecheck.TString(),
		"provider-url":              typecheck.TString(),
		"user-pool-id":              typecheck.TString(),
		"user-pool-tier-actual":     typecheck.TString(),
	}, got.Outputs)
	assert.Empty(t, got.SensitiveInputs)
	assert.Empty(t, got.SensitiveOutputs)
	assert.Empty(t, got.Defaults)

	messages := make(map[string]lang.ConstraintSpec, len(got.Constraints))
	for _, value := range got.Constraints {
		messages[value.Message] = value
	}
	expectedMessages := []string{
		"name must contain at least 1 character",
		"name must contain at most 128 characters",
		"alias-attributes holds at most 3 entries",
		"alias-attributes values must be valid alias attributes",
		"username-attributes holds at most 2 entries",
		"username-attributes values must be email or phone_number",
		"alias-attributes conflicts with username-attributes",
		"auto-verified-attributes holds at most 2 entries",
		"auto-verified-attributes values must be email or phone_number",
		"deletion-protection must be ACTIVE or INACTIVE",
		"recovery-mechanisms must contain 1 or 2 entries",
		"recovery mechanism name is invalid",
		"recovery mechanism priority must be 1 or 2",
		"unused-account-validity-days conflicts with temporary-password-validity-days",
		"unused-account-validity-days must be between 0 and 365",
		"email-sending-account must be COGNITO_DEFAULT or DEVELOPER",
		"custom-email-sender lambda-version must be V1_0",
		"custom-sms-sender lambda-version must be V1_0",
		"pre-token-generation-config lambda-version is invalid",
		"inbound-federation lambda-version must be V1_0",
		"custom senders require kms-key-id",
		"pre-token-generation Lambda ARNs must match",
		"mfa-configuration must be OFF, ON, or OPTIONAL",
		"enabled-mfas holds at most 3 entries",
		"enabled-mfas contains an invalid factor",
		"ON or OPTIONAL MFA requires an enabled factor",
		"OFF MFA forbids enabled factors",
		"web-authn factor-configuration is invalid",
		"web-authn user-verification must be required or preferred",
		"web-authn relying-party-id must be 1 to 63 characters",
		"multi-factor WebAuthn requires verification and an eligible tier",
		"password minimum-length must be between 6 and 99",
		"password-history-size must be between 0 and 24",
		"temporary-password-validity-days must be between 0 and 365",
		"password history requires advanced security",
		"allowed-first-auth-factors must contain 1 to 4 entries",
		"allowed-first-auth-factors contains an invalid factor",
		"sign-in-policy requires user-pool-tier ESSENTIALS or PLUS",
		"schema must contain 1 to 50 attributes",
		"schema attribute-data-type is invalid",
		"string-attribute-constraints requires String",
		"number-attribute-constraints requires Number",
		"default-email-option must be CONFIRM_WITH_CODE or CONFIRM_WITH_LINK",
		"attributes-require-verification-before-update holds at most 2 entries",
		"verification attributes must be email or phone_number",
		"advanced-security-mode must be OFF, AUDIT, or ENFORCED",
		"custom-auth-mode must be AUDIT or ENFORCED",
		"advanced security requires user-pool-tier PLUS",
		"user-pool-tier must be LITE, ESSENTIALS, or PLUS",
		"tags holds at most 50 entries",
	}
	require.Len(t, got.Constraints, len(expectedMessages))
	for _, message := range expectedMessages {
		assert.Contains(t, messages, message)
	}
	assert.Equal(t, lang.ConstraintSpec{
		Kind: "forbidden-with",
		Fields: []string{
			"input.alias-attributes",
			"input.username-attributes",
		},
		Message: "alias-attributes conflicts with username-attributes",
	}, messages["alias-attributes conflicts with username-attributes"])
	assert.Equal(t, lang.ConstraintSpec{
		Kind: "forbidden-with",
		Fields: []string{
			"input.admin-create-user-config.unused-account-validity-days",
			"input.password-policy.temporary-password-validity-days",
		},
		Message: "unused-account-validity-days conflicts with " +
			"temporary-password-validity-days",
	}, messages["unused-account-validity-days conflicts with temporary-password-validity-days"])
	assert.Equal(t,
		"((input.password-policy.password-history-size != null) && "+
			"(input.password-policy.password-history-size == null || "+
			"input.password-policy.password-history-size > 0))",
		messages["password history requires advanced security"].When,
	)
}

func TestUserPoolReplacementFields(t *testing.T) {
	resource := &svc.UserPoolResource{}
	assert.Equal(t, []string{
		"alias-attributes",
		"username-attributes",
		"username-configuration",
	}, resource.ReplaceFields())
}

func expectedUserPoolInputs() map[string]typecheck.Type {
	lambdaVersion := typecheck.TObject([]typecheck.ObjectField{
		{Name: "lambda-arn", Type: typecheck.TString()},
		{Name: "lambda-version", Type: typecheck.TString()},
	})
	recoveryMechanism := typecheck.TObject([]typecheck.ObjectField{
		{Name: "name", Type: typecheck.TString()},
		{Name: "priority", Type: typecheck.TInteger()},
	})
	accountRecovery := typecheck.TObject([]typecheck.ObjectField{
		{Name: "recovery-mechanisms", Type: typecheck.TList(recoveryMechanism)},
	})
	inviteTemplate := typecheck.TObject([]typecheck.ObjectField{
		{Name: "email-message", Type: typecheck.TString(), Optional: true},
		{Name: "email-subject", Type: typecheck.TString(), Optional: true},
		{Name: "sms-message", Type: typecheck.TString(), Optional: true},
	})
	adminCreate := typecheck.TObject([]typecheck.ObjectField{
		{Name: "allow-admin-create-user-only", Type: typecheck.TBoolean(), Optional: true},
		{Name: "invite-message-template", Type: inviteTemplate, Optional: true},
		{Name: "unused-account-validity-days", Type: typecheck.TInteger(), Optional: true},
	})
	device := typecheck.TObject([]typecheck.ObjectField{
		{Name: "challenge-required-on-new-device", Type: typecheck.TBoolean(), Optional: true},
		{
			Name:     "device-only-remembered-on-user-prompt",
			Type:     typecheck.TBoolean(),
			Optional: true,
		},
	})
	email := typecheck.TObject([]typecheck.ObjectField{
		{Name: "configuration-set", Type: typecheck.TString(), Optional: true},
		{Name: "email-sending-account", Type: typecheck.TString(), Optional: true},
		{Name: "from-email-address", Type: typecheck.TString(), Optional: true},
		{Name: "reply-to-email-address", Type: typecheck.TString(), Optional: true},
		{Name: "source-arn", Type: typecheck.TString(), Optional: true},
	})
	emailMFA := typecheck.TObject([]typecheck.ObjectField{
		{Name: "message", Type: typecheck.TString(), Optional: true},
		{Name: "subject", Type: typecheck.TString(), Optional: true},
	})
	lambda := typecheck.TObject([]typecheck.ObjectField{
		{Name: "create-auth-challenge", Type: typecheck.TString(), Optional: true},
		{Name: "custom-email-sender", Type: lambdaVersion, Optional: true},
		{Name: "custom-message", Type: typecheck.TString(), Optional: true},
		{Name: "custom-sms-sender", Type: lambdaVersion, Optional: true},
		{Name: "define-auth-challenge", Type: typecheck.TString(), Optional: true},
		{Name: "inbound-federation", Type: lambdaVersion, Optional: true},
		{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
		{Name: "post-authentication", Type: typecheck.TString(), Optional: true},
		{Name: "post-confirmation", Type: typecheck.TString(), Optional: true},
		{Name: "pre-authentication", Type: typecheck.TString(), Optional: true},
		{Name: "pre-sign-up", Type: typecheck.TString(), Optional: true},
		{Name: "pre-token-generation", Type: typecheck.TString(), Optional: true},
		{Name: "pre-token-generation-config", Type: lambdaVersion, Optional: true},
		{Name: "user-migration", Type: typecheck.TString(), Optional: true},
		{
			Name:     "verify-auth-challenge-response",
			Type:     typecheck.TString(),
			Optional: true,
		},
	})
	password := typecheck.TObject([]typecheck.ObjectField{
		{Name: "minimum-length", Type: typecheck.TInteger(), Optional: true},
		{Name: "password-history-size", Type: typecheck.TInteger(), Optional: true},
		{Name: "require-lowercase", Type: typecheck.TBoolean(), Optional: true},
		{Name: "require-numbers", Type: typecheck.TBoolean(), Optional: true},
		{Name: "require-symbols", Type: typecheck.TBoolean(), Optional: true},
		{Name: "require-uppercase", Type: typecheck.TBoolean(), Optional: true},
		{
			Name:     "temporary-password-validity-days",
			Type:     typecheck.TInteger(),
			Optional: true,
		},
	})
	signIn := typecheck.TObject([]typecheck.ObjectField{
		{
			Name:     "allowed-first-auth-factors",
			Type:     typecheck.TList(typecheck.TString()),
			Optional: true,
		},
	})
	numberConstraints := typecheck.TObject([]typecheck.ObjectField{
		{Name: "max-value", Type: typecheck.TString(), Optional: true},
		{Name: "min-value", Type: typecheck.TString(), Optional: true},
	})
	stringConstraints := typecheck.TObject([]typecheck.ObjectField{
		{Name: "max-length", Type: typecheck.TString(), Optional: true},
		{Name: "min-length", Type: typecheck.TString(), Optional: true},
	})
	schemaAttribute := typecheck.TObject([]typecheck.ObjectField{
		{Name: "attribute-data-type", Type: typecheck.TString()},
		{Name: "developer-only-attribute", Type: typecheck.TBoolean(), Optional: true},
		{Name: "mutable", Type: typecheck.TBoolean(), Optional: true},
		{Name: "name", Type: typecheck.TString()},
		{
			Name:     "number-attribute-constraints",
			Type:     numberConstraints,
			Optional: true,
		},
		{Name: "required", Type: typecheck.TBoolean(), Optional: true},
		{
			Name:     "string-attribute-constraints",
			Type:     stringConstraints,
			Optional: true,
		},
	})
	sms := typecheck.TObject([]typecheck.ObjectField{
		{Name: "sns-caller-arn", Type: typecheck.TString()},
		{Name: "external-id", Type: typecheck.TString(), Optional: true},
		{Name: "sns-region", Type: typecheck.TString(), Optional: true},
	})
	attributeUpdate := typecheck.TObject([]typecheck.ObjectField{
		{
			Name: "attributes-require-verification-before-update",
			Type: typecheck.TList(typecheck.TString()),
		},
	})
	additionalFlows := typecheck.TObject([]typecheck.ObjectField{
		{Name: "custom-auth-mode", Type: typecheck.TString(), Optional: true},
	})
	addOns := typecheck.TObject([]typecheck.ObjectField{
		{
			Name:     "advanced-security-additional-flows",
			Type:     additionalFlows,
			Optional: true,
		},
		{Name: "advanced-security-mode", Type: typecheck.TString()},
	})
	username := typecheck.TObject([]typecheck.ObjectField{
		{Name: "case-sensitive", Type: typecheck.TBoolean()},
	})
	verification := typecheck.TObject([]typecheck.ObjectField{
		{Name: "default-email-option", Type: typecheck.TString(), Optional: true},
		{Name: "email-message", Type: typecheck.TString(), Optional: true},
		{Name: "email-message-by-link", Type: typecheck.TString(), Optional: true},
		{Name: "email-subject", Type: typecheck.TString(), Optional: true},
		{Name: "email-subject-by-link", Type: typecheck.TString(), Optional: true},
		{Name: "sms-message", Type: typecheck.TString(), Optional: true},
	})
	webAuthn := typecheck.TObject([]typecheck.ObjectField{
		{Name: "factor-configuration", Type: typecheck.TString(), Optional: true},
		{Name: "relying-party-id", Type: typecheck.TString(), Optional: true},
		{Name: "user-verification", Type: typecheck.TString(), Optional: true},
	})

	return map[string]typecheck.Type{
		"account-recovery-setting": optional(accountRecovery),
		"admin-create-user-config": optional(adminCreate),
		"alias-attributes": optional(
			typecheck.TList(typecheck.TString()),
		),
		"auto-verified-attributes": optional(
			typecheck.TList(typecheck.TString()),
		),
		"deletion-protection":        optional(typecheck.TString()),
		"device-configuration":       optional(device),
		"email-configuration":        optional(email),
		"email-mfa-configuration":    optional(emailMFA),
		"email-verification-message": optional(typecheck.TString()),
		"email-verification-subject": optional(typecheck.TString()),
		"enabled-mfas": optional(
			typecheck.TList(typecheck.TString()),
		),
		"lambda-config":                  optional(lambda),
		"mfa-configuration":              optional(typecheck.TString()),
		"name":                           typecheck.TString(),
		"password-policy":                optional(password),
		"schema":                         optional(typecheck.TList(schemaAttribute)),
		"sign-in-policy":                 optional(signIn),
		"sms-authentication-message":     optional(typecheck.TString()),
		"sms-configuration":              optional(sms),
		"sms-verification-message":       optional(typecheck.TString()),
		"tags":                           optional(typecheck.TMap(typecheck.TString())),
		"user-attribute-update-settings": optional(attributeUpdate),
		"user-pool-add-ons":              optional(addOns),
		"user-pool-tier":                 optional(typecheck.TString()),
		"username-attributes": optional(
			typecheck.TList(typecheck.TString()),
		),
		"username-configuration":        optional(username),
		"verification-message-template": optional(verification),
		"web-authn-configuration":       optional(webAuthn),
	}
}

func optional(value typecheck.Type) typecheck.Type {
	return typecheck.TOptional(value)
}

func readLibrarySchema(t *testing.T) *runtime.LibrarySchema {
	t.Helper()
	moduleRoot := moduleRoot(t, ".")
	unobinRoot := unobinRoot(t)
	schema, warnings, err := goschema.Read(".", moduleRoot, unobinRoot)
	require.NoError(t, err)
	require.Empty(t, warnings)
	return schema
}

func moduleRoot(t *testing.T, directory string) goschema.ModuleRoot {
	t.Helper()
	command := exec.Command("go", "list", "-m", "-f", "{{.Path}}\n{{.Dir}}")
	command.Dir = directory
	out, err := command.Output()
	require.NoError(t, err)
	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, parts, 2)
	return goschema.ModuleRoot{Path: parts[0], Dir: parts[1]}
}

func unobinRoot(t *testing.T) goschema.ModuleRoot {
	t.Helper()
	out, err := exec.Command(
		"go", "list", "-m", "-f", "{{.Dir}}", unobinModulePath,
	).Output()
	require.NoError(t, err)
	directory := strings.TrimSpace(string(out))
	require.NotEmpty(t, directory)
	return goschema.ModuleRoot{Path: unobinModulePath, Dir: directory}
}
