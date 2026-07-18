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

const clusterName = "unobin-it-eks-cluster"

type verifierClient interface {
	DescribeCluster(context.Context, *eks.DescribeClusterInput,
		...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	ListTagsForResource(context.Context, *eks.ListTagsForResourceInput,
		...func(*eks.Options)) (*eks.ListTagsForResourceOutput, error)
}

type clusterIdentity struct {
	ARN       string `json:"arn"`
	CreatedAt string `json:"created_at"`
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
	identity, err := verifyPresent(ctx, client, map[string]string{
		"change": "old", "keep": "1", "remove": "yes",
	})
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
	current, err := verifyPresent(ctx, client, map[string]string{
		"add": "yes", "change": "new", "keep": "1",
	})
	if err != nil {
		return err
	}
	if current != recorded {
		return fmt.Errorf("cluster identity changed: got %+v, want %+v", current, recorded)
	}
	return nil
}

func verifyDestroyed(ctx context.Context, client verifierClient) error {
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
	expectedTags map[string]string,
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
	if err := verifyTags(ctx, client, arn, expectedTags); err != nil {
		return clusterIdentity{}, err
	}
	return clusterIdentity{
		ARN: arn, CreatedAt: cluster.CreatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
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

func verifyTags(
	ctx context.Context,
	client verifierClient,
	arn string,
	expected map[string]string,
) error {
	output, err := client.ListTagsForResource(ctx, &eks.ListTagsForResourceInput{
		ResourceArn: aws.String(arn),
	})
	if err != nil {
		return fmt.Errorf("list EKS cluster tags: %w", err)
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
		return fmt.Errorf("cluster tags are %v, want %v", actual, expected)
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
	if identity.ARN == "" || identity.CreatedAt == "" {
		return clusterIdentity{}, errors.New("recorded cluster identity is incomplete")
	}
	return identity, nil
}

func identityPath() string {
	return filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "eks-cluster-identity.json")
}
