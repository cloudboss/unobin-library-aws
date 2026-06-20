package ec2

import (
	"context"
	"encoding/base64"
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

// Instance is an EC2 instance: a virtual machine launched from an AMI or a
// launch template into a subnet. One RunInstances call provisions it with every
// field that call accepts; the only create-time field RunInstances does not take
// is source-dest-check, which a follow-on ModifyInstanceAttribute disables when
// the input asks for it. The instance settles from pending to running before its
// computed addresses, DNS names, and root volume id exist, so Create waits for
// that and returns the settled values from a Read rather than the run response.
//
// The image, key pair, subnet, Availability Zone, primary private address,
// public-address association, tenancy, EBS-optimization flag, launch template,
// and the additional and instance-store volumes are fixed when the instance is
// created, so a change to any of them replaces the instance. The instance type
// and user data are reconciled by stopping the instance, modifying the one
// attribute, and starting it again. The security group set, the IAM instance
// profile, source-dest-check, monitoring, the two API-protection flags, the
// shutdown behavior, the metadata options, the volume tags, and the root volume's
// size, type, IOPS, throughput, delete-on-termination flag, and tags are all
// reconciled in place, each by its own call. A nil optional field is never sent:
// AWS applies its own default and fills the computed outputs.
//
// A terminated instance still describes for a while, so Read maps a terminated
// state to a gone resource, the same as a not-found error code; a shutting-down
// instance is still live.
type Instance struct {
	Ami                               *string                        `ub:"ami"`
	InstanceType                      *string                        `ub:"instance-type"`
	SubnetId                          *string                        `ub:"subnet-id"`
	AvailabilityZone                  *string                        `ub:"availability-zone"`
	KeyName                           *string                        `ub:"key-name"`
	VpcSecurityGroupIds               []string                       `ub:"vpc-security-group-ids"`
	IamInstanceProfile                *string                        `ub:"iam-instance-profile"`
	UserData                          *string                        `ub:"user-data"`
	UserDataBase64                    *string                        `ub:"user-data-base64"`
	PrivateIp                         *string                        `ub:"private-ip"`
	AssociatePublicIpAddress          *bool                          `ub:"associate-public-ip-address"`
	Monitoring                        *bool                          `ub:"monitoring"`
	EbsOptimized                      *bool                          `ub:"ebs-optimized"`
	DisableApiTermination             *bool                          `ub:"disable-api-termination"`
	DisableApiStop                    *bool                          `ub:"disable-api-stop"`
	InstanceInitiatedShutdownBehavior *string                        `ub:"instance-initiated-shutdown-behavior"`
	SourceDestCheck                   *bool                          `ub:"source-dest-check"`
	Tenancy                           *string                        `ub:"tenancy"`
	MetadataOptions                   *InstanceMetadataOptions       `ub:"metadata-options"`
	RootBlockDevice                   *InstanceRootBlockDevice       `ub:"root-block-device"`
	EbsBlockDevice                    []InstanceEbsBlockDevice       `ub:"ebs-block-device"`
	EphemeralBlockDevice              []InstanceEphemeralBlockDevice `ub:"ephemeral-block-device"`
	LaunchTemplate                    *InstanceLaunchTemplate        `ub:"launch-template"`
	// VolumeTags are applied to every EBS volume the instance creates, at create
	// time and reconciled per volume on Update. Per-block-device tags are a future
	// addition; this one flat map tags all of the instance's volumes alike.
	VolumeTags map[string]string `ub:"volume-tags"`
	Tags       map[string]string `ub:"tags"`
	// ForceDestroy is read only at delete time. When true, Delete first clears the
	// stop- and termination-protection attributes so a protected instance can be
	// terminated. It backs no RunInstances field and is never reconciled after
	// create.
	ForceDestroy *bool `ub:"force-destroy"`
}

// InstanceOutput holds the values EC2 computes for an instance. The id is the
// instance's handle. The state, the resolved Availability Zone and subnet, the
// private and public addresses and DNS names, and the primary network interface
// id come from the settled instance after the create wait. The Availability Zone
// and subnet are computed when the input omitted them, as when a launch template
// provides them, so they are reported here even though they share input names.
// The root volume id and root device name come from the instance's block device
// mappings, the device name being the AMI-assigned root that is not an input.
type InstanceOutput struct {
	InstanceId                string `ub:"instance-id"`
	InstanceState             string `ub:"instance-state"`
	AvailabilityZone          string `ub:"availability-zone"`
	SubnetId                  string `ub:"subnet-id"`
	PrivateIp                 string `ub:"private-ip"`
	PublicIp                  string `ub:"public-ip"`
	PrivateDns                string `ub:"private-dns"`
	PublicDns                 string `ub:"public-dns"`
	PrimaryNetworkInterfaceId string `ub:"primary-network-interface-id"`
	RootVolumeId              string `ub:"root-volume-id"`
	RootDeviceName            string `ub:"root-device-name"`
}

func (r *Instance) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 fixes when an instance is created. The
// image, the public-address association, the Availability Zone, the
// EBS-optimization flag, the key pair, the launch template, the primary private
// address, the subnet, and the tenancy cannot change on an existing instance, so
// a change to any of them requires a new instance. The additional and
// instance-store volume lists are whole-list replace, since every field in them
// is fixed at launch. The security group set, IAM profile, source-dest-check,
// monitoring, protection flags, shutdown behavior, metadata options, root volume,
// and tags are reconciled in place and are not listed.
func (r *Instance) ReplaceFields() []string {
	return []string{
		"ami",
		"associate-public-ip-address",
		"availability-zone",
		"ebs-block-device",
		"ephemeral-block-device",
		"ebs-optimized",
		"key-name",
		"launch-template",
		"private-ip",
		"subnet-id",
		"tenancy",
	}
}

// Defaults marks the collection and map inputs an instance may omit. The pointer
// blocks -- root-block-device, metadata-options, launch-template -- are omittable
// through the pointer itself and are not marked here.
func (r Instance) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.VpcSecurityGroupIds),
		defaults.Optional(r.EbsBlockDevice),
		defaults.Optional(r.EphemeralBlockDevice),
		defaults.Optional(r.VolumeTags),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the cross-field rules EC2 enforces on an instance's
