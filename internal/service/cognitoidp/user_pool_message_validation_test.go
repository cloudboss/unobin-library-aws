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

type userPoolTextFieldCase struct {
	name                string
	minimum             int
	maximum             int
	seed                string
	unsupported         string
	requiresPlaceholder bool
	base                func() UserPoolResource
	set                 func(*UserPoolResource, *string)
}

func TestUserPoolMessageValidationBoundaries(t *testing.T) {
	for _, tt := range userPoolTextFieldCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("below minimum", func(t *testing.T) {
				resource := tt.base()
				value := strings.Repeat("a", tt.minimum-1)
				tt.set(&resource, &value)
				err := resource.ValidateInputs(context.Background(), nil)
				require.Error(t, err)
				assert.ErrorContains(t, err, fmt.Sprintf(
					"must contain %d to %d characters", tt.minimum, tt.maximum,
				))
			})
			t.Run("minimum valid", func(t *testing.T) {
				resource := tt.base()
				value := textAtLength(tt.seed, max(tt.minimum, len([]rune(tt.seed))))
				tt.set(&resource, &value)
				require.NoError(t, resource.ValidateInputs(context.Background(), nil))
			})
			t.Run("maximum valid", func(t *testing.T) {
				resource := tt.base()
				value := textAtLength(tt.seed, tt.maximum)
				tt.set(&resource, &value)
				require.NoError(t, resource.ValidateInputs(context.Background(), nil))
			})
			t.Run("above maximum", func(t *testing.T) {
				resource := tt.base()
				value := textAtLength(tt.seed, tt.maximum+1)
				tt.set(&resource, &value)
				err := resource.ValidateInputs(context.Background(), nil)
				require.Error(t, err)
				assert.ErrorContains(t, err, fmt.Sprintf(
					"must contain %d to %d characters", tt.minimum, tt.maximum,
				))
			})
			t.Run("unsupported character", func(t *testing.T) {
				resource := tt.base()
				value := tt.seed + tt.unsupported
				tt.set(&resource, &value)
				err := resource.ValidateInputs(context.Background(), nil)
				require.Error(t, err)
				assert.ErrorContains(t, err, "contains unsupported characters")
			})
			if tt.requiresPlaceholder {
				t.Run("missing placeholder", func(t *testing.T) {
					resource := tt.base()
					value := "message without template value"
					tt.set(&resource, &value)
					require.Error(t, resource.ValidateInputs(context.Background(), nil))
				})
			}
		})
	}
}

