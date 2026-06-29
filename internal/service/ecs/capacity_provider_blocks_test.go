package ecs

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapacityProviderManagedScalingSDKZeroConstruction(t *testing.T) {
	zero := int64(0)
	scaling := (&CapacityProviderManagedScaling{
		InstanceWarmupPeriod:   &zero,
		MaximumScalingStepSize: &zero,
		MinimumScalingStepSize: &zero,
		TargetCapacity:         &zero,
	}).sdk()

	require.NotNil(t, scaling.InstanceWarmupPeriod)
	assert.Equal(t, int32(0), aws.ToInt32(scaling.InstanceWarmupPeriod))
	assert.Nil(t, scaling.MaximumScalingStepSize)
	assert.Nil(t, scaling.MinimumScalingStepSize)
	assert.Nil(t, scaling.TargetCapacity)
}

func TestCapacityProviderInfrastructureOptimizationSDKZeroConstruction(t *testing.T) {
	for _, value := range []int64{-1, 0} {
		t.Run("scale-in-after", func(t *testing.T) {
			got := (&CapacityProviderInfrastructureOptimization{ScaleInAfter: &value}).sdk()

			require.NotNil(t, got.ScaleInAfter)
			assert.Equal(t, int32(value), aws.ToInt32(got.ScaleInAfter))
		})
	}
}

func TestCapacityProviderInstanceRequirementsSDKZeroConstruction(t *testing.T) {
	zero := int64(0)
	falseValue := false
	reqs := (&CapacityProviderInstanceRequirementsRequest{
		MemoryMiB:        CapacityProviderMemoryMiBRequest{Min: 1},
		VCpuCount:        CapacityProviderVCpuCountRangeRequest{Min: 1},
		AcceleratorCount: &CapacityProviderAcceleratorCountRequest{Min: &zero, Max: &zero},
		AcceleratorTotalMemoryMiB: &CapacityProviderAcceleratorTotalMemoryMiBRequest{
			Min: &zero,
			Max: &zero,
		},
		BaselineEbsBandwidthMbps: &CapacityProviderBaselineEbsBandwidthMbpsRequest{
			Min: &zero,
			Max: &zero,
		},
		RequireHibernateSupport:                        &falseValue,
		OnDemandMaxPricePercentageOverLowestPrice:      &zero,
		SpotMaxPricePercentageOverLowestPrice:          &zero,
		MaxSpotPriceAsPercentageOfOptimalOnDemandPrice: &zero,
	}).sdk()

	require.NotNil(t, reqs.AcceleratorCount.Min)
	assert.Equal(t, int32(0), aws.ToInt32(reqs.AcceleratorCount.Min))
	require.NotNil(t, reqs.AcceleratorCount.Max)
	assert.Equal(t, int32(0), aws.ToInt32(reqs.AcceleratorCount.Max))
	require.NotNil(t, reqs.AcceleratorTotalMemoryMiB.Min)
	assert.Equal(t, int32(0), aws.ToInt32(reqs.AcceleratorTotalMemoryMiB.Min))
	require.NotNil(t, reqs.AcceleratorTotalMemoryMiB.Max)
	assert.Equal(t, int32(0), aws.ToInt32(reqs.AcceleratorTotalMemoryMiB.Max))
	assert.Nil(t, reqs.BaselineEbsBandwidthMbps.Min)
	assert.Nil(t, reqs.BaselineEbsBandwidthMbps.Max)
	require.NotNil(t, reqs.RequireHibernateSupport)
	assert.False(t, aws.ToBool(reqs.RequireHibernateSupport))
	assert.Nil(t, reqs.OnDemandMaxPricePercentageOverLowestPrice)
	assert.Nil(t, reqs.SpotMaxPricePercentageOverLowestPrice)
	assert.Nil(t, reqs.MaxSpotPriceAsPercentageOfOptimalOnDemandPrice)
}

