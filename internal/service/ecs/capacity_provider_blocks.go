package ecs

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

type (
	ecsASGProviderUpdate                 = ecstypes.AutoScalingGroupProviderUpdate
	ecsCreateMIProviderConfiguration     = ecstypes.CreateManagedInstancesProviderConfiguration
	ecsUpdateMIProviderConfiguration     = ecstypes.UpdateManagedInstancesProviderConfiguration
	ecsInstanceLaunchTemplateUpdate      = ecstypes.InstanceLaunchTemplateUpdate
	ecsMINetworkConfiguration            = ecstypes.ManagedInstancesNetworkConfiguration
	ecsMILocalStorage                    = ecstypes.ManagedInstancesLocalStorageConfiguration
	ecsMIStorageConfiguration            = ecstypes.ManagedInstancesStorageConfiguration
	ecsAccelTotalMemory                  = ecstypes.AcceleratorTotalMemoryMiBRequest
	ecsBaselineEBSBandwidth              = ecstypes.BaselineEbsBandwidthMbpsRequest
	ecsNetworkInterfaceCountRangeRequest = ecstypes.NetworkInterfaceCountRequest
)

// CapacityProviderAutoScalingGroupProvider is the Auto Scaling group backing
// a capacity provider. The group ARN is fixed at create time; the draining,
// scaling, and termination-protection settings are mutable and are reconciled
// through UpdateCapacityProvider.
type CapacityProviderAutoScalingGroupProvider struct {
	AutoScalingGroupArn          string                          `ub:"auto-scaling-group-arn"`
	ManagedDraining              *string                         `ub:"managed-draining"`
	ManagedScaling               *CapacityProviderManagedScaling `ub:"managed-scaling"`
	ManagedTerminationProtection *string                         `ub:"managed-termination-protection"`
}

// CapacityProviderManagedScaling controls the ECS-managed target tracking
// policy for an Auto Scaling group capacity provider. InstanceWarmupPeriod is
// sent when set, even when it is zero; the other numeric fields are sent only
// when greater than zero, matching the ECS API's defaulting behavior.
type CapacityProviderManagedScaling struct {
	InstanceWarmupPeriod   *int64  `ub:"instance-warmup-period"`
	MaximumScalingStepSize *int64  `ub:"maximum-scaling-step-size"`
	MinimumScalingStepSize *int64  `ub:"minimum-scaling-step-size"`
	Status                 *string `ub:"status"`
	TargetCapacity         *int64  `ub:"target-capacity"`
}

// CapacityProviderManagedInstancesProvider is the Amazon ECS Managed
// Instances backing for a cluster-scoped capacity provider. The cluster input
// on the parent resource is required with this block.
type CapacityProviderManagedInstancesProvider struct {
	AutoRepairConfiguration    *CapacityProviderAutoRepairConfiguration    `ub:"auto-repair-configuration"`
	InfrastructureOptimization *CapacityProviderInfrastructureOptimization `ub:"infrastructure-optimization"`
	InfrastructureRoleArn      string                                      `ub:"infrastructure-role-arn"`
	InstanceLaunchTemplate     CapacityProviderInstanceLaunchTemplate      `ub:"instance-launch-template"`
	PropagateTags              *string                                     `ub:"propagate-tags"`
}

// CapacityProviderAutoRepairConfiguration toggles automatic replacement of
// impaired Amazon ECS Managed Instances.
type CapacityProviderAutoRepairConfiguration struct {
	ActionsStatus *string `ub:"actions-status"`
}

// CapacityProviderInfrastructureOptimization controls the delay before ECS
// optimizes idle or underutilized managed instances. ScaleInAfter accepts -1
// to disable optimization and is sent when set, even for -1 or zero.
type CapacityProviderInfrastructureOptimization struct {
	ScaleInAfter *int64 `ub:"scale-in-after"`
}

// CapacityProviderInstanceLaunchTemplate is the launch template configuration
// that ECS uses for managed instances. CapacityOptionType is create-only and
// is omitted from UpdateCapacityProvider.
type CapacityProviderInstanceLaunchTemplate struct {
	CapacityOptionType              *string                                                    `ub:"capacity-option-type"`
	CapacityReservations            *CapacityProviderCapacityReservationRequest                `ub:"capacity-reservations"`
	Ec2InstanceProfileArn           string                                                     `ub:"ec2-instance-profile-arn"`
	FipsEnabled                     *bool                                                      `ub:"fips-enabled"`
	InstanceMetadataTagsPropagation *bool                                                      `ub:"instance-metadata-tags-propagation"`
	InstanceRequirements            *CapacityProviderInstanceRequirementsRequest               `ub:"instance-requirements"`
	LocalStorageConfiguration       *CapacityProviderManagedInstancesLocalStorageConfiguration `ub:"local-storage-configuration"`
	Monitoring                      *string                                                    `ub:"monitoring"`
	NetworkConfiguration            CapacityProviderManagedInstancesNetworkConfiguration       `ub:"network-configuration"`
	StorageConfiguration            *CapacityProviderManagedInstancesStorageConfiguration      `ub:"storage-configuration"`
}

