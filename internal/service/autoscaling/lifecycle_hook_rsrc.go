package autoscaling

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

// LifecycleHook is an EC2 Auto Scaling lifecycle hook: a pause point in an
// instance's launch or termination that lets a custom action run before the
// transition completes. The whole resource is one PutLifecycleHook upsert, used
// for both create and update, with no eventual-consistency waiter. The hook is
// keyed by the group name plus the hook name, both fixed at create time; a
// change to either makes a new hook.
//
// The one resilience wrapper is on the upsert: a notification target created
// moments earlier -- an SNS topic, an SQS queue, the IAM role that publishes to
// it -- may not have propagated, so AWS cannot yet publish its test message.
// The call retries through that ValidationError for five minutes. Lifecycle
// hooks are not taggable, so the resource has no tag fields.
type LifecycleHook struct {
	AutoScalingGroupName  string  `ub:"autoscaling-group-name"`
	Name                  string  `ub:"name"`
	LifecycleTransition   string  `ub:"lifecycle-transition"`
	DefaultResult         *string `ub:"default-result"`
	HeartbeatTimeout      *int64  `ub:"heartbeat-timeout"`
	NotificationMetadata  *string `ub:"notification-metadata"`
	NotificationTargetARN *string `ub:"notification-target-arn"`
	RoleARN               *string `ub:"role-arn"`
}

// LifecycleHookOutput holds the values the API fills for a lifecycle hook. The
// group and hook names together are the hook's identity, kept so a replacement's
// delete, which receives the prior outputs, targets the old hook. The default
// result is filled with CONTINUE when omitted, so a consumer reads the value the
// cloud settled on.
type LifecycleHookOutput struct {
	AutoScalingGroupName string `ub:"autoscaling-group-name"`
	Name                 string `ub:"name"`
	DefaultResult        string `ub:"default-result"`
}

func (r *LifecycleHook) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs the API fixes when a hook is created. The group
// name and the hook name together form the hook's identity; a change to either
// requires a new hook. The group name is required but not otherwise immutable at
// the API, yet changing it would orphan the old hook under the old group, so it
// is replaced rather than updated in place. Every other field is reconciled by
// re-issuing PutLifecycleHook.
func (r *LifecycleHook) ReplaceFields() []string {
	return []string{"autoscaling-group-name", "name"}
}

// Constraints declares the rules the API enforces on a hook's inputs. The
// lifecycle transition is required and is one of the two transitions. The
// default result, when given, is CONTINUE or ABANDON. The heartbeat timeout,
// when given, is from 30 to 7200 seconds inclusive. The hook name's length (1 to
// 255) and character set stay enforced by the API: the length function counts
// bytes rather than the characters AWS counts, and the character-set rule needs
// a regular-expression match the condition vocabulary does not offer. The
// notification target and role ARNs are independently optional with no
// cross-field rule and are validated only as ARNs by the API.
func (r LifecycleHook) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.LifecycleTransition,
			"autoscaling:EC2_INSTANCE_LAUNCHING", "autoscaling:EC2_INSTANCE_TERMINATING")).
			Message("lifecycle-transition must be autoscaling:EC2_INSTANCE_LAUNCHING " +
				"or autoscaling:EC2_INSTANCE_TERMINATING"),
		constraint.When(constraint.Present(r.DefaultResult)).
			Require(constraint.OneOf(r.DefaultResult, "CONTINUE", "ABANDON")).
			Message("default-result must be CONTINUE or ABANDON"),
		constraint.When(constraint.Present(r.HeartbeatTimeout)).
			Require(constraint.AtLeast(r.HeartbeatTimeout, 30),
				constraint.AtMost(r.HeartbeatTimeout, 7200)).
			Message("heartbeat-timeout must be from 30 to 7200 seconds"),
	}
}

func (r *LifecycleHook) Create(ctx context.Context, cfg any) (*LifecycleHookOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client); err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// put issues PutLifecycleHook, the upsert used for both create and update. A
// notification target created moments earlier may not have propagated, so AWS
// cannot yet publish its test message; the call retries through that
// ValidationError for five minutes and returns the last error on timeout.
func (r *LifecycleHook) put(ctx context.Context, client *autoscaling.Client) error {
	in := r.putInput()
	err := retry.OnError(ctx, isTestMessageNotPublishable, func(ctx context.Context) error {
		_, err := client.PutLifecycleHook(ctx, in)
		return err
	}, retry.WithTimeout(5*time.Minute), retry.WithInterval(10*time.Second))
	if err != nil {
		return fmt.Errorf("put lifecycle hook: %w", err)
	}
	return nil
}

