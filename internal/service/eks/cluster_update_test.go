package eks

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterUpdateSerializesEveryMutationInOrder(t *testing.T) {
	prior, current := clusterUpdatePair(t)
	client := &fakeEKSClient{
		describeResults: []clusterDescribeResult{{
			cluster: completeSDKCluster(time.Date(2026, time.July, 17, 12, 30, 0, 0, time.UTC)),
		}},
		listTags: map[string]string{
			"aws:retained": "service", "change": "old", "drop": "yes", "keep": "1",
		},
	}

	output, err := current.updateWithClient(
		context.Background(), client, prior, newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"list-tags", "untag", "tag",
		"version", "wait:version",
		"access", "wait:access",
		"auto", "wait:auto",
		"scaling", "wait:scaling",
		"deletion-protection", "wait:deletion-protection",
		"encryption", "wait:encryption",
		"logging", "wait:logging",
		"remote-networks", "wait:remote-networks",
		"upgrade-policy", "wait:upgrade-policy",
		"vpc-egress", "wait:vpc-egress",
		"vpc-endpoint", "wait:vpc-endpoint",
		"vpc-subnets", "wait:vpc-subnets",
		"vpc-security-groups", "wait:vpc-security-groups",
		"zonal-shift", "wait:zonal-shift",
		"describe",
	}, client.calls)
	assert.Equal(t, "observed-arn", client.listTagsARN)
	assert.Equal(t, "observed-arn", client.untagARN)
	assert.Equal(t, []string{"drop"}, client.untagKeys)
	assert.Equal(t, "observed-arn", client.tagARN)
	assert.Equal(t, map[string]string{"add": "yes", "change": "new"}, client.tagValues)
	require.Len(t, client.versionInputs, 1)
	assert.True(t, client.versionInputs[0].Force)
	assert.Equal(t, "1.33", aws.ToString(client.versionInputs[0].Version))
	require.Len(t, client.configInputs, 12)
	auto := client.configInputs[1]
	assert.True(t, aws.ToBool(auto.ComputeConfig.Enabled))
	assert.True(t, aws.ToBool(
		auto.KubernetesNetworkConfig.ElasticLoadBalancing.Enabled,
	))
	assert.True(t, aws.ToBool(auto.StorageConfig.BlockStorage.Enabled))
	endpoint := client.configInputs[8].ResourcesVpcConfig
	assert.True(t, aws.ToBool(endpoint.EndpointPrivateAccess))
	assert.False(t, aws.ToBool(endpoint.EndpointPublicAccess))
	assert.Equal(t, []string{"203.0.113.0/24"}, endpoint.PublicAccessCidrs)
	assert.Equal(t, "example", output.Name)
}

func TestClusterUpdateTreatsSpecifiedOmissionsAsNoChange(t *testing.T) {
	priorResource := *validClusterResource()
	priorResource.Version = stringPointer("1.32")
	priorResource.AccessConfig = &ClusterAccessConfig{
		AuthenticationMode: stringPointer("CONFIG_MAP"),
	}
	priorResource.ControlPlaneScalingConfig = &ClusterControlPlaneScalingConfig{
		Tier: stringPointer("standard"),
	}
	priorResource.DeletionProtection = boolPointer(true)
	priorResource.UpgradePolicy = &ClusterUpgradePolicy{SupportType: stringPointer("STANDARD")}
	priorResource.VPCConfig.ControlPlaneEgressMode = stringPointer("AWS_MANAGED")
	priorResource.VPCConfig.PublicAccessCIDRs = &[]string{"203.0.113.0/24"}
	priorResource.ZonalShiftConfig = &ClusterZonalShiftConfig{Enabled: true}
	current := cloneClusterResource(t, priorResource)
	current.Version = nil
	current.AccessConfig = nil
	current.ControlPlaneScalingConfig = nil
	current.DeletionProtection = nil
	current.UpgradePolicy = nil
	current.VPCConfig.ControlPlaneEgressMode = nil
	current.VPCConfig.PublicAccessCIDRs = nil
	current.ZonalShiftConfig = nil
	client := finalReadClient()

	_, err := current.updateWithClient(
		context.Background(),
		client,
		runtime.Prior[ClusterResource, *ClusterResourceOutput]{
			Inputs: priorResource, Outputs: &ClusterResourceOutput{Name: "example"},
		},
		newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"describe"}, client.calls)
}

