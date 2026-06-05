package ec2

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// The blocks below model the structured members of RequestLaunchTemplateData,
// the instance configuration a launch template version holds. Each maps to an
// SDK request type and is assembled into the version request rather than written
// by its own call. A nil block leaves that member unset, so AWS applies its own
// default; an omittable collection is a pointer to a slice for the same reason.
// Because every update builds a complete new version from the declared inputs
// and submits it whole, a removed block is simply absent from the next version;
// there is no empty sentinel to clear it with. The enum and range rules on a
// single block's fields are declared in LaunchTemplate's Constraints; the rules
// on elements of the block-device-mappings and network-interfaces lists are
// validated by AWS, except the address count-or-list choice, which
// validateNetworkInterfaces checks when the version is built. The converters are
// free functions, each handling a nil block, so the long SDK request type names
// stay off the column-limited method-receiver line.

// LaunchTemplateData is the instance configuration of a launch template
// version, the RequestLaunchTemplateData the create and version calls accept.
// It is a required, single value-type block: a version must include at least
// one instance parameter. The kernel-id, ram-disk-id, secondary-interfaces, and
// instance-requirements members of the SDK type are deliberately not modeled.
type LaunchTemplateData struct {
	ImageId                           *string   `ub:"image-id"`
	InstanceType                      *string   `ub:"instance-type"`
	KeyName                           *string   `ub:"key-name"`
	UserData                          *string   `ub:"user-data"`
	EbsOptimized                      *bool     `ub:"ebs-optimized"`
	DisableApiStop                    *bool     `ub:"disable-api-stop"`
	DisableApiTermination             *bool     `ub:"disable-api-termination"`
	InstanceInitiatedShutdownBehavior *string   `ub:"instance-initiated-shutdown-behavior"`
	SecurityGroupIds                  *[]string `ub:"security-group-ids"`
	SecurityGroups                    *[]string `ub:"security-groups"`

	BlockDeviceMappings   *[]LaunchTemplateBlockDeviceMapping   `ub:"block-device-mappings"`
	NetworkInterfaces     *[]LaunchTemplateNetworkInterface     `ub:"network-interfaces"`
	IamInstanceProfile    *LaunchTemplateIamInstanceProfile     `ub:"iam-instance-profile"`
	Monitoring            *LaunchTemplateMonitoring             `ub:"monitoring"`
	MetadataOptions       *LaunchTemplateMetadataOptions        `ub:"metadata-options"`
	Placement             *LaunchTemplatePlacement              `ub:"placement"`
	TagSpecifications     *[]LaunchTemplateTagSpecification     `ub:"tag-specifications"`
	CreditSpecification   *LaunchTemplateCreditSpecification    `ub:"credit-specification"`
	CpuOptions            *LaunchTemplateCpuOptions             `ub:"cpu-options"`
	EnclaveOptions        *LaunchTemplateEnclaveOptions         `ub:"enclave-options"`
	HibernationOptions    *LaunchTemplateHibernationOptions     `ub:"hibernation-options"`
	PrivateDnsNameOptions *LaunchTemplatePrivateDnsNameOptions  `ub:"private-dns-name-options"`
	MaintenanceOptions    *LaunchTemplateMaintenanceOptions     `ub:"maintenance-options"`
	LicenseSpecifications *[]LaunchTemplateLicenseSpecification `ub:"license-specifications"`
	InstanceMarketOptions *LaunchTemplateInstanceMarketOptions  `ub:"instance-market-options"`

	CapacityReservationSpecification *LaunchTemplateCapacityReservation `ub:"capacity-reservation-specification"`

	NetworkPerformanceOptions *LaunchTemplateNetworkPerformanceOptions `ub:"network-performance-options"`
}

// fromList returns the slice a declared optional list holds, or nil when the
// list was not declared. An optional collection inside a block is a pointer so
// the type checker accepts a body that leaves it out; the SDK reads nil and an
// absent list the same way.
func fromList[T any](p *[]T) []T {
	if p == nil {
		return nil
	}
	return *p
}

