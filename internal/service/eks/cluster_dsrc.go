package eks

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/constraint"
)

const clusterLookupNamePattern = `^[0-9A-Za-z][0-9A-Za-z_-]{0,99}$`

type ClusterDataSource struct {
	Name string `ub:"name"`
}

type ClusterDataSourceOutput struct {
	Name                      string                          `ub:"name"`
	Arn                       string                          `ub:"arn"`
	CreatedAt                 string                          `ub:"created-at"`
	Endpoint                  string                          `ub:"endpoint"`
	PlatformVersion           string                          `ub:"platform-version"`
	RoleArn                   string                          `ub:"role-arn"`
	Status                    string                          `ub:"status"`
	Version                   string                          `ub:"version"`
	ClusterID                 *string                         `ub:"cluster-id"`
	DeletionProtection        bool                            `ub:"deletion-protection"`
	EnabledClusterLogTypes    []string                        `ub:"enabled-cluster-log-types"`
	Tags                      map[string]string               `ub:"tags"`
	AccessConfig              *clusterLookupAccess            `ub:"access-config"`
	CertificateAuthority      *clusterLookupCertificate       `ub:"certificate-authority"`
	ComputeConfig             *clusterLookupCompute           `ub:"compute-config"`
	ControlPlaneScalingConfig *clusterLookupScaling           `ub:"control-plane-scaling-config"`
	Identity                  *clusterLookupIdentity          `ub:"identity"`
	KubernetesNetworkConfig   *clusterLookupKubernetesNetwork `ub:"kubernetes-network-config"`
	OutpostConfig             *clusterLookupOutpost           `ub:"outpost-config"`
	RemoteNetworkConfig       *clusterLookupRemoteNetwork     `ub:"remote-network-config"`
	StorageConfig             *ClusterStorageConfig           `ub:"storage-config"`
	UpgradePolicy             *clusterLookupUpgradePolicy     `ub:"upgrade-policy"`
	ZonalShiftConfig          *ClusterZonalShiftConfig        `ub:"zonal-shift-config"`
	VPCConfig                 *clusterLookupVPC               `ub:"vpc-config"`
}

type clusterLookupAccess struct {
	AuthenticationMode                      string `ub:"authentication-mode"`
	BootstrapClusterCreatorAdminPermissions bool   `ub:"bootstrap-cluster-creator-admin-permissions"`
}

type clusterLookupCertificate struct {
	Data string `ub:"data"`
}

type clusterLookupCompute struct {
	Enabled     bool     `ub:"enabled"`
	NodePools   []string `ub:"node-pools"`
	NodeRoleArn string   `ub:"node-role-arn"`
}

type clusterLookupScaling struct {
	Tier string `ub:"tier"`
}

type clusterLookupIdentity struct {
	OIDC *clusterLookupOIDC `ub:"oidc"`
}

type clusterLookupOIDC struct {
	Issuer string `ub:"issuer"`
}

type clusterLookupKubernetesNetwork struct {
	ElasticLoadBalancing *ClusterElasticLoadBalancing `ub:"elastic-load-balancing"`
	IPFamily             string                       `ub:"ip-family"`
	ServiceIPv4CIDR      string                       `ub:"service-ipv4-cidr"`
	ServiceIPv6CIDR      string                       `ub:"service-ipv6-cidr"`
}

type clusterLookupOutpost struct {
	ControlPlaneInstanceType string                              `ub:"control-plane-instance-type"`
	ControlPlanePlacement    *clusterLookupControlPlanePlacement `ub:"control-plane-placement"`
	EtcdInstanceType         string                              `ub:"etcd-instance-type"`
	EtcdPlacement            *clusterLookupEtcdPlacement         `ub:"etcd-placement"`
	OutpostArns              []string                            `ub:"outpost-arns"`
}

type clusterLookupControlPlanePlacement struct {
	GroupName   string `ub:"group-name"`
	SpreadLevel string `ub:"spread-level"`
}

type clusterLookupEtcdPlacement struct {
	SpreadLevel string `ub:"spread-level"`
}

type clusterLookupRemoteNetwork struct {
	RemoteNodeNetworks []ClusterRemoteNetwork `ub:"remote-node-networks"`
	RemotePodNetworks  []ClusterRemoteNetwork `ub:"remote-pod-networks"`
}

type clusterLookupUpgradePolicy struct {
	SupportType string `ub:"support-type"`
}

