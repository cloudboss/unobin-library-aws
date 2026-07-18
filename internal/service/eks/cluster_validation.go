package eks

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"

	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
)

var clusterNamePattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z_-]*$`)

func (r *ClusterResource) validateInputs(context.Context, *awsCfg) error {
	if len(r.Name) < 1 || len(r.Name) > 100 || !clusterNamePattern.MatchString(r.Name) {
		return fmt.Errorf("name must match %s and contain 1 to 100 characters",
			clusterNamePattern.String())
	}
	if err := validateClusterARN("role-arn", r.RoleArn); err != nil {
		return err
	}
	minimumSubnets := 2
	if r.OutpostConfig != nil {
		minimumSubnets = 1
	}
	if len(r.VPCConfig.SubnetIds) < minimumSubnets {
		if minimumSubnets == 2 {
			return errors.New("vpc-config.subnet-ids must contain at least two subnets")
		}
		return errors.New("vpc-config.subnet-ids must not be empty")
	}
	if err := validateOptionalEnum("access-config.authentication-mode",
		clusterAccessMode(r), "CONFIG_MAP", "API", "API_AND_CONFIG_MAP"); err != nil {
		return err
	}
	if err := validateComputeConfig(r.ComputeConfig); err != nil {
		return err
	}
	if err := validateAutoMode(r); err != nil {
		return err
	}
	if err := validateScalingConfig(r.ControlPlaneScalingConfig); err != nil {
		return err
	}
	if err := validateStringValues("enabled-cluster-log-types",
		valueOrNil(r.EnabledClusterLogTypes),
		"api", "audit", "authenticator", "controllerManager", "scheduler"); err != nil {
		return err
	}
	if err := validateEncryptionConfig(r.EncryptionConfig); err != nil {
		return err
	}
	if r.EncryptionConfig != nil && r.OutpostConfig != nil {
		return errors.New("encryption-config conflicts with outpost-config")
	}
	if r.KubernetesNetworkConfig != nil && r.OutpostConfig != nil {
		return errors.New("kubernetes-network-config conflicts with outpost-config")
	}
	if err := validateKubernetesNetworkConfig(r.KubernetesNetworkConfig); err != nil {
		return err
	}
	if err := validateOutpostConfig(r.OutpostConfig); err != nil {
		return err
	}
	if err := validateRemoteNetworkConfig(r.RemoteNetworkConfig); err != nil {
		return err
	}
	if err := validateUpgradePolicy(r.UpgradePolicy); err != nil {
		return err
	}
	if err := validateOptionalEnum("vpc-config.control-plane-egress-mode",
		r.VPCConfig.ControlPlaneEgressMode, "AWS_MANAGED", "CUSTOMER_ROUTED"); err != nil {
		return err
	}
	if err := validateCIDRCollection(
		"vpc-config.public-access-cidrs", r.VPCConfig.PublicAccessCIDRs, false,
	); err != nil {
		return err
	}
	return nil
}

func clusterAccessMode(r *ClusterResource) *string {
	if r.AccessConfig == nil {
		return nil
	}
	return r.AccessConfig.AuthenticationMode
}

func validateComputeConfig(config *ClusterComputeConfig) error {
	if config == nil {
		return nil
	}
	if config.NodeRoleArn != nil {
		if err := validateClusterARN("compute-config.node-role-arn", *config.NodeRoleArn); err != nil {
			return err
		}
	}
	return validateStringValues("compute-config.node-pools", valueOrNil(config.NodePools),
		"general-purpose", "system")
}

func validateAutoMode(r *ClusterResource) error {
	compute, loadBalancing, storage := autoModeEnabled(r)
	if compute != loadBalancing || compute != storage {
		return errors.New("compute, load balancing, and block storage must all be enabled or disabled")
	}
	if compute && r.BootstrapSelfManagedAddons {
		return errors.New("bootstrap-self-managed-addons must be false when Auto Mode is enabled")
	}
	return nil
}

func autoModeEnabled(r *ClusterResource) (bool, bool, bool) {
	compute := r.ComputeConfig != nil && r.ComputeConfig.Enabled
	loadBalancing := r.KubernetesNetworkConfig != nil &&
		r.KubernetesNetworkConfig.ElasticLoadBalancing != nil &&
		r.KubernetesNetworkConfig.ElasticLoadBalancing.Enabled
	storage := r.StorageConfig != nil && r.StorageConfig.BlockStorage != nil &&
		r.StorageConfig.BlockStorage.Enabled
	return compute, loadBalancing, storage
}

func validateScalingConfig(config *ClusterControlPlaneScalingConfig) error {
	if config == nil {
		return nil
	}
	return validateOptionalEnum("control-plane-scaling-config.tier", config.Tier,
		"standard", "tier-xl", "tier-2xl", "tier-4xl", "tier-8xl", "tier-ultra")
}

func validateEncryptionConfig(config *ClusterEncryptionConfig) error {
	if config == nil {
		return nil
	}
	if err := validateClusterARN(
		"encryption-config.provider.key-arn", config.Provider.KeyArn,
	); err != nil {
		return err
	}
	if len(config.Resources) == 0 {
		return errors.New("encryption-config.resources must not be empty")
	}
	for _, resource := range config.Resources {
		if resource != "secrets" {
			return fmt.Errorf("encryption-config.resources may contain only secrets, got %q", resource)
		}
	}
	return nil
}

func validateKubernetesNetworkConfig(config *ClusterKubernetesNetworkConfig) error {
	if config == nil {
		return nil
	}
	if err := validateOptionalEnum(
		"kubernetes-network-config.ip-family", config.IPFamily, "ipv4", "ipv6",
	); err != nil {
		return err
	}
	if config.ServiceIPv4CIDR == nil {
		return nil
	}
	prefix, err := parsePrivateIPv4Network(
		"kubernetes-network-config.service-ipv4-cidr", *config.ServiceIPv4CIDR,
	)
	if err != nil {
		return err
	}
	if prefix.Bits() < 12 || prefix.Bits() > 24 {
		return errors.New(
			"kubernetes-network-config.service-ipv4-cidr prefix must be between 12 and 24",
		)
	}
	return nil
}

func validateOutpostConfig(config *ClusterOutpostConfig) error {
	if config == nil {
		return nil
	}
	if config.ControlPlaneInstanceType == "" {
		return errors.New("outpost-config.control-plane-instance-type must not be empty")
	}
	if len(config.OutpostArns) == 0 {
		return errors.New("outpost-config.outpost-arns must not be empty")
	}
	for _, arn := range config.OutpostArns {
		if err := validateClusterARN("outpost-config.outpost-arns", arn); err != nil {
			return err
		}
	}
	if config.ControlPlanePlacement != nil {
		if err := validateOptionalEnum("outpost-config.control-plane-placement.spread-level",
			config.ControlPlanePlacement.SpreadLevel, "host", "rack"); err != nil {
			return err
		}
	}
	if config.EtcdPlacement != nil {
		if err := validateOptionalEnum("outpost-config.etcd-placement.spread-level",
			config.EtcdPlacement.SpreadLevel, "host", "rack"); err != nil {
			return err
		}
	}
	return nil
}

func validateRemoteNetworkConfig(config *ClusterRemoteNetworkConfig) error {
	if config == nil {
		return nil
	}
	if err := validateRemoteNetworks(
		"remote-network-config.remote-node-networks", config.RemoteNodeNetworks,
	); err != nil {
		return err
	}
	return validateRemoteNetworks(
		"remote-network-config.remote-pod-networks", config.RemotePodNetworks,
	)
}

func validateRemoteNetworks(field string, networks *[]ClusterRemoteNetwork) error {
	if networks == nil {
		return nil
	}
	if len(*networks) == 0 {
		return fmt.Errorf("%s must not be empty", field)
	}
	for _, network := range *networks {
		if len(network.CIDRs) == 0 {
			return fmt.Errorf("%s.cidrs must not be empty", field)
		}
		for _, cidr := range network.CIDRs {
			if _, err := parsePrivateIPv4Network(field+".cidrs", cidr); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateUpgradePolicy(policy *ClusterUpgradePolicy) error {
	if policy == nil {
		return nil
	}
	return validateOptionalEnum(
		"upgrade-policy.support-type", policy.SupportType, "STANDARD", "EXTENDED",
	)
}

func validateCIDRCollection(field string, cidrs *[]string, private bool) error {
	if cidrs == nil {
		return nil
	}
	if len(*cidrs) == 0 {
		return fmt.Errorf("%s must not be empty", field)
	}
	for _, cidr := range *cidrs {
		if private {
			if _, err := parsePrivateIPv4Network(field, cidr); err != nil {
				return err
			}
			continue
		}
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return fmt.Errorf("%s contains invalid CIDR %q: %w", field, cidr, err)
		}
		if prefix != prefix.Masked() {
			return fmt.Errorf("%s CIDR %q must be a network address", field, cidr)
		}
	}
	return nil
}

func parsePrivateIPv4Network(field, value string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("%s must be an IPv4 CIDR, got %q", field, value)
	}
	if prefix != prefix.Masked() {
		return netip.Prefix{}, fmt.Errorf("%s %q must be a network address", field, value)
	}
	if !privatePrefix(prefix) {
		return netip.Prefix{}, fmt.Errorf("%s %q must be a private or CGNAT network", field, value)
	}
	return prefix, nil
}

func privatePrefix(prefix netip.Prefix) bool {
	ranges := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("100.64.0.0/10"),
	}
	for _, allowed := range ranges {
		if prefix.Bits() >= allowed.Bits() && allowed.Contains(prefix.Addr()) {
			return true
		}
	}
	return false
}

func validateClusterARN(field, value string) error {
	parsed, err := awsarn.Parse(value)
	if err != nil || parsed.Partition == "" || parsed.Service == "" || parsed.Resource == "" {
		return fmt.Errorf("%s must be a valid ARN", field)
	}
	return nil
}

func validateOptionalEnum(field string, value *string, allowed ...string) error {
	if value == nil || slices.Contains(allowed, *value) {
		return nil
	}
	return fmt.Errorf("%s contains invalid value %q", field, *value)
}

func validateStringValues(field string, values []string, allowed ...string) error {
	seen := map[string]struct{}{}
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("%s contains invalid value %q", field, value)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%s contains duplicate value %q", field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func valueOrNil[T any](value *[]T) []T {
	if value == nil {
		return nil
	}
	return *value
}