// launchTemplateData converts the declared instance configuration to the SDK
// request type. UserData is always set, to the empty string when unset, so an
// otherwise-empty data block still produces a valid version request: EC2 rejects
// a version with no parameters at all. It returns an error when a spot-options
// valid-until timestamp is not a valid RFC3339 time or when a network interface
// declares both an address count and an address list.
func launchTemplateData(b LaunchTemplateData) (*ec2types.RequestLaunchTemplateData, error) {
	marketOptions, err := instanceMarketOptions(b.InstanceMarketOptions)
	if err != nil {
		return nil, err
	}
	if err := validateNetworkInterfaces(fromList(b.NetworkInterfaces)); err != nil {
		return nil, err
	}
	out := &ec2types.RequestLaunchTemplateData{
		ImageId:                          b.ImageId,
		KeyName:                          b.KeyName,
		UserData:                         aws.String(aws.ToString(b.UserData)),
		EbsOptimized:                     b.EbsOptimized,
		DisableApiStop:                   b.DisableApiStop,
		DisableApiTermination:            b.DisableApiTermination,
		SecurityGroupIds:                 fromList(b.SecurityGroupIds),
		SecurityGroups:                   fromList(b.SecurityGroups),
		BlockDeviceMappings:              blockDeviceMappings(fromList(b.BlockDeviceMappings)),
		NetworkInterfaces:                networkInterfaces(fromList(b.NetworkInterfaces)),
		IamInstanceProfile:               iamInstanceProfile(b.IamInstanceProfile),
		Monitoring:                       monitoring(b.Monitoring),
		MetadataOptions:                  metadataOptions(b.MetadataOptions),
		Placement:                        placement(b.Placement),
		TagSpecifications:                dataTagSpecifications(fromList(b.TagSpecifications)),
		CreditSpecification:              creditSpecification(b.CreditSpecification),
		CpuOptions:                       cpuOptions(b.CpuOptions),
		CapacityReservationSpecification: capacityReservation(b.CapacityReservationSpecification),
		EnclaveOptions:                   enclaveOptions(b.EnclaveOptions),
		HibernationOptions:               hibernationOptions(b.HibernationOptions),
		PrivateDnsNameOptions:            privateDnsNameOptions(b.PrivateDnsNameOptions),
		MaintenanceOptions:               maintenanceOptions(b.MaintenanceOptions),
		NetworkPerformanceOptions:        networkPerformanceOptions(b.NetworkPerformanceOptions),
		LicenseSpecifications:            licenseSpecifications(fromList(b.LicenseSpecifications)),
		InstanceMarketOptions:            marketOptions,
	}
	if b.InstanceType != nil {
		out.InstanceType = ec2types.InstanceType(*b.InstanceType)
	}
	if b.InstanceInitiatedShutdownBehavior != nil {
		out.InstanceInitiatedShutdownBehavior =
			ec2types.ShutdownBehavior(*b.InstanceInitiatedShutdownBehavior)
	}
	return out, nil
}

// LaunchTemplateBlockDeviceMapping attaches a volume at a device name. Exactly
// one of an EBS volume, a no-device marker, or an instance-store virtual name is
// given for a mapping.
type LaunchTemplateBlockDeviceMapping struct {
	DeviceName  *string                       `ub:"device-name"`
	NoDevice    *string                       `ub:"no-device"`
	VirtualName *string                       `ub:"virtual-name"`
	Ebs         *LaunchTemplateEbsBlockDevice `ub:"ebs"`
}

func blockDeviceMappings(
	blocks []LaunchTemplateBlockDeviceMapping,
) []ec2types.LaunchTemplateBlockDeviceMappingRequest {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]ec2types.LaunchTemplateBlockDeviceMappingRequest, 0, len(blocks))
	for i := range blocks {
		out = append(out, ec2types.LaunchTemplateBlockDeviceMappingRequest{
			DeviceName:  blocks[i].DeviceName,
			NoDevice:    blocks[i].NoDevice,
			VirtualName: blocks[i].VirtualName,
			Ebs:         ebsBlockDevice(blocks[i].Ebs),
		})
	}
	return out
}

// LaunchTemplateEbsBlockDevice configures the EBS volume of a block device
// mapping. The size, type, IOPS, throughput, encryption, snapshot, KMS key, and
// initialization rate are all optional; an unset field takes the EBS default.
type LaunchTemplateEbsBlockDevice struct {
	DeleteOnTermination      *bool   `ub:"delete-on-termination"`
	Encrypted                *bool   `ub:"encrypted"`
	Iops                     *int64  `ub:"iops"`
	KmsKeyId                 *string `ub:"kms-key-id"`
	SnapshotId               *string `ub:"snapshot-id"`
	Throughput               *int64  `ub:"throughput"`
	VolumeInitializationRate *int64  `ub:"volume-initialization-rate"`
	VolumeSize               *int64  `ub:"volume-size"`
	VolumeType               *string `ub:"volume-type"`
}