type clusterLookupVPC struct {
	ClusterSecurityGroupID string   `ub:"cluster-security-group-id"`
	ControlPlaneEgressMode string   `ub:"control-plane-egress-mode"`
	EndpointPrivateAccess  bool     `ub:"endpoint-private-access"`
	EndpointPublicAccess   bool     `ub:"endpoint-public-access"`
	PublicAccessCIDRs      []string `ub:"public-access-cidrs"`
	SecurityGroupIDs       []string `ub:"security-group-ids"`
	SubnetIDs              []string `ub:"subnet-ids"`
	VPCID                  string   `ub:"vpc-id"`
}

func (r ClusterDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.Name, nil)).Message("name is required"),
		constraint.Must(constraint.IsMatch(r.Name, clusterLookupNamePattern)).
			Message("name must be 1 to 100 ASCII characters, start with a letter or digit, " +
				"and contain only letters, digits, underscores, or hyphens"),
	}
}

type clusterDescriber interface {
	DescribeCluster(context.Context, *eks.DescribeClusterInput,
		...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

func (r *ClusterDataSource) Read(
	ctx context.Context, cfg *awsCfg,
) (*ClusterDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

func (r *ClusterDataSource) read(
	ctx context.Context, client clusterDescriber,
) (*ClusterDataSourceOutput, error) {
	out, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(r.Name)})
	if err != nil {
		if isClusterNotFound(err) {
			return nil, fmt.Errorf("describe EKS cluster %q: cluster not found: %w", r.Name, err)
		}
		return nil, fmt.Errorf("describe EKS cluster %q: %w", r.Name, err)
	}
	if out == nil || out.Cluster == nil {
		return nil, fmt.Errorf("describe EKS cluster %q: no cluster returned", r.Name)
	}
	if out.Cluster.CreatedAt == nil {
		return nil, fmt.Errorf("describe EKS cluster %q: missing creation time", r.Name)
	}
	return clusterLookupOutput(out.Cluster), nil
}

func clusterLookupOutput(cluster *ekstypes.Cluster) *ClusterDataSourceOutput {
	out := &ClusterDataSourceOutput{
		Name:                   aws.ToString(cluster.Name),
		Arn:                    aws.ToString(cluster.Arn),
		CreatedAt:              cluster.CreatedAt.Format(time.RFC3339),
		Endpoint:               aws.ToString(cluster.Endpoint),
		PlatformVersion:        aws.ToString(cluster.PlatformVersion),
		RoleArn:                aws.ToString(cluster.RoleArn),
		Status:                 string(cluster.Status),
		Version:                aws.ToString(cluster.Version),
		DeletionProtection:     aws.ToBool(cluster.DeletionProtection),
		EnabledClusterLogTypes: clusterLookupLogTypes(cluster.Logging),
		Tags:                   clusterTags(cluster.Tags),
		OutpostConfig:          clusterLookupOutpostConfig(cluster.OutpostConfig),
		RemoteNetworkConfig:    clusterLookupRemoteNetworks(cluster.RemoteNetworkConfig),
		VPCConfig:              clusterLookupVPCConfig(cluster.ResourcesVpcConfig),
	}
	if out.Tags == nil {
		out.Tags = map[string]string{}
	}
	if cluster.OutpostConfig != nil && aws.ToString(cluster.Id) != "" {
		out.ClusterID = cluster.Id
	}
	if value := cluster.AccessConfig; value != nil {
		out.AccessConfig = &clusterLookupAccess{
			AuthenticationMode: string(value.AuthenticationMode),
			// The lookup contract always exposes this compatibility value.
			BootstrapClusterCreatorAdminPermissions: true,
		}
	}
	if value := cluster.CertificateAuthority; value != nil {
		out.CertificateAuthority = &clusterLookupCertificate{Data: aws.ToString(value.Data)}
	}
	if value := cluster.ComputeConfig; value != nil {
		out.ComputeConfig = &clusterLookupCompute{
			Enabled:     aws.ToBool(value.Enabled),
			NodePools:   clusterLookupStringSet(value.NodePools),
			NodeRoleArn: aws.ToString(value.NodeRoleArn),
		}
	}
	if value := cluster.ControlPlaneScalingConfig; value != nil {
		out.ControlPlaneScalingConfig = &clusterLookupScaling{Tier: string(value.Tier)}
	}
	if value := cluster.Identity; value != nil {
		out.Identity = &clusterLookupIdentity{}
		if value.Oidc != nil {
			out.Identity.OIDC = &clusterLookupOIDC{Issuer: aws.ToString(value.Oidc.Issuer)}
		}
	}
	if value := cluster.KubernetesNetworkConfig; value != nil {
		out.KubernetesNetworkConfig = &clusterLookupKubernetesNetwork{
			IPFamily:        string(value.IpFamily),
			ServiceIPv4CIDR: aws.ToString(value.ServiceIpv4Cidr),
			ServiceIPv6CIDR: aws.ToString(value.ServiceIpv6Cidr),
		}
		if elb := value.ElasticLoadBalancing; elb != nil {
			out.KubernetesNetworkConfig.ElasticLoadBalancing = &ClusterElasticLoadBalancing{
				Enabled: aws.ToBool(elb.Enabled),
			}
		}
	}
	if value := cluster.StorageConfig; value != nil {
		out.StorageConfig = &ClusterStorageConfig{}
		if value.BlockStorage != nil {
			out.StorageConfig.BlockStorage = &ClusterBlockStorage{
				Enabled: aws.ToBool(value.BlockStorage.Enabled),
			}
		}
	}
	if value := cluster.UpgradePolicy; value != nil {
		out.UpgradePolicy = &clusterLookupUpgradePolicy{SupportType: string(value.SupportType)}
	}
	if value := cluster.ZonalShiftConfig; value != nil {
		out.ZonalShiftConfig = &ClusterZonalShiftConfig{Enabled: aws.ToBool(value.Enabled)}
	}
	return out
}

func clusterLookupOutpostConfig(value *ekstypes.OutpostConfigResponse) *clusterLookupOutpost {
	if value == nil {
		return nil
	}
	out := &clusterLookupOutpost{
		ControlPlaneInstanceType: aws.ToString(value.ControlPlaneInstanceType),
		EtcdInstanceType:         aws.ToString(value.EtcdInstanceType),
		OutpostArns:              clusterLookupStringSet(value.OutpostArns),
	}
	if placement := value.ControlPlanePlacement; placement != nil {
		out.ControlPlanePlacement = &clusterLookupControlPlanePlacement{
			GroupName: aws.ToString(placement.GroupName), SpreadLevel: string(placement.SpreadLevel),
		}
	}
	if placement := value.EtcdPlacement; placement != nil {
		out.EtcdPlacement = &clusterLookupEtcdPlacement{SpreadLevel: string(placement.SpreadLevel)}
	}
	return out
}

func clusterLookupRemoteNetworks(
	value *ekstypes.RemoteNetworkConfigResponse,
) *clusterLookupRemoteNetwork {
	if value == nil {
		return nil
	}
	out := &clusterLookupRemoteNetwork{
		RemoteNodeNetworks: make([]ClusterRemoteNetwork, 0, len(value.RemoteNodeNetworks)),
		RemotePodNetworks:  make([]ClusterRemoteNetwork, 0, len(value.RemotePodNetworks)),
	}
	for _, network := range value.RemoteNodeNetworks {
		out.RemoteNodeNetworks = append(out.RemoteNodeNetworks,
			ClusterRemoteNetwork{CIDRs: clusterLookupStringSet(network.Cidrs)})
	}
	for _, network := range value.RemotePodNetworks {
		out.RemotePodNetworks = append(out.RemotePodNetworks,
			ClusterRemoteNetwork{CIDRs: clusterLookupStringSet(network.Cidrs)})
	}
	return out
}

func clusterLookupVPCConfig(value *ekstypes.VpcConfigResponse) *clusterLookupVPC {
	if value == nil {
		return nil
	}
	return &clusterLookupVPC{
		ClusterSecurityGroupID: aws.ToString(value.ClusterSecurityGroupId),
		ControlPlaneEgressMode: string(value.ControlPlaneEgressMode),
		EndpointPrivateAccess:  value.EndpointPrivateAccess,
		EndpointPublicAccess:   value.EndpointPublicAccess,
		PublicAccessCIDRs:      clusterLookupStringSet(value.PublicAccessCidrs),
		SecurityGroupIDs:       clusterLookupStringSet(value.SecurityGroupIds),
		SubnetIDs:              clusterLookupStringSet(value.SubnetIds),
		VPCID:                  aws.ToString(value.VpcId),
	}
}

func clusterLookupLogTypes(logging *ekstypes.Logging) []string {
	types := []string{}
	if logging != nil {
		for _, group := range logging.ClusterLogging {
			if aws.ToBool(group.Enabled) {
				for _, logType := range group.Types {
					types = append(types, string(logType))
				}
			}
		}
	}
	slices.Sort(types)
	return slices.Compact(types)
}

func clusterLookupStringSet(values []string) []string {
	out := append(make([]string, 0, len(values)), values...)
	slices.Sort(out)
	return slices.Compact(out)
}