// inputs. An image or a launch template supplies the AMI, and an instance type
// or a launch template supplies the type, so each pair needs at least one. User
// data is given as plain text or as base64, not both. The tenancy is one of the
// documented values. The rules on nested block fields reach them by selector --
// the launch-template id-or-name choice, the metadata-options enums and hop
// limit, and the root volume's type family -- and the per-element rules on the
// additional EBS and instance-store volume lists derive through ForEach.
func (r Instance) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtLeastOneOf(r.Ami, r.LaunchTemplate),
		constraint.AtLeastOneOf(r.InstanceType, r.LaunchTemplate),
		constraint.AtMostOneOf(r.UserData, r.UserDataBase64),
		constraint.When(constraint.Present(r.Tenancy)).
			Require(constraint.OneOf(r.Tenancy, "default", "dedicated", "host")).
			Message("tenancy must be default, dedicated, or host"),
		constraint.When(constraint.Present(r.LaunchTemplate)).
			Require(constraint.Any(
				constraint.All(constraint.Present(r.LaunchTemplate.Id),
					constraint.Absent(r.LaunchTemplate.Name)),
				constraint.All(constraint.Absent(r.LaunchTemplate.Id),
					constraint.Present(r.LaunchTemplate.Name)))).
			Message("launch-template requires exactly one of id and name"),
		constraint.When(constraint.Present(r.MetadataOptions.HttpEndpoint)).
			Require(constraint.OneOf(r.MetadataOptions.HttpEndpoint,
				"enabled", "disabled")).
			Message("metadata-options http-endpoint must be enabled or disabled"),
		constraint.When(constraint.Present(r.MetadataOptions.HttpTokens)).
			Require(constraint.OneOf(r.MetadataOptions.HttpTokens,
				"optional", "required")).
			Message("metadata-options http-tokens must be optional or required"),
		constraint.When(constraint.Present(r.MetadataOptions.HttpProtocolIpv6)).
			Require(constraint.OneOf(r.MetadataOptions.HttpProtocolIpv6,
				"enabled", "disabled")).
			Message("metadata-options http-protocol-ipv6 must be enabled or disabled"),
		constraint.When(constraint.Present(r.MetadataOptions.InstanceMetadataTags)).
			Require(constraint.OneOf(r.MetadataOptions.InstanceMetadataTags,
				"enabled", "disabled")).
			Message("metadata-options instance-metadata-tags must be enabled or disabled"),
		constraint.When(constraint.Present(r.MetadataOptions.HttpPutResponseHopLimit)).
			Require(constraint.AtLeast(r.MetadataOptions.HttpPutResponseHopLimit, 1),
				constraint.AtMost(r.MetadataOptions.HttpPutResponseHopLimit, 64)).
			Message("metadata-options http-put-response-hop-limit must be 1 to 64"),
		constraint.When(constraint.All(constraint.Present(r.RootBlockDevice.Iops),
			constraint.Present(r.RootBlockDevice.VolumeType))).
			Require(constraint.OneOf(r.RootBlockDevice.VolumeType, "gp3", "io1", "io2")).
			Message("root-block-device iops is valid only for gp3, io1, or io2 volume types"),
		constraint.When(constraint.OneOf(r.RootBlockDevice.VolumeType, "io1", "io2")).
			Require(constraint.Present(r.RootBlockDevice.Iops)).
			Message("root-block-device iops is required when volume-type is io1 or io2"),
		constraint.When(constraint.All(constraint.Present(r.RootBlockDevice.Throughput),
			constraint.Present(r.RootBlockDevice.VolumeType))).
			Require(constraint.Equals(r.RootBlockDevice.VolumeType, "gp3")).
			Message("root-block-device throughput is valid only for gp3 volumes"),
		constraint.When(constraint.Present(r.RootBlockDevice.Tags)).
			Require(constraint.Absent(r.VolumeTags)).
			Message("root-block-device tags cannot combine with volume-tags"),
		constraint.ForEach(r.EbsBlockDevice,
			func(b InstanceEbsBlockDevice) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.All(
						constraint.Present(b.Iops), constraint.Present(b.VolumeType))).
						Require(constraint.OneOf(b.VolumeType, "gp3", "io1", "io2")).
						Message("iops is valid only for gp3, io1, or io2 volume types"),
					constraint.When(constraint.OneOf(b.VolumeType, "io1", "io2")).
						Require(constraint.Present(b.Iops)).
						Message("iops is required when volume-type is io1 or io2"),
					constraint.When(constraint.All(
						constraint.Present(b.Throughput), constraint.Present(b.VolumeType))).
						Require(constraint.Equals(b.VolumeType, "gp3")).
						Message("throughput is valid only for gp3 volumes"),
				}
			}),
		constraint.ForEach(r.EphemeralBlockDevice,
			func(b InstanceEphemeralBlockDevice) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Not(constraint.IsTrue(b.NoDevice))).
						Require(constraint.NotEmpty(b.VirtualName)).
						Message("virtual-name is required unless no-device is true"),
				}
			}),
	}
}

func (r *Instance) Create(ctx context.Context, cfg *awsCfg) (*InstanceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in, err := r.runInput(ctx, client)
	if err != nil {
		return nil, err
	}
	id, err := r.run(ctx, client, in)
	if err != nil {
		return nil, err
	}
	if err := r.waitRunning(ctx, client, id); err != nil {
		return nil, err
	}
	if err := r.applyCreateFollowOns(ctx, client, id); err != nil {
		return nil, err
	}
	return r.read(ctx, client, id)
}

func (r *Instance) Read(
	ctx context.Context, cfg *awsCfg, prior *InstanceOutput,
) (*InstanceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.InstanceId)
}

func (r *Instance) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Instance, *InstanceOutput],
) (*InstanceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.InstanceId
	if err := r.updateInPlace(ctx, client, id, prior); err != nil {
		return nil, err
	}
	return r.read(ctx, client, id)
}

func (r *Instance) Delete(ctx context.Context, cfg *awsCfg, prior *InstanceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.InstanceId
	// When force-destroy is set, clear the two protection attributes so a protected
	// instance can be terminated. Either clear can fail because the protection was
	// never set or is not supported, which is not fatal to the delete, so the
	// outcome is logged through the error context and the terminate proceeds.
	if aws.ToBool(r.ForceDestroy) {
		_ = r.setProtection(ctx, client, id, ec2types.InstanceAttributeNameDisableApiStop, false)
		_ = r.setProtection(
			ctx, client, id, ec2types.InstanceAttributeNameDisableApiTermination, false)
	}
	_, err = client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{id},
	})
	if err != nil {
		// An instance that is already gone is a successful delete with nothing to do.
		if isNotFound(err, instanceNotFoundCode) {
			return nil
		}
		return fmt.Errorf("terminate instances: %w", err)
	}
	return r.waitTerminated(ctx, client, id)
}

// runInput assembles the RunInstances request from the create-time fields. The
// root volume settings target the AMI's root device name, which is resolved from
// the image, so a root-block-device requires an ami; the launch-template-only
// path leaves the root volume at the template's mapping. When a public address is
// requested, the subnet, security groups, and primary private address move into
// the primary network interface specification, since RunInstances rejects the
// top-level forms alongside a network interface.
func (r *Instance) runInput(
	ctx context.Context, client *ec2.Client,
) (*ec2.RunInstancesInput, error) {
	userData, err := r.encodedUserData()
	if err != nil {
		return nil, err
	}
	in := &ec2.RunInstancesInput{
		MinCount:              aws.Int32(1),
		MaxCount:              aws.Int32(1),
		ImageId:               r.Ami,
		KeyName:               r.KeyName,
		UserData:              userData,
		EbsOptimized:          r.EbsOptimized,
		DisableApiStop:        r.DisableApiStop,
		DisableApiTermination: r.DisableApiTermination,
		MetadataOptions:       metadataOptionsRequest(r.MetadataOptions),
		LaunchTemplate:        launchTemplateSpecification(r.LaunchTemplate),
		TagSpecifications:     r.tagSpecifications(),
	}
	if r.InstanceType != nil {
		in.InstanceType = ec2types.InstanceType(*r.InstanceType)
	}
	if r.IamInstanceProfile != nil {
		in.IamInstanceProfile = &ec2types.IamInstanceProfileSpecification{Name: r.IamInstanceProfile}
	}
	if r.InstanceInitiatedShutdownBehavior != nil {
		in.InstanceInitiatedShutdownBehavior =
			ec2types.ShutdownBehavior(*r.InstanceInitiatedShutdownBehavior)
	}
	if aws.ToBool(r.Monitoring) {
		in.Monitoring = &ec2types.RunInstancesMonitoringEnabled{Enabled: aws.Bool(true)}
	}
	if placement := r.placement(); placement != nil {
		in.Placement = placement
	}
	if err := r.applyAddressing(in); err != nil {
		return nil, err
	}
	if err := r.applyBlockDevices(ctx, client, in); err != nil {
		return nil, err
	}
	return in, nil
}

