package lambdamicrovms

import "context"

type MicrovmImageBuildData struct {
	ImageIdentifier string `ub:"image-identifier"`
	ImageVersion    string `ub:"image-version"`
	BuildId         string `ub:"build-id"`
}

func (r *MicrovmImageBuildData) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageBuildDataOutput, error) {
	panic("unimplemented")
}
