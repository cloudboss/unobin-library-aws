package ec2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	volumeAttachmentTimeout         = 5 * time.Minute
	volumeAttachmentInstanceTimeout = 10 * time.Minute
	volumeAttachmentPollInterval    = 5 * time.Second
	volumeAttachmentMissingLimit    = 20
)

type VolumeAttachmentResource struct {
	DeviceName                  string `ub:"device-name"`
	InstanceId                  string `ub:"instance-id"`
	VolumeId                    string `ub:"volume-id"`
	ForceDetach                 *bool  `ub:"force-detach"`
	SkipDestroy                 *bool  `ub:"skip-destroy"`
	StopInstanceBeforeDetaching *bool  `ub:"stop-instance-before-detaching"`
}

type VolumeAttachmentResourceOutput struct {
	Region string `ub:"region"`
}

type volumeAttachmentPrior = runtime.Prior[
	VolumeAttachmentResource, *VolumeAttachmentResourceOutput, *awsCfg,
]

func (r VolumeAttachmentResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.DeviceName, nil)).
			Message("device-name must be a non-null string"),
		constraint.Must(constraint.NotEquals(r.InstanceId, nil)).
			Message("instance-id must be a non-null string"),
		constraint.Must(constraint.NotEquals(r.VolumeId, nil)).
			Message("volume-id must be a non-null string"),
	}
}