func ebsBlockDevice(b *LaunchTemplateEbsBlockDevice) *ec2types.LaunchTemplateEbsBlockDeviceRequest {
	if b == nil {
		return nil
	}
	out := &ec2types.LaunchTemplateEbsBlockDeviceRequest{
		DeleteOnTermination:      b.DeleteOnTermination,
		Encrypted:                b.Encrypted,
		Iops:                     ptr.Int32(b.Iops),
		KmsKeyId:                 b.KmsKeyId,
		SnapshotId:               b.SnapshotId,
		Throughput:               ptr.Int32(b.Throughput),
		VolumeInitializationRate: ptr.Int32(b.VolumeInitializationRate),
		VolumeSize:               ptr.Int32(b.VolumeSize),
	}
	if b.VolumeType != nil {
		out.VolumeType = ec2types.VolumeType(*b.VolumeType)
	}
	return out
}

// LaunchTemplateNetworkInterface is one network interface attached to instances
// launched from the template. A primary interface sits at device index 0. The
// secondary private IPv4 addresses are given either as an explicit list or as a
// count, and likewise the IPv6 addresses; the resource sends whichever is
// declared.
type LaunchTemplateNetworkInterface struct {
	AssociateCarrierIpAddress *bool     `ub:"associate-carrier-ip-address"`
	AssociatePublicIpAddress  *bool     `ub:"associate-public-ip-address"`
	DeleteOnTermination       *bool     `ub:"delete-on-termination"`
	Description               *string   `ub:"description"`
	DeviceIndex               *int64    `ub:"device-index"`
	InterfaceType             *string   `ub:"interface-type"`
	Ipv4PrefixCount           *int64    `ub:"ipv4-prefix-count"`
	Ipv4Prefixes              *[]string `ub:"ipv4-prefixes"`
	Ipv6AddressCount          *int64    `ub:"ipv6-address-count"`
	Ipv6Addresses             *[]string `ub:"ipv6-addresses"`
	Ipv6PrefixCount           *int64    `ub:"ipv6-prefix-count"`
	Ipv6Prefixes              *[]string `ub:"ipv6-prefixes"`
	NetworkCardIndex          *int64    `ub:"network-card-index"`
	NetworkInterfaceId        *string   `ub:"network-interface-id"`
	PrimaryIpv6               *bool     `ub:"primary-ipv6"`
	PrivateIpAddress          *string   `ub:"private-ip-address"`
	Ipv4Addresses             *[]string `ub:"ipv4-addresses"`
	Ipv4AddressCount          *int64    `ub:"ipv4-address-count"`
	SubnetId                  *string   `ub:"subnet-id"`
	Groups                    *[]string `ub:"groups"`

	EnaSrdSpecification             *LaunchTemplateEnaSrdSpecification `ub:"ena-srd-specification"`
	ConnectionTrackingSpecification *LaunchTemplateConnectionTracking  `ub:"connection-tracking-specification"`
}

func networkInterfaces(
	blocks []LaunchTemplateNetworkInterface,
) []ec2types.LaunchTemplateInstanceNetworkInterfaceSpecificationRequest {
	if len(blocks) == 0 {
		return nil
	}
	out := make(
		[]ec2types.LaunchTemplateInstanceNetworkInterfaceSpecificationRequest, 0, len(blocks),
	)
	for i := range blocks {
		out = append(out, networkInterface(blocks[i]))
	}
	return out
}

// validateNetworkInterfaces rejects a network interface that declares both an
// address count and an address list, for IPv4 or IPv6. Each pair is one choice;
// this rule lives here because a constraint cannot iterate an omittable list.
func validateNetworkInterfaces(blocks []LaunchTemplateNetworkInterface) error {
	for i := range blocks {
		if blocks[i].Ipv4AddressCount != nil && blocks[i].Ipv4Addresses != nil {
			return fmt.Errorf(
				"network interface %d declares both ipv4-address-count and ipv4-addresses", i)
		}
		if blocks[i].Ipv6AddressCount != nil && blocks[i].Ipv6Addresses != nil {
			return fmt.Errorf(
				"network interface %d declares both ipv6-address-count and ipv6-addresses", i)
		}
	}
	return nil
}