// putInput builds the PutLifecycleHook request. The group name, hook name, and
// lifecycle transition are always sent; every optional field is sent only when
// set, so an omitted value leaves the API to apply its own default. The default
// result is never fabricated, so a fresh hook settles on the server's CONTINUE.
func (r *LifecycleHook) putInput() *autoscaling.PutLifecycleHookInput {
	in := &autoscaling.PutLifecycleHookInput{
		AutoScalingGroupName: aws.String(r.AutoScalingGroupName),
		LifecycleHookName:    aws.String(r.Name),
		LifecycleTransition:  aws.String(r.LifecycleTransition),
		DefaultResult:        r.DefaultResult,
		HeartbeatTimeout:     ptr.Int32(r.HeartbeatTimeout),
		NotificationMetadata: r.NotificationMetadata,
		RoleARN:              r.RoleARN,
	}
	if r.NotificationTargetARN != nil {
		in.NotificationTargetARN = r.NotificationTargetARN
	}
	return in
}

func (r *LifecycleHook) Read(
	ctx context.Context, cfg any, prior *LifecycleHookOutput,
) (*LifecycleHookOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read fetches the hook by its group and hook names and maps it to outputs. The
// API does not error for a missing hook; it returns an empty list, which maps to
// runtime.ErrNotFound. A describe that does error with a ValidationError saying
// the group is gone maps to the same, so a deleted group reads as drift rather
// than failing the read.
func (r *LifecycleHook) read(
	ctx context.Context, client *autoscaling.Client,
) (*LifecycleHookOutput, error) {
	hook, err := findLifecycleHook(ctx, client, r.AutoScalingGroupName, r.Name)
	if err != nil {
		return nil, err
	}
	return &LifecycleHookOutput{
		AutoScalingGroupName: aws.ToString(hook.AutoScalingGroupName),
		Name:                 aws.ToString(hook.LifecycleHookName),
		DefaultResult:        aws.ToString(hook.DefaultResult),
	}, nil
}

func (r *LifecycleHook) Update(
	ctx context.Context, cfg any, prior runtime.Prior[LifecycleHook, *LifecycleHookOutput],
) (*LifecycleHookOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// PutLifecycleHook is a full upsert that resends every parameter without
	// recreating the hook, so the whole call is gated on any input change rather
	// than each field on its own.
	if r.changed(prior) {
		if err := r.put(ctx, client); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

// changed reports whether any input that rides PutLifecycleHook differs from the
// prior inputs. The group name and hook name are not tested: a change to either
// replaces the hook rather than updating it.
func (r *LifecycleHook) changed(
	prior runtime.Prior[LifecycleHook, *LifecycleHookOutput],
) bool {
	p := prior.Inputs
	return runtime.Changed(p.LifecycleTransition, r.LifecycleTransition) ||
		runtime.Changed(p.DefaultResult, r.DefaultResult) ||
		runtime.Changed(p.HeartbeatTimeout, r.HeartbeatTimeout) ||
		runtime.Changed(p.NotificationMetadata, r.NotificationMetadata) ||
		runtime.Changed(p.NotificationTargetARN, r.NotificationTargetARN) ||
		runtime.Changed(p.RoleARN, r.RoleARN)
}

func (r *LifecycleHook) Delete(ctx context.Context, cfg any, prior *LifecycleHookOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// The delete keys off the prior outputs so a replacement removes the old
	// hook. A hook already gone reports a ValidationError saying so, which is
	// treated as success; that substring differs from the not-found message the
	// describe path matches, so it is checked on its own.
	_, err = client.DeleteLifecycleHook(ctx, &autoscaling.DeleteLifecycleHookInput{
		AutoScalingGroupName: aws.String(prior.AutoScalingGroupName),
		LifecycleHookName:    aws.String(prior.Name),
	})
	if err != nil && !isLifecycleHookGone(err) {
		return fmt.Errorf("delete lifecycle hook: %w", err)
	}
	return nil
}

// findLifecycleHook describes the hook by its group and hook names and returns
// it. The API returns an empty list for a missing hook rather than an error, so
// an empty result maps to runtime.ErrNotFound; a ValidationError saying the
// group is gone maps to the same.
func findLifecycleHook(
	ctx context.Context, client *autoscaling.Client, group, name string,
) (*autoscalingtypes.LifecycleHook, error) {
	resp, err := client.DescribeLifecycleHooks(ctx, &autoscaling.DescribeLifecycleHooksInput{
		AutoScalingGroupName: aws.String(group),
		LifecycleHookNames:   []string{name},
	})
	if err != nil {
		if isNotFoundValidation(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe lifecycle hooks: %w", err)
	}
	if len(resp.LifecycleHooks) == 0 {
		return nil, runtime.ErrNotFound
	}
	return &resp.LifecycleHooks[0], nil
}

// isTestMessageNotPublishable reports whether err is the ValidationError AWS
// raises when it cannot yet publish the test message to the notification target,
// the transient race a just-created topic, queue, or role clears. The upsert
// retries on it.
func isTestMessageNotPublishable(err error) bool {
	return isValidationError(err) &&
		messageContains(err, "Unable to publish test message to notification target")
}

// isLifecycleHookGone reports whether a DeleteLifecycleHook error is the
// ValidationError raised when the hook is already gone, which the delete treats
// as success. This message differs from the group-not-found message the describe
// path matches, so it cannot reuse isNotFoundValidation.
func isLifecycleHookGone(err error) bool {
	return isValidationError(err) && messageContains(err, "No Lifecycle Hook found")
}
