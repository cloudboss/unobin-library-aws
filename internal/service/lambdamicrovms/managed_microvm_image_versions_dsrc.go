package lambdamicrovms

import "context"

type ManagedMicrovmImageVersions struct {
	ImageIdentifier string `ub:"image-identifier"`
}

func (r *ManagedMicrovmImageVersions) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*ManagedMicrovmImageVersionsOutput, error) {
	panic("unimplemented")
}
