package eks

import (
	"maps"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

func (r *ClusterResource) createInput() *eks.CreateClusterInput {
	compute, network, storage := r.autoModeInput()
	input := &eks.CreateClusterInput{
		BootstrapSelfManagedAddons: aws.Bool(r.BootstrapSelfManagedAddons),
		ComputeConfig:              compute,
		EncryptionConfig:           encryptionInput(r.EncryptionConfig),
		KubernetesNetworkConfig:    network,
		Logging:                    clusterLogging(valueOrNil(r.EnabledClusterLogTypes)),
		Name:                       aws.String(r.Name),
		ResourcesVpcConfig:         vpcInput(r.VPCConfig),
		RoleArn:                    aws.String(r.RoleArn),
		StorageConfig:              storage,
		Tags:                       clusterTags(valueMapOrNil(r.Tags)),
	}
	if r.AccessConfig != nil {
		input.AccessConfig = createAccessInput(r.AccessConfig)
	}
	if r.ControlPlaneScalingConfig != nil {
		input.ControlPlaneScalingConfig = scalingInput(r.ControlPlaneScalingConfig)
	}
	input.DeletionProtection = r.DeletionProtection
	input.OutpostConfig = outpostInput(r.OutpostConfig)
	input.RemoteNetworkConfig = remoteNetworkInput(r.RemoteNetworkConfig, false)
	input.UpgradePolicy = upgradeInput(r.UpgradePolicy)
	input.Version = r.Version
	input.ZonalShiftConfig = zonalShiftInput(r.ZonalShiftConfig)
	return input
}

func (r *ClusterResource) autoModeInput() (
	*ekstypes.ComputeConfigRequest,
	*ekstypes.KubernetesNetworkConfigRequest,
	*ekstypes.StorageConfigRequest,
) {
	return computeInput(r.ComputeConfig),
		kubernetesNetworkInput(r.KubernetesNetworkConfig),
		storageInput(r.StorageConfig)
}

func clusterLogging(enabled []string) *ekstypes.Logging {
	all := []ekstypes.LogType{
		ekstypes.LogTypeApi,
		ekstypes.LogTypeAudit,
		ekstypes.LogTypeAuthenticator,
		ekstypes.LogTypeControllerManager,
		ekstypes.LogTypeScheduler,
	}
	enabledSet := make(map[ekstypes.LogType]bool, len(enabled))
	var enabledTypes []ekstypes.LogType
	for _, value := range enabled {
		logType := ekstypes.LogType(value)
		if !enabledSet[logType] {
			enabledTypes = append(enabledTypes, logType)
			enabledSet[logType] = true
		}
	}
	disabledTypes := make([]ekstypes.LogType, 0, len(all)-len(enabledTypes))
	for _, logType := range all {
		if !enabledSet[logType] {
			disabledTypes = append(disabledTypes, logType)
		}
	}
	setups := make([]ekstypes.LogSetup, 0, 2)
	if len(enabledTypes) > 0 {
		setups = append(setups, ekstypes.LogSetup{
			Enabled: aws.Bool(true), Types: enabledTypes,
		})
	}
	if len(disabledTypes) > 0 {
		setups = append(setups, ekstypes.LogSetup{
			Enabled: aws.Bool(false), Types: disabledTypes,
		})
	}
	return &ekstypes.Logging{ClusterLogging: setups}
}

func remoteNetworkInput(
	config *ClusterRemoteNetworkConfig,
	includeEmpty bool,
) *ekstypes.RemoteNetworkConfigRequest {
	if config == nil && !includeEmpty {
		return nil
	}
	input := &ekstypes.RemoteNetworkConfigRequest{
		RemoteNodeNetworks: []ekstypes.RemoteNodeNetwork{},
		RemotePodNetworks:  []ekstypes.RemotePodNetwork{},
	}
	if config == nil {
		return input
	}
	input.RemoteNodeNetworks = remoteNodeInputs(valueOrNil(config.RemoteNodeNetworks))
	input.RemotePodNetworks = remotePodInputs(valueOrNil(config.RemotePodNetworks))
	return input
}

func createAccessInput(config *ClusterAccessConfig) *ekstypes.CreateAccessConfigRequest {
	input := &ekstypes.CreateAccessConfigRequest{
		BootstrapClusterCreatorAdminPermissions: config.BootstrapClusterCreatorAdminPermissions,
	}
	if config.AuthenticationMode != nil {
		input.AuthenticationMode = ekstypes.AuthenticationMode(*config.AuthenticationMode)
	}
	return input
}

func updateAccessInput(config *ClusterAccessConfig) *ekstypes.UpdateAccessConfigRequest {
	input := &ekstypes.UpdateAccessConfigRequest{}
	if config != nil && config.AuthenticationMode != nil {
		input.AuthenticationMode = ekstypes.AuthenticationMode(*config.AuthenticationMode)
	}
	return input
}

func computeInput(config *ClusterComputeConfig) *ekstypes.ComputeConfigRequest {
	input := &ekstypes.ComputeConfigRequest{Enabled: aws.Bool(false)}
	if config == nil {
		return input
	}
	input.Enabled = aws.Bool(config.Enabled)
	input.NodePools = slices.Clone(valueOrNil(config.NodePools))
	input.NodeRoleArn = config.NodeRoleArn
	return input
}

func kubernetesNetworkInput(
	config *ClusterKubernetesNetworkConfig,
) *ekstypes.KubernetesNetworkConfigRequest {
	input := &ekstypes.KubernetesNetworkConfigRequest{
		ElasticLoadBalancing: &ekstypes.ElasticLoadBalancing{Enabled: aws.Bool(false)},
	}
	if config == nil {
		return input
	}
	if config.ElasticLoadBalancing != nil {
		input.ElasticLoadBalancing.Enabled = aws.Bool(config.ElasticLoadBalancing.Enabled)
	}
	if config.IPFamily != nil {
		input.IpFamily = ekstypes.IpFamily(*config.IPFamily)
	}
	input.ServiceIpv4Cidr = config.ServiceIPv4CIDR
	return input
}

func storageInput(config *ClusterStorageConfig) *ekstypes.StorageConfigRequest {
	input := &ekstypes.StorageConfigRequest{
		BlockStorage: &ekstypes.BlockStorage{Enabled: aws.Bool(false)},
	}
	if config != nil && config.BlockStorage != nil {
		input.BlockStorage.Enabled = aws.Bool(config.BlockStorage.Enabled)
	}
	return input
}

func scalingInput(
	config *ClusterControlPlaneScalingConfig,
) *ekstypes.ControlPlaneScalingConfig {
	input := &ekstypes.ControlPlaneScalingConfig{}
	if config != nil && config.Tier != nil {
		input.Tier = ekstypes.ProvisionedControlPlaneTier(*config.Tier)
	}
	return input
}

func encryptionInput(config *ClusterEncryptionConfig) []ekstypes.EncryptionConfig {
	if config == nil {
		return nil
	}
	return []ekstypes.EncryptionConfig{{
		Provider:  &ekstypes.Provider{KeyArn: aws.String(config.Provider.KeyArn)},
		Resources: slices.Clone(config.Resources),
	}}
}

func vpcInput(config ClusterVPCConfig) *ekstypes.VpcConfigRequest {
	input := &ekstypes.VpcConfigRequest{
		EndpointPrivateAccess: aws.Bool(config.EndpointPrivateAccess),
		EndpointPublicAccess:  aws.Bool(config.EndpointPublicAccess),
		SecurityGroupIds:      slices.Clone(valueOrNil(config.SecurityGroupIds)),
		SubnetIds:             slices.Clone(config.SubnetIds),
	}
	if config.ControlPlaneEgressMode != nil {
		input.ControlPlaneEgressMode =
			ekstypes.ControlPlaneEgressModeType(*config.ControlPlaneEgressMode)
	}
	if config.PublicAccessCIDRs != nil {
		input.PublicAccessCidrs = slices.Clone(*config.PublicAccessCIDRs)
	}
	return input
}

func outpostInput(config *ClusterOutpostConfig) *ekstypes.OutpostConfigRequest {
	if config == nil {
		return nil
	}
	input := &ekstypes.OutpostConfigRequest{
		ControlPlaneInstanceType: aws.String(config.ControlPlaneInstanceType),
		EtcdInstanceType:         config.EtcdInstanceType,
		OutpostArns:              slices.Clone(config.OutpostArns),
	}
	if config.ControlPlanePlacement != nil {
		placement := config.ControlPlanePlacement
		input.ControlPlanePlacement = &ekstypes.ControlPlanePlacementRequest{
			GroupName: placement.GroupName,
		}
		if placement.SpreadLevel != nil {
			input.ControlPlanePlacement.SpreadLevel =
				ekstypes.SpreadLevel(*placement.SpreadLevel)
		}
	}
	if config.EtcdPlacement != nil {
		input.EtcdPlacement = &ekstypes.EtcdPlacementRequest{}
		if config.EtcdPlacement.SpreadLevel != nil {
			input.EtcdPlacement.SpreadLevel =
				ekstypes.SpreadLevel(*config.EtcdPlacement.SpreadLevel)
		}
	}
	return input
}

func remoteNodeInputs(networks []ClusterRemoteNetwork) []ekstypes.RemoteNodeNetwork {
	inputs := make([]ekstypes.RemoteNodeNetwork, 0, len(networks))
	for _, network := range networks {
		inputs = append(inputs, ekstypes.RemoteNodeNetwork{Cidrs: slices.Clone(network.CIDRs)})
	}
	return inputs
}

func remotePodInputs(networks []ClusterRemoteNetwork) []ekstypes.RemotePodNetwork {
	inputs := make([]ekstypes.RemotePodNetwork, 0, len(networks))
	for _, network := range networks {
		inputs = append(inputs, ekstypes.RemotePodNetwork{Cidrs: slices.Clone(network.CIDRs)})
	}
	return inputs
}

func upgradeInput(policy *ClusterUpgradePolicy) *ekstypes.UpgradePolicyRequest {
	if policy == nil {
		return nil
	}
	input := &ekstypes.UpgradePolicyRequest{}
	if policy.SupportType != nil {
		input.SupportType = ekstypes.SupportType(*policy.SupportType)
	}
	return input
}

func zonalShiftInput(config *ClusterZonalShiftConfig) *ekstypes.ZonalShiftConfigRequest {
	if config == nil {
		return nil
	}
	return &ekstypes.ZonalShiftConfigRequest{Enabled: aws.Bool(config.Enabled)}
}

func clusterTags(tags map[string]string) map[string]string {
	filtered := maps.Clone(tags)
	for key := range filtered {
		if strings.HasPrefix(key, "aws:") {
			delete(filtered, key)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func valueMapOrNil[K comparable, V any](value *map[K]V) map[K]V {
	if value == nil {
		return nil
	}
	return *value
}