// CapacityProviderCapacityReservationRequest configures the capacity
// reservations used by a RESERVED managed instances capacity provider.
type CapacityProviderCapacityReservationRequest struct {
	ReservationGroupArn   *string `ub:"reservation-group-arn"`
	ReservationPreference *string `ub:"reservation-preference"`
}

// CapacityProviderManagedInstancesNetworkConfiguration names the subnets and
// optional security groups used for managed instances.
type CapacityProviderManagedInstancesNetworkConfiguration struct {
	SecurityGroups []string `ub:"security-groups"`
	Subnets        []string `ub:"subnets"`
}

// CapacityProviderManagedInstancesLocalStorageConfiguration controls whether
// ECS uses instance store volumes when available.
type CapacityProviderManagedInstancesLocalStorageConfiguration struct {
	UseLocalStorage bool `ub:"use-local-storage"`
}

// CapacityProviderManagedInstancesStorageConfiguration configures the managed
// data volume size for Amazon ECS Managed Instances.
type CapacityProviderManagedInstancesStorageConfiguration struct {
	StorageSizeGiB int64 `ub:"storage-size-gib"`
}

// CapacityProviderInstanceRequirementsRequest describes the EC2 instance
// types ECS may select for managed instances. The allowed-instance-types and
// excluded-instance-types entries are also checked in validate for the API's
// character pattern.
type CapacityProviderInstanceRequirementsRequest struct {
	MemoryMiB                                      CapacityProviderMemoryMiBRequest                  `ub:"memory-mib"`
	VCpuCount                                      CapacityProviderVCpuCountRangeRequest             `ub:"vcpu-count"`
	AcceleratorCount                               *CapacityProviderAcceleratorCountRequest          `ub:"accelerator-count"`
	AcceleratorManufacturers                       []string                                          `ub:"accelerator-manufacturers"`
	AcceleratorNames                               []string                                          `ub:"accelerator-names"`
	AcceleratorTotalMemoryMiB                      *CapacityProviderAcceleratorTotalMemoryMiBRequest `ub:"accelerator-total-memory-mib"`
	AcceleratorTypes                               []string                                          `ub:"accelerator-types"`
	AllowedInstanceTypes                           []string                                          `ub:"allowed-instance-types"`
	BareMetal                                      *string                                           `ub:"bare-metal"`
	BaselineEbsBandwidthMbps                       *CapacityProviderBaselineEbsBandwidthMbpsRequest  `ub:"baseline-ebs-bandwidth-mbps"`
	BurstablePerformance                           *string                                           `ub:"burstable-performance"`
	CpuManufacturers                               []string                                          `ub:"cpu-manufacturers"`
	ExcludedInstanceTypes                          []string                                          `ub:"excluded-instance-types"`
	InstanceGenerations                            []string                                          `ub:"instance-generations"`
	LocalStorage                                   *string                                           `ub:"local-storage"`
	LocalStorageTypes                              []string                                          `ub:"local-storage-types"`
	MaxSpotPriceAsPercentageOfOptimalOnDemandPrice *int64                                            `ub:"max-spot-price-as-percentage-of-optimal-on-demand-price"`
	MemoryGiBPerVCpu                               *CapacityProviderMemoryGiBPerVCpuRequest          `ub:"memory-gib-per-vcpu"`
	NetworkBandwidthGbps                           *CapacityProviderNetworkBandwidthGbpsRequest      `ub:"network-bandwidth-gbps"`
	NetworkInterfaceCount                          *CapacityProviderNetworkInterfaceCountRequest     `ub:"network-interface-count"`
	OnDemandMaxPricePercentageOverLowestPrice      *int64                                            `ub:"on-demand-max-price-percentage-over-lowest-price"`
	RequireHibernateSupport                        *bool                                             `ub:"require-hibernate-support"`
	SpotMaxPricePercentageOverLowestPrice          *int64                                            `ub:"spot-max-price-percentage-over-lowest-price"`
	TotalLocalStorageGB                            *CapacityProviderTotalLocalStorageGBRequest       `ub:"total-local-storage-gb"`
}

// CapacityProviderMemoryMiBRequest is a memory range in MiB.
type CapacityProviderMemoryMiBRequest struct {
	Min int64  `ub:"min"`
	Max *int64 `ub:"max"`
}

// CapacityProviderVCpuCountRangeRequest is a vCPU count range.
type CapacityProviderVCpuCountRangeRequest struct {
	Min int64  `ub:"min"`
	Max *int64 `ub:"max"`
}

// CapacityProviderAcceleratorCountRequest is an accelerator count range.
type CapacityProviderAcceleratorCountRequest struct {
	Max *int64 `ub:"max"`
	Min *int64 `ub:"min"`
}

// CapacityProviderAcceleratorTotalMemoryMiBRequest is an accelerator memory
// range in MiB.
type CapacityProviderAcceleratorTotalMemoryMiBRequest struct {
	Max *int64 `ub:"max"`
	Min *int64 `ub:"min"`
}