func TestCapacityProviderOutputIncludesArnAndLegacyHandle(t *testing.T) {
	arn := "arn:aws:ecs:us-east-1:123456789012:capacity-provider/example"
	got := capacityProviderOutput(&ecstypes.CapacityProvider{CapacityProviderArn: aws.String(arn)})

	require.NotNil(t, got)
	assert.Equal(t, arn, got.Arn)
	assert.Equal(t, arn, got.CapacityProviderArn)
	assert.Equal(t, arn, got.capacityProviderArn())
	assert.Equal(t, arn, (&CapacityProviderOutput{CapacityProviderArn: arn}).capacityProviderArn())
}

func TestCapacityProviderOutputIncludesObservedTags(t *testing.T) {
	got := capacityProviderOutput(&ecstypes.CapacityProvider{
		Tags: []ecstypes.Tag{
			{Key: aws.String("aws:owner"), Value: aws.String("system")},
			{Key: aws.String("env"), Value: aws.String("prod")},
		},
	})

	require.NotNil(t, got)
	assert.Equal(t, map[string]string{"env": "prod"}, got.Tags)
	assert.Equal(t, map[string]string{}, capacityProviderTagsOutput(nil))
}

func TestCapacityProviderASGOutputIncludesCompleteDefaultedBlock(t *testing.T) {
	got := capacityProviderASGOutput(&ecstypes.AutoScalingGroupProvider{
		AutoScalingGroupArn: aws.String("arn:aws:autoscaling:us-east-1:123456789012:x"),
	})

	require.NotNil(t, got)
	assert.Equal(t, "arn:aws:autoscaling:us-east-1:123456789012:x", got.AutoScalingGroupArn)
	assert.Equal(t, "ENABLED", aws.ToString(got.ManagedDraining))
	assert.Equal(t, "DISABLED", aws.ToString(got.ManagedTerminationProtection))
	require.NotNil(t, got.ManagedScaling)
	assert.Equal(t, int64(300), aws.ToInt64(got.ManagedScaling.InstanceWarmupPeriod))
	assert.Equal(t, int64(10000), aws.ToInt64(got.ManagedScaling.MaximumScalingStepSize))
	assert.Equal(t, int64(1), aws.ToInt64(got.ManagedScaling.MinimumScalingStepSize))
	assert.Equal(t, "DISABLED", aws.ToString(got.ManagedScaling.Status))
	assert.Equal(t, int64(100), aws.ToInt64(got.ManagedScaling.TargetCapacity))
}

func TestCapacityProviderManagedInstancesOutputIncludesCompleteDefaultedBlock(t *testing.T) {
	got := capacityProviderManagedInstancesOutput(&ecstypes.ManagedInstancesProvider{
		InfrastructureRoleArn: aws.String("arn:aws:iam::123456789012:role/ecs"),
		InstanceLaunchTemplate: &ecstypes.InstanceLaunchTemplate{
			Ec2InstanceProfileArn: aws.String("arn:aws:iam::123456789012:instance-profile/ecs"),
			NetworkConfiguration: &ecstypes.ManagedInstancesNetworkConfiguration{
				SecurityGroups: []string{"sg-1"},
				Subnets:        []string{"subnet-1"},
			},
		},
	})

	require.NotNil(t, got)
	assert.Equal(t, "arn:aws:iam::123456789012:role/ecs", got.InfrastructureRoleArn)
	assert.Equal(t, "ON_DEMAND", aws.ToString(got.InstanceLaunchTemplate.CapacityOptionType))
	assert.Equal(t, "arn:aws:iam::123456789012:instance-profile/ecs",
		got.InstanceLaunchTemplate.Ec2InstanceProfileArn)
	require.NotNil(t, got.InstanceLaunchTemplate.NetworkConfiguration.SecurityGroups)
	assert.Equal(t, []string{"sg-1"}, *got.InstanceLaunchTemplate.NetworkConfiguration.SecurityGroups)
	assert.Equal(t, []string{"subnet-1"}, got.InstanceLaunchTemplate.NetworkConfiguration.Subnets)
}

