package lambdamicrovms

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
)

type MicrovmImageBuilds struct {
	ImageIdentifier   string  `ub:"image-identifier"`
	ImageVersion      string  `ub:"image-version"`
	Architecture      *string `ub:"architecture"`
	Chipset           *string `ub:"chipset"`
	ChipsetGeneration *string `ub:"chipset-generation"`
}

func (r MicrovmImageBuilds) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.Architecture)).
			Require(constraint.Equals(r.Architecture, "ARM_64")).
			Message("architecture must be ARM_64"),
		constraint.When(constraint.Present(r.Chipset)).
			Require(constraint.Equals(r.Chipset, "GRAVITON")).
			Message("chipset must be GRAVITON"),
	}
}

func (r *MicrovmImageBuilds) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageBuildsOutput, error) {
	panic("unimplemented")
}