// CapacityProviderBaselineEbsBandwidthMbpsRequest is an EBS bandwidth range.
type CapacityProviderBaselineEbsBandwidthMbpsRequest struct {
	Max *int64 `ub:"max"`
	Min *int64 `ub:"min"`
}

// CapacityProviderMemoryGiBPerVCpuRequest is a memory per vCPU range.
type CapacityProviderMemoryGiBPerVCpuRequest struct {
	Max *float64 `ub:"max"`
	Min *float64 `ub:"min"`
}

// CapacityProviderNetworkBandwidthGbpsRequest is a network bandwidth range.
type CapacityProviderNetworkBandwidthGbpsRequest struct {
	Max *float64 `ub:"max"`
	Min *float64 `ub:"min"`
}

// CapacityProviderNetworkInterfaceCountRequest is a network interface count
// range.
type CapacityProviderNetworkInterfaceCountRequest struct {
	Max *int64 `ub:"max"`
	Min *int64 `ub:"min"`
}

// CapacityProviderTotalLocalStorageGBRequest is a local storage range in GB.
type CapacityProviderTotalLocalStorageGBRequest struct {
	Max *float64 `ub:"max"`
	Min *float64 `ub:"min"`
}

func (p *CapacityProviderAutoScalingGroupProvider) sdk() *ecstypes.AutoScalingGroupProvider {
	if p == nil {
		return nil
	}
	out := &ecstypes.AutoScalingGroupProvider{
		AutoScalingGroupArn: aws.String(p.AutoScalingGroupArn),
		ManagedScaling:      p.ManagedScaling.sdk(),
	}
	if p.ManagedDraining != nil {
		out.ManagedDraining = ecstypes.ManagedDraining(*p.ManagedDraining)
	}
	if p.ManagedTerminationProtection != nil {
		out.ManagedTerminationProtection =
			ecstypes.ManagedTerminationProtection(*p.ManagedTerminationProtection)
	}
	return out
}

func (p *CapacityProviderAutoScalingGroupProvider) updateSDK() *ecsASGProviderUpdate {
	if p == nil {
		return nil
	}
	out := &ecstypes.AutoScalingGroupProviderUpdate{ManagedScaling: p.ManagedScaling.sdk()}
	if p.ManagedDraining != nil {
		out.ManagedDraining = ecstypes.ManagedDraining(*p.ManagedDraining)
	}
	if p.ManagedTerminationProtection != nil {
		out.ManagedTerminationProtection =
			ecstypes.ManagedTerminationProtection(*p.ManagedTerminationProtection)
	}
	return out
}

func (m *CapacityProviderManagedScaling) sdk() *ecstypes.ManagedScaling {
	if m == nil {
		return nil
	}
	out := &ecstypes.ManagedScaling{
		InstanceWarmupPeriod:   ptr.Int32(m.InstanceWarmupPeriod),
		MaximumScalingStepSize: positiveInt32(m.MaximumScalingStepSize),
		MinimumScalingStepSize: positiveInt32(m.MinimumScalingStepSize),
		TargetCapacity:         positiveInt32(m.TargetCapacity),
	}
	if m.Status != nil {
		out.Status = ecstypes.ManagedScalingStatus(*m.Status)
	}
	return out
}

func (p *CapacityProviderManagedInstancesProvider) sdk() *ecsCreateMIProviderConfiguration {
	if p == nil {
		return nil
	}
	out := &ecstypes.CreateManagedInstancesProviderConfiguration{
		AutoRepairConfiguration:    p.AutoRepairConfiguration.sdk(),
		InfrastructureOptimization: p.InfrastructureOptimization.sdk(),
		InfrastructureRoleArn:      aws.String(p.InfrastructureRoleArn),
		InstanceLaunchTemplate:     p.InstanceLaunchTemplate.sdk(),
	}
	if p.PropagateTags != nil {
		out.PropagateTags = ecstypes.PropagateMITags(*p.PropagateTags)
	}
	return out
}

func (p *CapacityProviderManagedInstancesProvider) updateSDK() *ecsUpdateMIProviderConfiguration {
	if p == nil {
		return nil
	}
	out := &ecstypes.UpdateManagedInstancesProviderConfiguration{
		AutoRepairConfiguration:    p.AutoRepairConfiguration.sdk(),
		InfrastructureOptimization: p.InfrastructureOptimization.sdk(),
		InfrastructureRoleArn:      aws.String(p.InfrastructureRoleArn),
		InstanceLaunchTemplate:     p.InstanceLaunchTemplate.updateSDK(),
	}
	if p.PropagateTags != nil {
		out.PropagateTags = ecstypes.PropagateMITags(*p.PropagateTags)
	}
	return out
}

