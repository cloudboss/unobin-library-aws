package ec2stop

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

type SeedAction struct {
	Value string `ub:"echo"`
}

type SeedOutput struct {
	Value string `ub:"value"`
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "ec2-stop-fixture",
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI: "1.1", SuggestedUnobinVersion: "v0.13.0",
		},
		Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[SeedAction, *SeedOutput, runtime.NoConfig](),
		},
	}
}

func (r *SeedAction) Run(context.Context, runtime.NoConfig) (*SeedOutput, error) {
	return &SeedOutput{Value: r.Value}, nil
}
