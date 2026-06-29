package lambdamicrovms

import "context"

type MicrovmData struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

func (r *MicrovmData) Read(ctx context.Context, cfg *awsCfg) (*MicrovmDataOutput, error) {
	panic("unimplemented")
}