func (c *CapacityProviderAutoRepairConfiguration) sdk() *ecstypes.AutoRepairConfiguration {
	if c == nil {
		return nil
	}
	out := &ecstypes.AutoRepairConfiguration{}
	if c.ActionsStatus != nil {
		out.ActionsStatus = ecstypes.AutoRepairActionsStatus(*c.ActionsStatus)
	}
	return out
}

func (o *CapacityProviderInfrastructureOptimization) sdk() *ecstypes.InfrastructureOptimization {
	if o == nil {
		return nil
	}
	return &ecstypes.InfrastructureOptimization{ScaleInAfter: ptr.Int32(o.ScaleInAfter)}
}

func (t *CapacityProviderInstanceLaunchTemplate) sdk() *ecstypes.InstanceLaunchTemplate {
	out := &ecstypes.InstanceLaunchTemplate{
		CapacityReservations:            t.CapacityReservations.sdk(),
		Ec2InstanceProfileArn:           aws.String(t.Ec2InstanceProfileArn),
		FipsEnabled:                     t.FipsEnabled,
		InstanceMetadataTagsPropagation: t.InstanceMetadataTagsPropagation,
		InstanceRequirements:            t.InstanceRequirements.sdk(),
		LocalStorageConfiguration:       t.LocalStorageConfiguration.sdk(),
		NetworkConfiguration:            t.NetworkConfiguration.sdk(),
		StorageConfiguration:            t.StorageConfiguration.sdk(),
	}
	if t.CapacityOptionType != nil {
		out.CapacityOptionType = ecstypes.CapacityOptionType(*t.CapacityOptionType)
	}
	if t.Monitoring != nil {
		out.Monitoring = ecstypes.ManagedInstancesMonitoringOptions(*t.Monitoring)
	}
	return out
}

func (t *CapacityProviderInstanceLaunchTemplate) updateSDK() *ecsInstanceLaunchTemplateUpdate {
	out := &ecstypes.InstanceLaunchTemplateUpdate{
		CapacityReservations:            t.CapacityReservations.sdk(),
		Ec2InstanceProfileArn:           aws.String(t.Ec2InstanceProfileArn),
		InstanceMetadataTagsPropagation: t.InstanceMetadataTagsPropagation,
		InstanceRequirements:            t.InstanceRequirements.sdk(),
		LocalStorageConfiguration:       t.LocalStorageConfiguration.sdk(),
		NetworkConfiguration:            t.NetworkConfiguration.sdk(),
		StorageConfiguration:            t.StorageConfiguration.sdk(),
	}
	if t.Monitoring != nil {
		out.Monitoring = ecstypes.ManagedInstancesMonitoringOptions(*t.Monitoring)
	}
	return out
}

func (r *CapacityProviderCapacityReservationRequest) sdk() *ecstypes.CapacityReservationRequest {
	if r == nil {
		return nil
	}
	out := &ecstypes.CapacityReservationRequest{ReservationGroupArn: r.ReservationGroupArn}
	if r.ReservationPreference != nil {
		out.ReservationPreference =
			ecstypes.CapacityReservationPreference(*r.ReservationPreference)
	}
	return out
}

func (n CapacityProviderManagedInstancesNetworkConfiguration) sdk() *ecsMINetworkConfiguration {
	return &ecstypes.ManagedInstancesNetworkConfiguration{
		SecurityGroups: nilIfEmpty(n.SecurityGroups),
		Subnets:        n.Subnets,
	}
}

func (c *CapacityProviderManagedInstancesLocalStorageConfiguration) sdk() *ecsMILocalStorage {
	if c == nil {
		return nil
	}
	return &ecstypes.ManagedInstancesLocalStorageConfiguration{UseLocalStorage: c.UseLocalStorage}
}

func (c *CapacityProviderManagedInstancesStorageConfiguration) sdk() *ecsMIStorageConfiguration {
	if c == nil {
		return nil
	}
	return &ecstypes.ManagedInstancesStorageConfiguration{
		StorageSizeGiB: aws.Int32(int32(c.StorageSizeGiB)),
	}
}

