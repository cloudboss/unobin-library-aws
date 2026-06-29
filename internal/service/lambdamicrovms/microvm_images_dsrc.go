package lambdamicrovms

import "context"

type MicrovmImages struct {
	NameFilter *string `ub:"name-filter"`
}

func (r *MicrovmImages) Read(ctx context.Context, cfg *awsCfg) (*MicrovmImagesOutput, error) {
	panic("unimplemented")
}
