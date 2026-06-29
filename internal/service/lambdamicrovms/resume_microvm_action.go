package lambdamicrovms

import "context"

type ResumeMicrovm struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

func (r *ResumeMicrovm) Run(ctx context.Context, cfg *awsCfg) (*ResumeMicrovmOutput, error) {
	panic("unimplemented")
}