func TestCapacityProviderTagsNeedSync(t *testing.T) {
	tests := []struct {
		name     string
		previous map[string]string
		desired  map[string]string
		observed map[string]string
		want     bool
	}{
		{
			name:     "unchanged tags do not sync",
			previous: map[string]string{"env": "prod"},
			desired:  map[string]string{"env": "prod"},
			observed: map[string]string{"env": "prod"},
		},
		{
			name:     "input change syncs",
			previous: map[string]string{"env": "dev"},
			desired:  map[string]string{"env": "prod"},
			observed: map[string]string{"env": "dev"},
			want:     true,
		},
		{
			name:     "observed drift syncs with unchanged inputs",
			previous: map[string]string{"env": "prod"},
			desired:  map[string]string{"env": "prod"},
			observed: map[string]string{"env": "dev"},
			want:     true,
		},
		{
			name:     "removed observed tag syncs with unchanged inputs",
			previous: map[string]string{"env": "prod"},
			desired:  map[string]string{"env": "prod"},
			observed: map[string]string{},
			want:     true,
		},
		{
			name:     "system tags are ignored",
			previous: map[string]string{"aws:owner": "system"},
			desired:  map[string]string{},
			observed: map[string]string{"aws:owner": "system"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &CapacityProvider{Tags: &tt.desired}
			prior := runtime.Prior[CapacityProvider, *CapacityProviderOutput]{
				Inputs:   CapacityProvider{Tags: &tt.previous},
				Observed: &CapacityProviderOutput{Tags: tt.observed},
			}

			assert.Equal(t, tt.want, r.tagsNeedSync(prior))
		})
	}
}