func networkInterface(
	b LaunchTemplateNetworkInterface,
) ec2types.LaunchTemplateInstanceNetworkInterfaceSpecificationRequest {
	// A declared network interface always sends a device index: the API
	// requires one for an interface-type NIC, and an omitted device-index
	// means the primary slot, index 0. Two declared interfaces that both omit
	// it collide on index 0 and the API rejects the pair visibly.
	deviceIndex := ptr.Int32(b.DeviceIndex)
	if deviceIndex == nil {
		deviceIndex = aws.Int32(0)
	}
	out := ec2types.LaunchTemplateInstanceNetworkInterfaceSpecificationRequest{
		AssociateCarrierIpAddress:       b.AssociateCarrierIpAddress,
		AssociatePublicIpAddress:        b.AssociatePublicIpAddress,
		DeleteOnTermination:             b.DeleteOnTermination,
		Description:                     b.Description,
		DeviceIndex:                     deviceIndex,
		InterfaceType:                   b.InterfaceType,
		Ipv4PrefixCount:                 ptr.Int32(b.Ipv4PrefixCount),
		Ipv4Prefixes:                    ipv4Prefixes(fromList(b.Ipv4Prefixes)),
		Ipv6PrefixCount:                 ptr.Int32(b.Ipv6PrefixCount),
		Ipv6Prefixes:                    ipv6Prefixes(fromList(b.Ipv6Prefixes)),
		NetworkCardIndex:                ptr.Int32(b.NetworkCardIndex),
		NetworkInterfaceId:              b.NetworkInterfaceId,
		PrimaryIpv6:                     b.PrimaryIpv6,
		PrivateIpAddress:                b.PrivateIpAddress,
		SubnetId:                        b.SubnetId,
		Groups:                          fromList(b.Groups),
		EnaSrdSpecification:             enaSrdSpecification(b.EnaSrdSpecification),
		ConnectionTrackingSpecification: connectionTracking(b.ConnectionTrackingSpecification),
	}
	// The secondary private IPv4 addresses come from a count or an explicit
	// list, never both: a count goes to SecondaryPrivateIpAddressCount, else the
	// list expands to PrivateIpAddresses with the entry matching
	// private-ip-address flagged primary. A pointer constraint forbids declaring
	// both.
	if b.Ipv4AddressCount != nil {
		out.SecondaryPrivateIpAddressCount = ptr.Int32(b.Ipv4AddressCount)
	} else if addrs := fromList(b.Ipv4Addresses); len(addrs) > 0 {
		out.PrivateIpAddresses = privateIpAddresses(addrs, b.PrivateIpAddress)
	}
	// The IPv6 addresses likewise come from a count or an explicit list.
	if b.Ipv6AddressCount != nil {
		out.Ipv6AddressCount = ptr.Int32(b.Ipv6AddressCount)
	} else if addrs := fromList(b.Ipv6Addresses); len(addrs) > 0 {
		out.Ipv6Addresses = ipv6Addresses(addrs)
	}
	return out
}

func ipv4Prefixes(prefixes []string) []ec2types.Ipv4PrefixSpecificationRequest {
	if len(prefixes) == 0 {
		return nil
	}
	out := make([]ec2types.Ipv4PrefixSpecificationRequest, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, ec2types.Ipv4PrefixSpecificationRequest{Ipv4Prefix: aws.String(p)})
	}
	return out
}

func ipv6Prefixes(prefixes []string) []ec2types.Ipv6PrefixSpecificationRequest {
	if len(prefixes) == 0 {
		return nil
	}
	out := make([]ec2types.Ipv6PrefixSpecificationRequest, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, ec2types.Ipv6PrefixSpecificationRequest{Ipv6Prefix: aws.String(p)})
	}
	return out
}

func ipv6Addresses(addrs []string) []ec2types.InstanceIpv6AddressRequest {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]ec2types.InstanceIpv6AddressRequest, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, ec2types.InstanceIpv6AddressRequest{Ipv6Address: aws.String(a)})
	}
	return out
}

// privateIpAddresses expands explicit secondary IPv4 addresses. The address
// that equals the interface's primary private address is flagged primary, the
// way AWS expects a primary to be designated within the list.
func privateIpAddresses(addrs []string, primary *string) []ec2types.PrivateIpAddressSpecification {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]ec2types.PrivateIpAddressSpecification, 0, len(addrs))
	for _, a := range addrs {
		spec := ec2types.PrivateIpAddressSpecification{PrivateIpAddress: aws.String(a)}
		if primary != nil && a == *primary {
			spec.Primary = aws.Bool(true)
		}
		out = append(out, spec)
	}
	return out
}

