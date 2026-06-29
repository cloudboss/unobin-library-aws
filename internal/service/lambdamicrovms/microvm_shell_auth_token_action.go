package lambdamicrovms

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
)

type MicrovmShellAuthToken struct {
	MicrovmIdentifier   string `ub:"microvm-identifier"`
	ExpirationInMinutes int64  `ub:"expiration-in-minutes"`
}

func (r MicrovmShellAuthToken) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.AtLeast(r.ExpirationInMinutes, 1),
			constraint.AtMost(r.ExpirationInMinutes, 60)).
			Message("expiration-in-minutes must be between 1 and 60"),
	}
}

func (r *MicrovmShellAuthToken) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmShellAuthTokenOutput, error) {
	panic("unimplemented")
}
