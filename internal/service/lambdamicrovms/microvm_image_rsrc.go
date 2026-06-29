package lambdamicrovms

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type MicrovmImage struct {
	Name                     string              `ub:"name"`
	BaseImageArn             string              `ub:"base-image-arn"`
	BuildRoleArn             string              `ub:"build-role-arn"`
	CodeArtifact             CodeArtifact        `ub:"code-artifact"`
	BaseImageVersion         *string             `ub:"base-image-version"`
	AdditionalOsCapabilities *[]string           `ub:"additional-os-capabilities"`
	CpuConfigurations        *[]CpuConfiguration `ub:"cpu-configurations"`
	Description              *string             `ub:"description"`
	EgressNetworkConnectors  *[]string           `ub:"egress-network-connectors"`
	EnvironmentVariables     *map[string]string  `ub:"environment-variables"`
	Hooks                    *Hooks              `ub:"hooks"`
	Logging                  *Logging            `ub:"logging"`
	Resources                *[]Resources        `ub:"resources"`
	Tags                     *map[string]string  `ub:"tags"`
}

func (r *MicrovmImage) SchemaVersion() int { return 1 }

func (r *MicrovmImage) ReplaceFields() []string {
	return []string{"name"}
}

func (r MicrovmImage) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
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
		constraint.ForEach(r.AdditionalOsCapabilities, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.Equals(value, "ALL")).
					Message("additional-os-capabilities values must be ALL"),
			}
		}),
		constraint.ForEach(r.CpuConfigurations, func(c CpuConfiguration) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.Equals(c.Architecture, "ARM_64")).
					Message("cpu-configurations architecture must be ARM_64"),
			}
		}),
		constraint.When(constraint.Present(r.EgressNetworkConnectors)).
			Require(constraint.MaxItems(r.EgressNetworkConnectors, 10)).
			Message("egress-network-connectors must have at most 10 items"),
		constraint.When(constraint.Present(r.Resources)).
			Require(constraint.MaxItems(r.Resources, 1)).
			Message("resources must have at most one item"),
		constraint.When(constraint.Present(r.Hooks.Port)).
			Require(constraint.AtLeast(r.Hooks.Port, 1), constraint.AtMost(r.Hooks.Port, 65535)).
			Message("hooks port must be between 1 and 65535"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.Run)).
			Require(constraint.OneOf(r.Hooks.MicrovmHooks.Run, "ENABLED", "DISABLED")).
			Message("microvm hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.Resume)).
			Require(constraint.OneOf(r.Hooks.MicrovmHooks.Resume, "ENABLED", "DISABLED")).
			Message("microvm hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.Suspend)).
			Require(constraint.OneOf(r.Hooks.MicrovmHooks.Suspend, "ENABLED", "DISABLED")).
			Message("microvm hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.Terminate)).
			Require(constraint.OneOf(r.Hooks.MicrovmHooks.Terminate, "ENABLED", "DISABLED")).
			Message("microvm hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.RunTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmHooks.RunTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmHooks.RunTimeoutInSeconds, 60)).
			Message("microvm hook timeouts must be between 1 and 60"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.ResumeTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmHooks.ResumeTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmHooks.ResumeTimeoutInSeconds, 60)).
			Message("microvm hook timeouts must be between 1 and 60"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.SuspendTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmHooks.SuspendTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmHooks.SuspendTimeoutInSeconds, 60)).
			Message("microvm hook timeouts must be between 1 and 60"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.TerminateTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmHooks.TerminateTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmHooks.TerminateTimeoutInSeconds, 60)).
			Message("microvm hook timeouts must be between 1 and 60"),
		constraint.When(constraint.Present(r.Hooks.MicrovmImageHooks.Ready)).
			Require(constraint.OneOf(r.Hooks.MicrovmImageHooks.Ready, "ENABLED", "DISABLED")).
			Message("microvm image hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmImageHooks.Validate)).
			Require(constraint.OneOf(r.Hooks.MicrovmImageHooks.Validate, "ENABLED", "DISABLED")).
			Message("microvm image hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmImageHooks.ReadyTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmImageHooks.ReadyTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmImageHooks.ReadyTimeoutInSeconds, 3600)).
			Message("microvm image hook timeouts must be between 1 and 3600"),
		constraint.When(constraint.Present(r.Hooks.MicrovmImageHooks.ValidateTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmImageHooks.ValidateTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmImageHooks.ValidateTimeoutInSeconds, 3600)).
			Message("microvm image hook timeouts must be between 1 and 3600"),
	}
}

func (r *MicrovmImage) Create(ctx context.Context, cfg *awsCfg) (*MicrovmImageOutput, error) {
	panic("unimplemented")
}

func (r *MicrovmImage) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *MicrovmImageOutput,
) (*MicrovmImageOutput, error) {
	panic("unimplemented")
}

func (r *MicrovmImage) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[MicrovmImage, *MicrovmImageOutput],
) (*MicrovmImageOutput, error) {
	panic("unimplemented")
}

func (r *MicrovmImage) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *MicrovmImageOutput,
) error {
	panic("unimplemented")
}
