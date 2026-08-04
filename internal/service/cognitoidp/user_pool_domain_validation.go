package cognitoidp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
)

var userPoolDomainARNAccountPattern = regexp.MustCompile(
	`^(aws|aws-managed|third-party|aws-marketplace|partner-managed|\d{12}|cw.{10})$`,
)

func (r *UserPoolDomainResource) ValidateInputs(context.Context, *awsCfg) error {
	if len(r.Domain) < 1 || len(r.Domain) > 63 {
		return errors.New("domain must contain 1 to 63 characters")
	}
	if r.UserPoolID == "" {
		return errors.New("user-pool-id must contain at least 1 character")
	}
	if v := r.ManagedLoginVersion; v != nil && *v != 1 && *v != 2 {
		return errors.New("managed-login-version must be 1 or 2")
	}
	if err := validateUserPoolDomainCustomConfig(r.CustomDomainConfig); err != nil {
		return err
	}
	if err := validateUserPoolDomainRouting(r.Routing, r.hasCustomCertificate()); err != nil {
		return err
	}
	return nil
}

func validateUserPoolDomainCustomConfig(config *UserPoolDomainCustomDomainConfig) error {
	if config == nil {
		return nil
	}
	certificate := stringPtrValue(config.CertificateARN)
	if certificate != "" && !validUserPoolDomainARN(certificate) {
		return errors.New("custom-domain-config.certificate-arn must be a valid ARN")
	}
	if config.SecurityPolicy != nil {
		if certificate == "" {
			return errors.New("custom-domain-config.security-policy requires certificate-arn")
		}
		if !slices.Contains([]string{"TLS_V1", "TLS_V1_2_2021", "TLS_V1_3_2025"},
			*config.SecurityPolicy) {
			return errors.New("custom-domain-config.security-policy is invalid")
		}
	}
	return nil
}

func validUserPoolDomainARN(value string) bool {
	parsed, err := awsarn.Parse(value)
	if err != nil || !userPoolARNPartitionPattern.MatchString(parsed.Partition) ||
		parsed.Resource == "" {
		return false
	}
	if parsed.Region != "" && !userPoolRegionPattern.MatchString(parsed.Region) {
		return false
	}
	return parsed.AccountID == "" || userPoolDomainARNAccountPattern.MatchString(parsed.AccountID)
}

func validateUserPoolDomainRouting(routing *UserPoolDomainRouting, custom bool) error {
	if routing == nil || routing.Failover == nil {
		return nil
	}
	if !custom {
		return errors.New("routing.failover requires a custom-domain certificate")
	}
	if routing.Failover.PrimaryRoute53HealthCheckID == "" {
		return errors.New("routing.failover.primary-route53-health-check-id is required")
	}
	if routing.Failover.SecondaryRegion == "" {
		return errors.New("routing.failover.secondary-region is required")
	}
	return nil
}

func (r *UserPoolDomainResource) validateUpdate(prior UserPoolDomainResource) error {
	if prior.hasCustomCertificate() != r.hasCustomCertificate() {
		return fmt.Errorf("custom-domain-config.certificate-arn change requires replacement")
	}
	return nil
}

func (r *UserPoolDomainResource) hasCustomCertificate() bool {
	return userPoolDomainCustomCertificate(r.CustomDomainConfig) != ""
}

func userPoolDomainCustomCertificate(config *UserPoolDomainCustomDomainConfig) string {
	if config == nil {
		return ""
	}
	return stringPtrValue(config.CertificateARN)
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
