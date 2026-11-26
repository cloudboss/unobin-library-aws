package applicationautoscaling

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type ScalableTargetResource struct {
	ResourceID        string             `ub:"resource-id"`
	ServiceNamespace  string             `ub:"service-namespace"`
	ScalableDimension string             `ub:"scalable-dimension"`
	MinCapacity       int64              `ub:"min-capacity"`
	MaxCapacity       int64              `ub:"max-capacity"`
	RoleARN           *string            `ub:"role-arn"`
	SuspendedState    *SuspendedState    `ub:"suspended-state"`
	Tags              *map[string]string `ub:"tags"`
}

type SuspendedState struct {
	DynamicScalingInSuspended  *bool `ub:"dynamic-scaling-in-suspended"`
	DynamicScalingOutSuspended *bool `ub:"dynamic-scaling-out-suspended"`
	ScheduledScalingSuspended  *bool `ub:"scheduled-scaling-suspended"`
}

type ScalableTargetResourceOutput struct {
	ResourceID        string            `ub:"resource-id"`
	ServiceNamespace  string            `ub:"service-namespace"`
	ScalableDimension string            `ub:"scalable-dimension"`
	MinCapacity       int64             `ub:"min-capacity"`
	MaxCapacity       int64             `ub:"max-capacity"`
	RoleARN           string            `ub:"role-arn"`
	SuspendedState    *SuspendedState   `ub:"suspended-state"`
	Tags              map[string]string `ub:"tags"`
	ScalableTargetARN string            `ub:"scalable-target-arn"`
}

type awsCfg = awscfg.Configuration
type targetPrior = runtime.Prior[ScalableTargetResource, *ScalableTargetResourceOutput, *awsCfg]
type targetDefinition = runtime.ResourceDefinition[
	ScalableTargetResource, *ScalableTargetResourceOutput, *awsCfg,
]

func (r ScalableTargetResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.ResourceID, nil)).
			Message("resource-id must be non-null"),
		constraint.Must(constraint.NotEquals(r.ServiceNamespace, nil)).
			Message("service-namespace must be non-null"),
		constraint.Must(constraint.NotEquals(r.ScalableDimension, nil)).
			Message("scalable-dimension must be non-null"),
		constraint.Must(constraint.NotEquals(r.MinCapacity, nil)).
			Message("min-capacity must be non-null"),
		constraint.Must(constraint.NotEquals(r.MaxCapacity, nil)).
			Message("max-capacity must be non-null"),
	}
}

func (r ScalableTargetResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.NullableValue(r.SuspendedState.DynamicScalingInSuspended, false),
		defaults.NullableValue(r.SuspendedState.DynamicScalingOutSuspended, false),
		defaults.NullableValue(r.SuspendedState.ScheduledScalingSuspended, false),
	}
}

func (r *ScalableTargetResource) ResourceDefinition() targetDefinition {
	return targetDefinition{
		SchemaVersion: 1,
		Replace: runtime.Replacement[
			ScalableTargetResource, *ScalableTargetResourceOutput, *awsCfg,
		]{Fields: []runtime.AnyInputField[ScalableTargetResource]{
			runtime.InputField(func(v *ScalableTargetResource) *string { return &v.ResourceID }),
			runtime.InputField(func(v *ScalableTargetResource) *string { return &v.ServiceNamespace }),
			runtime.InputField(func(v *ScalableTargetResource) *string { return &v.ScalableDimension }),
		}},
		StableID: func(_ ScalableTargetResource, out *ScalableTargetResourceOutput) (string, error) {
			if out == nil {
				return "", fmt.Errorf("scalable target prior output is missing")
			}
			return fmt.Sprintf("%q/%q/%q", out.ServiceNamespace, out.ResourceID,
				out.ScalableDimension), nil
		},
	}
}

func newClient(ctx context.Context, cfg *awsCfg) (*applicationautoscaling.Client, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return applicationautoscaling.NewFromConfig(loaded), nil
}

func (r *ScalableTargetResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*ScalableTargetResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, systemClock{})
}

func (r *ScalableTargetResource) Read(
	ctx context.Context, cfg *awsCfg, prior targetPrior,
) (*ScalableTargetResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return readTarget(ctx, client, prior.Outputs)
}

func (r *ScalableTargetResource) Update(
	ctx context.Context, cfg *awsCfg, prior targetPrior,
) (*ScalableTargetResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior, systemClock{})
}

func (r *ScalableTargetResource) Delete(
	ctx context.Context, cfg *awsCfg, prior targetPrior,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return deleteTarget(ctx, client, prior.Outputs, systemClock{})
}

func (r ScalableTargetResource) prepare(
	prior *ScalableTargetResourceOutput,
) *applicationautoscaling.RegisterScalableTargetInput {
	key := r.key()
	role, suspended := r.RoleARN, r.SuspendedState
	if prior != nil {
		key = prior
		if role == nil {
			role = aws.String(prior.RoleARN)
		}
		if suspended == nil {
			suspended = prior.SuspendedState
		}
	}
	in := &applicationautoscaling.RegisterScalableTargetInput{
		ResourceId:        aws.String(key.ResourceID),
		ServiceNamespace:  types.ServiceNamespace(key.ServiceNamespace),
		ScalableDimension: types.ScalableDimension(key.ScalableDimension),
		MinCapacity:       aws.Int32(int32(r.MinCapacity)),
		MaxCapacity:       aws.Int32(int32(r.MaxCapacity)),
	}
	if role != nil && *role != "" {
		in.RoleARN = role
	}
	if suspended != nil {
		in.SuspendedState = &types.SuspendedState{
			DynamicScalingInSuspended:  aws.Bool(aws.ToBool(suspended.DynamicScalingInSuspended)),
			DynamicScalingOutSuspended: aws.Bool(aws.ToBool(suspended.DynamicScalingOutSuspended)),
			ScheduledScalingSuspended:  aws.Bool(aws.ToBool(suspended.ScheduledScalingSuspended)),
		}
	}
	if prior == nil && r.Tags != nil {
		in.Tags = userTags(*r.Tags)
		if len(in.Tags) == 0 {
			in.Tags = nil
		}
	}
	return in
}

func (r ScalableTargetResource) key() *ScalableTargetResourceOutput {
	return &ScalableTargetResourceOutput{ResourceID: r.ResourceID,
		ServiceNamespace: r.ServiceNamespace, ScalableDimension: r.ScalableDimension}
}

type targetClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

func retryTarget(
	ctx context.Context, clock targetClock, timeout time.Duration,
	operation func(context.Context) error, retryable func(error) bool,
) error {
	deadline := clock.Now().Add(timeout)
	delay := 500 * time.Millisecond
	final := false
	var lastError error
	for {
		if cause := context.Cause(ctx); cause != nil {
			if lastError != nil {
				return lastError
			}
			return cause
		}
		err := operation(ctx)
		if err == nil || !retryable(err) {
			return err
		}
		lastError = err
		if context.Cause(ctx) != nil {
			return err
		}
		if final {
			return err
		}
		if !clock.Now().Before(deadline) {
			final = true
		}
		if sleepErr := clock.Sleep(ctx, delay); sleepErr != nil {
			if context.Cause(ctx) != nil {
				return err
			}
			return sleepErr
		}
		delay = min(2*delay, 10*time.Second)
	}
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
func (systemClock) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}
