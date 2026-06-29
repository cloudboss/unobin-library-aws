package lambdamicrovms

import "context"

type MicrovmImageVersionData struct {
	ImageIdentifier string `ub:"image-identifier"`
	ImageVersion    string `ub:"image-version"`
}

func (r *MicrovmImageVersionData) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageVersionDataOutput, error) {
	panic("unimplemented")
}
