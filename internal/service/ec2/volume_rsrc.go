package ec2

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// Volume is an EBS volume: a block storage device created in one Availability
// Zone from a size, a snapshot, or both. The zone, encryption, KMS key,
// Multi-Attach flag, Outpost, source snapshot, and initialization rate are
// fixed when the volume is created, so a change to any of them replaces the
// volume; the size, IOPS, throughput, and volume type are reconciled in place
// by ModifyVolume. CreateVolume accepts every create-time field in one call;
// the only follow-on work is the settling wait and, at delete time, an optional
// final snapshot. A nil optional field is never sent: the server applies its own
// default and fills the computed outputs.
type Volume struct {
	AvailabilityZone         string            `ub:"availability-zone"`
	Encrypted                *bool             `ub:"encrypted"`
	Iops                     *int64            `ub:"iops"`
	KmsKeyId                 *string           `ub:"kms-key-id"`
	MultiAttachEnabled       *bool             `ub:"multi-attach-enabled"`
	OutpostArn               *string           `ub:"outpost-arn"`
	Size                     *int64            `ub:"size"`
	SnapshotId               *string           `ub:"snapshot-id"`
	Throughput               *int64            `ub:"throughput"`
	Type                     *string           `ub:"type"`
	VolumeInitializationRate *int64            `ub:"volume-initialization-rate"`
	Tags                     map[string]string `ub:"tags"`
	// FinalSnapshot is read only at delete time. When true, Delete first takes a
	// snapshot of the volume and waits for it to complete before removing the
	// volume. It backs no CreateVolume field and is never reconciled after create.
	FinalSnapshot *bool `ub:"final-snapshot"`
}

// VolumeOutput holds the values EC2 computes for a volume. The id is the
// volume's handle. The create time comes only from a describe, not the
// create response. The size, type, IOPS, throughput, encryption flag, and the
// server-resolved KMS key ARN are filled by EC2 when the input omits them, so
// the settled values come from a describe and differ from an empty input.
type VolumeOutput struct {
	VolumeId   string `ub:"volume-id"`
	CreateTime string `ub:"create-time"`
	Size       int64  `ub:"size"`
	Type       string `ub:"type"`
	Iops       int64  `ub:"iops"`
	Throughput int64  `ub:"throughput"`
	Encrypted  bool   `ub:"encrypted"`
	KmsKeyId   string `ub:"kms-key-id"`
}

func (r *Volume) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 fixes when a volume is created. The zone,
// the encryption flag, the KMS key, the Multi-Attach flag, the Outpost ARN, and
// the source snapshot cannot change on an existing volume, so a change to any of
// them requires a new volume. The volume-initialization-rate is not listed even
// though it only rides CreateVolume: it governs the initial hydration from the
// snapshot and leaves no state on the live volume, so changing it later is not
// applied rather than replacing a volume over a setting with no remaining
// effect.
func (r *Volume) ReplaceFields() []string {
	return []string{
		"availability-zone",
		"encrypted",
		"kms-key-id",
		"multi-attach-enabled",
		"outpost-arn",
		"snapshot-id",
	}
}

// Defaults marks the tag map a volume may omit.
func (r Volume) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the cross-field rules EC2 enforces on a volume's inputs.
// A volume needs a size, a source snapshot, or both. The IOPS field follows the
// volume type: io1 and io2 require it, gp3 allows it, and every other type
// forbids it. Throughput applies only to gp3. Multi-Attach applies only to io1
// and io2. The initialization rate applies only when creating from a snapshot.
// The type is one of the documented EBS volume types, and the numeric bounds on
// throughput and the initialization rate come from the API.
//
// The kms-key-id and outpost-arn fields are ARNs (or, for kms-key-id, an id or
// alias the server resolves); their well-formedness is left to EC2, which
// rejects a malformed value, since a regex check is not expressible here.
func (r Volume) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtLeastOneOf(r.Size, r.SnapshotId),
		constraint.When(constraint.Present(r.Type)).
			Require(constraint.OneOf(r.Type,
				"standard", "gp2", "gp3", "io1", "io2", "sc1", "st1")).
			Message("type must be standard, gp2, gp3, io1, io2, sc1, or st1"),
		constraint.When(constraint.Equals(r.Type, "io1")).
			Require(constraint.Present(r.Iops)).
			Message("iops is required when type is io1"),
		constraint.When(constraint.Equals(r.Type, "io2")).
			Require(constraint.Present(r.Iops)).
			Message("iops is required when type is io2"),
		constraint.When(constraint.Present(r.Iops)).
			Require(constraint.OneOf(r.Type, "gp3", "io1", "io2")).
			Message("iops is valid only for gp3, io1, or io2 volume types"),
		constraint.When(constraint.Present(r.Throughput)).
			Require(constraint.Equals(r.Type, "gp3"),
				constraint.AtLeast(r.Throughput, 125),
				constraint.AtMost(r.Throughput, 2000)).
			Message("throughput is valid only for gp3 volumes and must be 125 to 2000"),
		constraint.When(constraint.IsTrue(r.MultiAttachEnabled)).
			Require(constraint.OneOf(r.Type, "io1", "io2")).
			Message("multi-attach-enabled is valid only for io1 or io2 volume types"),
		constraint.When(constraint.Present(r.VolumeInitializationRate)).
			Require(constraint.Present(r.SnapshotId),
				constraint.AtLeast(r.VolumeInitializationRate, 100),
				constraint.AtMost(r.VolumeInitializationRate, 300)).
			Message("volume-initialization-rate requires snapshot-id and must be 100 to 300"),
	}
}

