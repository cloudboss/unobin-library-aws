package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

const (
	clusterName = "unobin-it-eks-policy"
	roleName    = "unobin-it-eks-policy-principal"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("verify EKS access policy association: %v", err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}
	client := eks.NewFromConfig(cfg)
	path := filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "association-principal")
	phase := os.Getenv("VERIFY_PHASE")
	var principal string
	if phase == "applied" {
		principal, err = clusterPrincipal(ctx, client)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(principal), 0o600); err != nil {
			return fmt.Errorf("save association principal: %w", err)
		}
	} else {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("load association principal: %w", err)
		}
		principal = string(data)
	}
	identity, err := awsarn.Parse(principal)
	if err != nil {
		return fmt.Errorf("parse principal ARN: %w", err)
	}
	policyARN := fmt.Sprintf("arn:%s:eks::aws:cluster-access-policy/AmazonEKSViewPolicy",
		identity.Partition)
	policies, err := matchingPolicies(ctx, client, principal, policyARN)
	if err != nil {
		return err
	}
	switch phase {
	case "applied":
		return verifyScope(policies, "cluster", nil)
	case "updated":
		return verifyScope(policies, "namespace", []string{"default", "kube-system"})
	case "destroyed":
		if len(policies) != 0 {
			return fmt.Errorf("access policy association still exists for %s", principal)
		}
		return nil
	default:
		return fmt.Errorf("unexpected VERIFY_PHASE %q", phase)
	}
}

func clusterPrincipal(ctx context.Context, client *eks.Client) (string, error) {
	cluster, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		return "", fmt.Errorf("describe EKS cluster: %w", err)
	}
	if cluster.Cluster == nil {
		return "", errors.New("describe EKS cluster returned no cluster")
	}
	identity, err := awsarn.Parse(aws.ToString(cluster.Cluster.Arn))
	if err != nil {
		return "", fmt.Errorf("parse cluster ARN: %w", err)
	}
	return fmt.Sprintf("arn:%s:iam::%s:role/%s", identity.Partition, identity.AccountID, roleName),
		nil
}

func matchingPolicies(
	ctx context.Context, client *eks.Client, principal, policyARN string,
) ([]ekstypes.AssociatedAccessPolicy, error) {
	request := &eks.ListAssociatedAccessPoliciesInput{
		ClusterName: aws.String(clusterName), PrincipalArn: aws.String(principal),
	}
	pages := eks.NewListAssociatedAccessPoliciesPaginator(client, request)
	var matched []ekstypes.AssociatedAccessPolicy
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		var missing *ekstypes.ResourceNotFoundException
		if errors.As(err, &missing) {
			return matched, nil
		}
		if err != nil {
			return nil, fmt.Errorf("list associated access policies: %w", err)
		}
		for _, policy := range page.AssociatedAccessPolicies {
			if aws.ToString(policy.PolicyArn) == policyARN {
				matched = append(matched, policy)
			}
		}
	}
	return matched, nil
}

func verifyScope(policies []ekstypes.AssociatedAccessPolicy, scopeType string, ns []string) error {
	if len(policies) != 1 || policies[0].AccessScope == nil {
		return fmt.Errorf("expected one associated policy with scope, got %v", policies)
	}
	scope := policies[0].AccessScope
	if string(scope.Type) != scopeType {
		return fmt.Errorf("scope type is %q, want %q", scope.Type, scopeType)
	}
	if got := slices.Sorted(slices.Values(scope.Namespaces)); !slices.Equal(got, ns) {
		return fmt.Errorf("scope namespaces are %v, want %v", got, ns)
	}
	return nil
}