func TestClusterUpdateSendsExplicitClearRequests(t *testing.T) {
	priorResource := clusterWithComputeRole("arn:aws:iam::123456789012:role/node")
	priorResource.EnabledClusterLogTypes = &[]string{"api"}
	networks := []ClusterRemoteNetwork{{CIDRs: []string{"10.20.0.0/16"}}}
	priorResource.RemoteNetworkConfig = &ClusterRemoteNetworkConfig{
		RemoteNodeNetworks: &networks,
	}
	priorResource.VPCConfig.SecurityGroupIds = &[]string{"sg-old"}
	priorResource.DeletionProtection = boolPointer(true)
	priorResource.ZonalShiftConfig = &ClusterZonalShiftConfig{Enabled: true}
	current := cloneClusterResource(t, priorResource)
	current.ComputeConfig = nil
	current.KubernetesNetworkConfig = nil
	current.StorageConfig = nil
	current.EnabledClusterLogTypes = nil
	current.RemoteNetworkConfig = nil
	current.VPCConfig.SecurityGroupIds = nil
	current.DeletionProtection = boolPointer(false)
	current.ZonalShiftConfig = &ClusterZonalShiftConfig{Enabled: false}
	client := finalReadClient()

	_, err := current.updateWithClient(
		context.Background(),
		client,
		runtime.Prior[ClusterResource, *ClusterResourceOutput]{
			Inputs: priorResource, Outputs: &ClusterResourceOutput{Name: "example"},
		},
		newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"auto", "wait:auto",
		"deletion-protection", "wait:deletion-protection",
		"logging", "wait:logging",
		"remote-networks", "wait:remote-networks",
		"vpc-security-groups", "wait:vpc-security-groups",
		"zonal-shift", "wait:zonal-shift",
		"describe",
	}, client.calls)
	assert.False(t, aws.ToBool(client.configInputs[0].ComputeConfig.Enabled))
	assert.False(t, aws.ToBool(
		client.configInputs[0].KubernetesNetworkConfig.ElasticLoadBalancing.Enabled,
	))
	assert.False(t, aws.ToBool(client.configInputs[0].StorageConfig.BlockStorage.Enabled))
	assertLogging(t, client.configInputs[2].Logging, nil)
	remote := client.configInputs[3].RemoteNetworkConfig
	assert.NotNil(t, remote.RemoteNodeNetworks)
	assert.NotNil(t, remote.RemotePodNetworks)
	assert.Empty(t, remote.RemoteNodeNetworks)
	assert.Empty(t, remote.RemotePodNetworks)
	securityGroups := client.configInputs[4].ResourcesVpcConfig.SecurityGroupIds
	assert.NotNil(t, securityGroups)
	assert.Empty(t, securityGroups)
}

func TestClusterUpdateRejectsReplacementBeforeTagsOrAPI(t *testing.T) {
	priorResource := clusterWithEncryption(
		"arn:aws:kms:us-east-1:123456789012:key/one",
	)
	current := cloneClusterResource(t, priorResource)
	current.EncryptionConfig.Provider.KeyArn =
		"arn:aws:kms:us-east-1:123456789012:key/two"
	tags := map[string]string{"new": "tag"}
	current.Tags = &tags
	client := &fakeEKSClient{}

	_, err := current.updateWithClient(
		context.Background(),
		client,
		runtime.Prior[ClusterResource, *ClusterResourceOutput]{
			Inputs: priorResource, Outputs: &ClusterResourceOutput{Name: "example"},
		},
		newFakeClusterClock(),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "requires replacement")
	assert.Empty(t, client.calls)
}

