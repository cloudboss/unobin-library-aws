package ec2

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// The blocks below model the structured members of a run-instances request that
// an instance holds alongside its scalar fields: the metadata-service options,
// the root and additional EBS volumes, the instance-store volumes, and the
// launch-template reference. Each converts to its SDK request type through a free
// function that handles a nil block, so the long SDK type names stay off the
// column-limited method-receiver lines. A nil block leaves that member unset, so
// AWS applies its own default. The enum, bound, and pairing rules on these block
// fields derive as constraints on the Instance type, reached by selector for the
// single blocks and through ForEach for the volume lists.

// InstanceMetadataOptions configures the instance metadata service: whether the
// HTTP endpoint answers, whether IMDSv2 tokens are required, the PUT response hop
// limit, IPv6 support, and whether instance tags are reachable through metadata.
// AWS applies its own default for any field left unset.
type InstanceMetadataOptions struct {
	HttpEndpoint            *string `ub:"http-endpoint"`
	HttpProtocolIpv6        *string `ub:"http-protocol-ipv6"`
	HttpPutResponseHopLimit *int64  `ub:"http-put-response-hop-limit"`
	HttpTokens              *string `ub:"http-tokens"`
	InstanceMetadataTags    *string `ub:"instance-metadata-tags"`
}

// metadataOptionsRequest converts the metadata-options block to the run-instances
// SDK request type. A nil block returns nil, leaving the instance with the AMI
// and account defaults.
func metadataOptionsRequest(
	b *InstanceMetadataOptions,
) *ec2types.InstanceMetadataOptionsRequest {
	if b == nil {
		return nil
	}
	out := &ec2types.InstanceMetadataOptionsRequest{
		HttpPutResponseHopLimit: ptr.Int32(b.HttpPutResponseHopLimit),
	}
	if b.HttpEndpoint != nil {
		out.HttpEndpoint = ec2types.InstanceMetadataEndpointState(*b.HttpEndpoint)
	}
	if b.HttpProtocolIpv6 != nil {
		out.HttpProtocolIpv6 = ec2types.InstanceMetadataProtocolState(*b.HttpProtocolIpv6)
	}
	if b.HttpTokens != nil {
		out.HttpTokens = ec2types.HttpTokensState(*b.HttpTokens)
	}
	if b.InstanceMetadataTags != nil {
		out.InstanceMetadataTags = ec2types.InstanceMetadataTagsState(*b.InstanceMetadataTags)
	}
	return out
}

// InstanceRootBlockDevice configures the instance's root EBS volume. The root
// device name is fixed by the AMI and reported as the root-device-name output,
// so it is not an input here. The size, type, IOPS, throughput,
// delete-on-termination flag, and tags are reconciled in place after create;
// encrypted and kms-key-id are fixed when the volume is created, so a change to
// either returns a clear error rather than silently replacing the instance's
// root volume. The tags apply to the root volume only and cannot combine with
// the instance-wide volume-tags map.
type InstanceRootBlockDevice struct {
	DeleteOnTermination *bool              `ub:"delete-on-termination"`
	Encrypted           *bool              `ub:"encrypted"`
	Iops                *int64             `ub:"iops"`
	KmsKeyId            *string            `ub:"kms-key-id"`
	Tags                *map[string]string `ub:"tags"`
	Throughput          *int64             `ub:"throughput"`
	VolumeSize          *int64             `ub:"volume-size"`
	VolumeType          *string            `ub:"volume-type"`
}

// rootBlockDeviceMapping builds the run-instances block device mapping that
// applies the root volume settings, targeting the AMI's root device name. An
// omitted delete-on-termination is sent as an explicit true, the declared
// default, rather than left to the AMI mapping.
func rootBlockDeviceMapping(
	b *InstanceRootBlockDevice, rootDeviceName string,
) ec2types.BlockDeviceMapping {
	ebs := &ec2types.EbsBlockDevice{
		DeleteOnTermination: deleteOnTerminationOrDefault(b.DeleteOnTermination),
		Encrypted:           b.Encrypted,
		Iops:                ptr.Int32(b.Iops),
		KmsKeyId:            b.KmsKeyId,
		Throughput:          ptr.Int32(b.Throughput),
		VolumeSize:          ptr.Int32(b.VolumeSize),
	}
	if b.VolumeType != nil {
		ebs.VolumeType = ec2types.VolumeType(*b.VolumeType)
	}
	return ec2types.BlockDeviceMapping{
		DeviceName: aws.String(rootDeviceName),
		Ebs:        ebs,
	}
}

