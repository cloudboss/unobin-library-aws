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
	clusterName        = "unobin-it-eks-cluster"
	nodeGroupName      = "unobin-it-eks-workers"
	fargateProfileName = "unobin-it-eks-pods"
	addonName          = "vpc-cni"
)

type verifierClient interface {
	DescribeCluster(context.Context, *eks.DescribeClusterInput,
		...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	DescribeNodegroup(context.Context, *eks.DescribeNodegroupInput,
		...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error)
	DescribeFargateProfile(context.Context, *eks.DescribeFargateProfileInput,
		...func(*eks.Options)) (*eks.DescribeFargateProfileOutput, error)
	DescribeAddon(context.Context, *eks.DescribeAddonInput,
		...func(*eks.Options)) (*eks.DescribeAddonOutput, error)
	ListTagsForResource(context.Context, *eks.ListTagsForResourceInput,
		...func(*eks.Options)) (*eks.ListTagsForResourceOutput, error)
}

type clusterIdentity struct {
	ARN                string `json:"arn"`
	CreatedAt          string `json:"created_at"`
	NodeGroupARN       string `json:"node_group_arn"`
	NodeGroupCreatedAt string `json:"node_group_created_at"`
	FargateProfileARN  string `json:"fargate_profile_arn"`
	AddonARN           string `json:"addon_arn"`
	AddonCreatedAt     string `json:"addon_created_at"`
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
	identity, err := verifyPresent(ctx, client, initialTags, initialTags, initialTags, initialTags)
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
	addon, err := findAddon(ctx, client)
	if err != nil {
		return err
	}
	if addon != nil {
		return fmt.Errorf("add-on %s still exists", addonName)
	}
	nodeGroup, err := findNodeGroup(ctx, client)
	if err != nil {
		return err
	}
	if nodeGroup != nil {
		return fmt.Errorf("node group %s still exists", nodeGroupName)
	}
	profile, err := findFargateProfile(ctx, client)
	if err != nil {
		return err
	}
	if profile != nil {
		return fmt.Errorf("fargate profile %s still exists", fargateProfileName)
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
	expectedFargateProfileTags map[string]string,
	expectedAddonTags map[string]string,
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
	profile, err := findFargateProfile(ctx, client)
	if err != nil {
		return clusterIdentity{}, err
	}
	if profile == nil {
		return clusterIdentity{}, fmt.Errorf("fargate profile %s not found", fargateProfileName)
	}
	if aws.ToString(profile.ClusterName) != clusterName {
		return clusterIdentity{}, fmt.Errorf(
			"fargate profile cluster is %q", aws.ToString(profile.ClusterName),
		)
	}
	if aws.ToString(profile.FargateProfileName) != fargateProfileName {
		return clusterIdentity{}, fmt.Errorf(
			"fargate profile name is %q", aws.ToString(profile.FargateProfileName),
		)
	}
	if profile.Status != ekstypes.FargateProfileStatusActive {
		return clusterIdentity{}, fmt.Errorf(
			"fargate profile status is %s, want ACTIVE", profile.Status,
		)
	}
	profileARN := aws.ToString(profile.FargateProfileArn)
	if profileARN == "" {
		return clusterIdentity{}, errors.New("fargate profile ARN is empty")
	}
	if aws.ToString(profile.PodExecutionRoleArn) == "" {
		return clusterIdentity{}, errors.New("fargate profile pod execution role is empty")
	}
	if len(profile.Selectors) != 1 ||
		aws.ToString(profile.Selectors[0].Namespace) != "unobin-fargate" {
		return clusterIdentity{}, fmt.Errorf("fargate profile selectors are %v",
			profile.Selectors)
	}
	if len(profile.Subnets) != 2 {
		return clusterIdentity{}, fmt.Errorf(
			"fargate profile has %d subnets, want 2", len(profile.Subnets),
		)
	}
	if err := verifyTags(
		ctx, client, "Fargate profile", profileARN, expectedFargateProfileTags,
	); err != nil {
		return clusterIdentity{}, err
	}
	addon, err := findAddon(ctx, client)
	if err != nil {
		return clusterIdentity{}, err
	}
	if addon == nil {
		return clusterIdentity{}, fmt.Errorf("add-on %s not found", addonName)
	}
	if aws.ToString(addon.ClusterName) != clusterName {
		return clusterIdentity{}, fmt.Errorf(
			"add-on cluster is %q", aws.ToString(addon.ClusterName),
		)
	}
	if aws.ToString(addon.AddonName) != addonName {
		return clusterIdentity{}, fmt.Errorf(
			"add-on name is %q", aws.ToString(addon.AddonName),
		)
	}
	if addon.Status != ekstypes.AddonStatusActive {
		return clusterIdentity{}, fmt.Errorf(
			"add-on status is %s, want ACTIVE", addon.Status,
		)
	}
	addonARN := aws.ToString(addon.AddonArn)
	if addonARN == "" {
		return clusterIdentity{}, errors.New("add-on ARN is empty")
	}
	if addon.CreatedAt == nil || addon.CreatedAt.IsZero() {
		return clusterIdentity{}, errors.New("add-on creation time is empty")
	}
	if err := verifyTags(ctx, client, "add-on", addonARN, expectedAddonTags); err != nil {
		return clusterIdentity{}, err
	}
	return clusterIdentity{
		ARN:                arn,
		CreatedAt:          cluster.CreatedAt.UTC().Format(time.RFC3339Nano),
		NodeGroupARN:       nodeGroupARN,
		NodeGroupCreatedAt: nodeGroup.CreatedAt.UTC().Format(time.RFC3339Nano),
		FargateProfileARN:  profileARN,
		AddonARN:           addonARN,
		AddonCreatedAt:     addon.CreatedAt.UTC().Format(time.RFC3339Nano),
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

func findAddon(
	ctx context.Context,
	client verifierClient,
) (*ekstypes.Addon, error) {
	output, err := client.DescribeAddon(ctx, &eks.DescribeAddonInput{
		ClusterName: aws.String(clusterName),
		AddonName:   aws.String(addonName),
	})
	if err != nil {
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe EKS add-on: %w", err)
	}
	if output == nil {
		return nil, nil
	}
	return output.Addon, nil
}

func findFargateProfile(
	ctx context.Context,
	client verifierClient,
) (*ekstypes.FargateProfile, error) {
	output, err := client.DescribeFargateProfile(ctx, &eks.DescribeFargateProfileInput{
		ClusterName:        aws.String(clusterName),
		FargateProfileName: aws.String(fargateProfileName),
	})
	if err != nil {
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe EKS Fargate profile: %w", err)
	}
	if output == nil {
		return nil, nil
	}
	return output.FargateProfile, nil
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
		identity.NodeGroupARN == "" || identity.NodeGroupCreatedAt == "" ||
		identity.FargateProfileARN == "" ||
		identity.AddonARN == "" || identity.AddonCreatedAt == "" {
		return clusterIdentity{}, errors.New("recorded cluster identity is incomplete")
	}
	return identity, nil
}

func identityPath() string {
	return filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "eks-cluster-identity.json")
}
