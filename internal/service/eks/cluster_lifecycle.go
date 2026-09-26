package eks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *ClusterResource) createWithClient(
	ctx context.Context,
	client eksClient,
	clock clusterClock,
) (*ClusterResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	input := r.createInput()
	var response *eks.CreateClusterOutput
	err := retryClusterCreate(ctx, clock, func(ctx context.Context) error {
		var err error
		response, err = client.CreateCluster(ctx, input)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create cluster %s: %w", r.Name, err)
	}
	if response == nil || response.Cluster == nil {
		return nil, fmt.Errorf("create cluster %s: response has no cluster", r.Name)
	}
	name := aws.ToString(response.Cluster.Name)
	if name == "" {
		return nil, fmt.Errorf("create cluster %s: response has no cluster name", r.Name)
	}
	if err := waitClusterCreated(ctx, client, name, clock); err != nil {
		return nil, err
	}
	return r.readWithClient(ctx, client, name)
}

func (r *ClusterResource) readWithClient(
	ctx context.Context,
	client eksClient,
	name string,
) (*ClusterResourceOutput, error) {
	cluster, err := describeCluster(ctx, client, name)
	if err != nil {
		return nil, err
	}
	return clusterOutput(cluster), nil
}

func (r *ClusterResource) updateWithClient(
	ctx context.Context,
	client eksClient,
	prior runtime.Prior[ClusterResource, *ClusterResourceOutput, *awsCfg],
	clock clusterClock,
) (*ClusterResourceOutput, error) {
	return r.updateCluster(ctx, client, prior, clock)
}

func (r *ClusterResource) deleteWithClient(
	ctx context.Context,
	client eksClient,
	prior *ClusterResourceOutput,
	clock clusterClock,
) error {
	if prior == nil || prior.Name == "" {
		return errors.New("prior cluster output has no name")
	}
	accepted, err := deleteCluster(ctx, client, prior.Name, clock)
	if err != nil || !accepted {
		return err
	}
	return waitClusterDeleted(ctx, client, prior.Name, clock)
}

func clusterOutput(cluster *ekstypes.Cluster) *ClusterResourceOutput {
	output := &ClusterResourceOutput{
		Name:            aws.ToString(cluster.Name),
		Arn:             aws.ToString(cluster.Arn),
		ClusterID:       copyStringPointer(cluster.Id),
		Endpoint:        aws.ToString(cluster.Endpoint),
		PlatformVersion: aws.ToString(cluster.PlatformVersion),
		Status:          string(cluster.Status),
		Version:         aws.ToString(cluster.Version),
	}
	if cluster.CertificateAuthority != nil {
		output.CertificateAuthorityData = aws.ToString(cluster.CertificateAuthority.Data)
	}
	if cluster.CreatedAt != nil {
		output.CreatedAt = cluster.CreatedAt.UTC().Format(time.RFC3339)
	}
	if cluster.Identity != nil && cluster.Identity.Oidc != nil {
		output.OIDCIssuer = copyStringPointer(cluster.Identity.Oidc.Issuer)
	}
	if network := cluster.KubernetesNetworkConfig; network != nil {
		output.IpFamily = string(network.IpFamily)
		output.ServiceIpv4Cidr = aws.ToString(network.ServiceIpv4Cidr)
		output.ServiceIPv6CIDR = copyStringPointer(network.ServiceIpv6Cidr)
	}
	if vpc := cluster.ResourcesVpcConfig; vpc != nil {
		output.ClusterSecurityGroupID = aws.ToString(vpc.ClusterSecurityGroupId)
		output.VPCID = aws.ToString(vpc.VpcId)
	}
	return output
}

func copyStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
