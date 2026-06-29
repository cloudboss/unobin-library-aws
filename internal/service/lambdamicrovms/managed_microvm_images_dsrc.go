package lambdamicrovms

import "context"

type ManagedMicrovmImages struct{}

func (r *ManagedMicrovmImages) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*ManagedMicrovmImagesOutput, error) {
	panic("unimplemented")
}
