package eks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterConditionalReplacementRejectsUnsupportedUpdates(t *testing.T) {
	tests := []struct {
		name   string
		prior  ClusterResource
		modify func(*ClusterResource)
		field  string
	}{
		{
			name:  "access bootstrap permission",
			prior: clusterWithAccessBootstrap(true),
			modify: func(resource *ClusterResource) {
				resource.AccessConfig.BootstrapClusterCreatorAdminPermissions = boolPointer(false)
			},
			field: "access-config.bootstrap-cluster-creator-admin-permissions",
		},
		{
			name:  "IP family",
			prior: clusterWithKubernetesNetwork("ipv4", "10.100.0.0/16"),
			modify: func(resource *ClusterResource) {
				resource.KubernetesNetworkConfig.IPFamily = stringPointer("ipv6")
			},
			field: "kubernetes-network-config.ip-family",
		},
		{
			name:  "service IPv4 CIDR",
			prior: clusterWithKubernetesNetwork("ipv4", "10.100.0.0/16"),
			modify: func(resource *ClusterResource) {
				resource.KubernetesNetworkConfig.ServiceIPv4CIDR = stringPointer("10.101.0.0/16")
			},
			field: "kubernetes-network-config.service-ipv4-cidr",
		},
		{
			name:  "existing encryption removal",
			prior: clusterWithEncryption("arn:aws:kms:us-east-1:123456789012:key/one"),
			modify: func(resource *ClusterResource) {
				resource.EncryptionConfig = nil
			},
			field: "encryption-config",
		},
		{
			name:  "existing encryption change",
			prior: clusterWithEncryption("arn:aws:kms:us-east-1:123456789012:key/one"),
			modify: func(resource *ClusterResource) {
				resource.EncryptionConfig.Provider.KeyArn =
					"arn:aws:kms:us-east-1:123456789012:key/two"
			},
			field: "encryption-config",
		},
		{
			name:  "egress reversal",
			prior: clusterWithEgress("CUSTOMER_ROUTED"),
			modify: func(resource *ClusterResource) {
				resource.VPCConfig.ControlPlaneEgressMode = stringPointer("AWS_MANAGED")
			},
			field: "vpc-config.control-plane-egress-mode",
		},
		{
			name:  "compute role",
			prior: clusterWithComputeRole("arn:aws:iam::123456789012:role/one"),
			modify: func(resource *ClusterResource) {
				resource.ComputeConfig.NodeRoleArn =
					stringPointer("arn:aws:iam::123456789012:role/two")
			},
			field: "compute-config.node-role-arn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := cloneClusterResource(t, tt.prior)
			tt.modify(&current)
			err := conditionalClusterReplacement(tt.prior, current)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.field)
			assert.ErrorContains(t, err, "requires replacement")
		})
	}
}

func TestClusterConditionalReplacementAllowsSupportedTransitions(t *testing.T) {
	tests := []struct {
		name   string
		prior  ClusterResource
		modify func(*ClusterResource)
	}{
		{
			name:  "first encryption association",
			prior: *validClusterResource(),
			modify: func(resource *ClusterResource) {
				resource.EncryptionConfig = clusterEncryption(
					"arn:aws:kms:us-east-1:123456789012:key/one",
				)
			},
		},
		{
			name:  "compute role first set",
			prior: clusterWithComputeRole(""),
			modify: func(resource *ClusterResource) {
				resource.ComputeConfig.NodeRoleArn =
					stringPointer("arn:aws:iam::123456789012:role/one")
			},
		},
		{
			name:  "compute role unset while disabled",
			prior: clusterWithComputeRole("arn:aws:iam::123456789012:role/one"),
			modify: func(resource *ClusterResource) {
				resource.ComputeConfig.Enabled = false
				resource.ComputeConfig.NodeRoleArn = nil
			},
		},
		{
			name:  "compute role unset with no node pools",
			prior: clusterWithComputeRole("arn:aws:iam::123456789012:role/one"),
			modify: func(resource *ClusterResource) {
				empty := []string{}
				resource.ComputeConfig.NodePools = &empty
				resource.ComputeConfig.NodeRoleArn = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := cloneClusterResource(t, tt.prior)
			tt.modify(&current)
			require.NoError(t, conditionalClusterReplacement(tt.prior, current))
		})
	}
}