func TestCapacityProviderConfiguredASGDrift(t *testing.T) {
	arn := "arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/asg"
	tests := []struct {
		name     string
		desired  *CapacityProviderAutoScalingGroupProvider
		observed *CapacityProviderAutoScalingGroupProvider
		want     bool
	}{
		{
			name: "configured managed draining drift updates",
			desired: &CapacityProviderAutoScalingGroupProvider{
				AutoScalingGroupArn: arn,
				ManagedDraining:     aws.String("ENABLED"),
			},
			observed: &CapacityProviderAutoScalingGroupProvider{
				AutoScalingGroupArn: arn,
				ManagedDraining:     aws.String("DISABLED"),
			},
			want: true,
		},
		{
			name:    "omitted managed draining accepts observed default",
			desired: &CapacityProviderAutoScalingGroupProvider{AutoScalingGroupArn: arn},
			observed: &CapacityProviderAutoScalingGroupProvider{
				AutoScalingGroupArn: arn,
				ManagedDraining:     aws.String("DISABLED"),
			},
			want: false,
		},
		{
			name: "configured managed scaling drift updates",
			desired: &CapacityProviderAutoScalingGroupProvider{
				AutoScalingGroupArn: arn,
				ManagedScaling: &CapacityProviderManagedScaling{
					Status: aws.String("ENABLED"),
				},
			},
			observed: &CapacityProviderAutoScalingGroupProvider{
				AutoScalingGroupArn: arn,
				ManagedScaling: &CapacityProviderManagedScaling{
					Status: aws.String("DISABLED"),
				},
			},
			want: true,
		},
		{
			name: "omitted managed scaling fields accept observed values",
			desired: &CapacityProviderAutoScalingGroupProvider{
				AutoScalingGroupArn: arn,
				ManagedScaling:      &CapacityProviderManagedScaling{},
			},
			observed: &CapacityProviderAutoScalingGroupProvider{
				AutoScalingGroupArn: arn,
				ManagedScaling: &CapacityProviderManagedScaling{
					Status: aws.String("DISABLED"),
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &CapacityProvider{AutoScalingGroupProvider: tt.desired}
			prior := runtime.Prior[CapacityProvider, *CapacityProviderOutput]{
				Inputs: CapacityProvider{
					AutoScalingGroupProvider: tt.desired,
				},
				Observed: &CapacityProviderOutput{
					AutoScalingGroupProvider: tt.observed,
				},
			}

			assert.Equal(t, tt.want, r.providerChanged(prior))
		})
	}
}

func TestCapacityProviderConfiguredManagedInstancesDrift(t *testing.T) {
	roleArn := "arn:aws:iam::123456789012:role/ecs"
	otherRoleArn := "arn:aws:iam::123456789012:role/other"
	profileArn := "arn:aws:iam::123456789012:instance-profile/ecs"
	otherProfileArn := "arn:aws:iam::123456789012:instance-profile/other"
	subnetID := "subnet-1"
	otherSubnetID := "subnet-2"
	spot := "SPOT"
	onDemand := "ON_DEMAND"
	detailed := "DETAILED"
	basic := "BASIC"
	managed := "CAPACITY_PROVIDER"
	none := "NONE"
	repairEnabled := "ENABLED"
	repairDisabled := "DISABLED"
	reservationsOnly := "RESERVATIONS_ONLY"
	reservationsFirst := "RESERVATIONS_FIRST"
	metadataTags := true
	otherMetadataTags := false
	scaleInAfter := int64(60)
	otherScaleInAfter := int64(120)
	memory := int64(1024)
	otherMemory := int64(2048)
	storage := int64(64)
	otherStorage := int64(128)

	provider := func() *CapacityProviderManagedInstancesProvider {
		return &CapacityProviderManagedInstancesProvider{
			InfrastructureRoleArn: roleArn,
			InstanceLaunchTemplate: CapacityProviderInstanceLaunchTemplate{
				CapacityOptionType:    &spot,
				Ec2InstanceProfileArn: profileArn,
				NetworkConfiguration: CapacityProviderManagedInstancesNetworkConfiguration{
					Subnets: []string{subnetID},
				},
			},
		}
	}

	tests := []struct {
		name     string
		mutate   func(*CapacityProviderManagedInstancesProvider)
		observed func() *CapacityProviderManagedInstancesProvider
		outputs  func() *CapacityProviderManagedInstancesProvider
		want     bool
	}{
		{
			name: "infrastructure role drift updates",
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InfrastructureRoleArn = otherRoleArn
				return p
			},
			want: true,
		},
		{
			name: "auto repair drift updates",
			mutate: func(p *CapacityProviderManagedInstancesProvider) {
				p.AutoRepairConfiguration = &CapacityProviderAutoRepairConfiguration{
					ActionsStatus: &repairEnabled,
				}
			},
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.AutoRepairConfiguration = &CapacityProviderAutoRepairConfiguration{
					ActionsStatus: &repairDisabled,
				}
				return p
			},
			want: true,
		},
		{
			name: "infrastructure optimization drift updates",
			mutate: func(p *CapacityProviderManagedInstancesProvider) {
				p.InfrastructureOptimization = &CapacityProviderInfrastructureOptimization{
					ScaleInAfter: &scaleInAfter,
				}
			},
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InfrastructureOptimization = &CapacityProviderInfrastructureOptimization{
					ScaleInAfter: &otherScaleInAfter,
				}
				return p
			},
			want: true,
		},
		{
			name: "propagate tags drift updates",
			mutate: func(p *CapacityProviderManagedInstancesProvider) {
				p.PropagateTags = &managed
			},
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.PropagateTags = &none
				return p
			},
			want: true,
		},
		{
			name: "launch template profile drift updates",
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InstanceLaunchTemplate.Ec2InstanceProfileArn = otherProfileArn
				return p
			},
			want: true,
		},
		{
			name: "capacity reservations drift updates",
			mutate: func(p *CapacityProviderManagedInstancesProvider) {
				p.InstanceLaunchTemplate.CapacityReservations =
					&CapacityProviderCapacityReservationRequest{
						ReservationPreference: &reservationsOnly,
					}
			},
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InstanceLaunchTemplate.CapacityReservations =
					&CapacityProviderCapacityReservationRequest{
						ReservationPreference: &reservationsFirst,
					}
				return p
			},
			want: true,
		},
		{
			name: "metadata tag propagation drift updates",
			mutate: func(p *CapacityProviderManagedInstancesProvider) {
				p.InstanceLaunchTemplate.InstanceMetadataTagsPropagation = &metadataTags
			},
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InstanceLaunchTemplate.InstanceMetadataTagsPropagation = &otherMetadataTags
				return p
			},
			want: true,
		},
		{
			name: "monitoring drift updates",
			mutate: func(p *CapacityProviderManagedInstancesProvider) {
				p.InstanceLaunchTemplate.Monitoring = &detailed
			},
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InstanceLaunchTemplate.Monitoring = &basic
				return p
			},
			want: true,
		},
		{
			name: "network drift updates",
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InstanceLaunchTemplate.NetworkConfiguration.Subnets = []string{otherSubnetID}
				return p
			},
			want: true,
		},
		{
			name: "storage drift updates",
			mutate: func(p *CapacityProviderManagedInstancesProvider) {
				p.InstanceLaunchTemplate.StorageConfiguration =
					&CapacityProviderManagedInstancesStorageConfiguration{
						StorageSizeGiB: storage,
					}
			},
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InstanceLaunchTemplate.StorageConfiguration =
					&CapacityProviderManagedInstancesStorageConfiguration{
						StorageSizeGiB: otherStorage,
					}
				return p
			},
			want: true,
		},
		{
			name: "local storage drift updates",
			mutate: func(p *CapacityProviderManagedInstancesProvider) {
				p.InstanceLaunchTemplate.LocalStorageConfiguration =
					&CapacityProviderManagedInstancesLocalStorageConfiguration{
						UseLocalStorage: true,
					}
			},
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InstanceLaunchTemplate.LocalStorageConfiguration =
					&CapacityProviderManagedInstancesLocalStorageConfiguration{}
				return p
			},
			want: true,
		},
		{
			name: "instance requirements drift updates",
			mutate: func(p *CapacityProviderManagedInstancesProvider) {
				p.InstanceLaunchTemplate.InstanceRequirements =
					&CapacityProviderInstanceRequirementsRequest{
						MemoryMiB: CapacityProviderMemoryMiBRequest{Min: memory},
						VCpuCount: CapacityProviderVCpuCountRangeRequest{Min: 1},
					}
			},
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InstanceLaunchTemplate.InstanceRequirements =
					&CapacityProviderInstanceRequirementsRequest{
						MemoryMiB: CapacityProviderMemoryMiBRequest{Min: otherMemory},
						VCpuCount: CapacityProviderVCpuCountRangeRequest{Min: 1},
					}
				return p
			},
			want: true,
		},
		{
			name: "capacity option drift is create only",
			observed: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InstanceLaunchTemplate.CapacityOptionType = &onDemand
				return p
			},
			want: false,
		},
		{
			name:     "observed state wins over stale output drift",
			observed: provider,
			outputs: func() *CapacityProviderManagedInstancesProvider {
				p := provider()
				p.InfrastructureRoleArn = otherRoleArn
				return p
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := provider()
			if tt.mutate != nil {
				tt.mutate(desired)
			}
			observed := desired
			if tt.observed != nil {
				observed = tt.observed()
			}
			outputs := observed
			if tt.outputs != nil {
				outputs = tt.outputs()
			}
			r := &CapacityProvider{ManagedInstancesProvider: desired}
			prior := runtime.Prior[CapacityProvider, *CapacityProviderOutput]{
				Inputs: CapacityProvider{
					ManagedInstancesProvider: desired,
				},
				Outputs: &CapacityProviderOutput{
					ManagedInstancesProvider: outputs,
				},
				Observed: &CapacityProviderOutput{
					ManagedInstancesProvider: observed,
				},
			}

			assert.Equal(t, tt.want, r.providerChanged(prior))
		})
	}
}

