package cognitoidp

import "github.com/cloudboss/unobin/pkg/constraint"

func (r UserResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.MinItems(r.UserPoolID, 1)).
			Message("user-pool-id must contain at least 1 character"),
		constraint.Must(
			constraint.MinItems(r.Username, 1),
			constraint.MaxItems(r.Username, 128),
		).Message("username must contain 1 to 128 characters"),
		constraint.When(constraint.Present(r.Password)).Require(
			constraint.MinItems(r.Password, 6),
			constraint.MaxItems(r.Password, 256),
		).Message("password must contain 6 to 256 characters"),
		constraint.When(constraint.Present(r.TemporaryPassword)).Require(
			constraint.MinItems(r.TemporaryPassword, 6),
			constraint.MaxItems(r.TemporaryPassword, 256),
		).Message("temporary-password must contain 6 to 256 characters"),
		constraint.ForbiddenWith(r.Password, r.TemporaryPassword).
			Message("password conflicts with temporary-password"),
		constraint.ForEach(r.DesiredDeliveryMediums, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(value, "EMAIL", "SMS")).
					Message("desired-delivery-mediums values must be EMAIL or SMS"),
			}
		}),
		constraint.When(constraint.Present(r.MessageAction)).Require(
			constraint.OneOf(r.MessageAction, "RESEND", "SUPPRESS"),
		).Message("message-action must be RESEND or SUPPRESS"),
	}
}
