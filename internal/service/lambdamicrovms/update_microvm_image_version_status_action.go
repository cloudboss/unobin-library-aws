package lambdamicrovms

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
)

type UpdateMicrovmImageVersionStatus struct {
	ImageIdentifier string `ub:"image-identifier"`
	ImageVersion    string `ub:"image-version"`
	Status          string `ub:"status"`
}

func (r UpdateMicrovmImageVersionStatus) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.Status, "ACTIVE", "INACTIVE")).
			Message("status must be ACTIVE or INACTIVE"),
	}
}

func (r *UpdateMicrovmImageVersionStatus) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageVersionDataOutput, error) {
	panic("unimplemented")
}