func TestWaitCapacityProviderUpdatedToleratesTransientNotFound(t *testing.T) {
	calls := 0
	err := waitCapacityProviderUpdatedWithFinder(
		context.Background(),
		"arn:aws:ecs:us-east-1:123456789012:capacity-provider/example",
		time.Second,
		0,
		func(context.Context) (*ecstypes.CapacityProvider, error) {
			calls++
			if calls == 1 {
				return nil, nil
			}
			if calls <= 3 {
				return nil, runtime.ErrNotFound
			}
			return &ecstypes.CapacityProvider{
				UpdateStatus: ecstypes.CapacityProviderUpdateStatusUpdateComplete,
			}, nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, 4, calls)
}

func TestWaitCapacityProviderUpdatedBoundsTransientNotFound(t *testing.T) {
	calls := 0
	err := waitCapacityProviderUpdatedWithFinder(
		context.Background(),
		"arn:aws:ecs:us-east-1:123456789012:capacity-provider/example",
		time.Second,
		0,
		func(context.Context) (*ecstypes.CapacityProvider, error) {
			calls++
			return nil, runtime.ErrNotFound
		},
	)

	require.ErrorIs(t, err, runtime.ErrNotFound)
	assert.Equal(t, capacityProviderUpdateNotFoundLimit+1, calls)
}

func TestWaitCapacityProviderUpdatedResetsTransientNotFound(t *testing.T) {
	calls := 0
	err := waitCapacityProviderUpdatedWithFinder(
		context.Background(),
		"arn:aws:ecs:us-east-1:123456789012:capacity-provider/example",
		time.Second,
		0,
		func(context.Context) (*ecstypes.CapacityProvider, error) {
			calls++
			switch {
			case calls <= capacityProviderUpdateNotFoundLimit:
				return nil, runtime.ErrNotFound
			case calls == capacityProviderUpdateNotFoundLimit+1:
				return &ecstypes.CapacityProvider{
					UpdateStatus: ecstypes.CapacityProviderUpdateStatusUpdateInProgress,
				}, nil
			case calls <= 2*capacityProviderUpdateNotFoundLimit+1:
				return nil, runtime.ErrNotFound
			default:
				return &ecstypes.CapacityProvider{
					UpdateStatus: ecstypes.CapacityProviderUpdateStatusUpdateComplete,
				}, nil
			}
		},
	)

	require.NoError(t, err)
	assert.Equal(t, 2*capacityProviderUpdateNotFoundLimit+2, calls)
}

func TestValidCapacityProviderARN(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "role arn", value: "arn:aws:iam::123456789012:role/ecs", want: true},
		{name: "gov partition", value: "arn:aws-us-gov:iam::aws:policy/Admin", want: true},
		{name: "empty region and account", value: "arn:aws:s3:::bucket/example", want: true},
		{name: "cw account", value: "arn:aws:logs:us-east-1:cw1234567890:log-group/x", want: true},
		{name: "empty service", value: "arn:aws::us-east-1:123456789012:thing", want: true},
		{name: "empty", value: "", want: false},
		{name: "missing arn prefix", value: "iam::123456789012:role/ecs", want: false},
		{name: "missing partition", value: "arn::iam::123456789012:role/ecs", want: false},
		{name: "invalid partition", value: "arn:aws123:iam::123456789012:role/ecs", want: false},
		{name: "invalid region", value: "arn:aws:iam:useast1:123456789012:role/ecs", want: false},
		{
			name:  "future canonical region",
			value: "arn:aws:iam:us-foo-1:123456789012:role/ecs",
			want:  true,
		},
		{name: "invalid account", value: "arn:aws:iam::123:role/ecs", want: false},
		{name: "missing resource", value: "arn:aws:iam::123456789012:", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validCapacityProviderARN(tt.value))
		})
	}
}

func TestValidateCapacityProviderInstanceTypes(t *testing.T) {
	long := "0123456789012345678901234567890"
	tests := []struct {
		name    string
		values  []string
		wantErr string
	}{
		{name: "valid", values: []string{"m7i.*", "c7g-large"}},
		{name: "empty", values: []string{""}, wantErr: "between 1 and 30 characters"},
		{name: "too long", values: []string{long}, wantErr: "between 1 and 30 characters"},
		{name: "bad character", values: []string{"m7i/large"}, wantErr: "must match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCapacityProviderInstanceTypes("allowed-instance-types", tt.values)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCapacityProviderTagsSDKDropsSystemTags(t *testing.T) {
	got := tagsSDK(map[string]string{
		"aws:owner": "system",
		"env":       "prod",
	})

	require.Len(t, got, 1)
	assert.Equal(t, "env", aws.ToString(got[0].Key))
	assert.Equal(t, "prod", aws.ToString(got[0].Value))
	assert.Nil(t, tagsSDK(map[string]string{"aws:owner": "system"}))
}