// placement builds the Placement member from the Availability Zone and tenancy,
// returning nil when neither is set so RunInstances picks a zone itself.
func (r *Instance) placement() *ec2types.Placement {
	if r.AvailabilityZone == nil && r.Tenancy == nil {
		return nil
	}
	out := &ec2types.Placement{AvailabilityZone: r.AvailabilityZone}
	if r.Tenancy != nil {
		out.Tenancy = ec2types.Tenancy(*r.Tenancy)
	}
	return out
}

// applyAddressing sets where the instance connects. Without a public-address
// request, the subnet, security groups, and primary private address are
// top-level fields. With one, they move into the primary network interface at
// device index 0, the only form RunInstances accepts a public-address toggle in.
func (r *Instance) applyAddressing(in *ec2.RunInstancesInput) error {
	if r.AssociatePublicIpAddress == nil {
		in.SubnetId = r.SubnetId
		in.SecurityGroupIds = r.VpcSecurityGroupIds
		in.PrivateIpAddress = r.PrivateIp
		return nil
	}
	in.NetworkInterfaces = []ec2types.InstanceNetworkInterfaceSpecification{{
		DeviceIndex:              aws.Int32(0),
		AssociatePublicIpAddress: r.AssociatePublicIpAddress,
		SubnetId:                 r.SubnetId,
		Groups:                   r.VpcSecurityGroupIds,
		PrivateIpAddress:         r.PrivateIp,
	}}
	return nil
}

// applyBlockDevices adds the root, additional, and instance-store volume mappings
// to the request. The root mapping targets the AMI's root device name; with no
// ami input, the image comes from the launch template the instance launches
// from, so a root-block-device works on either path.
func (r *Instance) applyBlockDevices(
	ctx context.Context, client *ec2.Client, in *ec2.RunInstancesInput,
) error {
	var mappings []ec2types.BlockDeviceMapping
	if r.RootBlockDevice != nil {
		imageID := aws.ToString(r.Ami)
		if imageID == "" {
			resolved, err := r.launchTemplateImageId(ctx, client)
			if err != nil {
				return err
			}
			imageID = resolved
		}
		rootName, err := rootDeviceNameFromImage(ctx, client, imageID)
		if err != nil {
			return err
		}
		mappings = append(mappings, rootBlockDeviceMapping(r.RootBlockDevice, rootName))
	}
	mappings = append(mappings, ebsBlockDeviceMappings(r.EbsBlockDevice)...)
	mappings = append(mappings, ephemeralBlockDeviceMappings(r.EphemeralBlockDevice)...)
	if len(mappings) > 0 {
		in.BlockDeviceMappings = mappings
	}
	return nil
}

// tagSpecifications builds the create-time tag specifications for the instance
// and its volumes, each from its own tag map.
func (r *Instance) tagSpecifications() []ec2types.TagSpecification {
	var specs []ec2types.TagSpecification
	specs = append(specs, tagSpecifications(ec2types.ResourceTypeInstance, r.Tags)...)
	specs = append(specs, tagSpecifications(ec2types.ResourceTypeVolume, r.VolumeTags)...)
	return specs
}

// run calls RunInstances and returns the new instance id. A just-created IAM
// instance profile, or its role, may not have propagated when the call is made,
// which RunInstances reports as an InvalidParameterValue naming the profile or
// its missing roles; the call is retried over a couple of minutes for that. Some
// partitions cannot tag a resource at create, so a tagged run that fails for that
// reason is retried without the tag specifications and the tags are reconciled
// per resource afterward.
func (r *Instance) run(
	ctx context.Context, client *ec2.Client, in *ec2.RunInstancesInput,
) (string, error) {
	var resp *ec2.RunInstancesOutput
	taggedSeparately := false
	err := retry.OnError(ctx, isInstanceProfileNotReady, func(ctx context.Context) error {
		var runErr error
		resp, runErr = client.RunInstances(ctx, in)
		// The partition check matches the InvalidParameterValue code, which
		// is also how a not-yet-propagated IAM profile reports; that race is
		// retried with the tags intact, not treated as a partition gap.
		if runErr != nil && in.TagSpecifications != nil &&
			!isInstanceProfileNotReady(runErr) &&
			partition.UnsupportedOperation(region(client), runErr) {
			in.TagSpecifications = nil
			taggedSeparately = true
			resp, runErr = client.RunInstances(ctx, in)
		}
		return runErr
	}, retry.WithTimeout(2*time.Minute))
	if err != nil {
		return "", fmt.Errorf("run instances: %w", err)
	}
	if len(resp.Instances) == 0 {
		return "", fmt.Errorf("run instances returned no instances")
	}
	id := aws.ToString(resp.Instances[0].InstanceId)
	if taggedSeparately {
		if err := r.tagAfterCreate(ctx, client, id, resp.Instances[0]); err != nil {
			return "", err
		}
	}
	return id, nil
}