func TestClusterStaticReplacement(t *testing.T) {
	prior := clusterWithAccessBootstrap(true)
	current := cloneClusterResource(t, prior)
	current.Name = "replacement"
	current.AccessConfig.BootstrapClusterCreatorAdminPermissions = boolPointer(false)
	assert.True(t, staticClusterReplacement(prior, current))
}

func TestClusterEgressReplacementGuardSurvivesOmittedInput(t *testing.T) {
	customerRouted := clusterWithEgress("CUSTOMER_ROUTED")
	omitted := cloneClusterResource(t, customerRouted)
	omitted.VPCConfig.ControlPlaneEgressMode = nil
	require.NoError(t, conditionalClusterReplacement(customerRouted, omitted))

	awsManaged := cloneClusterResource(t, omitted)
	awsManaged.VPCConfig.ControlPlaneEgressMode = stringPointer("AWS_MANAGED")
	currentTags := map[string]string{"new": "tag"}
	awsManaged.Tags = &currentTags
	err := conditionalClusterReplacement(omitted, awsManaged)
	require.Error(t, err)
	assert.ErrorContains(t, err, "vpc-config.control-plane-egress-mode")
	assert.ErrorContains(t, err, "requires replacement")

	client := &fakeEKSClient{}
	_, err = awsManaged.updateWithClient(
		context.Background(),
		client,
		runtime.Prior[ClusterResource, *ClusterResourceOutput, *awsCfg]{
			Inputs: omitted,
			Outputs: &ClusterResourceOutput{
				Name: "example",
				Arn:  "arn:aws:eks:us-east-1:123456789012:cluster/example",
			},
		},
		newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "requires replacement")
	assert.Empty(t, client.calls)
}

func clusterWithAccessBootstrap(enabled bool) ClusterResource {
	resource := *validClusterResource()
	resource.AccessConfig = &ClusterAccessConfig{
		BootstrapClusterCreatorAdminPermissions: boolPointer(enabled),
	}
	return resource
}

func clusterWithKubernetesNetwork(ipFamily, serviceCIDR string) ClusterResource {
	resource := *validClusterResource()
	resource.KubernetesNetworkConfig = &ClusterKubernetesNetworkConfig{
		IPFamily:        stringPointer(ipFamily),
		ServiceIPv4CIDR: stringPointer(serviceCIDR),
	}
	return resource
}

func clusterWithEncryption(key string) ClusterResource {
	resource := *validClusterResource()
	resource.EncryptionConfig = clusterEncryption(key)
	return resource
}

func clusterEncryption(key string) *ClusterEncryptionConfig {
	return &ClusterEncryptionConfig{
		Provider:  ClusterEncryptionProvider{KeyArn: key},
		Resources: []string{"secrets"},
	}
}

func clusterWithEgress(mode string) ClusterResource {
	resource := *validClusterResource()
	resource.VPCConfig.ControlPlaneEgressMode = stringPointer(mode)
	return resource
}

func clusterWithComputeRole(role string) ClusterResource {
	resource := *validClusterResource()
	pools := []string{"general-purpose"}
	resource.BootstrapSelfManagedAddons = false
	resource.ComputeConfig = &ClusterComputeConfig{Enabled: true, NodePools: &pools}
	resource.KubernetesNetworkConfig = &ClusterKubernetesNetworkConfig{
		ElasticLoadBalancing: &ClusterElasticLoadBalancing{Enabled: true},
	}
	resource.StorageConfig = &ClusterStorageConfig{
		BlockStorage: &ClusterBlockStorage{Enabled: true},
	}
	if role != "" {
		resource.ComputeConfig.NodeRoleArn = stringPointer(role)
	}
	return resource
}

func stringPointer(value string) *string { return &value }

func boolPointer(value bool) *bool { return &value }

func cloneClusterResource(t *testing.T, resource ClusterResource) ClusterResource {
	t.Helper()
	encoded, err := json.Marshal(resource)
	require.NoError(t, err)
	var clone ClusterResource
	require.NoError(t, json.Unmarshal(encoded, &clone))
	return clone
}
