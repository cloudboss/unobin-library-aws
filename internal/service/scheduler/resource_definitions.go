package scheduler

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *ScheduleResource) ResourceDefinition() runtime.ResourceDefinition[ScheduleResource, *ScheduleResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ScheduleResource, *ScheduleResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input ScheduleResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[ScheduleResource, *ScheduleResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ScheduleResource]{
				runtime.InputField(func(input *ScheduleResource) **string { return &input.Name }),
				runtime.InputField(func(input *ScheduleResource) **string { return &input.GroupName }),
			},
		},
	}
}
