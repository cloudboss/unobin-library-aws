package eks

import (
	"context"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

func (r *ClusterResource) updateCluster(
	ctx context.Context,
	client eksClient,
	prior runtime.Prior[ClusterResource, *ClusterResourceOutput],
	clock clusterClock,
) (*ClusterResourceOutput, error) {
	if err := conditionalClusterReplacement(prior.Inputs, *r); err != nil {
		return nil, err
	}
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	name, err := clusterUpdateName(prior.Outputs)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		arn, err := clusterTagARN(prior)
		if err != nil {
			return nil, err
		}
		if err := syncClusterTags(ctx, client, arn, valueMapOrNil(r.Tags)); err != nil {
			return nil, err
		}
	}
	if r.Version != nil && runtime.Changed(prior.Inputs.Version, r.Version) {
		input := &eks.UpdateClusterVersionInput{
			Name: aws.String(name), Version: r.Version, Force: aws.ToBool(r.ForceUpdateVersion),
		}
		if err := runClusterUpdate(ctx, client, name, "version", clock,
			func(ctx context.Context) (*ekstypes.Update, error) {
				output, err := client.UpdateClusterVersion(ctx, input)
				if output == nil {
					return nil, err
				}
				return output.Update, err
			}); err != nil {
			return nil, err
		}
	}
	if accessConfigurationChanged(prior.Inputs.AccessConfig, r.AccessConfig) {
		input := &eks.UpdateClusterConfigInput{
			Name: aws.String(name), AccessConfig: updateAccessInput(r.AccessConfig),
		}
		if err := runClusterConfigUpdate(ctx, client, name, "access", input, clock); err != nil {
			return nil, err
		}
	}
	if autoModeChanged(prior.Inputs, *r) {
		compute, network, storage := r.autoModeInput()
		input := &eks.UpdateClusterConfigInput{
			Name:                    aws.String(name),
			ComputeConfig:           compute,
			KubernetesNetworkConfig: network,
			StorageConfig:           storage,
		}
		if err := runClusterConfigUpdate(ctx, client, name, "Auto Mode", input, clock); err != nil {
			return nil, err
		}
	}
	if scalingConfigurationChanged(
		prior.Inputs.ControlPlaneScalingConfig,
		r.ControlPlaneScalingConfig,
	) {
		input := &eks.UpdateClusterConfigInput{
			Name:                      aws.String(name),
			ControlPlaneScalingConfig: scalingInput(r.ControlPlaneScalingConfig),
		}
		if err := runClusterConfigUpdate(
			ctx, client, name, "control plane scaling", input, clock,
		); err != nil {
			return nil, err
		}
	}
	if r.DeletionProtection != nil &&
		runtime.Changed(prior.Inputs.DeletionProtection, r.DeletionProtection) {
		input := &eks.UpdateClusterConfigInput{
			Name: aws.String(name), DeletionProtection: r.DeletionProtection,
		}
		if err := runClusterConfigUpdate(
			ctx, client, name, "deletion protection", input, clock,
		); err != nil {
			return nil, err
		}
	}
	if prior.Inputs.EncryptionConfig == nil && r.EncryptionConfig != nil {
		input := &eks.AssociateEncryptionConfigInput{
			ClusterName: aws.String(name), EncryptionConfig: encryptionInput(r.EncryptionConfig),
		}
		if err := runClusterUpdate(ctx, client, name, "encryption", clock,
			func(ctx context.Context) (*ekstypes.Update, error) {
				output, err := client.AssociateEncryptionConfig(ctx, input)
				if output == nil {
					return nil, err
				}
				return output.Update, err
			}); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.EnabledClusterLogTypes, r.EnabledClusterLogTypes) {
		input := &eks.UpdateClusterConfigInput{
			Name:    aws.String(name),
			Logging: clusterLogging(valueOrNil(r.EnabledClusterLogTypes)),
		}
		if err := runClusterConfigUpdate(ctx, client, name, "logging", input, clock); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.RemoteNetworkConfig, r.RemoteNetworkConfig) {
		input := &eks.UpdateClusterConfigInput{
			Name:                aws.String(name),
			RemoteNetworkConfig: remoteNetworkInput(r.RemoteNetworkConfig, true),
		}
		if err := runClusterConfigUpdate(ctx, client, name, "remote networks", input, clock); err != nil {
			return nil, err
		}
	}
	if upgradePolicyChanged(prior.Inputs.UpgradePolicy, r.UpgradePolicy) {
		input := &eks.UpdateClusterConfigInput{
			Name: aws.String(name), UpgradePolicy: upgradeInput(r.UpgradePolicy),
		}
		if err := runClusterConfigUpdate(ctx, client, name, "upgrade policy", input, clock); err != nil {
			return nil, err
		}
	}
	if egressChanged(prior.Inputs.VPCConfig, r.VPCConfig) {
		input := &eks.UpdateClusterConfigInput{
			Name: aws.String(name),
			ResourcesVpcConfig: &ekstypes.VpcConfigRequest{
				ControlPlaneEgressMode: ekstypes.ControlPlaneEgressModeType(
					*r.VPCConfig.ControlPlaneEgressMode,
				),
			},
		}
		if err := runClusterConfigUpdate(ctx, client, name, "VPC egress", input, clock); err != nil {
			return nil, err
		}
	}
	if endpointConfigurationChanged(prior.Inputs.VPCConfig, r.VPCConfig) {
		vpc := &ekstypes.VpcConfigRequest{
			EndpointPrivateAccess: aws.Bool(r.VPCConfig.EndpointPrivateAccess),
			EndpointPublicAccess:  aws.Bool(r.VPCConfig.EndpointPublicAccess),
		}
		if r.VPCConfig.PublicAccessCIDRs != nil {
			vpc.PublicAccessCidrs = slices.Clone(*r.VPCConfig.PublicAccessCIDRs)
		}
		input := &eks.UpdateClusterConfigInput{
			Name: aws.String(name), ResourcesVpcConfig: vpc,
		}
		if err := runClusterConfigUpdate(ctx, client, name, "VPC endpoint", input, clock); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.VPCConfig.SubnetIds, r.VPCConfig.SubnetIds) {
		input := &eks.UpdateClusterConfigInput{
			Name: aws.String(name),
			ResourcesVpcConfig: &ekstypes.VpcConfigRequest{
				SubnetIds: slices.Clone(r.VPCConfig.SubnetIds),
			},
		}
		if err := runClusterConfigUpdate(ctx, client, name, "VPC subnets", input, clock); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(
		prior.Inputs.VPCConfig.SecurityGroupIds,
		r.VPCConfig.SecurityGroupIds,
	) {
		securityGroups := append([]string{}, valueOrNil(r.VPCConfig.SecurityGroupIds)...)
		input := &eks.UpdateClusterConfigInput{
			Name: aws.String(name),
			ResourcesVpcConfig: &ekstypes.VpcConfigRequest{
				SecurityGroupIds: securityGroups,
			},
		}
		if err := runClusterConfigUpdate(
			ctx, client, name, "VPC security groups", input, clock,
		); err != nil {
			return nil, err
		}
	}
	if r.ZonalShiftConfig != nil &&
		runtime.Changed(prior.Inputs.ZonalShiftConfig, r.ZonalShiftConfig) {
		input := &eks.UpdateClusterConfigInput{
			Name: aws.String(name), ZonalShiftConfig: zonalShiftInput(r.ZonalShiftConfig),
		}
		if err := runClusterConfigUpdate(ctx, client, name, "zonal shift", input, clock); err != nil {
			return nil, err
		}
	}
	return r.readWithClient(ctx, client, name)
}

func runClusterConfigUpdate(
	ctx context.Context,
	client eksClient,
	name string,
	description string,
	input *eks.UpdateClusterConfigInput,
	clock clusterClock,
) error {
	return runClusterUpdate(ctx, client, name, description, clock,
		func(ctx context.Context) (*ekstypes.Update, error) {
			output, err := client.UpdateClusterConfig(ctx, input)
			if output == nil {
				return nil, err
			}
			return output.Update, err
		})
}

func runClusterUpdate(
	ctx context.Context,
	client eksClient,
	name string,
	description string,
	clock clusterClock,
	update func(context.Context) (*ekstypes.Update, error),
) error {
	result, err := update(ctx)
	if err != nil {
		return fmt.Errorf("update cluster %s %s: %w", name, description, err)
	}
	if result == nil || aws.ToString(result.Id) == "" {
		return fmt.Errorf("update cluster %s %s: response has no update ID", name, description)
	}
	id := aws.ToString(result.Id)
	if err := waitClusterUpdate(ctx, client, name, id, clock); err != nil {
		return fmt.Errorf("wait for cluster %s %s update %s: %w",
			name, description, id, err)
	}
	return nil
}

func syncClusterTags(
	ctx context.Context,
	client eksClient,
	arn string,
	desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			output, err := client.ListTagsForResource(ctx, &eks.ListTagsForResourceInput{
				ResourceArn: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("list cluster tags: %w", err)
			}
			if output == nil {
				return map[string]string{}, nil
			}
			return output.Tags, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &eks.TagResourceInput{
				ResourceArn: aws.String(arn), Tags: upsert,
			})
			if err != nil {
				return fmt.Errorf("tag cluster: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &eks.UntagResourceInput{
				ResourceArn: aws.String(arn), TagKeys: remove,
			})
			if err != nil {
				return fmt.Errorf("untag cluster: %w", err)
			}
			return nil
		},
	)
}

