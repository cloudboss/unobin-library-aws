package lambdamicrovms

import "context"

type SuspendMicrovm struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

func (r *SuspendMicrovm) Run(ctx context.Context, cfg *awsCfg) (*SuspendMicrovmOutput, error) {
	panic("unimplemented")
}