func TestClusterUpdateRejectsMissingUpdateID(t *testing.T) {
	priorResource := *validClusterResource()
	current := cloneClusterResource(t, priorResource)
	current.Version = stringPointer("1.33")
	client := &fakeEKSClient{malformedUpdateKind: "version"}

	_, err := current.updateWithClient(
		context.Background(),
		client,
		runtime.Prior[ClusterResource, *ClusterResourceOutput]{
			Inputs: priorResource, Outputs: &ClusterResourceOutput{Name: "example"},
		},
		newFakeClusterClock(),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "no update ID")
	assert.Equal(t, []string{"version"}, client.calls)
}

func clusterUpdatePair(
	t *testing.T,
) (runtime.Prior[ClusterResource, *ClusterResourceOutput], *ClusterResource) {
	t.Helper()
	priorResource := *validClusterResource()
	priorResource.BootstrapSelfManagedAddons = false
	priorResource.Version = stringPointer("1.32")
	priorResource.AccessConfig = &ClusterAccessConfig{
		AuthenticationMode: stringPointer("CONFIG_MAP"),
	}
	priorResource.ControlPlaneScalingConfig = &ClusterControlPlaneScalingConfig{
		Tier: stringPointer("standard"),
	}
	priorResource.UpgradePolicy = &ClusterUpgradePolicy{SupportType: stringPointer("STANDARD")}
	priorResource.VPCConfig.SecurityGroupIds = &[]string{"sg-old"}
	priorTags := map[string]string{"change": "old", "drop": "yes", "keep": "1"}
	priorResource.Tags = &priorTags

	currentValue := cloneClusterResource(t, priorResource)
	current := &currentValue
	current.Version = stringPointer("1.33")
	current.ForceUpdateVersion = boolPointer(true)
	current.AccessConfig.AuthenticationMode = stringPointer("API")
	pools := []string{"general-purpose"}
	current.ComputeConfig = &ClusterComputeConfig{
		Enabled: true, NodePools: &pools,
		NodeRoleArn: stringPointer("arn:aws:iam::123456789012:role/node"),
	}
	current.KubernetesNetworkConfig = &ClusterKubernetesNetworkConfig{
		ElasticLoadBalancing: &ClusterElasticLoadBalancing{Enabled: true},
	}
	current.StorageConfig = &ClusterStorageConfig{
		BlockStorage: &ClusterBlockStorage{Enabled: true},
	}
	current.ControlPlaneScalingConfig.Tier = stringPointer("tier-xl")
	current.DeletionProtection = boolPointer(true)
	current.EncryptionConfig = clusterEncryption(
		"arn:aws:kms:us-east-1:123456789012:key/one",
	)
	current.EnabledClusterLogTypes = &[]string{"audit"}
	remote := []ClusterRemoteNetwork{{CIDRs: []string{"10.20.0.0/16"}}}
	current.RemoteNetworkConfig = &ClusterRemoteNetworkConfig{RemoteNodeNetworks: &remote}
	current.UpgradePolicy.SupportType = stringPointer("EXTENDED")
	current.VPCConfig.ControlPlaneEgressMode = stringPointer("CUSTOMER_ROUTED")
	current.VPCConfig.EndpointPrivateAccess = true
	current.VPCConfig.EndpointPublicAccess = false
	current.VPCConfig.PublicAccessCIDRs = &[]string{"203.0.113.0/24"}
	current.VPCConfig.SubnetIds = []string{"subnet-c", "subnet-d"}
	current.VPCConfig.SecurityGroupIds = &[]string{"sg-new"}
	current.ZonalShiftConfig = &ClusterZonalShiftConfig{Enabled: false}
	currentTags := map[string]string{"add": "yes", "change": "new", "keep": "1"}
	current.Tags = &currentTags

	return runtime.Prior[ClusterResource, *ClusterResourceOutput]{
		Inputs: priorResource,
		Outputs: &ClusterResourceOutput{
			Name: "example", Arn: "prior-arn",
		},
		Observed: &ClusterResourceOutput{
			Name: "example", Arn: "observed-arn",
		},
	}, current
}

func finalReadClient() *fakeEKSClient {
	return &fakeEKSClient{describeResults: []clusterDescribeResult{{
		cluster: completeSDKCluster(time.Date(2026, time.July, 17, 12, 30, 0, 0, time.UTC)),
	}}}
}

var _ = ekstypes.ClusterStatusActive
