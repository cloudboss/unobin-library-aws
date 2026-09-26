package kinesis

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *StreamResource) ResourceDefinition() runtime.ResourceDefinition[StreamResource, *StreamResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[StreamResource, *StreamResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input StreamResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Equality: []runtime.InputEqualityRule[StreamResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *StreamResource) **StreamModeDetails { return &input.StreamModeDetails }),
				func(prior, desired *StreamModeDetails) bool {
					return r.EquivalentInput("stream-mode-details", StreamResource{StreamModeDetails: prior}, StreamResource{StreamModeDetails: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *StreamResource) **[]string { return &input.ShardLevelMetrics }),
				func(prior, desired *[]string) bool {
					return r.EquivalentInput("shard-level-metrics", StreamResource{ShardLevelMetrics: prior}, StreamResource{ShardLevelMetrics: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *StreamResource) **map[string]string { return &input.Tags }),
				func(prior, desired *map[string]string) bool {
					return r.EquivalentInput("tags", StreamResource{Tags: prior}, StreamResource{Tags: desired})
				},
			),
		},
		Replace: runtime.Replacement[StreamResource, *StreamResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[StreamResource]{
				runtime.InputField(func(input *StreamResource) *string { return &input.Name }),
			},
		},
	}
}