// tagAfterCreate reconciles the instance and volume tags with separate calls,
// for the partition path where the tagged run was not accepted. The volume ids
// come from the run response's block device mappings.
func (r *Instance) tagAfterCreate(
	ctx context.Context, client *ec2.Client, id string, instance ec2types.Instance,
) error {
	if len(r.Tags) > 0 {
		if err := syncTags(ctx, client, id, r.Tags); err != nil {
			return err
		}
	}
	if len(r.VolumeTags) > 0 {
		for _, volumeID := range instanceVolumeIds(instance.BlockDeviceMappings) {
			if err := syncTags(ctx, client, volumeID, r.VolumeTags); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyCreateFollowOns makes the create-time calls for the one field RunInstances
// does not accept. Source-dest-check defaults to enabled, so it is disabled only
// when the input explicitly asks for false; every other create-time field rides
// the run call itself.
func (r *Instance) applyCreateFollowOns(
	ctx context.Context, client *ec2.Client, id string,
) error {
	if r.SourceDestCheck != nil && !*r.SourceDestCheck {
		if err := r.setSourceDestCheck(ctx, client, id, false); err != nil {
			return err
		}
	}
	// Root-volume tags differ per device, so they cannot ride the create-time
	// volume tag specification; they are applied to the settled root volume.
	if tags := rootBlockDeviceTags(r.RootBlockDevice); len(tags) > 0 {
		instance, err := describeInstance(ctx, client, id)
		if err != nil {
			return err
		}
		volumeID := rootVolumeId(instance)
		if volumeID == "" {
			return fmt.Errorf("instance %s has no root volume to tag", id)
		}
		if err := syncTags(ctx, client, volumeID, tags); err != nil {
			return err
		}
	}
	return nil
}

// updateInPlace reconciles, in order, every field that changes without replacing
// the instance. Each block is gated on a real change to the input it reconciles,
// so a re-apply with no change makes no write.
func (r *Instance) updateInPlace(
	ctx context.Context, client *ec2.Client, id string,
	prior runtime.Prior[Instance, *InstanceOutput],
) error {
	if runtime.Changed(prior.Inputs.VolumeTags, r.VolumeTags) {
		if err := r.reconcileVolumeTags(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.IamInstanceProfile, r.IamInstanceProfile) {
		if err := r.reconcileIamProfile(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.SourceDestCheck, r.SourceDestCheck) {
		if err := r.setSourceDestCheck(ctx, client, id, aws.ToBool(r.SourceDestCheck)); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.VpcSecurityGroupIds, r.VpcSecurityGroupIds) {
		if err := r.setSecurityGroups(ctx, client, id); err != nil {
			return err
		}
	}
	if err := r.reconcileStoppedAttributes(ctx, client, id, prior); err != nil {
		return err
	}
	if err := r.reconcileProtections(ctx, client, id, prior); err != nil {
		return err
	}
	if runtime.Changed(
		prior.Inputs.InstanceInitiatedShutdownBehavior, r.InstanceInitiatedShutdownBehavior) {
		if err := r.setShutdownBehavior(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.Monitoring, r.Monitoring) {
		if err := r.setMonitoring(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.MetadataOptions, r.MetadataOptions) {
		if err := r.reconcileMetadataOptions(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.RootBlockDevice, r.RootBlockDevice) {
		if err := r.reconcileRootBlockDevice(ctx, client, id, prior); err != nil {
			return err
		}
	}
	return nil
}

// read fetches the instance by id and returns its computed outputs. The
// secondary describe of the root volume is best-effort: a transient failure
// reading it must not turn a live instance into a gone one, since only the
// primary describe decides that.
func (r *Instance) read(
	ctx context.Context, client *ec2.Client, id string,
) (*InstanceOutput, error) {
	instance, err := describeInstance(ctx, client, id)
	if err != nil {
		return nil, err
	}
	out := &InstanceOutput{
		InstanceId:                aws.ToString(instance.InstanceId),
		AvailabilityZone:          placementZone(instance.Placement),
		SubnetId:                  aws.ToString(instance.SubnetId),
		PrivateIp:                 aws.ToString(instance.PrivateIpAddress),
		PublicIp:                  aws.ToString(instance.PublicIpAddress),
		PrivateDns:                aws.ToString(instance.PrivateDnsName),
		PublicDns:                 aws.ToString(instance.PublicDnsName),
		PrimaryNetworkInterfaceId: primaryNetworkInterfaceId(instance),
		RootDeviceName:            aws.ToString(instance.RootDeviceName),
		RootVolumeId:              rootVolumeId(instance),
	}
	if instance.State != nil {
		out.InstanceState = string(instance.State.Name)
	}
	return out, nil
}

// encodedUserData returns the base64 user data RunInstances expects. The
// user-data field is plain text the SDK base64-encodes here; the
// user-data-base64 field is already encoded and passes through. Exactly one is
// set, enforced by a constraint, so the first non-nil wins.
func (r *Instance) encodedUserData() (*string, error) {
	if r.UserData != nil {
		return aws.String(base64.StdEncoding.EncodeToString([]byte(*r.UserData))), nil
	}
	if r.UserDataBase64 != nil {
		if _, err := base64.StdEncoding.DecodeString(*r.UserDataBase64); err != nil {
			return nil, fmt.Errorf("user-data-base64 is not valid base64: %w", err)
		}
		return r.UserDataBase64, nil
	}
	return nil, nil
}

// rootDeviceNameFromImage resolves the image's root device name, which the root
// block device mapping targets. An image that cannot be found, or that reports no
// root device name, stops the create with a descriptive error.
func rootDeviceNameFromImage(
	ctx context.Context, client *ec2.Client, imageID string,
) (string, error) {
	resp, err := client.DescribeImages(ctx, &ec2.DescribeImagesInput{
		ImageIds: []string{imageID},
	})
	if err != nil {
		return "", fmt.Errorf("describe images: %w", err)
	}
	if len(resp.Images) == 0 {
		return "", fmt.Errorf("image %s not found", imageID)
	}
	name := aws.ToString(resp.Images[0].RootDeviceName)
	if name == "" {
		return "", fmt.Errorf("image %s reports no root device name", imageID)
	}
	return name, nil
}

// launchTemplateImageId resolves the image id from the instance's launch
// template, for a root-block-device on an instance whose configuration names no
// ami of its own. With no version set, the template's default version applies,
// matching what RunInstances launches.
func (r *Instance) launchTemplateImageId(
	ctx context.Context, client *ec2.Client,
) (string, error) {
	lt := r.LaunchTemplate
	if lt == nil {
		return "", fmt.Errorf(
			"root-block-device requires ami or launch-template to resolve the root device name")
	}
	version := "$Default"
	if lt.Version != nil {
		version = *lt.Version
	}
	in := &ec2.DescribeLaunchTemplateVersionsInput{Versions: []string{version}}
	if lt.Id != nil {
		in.LaunchTemplateId = lt.Id
	} else {
		in.LaunchTemplateName = lt.Name
	}
	resp, err := client.DescribeLaunchTemplateVersions(ctx, in)
	if err != nil {
		return "", fmt.Errorf("describe launch template versions: %w", err)
	}
	if len(resp.LaunchTemplateVersions) == 0 ||
		resp.LaunchTemplateVersions[0].LaunchTemplateData == nil {
		return "", fmt.Errorf("launch template version %s not found", version)
	}
	imageID := aws.ToString(resp.LaunchTemplateVersions[0].LaunchTemplateData.ImageId)
	if imageID == "" {
		return "", fmt.Errorf(
			"launch template version %s names no image; set ami to use root-block-device",
			version)
	}
	return imageID, nil
}

// reconcileVolumeTags brings the tags on each of the instance's EBS volumes to
// the desired set. The volume ids come from a fresh describe of the instance.
func (r *Instance) reconcileVolumeTags(
	ctx context.Context, client *ec2.Client, id string,
) error {
	instance, err := describeInstance(ctx, client, id)
	if err != nil {
		return err
	}
	for _, volumeID := range instanceVolumeIds(instance.BlockDeviceMappings) {
		if err := syncTags(ctx, client, volumeID, r.VolumeTags); err != nil {
			return err
		}
	}
	return nil
}

// reconcileIamProfile brings the instance's IAM instance profile to the desired
// state. With no profile currently associated, the desired one is associated;
// with one associated and a desired profile, it is replaced or, when the desired
// profile is now empty, disassociated. A running instance is replaced through
// ReplaceIamInstanceProfileAssociation; a stopped or stopping one cannot be
// replaced, so the association is removed and re-added. Either change is retried
// while a just-created profile is still propagating, and waited until the
// association reports associated.
func (r *Instance) reconcileIamProfile(
	ctx context.Context, client *ec2.Client, id string,
) error {
	assoc, err := instanceProfileAssociation(ctx, client, id)
	if err != nil {
		return err
	}
	desired := aws.ToString(r.IamInstanceProfile)
	if assoc == nil {
		if desired == "" {
			return nil
		}
		return r.associateIamProfile(ctx, client, id)
	}
	if desired == "" {
		_, err := client.DisassociateIamInstanceProfile(ctx,
			&ec2.DisassociateIamInstanceProfileInput{AssociationId: assoc.AssociationId})
		if err != nil {
			return fmt.Errorf("disassociate iam instance profile: %w", err)
		}
		return nil
	}
	state, err := instanceState(ctx, client, id)
	if err != nil {
		return err
	}
	if state == ec2types.InstanceStateNameRunning {
		return r.replaceIamProfile(ctx, client, id, aws.ToString(assoc.AssociationId))
	}
	_, err = client.DisassociateIamInstanceProfile(ctx,
		&ec2.DisassociateIamInstanceProfileInput{AssociationId: assoc.AssociationId})
	if err != nil {
		return fmt.Errorf("disassociate iam instance profile: %w", err)
	}
	return r.associateIamProfile(ctx, client, id)
}

// associateIamProfile attaches the desired profile by name, retrying while it is
// still propagating, then waits for the association to report associated.
func (r *Instance) associateIamProfile(
	ctx context.Context, client *ec2.Client, id string,
) error {
	err := retry.OnError(ctx, isInstanceProfileNotReady, func(ctx context.Context) error {
		_, err := client.AssociateIamInstanceProfile(ctx,
			&ec2.AssociateIamInstanceProfileInput{
				InstanceId:         aws.String(id),
				IamInstanceProfile: &ec2types.IamInstanceProfileSpecification{Name: r.IamInstanceProfile},
			})
		return err
	}, retry.WithTimeout(2*time.Minute))
	if err != nil {
		return fmt.Errorf("associate iam instance profile: %w", err)
	}
	return r.waitIamProfileAssociated(ctx, client, id)
}

// replaceIamProfile swaps a running instance's profile association for the
// desired profile, retrying while it is still propagating, then waits for the new
// association to report associated.
func (r *Instance) replaceIamProfile(
	ctx context.Context, client *ec2.Client, id, associationID string,
) error {
	err := retry.OnError(ctx, isInstanceProfileNotReady, func(ctx context.Context) error {
		_, err := client.ReplaceIamInstanceProfileAssociation(ctx,
			&ec2.ReplaceIamInstanceProfileAssociationInput{
				AssociationId:      aws.String(associationID),
				IamInstanceProfile: &ec2types.IamInstanceProfileSpecification{Name: r.IamInstanceProfile},
			})
		return err
	}, retry.WithTimeout(2*time.Minute))
	if err != nil {
		return fmt.Errorf("replace iam instance profile association: %w", err)
	}
	return r.waitIamProfileAssociated(ctx, client, id)
}

// setSourceDestCheck sets the instance's source-dest-check flag with its own
// single-attribute call.
func (r *Instance) setSourceDestCheck(
	ctx context.Context, client *ec2.Client, id string, value bool,
) error {
	_, err := client.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId:      aws.String(id),
		SourceDestCheck: &ec2types.AttributeBooleanValue{Value: aws.Bool(value)},
	})
	if err != nil {
		return fmt.Errorf("modify source-dest-check: %w", err)
	}
	return nil
}

// setSecurityGroups replaces the instance's security group set in one call. EC2
// requires at least one group, so an empty desired set is rejected by the API
// rather than silently cleared.
func (r *Instance) setSecurityGroups(
	ctx context.Context, client *ec2.Client, id string,
) error {
	_, err := client.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(id),
		Groups:     r.VpcSecurityGroupIds,
	})
	if err != nil {
		return fmt.Errorf("modify security groups: %w", err)
	}
	return nil
}

// reconcileStoppedAttributes reconciles the instance type and user data, the two
// attributes that can change only while the instance is stopped. When either
// changed, the instance is stopped, the changed attributes are modified one call
// each, and the instance is started again. User data is modified only on its own
// change and the instance type on its own, so an unchanged one is left alone even
// when the other moves.
func (r *Instance) reconcileStoppedAttributes(
	ctx context.Context, client *ec2.Client, id string,
	prior runtime.Prior[Instance, *InstanceOutput],
) error {
	typeChanged := runtime.Changed(prior.Inputs.InstanceType, r.InstanceType)
	userDataChanged := runtime.Changed(prior.Inputs.UserData, r.UserData) ||
		runtime.Changed(prior.Inputs.UserDataBase64, r.UserDataBase64)
	if !typeChanged && !userDataChanged {
		return nil
	}
	if err := r.stop(ctx, client, id); err != nil {
		return err
	}
	if typeChanged {
		if err := r.setInstanceType(ctx, client, id); err != nil {
			return err
		}
	}
	if userDataChanged {
		if err := r.setUserData(ctx, client, id); err != nil {
			return err
		}
	}
	return r.start(ctx, client, id)
}

// setInstanceType modifies the stopped instance's type with its own
// single-attribute call.
func (r *Instance) setInstanceType(
	ctx context.Context, client *ec2.Client, id string,
) error {
	_, err := client.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId:   aws.String(id),
		InstanceType: &ec2types.AttributeValue{Value: r.InstanceType},
	})
	if err != nil {
		return fmt.Errorf("modify instance type: %w", err)
	}
	return nil
}

// setUserData modifies the stopped instance's user data with its own
// single-attribute call. The user data is base64-decoded to raw bytes before the
// call, because ModifyInstanceAttribute base64-encodes the blob value itself and
// passing the already-encoded form would double-encode it.
func (r *Instance) setUserData(
	ctx context.Context, client *ec2.Client, id string,
) error {
	encoded, err := r.encodedUserData()
	if err != nil {
		return err
	}
	var raw []byte
	if encoded != nil {
		raw, err = base64.StdEncoding.DecodeString(*encoded)
		if err != nil {
			return fmt.Errorf("decode user data: %w", err)
		}
	}
	_, err = client.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(id),
		UserData:   &ec2types.BlobAttributeValue{Value: raw},
	})
	if err != nil {
		return fmt.Errorf("modify user data: %w", err)
	}
	return nil
}