func (r *Volume) Create(ctx context.Context, cfg any) (*VolumeOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &ec2.CreateVolumeInput{
		AvailabilityZone:         aws.String(r.AvailabilityZone),
		Encrypted:                r.Encrypted,
		Iops:                     ptr.Int32(r.Iops),
		KmsKeyId:                 r.KmsKeyId,
		MultiAttachEnabled:       r.MultiAttachEnabled,
		OutpostArn:               r.OutpostArn,
		Size:                     ptr.Int32(r.Size),
		SnapshotId:               r.SnapshotId,
		Throughput:               ptr.Int32(r.Throughput),
		VolumeInitializationRate: ptr.Int32(r.VolumeInitializationRate),
		VolumeType:               ec2types.VolumeType(aws.ToString(r.Type)),
		TagSpecifications:        tagSpecifications(ec2types.ResourceTypeVolume, r.Tags),
	}
	resp, err := client.CreateVolume(ctx, in)
	// Some partitions, such as the ISO partitions, cannot tag a volume as it is
	// created. When the tagged create fails for that reason, create the volume
	// without tags and apply them with a separate call below.
	taggedSeparately := false
	if err != nil && in.TagSpecifications != nil &&
		partition.UnsupportedOperation(region(client), err) {
		in.TagSpecifications = nil
		taggedSeparately = true
		resp, err = client.CreateVolume(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create volume: %w", err)
	}
	id := aws.ToString(resp.VolumeId)
	if taggedSeparately && len(r.Tags) > 0 {
		if err := syncTags(ctx, client, id, r.Tags); err != nil {
			return nil, err
		}
	}
	// CreateVolume returns while the volume is still creating. Wait for it to
	// settle to available before reading, so the outputs come from the settled
	// record rather than the unsettled create response.
	if err := r.waitAvailable(ctx, client, id); err != nil {
		return nil, err
	}
	return r.read(ctx, client, id)
}

func (r *Volume) Read(ctx context.Context, cfg any, prior *VolumeOutput) (*VolumeOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.VolumeId)
}

func (r *Volume) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Volume, *VolumeOutput],
) (*VolumeOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.VolumeId
	// ModifyVolume reconciles size, IOPS, throughput, and volume type in one
	// call; issue it only when one of those fields actually changed, so a re-apply
	// with no change makes no write.
	if r.modifyChanged(prior) {
		if err := r.modify(ctx, client, prior.Outputs); err != nil {
			return nil, err
		}
	}
	// ModifyVolume does not touch tags, so reconcile them as a set whenever they
	// changed, the same as the other EC2 resources.
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, id, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, id)
}

func (r *Volume) Delete(ctx context.Context, cfg any, prior *VolumeOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.VolumeId
	// A final snapshot, when asked for, is taken and confirmed complete before the
	// volume is removed, so the volume's data survives the delete.
	if aws.ToBool(r.FinalSnapshot) {
		if err := r.createFinalSnapshot(ctx, client, id); err != nil {
			return err
		}
	}
	// A just-detached volume can briefly still read as in use, which DeleteVolume
	// reports as VolumeInUse; retry through that window. A volume that is already
	// gone is a successful delete with nothing to do.
	err = retry.OnError(ctx, isVolumeInUse, func(ctx context.Context) error {
		_, err := client.DeleteVolume(ctx, &ec2.DeleteVolumeInput{
			VolumeId: aws.String(id),
		})
		return err
	}, retry.WithTimeout(10*time.Minute))
	if err != nil {
		if isNotFound(err, "InvalidVolume.NotFound") {
			return nil
		}
		return fmt.Errorf("delete volume: %w", err)
	}
	return r.waitDeleted(ctx, client, id)
}

