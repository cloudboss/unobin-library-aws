package lambdamicrovms

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
)

type MicrovmAuthToken struct {
	MicrovmIdentifier   string              `ub:"microvm-identifier"`
	ExpirationInMinutes int64               `ub:"expiration-in-minutes"`
	AllowedPorts        []PortSpecification `ub:"allowed-ports"`
}

func (r MicrovmAuthToken) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.AtLeast(r.ExpirationInMinutes, 1),
			constraint.AtMost(r.ExpirationInMinutes, 60)).
			Message("expiration-in-minutes must be between 1 and 60"),
		constraint.Must(constraint.NotEmpty(r.AllowedPorts)).
			Message("allowed-ports must not be empty"),
		constraint.ForEach(r.AllowedPorts, func(p PortSpecification) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ExactlyOneOf(p.AllPorts, p.Port, p.Range),
				constraint.When(constraint.Present(p.AllPorts)).
					Require(constraint.IsTrue(p.AllPorts)).
					Message("all-ports must be true"),
				constraint.When(constraint.Present(p.Port)).
					Require(constraint.AtLeast(p.Port, 1), constraint.AtMost(p.Port, 65535)).
					Message("port must be between 1 and 65535"),
				constraint.When(constraint.Present(p.Range)).
					Require(constraint.AtLeast(p.Range.StartPort, 1),
						constraint.AtMost(p.Range.StartPort, 65535),
						constraint.AtLeast(p.Range.EndPort, 1),
						constraint.AtMost(p.Range.EndPort, 65535)).
					Message("range ports must be between 1 and 65535"),
				constraint.When(constraint.Present(p.Range)).
					Require(constraint.AtMost(p.Range.StartPort, p.Range.EndPort)).
					Message("port range start must be no greater than end"),
			}
		}),
	}
}

func (r *MicrovmAuthToken) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmAuthTokenOutput, error) {
	panic("unimplemented")
}