func (r *CapacityProviderInstanceRequirementsRequest) sdk() *ecstypes.InstanceRequirementsRequest {
	if r == nil {
		return nil
	}
	out := &ecstypes.InstanceRequirementsRequest{
		MemoryMiB:                 r.MemoryMiB.sdk(),
		VCpuCount:                 r.VCpuCount.sdk(),
		AcceleratorCount:          r.AcceleratorCount.sdk(),
		AcceleratorManufacturers:  acceleratorManufacturersSDK(r.AcceleratorManufacturers),
		AcceleratorNames:          acceleratorNamesSDK(r.AcceleratorNames),
		AcceleratorTotalMemoryMiB: r.AcceleratorTotalMemoryMiB.sdk(),
		AcceleratorTypes:          acceleratorTypesSDK(r.AcceleratorTypes),
		AllowedInstanceTypes:      nilIfEmpty(r.AllowedInstanceTypes),
		BaselineEbsBandwidthMbps:  r.BaselineEbsBandwidthMbps.sdk(),
		CpuManufacturers:          cpuManufacturersSDK(r.CpuManufacturers),
		ExcludedInstanceTypes:     nilIfEmpty(r.ExcludedInstanceTypes),
		InstanceGenerations:       instanceGenerationsSDK(r.InstanceGenerations),
		LocalStorageTypes:         localStorageTypesSDK(r.LocalStorageTypes),
		MaxSpotPriceAsPercentageOfOptimalOnDemandPrice: positiveInt32(
			r.MaxSpotPriceAsPercentageOfOptimalOnDemandPrice),
		MemoryGiBPerVCpu:      r.MemoryGiBPerVCpu.sdk(),
		NetworkBandwidthGbps:  r.NetworkBandwidthGbps.sdk(),
		NetworkInterfaceCount: r.NetworkInterfaceCount.sdk(),
		OnDemandMaxPricePercentageOverLowestPrice: positiveInt32(
			r.OnDemandMaxPricePercentageOverLowestPrice),
		RequireHibernateSupport:               r.RequireHibernateSupport,
		SpotMaxPricePercentageOverLowestPrice: positiveInt32(r.SpotMaxPricePercentageOverLowestPrice),
		TotalLocalStorageGB:                   r.TotalLocalStorageGB.sdk(),
	}
	if r.BareMetal != nil {
		out.BareMetal = ecstypes.BareMetal(*r.BareMetal)
	}
	if r.BurstablePerformance != nil {
		out.BurstablePerformance = ecstypes.BurstablePerformance(*r.BurstablePerformance)
	}
	if r.LocalStorage != nil {
		out.LocalStorage = ecstypes.LocalStorage(*r.LocalStorage)
	}
	return out
}

func (r CapacityProviderMemoryMiBRequest) sdk() *ecstypes.MemoryMiBRequest {
	return &ecstypes.MemoryMiBRequest{Min: aws.Int32(int32(r.Min)), Max: positiveInt32(r.Max)}
}

func (r CapacityProviderVCpuCountRangeRequest) sdk() *ecstypes.VCpuCountRangeRequest {
	return &ecstypes.VCpuCountRangeRequest{Min: aws.Int32(int32(r.Min)), Max: positiveInt32(r.Max)}
}

func (r *CapacityProviderAcceleratorCountRequest) sdk() *ecstypes.AcceleratorCountRequest {
	if r == nil {
		return nil
	}
	return &ecstypes.AcceleratorCountRequest{Max: ptr.Int32(r.Max), Min: ptr.Int32(r.Min)}
}

func (r *CapacityProviderAcceleratorTotalMemoryMiBRequest) sdk() *ecsAccelTotalMemory {
	if r == nil {
		return nil
	}
	return &ecstypes.AcceleratorTotalMemoryMiBRequest{
		Max: ptr.Int32(r.Max),
		Min: ptr.Int32(r.Min),
	}
}

func (r *CapacityProviderBaselineEbsBandwidthMbpsRequest) sdk() *ecsBaselineEBSBandwidth {
	if r == nil {
		return nil
	}
	return &ecstypes.BaselineEbsBandwidthMbpsRequest{
		Max: positiveInt32(r.Max),
		Min: positiveInt32(r.Min),
	}
}

func (r *CapacityProviderMemoryGiBPerVCpuRequest) sdk() *ecstypes.MemoryGiBPerVCpuRequest {
	if r == nil {
		return nil
	}
	return &ecstypes.MemoryGiBPerVCpuRequest{
		Max: positiveFloat64(r.Max),
		Min: positiveFloat64(r.Min),
	}
}

func (r *CapacityProviderNetworkBandwidthGbpsRequest) sdk() *ecstypes.NetworkBandwidthGbpsRequest {
	if r == nil {
		return nil
	}
	return &ecstypes.NetworkBandwidthGbpsRequest{
		Max: positiveFloat64(r.Max),
		Min: positiveFloat64(r.Min),
	}
}

func (r *CapacityProviderNetworkInterfaceCountRequest) sdk() *ecsNetworkInterfaceCountRangeRequest {
	if r == nil {
		return nil
	}
	return &ecstypes.NetworkInterfaceCountRequest{
		Max: positiveInt32(r.Max),
		Min: positiveInt32(r.Min),
	}
}

func (r *CapacityProviderTotalLocalStorageGBRequest) sdk() *ecstypes.TotalLocalStorageGBRequest {
	if r == nil {
		return nil
	}
	return &ecstypes.TotalLocalStorageGBRequest{
		Max: positiveFloat64(r.Max),
		Min: positiveFloat64(r.Min),
	}
}

func capacityProviderASGOutput(
	p *ecstypes.AutoScalingGroupProvider,
) *CapacityProviderAutoScalingGroupProvider {
	if p == nil {
		return nil
	}
	return &CapacityProviderAutoScalingGroupProvider{
		AutoScalingGroupArn: aws.ToString(p.AutoScalingGroupArn),
		ManagedDraining:     enumOutputDefault(string(p.ManagedDraining), "ENABLED"),
		ManagedScaling:      capacityProviderManagedScalingOutput(p.ManagedScaling),
		ManagedTerminationProtection: enumOutputDefault(
			string(p.ManagedTerminationProtection), "DISABLED"),
	}
}

