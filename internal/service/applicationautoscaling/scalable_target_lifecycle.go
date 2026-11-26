package applicationautoscaling

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

func registerTarget(
	ctx context.Context, client *sdk.Client, input *sdk.RegisterScalableTargetInput, clock targetClock,
) error {
	return retryTarget(ctx, clock, 2*time.Minute, func(ctx context.Context) error {
		_, err := client.RegisterScalableTarget(ctx, input)
		return err
	}, func(err error) bool {
		var invalid *types.ValidationException
		return errors.As(err, &invalid) &&
			(strings.Contains(invalid.ErrorMessage(), "Unable to assume IAM role") ||
				strings.Contains(invalid.ErrorMessage(), "ECS service doesn't exist"))
	})
}

func (r ScalableTargetResource) create(
	ctx context.Context, client *sdk.Client, clock targetClock,
) (*ScalableTargetResourceOutput, error) {
	if err := registerTarget(ctx, client, r.prepare(nil), clock); err != nil {
		return nil, fmt.Errorf("register scalable target: %w", err)
	}
	var output *ScalableTargetResourceOutput
	err := retryTarget(ctx, clock, 2*time.Minute, func(ctx context.Context) error {
		var err error
		output, err = readTarget(ctx, client, r.key())
		return err
	}, func(err error) bool { return errors.Is(err, runtime.ErrNotFound) })
	if err == nil {
		return output, nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	if cleanupErr := deleteTarget(cleanupCtx, client, r.key(), clock); cleanupErr != nil {
		err = errors.Join(err, fmt.Errorf("clean up scalable target after failed create: %w", cleanupErr))
	}
	return nil, err
}

func findTarget(
	ctx context.Context, client *sdk.Client, key *ScalableTargetResourceOutput,
) (*types.ScalableTarget, error) {
	if key == nil {
		return nil, fmt.Errorf("scalable target prior output is missing")
	}
	in := &sdk.DescribeScalableTargetsInput{
		ResourceIds:       []string{key.ResourceID},
		ServiceNamespace:  types.ServiceNamespace(key.ServiceNamespace),
		ScalableDimension: types.ScalableDimension(key.ScalableDimension),
	}
	var targets []types.ScalableTarget
	for {
		page, err := client.DescribeScalableTargets(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("describe scalable targets: %w", err)
		}
		targets = append(targets, page.ScalableTargets...)
		if aws.ToString(page.NextToken) == "" {
			break
		}
		in.NextToken = page.NextToken
	}
	if len(targets) != 1 {
		return nil, runtime.ErrNotFound
	}
	target := &targets[0]
	if aws.ToString(target.ResourceId) != key.ResourceID ||
		string(target.ServiceNamespace) != key.ServiceNamespace ||
		string(target.ScalableDimension) != key.ScalableDimension {
		return nil, runtime.ErrNotFound
	}
	return target, nil
}

func readTarget(
	ctx context.Context, client *sdk.Client, key *ScalableTargetResourceOutput,
) (*ScalableTargetResourceOutput, error) {
	target, err := findTarget(ctx, client, key)
	if err != nil {
		return nil, err
	}
	output := &ScalableTargetResourceOutput{
		ResourceID: aws.ToString(target.ResourceId), ServiceNamespace: string(target.ServiceNamespace),
		ScalableDimension: string(target.ScalableDimension),
		ScalableTargetARN: aws.ToString(target.ScalableTargetARN),
		MinCapacity:       int64(aws.ToInt32(target.MinCapacity)),
		MaxCapacity:       int64(aws.ToInt32(target.MaxCapacity)), RoleARN: aws.ToString(target.RoleARN),
	}
	if state := target.SuspendedState; state != nil {
		output.SuspendedState = &SuspendedState{
			DynamicScalingInSuspended:  aws.Bool(aws.ToBool(state.DynamicScalingInSuspended)),
			DynamicScalingOutSuspended: aws.Bool(aws.ToBool(state.DynamicScalingOutSuspended)),
			ScheduledScalingSuspended:  aws.Bool(aws.ToBool(state.ScheduledScalingSuspended)),
		}
	}
	tags, err := client.ListTagsForResource(ctx, &sdk.ListTagsForResourceInput{
		ResourceARN: aws.String(output.ScalableTargetARN),
	})
	if err != nil {
		return nil, fmt.Errorf("list scalable target tags: %w", err)
	}
	output.Tags = userTags(tags.Tags)
	return output, nil
}

func (r ScalableTargetResource) update(
	ctx context.Context, client *sdk.Client, prior targetPrior, clock targetClock,
) (*ScalableTargetResourceOutput, error) {
	if prior.Outputs == nil {
		return nil, fmt.Errorf("scalable target prior output is missing")
	}
	observed := prior.Observed
	if observed == nil {
		observed = prior.Outputs
	}
	var desired map[string]string
	if r.Tags != nil {
		desired = *r.Tags
	}
	if !maps.Equal(userTags(observed.Tags), userTags(desired)) {
		if err := syncTags(ctx, client, observed.ScalableTargetARN, desired); err != nil {
			return nil, err
		}
	}
	effective := *observed
	effective.ResourceID = prior.Outputs.ResourceID
	effective.ServiceNamespace = prior.Outputs.ServiceNamespace
	effective.ScalableDimension = prior.Outputs.ScalableDimension
	request := r.prepare(&effective)
	changed := r.MinCapacity != observed.MinCapacity || r.MaxCapacity != observed.MaxCapacity ||
		(r.RoleARN != nil && *r.RoleARN != observed.RoleARN) ||
		(r.SuspendedState != nil && !sameSuspension(r.SuspendedState, observed.SuspendedState))
	if changed {
		if err := registerTarget(ctx, client, request, clock); err != nil {
			return nil, err
		}
	}
	return readTarget(ctx, client, prior.Outputs)
}

func sameSuspension(a, b *SuspendedState) bool {
	if a == nil || b == nil {
		return a == b
	}
	return aws.ToBool(a.DynamicScalingInSuspended) == aws.ToBool(b.DynamicScalingInSuspended) &&
		aws.ToBool(a.DynamicScalingOutSuspended) == aws.ToBool(b.DynamicScalingOutSuspended) &&
		aws.ToBool(a.ScheduledScalingSuspended) == aws.ToBool(b.ScheduledScalingSuspended)
}

func deleteTarget(
	ctx context.Context, client *sdk.Client, key *ScalableTargetResourceOutput, clock targetClock,
) error {
	if key == nil {
		return fmt.Errorf("scalable target prior output is missing")
	}
	_, err := client.DeregisterScalableTarget(ctx, &sdk.DeregisterScalableTargetInput{
		ResourceId:        aws.String(key.ResourceID),
		ServiceNamespace:  types.ServiceNamespace(key.ServiceNamespace),
		ScalableDimension: types.ScalableDimension(key.ScalableDimension),
	})
	var missing *types.ObjectNotFoundException
	if errors.As(err, &missing) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("deregister scalable target: %w", err)
	}
	return retryTarget(ctx, clock, 5*time.Minute, func(ctx context.Context) error {
		_, err := findTarget(ctx, client, key)
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return errTargetPresent
	}, func(err error) bool { return errors.Is(err, errTargetPresent) })
}

var errTargetPresent = errors.New("scalable target still exists")

func userTags(tags map[string]string) map[string]string {
	filtered := make(map[string]string)
	for k, v := range tags {
		if !strings.HasPrefix(k, "aws:") {
			filtered[k] = v
		}
	}
	return filtered
}

func syncTags(ctx context.Context, client *sdk.Client, arn string, tags map[string]string) error {
	return tagsync.Sync(ctx, tags,
		func(ctx context.Context) (map[string]string, error) {
			out, err := client.ListTagsForResource(ctx, &sdk.ListTagsForResourceInput{
				ResourceARN: aws.String(arn),
			})
			if err != nil {
				return nil, err
			}
			return out.Tags, nil
		},
		func(ctx context.Context, tags map[string]string) error {
			_, err := client.TagResource(ctx, &sdk.TagResourceInput{
				ResourceARN: aws.String(arn), Tags: tags,
			})
			return err
		},
		func(ctx context.Context, keys []string) error {
			_, err := client.UntagResource(ctx, &sdk.UntagResourceInput{
				ResourceARN: aws.String(arn), TagKeys: keys,
			})
			return err
		},
	)
}
