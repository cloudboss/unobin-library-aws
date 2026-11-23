package eks

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

const clusterLookupTestName = "MiXeD_12-test"

var clusterLookupTestTime = time.Date(
	2026, time.June, 17, 12, 30, 45, 123000000, time.FixedZone("offset", -4*60*60),
)

type clusterLookupClient struct {
	requests []*eks.DescribeClusterInput
	result   *eks.DescribeClusterOutput
	err      error
}

func (c *clusterLookupClient) DescribeCluster(
	_ context.Context, request *eks.DescribeClusterInput, _ ...func(*eks.Options),
) (*eks.DescribeClusterOutput, error) {
	c.requests = append(c.requests, request)
	return c.result, c.err
}

func readClusterLookup(
	t *testing.T, result *eks.DescribeClusterOutput, apiErr error,
) (*ClusterDataSourceOutput, error) {
	t.Helper()
	client := &clusterLookupClient{result: result, err: apiErr}
	lookup := &ClusterDataSource{Name: clusterLookupTestName}
	got, err := lookup.read(t.Context(), client)
	require.Equal(t, []*eks.DescribeClusterInput{{Name: aws.String(clusterLookupTestName)}},
		client.requests)
	return got, err
}

func TestClusterLookupErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *eks.DescribeClusterOutput
		err    error
		want   string
	}{
		{
			name: "typed missing", want: "cluster not found",
			err: &ekstypes.ResourceNotFoundException{Message: aws.String("missing")},
		},
		{
			name: "client missing", want: "cluster not found",
			err: &ekstypes.ClientException{
				Message: aws.String("No cluster found for name: " + clusterLookupTestName),
			},
		},
		{
			name: "different client text", want: "no cluster found for name:",
			err: &ekstypes.ClientException{Message: aws.String("no cluster found for name: foo")},
		},
		{
			name: "denied", want: "denied",
			err: &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "denied"},
		},
		{name: "nil response", want: "no cluster returned"},
		{name: "nil cluster", result: &eks.DescribeClusterOutput{}, want: "no cluster returned"},
		{
			name: "missing creation time", want: "missing creation time",
			result: &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readClusterLookup(t, tc.result, tc.err)
			require.Nil(t, got)
			require.ErrorContains(t, err, `describe EKS cluster "MiXeD_12-test":`)
			require.ErrorContains(t, err, tc.want)
			require.NotErrorIs(t, err, runtime.ErrNotFound)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
			if tc.name == "different client text" {
				require.NotContains(t, err.Error(), "cluster not found")
			}
		})
	}
}