// InstanceEbsBlockDevice attaches an additional EBS volume at a device name.
// Every field is fixed when the instance is created, so a change to the
// ebs-block-device list replaces the instance.
type InstanceEbsBlockDevice struct {
	DeviceName          string  `ub:"device-name"`
	DeleteOnTermination *bool   `ub:"delete-on-termination"`
	Encrypted           *bool   `ub:"encrypted"`
	Iops                *int64  `ub:"iops"`
	KmsKeyId            *string `ub:"kms-key-id"`
	SnapshotId          *string `ub:"snapshot-id"`
	Throughput          *int64  `ub:"throughput"`
	VolumeSize          *int64  `ub:"volume-size"`
	VolumeType          *string `ub:"volume-type"`
}

// ebsBlockDeviceMappings converts the additional EBS volumes to run-instances
// block device mappings. An omitted delete-on-termination is sent as an
// explicit true, the declared default, so an added data volume is cleaned up
// when the instance terminates.
func ebsBlockDeviceMappings(blocks []InstanceEbsBlockDevice) []ec2types.BlockDeviceMapping {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]ec2types.BlockDeviceMapping, 0, len(blocks))
	for i := range blocks {
		ebs := &ec2types.EbsBlockDevice{
			DeleteOnTermination: deleteOnTerminationOrDefault(blocks[i].DeleteOnTermination),
			Encrypted:           blocks[i].Encrypted,
			Iops:                ptr.Int32(blocks[i].Iops),
			KmsKeyId:            blocks[i].KmsKeyId,
			SnapshotId:          blocks[i].SnapshotId,
			Throughput:          ptr.Int32(blocks[i].Throughput),
			VolumeSize:          ptr.Int32(blocks[i].VolumeSize),
		}
		if blocks[i].VolumeType != nil {
			ebs.VolumeType = ec2types.VolumeType(*blocks[i].VolumeType)
		}
		out = append(out, ec2types.BlockDeviceMapping{
			DeviceName: aws.String(blocks[i].DeviceName),
			Ebs:        ebs,
		})
	}
	return out
}

// deleteOnTerminationOrDefault returns the configured flag, or the declared
// default of true when the field is omitted.
func deleteOnTerminationOrDefault(v *bool) *bool {
	if v == nil {
		return aws.Bool(true)
	}
	return v
}

// InstanceEphemeralBlockDevice attaches an instance-store volume at a device
// name, or suppresses a device the AMI maps. Instance-store volumes do not
// appear in a describe, so the list is write-only and Read leaves it untouched.
// A change to the list replaces the instance.
type InstanceEphemeralBlockDevice struct {
	DeviceName  string  `ub:"device-name"`
	NoDevice    *bool   `ub:"no-device"`
	VirtualName *string `ub:"virtual-name"`
}

// ephemeralBlockDeviceMappings converts the instance-store volumes to
// run-instances block device mappings. A device with no-device set is mapped to
// the empty-string NoDevice marker that suppresses it; otherwise the virtual
// name names the instance-store volume.
func ephemeralBlockDeviceMappings(
	blocks []InstanceEphemeralBlockDevice,
) []ec2types.BlockDeviceMapping {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]ec2types.BlockDeviceMapping, 0, len(blocks))
	for i := range blocks {
		mapping := ec2types.BlockDeviceMapping{DeviceName: aws.String(blocks[i].DeviceName)}
		if aws.ToBool(blocks[i].NoDevice) {
			mapping.NoDevice = aws.String("")
		} else {
			mapping.VirtualName = blocks[i].VirtualName
		}
		out = append(out, mapping)
	}
	return out
}

// InstanceLaunchTemplate references a launch template to seed the instance
// configuration, by id or by name, optionally pinning a version. The whole block
// is fixed at create: a change to it replaces the instance, since the running
// version is fixed at launch. The $Default and $Latest auto-tracking semantics
// are not reproduced; a literal version change replaces the instance.
type InstanceLaunchTemplate struct {
	Id      *string `ub:"id"`
	Name    *string `ub:"name"`
	Version *string `ub:"version"`
}

// launchTemplateSpecification converts the launch-template reference to its SDK
// request type. A nil block returns nil.
func launchTemplateSpecification(
	b *InstanceLaunchTemplate,
) *ec2types.LaunchTemplateSpecification {
	if b == nil {
		return nil
	}
	return &ec2types.LaunchTemplateSpecification{
		LaunchTemplateId:   b.Id,
		LaunchTemplateName: b.Name,
		Version:            b.Version,
	}
}
