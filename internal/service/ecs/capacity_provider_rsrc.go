package ecs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	ecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	capacityProviderUpdateTimeout       = 10 * time.Minute
	capacityProviderDeleteTimeout       = 20 * time.Minute
	capacityProviderUpdateNotFoundLimit = 20
)

var (
	capacityProviderNameRegexp         = regexp.MustCompile(`^[0-9A-Za-z_-]{1,255}$`)
	capacityProviderClusterRegexp      = regexp.MustCompile(`^[0-9A-Za-z_-]{1,255}$`)
	capacityProviderInstanceTypeRegexp = regexp.MustCompile(`^[a-zA-Z0-9\.\*\-]+$`)
	capacityProviderARNPartitionRegexp = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	capacityProviderARNRegionRegexp    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	capacityProviderARNAccountRegexp   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|` +
			`partner-managed|\d{12}|cw.{10})$`)
)

// CapacityProvider manages an ECS capacity provider. Exactly one provider
// block is required: an Auto Scaling group provider is account-scoped and
// must not set cluster, while a Managed Instances provider is cluster-scoped
// and requires cluster. Tags are reconciled separately from provider settings.
//
// The name and cluster inputs must match ^[0-9A-Za-z_-]{1,255}$, ARN fields
// get generic ARN validation, and allowed/excluded instance types are checked
// for length and ^[a-zA-Z0-9\.\*\-]+$ in validate because those checks are not
// derivable constraints.
type CapacityProvider struct {
	Name                     string                                    `ub:"name"`
	Cluster                  *string                                   `ub:"cluster"`
	AutoScalingGroupProvider *CapacityProviderAutoScalingGroupProvider `ub:"auto-scaling-group-provider"`
	ManagedInstancesProvider *CapacityProviderManagedInstancesProvider `ub:"managed-instances-provider"`
	Tags                     map[string]string                         `ub:"tags"`
}

// CapacityProviderOutput holds the ECS-computed identity and status fields,
// observed user tags, plus the complete observed selected provider block.
// Unobin merges output references shallowly, so a provider block output must
// not be partial and tags must be cloud-observed rather than echoed inputs.
type CapacityProviderOutput struct {
	Arn                      string                                    `ub:"arn"`
	CapacityProviderArn      string                                    `ub:"capacity-provider-arn"`
	Status                   string                                    `ub:"status"`
	UpdateStatus             string                                    `ub:"update-status"`
	UpdateStatusReason       *string                                   `ub:"update-status-reason"`
	Tags                     map[string]string                         `ub:"tags"`
	AutoScalingGroupProvider *CapacityProviderAutoScalingGroupProvider `ub:"auto-scaling-group-provider"`
	ManagedInstancesProvider *CapacityProviderManagedInstancesProvider `ub:"managed-instances-provider"`
}

func (o *CapacityProviderOutput) capacityProviderArn() string {
	if o == nil {
		return ""
	}
	if o.Arn != "" {
		return o.Arn
	}
	return o.CapacityProviderArn
}

func (r *CapacityProvider) SchemaVersion() int { return 1 }

// ReplaceFields lists the top-level fields ECS fixes at create time. The ASG
// ARN, Managed Instances capacity option type, and provider-kind switches are
// also create-only, but the current runtime only evaluates top-level replace
// triggers, so those nested leaves cannot be represented safely here.
func (r *CapacityProvider) ReplaceFields() []string {
	return []string{"name", "cluster"}
}

// Defaults marks optional collection inputs, including optional collections
// nested under the Managed Instances instance requirements block.
func (r CapacityProvider) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
		defaults.Optional(
			r.ManagedInstancesProvider.InstanceLaunchTemplate.NetworkConfiguration.SecurityGroups),
		defaults.Optional(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.AcceleratorManufacturers),
		defaults.Optional(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.AcceleratorNames),
		defaults.Optional(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.AcceleratorTypes),
		defaults.Optional(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.AllowedInstanceTypes),
		defaults.Optional(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.CpuManufacturers),
		defaults.Optional(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.ExcludedInstanceTypes),
		defaults.Optional(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.InstanceGenerations),
		defaults.Optional(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.LocalStorageTypes),
	}
}

// Constraints declares the provider selection rules, mutable enum sets, and
// numeric bounds that can be derived from fields. Regex checks for names,
// ARNs, and instance type patterns run in validate instead.
func (r CapacityProvider) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.AutoScalingGroupProvider, r.ManagedInstancesProvider),
		constraint.When(constraint.Present(r.ManagedInstancesProvider)).
			Require(constraint.Present(r.Cluster)).
			Message("cluster is required with managed-instances-provider"),
		constraint.ForbiddenWith(r.AutoScalingGroupProvider, r.Cluster),
		constraint.When(constraint.Present(r.AutoScalingGroupProvider.ManagedDraining)).
			Require(constraint.OneOf(r.AutoScalingGroupProvider.ManagedDraining,
				"ENABLED", "DISABLED")).
			Message("managed-draining must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(
			r.AutoScalingGroupProvider.ManagedTerminationProtection)).
			Require(constraint.OneOf(
				r.AutoScalingGroupProvider.ManagedTerminationProtection,
				"ENABLED", "DISABLED")).
			Message("managed-termination-protection must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.AutoScalingGroupProvider.ManagedScaling.Status)).
			Require(constraint.OneOf(r.AutoScalingGroupProvider.ManagedScaling.Status,
				"ENABLED", "DISABLED")).
			Message("managed-scaling status must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(
			r.AutoScalingGroupProvider.ManagedScaling.InstanceWarmupPeriod)).
			Require(constraint.AtLeast(
				r.AutoScalingGroupProvider.ManagedScaling.InstanceWarmupPeriod, 0),
				constraint.AtMost(
					r.AutoScalingGroupProvider.ManagedScaling.InstanceWarmupPeriod, 10000)).
			Message("instance-warmup-period must be between 0 and 10000"),
		constraint.When(constraint.Present(
			r.AutoScalingGroupProvider.ManagedScaling.MaximumScalingStepSize)).
			Require(constraint.AtLeast(
				r.AutoScalingGroupProvider.ManagedScaling.MaximumScalingStepSize, 1),
				constraint.AtMost(
					r.AutoScalingGroupProvider.ManagedScaling.MaximumScalingStepSize,
					10000)).
			Message("maximum-scaling-step-size must be between 1 and 10000"),
		constraint.When(constraint.Present(
			r.AutoScalingGroupProvider.ManagedScaling.MinimumScalingStepSize)).
			Require(constraint.AtLeast(
				r.AutoScalingGroupProvider.ManagedScaling.MinimumScalingStepSize, 1),
				constraint.AtMost(
					r.AutoScalingGroupProvider.ManagedScaling.MinimumScalingStepSize,
					10000)).
			Message("minimum-scaling-step-size must be between 1 and 10000"),
		constraint.When(constraint.Present(
			r.AutoScalingGroupProvider.ManagedScaling.TargetCapacity)).
			Require(constraint.AtLeast(
				r.AutoScalingGroupProvider.ManagedScaling.TargetCapacity, 1),
				constraint.AtMost(
					r.AutoScalingGroupProvider.ManagedScaling.TargetCapacity, 100)).
			Message("target-capacity must be between 1 and 100"),
		constraint.When(constraint.Present(
			r.ManagedInstancesProvider.AutoRepairConfiguration.ActionsStatus)).
			Require(constraint.OneOf(
				r.ManagedInstancesProvider.AutoRepairConfiguration.ActionsStatus,
				"ENABLED", "DISABLED")).
			Message("auto-repair actions-status must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(
			r.ManagedInstancesProvider.InfrastructureOptimization.ScaleInAfter)).
			Require(constraint.AtLeast(
				r.ManagedInstancesProvider.InfrastructureOptimization.ScaleInAfter, -1),
				constraint.AtMost(
					r.ManagedInstancesProvider.InfrastructureOptimization.ScaleInAfter, 3600)).
			Message("scale-in-after must be between -1 and 3600"),
		constraint.When(constraint.Present(
			r.ManagedInstancesProvider.InstanceLaunchTemplate.CapacityOptionType)).
			Require(constraint.OneOf(
				r.ManagedInstancesProvider.InstanceLaunchTemplate.CapacityOptionType,
				"ON_DEMAND", "SPOT", "RESERVED")).
			Message("capacity-option-type must be ON_DEMAND, SPOT, or RESERVED"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider)).
			Require(constraint.NotEmpty(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				NetworkConfiguration.Subnets)).
			Message("managed instances network-configuration subnets must not be empty"),
		constraint.When(constraint.Present(
			r.ManagedInstancesProvider.InstanceLaunchTemplate.Monitoring)).
			Require(constraint.OneOf(
				r.ManagedInstancesProvider.InstanceLaunchTemplate.Monitoring,
				"BASIC", "DETAILED")).
			Message("monitoring must be BASIC or DETAILED"),
		constraint.When(constraint.Present(
			r.ManagedInstancesProvider.InstanceLaunchTemplate.CapacityReservations.
				ReservationPreference)).
			Require(constraint.OneOf(
				r.ManagedInstancesProvider.InstanceLaunchTemplate.CapacityReservations.
					ReservationPreference,
				"RESERVATIONS_ONLY", "RESERVATIONS_FIRST", "RESERVATIONS_EXCLUDED")).
			Message("reservation-preference must be a valid capacity reservation preference"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.PropagateTags)).
			Require(constraint.OneOf(r.ManagedInstancesProvider.PropagateTags,
				"CAPACITY_PROVIDER", "NONE")).
			Message("propagate-tags must be CAPACITY_PROVIDER or NONE"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.AllowedInstanceTypes)).
			Require(constraint.MaxItems(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.AllowedInstanceTypes, 400)).
			Message("allowed-instance-types allows at most 400 entries"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.ExcludedInstanceTypes)).
			Require(constraint.MaxItems(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.ExcludedInstanceTypes, 400)).
			Message("excluded-instance-types allows at most 400 entries"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.MemoryMiB.Min, 1),
				constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
					InstanceRequirements.MemoryMiB.Max, 1),
				constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
					InstanceRequirements.VCpuCount.Min, 1),
				constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
					InstanceRequirements.VCpuCount.Max, 1)).
			Message("memory-mib and vcpu-count ranges must be at least 1"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.AcceleratorCount)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.AcceleratorCount.Min, 0),
				constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
					InstanceRequirements.AcceleratorCount.Max, 0)).
			Message("accelerator-count range must be at least 0"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.AcceleratorTotalMemoryMiB)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.AcceleratorTotalMemoryMiB.Min, 0),
				constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
					InstanceRequirements.AcceleratorTotalMemoryMiB.Max, 0)).
			Message("accelerator-total-memory-mib range must be at least 0"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.BaselineEbsBandwidthMbps)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.BaselineEbsBandwidthMbps.Min, 0),
				constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
					InstanceRequirements.BaselineEbsBandwidthMbps.Max, 0)).
			Message("baseline-ebs-bandwidth-mbps range must be at least 0"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.MemoryGiBPerVCpu)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.MemoryGiBPerVCpu.Min, 0),
				constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
					InstanceRequirements.MemoryGiBPerVCpu.Max, 0)).
			Message("memory-gib-per-vcpu range must be at least 0"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.NetworkBandwidthGbps)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.NetworkBandwidthGbps.Min, 0),
				constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
					InstanceRequirements.NetworkBandwidthGbps.Max, 0)).
			Message("network-bandwidth-gbps range must be at least 0"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.NetworkInterfaceCount)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.NetworkInterfaceCount.Min, 1),
				constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
					InstanceRequirements.NetworkInterfaceCount.Max, 1)).
			Message("network-interface-count range must be at least 1"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			StorageConfiguration)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				StorageConfiguration.StorageSizeGiB, 1)).
			Message("storage-size-gib must be at least 1"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.TotalLocalStorageGB)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.TotalLocalStorageGB.Min, 0),
				constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
					InstanceRequirements.TotalLocalStorageGB.Max, 0)).
			Message("total-local-storage-gb range must be at least 0"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.BareMetal)).
			Require(constraint.OneOf(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.BareMetal, "included", "required", "excluded")).
			Message("bare-metal must be included, required, or excluded"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.BurstablePerformance)).
			Require(constraint.OneOf(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.BurstablePerformance,
				"included", "required", "excluded")).
			Message("burstable-performance must be included, required, or excluded"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.LocalStorage)).
			Require(constraint.OneOf(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.LocalStorage, "included", "required", "excluded")).
			Message("local-storage must be included, required, or excluded"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.MaxSpotPriceAsPercentageOfOptimalOnDemandPrice)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.MaxSpotPriceAsPercentageOfOptimalOnDemandPrice, 0)).
			Message("max-spot-price-as-percentage-of-optimal-on-demand-price must be at least 0"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.OnDemandMaxPricePercentageOverLowestPrice)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.OnDemandMaxPricePercentageOverLowestPrice, 0)).
			Message("on-demand-max-price-percentage-over-lowest-price must be at least 0"),
		constraint.When(constraint.Present(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.SpotMaxPricePercentageOverLowestPrice)).
			Require(constraint.AtLeast(r.ManagedInstancesProvider.InstanceLaunchTemplate.
				InstanceRequirements.SpotMaxPricePercentageOverLowestPrice, 0)).
			Message("spot-max-price-percentage-over-lowest-price must be at least 0"),
		constraint.ForEach(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.AcceleratorManufacturers, func(v string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(v,
					"amazon-web-services", "amd", "nvidia", "xilinx", "habana")).
					Message("accelerator-manufacturers entries must be valid"),
			}
		}),
		constraint.ForEach(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.AcceleratorNames, func(v string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(v, "a100", "inferentia", "k520",
					"k80", "m60", "radeon-pro-v520", "t4", "vu9p", "v100",
					"a10g", "h100", "t4g")).
					Message("accelerator-names entries must be valid"),
			}
		}),
		constraint.ForEach(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.AcceleratorTypes, func(v string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(v, "gpu", "fpga", "inference")).
					Message("accelerator-types entries must be gpu, fpga, or inference"),
			}
		}),
		constraint.ForEach(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.CpuManufacturers, func(v string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(v,
					"intel", "amd", "amazon-web-services")).
					Message("cpu-manufacturers entries must be valid"),
			}
		}),
		constraint.ForEach(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.InstanceGenerations, func(v string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(v, "current", "previous")).
					Message("instance-generations entries must be current or previous"),
			}
		}),
		constraint.ForEach(r.ManagedInstancesProvider.InstanceLaunchTemplate.
			InstanceRequirements.LocalStorageTypes, func(v string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(v, "hdd", "ssd")).
					Message("local-storage-types entries must be hdd or ssd"),
			}
		}),
	}
}

func (r *CapacityProvider) Create(
	ctx context.Context, cfg *awsCfg,
) (*CapacityProviderOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	in := &ecs.CreateCapacityProviderInput{
		Name:                     aws.String(r.Name),
		AutoScalingGroupProvider: r.AutoScalingGroupProvider.sdk(),
		Cluster:                  r.Cluster,
		ManagedInstancesProvider: r.ManagedInstancesProvider.sdk(),
		Tags:                     tagsSDK(r.Tags),
	}
	create := func() (*ecs.CreateCapacityProviderOutput, error) {
		return client.CreateCapacityProvider(ctx, in)
	}
	resp, err := create()
	taggedSeparately := false
	if err != nil && in.Tags != nil && partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		resp, err = create()
	}
	if err != nil {
		return nil, fmt.Errorf("create capacity provider: %w", err)
	}
	if resp.CapacityProvider == nil {
		return nil, errors.New("create capacity provider: response holds no capacity provider")
	}
	capacityProviderArn := aws.ToString(resp.CapacityProvider.CapacityProviderArn)
	if fallbackTags := tagsSDK(r.Tags); taggedSeparately && len(fallbackTags) > 0 {
		if _, err := client.TagResource(ctx, &ecs.TagResourceInput{
			ResourceArn: aws.String(capacityProviderArn),
			Tags:        fallbackTags,
		}); err != nil {
			return nil, fmt.Errorf("tag capacity provider: %w", err)
		}
	}
	return r.read(ctx, client, capacityProviderArn, true)
}

func (r *CapacityProvider) Read(
	ctx context.Context, cfg *awsCfg, prior *CapacityProviderOutput,
) (*CapacityProviderOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.capacityProviderArn(), true)
}

func (r *CapacityProvider) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[CapacityProvider, *CapacityProviderOutput],
) (*CapacityProviderOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	capacityProviderArn := prior.Outputs.capacityProviderArn()
	if r.providerChanged(prior) {
		in := &ecs.UpdateCapacityProviderInput{
			Name:                     aws.String(r.Name),
			Cluster:                  r.Cluster,
			AutoScalingGroupProvider: r.AutoScalingGroupProvider.updateSDK(),
			ManagedInstancesProvider: r.ManagedInstancesProvider.updateSDK(),
		}
		err := retry.OnError(ctx, capacityProviderUpdateRetryable,
			func(ctx context.Context) error {
				_, err := client.UpdateCapacityProvider(ctx, in)
				return err
			}, retry.WithTimeout(capacityProviderUpdateTimeout))
		if err != nil {
			return nil, fmt.Errorf("update capacity provider: %w", err)
		}
		if err := waitCapacityProviderUpdated(ctx, client, capacityProviderArn); err != nil {
			return nil, err
		}
	}
	if r.tagsNeedSync(prior) {
		if err := syncResourceTags(ctx, client, capacityProviderArn, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, capacityProviderArn, true)
}

func (r *CapacityProvider) Delete(
	ctx context.Context, cfg *awsCfg, prior *CapacityProviderOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	capacityProviderArn := prior.capacityProviderArn()
	_, err = client.DeleteCapacityProvider(ctx, &ecs.DeleteCapacityProviderInput{
		CapacityProvider: aws.String(capacityProviderArn),
	})
	if err != nil {
		if capacityProviderAlreadyDeleted(err) {
			return nil
		}
		return fmt.Errorf("delete capacity provider: %w", err)
	}
	return waitCapacityProviderDeleted(ctx, client, capacityProviderArn)
}

func (r *CapacityProvider) tagsNeedSync(
	prior runtime.Prior[CapacityProvider, *CapacityProviderOutput],
) bool {
	desired := capacityProviderUserTags(r.Tags)
	if !maps.Equal(capacityProviderUserTags(prior.Inputs.Tags), desired) {
		return true
	}
	return prior.Observed != nil &&
		!maps.Equal(capacityProviderUserTags(prior.Observed.Tags), desired)
}

func (r *CapacityProvider) providerChanged(
	prior runtime.Prior[CapacityProvider, *CapacityProviderOutput],
) bool {
	if runtime.Changed(prior.Inputs.AutoScalingGroupProvider, r.AutoScalingGroupProvider) ||
		runtime.Changed(prior.Inputs.ManagedInstancesProvider, r.ManagedInstancesProvider) {
		return true
	}
	return r.configuredASGDrifted(prior.Observed) ||
		r.configuredManagedInstancesDrifted(prior.Observed)
}

func (r *CapacityProvider) configuredASGDrifted(observed *CapacityProviderOutput) bool {
	desired := r.AutoScalingGroupProvider
	if desired == nil || observed == nil || observed.AutoScalingGroupProvider == nil {
		return false
	}
	actual := observed.AutoScalingGroupProvider
	if desired.ManagedDraining != nil &&
		runtime.Changed(actual.ManagedDraining, desired.ManagedDraining) {
		return true
	}
	if desired.ManagedTerminationProtection != nil &&
		runtime.Changed(actual.ManagedTerminationProtection,
			desired.ManagedTerminationProtection) {
		return true
	}
	return configuredManagedScalingDrifted(desired.ManagedScaling, actual.ManagedScaling)
}

func configuredManagedScalingDrifted(
	desired, actual *CapacityProviderManagedScaling,
) bool {
	if desired == nil {
		return false
	}
	if actual == nil {
		return desired.InstanceWarmupPeriod != nil || desired.MaximumScalingStepSize != nil ||
			desired.MinimumScalingStepSize != nil || desired.Status != nil ||
			desired.TargetCapacity != nil
	}
	if desired.InstanceWarmupPeriod != nil &&
		runtime.Changed(actual.InstanceWarmupPeriod, desired.InstanceWarmupPeriod) {
		return true
	}
	if desired.MaximumScalingStepSize != nil &&
		runtime.Changed(actual.MaximumScalingStepSize, desired.MaximumScalingStepSize) {
		return true
	}
	if desired.MinimumScalingStepSize != nil &&
		runtime.Changed(actual.MinimumScalingStepSize, desired.MinimumScalingStepSize) {
		return true
	}
	if desired.Status != nil && runtime.Changed(actual.Status, desired.Status) {
		return true
	}
	if desired.TargetCapacity != nil &&
		runtime.Changed(actual.TargetCapacity, desired.TargetCapacity) {
		return true
	}
	return false
}

func (r *CapacityProvider) configuredManagedInstancesDrifted(
	observed *CapacityProviderOutput,
) bool {
	desired := r.ManagedInstancesProvider
	if desired == nil || observed == nil || observed.ManagedInstancesProvider == nil {
		return false
	}
	actual := observed.ManagedInstancesProvider
	if configuredAutoRepairConfigurationDrifted(
		desired.AutoRepairConfiguration,
		actual.AutoRepairConfiguration,
	) {
		return true
	}
	if runtime.Changed(actual.InfrastructureRoleArn, desired.InfrastructureRoleArn) {
		return true
	}
	if desired.PropagateTags != nil && runtime.Changed(actual.PropagateTags, desired.PropagateTags) {
		return true
	}
	if configuredInfrastructureOptimizationDrifted(
		desired.InfrastructureOptimization,
		actual.InfrastructureOptimization,
	) {
		return true
	}
	return configuredInstanceLaunchTemplateDrifted(
		&desired.InstanceLaunchTemplate,
		&actual.InstanceLaunchTemplate,
	)
}

func configuredAutoRepairConfigurationDrifted(
	desired, actual *CapacityProviderAutoRepairConfiguration,
) bool {
	if desired == nil || desired.ActionsStatus == nil {
		return false
	}
	return actual == nil || runtime.Changed(actual.ActionsStatus, desired.ActionsStatus)
}

func configuredInfrastructureOptimizationDrifted(
	desired, actual *CapacityProviderInfrastructureOptimization,
) bool {
	if desired == nil || desired.ScaleInAfter == nil {
		return false
	}
	return actual == nil || runtime.Changed(actual.ScaleInAfter, desired.ScaleInAfter)
}

func configuredInstanceLaunchTemplateDrifted(
	desired, actual *CapacityProviderInstanceLaunchTemplate,
) bool {
	if runtime.Changed(actual.Ec2InstanceProfileArn, desired.Ec2InstanceProfileArn) {
		return true
	}
	if configuredCapacityReservationsDrifted(
		desired.CapacityReservations,
		actual.CapacityReservations,
	) {
		return true
	}
	if configuredPtrDrifted(
		desired.InstanceMetadataTagsPropagation,
		actual.InstanceMetadataTagsPropagation,
	) {
		return true
	}
	if configuredLocalStorageConfigurationDrifted(
		desired.LocalStorageConfiguration,
		actual.LocalStorageConfiguration,
	) {
		return true
	}
	if desired.Monitoring != nil && runtime.Changed(actual.Monitoring, desired.Monitoring) {
		return true
	}
	if configuredNetworkConfigurationDrifted(
		desired.NetworkConfiguration,
		actual.NetworkConfiguration,
	) {
		return true
	}
	if configuredStorageConfigurationDrifted(
		desired.StorageConfiguration,
		actual.StorageConfiguration,
	) {
		return true
	}
	return configuredInstanceRequirementsDrifted(
		desired.InstanceRequirements,
		actual.InstanceRequirements,
	)
}

func configuredCapacityReservationsDrifted(
	desired, actual *CapacityProviderCapacityReservationRequest,
) bool {
	if desired == nil {
		return false
	}
	if actual == nil {
		return desired.ReservationGroupArn != nil || desired.ReservationPreference != nil
	}
	return configuredPtrDrifted(desired.ReservationGroupArn, actual.ReservationGroupArn) ||
		configuredPtrDrifted(desired.ReservationPreference, actual.ReservationPreference)
}

func configuredNetworkConfigurationDrifted(
	desired, actual CapacityProviderManagedInstancesNetworkConfiguration,
) bool {
	if runtime.Changed(actual.Subnets, desired.Subnets) {
		return true
	}
	return len(desired.SecurityGroups) > 0 &&
		runtime.Changed(actual.SecurityGroups, desired.SecurityGroups)
}

func configuredLocalStorageConfigurationDrifted(
	desired, actual *CapacityProviderManagedInstancesLocalStorageConfiguration,
) bool {
	if desired == nil {
		return false
	}
	if actual == nil {
		return desired.UseLocalStorage
	}
	return actual.UseLocalStorage != desired.UseLocalStorage
}

func configuredStorageConfigurationDrifted(
	desired, actual *CapacityProviderManagedInstancesStorageConfiguration,
) bool {
	if desired == nil {
		return false
	}
	return actual == nil || runtime.Changed(actual.StorageSizeGiB, desired.StorageSizeGiB)
}

func configuredInstanceRequirementsDrifted(
	desired, actual *CapacityProviderInstanceRequirementsRequest,
) bool {
	if desired == nil {
		return false
	}
	if actual == nil {
		return true
	}
	if configuredMemoryMiBDrifted(desired.MemoryMiB, actual.MemoryMiB) ||
		configuredVCpuCountDrifted(desired.VCpuCount, actual.VCpuCount) ||
		configuredZeroIntRangeDrifted(desired.AcceleratorCount, actual.AcceleratorCount) ||
		configuredZeroIntRangeDrifted(
			desired.AcceleratorTotalMemoryMiB,
			actual.AcceleratorTotalMemoryMiB,
		) ||
		configuredPositiveIntRangeDrifted(
			desired.BaselineEbsBandwidthMbps,
			actual.BaselineEbsBandwidthMbps,
		) ||
		configuredPositiveFloatRangeDrifted(
			desired.MemoryGiBPerVCpu,
			actual.MemoryGiBPerVCpu,
		) ||
		configuredPositiveFloatRangeDrifted(
			desired.NetworkBandwidthGbps,
			actual.NetworkBandwidthGbps,
		) ||
		configuredPositiveIntRangeDrifted(
			desired.NetworkInterfaceCount,
			actual.NetworkInterfaceCount,
		) ||
		configuredPositiveFloatRangeDrifted(
			desired.TotalLocalStorageGB,
			actual.TotalLocalStorageGB,
		) {
		return true
	}
	return configuredInstanceRequirementsScalarDrifted(desired, actual)
}

func configuredMemoryMiBDrifted(
	desired, actual CapacityProviderMemoryMiBRequest,
) bool {
	return runtime.Changed(actual.Min, desired.Min) ||
		configuredPositiveInt64Drifted(desired.Max, actual.Max)
}

func configuredVCpuCountDrifted(
	desired, actual CapacityProviderVCpuCountRangeRequest,
) bool {
	return runtime.Changed(actual.Min, desired.Min) ||
		configuredPositiveInt64Drifted(desired.Max, actual.Max)
}

func configuredInstanceRequirementsScalarDrifted(
	desired, actual *CapacityProviderInstanceRequirementsRequest,
) bool {
	if configuredStringSliceDrifted(
		desired.AcceleratorManufacturers,
		actual.AcceleratorManufacturers,
	) || configuredStringSliceDrifted(desired.AcceleratorNames, actual.AcceleratorNames) ||
		configuredStringSliceDrifted(desired.AcceleratorTypes, actual.AcceleratorTypes) ||
		configuredStringSliceDrifted(desired.AllowedInstanceTypes, actual.AllowedInstanceTypes) ||
		configuredStringSliceDrifted(desired.CpuManufacturers, actual.CpuManufacturers) ||
		configuredStringSliceDrifted(desired.ExcludedInstanceTypes, actual.ExcludedInstanceTypes) ||
		configuredStringSliceDrifted(desired.InstanceGenerations, actual.InstanceGenerations) ||
		configuredStringSliceDrifted(desired.LocalStorageTypes, actual.LocalStorageTypes) {
		return true
	}
	if configuredPtrDrifted(desired.BareMetal, actual.BareMetal) ||
		configuredPtrDrifted(desired.BurstablePerformance, actual.BurstablePerformance) ||
		configuredPtrDrifted(desired.LocalStorage, actual.LocalStorage) ||
		configuredPtrDrifted(desired.RequireHibernateSupport, actual.RequireHibernateSupport) {
		return true
	}
	return configuredPositiveInt64Drifted(
		desired.MaxSpotPriceAsPercentageOfOptimalOnDemandPrice,
		actual.MaxSpotPriceAsPercentageOfOptimalOnDemandPrice,
	) || configuredPositiveInt64Drifted(
		desired.OnDemandMaxPricePercentageOverLowestPrice,
		actual.OnDemandMaxPricePercentageOverLowestPrice,
	) || configuredPositiveInt64Drifted(
		desired.SpotMaxPricePercentageOverLowestPrice,
		actual.SpotMaxPricePercentageOverLowestPrice,
	)
}

func configuredStringSliceDrifted(desired, actual []string) bool {
	return len(desired) > 0 && runtime.Changed(actual, desired)
}

func configuredPtrDrifted[T comparable](desired, actual *T) bool {
	return desired != nil && runtime.Changed(actual, desired)
}

func configuredPositiveInt64Drifted(desired, actual *int64) bool {
	return desired != nil && *desired > 0 && runtime.Changed(actual, desired)
}

func configuredPositiveFloat64Drifted(desired, actual *float64) bool {
	return desired != nil && *desired > 0 && runtime.Changed(actual, desired)
}

func configuredZeroIntRangeDrifted[R interface{ zeroIntRange() }](desired, actual *R) bool {
	if desired == nil {
		return false
	}
	if actual == nil {
		return zeroIntRangeConfigured(*desired)
	}
	return configuredPtrDrifted(zeroIntRangeMin(*desired), zeroIntRangeMin(*actual)) ||
		configuredPtrDrifted(zeroIntRangeMax(*desired), zeroIntRangeMax(*actual))
}

func configuredPositiveIntRangeDrifted[R interface{ positiveIntRange() }](
	desired, actual *R,
) bool {
	if desired == nil {
		return false
	}
	if actual == nil {
		return positiveIntRangeConfigured(*desired)
	}
	return configuredPositiveInt64Drifted(
		positiveIntRangeMin(*desired),
		positiveIntRangeMin(*actual),
	) || configuredPositiveInt64Drifted(
		positiveIntRangeMax(*desired),
		positiveIntRangeMax(*actual),
	)
}

func configuredPositiveFloatRangeDrifted[R interface{ positiveFloatRange() }](
	desired, actual *R,
) bool {
	if desired == nil {
		return false
	}
	if actual == nil {
		return positiveFloatRangeConfigured(*desired)
	}
	return configuredPositiveFloat64Drifted(
		positiveFloatRangeMin(*desired),
		positiveFloatRangeMin(*actual),
	) || configuredPositiveFloat64Drifted(
		positiveFloatRangeMax(*desired),
		positiveFloatRangeMax(*actual),
	)
}

func (r CapacityProviderAcceleratorCountRequest) zeroIntRange()             {}
func (r CapacityProviderAcceleratorTotalMemoryMiBRequest) zeroIntRange()    {}
func (r CapacityProviderBaselineEbsBandwidthMbpsRequest) positiveIntRange() {}
func (r CapacityProviderNetworkInterfaceCountRequest) positiveIntRange()    {}
func (r CapacityProviderMemoryGiBPerVCpuRequest) positiveFloatRange()       {}
func (r CapacityProviderNetworkBandwidthGbpsRequest) positiveFloatRange()   {}
func (r CapacityProviderTotalLocalStorageGBRequest) positiveFloatRange()    {}

func zeroIntRangeConfigured[R interface{ zeroIntRange() }](r R) bool {
	return zeroIntRangeMin(r) != nil || zeroIntRangeMax(r) != nil
}

func positiveIntRangeConfigured[R interface{ positiveIntRange() }](r R) bool {
	return positiveIntRangePointerConfigured(positiveIntRangeMin(r)) ||
		positiveIntRangePointerConfigured(positiveIntRangeMax(r))
}

func positiveFloatRangeConfigured[R interface{ positiveFloatRange() }](r R) bool {
	return positiveFloatRangePointerConfigured(positiveFloatRangeMin(r)) ||
		positiveFloatRangePointerConfigured(positiveFloatRangeMax(r))
}

func positiveIntRangePointerConfigured(v *int64) bool {
	return v != nil && *v > 0
}

func positiveFloatRangePointerConfigured(v *float64) bool {
	return v != nil && *v > 0
}

func zeroIntRangeMin[R interface{ zeroIntRange() }](r R) *int64 {
	switch v := any(r).(type) {
	case CapacityProviderAcceleratorCountRequest:
		return v.Min
	case CapacityProviderAcceleratorTotalMemoryMiBRequest:
		return v.Min
	default:
		return nil
	}
}

func zeroIntRangeMax[R interface{ zeroIntRange() }](r R) *int64 {
	switch v := any(r).(type) {
	case CapacityProviderAcceleratorCountRequest:
		return v.Max
	case CapacityProviderAcceleratorTotalMemoryMiBRequest:
		return v.Max
	default:
		return nil
	}
}

func positiveIntRangeMin[R interface{ positiveIntRange() }](r R) *int64 {
	switch v := any(r).(type) {
	case CapacityProviderBaselineEbsBandwidthMbpsRequest:
		return v.Min
	case CapacityProviderNetworkInterfaceCountRequest:
		return v.Min
	default:
		return nil
	}
}

func positiveIntRangeMax[R interface{ positiveIntRange() }](r R) *int64 {
	switch v := any(r).(type) {
	case CapacityProviderBaselineEbsBandwidthMbpsRequest:
		return v.Max
	case CapacityProviderNetworkInterfaceCountRequest:
		return v.Max
	default:
		return nil
	}
}

func positiveFloatRangeMin[R interface{ positiveFloatRange() }](r R) *float64 {
	switch v := any(r).(type) {
	case CapacityProviderMemoryGiBPerVCpuRequest:
		return v.Min
	case CapacityProviderNetworkBandwidthGbpsRequest:
		return v.Min
	case CapacityProviderTotalLocalStorageGBRequest:
		return v.Min
	default:
		return nil
	}
}

func positiveFloatRangeMax[R interface{ positiveFloatRange() }](r R) *float64 {
	switch v := any(r).(type) {
	case CapacityProviderMemoryGiBPerVCpuRequest:
		return v.Max
	case CapacityProviderNetworkBandwidthGbpsRequest:
		return v.Max
	case CapacityProviderTotalLocalStorageGBRequest:
		return v.Max
	default:
		return nil
	}
}

func (r *CapacityProvider) read(
	ctx context.Context, client *ecs.Client, capacityProviderArn string, includeTags bool,
) (*CapacityProviderOutput, error) {
	cp, err := findCapacityProvider(ctx, client, capacityProviderArn, includeTags)
	if err != nil {
		return nil, err
	}
	return capacityProviderOutput(cp), nil
}

func capacityProviderOutput(cp *ecstypes.CapacityProvider) *CapacityProviderOutput {
	arn := aws.ToString(cp.CapacityProviderArn)
	return &CapacityProviderOutput{
		Arn:                      arn,
		CapacityProviderArn:      arn,
		Status:                   string(cp.Status),
		UpdateStatus:             string(cp.UpdateStatus),
		UpdateStatusReason:       cp.UpdateStatusReason,
		Tags:                     capacityProviderTagsOutput(cp.Tags),
		AutoScalingGroupProvider: capacityProviderASGOutput(cp.AutoScalingGroupProvider),
		ManagedInstancesProvider: capacityProviderManagedInstancesOutput(cp.ManagedInstancesProvider),
	}
}

func capacityProviderTagsOutput(tags []ecstypes.Tag) map[string]string {
	out := map[string]string{}
	for _, tag := range tags {
		key := aws.ToString(tag.Key)
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	return out
}

func capacityProviderUserTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func findCapacityProvider(
	ctx context.Context, client *ecs.Client, capacityProviderArn string, includeTags bool,
) (*ecstypes.CapacityProvider, error) {
	in := &ecs.DescribeCapacityProvidersInput{
		CapacityProviders: []string{capacityProviderArn},
	}
	if includeTags {
		in.Include = []ecstypes.CapacityProviderField{ecstypes.CapacityProviderFieldTags}
	}
	resp, err := client.DescribeCapacityProviders(ctx, in)
	if err != nil && includeTags && partition.UnsupportedOperation(region(client), err) {
		in.Include = nil
		resp, err = client.DescribeCapacityProviders(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("describe capacity providers: %w", err)
	}
	if len(resp.CapacityProviders) != 1 {
		return nil, runtime.ErrNotFound
	}
	cp := resp.CapacityProviders[0]
	if cp.Status == ecstypes.CapacityProviderStatusInactive {
		return nil, runtime.ErrNotFound
	}
	return &cp, nil
}

type capacityProviderFinder func(context.Context) (*ecstypes.CapacityProvider, error)

func waitCapacityProviderUpdated(
	ctx context.Context, client *ecs.Client, capacityProviderArn string,
) error {
	return waitCapacityProviderUpdatedWithFinder(
		ctx,
		capacityProviderArn,
		capacityProviderUpdateTimeout,
		10*time.Second,
		func(ctx context.Context) (*ecstypes.CapacityProvider, error) {
			return findCapacityProvider(ctx, client, capacityProviderArn, false)
		},
	)
}

func waitCapacityProviderUpdatedWithFinder(
	ctx context.Context,
	capacityProviderArn string,
	timeout time.Duration,
	interval time.Duration,
	find capacityProviderFinder,
) error {
	var lastReason string
	notFoundCount := 0
	err := wait.Until(ctx, fmt.Sprintf("capacity provider %s to update", capacityProviderArn),
		func(ctx context.Context) (bool, error) {
			cp, err := find(ctx)
			if err != nil {
				if errors.Is(err, runtime.ErrNotFound) {
					return capacityProviderUpdateNotFound(capacityProviderArn, &notFoundCount)
				}
				return false, err
			}
			if cp == nil {
				return capacityProviderUpdateNotFound(capacityProviderArn, &notFoundCount)
			}
			notFoundCount = 0
			if cp.UpdateStatusReason != nil {
				lastReason = aws.ToString(cp.UpdateStatusReason)
			}
			switch cp.UpdateStatus {
			case ecstypes.CapacityProviderUpdateStatusUpdateComplete:
				return true, nil
			case ecstypes.CapacityProviderUpdateStatusUpdateInProgress:
				return false, nil
			default:
				return false, fmt.Errorf("capacity provider %s update status %s",
					capacityProviderArn, cp.UpdateStatus)
			}
		},
		wait.WithTimeout(timeout),
		wait.WithInterval(interval),
	)
	if err != nil && lastReason != "" {
		return fmt.Errorf("%w: %s", err, lastReason)
	}
	return err
}

func capacityProviderUpdateNotFound(
	capacityProviderArn string, notFoundCount *int,
) (bool, error) {
	*notFoundCount = *notFoundCount + 1
	if *notFoundCount > capacityProviderUpdateNotFoundLimit {
		return false, fmt.Errorf(
			"capacity provider %s not found after %d checks: %w",
			capacityProviderArn,
			capacityProviderUpdateNotFoundLimit,
			runtime.ErrNotFound,
		)
	}
	return false, nil
}

func waitCapacityProviderDeleted(
	ctx context.Context, client *ecs.Client, capacityProviderArn string,
) error {
	return wait.Until(ctx, fmt.Sprintf("capacity provider %s to be deleted", capacityProviderArn),
		func(ctx context.Context) (bool, error) {
			cp, err := findCapacityProvider(ctx, client, capacityProviderArn, false)
			if err != nil {
				if errors.Is(err, runtime.ErrNotFound) {
					return true, nil
				}
				return false, err
			}
			switch cp.Status {
			case ecstypes.CapacityProviderStatusActive,
				ecstypes.CapacityProviderStatusDeprovisioning:
				return false, nil
			default:
				return false, fmt.Errorf(
					"capacity provider %s entered unexpected status %s while deleting",
					capacityProviderArn, cp.Status)
			}
		},
		wait.WithTimeout(capacityProviderDeleteTimeout),
		wait.WithInterval(10*time.Second),
	)
}

func capacityProviderUpdateRetryable(err error) bool {
	var updateInProgress *ecstypes.UpdateInProgressException
	return errors.As(err, &updateInProgress)
}

func capacityProviderAlreadyDeleted(err error) bool {
	var clientErr *ecstypes.ClientException
	return errors.As(err, &clientErr) &&
		strings.Contains(clientErr.ErrorMessage(), "capacity provider does not exist")
}

func (r *CapacityProvider) validate() error {
	if !capacityProviderNameRegexp.MatchString(r.Name) {
		return fmt.Errorf("name %q must match %s", r.Name, capacityProviderNameRegexp.String())
	}
	if r.Cluster != nil && !capacityProviderClusterRegexp.MatchString(aws.ToString(r.Cluster)) {
		return fmt.Errorf("cluster %q must match %s", aws.ToString(r.Cluster),
			capacityProviderClusterRegexp.String())
	}
	if p := r.AutoScalingGroupProvider; p != nil {
		if err := validateCapacityProviderARN(
			"auto-scaling-group-provider.auto-scaling-group-arn",
			p.AutoScalingGroupArn,
		); err != nil {
			return err
		}
	}
	if p := r.ManagedInstancesProvider; p != nil {
		if err := validateCapacityProviderARN("managed-instances-provider.infrastructure-role-arn",
			p.InfrastructureRoleArn); err != nil {
			return err
		}
		if err := validateCapacityProviderARN(
			"managed-instances-provider.instance-launch-template.ec2-instance-profile-arn",
			p.InstanceLaunchTemplate.Ec2InstanceProfileArn,
		); err != nil {
			return err
		}
		reservations := p.InstanceLaunchTemplate.CapacityReservations
		if reservations != nil && reservations.ReservationGroupArn != nil {
			if err := validateCapacityProviderARN(
				"managed-instances-provider.instance-launch-template.capacity-reservations"+
					".reservation-group-arn",
				aws.ToString(reservations.ReservationGroupArn),
			); err != nil {
				return err
			}
		}
		if reqs := p.InstanceLaunchTemplate.InstanceRequirements; reqs != nil {
			if err := validateCapacityProviderInstanceTypes(
				"allowed-instance-types", reqs.AllowedInstanceTypes); err != nil {
				return err
			}
			if err := validateCapacityProviderInstanceTypes(
				"excluded-instance-types", reqs.ExcludedInstanceTypes); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateCapacityProviderARN(field, value string) error {
	if !validCapacityProviderARN(value) {
		return fmt.Errorf("%s must be a valid ARN", field)
	}
	return nil
}

func validCapacityProviderARN(value string) bool {
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return false
	}
	if !capacityProviderARNPartitionRegexp.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !capacityProviderARNRegionRegexp.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" && !capacityProviderARNAccountRegexp.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}

func validateCapacityProviderInstanceTypes(field string, values []string) error {
	if len(values) > 400 {
		return fmt.Errorf("%s allows at most 400 entries", field)
	}
	for _, value := range values {
		if len(value) < 1 || len(value) > 30 {
			return fmt.Errorf("%s entry %q must be between 1 and 30 characters", field, value)
		}
		if !capacityProviderInstanceTypeRegexp.MatchString(value) {
			return fmt.Errorf("%s entry %q must match %s", field, value,
				capacityProviderInstanceTypeRegexp.String())
		}
	}
	return nil
}