// LaunchTemplateEnaSrdSpecification turns on ENA Express for the interface and,
// when enabled, may extend it to UDP traffic.
type LaunchTemplateEnaSrdSpecification struct {
	EnaSrdEnabled          *bool                                 `ub:"ena-srd-enabled"`
	EnaSrdUdpSpecification *LaunchTemplateEnaSrdUdpSpecification `ub:"ena-srd-udp-specification"`
}

func enaSrdSpecification(
	b *LaunchTemplateEnaSrdSpecification,
) *ec2types.EnaSrdSpecificationRequest {
	if b == nil {
		return nil
	}
	return &ec2types.EnaSrdSpecificationRequest{
		EnaSrdEnabled:          b.EnaSrdEnabled,
		EnaSrdUdpSpecification: enaSrdUdpSpecification(b.EnaSrdUdpSpecification),
	}
}

// LaunchTemplateEnaSrdUdpSpecification controls whether UDP traffic uses ENA
// Express.
type LaunchTemplateEnaSrdUdpSpecification struct {
	EnaSrdUdpEnabled *bool `ub:"ena-srd-udp-enabled"`
}

func enaSrdUdpSpecification(
	b *LaunchTemplateEnaSrdUdpSpecification,
) *ec2types.EnaSrdUdpSpecificationRequest {
	if b == nil {
		return nil
	}
	return &ec2types.EnaSrdUdpSpecificationRequest{EnaSrdUdpEnabled: b.EnaSrdUdpEnabled}
}

// LaunchTemplateConnectionTracking sets the idle connection-tracking timeouts
// for the interface, in seconds.
type LaunchTemplateConnectionTracking struct {
	TcpEstablishedTimeout *int64 `ub:"tcp-established-timeout"`
	UdpStreamTimeout      *int64 `ub:"udp-stream-timeout"`
	UdpTimeout            *int64 `ub:"udp-timeout"`
}

func connectionTracking(
	b *LaunchTemplateConnectionTracking,
) *ec2types.ConnectionTrackingSpecificationRequest {
	if b == nil {
		return nil
	}
	return &ec2types.ConnectionTrackingSpecificationRequest{
		TcpEstablishedTimeout: ptr.Int32(b.TcpEstablishedTimeout),
		UdpStreamTimeout:      ptr.Int32(b.UdpStreamTimeout),
		UdpTimeout:            ptr.Int32(b.UdpTimeout),
	}
}

// LaunchTemplateIamInstanceProfile names the IAM instance profile to attach,
// by ARN or by name.
type LaunchTemplateIamInstanceProfile struct {
	Arn  *string `ub:"arn"`
	Name *string `ub:"name"`
}

func iamInstanceProfile(
	b *LaunchTemplateIamInstanceProfile,
) *ec2types.LaunchTemplateIamInstanceProfileSpecificationRequest {
	if b == nil {
		return nil
	}
	return &ec2types.LaunchTemplateIamInstanceProfileSpecificationRequest{
		Arn:  b.Arn,
		Name: b.Name,
	}
}

// LaunchTemplateMonitoring turns on detailed CloudWatch monitoring for the
// instance.
type LaunchTemplateMonitoring struct {
	Enabled *bool `ub:"enabled"`
}

func monitoring(b *LaunchTemplateMonitoring) *ec2types.LaunchTemplatesMonitoringRequest {
	if b == nil {
		return nil
	}
	return &ec2types.LaunchTemplatesMonitoringRequest{Enabled: b.Enabled}
}

// LaunchTemplateMetadataOptions configures the instance metadata service: the
// HTTP endpoint and token policy, the PUT response hop limit, IPv6 support, and
// whether instance tags are reachable through metadata.
type LaunchTemplateMetadataOptions struct {
	HttpEndpoint            *string `ub:"http-endpoint"`
	HttpProtocolIpv6        *string `ub:"http-protocol-ipv6"`
	HttpPutResponseHopLimit *int64  `ub:"http-put-response-hop-limit"`
	HttpTokens              *string `ub:"http-tokens"`
	InstanceMetadataTags    *string `ub:"instance-metadata-tags"`
}

