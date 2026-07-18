package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

const (
	clusterName   = "unobin-it-eks-cluster"
	nodeGroupName = "unobin-it-eks-workers"
)

type verifierClient interface {
	DescribeCluster(context.Context, *eks.DescribeClusterInput,
		...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	DescribeNodegroup(context.Context, *eks.DescribeNodegroupInput,
		...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error)
	ListTagsForResource(context.Context, *eks.ListTagsForResourceInput,
		...func(*eks.Options)) (*eks.ListTagsForResourceOutput, error)
}

type clusterIdentity struct {
	ARN                string `json:"arn"`
	CreatedAt          string `json:"created_at"`
	NodeGroupARN       string `json:"node_group_arn"`
	NodeGroupCreatedAt string `json:"node_group_created_at"`
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	configuration, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	client := eks.NewFromConfig(configuration)
	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "updated":
		return verifyUpdated(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied, updated, or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client verifierClient) error {
	initialTags := map[string]string{"change": "old", "keep": "1", "remove": "yes"}
	identity, err := verifyPresent(ctx, client, initialTags, initialTags)
	if err != nil {
		return err
	}
	return writeIdentity(identity)
}

func verifyUpdated(ctx context.Context, client verifierClient) error {
	recorded, err := readIdentity()
	if err != nil {
		return err
	}
	current, err := verifyPresent(
		ctx,
		client,
		map[string]string{"add": "yes", "change": "new", "keep": "1"},
		map[string]string{"add": "yes", "change": "new", "keep": "1"},
	)
	if err != nil {
		return err
	}
	if current != recorded {
		return fmt.Errorf("cluster identity changed: got %+v, want %+v", current, recorded)
	}
	return nil
}

func verifyDestroyed(ctx context.Context, client verifierClient) error {
	nodeGroup, err := findNodeGroup(ctx, client)
	if err != nil {
		return err
	}
	if nodeGroup != nil {
		return fmt.Errorf("node group %s still exists", nodeGroupName)
	}
	cluster, err := findCluster(ctx, client)
	if err != nil {
		return err
	}
	if cluster != nil {
		return fmt.Errorf("cluster %s still exists", clusterName)
	}
	return nil
}

func verifyPresent(
	ctx context.Context,
	client verifierClient,
	expectedClusterTags map[string]string,
	expectedNodeGroupTags map[string]string,
) (clusterIdentity, error) {
	cluster, err := findCluster(ctx, client)
	if err != nil {
		return clusterIdentity{}, err
	}
	if cluster == nil {
		return clusterIdentity{}, fmt.Errorf("cluster %s not found", clusterName)
	}
	if aws.ToString(cluster.Name) != clusterName {
		return clusterIdentity{}, fmt.Errorf("cluster name is %q", aws.ToString(cluster.Name))
	}
	if cluster.Status != ekstypes.ClusterStatusActive {
		return clusterIdentity{}, fmt.Errorf("cluster status is %s, want ACTIVE", cluster.Status)
	}
	arn := aws.ToString(cluster.Arn)
	if arn == "" {
		return clusterIdentity{}, errors.New("cluster ARN is empty")
	}
	if cluster.CreatedAt == nil || cluster.CreatedAt.IsZero() {
		return clusterIdentity{}, errors.New("cluster creation time is empty")
	}
	for field, value := range map[string]string{
		"endpoint":         aws.ToString(cluster.Endpoint),
		"platform-version": aws.ToString(cluster.PlatformVersion),
		"version":          aws.ToString(cluster.Version),
	} {
		if value == "" {
			return clusterIdentity{}, fmt.Errorf("cluster %s is empty", field)
		}
	}
	if err := verifyTags(ctx, client, "cluster", arn, expectedClusterTags); err != nil {
		return clusterIdentity{}, err
	}
	nodeGroup, err := findNodeGroup(ctx, client)
	if err != nil {
		return clusterIdentity{}, err
	}
	if nodeGroup == nil {
		return clusterIdentity{}, fmt.Errorf("node group %s not found", nodeGroupName)
	}
	if aws.ToString(nodeGroup.ClusterName) != clusterName {
		return clusterIdentity{}, fmt.Errorf(
			"node group cluster is %q", aws.ToString(nodeGroup.ClusterName),
		)
	}
	if aws.ToString(nodeGroup.NodegroupName) != nodeGroupName {
		return clusterIdentity{}, fmt.Errorf(
			"node group name is %q", aws.ToString(nodeGroup.NodegroupName),
		)
	}
	if nodeGroup.Status != ekstypes.NodegroupStatusActive {
		return clusterIdentity{}, fmt.Errorf(
			"node group status is %s, want ACTIVE", nodeGroup.Status,
		)
	}
	nodeGroupARN := aws.ToString(nodeGroup.NodegroupArn)
	if nodeGroupARN == "" {
		return clusterIdentity{}, errors.New("node group ARN is empty")
	}
	if nodeGroup.CreatedAt == nil || nodeGroup.CreatedAt.IsZero() {
		return clusterIdentity{}, errors.New("node group creation time is empty")
	}
	if aws.ToString(nodeGroup.NodeRole) == "" {
		return clusterIdentity{}, errors.New("node group role is empty")
	}
	if len(nodeGroup.Subnets) != 2 {
		return clusterIdentity{}, fmt.Errorf(
			"node group has %d subnets, want 2", len(nodeGroup.Subnets),
		)
	}
	if err := verifyNodeGroupScaling(nodeGroup.ScalingConfig); err != nil {
		return clusterIdentity{}, err
	}
	if err := verifyTags(
		ctx, client, "node group", nodeGroupARN, expectedNodeGroupTags,
	); err != nil {
		return clusterIdentity{}, err
	}
	return clusterIdentity{
		ARN:                arn,
		CreatedAt:          cluster.CreatedAt.UTC().Format(time.RFC3339Nano),
		NodeGroupARN:       nodeGroupARN,
		NodeGroupCreatedAt: nodeGroup.CreatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func verifyNodeGroupScaling(config *ekstypes.NodegroupScalingConfig) error {
	if config == nil {
		return errors.New("node group scaling configuration is empty")
	}
	if aws.ToInt32(config.DesiredSize) != 0 || aws.ToInt32(config.MinSize) != 0 ||
		aws.ToInt32(config.MaxSize) != 1 {
		return fmt.Errorf(
			"node group scaling is desired=%d min=%d max=%d, want 0/0/1",
			aws.ToInt32(config.DesiredSize),
			aws.ToInt32(config.MinSize),
			aws.ToInt32(config.MaxSize),
		)
	}
	return nil
}

func findCluster(
	ctx context.Context,
	client verifierClient,
) (*ekstypes.Cluster, error) {
	output, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		var clientError *ekstypes.ClientException
		if errors.As(err, &clientError) && strings.Contains(
			aws.ToString(clientError.Message),
			"No cluster found for name:",
		) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe EKS cluster: %w", err)
	}
	if output == nil {
		return nil, nil
	}
	return output.Cluster, nil
}

func findNodeGroup(
	ctx context.Context,
	client verifierClient,
) (*ekstypes.Nodegroup, error) {
	output, err := client.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(nodeGroupName),
	})
	if err != nil {
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe EKS node group: %w", err)
	}
	if output == nil {
		return nil, nil
	}
	return output.Nodegroup, nil
}

func verifyTags(
	ctx context.Context,
	client verifierClient,
	resource string,
	arn string,
	expected map[string]string,
) error {
	output, err := client.ListTagsForResource(ctx, &eks.ListTagsForResourceInput{
		ResourceArn: aws.String(arn),
	})
	if err != nil {
		return fmt.Errorf("list EKS %s tags: %w", resource, err)
	}
	actual := map[string]string{}
	if output != nil {
		actual = maps.Clone(output.Tags)
	}
	for key := range actual {
		if strings.HasPrefix(key, "aws:") {
			delete(actual, key)
		}
	}
	if !maps.Equal(actual, expected) {
		return fmt.Errorf("%s tags are %v, want %v", resource, actual, expected)
	}
	return nil
}

func writeIdentity(identity clusterIdentity) error {
	encoded, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("encode cluster identity: %w", err)
	}
	if err := os.WriteFile(identityPath(), encoded, 0o600); err != nil {
		return fmt.Errorf("record cluster identity: %w", err)
	}
	return nil
}

func readIdentity() (clusterIdentity, error) {
	encoded, err := os.ReadFile(identityPath())
	if err != nil {
		return clusterIdentity{}, fmt.Errorf("read cluster identity: %w", err)
	}
	var identity clusterIdentity
	if err := json.Unmarshal(encoded, &identity); err != nil {
		return clusterIdentity{}, fmt.Errorf("decode cluster identity: %w", err)
	}
	if identity.ARN == "" || identity.CreatedAt == "" ||
		identity.NodeGroupARN == "" || identity.NodeGroupCreatedAt == "" {
		return clusterIdentity{}, errors.New("recorded cluster identity is incomplete")
	}
	return identity, nil
}

func identityPath() string {
	return filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "eks-cluster-identity.json")
}
