package cognitoidp

import "github.com/cloudboss/unobin/pkg/constraint"

func (r UserPoolDomainResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(
			constraint.MinItems(r.Domain, 1),
			constraint.MaxItems(r.Domain, 63),
		).Message("domain must contain 1 to 63 characters"),
		constraint.Must(constraint.MinItems(r.UserPoolID, 1)).
			Message("user-pool-id must contain at least 1 character"),
		constraint.When(constraint.Present(r.ManagedLoginVersion)).Require(
			constraint.OneOf(r.ManagedLoginVersion, 1, 2),
		).Message("managed-login-version must be 1 or 2"),
		constraint.When(constraint.Present(r.CustomDomainConfig.SecurityPolicy)).Require(
			constraint.OneOf(
				r.CustomDomainConfig.SecurityPolicy,
				"TLS_V1",
				"TLS_V1_2_2021",
				"TLS_V1_3_2025",
			),
		).Message("custom-domain-config.security-policy is invalid"),
	}
}