func metadataOptions(
	b *LaunchTemplateMetadataOptions,
) *ec2types.LaunchTemplateInstanceMetadataOptionsRequest {
	if b == nil {
		return nil
	}
	out := &ec2types.LaunchTemplateInstanceMetadataOptionsRequest{
		HttpPutResponseHopLimit: ptr.Int32(b.HttpPutResponseHopLimit),
	}
	if b.HttpEndpoint != nil {
		out.HttpEndpoint =
			ec2types.LaunchTemplateInstanceMetadataEndpointState(*b.HttpEndpoint)
	}
	if b.HttpProtocolIpv6 != nil {
		out.HttpProtocolIpv6 =
			ec2types.LaunchTemplateInstanceMetadataProtocolIpv6(*b.HttpProtocolIpv6)
	}
	if b.HttpTokens != nil {
		out.HttpTokens = ec2types.LaunchTemplateHttpTokensState(*b.HttpTokens)
	}
	if b.InstanceMetadataTags != nil {
		out.InstanceMetadataTags =
			ec2types.LaunchTemplateInstanceMetadataTagsState(*b.InstanceMetadataTags)
	}
	return out
}

// LaunchTemplatePlacement controls where the instance runs: its Availability
// Zone, placement group, tenancy, Dedicated Host, affinity, and partition.
type LaunchTemplatePlacement struct {
	Affinity             *string `ub:"affinity"`
	AvailabilityZone     *string `ub:"availability-zone"`
	AvailabilityZoneId   *string `ub:"availability-zone-id"`
	GroupId              *string `ub:"group-id"`
	GroupName            *string `ub:"group-name"`
	HostId               *string `ub:"host-id"`
	HostResourceGroupArn *string `ub:"host-resource-group-arn"`
	PartitionNumber      *int64  `ub:"partition-number"`
	SpreadDomain         *string `ub:"spread-domain"`
	Tenancy              *string `ub:"tenancy"`
}

func placement(b *LaunchTemplatePlacement) *ec2types.LaunchTemplatePlacementRequest {
	if b == nil {
		return nil
	}
	out := &ec2types.LaunchTemplatePlacementRequest{
		Affinity:             b.Affinity,
		AvailabilityZone:     b.AvailabilityZone,
		AvailabilityZoneId:   b.AvailabilityZoneId,
		GroupId:              b.GroupId,
		GroupName:            b.GroupName,
		HostId:               b.HostId,
		HostResourceGroupArn: b.HostResourceGroupArn,
		PartitionNumber:      ptr.Int32(b.PartitionNumber),
		SpreadDomain:         b.SpreadDomain,
	}
	if b.Tenancy != nil {
		out.Tenancy = ec2types.Tenancy(*b.Tenancy)
	}
	return out
}

// LaunchTemplateTagSpecification applies tags to a resource type created during
// instance launch, such as the instance or its volumes. These are distinct from
// the template's own tags.
type LaunchTemplateTagSpecification struct {
	ResourceType *string           `ub:"resource-type"`
	Tags         map[string]string `ub:"tags"`
}

func dataTagSpecifications(
	blocks []LaunchTemplateTagSpecification,
) []ec2types.LaunchTemplateTagSpecificationRequest {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]ec2types.LaunchTemplateTagSpecificationRequest, 0, len(blocks))
	for i := range blocks {
		spec := ec2types.LaunchTemplateTagSpecificationRequest{Tags: mapToTags(blocks[i].Tags)}
		if blocks[i].ResourceType != nil {
			spec.ResourceType = ec2types.ResourceType(*blocks[i].ResourceType)
		}
		out = append(out, spec)
	}
	return out
}

// LaunchTemplateCreditSpecification sets the CPU credit option for a burstable
// (T-family) instance. It is sent whenever declared and AWS validates it against
// the instance type; this library does not silently withhold a declared credit
// specification by first checking whether the instance type is
// burstable-performance-capable.
type LaunchTemplateCreditSpecification struct {
	CpuCredits *string `ub:"cpu-credits"`
}

func creditSpecification(
	b *LaunchTemplateCreditSpecification,
) *ec2types.CreditSpecificationRequest {
	if b == nil {
		return nil
	}
	return &ec2types.CreditSpecificationRequest{CpuCredits: b.CpuCredits}
}

// LaunchTemplateCpuOptions overrides the instance's CPU layout: its core count,
// threads per core, AMD SEV-SNP setting, and nested-virtualization setting.
type LaunchTemplateCpuOptions struct {
	AmdSevSnp            *string `ub:"amd-sev-snp"`
	CoreCount            *int64  `ub:"core-count"`
	NestedVirtualization *string `ub:"nested-virtualization"`
	ThreadsPerCore       *int64  `ub:"threads-per-core"`
}

