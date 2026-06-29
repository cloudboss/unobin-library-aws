package lambdamicrovms

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
)

type RunMicrovm struct {
	ImageIdentifier          string      `ub:"image-identifier"`
	ImageVersion             *string     `ub:"image-version"`
	ExecutionRoleArn         *string     `ub:"execution-role-arn"`
	IngressNetworkConnectors *[]string   `ub:"ingress-network-connectors"`
	EgressNetworkConnectors  *[]string   `ub:"egress-network-connectors"`
	IdlePolicy               *IdlePolicy `ub:"idle-policy"`
	Logging                  *Logging    `ub:"logging"`
	MaximumDurationInSeconds *int64      `ub:"maximum-duration-in-seconds"`
	RunHookPayloadContent    *string     `ub:"run-hook-payload-content"`
	RunHookPayloadPath       *string     `ub:"run-hook-payload-path"`
}

func (r RunMicrovm) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.RunHookPayloadContent, r.RunHookPayloadPath),
		constraint.When(constraint.Present(r.MaximumDurationInSeconds)).
			Require(constraint.AtLeast(r.MaximumDurationInSeconds, 1),
				constraint.AtMost(r.MaximumDurationInSeconds, 28800)).
			Message("maximum-duration-in-seconds must be between 1 and 28800"),
		constraint.When(constraint.Present(r.Logging)).Require(constraint.Any(
			constraint.All(
				constraint.Present(r.Logging.CloudWatch),
				constraint.Absent(r.Logging.Disabled),
			),
			constraint.All(
				constraint.Absent(r.Logging.CloudWatch),
				constraint.Present(r.Logging.Disabled),
			),
		)).Message("logging must set exactly one of cloud-watch or disabled"),
		constraint.When(constraint.Present(r.Logging.Disabled)).
			Require(constraint.IsTrue(r.Logging.Disabled)).
			Message("logging disabled must be true"),
	}
}

func (r *RunMicrovm) Run(ctx context.Context, cfg *awsCfg) (*MicrovmDataOutput, error) {
	panic("unimplemented")
}