func capacityProviderManagedScalingOutput(
	m *ecstypes.ManagedScaling,
) *CapacityProviderManagedScaling {
	if m == nil {
		return &CapacityProviderManagedScaling{
			InstanceWarmupPeriod:   new(int64(300)),
			MaximumScalingStepSize: new(int64(10000)),
			MinimumScalingStepSize: new(int64(1)),
			Status:                 aws.String("DISABLED"),
			TargetCapacity:         new(int64(100)),
		}
	}
	return &CapacityProviderManagedScaling{
		InstanceWarmupPeriod:   int64FromInt32Default(m.InstanceWarmupPeriod, 300),
		MaximumScalingStepSize: int64FromInt32Default(m.MaximumScalingStepSize, 10000),
		MinimumScalingStepSize: int64FromInt32Default(m.MinimumScalingStepSize, 1),
		Status:                 enumOutputDefault(string(m.Status), "DISABLED"),
		TargetCapacity:         int64FromInt32Default(m.TargetCapacity, 100),
	}
}

func capacityProviderManagedInstancesOutput(
	p *ecstypes.ManagedInstancesProvider,
) *CapacityProviderManagedInstancesProvider {
	if p == nil {
		return nil
	}
	return &CapacityProviderManagedInstancesProvider{
		AutoRepairConfiguration: capacityProviderAutoRepairConfigurationOutput(
			p.AutoRepairConfiguration),
		InfrastructureOptimization: capacityProviderInfrastructureOptimizationOutput(
			p.InfrastructureOptimization),
		InfrastructureRoleArn:  aws.ToString(p.InfrastructureRoleArn),
		InstanceLaunchTemplate: capacityProviderInstanceLaunchTemplateOutput(p.InstanceLaunchTemplate),
		PropagateTags:          enumOutput(string(p.PropagateTags)),
	}
}

func capacityProviderAutoRepairConfigurationOutput(
	c *ecstypes.AutoRepairConfiguration,
) *CapacityProviderAutoRepairConfiguration {
	if c == nil {
		return nil
	}
	return &CapacityProviderAutoRepairConfiguration{
		ActionsStatus: enumOutput(string(c.ActionsStatus)),
	}
}

func capacityProviderInfrastructureOptimizationOutput(
	o *ecstypes.InfrastructureOptimization,
) *CapacityProviderInfrastructureOptimization {
	if o == nil {
		return nil
	}
	return &CapacityProviderInfrastructureOptimization{
		ScaleInAfter: int64FromInt32(o.ScaleInAfter),
	}
}

func capacityProviderInstanceLaunchTemplateOutput(
	t *ecstypes.InstanceLaunchTemplate,
) CapacityProviderInstanceLaunchTemplate {
	if t == nil {
		return CapacityProviderInstanceLaunchTemplate{
			CapacityOptionType: aws.String("ON_DEMAND"),
		}
	}
	return CapacityProviderInstanceLaunchTemplate{
		CapacityOptionType: enumOutputDefault(string(t.CapacityOptionType), "ON_DEMAND"),
		CapacityReservations: capacityProviderCapacityReservationsOutput(
			t.CapacityReservations),
		Ec2InstanceProfileArn:           aws.ToString(t.Ec2InstanceProfileArn),
		FipsEnabled:                     t.FipsEnabled,
		InstanceMetadataTagsPropagation: t.InstanceMetadataTagsPropagation,
		InstanceRequirements: capacityProviderInstanceRequirementsOutput(
			t.InstanceRequirements),
		LocalStorageConfiguration: capacityProviderLocalStorageConfigurationOutput(
			t.LocalStorageConfiguration),
		Monitoring:           enumOutput(string(t.Monitoring)),
		NetworkConfiguration: capacityProviderNetworkConfigurationOutput(t.NetworkConfiguration),
		StorageConfiguration: capacityProviderStorageConfigurationOutput(t.StorageConfiguration),
	}
}

func capacityProviderCapacityReservationsOutput(
	r *ecstypes.CapacityReservationRequest,
) *CapacityProviderCapacityReservationRequest {
	if r == nil {
		return nil
	}
	return &CapacityProviderCapacityReservationRequest{
		ReservationGroupArn:   r.ReservationGroupArn,
		ReservationPreference: enumOutput(string(r.ReservationPreference)),
	}
}

func capacityProviderNetworkConfigurationOutput(
	n *ecstypes.ManagedInstancesNetworkConfiguration,
) CapacityProviderManagedInstancesNetworkConfiguration {
	if n == nil {
		return CapacityProviderManagedInstancesNetworkConfiguration{}
	}
	return CapacityProviderManagedInstancesNetworkConfiguration{
		SecurityGroups: stringSliceOutput(n.SecurityGroups),
		Subnets:        stringSliceOutput(n.Subnets),
	}
}