func clusterUpdateName(output *ClusterResourceOutput) (string, error) {
	if output == nil || output.Name == "" {
		return "", fmt.Errorf("prior cluster output has no name")
	}
	return output.Name, nil
}

func clusterTagARN(
	prior runtime.Prior[ClusterResource, *ClusterResourceOutput],
) (string, error) {
	if prior.Observed != nil && prior.Observed.Arn != "" {
		return prior.Observed.Arn, nil
	}
	if prior.Outputs != nil && prior.Outputs.Arn != "" {
		return prior.Outputs.Arn, nil
	}
	return "", fmt.Errorf("cluster output has no ARN for tag update")
}

func accessConfigurationChanged(prior, current *ClusterAccessConfig) bool {
	return current != nil && current.AuthenticationMode != nil &&
		runtime.Changed(accessAuthenticationMode(prior), current.AuthenticationMode)
}

func accessAuthenticationMode(config *ClusterAccessConfig) *string {
	if config == nil {
		return nil
	}
	return config.AuthenticationMode
}

func autoModeChanged(prior, current ClusterResource) bool {
	priorCompute := computeInput(prior.ComputeConfig)
	currentCompute := computeInput(current.ComputeConfig)
	return runtime.Changed(priorCompute, currentCompute) ||
		loadBalancingEnabled(prior.KubernetesNetworkConfig) !=
			loadBalancingEnabled(current.KubernetesNetworkConfig) ||
		blockStorageEnabled(prior.StorageConfig) != blockStorageEnabled(current.StorageConfig)
}