// reconcileProtections reconciles the stop- and termination-protection flags,
// each on its own change and each through its own single-attribute call.
func (r *Instance) reconcileProtections(
	ctx context.Context, client *ec2.Client, id string,
	prior runtime.Prior[Instance, *InstanceOutput],
) error {
	if runtime.Changed(prior.Inputs.DisableApiStop, r.DisableApiStop) {
		if err := r.setProtection(ctx, client, id,
			ec2types.InstanceAttributeNameDisableApiStop, aws.ToBool(r.DisableApiStop)); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.DisableApiTermination, r.DisableApiTermination) {
		if err := r.setProtection(ctx, client, id,
			ec2types.InstanceAttributeNameDisableApiTermination,
			aws.ToBool(r.DisableApiTermination)); err != nil {
			return err
		}
	}
	return nil
}

// setProtection sets one of the two protection attributes with its own
// single-attribute call. Either attribute is rejected as unsupported for a spot
// instance, which is treated as nothing to do rather than a failure, since the
// protection does not apply there.
func (r *Instance) setProtection(
	ctx context.Context, client *ec2.Client, id string,
	attribute ec2types.InstanceAttributeName, value bool,
) error {
	in := &ec2.ModifyInstanceAttributeInput{InstanceId: aws.String(id)}
	switch attribute {
	case ec2types.InstanceAttributeNameDisableApiStop:
		in.DisableApiStop = &ec2types.AttributeBooleanValue{Value: aws.Bool(value)}
	case ec2types.InstanceAttributeNameDisableApiTermination:
		in.DisableApiTermination = &ec2types.AttributeBooleanValue{Value: aws.Bool(value)}
	}
	_, err := client.ModifyInstanceAttribute(ctx, in)
	if err != nil {
		if isSpotUnsupported(err) {
			return nil
		}
		return fmt.Errorf("modify %s: %w", attribute, err)
	}
	return nil
}

// setShutdownBehavior sets the instance-initiated shutdown behavior with its own
// single-attribute call, which uses the generic string value member.
func (r *Instance) setShutdownBehavior(
	ctx context.Context, client *ec2.Client, id string,
) error {
	value := &ec2types.AttributeValue{Value: r.InstanceInitiatedShutdownBehavior}
	_, err := client.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId:                        aws.String(id),
		InstanceInitiatedShutdownBehavior: value,
	})
	if err != nil {
		return fmt.Errorf("modify shutdown behavior: %w", err)
	}
	return nil
}