func cpuOptions(b *LaunchTemplateCpuOptions) *ec2types.LaunchTemplateCpuOptionsRequest {
	if b == nil {
		return nil
	}
	out := &ec2types.LaunchTemplateCpuOptionsRequest{
		CoreCount:      ptr.Int32(b.CoreCount),
		ThreadsPerCore: ptr.Int32(b.ThreadsPerCore),
	}
	if b.AmdSevSnp != nil {
		out.AmdSevSnp = ec2types.AmdSevSnpSpecification(*b.AmdSevSnp)
	}
	if b.NestedVirtualization != nil {
		out.NestedVirtualization =
			ec2types.NestedVirtualizationSpecification(*b.NestedVirtualization)
	}
	return out
}

// LaunchTemplateCapacityReservation directs how the instance uses Capacity
// Reservations: a preference, or an explicit target reservation or group.
type LaunchTemplateCapacityReservation struct {
	CapacityReservationPreference *string `ub:"capacity-reservation-preference"`

	CapacityReservationTarget *LaunchTemplateCapacityReservationTarget `ub:"capacity-reservation-target"`
}

func capacityReservation(
	b *LaunchTemplateCapacityReservation,
) *ec2types.LaunchTemplateCapacityReservationSpecificationRequest {
	if b == nil {
		return nil
	}
	out := &ec2types.LaunchTemplateCapacityReservationSpecificationRequest{
		CapacityReservationTarget: capacityReservationTarget(b.CapacityReservationTarget),
	}
	if b.CapacityReservationPreference != nil {
		out.CapacityReservationPreference =
			ec2types.CapacityReservationPreference(*b.CapacityReservationPreference)
	}
	return out
}

// LaunchTemplateCapacityReservationTarget names the Capacity Reservation to run
// in, by reservation id or by resource group ARN.
type LaunchTemplateCapacityReservationTarget struct {
	CapacityReservationId *string `ub:"capacity-reservation-id"`

	CapacityReservationResourceGroupArn *string `ub:"capacity-reservation-resource-group-arn"`
}

func capacityReservationTarget(
	b *LaunchTemplateCapacityReservationTarget,
) *ec2types.CapacityReservationTarget {
	if b == nil {
		return nil
	}
	return &ec2types.CapacityReservationTarget{
		CapacityReservationId:               b.CapacityReservationId,
		CapacityReservationResourceGroupArn: b.CapacityReservationResourceGroupArn,
	}
}

// LaunchTemplateEnclaveOptions turns on AWS Nitro Enclaves for the instance.
type LaunchTemplateEnclaveOptions struct {
	Enabled *bool `ub:"enabled"`
}

func enclaveOptions(b *LaunchTemplateEnclaveOptions) *ec2types.LaunchTemplateEnclaveOptionsRequest {
	if b == nil {
		return nil
	}
	return &ec2types.LaunchTemplateEnclaveOptionsRequest{Enabled: b.Enabled}
}

// LaunchTemplateHibernationOptions configures the instance for hibernation.
type LaunchTemplateHibernationOptions struct {
	Configured *bool `ub:"configured"`
}

func hibernationOptions(
	b *LaunchTemplateHibernationOptions,
) *ec2types.LaunchTemplateHibernationOptionsRequest {
	if b == nil {
		return nil
	}
	return &ec2types.LaunchTemplateHibernationOptionsRequest{Configured: b.Configured}
}

// LaunchTemplatePrivateDnsNameOptions sets the instance hostname type and which
// DNS record queries it answers.
type LaunchTemplatePrivateDnsNameOptions struct {
	EnableResourceNameDnsAAAARecord *bool   `ub:"enable-resource-name-dns-aaaa-record"`
	EnableResourceNameDnsARecord    *bool   `ub:"enable-resource-name-dns-a-record"`
	HostnameType                    *string `ub:"hostname-type"`
}

func privateDnsNameOptions(
	b *LaunchTemplatePrivateDnsNameOptions,
) *ec2types.LaunchTemplatePrivateDnsNameOptionsRequest {
	if b == nil {
		return nil
	}
	out := &ec2types.LaunchTemplatePrivateDnsNameOptionsRequest{
		EnableResourceNameDnsAAAARecord: b.EnableResourceNameDnsAAAARecord,
		EnableResourceNameDnsARecord:    b.EnableResourceNameDnsARecord,
	}
	if b.HostnameType != nil {
		out.HostnameType = ec2types.HostnameType(*b.HostnameType)
	}
	return out
}

// LaunchTemplateMaintenanceOptions sets the instance's automatic-recovery
// behavior.
type LaunchTemplateMaintenanceOptions struct {
	AutoRecovery *string `ub:"auto-recovery"`
}

