package lambdamicrovms

import "context"

type MicrovmImageVersions struct {
	ImageIdentifier string `ub:"image-identifier"`
}

func (r *MicrovmImageVersions) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageVersionsOutput, error) {
	panic("unimplemented")
}