// setMonitoring turns detailed monitoring on or off to match the desired flag,
// using the enable or disable call as appropriate.
func (r *Instance) setMonitoring(
	ctx context.Context, client *ec2.Client, id string,
) error {
	if aws.ToBool(r.Monitoring) {
		_, err := client.MonitorInstances(ctx, &ec2.MonitorInstancesInput{
			InstanceIds: []string{id},
		})
		if err != nil {
			return fmt.Errorf("monitor instances: %w", err)
		}
		return nil
	}
	_, err := client.UnmonitorInstances(ctx, &ec2.UnmonitorInstancesInput{
		InstanceIds: []string{id},
	})
	if err != nil {
		return fmt.Errorf("unmonitor instances: %w", err)
	}
	return nil
}

// reconcileMetadataOptions applies the desired metadata options and waits for
// them to take effect. A removed block leaves the options at their last applied
// state rather than clearing them, since there is no empty form to send. The
// modify is retried once without the tags member when the partition reports
// instance-metadata-tags unsupported, the one parameter some partitions reject.
func (r *Instance) reconcileMetadataOptions(
	ctx context.Context, client *ec2.Client, id string,
) error {
	if r.MetadataOptions == nil {
		return nil
	}
	in := r.metadataModifyInput(id)
	_, err := client.ModifyInstanceMetadataOptions(ctx, in)
	if err != nil && isMetadataTagsUnsupported(err) {
		in.InstanceMetadataTags = ""
		_, err = client.ModifyInstanceMetadataOptions(ctx, in)
	}
	if err != nil {
		return fmt.Errorf("modify instance metadata options: %w", err)
	}
	return r.waitMetadataOptionsApplied(ctx, client, id)
}

// metadataModifyInput builds the modify-metadata request from the desired block.
// When the endpoint is being disabled, only the endpoint state and the token
// requirement travel; the hop limit, protocol, and metadata-tags settings apply
// only to an enabled endpoint.
func (r *Instance) metadataModifyInput(id string) *ec2.ModifyInstanceMetadataOptionsInput {
	b := r.MetadataOptions
	in := &ec2.ModifyInstanceMetadataOptionsInput{InstanceId: aws.String(id)}
	if b.HttpEndpoint != nil {
		in.HttpEndpoint = ec2types.InstanceMetadataEndpointState(*b.HttpEndpoint)
	}
	if b.HttpTokens != nil {
		in.HttpTokens = ec2types.HttpTokensState(*b.HttpTokens)
	}
	if b.HttpEndpoint != nil && *b.HttpEndpoint == "disabled" {
		return in
	}
	in.HttpPutResponseHopLimit = ptr.Int32(b.HttpPutResponseHopLimit)
	if b.HttpProtocolIpv6 != nil {
		in.HttpProtocolIpv6 = ec2types.InstanceMetadataProtocolState(*b.HttpProtocolIpv6)
	}
	if b.InstanceMetadataTags != nil {
		in.InstanceMetadataTags = ec2types.InstanceMetadataTagsState(*b.InstanceMetadataTags)
	}
	return in
}

// reconcileRootBlockDevice brings the root volume's in-place fields to the
// desired state. A change to encrypted or kms-key-id cannot apply to an existing
// volume, so it stops the update with a clear error rather than replacing the
// instance. The size, type, IOPS, and throughput are reconciled with ModifyVolume
// and waited until the modification settles; the delete-on-termination flag is
// reconciled through the instance's block device mapping and waited until it
// reads back; the tags are reconciled as a set.
func (r *Instance) reconcileRootBlockDevice(
	ctx context.Context, client *ec2.Client, id string,
	prior runtime.Prior[Instance, *InstanceOutput],
) error {
	desired := r.RootBlockDevice
	if desired == nil {
		return nil
	}
	priorRoot := prior.Inputs.RootBlockDevice
	if rootEncryptionChanged(priorRoot, desired) {
		return fmt.Errorf(
			"root-block-device encrypted and kms-key-id cannot change in place; " +
				"replace the instance to re-encrypt the root volume")
	}
	instance, err := describeInstance(ctx, client, id)
	if err != nil {
		return err
	}
	volumeID := rootVolumeId(instance)
	if volumeID == "" {
		return fmt.Errorf("instance %s has no root volume to reconcile", id)
	}
	if rootVolumeModifyChanged(priorRoot, desired) {
		if err := r.modifyRootVolume(ctx, client, volumeID); err != nil {
			return err
		}
	}
	if runtime.Changed(rootDeleteOnTermination(priorRoot), rootDeleteOnTermination(desired)) {
		if err := r.setRootDeleteOnTermination(ctx, client, id, instance); err != nil {
			return err
		}
	}
	if runtime.Changed(rootBlockDeviceTags(priorRoot), rootBlockDeviceTags(desired)) {
		if err := syncTags(ctx, client, volumeID, rootBlockDeviceTags(desired)); err != nil {
			return err
		}
	}
	return nil
}

// rootBlockDeviceTags returns the block's root-volume tag map, or nil when the
// block or its tags are absent.
func rootBlockDeviceTags(b *InstanceRootBlockDevice) map[string]string {
	if b == nil || b.Tags == nil {
		return nil
	}
	return *b.Tags
}

// modifyRootVolume applies the root volume's size, type, IOPS, and throughput in
// one ModifyVolume call and waits for the volume to settle.
func (r *Instance) modifyRootVolume(
	ctx context.Context, client *ec2.Client, volumeID string,
) error {
	b := r.RootBlockDevice
	in := &ec2.ModifyVolumeInput{
		VolumeId:   aws.String(volumeID),
		Size:       ptr.Int32(b.VolumeSize),
		Iops:       ptr.Int32(b.Iops),
		Throughput: ptr.Int32(b.Throughput),
	}
	if b.VolumeType != nil {
		in.VolumeType = ec2types.VolumeType(*b.VolumeType)
	}
	if _, err := client.ModifyVolume(ctx, in); err != nil {
		return fmt.Errorf("modify root volume: %w", err)
	}
	return r.waitRootVolumeModified(ctx, client, volumeID)
}

// setRootDeleteOnTermination reconciles the root volume's delete-on-termination
// flag through the instance's block device mapping, then waits for the new value
// to read back. The root device is identified by the instance's root device name.
func (r *Instance) setRootDeleteOnTermination(
	ctx context.Context, client *ec2.Client, id string, instance *ec2types.Instance,
) error {
	want := aws.ToBool(rootDeleteOnTermination(r.RootBlockDevice))
	rootName := aws.ToString(instance.RootDeviceName)
	_, err := client.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(id),
		BlockDeviceMappings: []ec2types.InstanceBlockDeviceMappingSpecification{{
			DeviceName: aws.String(rootName),
			Ebs: &ec2types.EbsInstanceBlockDeviceSpecification{
				DeleteOnTermination: aws.Bool(want),
			},
		}},
	})
	if err != nil {
		return fmt.Errorf("modify root delete-on-termination: %w", err)
	}
	return r.waitRootDeleteOnTermination(ctx, client, id, rootName, want)
}

// stop stops the instance and waits for it to reach the stopped state. A stop is
// idempotent on an already-stopped instance, which the wait then completes on its
// first poll.
func (r *Instance) stop(ctx context.Context, client *ec2.Client, id string) error {
	_, err := client.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{id}})
	if err != nil {
		return fmt.Errorf("stop instances: %w", err)
	}
	return r.waitStopped(ctx, client, id)
}