// read fetches the volume by id and returns its computed outputs. A volume's
// describe record has no ARN or owner, so the output exposes the volume id
// as the handle.
func (r *Volume) read(
	ctx context.Context, client *ec2.Client, id string,
) (*VolumeOutput, error) {
	volume, err := describeVolume(ctx, client, id)
	if err != nil {
		return nil, err
	}
	out := &VolumeOutput{
		VolumeId:   aws.ToString(volume.VolumeId),
		Size:       int64(aws.ToInt32(volume.Size)),
		Type:       string(volume.VolumeType),
		Iops:       int64(aws.ToInt32(volume.Iops)),
		Throughput: int64(aws.ToInt32(volume.Throughput)),
		Encrypted:  aws.ToBool(volume.Encrypted),
		KmsKeyId:   aws.ToString(volume.KmsKeyId),
	}
	if volume.CreateTime != nil {
		out.CreateTime = volume.CreateTime.UTC().Format(time.RFC3339)
	}
	return out, nil
}

// modifyChanged reports whether any field ModifyVolume reconciles -- size, IOPS,
// throughput, or volume type -- differs from the prior inputs, so Update makes
// the call only when there is real work.
func (r *Volume) modifyChanged(prior runtime.Prior[Volume, *VolumeOutput]) bool {
	return runtime.Changed(prior.Inputs.Size, r.Size) ||
		runtime.Changed(prior.Inputs.Iops, r.Iops) ||
		runtime.Changed(prior.Inputs.Throughput, r.Throughput) ||
		runtime.Changed(prior.Inputs.Type, r.Type)
}

// modify issues the single ModifyVolume call that reconciles the in-place
// fields, then waits for the volume to settle. Size and IOPS are sent when set.
// Throughput is sent only for a gp3 volume, the one type that takes it. The
// volume type is sent when it changed; switching the type into io1, io2, or gp3
// makes ModifyVolume default IOPS to 3000 unless an IOPS value is supplied, so a
// type switch into one of those re-sends the current IOPS to hold it.
func (r *Volume) modify(ctx context.Context, client *ec2.Client, prior *VolumeOutput) error {
	in := &ec2.ModifyVolumeInput{VolumeId: aws.String(prior.VolumeId)}
	if r.Size != nil {
		in.Size = ptr.Int32(r.Size)
	}
	if r.Iops != nil {
		in.Iops = ptr.Int32(r.Iops)
	}
	if aws.ToInt64(r.Throughput) > 0 && aws.ToString(r.Type) == "gp3" {
		in.Throughput = ptr.Int32(r.Throughput)
	}
	if r.Type != nil && string(prior.Type) != *r.Type {
		newType := *r.Type
		in.VolumeType = ec2types.VolumeType(newType)
		if in.Iops == nil && volumeTypeTakesIops(newType) {
			in.Iops = aws.Int32(int32(prior.Iops))
		}
	}
	if _, err := client.ModifyVolume(ctx, in); err != nil {
		return fmt.Errorf("modify volume: %w", err)
	}
	return r.waitModified(ctx, client, prior.VolumeId)
}

// createFinalSnapshot takes a snapshot of the volume, tagged from the volume's
// own tags, and waits for it to complete before the volume is removed.
func (r *Volume) createFinalSnapshot(ctx context.Context, client *ec2.Client, id string) error {
	in := &ec2.CreateSnapshotInput{
		VolumeId:          aws.String(id),
		TagSpecifications: tagSpecifications(ec2types.ResourceTypeSnapshot, r.Tags),
	}
	var snapshotID string
	// EC2 caps how often a single volume can be snapshotted; a burst of requests
	// briefly returns a per-volume rate error that clears on its own. Retry the
	// create through that short window.
	err := retry.OnError(ctx, isSnapshotRateExceeded, func(ctx context.Context) error {
		resp, err := client.CreateSnapshot(ctx, in)
		if err != nil {
			return err
		}
		snapshotID = aws.ToString(resp.SnapshotId)
		return nil
	}, retry.WithTimeout(time.Minute), retry.WithInterval(time.Second))
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	return r.waitSnapshotCompleted(ctx, client, snapshotID)
}

// waitAvailable polls the volume until it reports state available, the point at
// which a create has settled. A volume that enters the error state stops the
// wait, since it will not become available.
func (r *Volume) waitAvailable(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("volume %s to become available", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		volume, err := describeVolume(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		switch volume.State {
		case ec2types.VolumeStateAvailable:
			return true, nil
		case ec2types.VolumeStateError:
			return false, fmt.Errorf("volume %s entered state error", id)
		default:
			return false, nil
		}
	}, wait.WithTimeout(5*time.Minute))
}

