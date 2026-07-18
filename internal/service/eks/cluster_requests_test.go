package eks

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterCreateInputSendsExplicitDefaults(t *testing.T) {
	resource := validClusterResource()

	input := resource.createInput()

	assert.Equal(t, "example", aws.ToString(input.Name))
	assert.Equal(t, resource.RoleArn, aws.ToString(input.RoleArn))
	assert.True(t, aws.ToBool(input.BootstrapSelfManagedAddons))
	require.NotNil(t, input.ComputeConfig)
	assert.False(t, aws.ToBool(input.ComputeConfig.Enabled))
	require.NotNil(t, input.KubernetesNetworkConfig)
	require.NotNil(t, input.KubernetesNetworkConfig.ElasticLoadBalancing)
	assert.False(t, aws.ToBool(input.KubernetesNetworkConfig.ElasticLoadBalancing.Enabled))
	require.NotNil(t, input.StorageConfig)
	require.NotNil(t, input.StorageConfig.BlockStorage)
	assert.False(t, aws.ToBool(input.StorageConfig.BlockStorage.Enabled))
	assert.Equal(t, []string{"subnet-a", "subnet-b"}, input.ResourcesVpcConfig.SubnetIds)
	assert.False(t, aws.ToBool(input.ResourcesVpcConfig.EndpointPrivateAccess))
	assert.True(t, aws.ToBool(input.ResourcesVpcConfig.EndpointPublicAccess))
	assertLogging(t, input.Logging, nil)
}

func TestClusterCreateInputConvertsOptionalBlocksAndTags(t *testing.T) {
	resource := fullClusterResource()

	input := resource.createInput()

	require.NotNil(t, input.AccessConfig)
	assert.Equal(t, ekstypes.AuthenticationModeApi, input.AccessConfig.AuthenticationMode)
	assert.False(t, aws.ToBool(input.AccessConfig.BootstrapClusterCreatorAdminPermissions))
	assert.True(t, aws.ToBool(input.ComputeConfig.Enabled))
	assert.Equal(t, []string{"general-purpose"}, input.ComputeConfig.NodePools)
	assert.Equal(t,
		"arn:aws:iam::123456789012:role/node",
		aws.ToString(input.ComputeConfig.NodeRoleArn),
	)
	assert.True(t, aws.ToBool(input.KubernetesNetworkConfig.ElasticLoadBalancing.Enabled))
	assert.Equal(t, ekstypes.IpFamilyIpv4, input.KubernetesNetworkConfig.IpFamily)
	assert.Equal(t, "10.100.0.0/16", aws.ToString(input.KubernetesNetworkConfig.ServiceIpv4Cidr))
	assert.True(t, aws.ToBool(input.StorageConfig.BlockStorage.Enabled))
	assert.Equal(t, map[string]string{"team": "platform"}, input.Tags)
	assertLogging(t, input.Logging, []ekstypes.LogType{ekstypes.LogTypeApi, ekstypes.LogTypeAudit})
}

func TestClusterAutoModeInputDisablesRemovedBlocks(t *testing.T) {
	resource := validClusterResource()

	compute, network, storage := resource.autoModeInput()

	assert.False(t, aws.ToBool(compute.Enabled))
	require.NotNil(t, network.ElasticLoadBalancing)
	assert.False(t, aws.ToBool(network.ElasticLoadBalancing.Enabled))
	require.NotNil(t, storage.BlockStorage)
	assert.False(t, aws.ToBool(storage.BlockStorage.Enabled))
}

func TestClusterRemoteNetworkUpdateRemovalSendsEmptyArrays(t *testing.T) {
	input := remoteNetworkInput(nil, true)
	require.NotNil(t, input)
	assert.Empty(t, input.RemoteNodeNetworks)
	assert.NotNil(t, input.RemoteNodeNetworks)
	assert.Empty(t, input.RemotePodNetworks)
	assert.NotNil(t, input.RemotePodNetworks)
}

func assertLogging(t *testing.T, logging *ekstypes.Logging, enabled []ekstypes.LogType) {
	t.Helper()
	require.NotNil(t, logging)
	require.Len(t, logging.ClusterLogging, 2)
	assert.True(t, aws.ToBool(logging.ClusterLogging[0].Enabled))
	assert.Equal(t, enabled, logging.ClusterLogging[0].Types)
	assert.False(t, aws.ToBool(logging.ClusterLogging[1].Enabled))
	wantDisabled := []ekstypes.LogType{
		ekstypes.LogTypeApi,
		ekstypes.LogTypeAudit,
		ekstypes.LogTypeAuthenticator,
		ekstypes.LogTypeControllerManager,
		ekstypes.LogTypeScheduler,
	}
	if len(enabled) > 0 {
		wantDisabled = wantDisabled[len(enabled):]
	}
	assert.Equal(t, wantDisabled, logging.ClusterLogging[1].Types)
}

func fullClusterResource() *ClusterResource {
	resource := validClusterResource()
	falseValue := false
	apiMode := "API"
	nodePools := []string{"general-purpose"}
	nodeRole := "arn:aws:iam::123456789012:role/node"
	ipv4 := "ipv4"
	serviceCIDR := "10.100.0.0/16"
	logs := []string{"api", "audit"}
	tags := map[string]string{"team": "platform", "aws:owned": "ignored"}
	resource.BootstrapSelfManagedAddons = false
	resource.AccessConfig = &ClusterAccessConfig{
		AuthenticationMode:                      &apiMode,
		BootstrapClusterCreatorAdminPermissions: &falseValue,
	}
	resource.ComputeConfig = &ClusterComputeConfig{
		Enabled:     true,
		NodePools:   &nodePools,
		NodeRoleArn: &nodeRole,
	}
	resource.KubernetesNetworkConfig = &ClusterKubernetesNetworkConfig{
		ElasticLoadBalancing: &ClusterElasticLoadBalancing{Enabled: true},
		IPFamily:             &ipv4,
		ServiceIPv4CIDR:      &serviceCIDR,
	}
	resource.StorageConfig = &ClusterStorageConfig{
		BlockStorage: &ClusterBlockStorage{Enabled: true},
	}
	resource.EnabledClusterLogTypes = &logs
	resource.Tags = &tags
	return resource
}
