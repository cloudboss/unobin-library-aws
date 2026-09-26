package cloudwatch

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *DashboardResource) ResourceDefinition() runtime.ResourceDefinition[DashboardResource, *DashboardResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[DashboardResource, *DashboardResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input DashboardResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Equality: []runtime.InputEqualityRule[DashboardResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *DashboardResource) *string { return &input.Body }),
				func(prior, desired string) bool {
					return r.EquivalentInput("body", DashboardResource{Body: prior}, DashboardResource{Body: desired})
				},
			),
		},
		Replace: runtime.Replacement[DashboardResource, *DashboardResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[DashboardResource]{
				runtime.InputField(func(input *DashboardResource) *string { return &input.Name }),
			},
		},
	}
}

func (r *MetricAlarmResource) ResourceDefinition() runtime.ResourceDefinition[MetricAlarmResource, *MetricAlarmResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[MetricAlarmResource, *MetricAlarmResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[MetricAlarmResource, *MetricAlarmResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[MetricAlarmResource]{
				runtime.InputField(func(input *MetricAlarmResource) *string { return &input.AlarmName }),
			},
		},
	}
}