// waitModified polls the volume after a ModifyVolume until it leaves the
// creating and modifying states and reads as available or in-use. The volume is
// usable at that point even though EC2 continues optimizing it in the
// background, which this wait does not block on. A volume in the error state
// stops the wait.
func (r *Volume) waitModified(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("volume %s modification", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		volume, err := describeVolume(ctx, client, id)
		if err != nil {
			return false, err
		}
		switch volume.State {
		case ec2types.VolumeStateAvailable, ec2types.VolumeStateInUse:
			return true, nil
		case ec2types.VolumeStateError:
			return false, fmt.Errorf("volume %s entered state error", id)
		default:
			return false, nil
		}
	}, wait.WithTimeout(5*time.Minute))
}

// waitDeleted polls the volume until a describe no longer finds it, confirming
// the delete has propagated. A just-deleted volume settles in about a second, so
// the poll runs at a one-second interval rather than the slower create pace.
func (r *Volume) waitDeleted(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("volume %s deletion", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		_, err := describeVolume(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, wait.WithTimeout(10*time.Minute), wait.WithInterval(time.Second))
}

// waitSnapshotCompleted polls the snapshot until it reports state completed. The
// whole poll is retried while DescribeSnapshots cannot yet find the snapshot,
// since a just-created snapshot is briefly not describable; that window maps to
// runtime.ErrNotFound, which the retry rides out. A snapshot in the error state
// stops the wait.
func (r *Volume) waitSnapshotCompleted(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("snapshot %s to complete", id)
	run := func(ctx context.Context) error {
		return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
			snapshot, err := describeSnapshot(ctx, client, id)
			if err != nil {
				return false, err
			}
			switch snapshot.State {
			case ec2types.SnapshotStateCompleted:
				return true, nil
			case ec2types.SnapshotStateError:
				return false, fmt.Errorf("snapshot %s entered state error: %s",
					id, aws.ToString(snapshot.StateMessage))
			default:
				return false, nil
			}
		}, wait.WithTimeout(15*time.Minute))
	}
	if err := retry.OnError(ctx, isSnapshotNotReady, run,
		retry.WithTimeout(15*time.Minute), retry.WithInterval(time.Second)); err != nil {
		return err
	}
	return nil
}

// volumeTypeTakesIops reports whether a volume type provisions IOPS, which gp3,
// io1, and io2 do; for those, a ModifyVolume that switches into the type must
// re-send IOPS or EC2 resets it to its 3000 default.
func volumeTypeTakesIops(volumeType string) bool {
	return volumeType == "gp3" || volumeType == "io1" || volumeType == "io2"
}

// describeVolume fetches the volume with the given id. EC2 reports a missing
// volume by service code on an HTTP 400, never a 404, so the not-found code maps
// to runtime.ErrNotFound. A record that reads as deleted, or a returned id that
// does not match the requested one, means the same: the volume the caller asked
// for is gone.
func describeVolume(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.Volume, error) {
	resp, err := client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{
		VolumeIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, "InvalidVolume.NotFound") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe volumes: %w", err)
	}
	if len(resp.Volumes) == 0 {
		return nil, runtime.ErrNotFound
	}
	volume := resp.Volumes[0]
	if volume.State == ec2types.VolumeStateDeleted {
		return nil, runtime.ErrNotFound
	}
	if aws.ToString(volume.VolumeId) != id {
		return nil, runtime.ErrNotFound
	}
	return &volume, nil
}

// describeSnapshot fetches the snapshot with the given id, mapping a not-found
// to runtime.ErrNotFound so the completion wait can ride out the brief window
// where a just-created snapshot is not yet describable.
func describeSnapshot(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.Snapshot, error) {
	resp, err := client.DescribeSnapshots(ctx, &ec2.DescribeSnapshotsInput{
		SnapshotIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, "InvalidSnapshot.NotFound") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe snapshots: %w", err)
	}
	if len(resp.Snapshots) == 0 {
		return nil, runtime.ErrNotFound
	}
	return &resp.Snapshots[0], nil
}

// isVolumeInUse reports whether a DeleteVolume error is the in-use conflict a
// just-detached volume briefly returns. EC2 raises it by service code on an HTTP
// 400, so it is matched the same way as a not-found.
func isVolumeInUse(err error) bool {
	return isNotFound(err, "VolumeInUse")
}

// isSnapshotRateExceeded reports whether a CreateSnapshot error is the
// per-volume snapshot-rate limit, which clears on its own. EC2 raises it by
// service code, and some responses only name it in the message, so both forms
// are matched.
func isSnapshotRateExceeded(err error) bool {
	if isNotFound(err, "SnapshotCreationPerVolumeRateExceeded") {
		return true
	}
	return err != nil && strings.Contains(err.Error(),
		"The maximum per volume CreateSnapshot request rate has been exceeded")
}

// isSnapshotNotReady reports whether a snapshot-completion wait failed only
// because the snapshot was not yet describable, the not-found the wait returns
// right after CreateSnapshot, so the wait can be retried.
func isSnapshotNotReady(err error) bool {
	return err == runtime.ErrNotFound
}