func TestClusterLookupPopulatedResponse(t *testing.T) {
	cluster := &ekstypes.Cluster{
		Name:               aws.String("Canonical"),
		Arn:                aws.String("arn:aws-us-gov:eks:us-gov-west-1:123:cluster/Canonical"),
		CreatedAt:          &clusterLookupTestTime,
		Endpoint:           aws.String("https://control.example"),
		PlatformVersion:    aws.String("eks.3"),
		RoleArn:            aws.String("arn:aws-us-gov:iam::123:role/cluster"),
		Status:             ekstypes.ClusterStatusCreating,
		Version:            aws.String("1.34"),
		Id:                 aws.String("outpost-cluster-id"),
		DeletionProtection: aws.Bool(true),
		Logging: &ekstypes.Logging{ClusterLogging: []ekstypes.LogSetup{
			{Enabled: aws.Bool(true), Types: []ekstypes.LogType{"audit", "api", "audit"}},
			{Enabled: aws.Bool(true), Types: []ekstypes.LogType{"new-log-type", "api"}},
			{Enabled: aws.Bool(false), Types: []ekstypes.LogType{"scheduler"}},
			{Types: []ekstypes.LogType{"authenticator"}},
		}},
		Tags: map[string]string{"Name": "canonical", "empty": "", "aws:system": "filtered"},
		AccessConfig: &ekstypes.AccessConfigResponse{
			AuthenticationMode: "NEW_AUTH_MODE", BootstrapClusterCreatorAdminPermissions: aws.Bool(false),
		},
		CertificateAuthority: &ekstypes.Certificate{Data: aws.String("YWJjCg==")},
		ComputeConfig: &ekstypes.ComputeConfigResponse{
			Enabled: aws.Bool(true), NodePools: []string{"system", "general-purpose", "system"},
			NodeRoleArn: aws.String("arn:aws-us-gov:iam::123:role/nodes"),
		},
		ControlPlaneScalingConfig: &ekstypes.ControlPlaneScalingConfig{Tier: "new-tier"},
		Identity: &ekstypes.Identity{Oidc: &ekstypes.OIDC{
			Issuer: aws.String("https://issuer.example"),
		}},
		KubernetesNetworkConfig: &ekstypes.KubernetesNetworkConfigResponse{
			ElasticLoadBalancing: &ekstypes.ElasticLoadBalancing{Enabled: aws.Bool(true)},
			IpFamily:             "new-family",
			ServiceIpv4Cidr:      aws.String("10.100.0.0/16"),
			ServiceIpv6Cidr:      aws.String("fd00::/108"),
		},
		OutpostConfig: &ekstypes.OutpostConfigResponse{
			ControlPlaneInstanceType: aws.String("m7i.xlarge"),
			ControlPlanePlacement: &ekstypes.ControlPlanePlacementResponse{
				GroupName: aws.String("spread-group"), SpreadLevel: "new-control-plane-spread",
			},
			EtcdInstanceType: aws.String("m7i.large"),
			EtcdPlacement:    &ekstypes.EtcdPlacementResponse{SpreadLevel: "new-etcd-spread"},
			OutpostArns:      []string{"arn:outpost:b", "arn:outpost:a", "arn:outpost:b"},
		},
		RemoteNetworkConfig: &ekstypes.RemoteNetworkConfigResponse{
			RemoteNodeNetworks: []ekstypes.RemoteNodeNetwork{
				{Cidrs: []string{"10.3.0.0/16", "10.2.0.0/16", "10.3.0.0/16"}},
				{Cidrs: []string{"10.1.0.0/16"}},
			},
			RemotePodNetworks: []ekstypes.RemotePodNetwork{
				{Cidrs: []string{"10.6.0.0/16", "10.5.0.0/16", "10.6.0.0/16"}},
				{Cidrs: []string{"10.4.0.0/16"}},
			},
		},
		StorageConfig: &ekstypes.StorageConfigResponse{
			BlockStorage: &ekstypes.BlockStorage{Enabled: aws.Bool(true)},
		},
		UpgradePolicy:    &ekstypes.UpgradePolicyResponse{SupportType: "NEW_SUPPORT"},
		ZonalShiftConfig: &ekstypes.ZonalShiftConfigResponse{Enabled: aws.Bool(true)},
		ResourcesVpcConfig: &ekstypes.VpcConfigResponse{
			ClusterSecurityGroupId: aws.String("sg-control-plane"),
			ControlPlaneEgressMode: "NEW_EGRESS_MODE",
			EndpointPrivateAccess:  true,
			EndpointPublicAccess:   true,
			PublicAccessCidrs:      []string{"203.0.113.0/24", "192.0.2.0/24", "203.0.113.0/24"},
			SecurityGroupIds:       []string{"sg-b", "sg-a", "sg-b"},
			SubnetIds:              []string{"subnet-b", "subnet-a", "subnet-b"},
			VpcId:                  aws.String("vpc-1234"),
		},
	}

	got, err := readClusterLookup(t, &eks.DescribeClusterOutput{Cluster: cluster}, nil)

	require.NoError(t, err)
	require.Equal(t, &ClusterDataSourceOutput{
		Name:                   "Canonical",
		Arn:                    "arn:aws-us-gov:eks:us-gov-west-1:123:cluster/Canonical",
		CreatedAt:              "2026-06-17T12:30:45-04:00",
		Endpoint:               "https://control.example",
		PlatformVersion:        "eks.3",
		RoleArn:                "arn:aws-us-gov:iam::123:role/cluster",
		Status:                 "CREATING",
		Version:                "1.34",
		ClusterID:              aws.String("outpost-cluster-id"),
		DeletionProtection:     true,
		EnabledClusterLogTypes: []string{"api", "audit", "new-log-type"},
		Tags:                   map[string]string{"Name": "canonical", "empty": ""},
		AccessConfig: &clusterLookupAccess{
			AuthenticationMode: "NEW_AUTH_MODE", BootstrapClusterCreatorAdminPermissions: true,
		},
		CertificateAuthority: &clusterLookupCertificate{Data: "YWJjCg=="},
		ComputeConfig: &clusterLookupCompute{
			Enabled: true, NodePools: []string{"general-purpose", "system"},
			NodeRoleArn: "arn:aws-us-gov:iam::123:role/nodes",
		},
		ControlPlaneScalingConfig: &clusterLookupScaling{Tier: "new-tier"},
		Identity: &clusterLookupIdentity{
			OIDC: &clusterLookupOIDC{Issuer: "https://issuer.example"},
		},
		KubernetesNetworkConfig: &clusterLookupKubernetesNetwork{
			ElasticLoadBalancing: &ClusterElasticLoadBalancing{Enabled: true},
			IPFamily:             "new-family",
			ServiceIPv4CIDR:      "10.100.0.0/16",
			ServiceIPv6CIDR:      "fd00::/108",
		},
		OutpostConfig: &clusterLookupOutpost{
			ControlPlaneInstanceType: "m7i.xlarge",
			ControlPlanePlacement: &clusterLookupControlPlanePlacement{
				GroupName: "spread-group", SpreadLevel: "new-control-plane-spread",
			},
			EtcdInstanceType: "m7i.large",
			EtcdPlacement:    &clusterLookupEtcdPlacement{SpreadLevel: "new-etcd-spread"},
			OutpostArns:      []string{"arn:outpost:a", "arn:outpost:b"},
		},
		RemoteNetworkConfig: &clusterLookupRemoteNetwork{
			RemoteNodeNetworks: []ClusterRemoteNetwork{
				{CIDRs: []string{"10.2.0.0/16", "10.3.0.0/16"}},
				{CIDRs: []string{"10.1.0.0/16"}},
			},
			RemotePodNetworks: []ClusterRemoteNetwork{
				{CIDRs: []string{"10.5.0.0/16", "10.6.0.0/16"}},
				{CIDRs: []string{"10.4.0.0/16"}},
			},
		},
		StorageConfig:    &ClusterStorageConfig{BlockStorage: &ClusterBlockStorage{Enabled: true}},
		UpgradePolicy:    &clusterLookupUpgradePolicy{SupportType: "NEW_SUPPORT"},
		ZonalShiftConfig: &ClusterZonalShiftConfig{Enabled: true},
		VPCConfig: &clusterLookupVPC{
			ClusterSecurityGroupID: "sg-control-plane",
			ControlPlaneEgressMode: "NEW_EGRESS_MODE",
			EndpointPrivateAccess:  true,
			EndpointPublicAccess:   true,
			PublicAccessCIDRs:      []string{"192.0.2.0/24", "203.0.113.0/24"},
			SecurityGroupIDs:       []string{"sg-a", "sg-b"},
			SubnetIDs:              []string{"subnet-a", "subnet-b"},
			VPCID:                  "vpc-1234",
		},
	}, got)
}

