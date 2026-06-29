package lambdamicrovms

import "context"

type Microvms struct {
	ImageIdentifier *string `ub:"image-identifier"`
	ImageVersion    *string `ub:"image-version"`
}

func (r *Microvms) Read(ctx context.Context, cfg *awsCfg) (*MicrovmsOutput, error) {
	panic("unimplemented")
}
