package eks

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClusterValidateInputsAcceptsMinimalCloudCluster(t *testing.T) {
	resource := validClusterResource()
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestClusterValidateInputsRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ClusterResource)
		message string
	}{
		{
			name: "name",
			mutate: func(resource *ClusterResource) {
				resource.Name = "-invalid"
			},
			message: "name",
		},
		{
			name: "role ARN",
			mutate: func(resource *ClusterResource) {
				resource.RoleArn = "not-an-arn"
			},
			message: "role-arn",
		},
		{
			name: "cloud subnet count",
			mutate: func(resource *ClusterResource) {
				resource.VPCConfig.SubnetIds = []string{"subnet-a"}
			},
			message: "at least two",
		},
		{
			name: "auto mode mismatch",
			mutate: func(resource *ClusterResource) {
				resource.ComputeConfig = &ClusterComputeConfig{Enabled: true}
				resource.BootstrapSelfManagedAddons = false
			},
			message: "must all be enabled or disabled",
		},
		{
			name: "auto mode bootstrap add-ons",
			mutate: func(resource *ClusterResource) {
				resource.ComputeConfig = &ClusterComputeConfig{Enabled: true}
				resource.KubernetesNetworkConfig = &ClusterKubernetesNetworkConfig{
					ElasticLoadBalancing: &ClusterElasticLoadBalancing{Enabled: true},
				}
				resource.StorageConfig = &ClusterStorageConfig{
					BlockStorage: &ClusterBlockStorage{Enabled: true},
				}
			},
			message: "bootstrap-self-managed-addons",
		},
		{
			name: "encryption resource",
			mutate: func(resource *ClusterResource) {
				resource.EncryptionConfig = &ClusterEncryptionConfig{
					Provider: ClusterEncryptionProvider{
						KeyArn: "arn:aws:kms:us-east-1:123456789012:key/key-id",
					},
					Resources: []string{"configmaps"},
				}
			},
			message: "secrets",
		},
		{
			name: "service CIDR",
			mutate: func(resource *ClusterResource) {
				cidr := "8.8.0.0/16"
				resource.KubernetesNetworkConfig = &ClusterKubernetesNetworkConfig{
					ServiceIPv4CIDR: &cidr,
				}
			},
			message: "private or CGNAT",
		},
		{
			name: "remote CIDR host address",
			mutate: func(resource *ClusterResource) {
				networks := []ClusterRemoteNetwork{{CIDRs: []string{"10.0.0.1/16"}}}
				resource.RemoteNetworkConfig = &ClusterRemoteNetworkConfig{
					RemoteNodeNetworks: &networks,
				}
			},
			message: "network address",
		},
		{
			name: "empty public CIDRs",
			mutate: func(resource *ClusterResource) {
				empty := []string{}
				resource.VPCConfig.PublicAccessCIDRs = &empty
			},
			message: "must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validClusterResource()
			tt.mutate(resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), tt.message), err.Error())
		})
	}
}

func validClusterResource() *ClusterResource {
	return &ClusterResource{
		Name:                       "example",
		RoleArn:                    "arn:aws:iam::123456789012:role/eks-cluster",
		BootstrapSelfManagedAddons: true,
		VPCConfig: ClusterVPCConfig{
			SubnetIds:            []string{"subnet-a", "subnet-b"},
			EndpointPublicAccess: true,
		},
	}
}