func TestClusterLookupAbsentAndSparseResponse(t *testing.T) {
	cluster := &ekstypes.Cluster{
		CreatedAt: &clusterLookupTestTime,
		Id:        aws.String("ignore-regular-cluster-id"),
	}
	want := ClusterDataSourceOutput{
		CreatedAt: "2026-06-17T12:30:45-04:00", EnabledClusterLogTypes: []string{},
		Tags: map[string]string{},
	}
	response := &eks.DescribeClusterOutput{Cluster: cluster}
	got, err := readClusterLookup(t, response, nil)
	require.NoError(t, err)
	require.Equal(t, &want, got)

	cluster.Id = nil
	cluster.AccessConfig = &ekstypes.AccessConfigResponse{}
	cluster.CertificateAuthority = &ekstypes.Certificate{}
	cluster.ComputeConfig = &ekstypes.ComputeConfigResponse{}
	cluster.ControlPlaneScalingConfig = &ekstypes.ControlPlaneScalingConfig{}
	cluster.Identity = &ekstypes.Identity{}
	cluster.KubernetesNetworkConfig = &ekstypes.KubernetesNetworkConfigResponse{}
	cluster.OutpostConfig = &ekstypes.OutpostConfigResponse{}
	cluster.RemoteNetworkConfig = &ekstypes.RemoteNetworkConfigResponse{}
	cluster.StorageConfig = &ekstypes.StorageConfigResponse{}
	cluster.UpgradePolicy = &ekstypes.UpgradePolicyResponse{}
	cluster.ZonalShiftConfig = &ekstypes.ZonalShiftConfigResponse{}
	cluster.ResourcesVpcConfig = &ekstypes.VpcConfigResponse{}
	cluster.Tags = map[string]string{"aws:reserved": "ignored"}

	want.AccessConfig = &clusterLookupAccess{BootstrapClusterCreatorAdminPermissions: true}
	want.CertificateAuthority = &clusterLookupCertificate{}
	want.ComputeConfig = &clusterLookupCompute{NodePools: []string{}}
	want.ControlPlaneScalingConfig = &clusterLookupScaling{}
	want.Identity = &clusterLookupIdentity{}
	want.KubernetesNetworkConfig = &clusterLookupKubernetesNetwork{}
	want.OutpostConfig = &clusterLookupOutpost{OutpostArns: []string{}}
	want.RemoteNetworkConfig = &clusterLookupRemoteNetwork{
		RemoteNodeNetworks: []ClusterRemoteNetwork{}, RemotePodNetworks: []ClusterRemoteNetwork{},
	}
	want.StorageConfig = &ClusterStorageConfig{}
	want.UpgradePolicy = &clusterLookupUpgradePolicy{}
	want.ZonalShiftConfig = &ClusterZonalShiftConfig{}
	want.VPCConfig = &clusterLookupVPC{
		PublicAccessCIDRs: []string{}, SecurityGroupIDs: []string{}, SubnetIDs: []string{},
	}
	got, err = readClusterLookup(t, response, nil)
	require.NoError(t, err)
	require.Equal(t, &want, got)
}