func capacityProviderLocalStorageConfigurationOutput(
	c *ecstypes.ManagedInstancesLocalStorageConfiguration,
) *CapacityProviderManagedInstancesLocalStorageConfiguration {
	if c == nil {
		return nil
	}
	return &CapacityProviderManagedInstancesLocalStorageConfiguration{
		UseLocalStorage: c.UseLocalStorage,
	}
}

func capacityProviderStorageConfigurationOutput(
	c *ecstypes.ManagedInstancesStorageConfiguration,
) *CapacityProviderManagedInstancesStorageConfiguration {
	if c == nil {
		return nil
	}
	return &CapacityProviderManagedInstancesStorageConfiguration{
		StorageSizeGiB: int64ValueFromInt32(c.StorageSizeGiB),
	}
}

func capacityProviderInstanceRequirementsOutput(
	r *ecstypes.InstanceRequirementsRequest,
) *CapacityProviderInstanceRequirementsRequest {
	if r == nil {
		return nil
	}
	return &CapacityProviderInstanceRequirementsRequest{
		MemoryMiB:                capacityProviderMemoryMiBOutput(r.MemoryMiB),
		VCpuCount:                capacityProviderVCpuCountOutput(r.VCpuCount),
		AcceleratorCount:         capacityProviderAcceleratorCountOutput(r.AcceleratorCount),
		AcceleratorManufacturers: enumStringSliceOutput(r.AcceleratorManufacturers),
		AcceleratorNames:         enumStringSliceOutput(r.AcceleratorNames),
		AcceleratorTotalMemoryMiB: capacityProviderAcceleratorTotalMemoryOutput(
			r.AcceleratorTotalMemoryMiB),
		AcceleratorTypes:     enumStringSliceOutput(r.AcceleratorTypes),
		AllowedInstanceTypes: stringSliceOutput(r.AllowedInstanceTypes),
		BareMetal:            enumOutput(string(r.BareMetal)),
		BaselineEbsBandwidthMbps: capacityProviderBaselineEbsBandwidthOutput(
			r.BaselineEbsBandwidthMbps),
		BurstablePerformance:  enumOutput(string(r.BurstablePerformance)),
		CpuManufacturers:      enumStringSliceOutput(r.CpuManufacturers),
		ExcludedInstanceTypes: stringSliceOutput(r.ExcludedInstanceTypes),
		InstanceGenerations:   enumStringSliceOutput(r.InstanceGenerations),
		LocalStorage:          enumOutput(string(r.LocalStorage)),
		LocalStorageTypes:     enumStringSliceOutput(r.LocalStorageTypes),
		MaxSpotPriceAsPercentageOfOptimalOnDemandPrice: int64FromInt32(
			r.MaxSpotPriceAsPercentageOfOptimalOnDemandPrice),
		MemoryGiBPerVCpu:     capacityProviderMemoryGiBPerVCpuOutput(r.MemoryGiBPerVCpu),
		NetworkBandwidthGbps: capacityProviderNetworkBandwidthOutput(r.NetworkBandwidthGbps),
		NetworkInterfaceCount: capacityProviderNetworkInterfaceCountOutput(
			r.NetworkInterfaceCount),
		OnDemandMaxPricePercentageOverLowestPrice: int64FromInt32(
			r.OnDemandMaxPricePercentageOverLowestPrice),
		RequireHibernateSupport: r.RequireHibernateSupport,
		SpotMaxPricePercentageOverLowestPrice: int64FromInt32(
			r.SpotMaxPricePercentageOverLowestPrice),
		TotalLocalStorageGB: capacityProviderTotalLocalStorageOutput(r.TotalLocalStorageGB),
	}
}

func capacityProviderMemoryMiBOutput(
	r *ecstypes.MemoryMiBRequest,
) CapacityProviderMemoryMiBRequest {
	if r == nil {
		return CapacityProviderMemoryMiBRequest{}
	}
	return CapacityProviderMemoryMiBRequest{
		Min: int64ValueFromInt32(r.Min),
		Max: int64FromInt32(r.Max),
	}
}

func capacityProviderVCpuCountOutput(
	r *ecstypes.VCpuCountRangeRequest,
) CapacityProviderVCpuCountRangeRequest {
	if r == nil {
		return CapacityProviderVCpuCountRangeRequest{}
	}
	return CapacityProviderVCpuCountRangeRequest{
		Min: int64ValueFromInt32(r.Min),
		Max: int64FromInt32(r.Max),
	}
}

func capacityProviderAcceleratorCountOutput(
	r *ecstypes.AcceleratorCountRequest,
) *CapacityProviderAcceleratorCountRequest {
	if r == nil {
		return nil
	}
	return &CapacityProviderAcceleratorCountRequest{
		Max: int64FromInt32(r.Max),
		Min: int64FromInt32(r.Min),
	}
}

