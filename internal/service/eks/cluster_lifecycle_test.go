package eks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterCreateRetriesWaitsAndReadsFinalState(t *testing.T) {
	createdAt := time.Date(2026, time.July, 17, 12, 30, 0, 0, time.UTC)
	client := &fakeEKSClient{
		createResults: []createResult{
			{err: &ekstypes.InvalidParameterException{
				Message: aws.String("role does not exist"),
			}},
			{output: &eks.CreateClusterOutput{Cluster: &ekstypes.Cluster{
				Name: aws.String("example"),
			}}},
		},
		describeResults: []clusterDescribeResult{
			{cluster: &ekstypes.Cluster{
				Name: aws.String("example"), Status: ekstypes.ClusterStatusCreating,
			}},
			{cluster: &ekstypes.Cluster{
				Name: aws.String("example"), Status: ekstypes.ClusterStatusActive,
			}},
			{cluster: completeSDKCluster(createdAt)},
		},
	}
	resource := validClusterResource()

	output, err := resource.createWithClient(
		context.Background(), client, newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"create", "create", "describe", "describe", "describe"},
		client.calls)
	assert.Equal(t, "example", output.Name)
	assert.Equal(t, "arn:aws:eks:us-east-1:123456789012:cluster/example", output.Arn)
	assert.Equal(t, "certificate", output.CertificateAuthorityData)
	assert.Equal(t, "2026-07-17T12:30:00Z", output.CreatedAt)
	assert.Equal(t, "https://issuer.example", aws.ToString(output.OIDCIssuer))
	assert.Equal(t, "10.0.0.0/16", output.ServiceIpv4Cidr)
	assert.Equal(t, "sg-cluster", output.ClusterSecurityGroupID)
	assert.Equal(t, "vpc-123", output.VPCID)
}

func TestClusterCreateRejectsMalformedOutput(t *testing.T) {
	tests := []struct {
		name   string
		output *eks.CreateClusterOutput
	}{
		{name: "nil output"},
		{name: "nil cluster", output: &eks.CreateClusterOutput{}},
		{name: "empty name", output: &eks.CreateClusterOutput{Cluster: &ekstypes.Cluster{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeEKSClient{createResults: []createResult{{output: tt.output}}}
			_, err := validClusterResource().createWithClient(
				context.Background(), client, newFakeClusterClock(),
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, "response")
		})
	}
}

func TestClusterReadMapsEveryAbsenceForm(t *testing.T) {
	tests := []struct {
		name   string
		result clusterDescribeResult
	}{
		{name: "typed", result: clusterDescribeResult{err: clusterNotFoundError()}},
		{name: "client text", result: clusterDescribeResult{err: &ekstypes.ClientException{
			Message: aws.String("No cluster found for name: example"),
		}}},
		{name: "nil cluster", result: clusterDescribeResult{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeEKSClient{describeResults: []clusterDescribeResult{tt.result}}
			_, err := validClusterResource().readWithClient(
				context.Background(), client, "example",
			)
			assert.ErrorIs(t, err, runtime.ErrNotFound)
		})
	}
}

func TestClusterDeleteUsesPriorNameAndAcceptsAlreadyGone(t *testing.T) {
	client := &fakeEKSClient{deleteErrors: []error{clusterNotFoundError()}}
	resource := validClusterResource()
	resource.Name = "desired-replacement"

	err := resource.deleteWithClient(
		context.Background(),
		client,
		&ClusterResourceOutput{Name: "prior-name"},
		newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, "prior-name", client.deletedNames[0])
	assert.Equal(t, []string{"delete"}, client.calls)
}

func TestClusterDeletePropagatesUnrelatedFailureWithoutCleanup(t *testing.T) {
	sentinel := errors.New("access denied")
	client := &fakeEKSClient{deleteErrors: []error{sentinel}}

	err := validClusterResource().deleteWithClient(
		context.Background(),
		client,
		&ClusterResourceOutput{Name: "example"},
		newFakeClusterClock(),
	)

	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, []string{"delete"}, client.calls)
}

func completeSDKCluster(createdAt time.Time) *ekstypes.Cluster {
	return &ekstypes.Cluster{
		Arn:                  aws.String("arn:aws:eks:us-east-1:123456789012:cluster/example"),
		CertificateAuthority: &ekstypes.Certificate{Data: aws.String("certificate")},
		CreatedAt:            &createdAt,
		Endpoint:             aws.String("https://cluster.example"),
		Id:                   aws.String("cluster-id"),
		Identity: &ekstypes.Identity{Oidc: &ekstypes.OIDC{
			Issuer: aws.String("https://issuer.example"),
		}},
		KubernetesNetworkConfig: &ekstypes.KubernetesNetworkConfigResponse{
			IpFamily:        ekstypes.IpFamilyIpv4,
			ServiceIpv4Cidr: aws.String("10.0.0.0/16"),
		},
		Name:            aws.String("example"),
		PlatformVersion: aws.String("eks.1"),
		ResourcesVpcConfig: &ekstypes.VpcConfigResponse{
			ClusterSecurityGroupId: aws.String("sg-cluster"),
			VpcId:                  aws.String("vpc-123"),
		},
		Status:  ekstypes.ClusterStatusActive,
		Version: aws.String("1.33"),
	}
}
