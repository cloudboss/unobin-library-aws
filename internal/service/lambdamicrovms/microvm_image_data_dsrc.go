package lambdamicrovms

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
)

type MicrovmImageData struct {
	ImageIdentifier *string `ub:"image-identifier"`
	Name            *string `ub:"name"`
}

func (r MicrovmImageData) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.ImageIdentifier, r.Name),
	}
}

func (r *MicrovmImageData) Read(ctx context.Context, cfg *awsCfg) (*MicrovmImageDataOutput, error) {
	panic("unimplemented")
}