func capacityProviderAcceleratorTotalMemoryOutput(
	r *ecstypes.AcceleratorTotalMemoryMiBRequest,
) *CapacityProviderAcceleratorTotalMemoryMiBRequest {
	if r == nil {
		return nil
	}
	return &CapacityProviderAcceleratorTotalMemoryMiBRequest{
		Max: int64FromInt32(r.Max),
		Min: int64FromInt32(r.Min),
	}
}

func capacityProviderBaselineEbsBandwidthOutput(
	r *ecstypes.BaselineEbsBandwidthMbpsRequest,
) *CapacityProviderBaselineEbsBandwidthMbpsRequest {
	if r == nil {
		return nil
	}
	return &CapacityProviderBaselineEbsBandwidthMbpsRequest{
		Max: int64FromInt32(r.Max),
		Min: int64FromInt32(r.Min),
	}
}

func capacityProviderMemoryGiBPerVCpuOutput(
	r *ecstypes.MemoryGiBPerVCpuRequest,
) *CapacityProviderMemoryGiBPerVCpuRequest {
	if r == nil {
		return nil
	}
	return &CapacityProviderMemoryGiBPerVCpuRequest{Max: r.Max, Min: r.Min}
}

func capacityProviderNetworkBandwidthOutput(
	r *ecstypes.NetworkBandwidthGbpsRequest,
) *CapacityProviderNetworkBandwidthGbpsRequest {
	if r == nil {
		return nil
	}
	return &CapacityProviderNetworkBandwidthGbpsRequest{Max: r.Max, Min: r.Min}
}

func capacityProviderNetworkInterfaceCountOutput(
	r *ecstypes.NetworkInterfaceCountRequest,
) *CapacityProviderNetworkInterfaceCountRequest {
	if r == nil {
		return nil
	}
	return &CapacityProviderNetworkInterfaceCountRequest{
		Max: int64FromInt32(r.Max),
		Min: int64FromInt32(r.Min),
	}
}

func capacityProviderTotalLocalStorageOutput(
	r *ecstypes.TotalLocalStorageGBRequest,
) *CapacityProviderTotalLocalStorageGBRequest {
	if r == nil {
		return nil
	}
	return &CapacityProviderTotalLocalStorageGBRequest{Max: r.Max, Min: r.Min}
}

func acceleratorManufacturersSDK(values []string) []ecstypes.AcceleratorManufacturer {
	if len(values) == 0 {
		return nil
	}
	out := make([]ecstypes.AcceleratorManufacturer, 0, len(values))
	for _, v := range values {
		out = append(out, ecstypes.AcceleratorManufacturer(v))
	}
	return out
}

func acceleratorNamesSDK(values []string) []ecstypes.AcceleratorName {
	if len(values) == 0 {
		return nil
	}
	out := make([]ecstypes.AcceleratorName, 0, len(values))
	for _, v := range values {
		out = append(out, ecstypes.AcceleratorName(v))
	}
	return out
}

func acceleratorTypesSDK(values []string) []ecstypes.AcceleratorType {
	if len(values) == 0 {
		return nil
	}
	out := make([]ecstypes.AcceleratorType, 0, len(values))
	for _, v := range values {
		out = append(out, ecstypes.AcceleratorType(v))
	}
	return out
}

func cpuManufacturersSDK(values []string) []ecstypes.CpuManufacturer {
	if len(values) == 0 {
		return nil
	}
	out := make([]ecstypes.CpuManufacturer, 0, len(values))
	for _, v := range values {
		out = append(out, ecstypes.CpuManufacturer(v))
	}
	return out
}

func instanceGenerationsSDK(values []string) []ecstypes.InstanceGeneration {
	if len(values) == 0 {
		return nil
	}
	out := make([]ecstypes.InstanceGeneration, 0, len(values))
	for _, v := range values {
		out = append(out, ecstypes.InstanceGeneration(v))
	}
	return out
}

func localStorageTypesSDK(values []string) []ecstypes.LocalStorageType {
	if len(values) == 0 {
		return nil
	}
	out := make([]ecstypes.LocalStorageType, 0, len(values))
	for _, v := range values {
		out = append(out, ecstypes.LocalStorageType(v))
	}
	return out
}

func nilIfEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}

func stringSliceOutput(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func enumStringSliceOutput[T ~string](values []T) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

func positiveInt32(v *int64) *int32 {
	if v == nil || *v <= 0 {
		return nil
	}
	return ptr.Int32(v)
}

func positiveFloat64(v *float64) *float64 {
	if v == nil || *v <= 0 {
		return nil
	}
	return v
}

func int64FromInt32(v *int32) *int64 {
	if v == nil {
		return nil
	}
	n := int64(*v)
	return &n
}

func int64FromInt32Default(v *int32, defaultValue int64) *int64 {
	if v == nil {
		return new(defaultValue)
	}
	n := int64(*v)
	return &n
}

func int64ValueFromInt32(v *int32) int64 {
	if v == nil {
		return 0
	}
	return int64(*v)
}

func enumOutput(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func enumOutputDefault(v, defaultValue string) *string {
	if v == "" {
		v = defaultValue
	}
	return &v
}