func maintenanceOptions(
	b *LaunchTemplateMaintenanceOptions,
) *ec2types.LaunchTemplateInstanceMaintenanceOptionsRequest {
	if b == nil {
		return nil
	}
	out := &ec2types.LaunchTemplateInstanceMaintenanceOptionsRequest{}
	if b.AutoRecovery != nil {
		out.AutoRecovery = ec2types.LaunchTemplateAutoRecoveryState(*b.AutoRecovery)
	}
	return out
}

// LaunchTemplateNetworkPerformanceOptions picks the bandwidth weighting that
// biases the instance toward networking or EBS throughput.
type LaunchTemplateNetworkPerformanceOptions struct {
	BandwidthWeighting *string `ub:"bandwidth-weighting"`
}

func networkPerformanceOptions(
	b *LaunchTemplateNetworkPerformanceOptions,
) *ec2types.LaunchTemplateNetworkPerformanceOptionsRequest {
	if b == nil {
		return nil
	}
	out := &ec2types.LaunchTemplateNetworkPerformanceOptionsRequest{}
	if b.BandwidthWeighting != nil {
		out.BandwidthWeighting = ec2types.InstanceBandwidthWeighting(*b.BandwidthWeighting)
	}
	return out
}

// LaunchTemplateLicenseSpecification attaches a License Manager configuration to
// instances launched from the template.
type LaunchTemplateLicenseSpecification struct {
	LicenseConfigurationArn *string `ub:"license-configuration-arn"`
}

func licenseSpecifications(
	blocks []LaunchTemplateLicenseSpecification,
) []ec2types.LaunchTemplateLicenseConfigurationRequest {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]ec2types.LaunchTemplateLicenseConfigurationRequest, 0, len(blocks))
	for i := range blocks {
		out = append(out, ec2types.LaunchTemplateLicenseConfigurationRequest{
			LicenseConfigurationArn: blocks[i].LicenseConfigurationArn,
		})
	}
	return out
}

// LaunchTemplateInstanceMarketOptions sets the purchasing model for the
// instance: the market type and, for Spot, the Spot options.
type LaunchTemplateInstanceMarketOptions struct {
	MarketType  *string                          `ub:"market-type"`
	SpotOptions *LaunchTemplateSpotMarketOptions `ub:"spot-options"`
}

func instanceMarketOptions(
	b *LaunchTemplateInstanceMarketOptions,
) (*ec2types.LaunchTemplateInstanceMarketOptionsRequest, error) {
	if b == nil {
		return nil, nil
	}
	spotOptions, err := spotMarketOptions(b.SpotOptions)
	if err != nil {
		return nil, err
	}
	out := &ec2types.LaunchTemplateInstanceMarketOptionsRequest{SpotOptions: spotOptions}
	if b.MarketType != nil {
		out.MarketType = ec2types.MarketType(*b.MarketType)
	}
	return out, nil
}

// LaunchTemplateSpotMarketOptions configures a Spot instance request: the
// request type, interruption behavior, maximum price, legacy block duration, and
// expiry. ValidUntil is an RFC3339 timestamp.
type LaunchTemplateSpotMarketOptions struct {
	BlockDurationMinutes         *int64  `ub:"block-duration-minutes"`
	InstanceInterruptionBehavior *string `ub:"instance-interruption-behavior"`
	MaxPrice                     *string `ub:"max-price"`
	SpotInstanceType             *string `ub:"spot-instance-type"`
	ValidUntil                   *string `ub:"valid-until"`
}

func spotMarketOptions(
	b *LaunchTemplateSpotMarketOptions,
) (*ec2types.LaunchTemplateSpotMarketOptionsRequest, error) {
	if b == nil {
		return nil, nil
	}
	out := &ec2types.LaunchTemplateSpotMarketOptionsRequest{
		BlockDurationMinutes: ptr.Int32(b.BlockDurationMinutes),
		MaxPrice:             b.MaxPrice,
	}
	if b.InstanceInterruptionBehavior != nil {
		out.InstanceInterruptionBehavior =
			ec2types.InstanceInterruptionBehavior(*b.InstanceInterruptionBehavior)
	}
	if b.SpotInstanceType != nil {
		out.SpotInstanceType = ec2types.SpotInstanceType(*b.SpotInstanceType)
	}
	if b.ValidUntil != nil {
		t, err := time.Parse(time.RFC3339, *b.ValidUntil)
		if err != nil {
			return nil, fmt.Errorf("parse spot-options valid-until %q: %w", *b.ValidUntil, err)
		}
		out.ValidUntil = aws.Time(t)
	}
	return out, nil
}