func loadBalancingEnabled(config *ClusterKubernetesNetworkConfig) bool {
	return config != nil && config.ElasticLoadBalancing != nil &&
		config.ElasticLoadBalancing.Enabled
}

func blockStorageEnabled(config *ClusterStorageConfig) bool {
	return config != nil && config.BlockStorage != nil && config.BlockStorage.Enabled
}

func scalingConfigurationChanged(
	prior, current *ClusterControlPlaneScalingConfig,
) bool {
	return current != nil && current.Tier != nil &&
		runtime.Changed(scalingTier(prior), current.Tier)
}

func scalingTier(config *ClusterControlPlaneScalingConfig) *string {
	if config == nil {
		return nil
	}
	return config.Tier
}

func upgradePolicyChanged(prior, current *ClusterUpgradePolicy) bool {
	return current != nil && current.SupportType != nil &&
		runtime.Changed(upgradeSupportType(prior), current.SupportType)
}

func upgradeSupportType(policy *ClusterUpgradePolicy) *string {
	if policy == nil {
		return nil
	}
	return policy.SupportType
}

func egressChanged(prior, current ClusterVPCConfig) bool {
	return current.ControlPlaneEgressMode != nil &&
		runtime.Changed(prior.ControlPlaneEgressMode, current.ControlPlaneEgressMode)
}

func endpointConfigurationChanged(prior, current ClusterVPCConfig) bool {
	return runtime.Changed(prior.EndpointPrivateAccess, current.EndpointPrivateAccess) ||
		runtime.Changed(prior.EndpointPublicAccess, current.EndpointPublicAccess) ||
		(current.PublicAccessCIDRs != nil &&
			runtime.Changed(prior.PublicAccessCIDRs, current.PublicAccessCIDRs))
}