func userPoolTextFieldCases() []userPoolTextFieldCase {
	plain := func() UserPoolResource { return UserPoolResource{Name: "unobin-pool"} }
	emailMFA := func() UserPoolResource {
		return validEmailMFAResource(func(*UserPoolResource) {})
	}
	email := func(
		name string,
		seed string,
		base func() UserPoolResource,
		set func(*UserPoolResource, *string),
	) userPoolTextFieldCase {
		return userPoolTextFieldCase{
			name:                name,
			minimum:             6,
			maximum:             20000,
			seed:                seed,
			unsupported:         "\x00",
			requiresPlaceholder: true,
			base:                base,
			set:                 set,
		}
	}
	sms := func(
		name string,
		seed string,
		set func(*UserPoolResource, *string),
	) userPoolTextFieldCase {
		return userPoolTextFieldCase{
			name:                name,
			minimum:             6,
			maximum:             140,
			seed:                seed,
			unsupported:         "\n",
			requiresPlaceholder: true,
			base:                plain,
			set:                 set,
		}
	}
	subject := func(
		name string,
		base func() UserPoolResource,
		set func(*UserPoolResource, *string),
	) userPoolTextFieldCase {
		return userPoolTextFieldCase{
			name:        name,
			minimum:     1,
			maximum:     140,
			seed:        "a",
			unsupported: "\x00",
			base:        base,
			set:         set,
		}
	}
	return []userPoolTextFieldCase{
		email(
			"invite email message",
			"{username}{####}",
			plain,
			func(r *UserPoolResource, value *string) {
				messageValidationInviteTemplate(r).EmailMessage = value
			},
		),
		email(
			"email verification message",
			"{####}",
			plain,
			func(r *UserPoolResource, value *string) { r.EmailVerificationMessage = value },
		),
		email(
			"email mfa message",
			"{####}",
			emailMFA,
			func(r *UserPoolResource, value *string) {
				r.EmailMFAConfiguration = &UserPoolEmailMFAConfiguration{Message: value}
			},
		),
		email(
			"verification email message",
			"{####}",
			plain,
			func(r *UserPoolResource, value *string) {
				messageValidationVerificationTemplate(r).EmailMessage = value
			},
		),
		email(
			"verification email message by link",
			"{####}",
			plain,
			func(r *UserPoolResource, value *string) {
				messageValidationVerificationTemplate(r).EmailMessageByLink = value
			},
		),
		sms(
			"invite sms message",
			"{username}{####}",
			func(r *UserPoolResource, value *string) {
				messageValidationInviteTemplate(r).SMSMessage = value
			},
		),
		sms(
			"sms authentication message",
			"{####}",
			func(r *UserPoolResource, value *string) { r.SMSAuthenticationMessage = value },
		),
		sms(
			"sms verification message",
			"{####}",
			func(r *UserPoolResource, value *string) { r.SMSVerificationMessage = value },
		),
		sms(
			"verification sms message",
			"{####}",
			func(r *UserPoolResource, value *string) {
				messageValidationVerificationTemplate(r).SMSMessage = value
			},
		),
		subject(
			"invite email subject",
			plain,
			func(r *UserPoolResource, value *string) {
				messageValidationInviteTemplate(r).EmailSubject = value
			},
		),
		subject(
			"email verification subject",
			plain,
			func(r *UserPoolResource, value *string) { r.EmailVerificationSubject = value },
		),
		subject(
			"email mfa subject",
			emailMFA,
			func(r *UserPoolResource, value *string) {
				r.EmailMFAConfiguration = &UserPoolEmailMFAConfiguration{Subject: value}
			},
		),
		subject(
			"verification email subject",
			plain,
			func(r *UserPoolResource, value *string) {
				messageValidationVerificationTemplate(r).EmailSubject = value
			},
		),
		subject(
			"verification email subject by link",
			plain,
			func(r *UserPoolResource, value *string) {
				messageValidationVerificationTemplate(r).EmailSubjectByLink = value
			},
		),
	}
}

func messageValidationInviteTemplate(r *UserPoolResource) *UserPoolInviteMessageTemplate {
	if r.AdminCreateUserConfig == nil {
		r.AdminCreateUserConfig = &UserPoolAdminCreateUserConfig{}
	}
	if r.AdminCreateUserConfig.InviteMessageTemplate == nil {
		r.AdminCreateUserConfig.InviteMessageTemplate = &UserPoolInviteMessageTemplate{}
	}
	return r.AdminCreateUserConfig.InviteMessageTemplate
}

func messageValidationVerificationTemplate(r *UserPoolResource) *UserPoolVerificationTemplate {
	if r.VerificationMessageTemplate == nil {
		r.VerificationMessageTemplate = &UserPoolVerificationTemplate{}
	}
	return r.VerificationMessageTemplate
}

func textAtLength(seed string, length int) string {
	runes := []rune(seed)
	return seed + strings.Repeat("a", length-len(runes))
}

func TestUserPoolMessageValidationPermitsUnicodeAndWhitespace(t *testing.T) {
	resource := UserPoolResource{
		Name:                     "unobin-pool",
		EmailVerificationMessage: aws.String("équipe 東京\n{####}"),
		EmailVerificationSubject: aws.String("équipe 東京\t"),
		SMSVerificationMessage:   aws.String("équipe 東京 {####}"),
	}
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}
