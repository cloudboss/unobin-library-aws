package lambdamicrovms

import "context"

type TerminateMicrovm struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

func (r *TerminateMicrovm) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*TerminateMicrovmOutput, error) {
	panic("unimplemented")
}
