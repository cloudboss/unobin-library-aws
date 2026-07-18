package eks

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func staticClusterReplacement(prior, current ClusterResource) bool {
	return runtime.Changed(prior.Name, current.Name) ||
		runtime.Changed(prior.RoleArn, current.RoleArn) ||
		runtime.Changed(
			prior.BootstrapSelfManagedAddons,
			current.BootstrapSelfManagedAddons,
		) ||
		runtime.Changed(prior.OutpostConfig, current.OutpostConfig)
}

func conditionalClusterReplacement(prior, current ClusterResource) error {
	if runtime.Changed(accessBootstrap(prior.AccessConfig), accessBootstrap(current.AccessConfig)) {
		return clusterReplacementError(
			"access-config.bootstrap-cluster-creator-admin-permissions",
		)
	}
	if runtime.Changed(networkIPFamily(prior.KubernetesNetworkConfig),
		networkIPFamily(current.KubernetesNetworkConfig)) {
		return clusterReplacementError("kubernetes-network-config.ip-family")
	}
	if runtime.Changed(networkServiceCIDR(prior.KubernetesNetworkConfig),
		networkServiceCIDR(current.KubernetesNetworkConfig)) {
		return clusterReplacementError("kubernetes-network-config.service-ipv4-cidr")
	}
	if prior.EncryptionConfig != nil &&
		runtime.Changed(prior.EncryptionConfig, current.EncryptionConfig) {
		return clusterReplacementError("encryption-config")
	}
	if requiresEgressReplacement(prior, current) {
		return clusterReplacementError("vpc-config.control-plane-egress-mode")
	}
	if changesExistingComputeRole(prior.ComputeConfig, current.ComputeConfig) {
		return clusterReplacementError("compute-config.node-role-arn")
	}
	return nil
}

func clusterReplacementError(field string) error {
	return fmt.Errorf("change to %s requires replacement", field)
}

func accessBootstrap(config *ClusterAccessConfig) *bool {
	if config == nil {
		return nil
	}
	return config.BootstrapClusterCreatorAdminPermissions
}

func networkIPFamily(config *ClusterKubernetesNetworkConfig) *string {
	if config == nil {
		return nil
	}
	return config.IPFamily
}

func networkServiceCIDR(config *ClusterKubernetesNetworkConfig) *string {
	if config == nil {
		return nil
	}
	return config.ServiceIPv4CIDR
}

func requiresEgressReplacement(prior, current ClusterResource) bool {
	oldMode := prior.VPCConfig.ControlPlaneEgressMode
	newMode := current.VPCConfig.ControlPlaneEgressMode
	if newMode == nil || *newMode != "AWS_MANAGED" {
		return false
	}
	return oldMode == nil || *oldMode == "CUSTOMER_ROUTED"
}

func changesExistingComputeRole(
	prior *ClusterComputeConfig,
	current *ClusterComputeConfig,
) bool {
	oldInput := computeInput(prior)
	newInput := computeInput(current)
	oldRole := aws.ToString(oldInput.NodeRoleArn)
	newRole := aws.ToString(newInput.NodeRoleArn)
	if oldRole == "" || oldRole == newRole {
		return false
	}
	if !aws.ToBool(newInput.Enabled) && newRole == "" {
		return false
	}
	if len(newInput.NodePools) == 0 && newRole == "" {
		return false
	}
	return true
}