func (*VolumeAttachmentResource) ResourceDefinition() runtime.ResourceDefinition[
	VolumeAttachmentResource, *VolumeAttachmentResourceOutput, *awsCfg,
] {
	return runtime.ResourceDefinition[
		VolumeAttachmentResource, *VolumeAttachmentResourceOutput, *awsCfg,
	]{
		SchemaVersion: 1,
		Replace: runtime.Replacement[
			VolumeAttachmentResource, *VolumeAttachmentResourceOutput, *awsCfg,
		]{
			Fields: []runtime.AnyInputField[VolumeAttachmentResource]{
				runtime.InputField(
					func(in *VolumeAttachmentResource) *string { return &in.DeviceName },
				),
				runtime.InputField(
					func(in *VolumeAttachmentResource) *string { return &in.InstanceId },
				),
				runtime.InputField(
					func(in *VolumeAttachmentResource) *string { return &in.VolumeId },
				),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
	}
}

type volumeAttachmentClient struct {
	ec2          *ec2.Client
	pollInterval time.Duration
}

func (r *VolumeAttachmentResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*VolumeAttachmentResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, volumeAttachmentClient{client, volumeAttachmentPollInterval})
}

func (r *VolumeAttachmentResource) create(
	ctx context.Context, client volumeAttachmentClient,
) (*VolumeAttachmentResourceOutput, error) {
	_, err := r.lookup(ctx, client.ec2)
	switch {
	case errors.Is(err, runtime.ErrNotFound):
		if err := r.waitForInstance(ctx, client, false); err != nil {
			return nil, err
		}
		_, err := client.ec2.AttachVolume(ctx, &ec2.AttachVolumeInput{
			Device: aws.String(r.DeviceName), InstanceId: aws.String(r.InstanceId),
			VolumeId: aws.String(r.VolumeId),
		})
		if err != nil {
			return nil, fmt.Errorf("attach volume %s: %w", r.VolumeId, err)
		}
	case err != nil:
		return nil, err
	}
	if err := r.waitForAttachment(ctx, client, true); err != nil {
		return nil, err
	}
	if _, err := r.lookup(ctx, client.ec2); err != nil {
		return nil, fmt.Errorf("confirm attached volume %s: %w", r.VolumeId, err)
	}
	return &VolumeAttachmentResourceOutput{Region: region(client.ec2)}, nil
}

func (*VolumeAttachmentResource) Read(
	ctx context.Context, cfg *awsCfg, prior volumeAttachmentPrior,
) (*VolumeAttachmentResourceOutput, error) {
	client, err := newClient(ctx, volumeAttachmentPriorConfig(cfg, prior))
	if err != nil {
		return nil, err
	}
	if _, err := prior.Inputs.lookup(ctx, client); err != nil {
		return nil, err
	}
	return &VolumeAttachmentResourceOutput{Region: region(client)}, nil
}

func (*VolumeAttachmentResource) Update(
	_ context.Context, _ *awsCfg, prior volumeAttachmentPrior,
) (*VolumeAttachmentResourceOutput, error) {
	return prior.Outputs, nil
}

func (*VolumeAttachmentResource) Delete(
	ctx context.Context, cfg *awsCfg, prior volumeAttachmentPrior,
) error {
	if aws.ToBool(prior.Inputs.SkipDestroy) {
		return nil
	}
	client, err := newClient(ctx, volumeAttachmentPriorConfig(cfg, prior))
	if err != nil {
		return err
	}
	return prior.Inputs.delete(ctx, volumeAttachmentClient{client, volumeAttachmentPollInterval})
}

func volumeAttachmentPriorConfig(cfg *awsCfg, prior volumeAttachmentPrior) *awsCfg {
	if prior.Configuration != nil {
		cfg = prior.Configuration
	}
	if prior.Outputs == nil || prior.Outputs.Region == "" {
		return cfg
	}
	saved := ptr.Value(cfg)
	saved.Region = aws.String(prior.Outputs.Region)
	return &saved
}

func (r *VolumeAttachmentResource) delete(
	ctx context.Context, client volumeAttachmentClient,
) error {
	if aws.ToBool(r.StopInstanceBeforeDetaching) {
		_, err := client.ec2.StopInstances(ctx, &ec2.StopInstancesInput{
			InstanceIds: []string{r.InstanceId}, Force: aws.Bool(false),
		})
		if err != nil {
			return fmt.Errorf("stop instance %s: %w", r.InstanceId, err)
		}
		if err := r.waitForInstance(ctx, client, true); err != nil {
			return err
		}
	}
	_, err := client.ec2.DetachVolume(ctx, &ec2.DetachVolumeInput{
		Device: aws.String(r.DeviceName), InstanceId: aws.String(r.InstanceId),
		VolumeId: aws.String(r.VolumeId), Force: aws.Bool(aws.ToBool(r.ForceDetach)),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "IncorrectState" &&
			strings.Contains(apiErr.ErrorMessage(), "available") {
			return nil
		}
		return fmt.Errorf("detach volume %s: %w", r.VolumeId, err)
	}
	return r.waitForAttachment(ctx, client, false)
}

func (r *VolumeAttachmentResource) lookup(
	ctx context.Context, client *ec2.Client,
) (ec2types.VolumeAttachmentState, error) {
	input := &ec2.DescribeVolumesInput{VolumeIds: []string{r.VolumeId}}
	for _, filter := range []struct{ name, value string }{
		{"attachment.device", r.DeviceName},
		{"attachment.instance-id", r.InstanceId},
	} {
		if filter.value != "" {
			input.Filters = append(input.Filters, ec2types.Filter{
				Name: aws.String(filter.name), Values: []string{filter.value},
			})
		}
	}
	pages := ec2.NewDescribeVolumesPaginator(client, input)
	var volume *ec2types.Volume
	var count int
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if isNotFound(err, "InvalidVolume.NotFound") {
			return "", runtime.ErrNotFound
		}
		if err != nil {
			return "", fmt.Errorf("describe volume %s: %w", r.VolumeId, err)
		}
		if count == 0 && len(page.Volumes) == 1 {
			volume = &page.Volumes[0]
		}
		count += len(page.Volumes)
	}
	if count != 1 || aws.ToString(volume.VolumeId) != r.VolumeId ||
		volume.State == ec2types.VolumeStateAvailable || volume.State == ec2types.VolumeStateDeleted {
		return "", runtime.ErrNotFound
	}
	for _, attachment := range volume.Attachments {
		if aws.ToString(attachment.Device) == r.DeviceName &&
			aws.ToString(attachment.InstanceId) == r.InstanceId &&
			attachment.State != ec2types.VolumeAttachmentStateDetached {
			return attachment.State, nil
		}
	}
	return "", runtime.ErrNotFound
}

func (r *VolumeAttachmentResource) instanceState(
	ctx context.Context, client *ec2.Client,
) (ec2types.InstanceStateName, string, error) {
	input := &ec2.DescribeInstancesInput{InstanceIds: []string{r.InstanceId}}
	pages := ec2.NewDescribeInstancesPaginator(client, input)
	var instance *ec2types.Instance
	var count int
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if isNotFound(err, "InvalidInstanceID.NotFound") {
			return "", "", runtime.ErrNotFound
		}
		if err != nil {
			return "", "", fmt.Errorf("describe instance %s: %w", r.InstanceId, err)
		}
		for _, reservation := range page.Reservations {
			if count == 0 && len(reservation.Instances) == 1 {
				instance = &reservation.Instances[0]
			}
			count += len(reservation.Instances)
		}
	}
	if count != 1 || instance.State == nil {
		return "", "", runtime.ErrNotFound
	}
	var reason string
	if instance.StateReason != nil {
		reason = aws.ToString(instance.StateReason.Message)
		if reason == "" {
			reason = aws.ToString(instance.StateReason.Code)
		}
	}
	return instance.State.Name, reason, nil
}