// start starts the instance and waits for it to reach the running state. Right
// after a type change, StartInstances can briefly report that the launch plan's
// instance type does not match the attribute value while the change propagates;
// the call is retried over a few minutes for that.
func (r *Instance) start(ctx context.Context, client *ec2.Client, id string) error {
	err := retry.OnError(ctx, isLaunchPlanTypeMismatch, func(ctx context.Context) error {
		_, err := client.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{id}})
		return err
	}, retry.WithTimeout(5*time.Minute))
	if err != nil {
		return fmt.Errorf("start instances: %w", err)
	}
	return r.waitRunning(ctx, client, id)
}

// waitRunning polls the instance until it reaches the running state, the point at
// which a create or start has settled and the computed values exist. An instance
// that reaches terminated instead -- a launch that failed -- stops the wait with
// a clear error rather than polling to the timeout. A describe that cannot yet
// find the just-launched instance is tolerated for a bounded run of consecutive
// polls.
func (r *Instance) waitRunning(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("instance %s to be running", id)
	notFound := 0
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		state, err := observeInstanceState(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				notFound++
				if notFound > instanceCreateNotFoundLimit {
					return false, runtime.ErrNotFound
				}
				return false, nil
			}
			return false, err
		}
		notFound = 0
		switch state {
		case ec2types.InstanceStateNameRunning:
			return true, nil
		case ec2types.InstanceStateNameTerminated, ec2types.InstanceStateNameShuttingDown:
			return false, fmt.Errorf("instance %s entered state %s while waiting to run", id, state)
		default:
			return false, nil
		}
	}, wait.WithTimeout(10*time.Minute))
}

// waitStopped polls the instance until it reaches the stopped state. An instance
// that terminates instead stops the wait with a clear error. A not-found is
// tolerated as a transient describe lag.
func (r *Instance) waitStopped(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("instance %s to stop", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		state, err := observeInstanceState(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		switch state {
		case ec2types.InstanceStateNameStopped:
			return true, nil
		case ec2types.InstanceStateNameTerminated:
			return false, fmt.Errorf("instance %s terminated while waiting to stop", id)
		default:
			return false, nil
		}
	}, wait.WithTimeout(10*time.Minute))
}

// waitTerminated polls the instance until it reaches the terminated state. A
// not-found means the instance is fully gone, which is the same outcome as
// terminated. Every transitional state -- including shutting-down -- is pending.
func (r *Instance) waitTerminated(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("instance %s to terminate", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		state, err := observeInstanceState(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		if state == ec2types.InstanceStateNameTerminated {
			return true, nil
		}
		return false, nil
	}, wait.WithTimeout(20*time.Minute), wait.WithInterval(10*time.Second))
}

// waitIamProfileAssociated polls the instance's IAM profile association until it
// reports the associated state. A missing association is tolerated as still
// associating.
func (r *Instance) waitIamProfileAssociated(
	ctx context.Context, client *ec2.Client, id string,
) error {
	what := fmt.Sprintf("instance %s iam profile association", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		assoc, err := instanceProfileAssociation(ctx, client, id)
		if err != nil {
			return false, err
		}
		if assoc == nil {
			return false, nil
		}
		return assoc.State == ec2types.IamInstanceProfileAssociationStateAssociated, nil
	}, wait.WithTimeout(5*time.Minute))
}

// waitMetadataOptionsApplied polls the instance until its metadata options report
// the applied state, leaving the pending state.
func (r *Instance) waitMetadataOptionsApplied(
	ctx context.Context, client *ec2.Client, id string,
) error {
	what := fmt.Sprintf("instance %s metadata options", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		instance, err := describeInstance(ctx, client, id)
		if err != nil {
			return false, err
		}
		if instance.MetadataOptions == nil {
			return false, nil
		}
		return instance.MetadataOptions.State == ec2types.InstanceMetadataOptionsStateApplied, nil
	}, wait.WithTimeout(10*time.Minute))
}

// waitRootVolumeModified polls the root volume until its ModifyVolume settles,
// reusing the volume-modification states the volume resource waits on.
func (r *Instance) waitRootVolumeModified(
	ctx context.Context, client *ec2.Client, volumeID string,
) error {
	what := fmt.Sprintf("root volume %s modification", volumeID)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		volume, err := describeVolume(ctx, client, volumeID)
		if err != nil {
			return false, err
		}
		switch volume.State {
		case ec2types.VolumeStateAvailable, ec2types.VolumeStateInUse:
			return true, nil
		case ec2types.VolumeStateError:
			return false, fmt.Errorf("root volume %s entered state error", volumeID)
		default:
			return false, nil
		}
	}, wait.WithTimeout(10*time.Minute))
}

// waitRootDeleteOnTermination polls the instance until its root device reports the
// wanted delete-on-termination flag, confirming the block-device modify took
// effect.
func (r *Instance) waitRootDeleteOnTermination(
	ctx context.Context, client *ec2.Client, id, rootName string, want bool,
) error {
	what := fmt.Sprintf("instance %s root delete-on-termination", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		instance, err := describeInstance(ctx, client, id)
		if err != nil {
			return false, err
		}
		for i := range instance.BlockDeviceMappings {
			mapping := instance.BlockDeviceMappings[i]
			if aws.ToString(mapping.DeviceName) == rootName && mapping.Ebs != nil {
				return aws.ToBool(mapping.Ebs.DeleteOnTermination) == want, nil
			}
		}
		return false, nil
	}, wait.WithTimeout(5*time.Minute))
}

// observeInstanceState returns the instance's current state name, mapping a
// missing instance to runtime.ErrNotFound so a wait can ride out a describe lag
// or recognize a fully-gone instance. Unlike a resource Read, this does not treat
// a terminated state as gone: the waiters need to observe terminated to decide
// whether it is the wanted target or a failure.
func observeInstanceState(
	ctx context.Context, client *ec2.Client, id string,
) (ec2types.InstanceStateName, error) {
	resp, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, instanceNotFoundCode) {
			return "", runtime.ErrNotFound
		}
		return "", fmt.Errorf("describe instances: %w", err)
	}
	instance := singleInstance(resp)
	if instance == nil {
		return "", runtime.ErrNotFound
	}
	if instance.State == nil {
		return "", nil
	}
	return instance.State.Name, nil
}

// instanceState returns the instance's current state name through the gone-aware
// describe, so a terminated or missing instance reads as not-found.
func instanceState(
	ctx context.Context, client *ec2.Client, id string,
) (ec2types.InstanceStateName, error) {
	instance, err := describeInstance(ctx, client, id)
	if err != nil {
		return "", err
	}
	if instance.State == nil {
		return "", nil
	}
	return instance.State.Name, nil
}

// instanceProfileAssociation returns the instance's current associated IAM
// profile association, or nil when none is associated. It is not best-effort: a
// describe error stops the caller.
func instanceProfileAssociation(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.IamInstanceProfileAssociation, error) {
	resp, err := client.DescribeIamInstanceProfileAssociations(ctx,
		&ec2.DescribeIamInstanceProfileAssociationsInput{
			Filters: []ec2types.Filter{{
				Name:   aws.String("instance-id"),
				Values: []string{id},
			}},
		})
	if err != nil {
		return nil, fmt.Errorf("describe iam instance profile associations: %w", err)
	}
	for i := range resp.IamInstanceProfileAssociations {
		assoc := resp.IamInstanceProfileAssociations[i]
		if assoc.State != ec2types.IamInstanceProfileAssociationStateDisassociated {
			return &assoc, nil
		}
	}
	return nil, nil
}

