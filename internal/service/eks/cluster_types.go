package eks

// ClusterResource manages an Amazon EKS control plane and its mutable cluster
// configuration.
type ClusterResource struct {
	Name                       string                            `ub:"name"`
	RoleArn                    string                            `ub:"role-arn"`
	VPCConfig                  ClusterVPCConfig                  `ub:"vpc-config"`
	Version                    *string                           `ub:"version"`
	ForceUpdateVersion         *bool                             `ub:"force-update-version"`
	BootstrapSelfManagedAddons bool                              `ub:"bootstrap-self-managed-addons"`
	AccessConfig               *ClusterAccessConfig              `ub:"access-config"`
	ComputeConfig              *ClusterComputeConfig             `ub:"compute-config"`
	ControlPlaneScalingConfig  *ClusterControlPlaneScalingConfig `ub:"control-plane-scaling-config"`
	DeletionProtection         *bool                             `ub:"deletion-protection"`
	EnabledClusterLogTypes     *[]string                         `ub:"enabled-cluster-log-types"`
	EncryptionConfig           *ClusterEncryptionConfig          `ub:"encryption-config"`
	KubernetesNetworkConfig    *ClusterKubernetesNetworkConfig   `ub:"kubernetes-network-config"`
	OutpostConfig              *ClusterOutpostConfig             `ub:"outpost-config"`
	RemoteNetworkConfig        *ClusterRemoteNetworkConfig       `ub:"remote-network-config"`
	StorageConfig              *ClusterStorageConfig             `ub:"storage-config"`
	UpgradePolicy              *ClusterUpgradePolicy             `ub:"upgrade-policy"`
	ZonalShiftConfig           *ClusterZonalShiftConfig          `ub:"zonal-shift-config"`
	Tags                       *map[string]string                `ub:"tags"`
}

// ClusterResourceOutput holds the durable cluster handle and values computed by EKS.
type ClusterResourceOutput struct {
	Name                     string  `ub:"name"`
	Arn                      string  `ub:"arn"`
	CertificateAuthorityData string  `ub:"certificate-authority-data"`
	ClusterID                *string `ub:"cluster-id"`
	CreatedAt                string  `ub:"created-at"`
	Endpoint                 string  `ub:"endpoint"`
	OIDCIssuer               *string `ub:"oidc-issuer"`
	PlatformVersion          string  `ub:"platform-version"`
	Status                   string  `ub:"status"`
	VersionActual            string  `ub:"version-actual"`
	IPFamilyActual           string  `ub:"ip-family-actual"`
	ServiceIPv4CIDRActual    string  `ub:"service-ipv4-cidr-actual"`
	ServiceIPv6CIDR          *string `ub:"service-ipv6-cidr"`
	ClusterSecurityGroupID   string  `ub:"cluster-security-group-id"`
	VPCID                    string  `ub:"vpc-id"`
}

type ClusterVPCConfig struct {
	SubnetIds              []string  `ub:"subnet-ids"`
	SecurityGroupIds       *[]string `ub:"security-group-ids"`
	EndpointPrivateAccess  bool      `ub:"endpoint-private-access"`
	EndpointPublicAccess   bool      `ub:"endpoint-public-access"`
	PublicAccessCIDRs      *[]string `ub:"public-access-cidrs"`
	ControlPlaneEgressMode *string   `ub:"control-plane-egress-mode"`
}

type ClusterAccessConfig struct {
	AuthenticationMode                      *string `ub:"authentication-mode"`
	BootstrapClusterCreatorAdminPermissions *bool   `ub:"bootstrap-cluster-creator-admin-permissions"`
}

type ClusterComputeConfig struct {
	Enabled     bool      `ub:"enabled"`
	NodePools   *[]string `ub:"node-pools"`
	NodeRoleArn *string   `ub:"node-role-arn"`
}

type ClusterControlPlaneScalingConfig struct {
	Tier *string `ub:"tier"`
}

type ClusterEncryptionConfig struct {
	Provider  ClusterEncryptionProvider `ub:"provider"`
	Resources []string                  `ub:"resources"`
}

type ClusterEncryptionProvider struct {
	KeyArn string `ub:"key-arn"`
}

type ClusterKubernetesNetworkConfig struct {
	ElasticLoadBalancing *ClusterElasticLoadBalancing `ub:"elastic-load-balancing"`
	IPFamily             *string                      `ub:"ip-family"`
	ServiceIPv4CIDR      *string                      `ub:"service-ipv4-cidr"`
}

type ClusterElasticLoadBalancing struct {
	Enabled bool `ub:"enabled"`
}

type ClusterOutpostConfig struct {
	ControlPlaneInstanceType string                        `ub:"control-plane-instance-type"`
	ControlPlanePlacement    *ClusterControlPlanePlacement `ub:"control-plane-placement"`
	EtcdInstanceType         *string                       `ub:"etcd-instance-type"`
	EtcdPlacement            *ClusterEtcdPlacement         `ub:"etcd-placement"`
	OutpostArns              []string                      `ub:"outpost-arns"`
}

type ClusterControlPlanePlacement struct {
	GroupName   *string `ub:"group-name"`
	SpreadLevel *string `ub:"spread-level"`
}

type ClusterEtcdPlacement struct {
	SpreadLevel *string `ub:"spread-level"`
}

type ClusterRemoteNetworkConfig struct {
	RemoteNodeNetworks *[]ClusterRemoteNetwork `ub:"remote-node-networks"`
	RemotePodNetworks  *[]ClusterRemoteNetwork `ub:"remote-pod-networks"`
}

type ClusterRemoteNetwork struct {
	CIDRs []string `ub:"cidrs"`
}

type ClusterStorageConfig struct {
	BlockStorage *ClusterBlockStorage `ub:"block-storage"`
}

type ClusterBlockStorage struct {
	Enabled bool `ub:"enabled"`
}

type ClusterUpgradePolicy struct {
	SupportType *string `ub:"support-type"`
}

type ClusterZonalShiftConfig struct {
	Enabled bool `ub:"enabled"`
}