func (r *VolumeAttachmentResource) waitForInstance(
	ctx context.Context, client volumeAttachmentClient, stopping bool,
) error {
	pending := []ec2types.InstanceStateName{
		ec2types.InstanceStateNamePending, ec2types.InstanceStateNameStopping,
	}
	targets := []ec2types.InstanceStateName{ec2types.InstanceStateNameStopped}
	description := "instance " + r.InstanceId + " to stop"
	if stopping {
		pending = append(pending,
			ec2types.InstanceStateNameRunning, ec2types.InstanceStateNameShuttingDown)
	} else {
		targets = append(targets, ec2types.InstanceStateNameRunning)
		description = "instance " + r.InstanceId + " to become ready"
	}
	return waitForVolumeAttachmentState(
		ctx, description, volumeAttachmentInstanceTimeout, client.pollInterval,
		func(ctx context.Context) (ec2types.InstanceStateName, string, error) {
			return r.instanceState(ctx, client.ec2)
		}, pending, targets,
	)
}

func (r *VolumeAttachmentResource) waitForAttachment(
	ctx context.Context, client volumeAttachmentClient, attaching bool,
) error {
	description := "volume " + r.VolumeId + " to detach"
	pending := []ec2types.VolumeAttachmentState{ec2types.VolumeAttachmentStateDetaching}
	var targets []ec2types.VolumeAttachmentState
	if attaching {
		description = "volume " + r.VolumeId + " to attach"
		pending = []ec2types.VolumeAttachmentState{ec2types.VolumeAttachmentStateAttaching}
		targets = []ec2types.VolumeAttachmentState{ec2types.VolumeAttachmentStateAttached}
	}
	return waitForVolumeAttachmentState(
		ctx, description, volumeAttachmentTimeout, client.pollInterval,
		func(ctx context.Context) (ec2types.VolumeAttachmentState, string, error) {
			state, err := r.lookup(ctx, client.ec2)
			return state, "", err
		}, pending, targets,
	)
}

// With no target state, the first missing observation completes the wait.
func waitForVolumeAttachmentState[S ~string](
	ctx context.Context, description string, timeout, interval time.Duration,
	observe func(context.Context) (S, string, error), pending, targets []S,
) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	missing := 0
	err := wait.Until(ctx, description, func(ctx context.Context) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		state, reason, err := observe(ctx)
		switch {
		case errors.Is(err, runtime.ErrNotFound):
			if len(targets) == 0 {
				return true, nil
			}
			missing++
			if missing > volumeAttachmentMissingLimit {
				return false, fmt.Errorf("missing for %d consecutive observations", missing)
			}
			return false, nil
		case err != nil:
			return false, err
		}
		missing = 0
		switch {
		case slices.Contains(targets, state):
			return true, nil
		case slices.Contains(pending, state):
			return false, nil
		case reason != "":
			return false, fmt.Errorf("unexpected state %q: %s", state, reason)
		default:
			return false, fmt.Errorf("unexpected state %q", state)
		}
	}, wait.WithTimeout(timeout), wait.WithInterval(interval))
	if err != nil {
		return fmt.Errorf("wait for %s: %w", description, err)
	}
	return nil
}