// describeInstance fetches the instance with the given id, mapping a gone
// instance to runtime.ErrNotFound. EC2 reports a missing instance by service code
// on an HTTP 400, never a 404. A terminated instance still describes, so a
// terminated state means the instance the caller asked for is gone, the headline
// not-found rule; a shutting-down instance is still live. A returned id that does
// not match the requested one is a stale read and counts as gone.
func describeInstance(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.Instance, error) {
	resp, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, instanceNotFoundCode) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe instances: %w", err)
	}
	instance := singleInstance(resp)
	if instance == nil {
		return nil, runtime.ErrNotFound
	}
	if instance.State != nil && instance.State.Name == ec2types.InstanceStateNameTerminated {
		return nil, runtime.ErrNotFound
	}
	if aws.ToString(instance.InstanceId) != id {
		return nil, runtime.ErrNotFound
	}
	return instance, nil
}

// singleInstance returns the one instance from a describe response, flattening
// the reservation nesting, or nil when none is present.
func singleInstance(resp *ec2.DescribeInstancesOutput) *ec2types.Instance {
	for i := range resp.Reservations {
		for j := range resp.Reservations[i].Instances {
			return &resp.Reservations[i].Instances[j]
		}
	}
	return nil
}

// placementZone returns the Availability Zone from an instance's placement, or
// the empty string when there is none.
func placementZone(placement *ec2types.Placement) string {
	if placement == nil {
		return ""
	}
	return aws.ToString(placement.AvailabilityZone)
}

// primaryNetworkInterfaceId returns the id of the instance's primary network
// interface, the one at device index 0, falling back to the sole interface when
// none is marked primary.
func primaryNetworkInterfaceId(instance *ec2types.Instance) string {
	for i := range instance.NetworkInterfaces {
		ni := instance.NetworkInterfaces[i]
		if ni.Attachment != nil && aws.ToInt32(ni.Attachment.DeviceIndex) == 0 {
			return aws.ToString(ni.NetworkInterfaceId)
		}
	}
	if len(instance.NetworkInterfaces) > 0 {
		return aws.ToString(instance.NetworkInterfaces[0].NetworkInterfaceId)
	}
	return ""
}

// rootVolumeId returns the id of the instance's root EBS volume, identified by
// matching the root device name against the block device mappings.
func rootVolumeId(instance *ec2types.Instance) string {
	rootName := aws.ToString(instance.RootDeviceName)
	for i := range instance.BlockDeviceMappings {
		mapping := instance.BlockDeviceMappings[i]
		if aws.ToString(mapping.DeviceName) == rootName && mapping.Ebs != nil {
			return aws.ToString(mapping.Ebs.VolumeId)
		}
	}
	return ""
}

// instanceVolumeIds returns the ids of the EBS volumes attached through the
// instance's block device mappings.
func instanceVolumeIds(mappings []ec2types.InstanceBlockDeviceMapping) []string {
	var ids []string
	for i := range mappings {
		if mappings[i].Ebs != nil {
			if id := aws.ToString(mappings[i].Ebs.VolumeId); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// rootEncryptionChanged reports whether the root volume's encrypted flag or KMS
// key changed between the prior and desired blocks, the change that cannot apply
// to an existing volume.
func rootEncryptionChanged(prior, desired *InstanceRootBlockDevice) bool {
	return runtime.Changed(rootEncrypted(prior), rootEncrypted(desired)) ||
		runtime.Changed(rootKmsKeyId(prior), rootKmsKeyId(desired))
}

// rootVolumeModifyChanged reports whether any field ModifyVolume reconciles on
// the root volume -- size, type, IOPS, or throughput -- changed between the prior
// and desired blocks.
func rootVolumeModifyChanged(prior, desired *InstanceRootBlockDevice) bool {
	return runtime.Changed(rootVolumeSize(prior), rootVolumeSize(desired)) ||
		runtime.Changed(rootVolumeType(prior), rootVolumeType(desired)) ||
		runtime.Changed(rootIops(prior), rootIops(desired)) ||
		runtime.Changed(rootThroughput(prior), rootThroughput(desired))
}

func rootEncrypted(b *InstanceRootBlockDevice) *bool {
	if b == nil {
		return nil
	}
	return b.Encrypted
}

func rootKmsKeyId(b *InstanceRootBlockDevice) *string {
	if b == nil {
		return nil
	}
	return b.KmsKeyId
}

func rootVolumeSize(b *InstanceRootBlockDevice) *int64 {
	if b == nil {
		return nil
	}
	return b.VolumeSize
}

func rootVolumeType(b *InstanceRootBlockDevice) *string {
	if b == nil {
		return nil
	}
	return b.VolumeType
}

func rootIops(b *InstanceRootBlockDevice) *int64 {
	if b == nil {
		return nil
	}
	return b.Iops
}

func rootThroughput(b *InstanceRootBlockDevice) *int64 {
	if b == nil {
		return nil
	}
	return b.Throughput
}

// rootDeleteOnTermination returns the block's effective delete-on-termination
// flag. An absent block or an omitted field means the declared default of
// true, matching what create sends, so an omission never reads as a change.
func rootDeleteOnTermination(b *InstanceRootBlockDevice) *bool {
	if b == nil {
		return aws.Bool(true)
	}
	return deleteOnTerminationOrDefault(b.DeleteOnTermination)
}

// isInstanceProfileNotReady reports whether an error is the propagation delay a
// just-created IAM instance profile or its role returns, which clears on its own.
// EC2 raises it as an InvalidParameterValue whose message names the missing
// profile or its absent roles.
func isInstanceProfileNotReady(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Invalid IAM Instance Profile") ||
		strings.Contains(msg, "has no associated IAM Roles")
}

// isLaunchPlanTypeMismatch reports whether a StartInstances error is the
// transient launch-plan mismatch that can follow an instance-type change, which
// clears once the change propagates.
func isLaunchPlanTypeMismatch(err error) bool {
	return err != nil && strings.Contains(
		err.Error(), "LaunchPlan instance type does not match attribute value")
}

// isMetadataTagsUnsupported reports whether a ModifyInstanceMetadataOptions error
// is the partition limitation on the instance-metadata-tags parameter, signaled
// as an UnsupportedOperation naming InstanceMetadataTags.
func isMetadataTagsUnsupported(err error) bool {
	if !isNotFound(err, "UnsupportedOperation") {
		return false
	}
	return strings.Contains(err.Error(), "InstanceMetadataTags")
}

// isSpotUnsupported reports whether a protection-attribute modify failed because
// the attribute is not supported for a spot instance, which is not a real failure
// since the protection does not apply there.
func isSpotUnsupported(err error) bool {
	if !isNotFound(err, "UnsupportedOperation") {
		return false
	}
	return strings.Contains(err.Error(), "not supported for spot instances")
}

// instanceNotFoundCode is the EC2 service error code for an instance that does
// not exist, returned on an HTTP 400 rather than a 404.
const instanceNotFoundCode = "InvalidInstanceID.NotFound"

// instanceCreateNotFoundLimit is how many consecutive not-found describes the
// run wait tolerates before giving up, covering the brief window where a
// just-launched instance is not yet visible to a describe.
const instanceCreateNotFoundLimit = 20
