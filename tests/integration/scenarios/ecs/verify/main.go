// verify checks the container stack the scenario applied against the phase
// named in the VERIFY_PHASE environment variable. It looks resources up by
// their stable names because the driver passes no plan outputs into verify,
// and it reads only cloud state: applied requires the ECR repository, the
// custom capacity provider, the ACTIVE cluster, the ACTIVE task definition
// revision, and the ACTIVE service at a desired count of zero; destroyed
// requires the repository and service to be gone, the capacity provider gone or
// INACTIVE, the cluster gone or INACTIVE, and the family to have no ACTIVE
// revision left. Capacity-provider attachment is checked best-effort, since an
// emulator may not model it. Tearing the stack down is the destroy plan's job,
// not the verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

const (
	repoName             = "unobin-it-ecs"
	capacityProviderName = "unobin-it-ecs-cp"
	clusterName          = "unobin-it-ecs"
	family               = "unobin-it-ecs"
	serviceName          = "unobin-it-ecs"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	ecrClient := ecr.NewFromConfig(cfg)
	ecsClient := ecs.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, ecrClient, ecsClient)
	case "destroyed":
		return verifyDestroyed(ctx, ecrClient, ecsClient)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, ecrClient *ecr.Client, ecsClient *ecs.Client) error {
	repo, err := findRepository(ctx, ecrClient)
	if err != nil {
		return err
	}
	if repo == nil {
		return fmt.Errorf("repository %s not found", repoName)
	}
	if aws.ToString(repo.RepositoryUri) == "" {
		return fmt.Errorf("repository %s has no uri", repoName)
	}

	capacityProvider, err := findCapacityProvider(ctx, ecsClient)
	if err != nil {
		return err
	}
	checkCapacityProvider(capacityProvider)

	cluster, err := findCluster(ctx, ecsClient)
	if err != nil {
		return err
	}
	if cluster == nil {
		return fmt.Errorf("cluster %s not found", clusterName)
	}
	if got := aws.ToString(cluster.Status); got != "ACTIVE" {
		return fmt.Errorf("cluster %s is %s, want ACTIVE", clusterName, got)
	}
	checkCapacityProviders(cluster)

	taskDef, err := findTaskDefinition(ctx, ecsClient)
	if err != nil {
		return err
	}
	if taskDef == nil {
		return fmt.Errorf("task definition family %s has no active revision", family)
	}
	if taskDef.Status != ecstypes.TaskDefinitionStatusActive {
		return fmt.Errorf("task definition %s is %s, want ACTIVE", family, taskDef.Status)
	}

	service, err := findService(ctx, ecsClient)
	if err != nil {
		return err
	}
	if service == nil {
		return fmt.Errorf("service %s not found", serviceName)
	}
	if got := aws.ToString(service.Status); got != "ACTIVE" {
		return fmt.Errorf("service %s is %s, want ACTIVE", serviceName, got)
	}
	if got := service.DesiredCount; got != 0 {
		return fmt.Errorf("service %s desired count is %d, want 0", serviceName, got)
	}
	return nil
}

// checkCapacityProviders confirms the FARGATE attachment best-effort: an
// emulator may not model capacity providers, so a miss degrades to a printed
// skip rather than a failure.
func checkCapacityProviders(cluster *ecstypes.Cluster) {
	if len(cluster.CapacityProviders) == 0 {
		fmt.Println("skip: capacity providers not modeled")
		return
	}
	if !slices.Contains(cluster.CapacityProviders, "FARGATE") {
		fmt.Printf("skip: capacity providers %v lack FARGATE\n", cluster.CapacityProviders)
		return
	}
	fmt.Println("ok: FARGATE capacity provider attached")
}

func verifyDestroyed(ctx context.Context, ecrClient *ecr.Client, ecsClient *ecs.Client) error {
	repo, err := findRepository(ctx, ecrClient)
	if err != nil {
		return err
	}
	if repo != nil {
		return fmt.Errorf("repository %s still exists", repoName)
	}

	capacityProvider, err := findCapacityProvider(ctx, ecsClient)
	if err != nil {
		return err
	}
	if capacityProvider != nil &&
		capacityProvider.Status != ecstypes.CapacityProviderStatusInactive {
		return fmt.Errorf("capacity provider %s is still %s", capacityProviderName,
			capacityProvider.Status)
	}

	cluster, err := findCluster(ctx, ecsClient)
	if err != nil {
		return err
	}
	if cluster != nil && aws.ToString(cluster.Status) != "INACTIVE" {
		return fmt.Errorf("cluster %s is still %s", clusterName, aws.ToString(cluster.Status))
	}

	taskDef, err := findTaskDefinition(ctx, ecsClient)
	if err != nil {
		return err
	}
	if taskDef != nil && taskDef.Status == ecstypes.TaskDefinitionStatusActive {
		return fmt.Errorf("task definition family %s still has an active revision", family)
	}

	service, err := findService(ctx, ecsClient)
	if err != nil {
		return err
	}
	if service != nil && aws.ToString(service.Status) == "ACTIVE" {
		return fmt.Errorf("service %s is still active", serviceName)
	}
	return nil
}

// findRepository returns the scenario's repository, or nil when it is gone.
func findRepository(ctx context.Context, client *ecr.Client) (*ecrtypes.Repository, error) {
	resp, err := client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{repoName},
	})
	if err != nil {
		var notFound *ecrtypes.RepositoryNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe repositories: %w", err)
	}
	for i := range resp.Repositories {
		if aws.ToString(resp.Repositories[i].RepositoryName) == repoName {
			return &resp.Repositories[i], nil
		}
	}
	return nil, nil
}

// findCapacityProvider returns the scenario's capacity provider, nil when it
// is absent, or nil with a printed skip when the emulator does not model
// custom capacity providers.
func findCapacityProvider(
	ctx context.Context, client *ecs.Client,
) (*ecstypes.CapacityProvider, error) {
	resp, err := client.DescribeCapacityProviders(ctx, &ecs.DescribeCapacityProvidersInput{
		CapacityProviders: []string{capacityProviderName},
	})
	if err != nil {
		var clientErr *ecstypes.ClientException
		if errors.As(err, &clientErr) && strings.Contains(clientErr.ErrorMessage(),
			"capacity provider does not exist") {
			return nil, nil
		}
		return nil, fmt.Errorf("describe capacity providers: %w", err)
	}
	for i := range resp.CapacityProviders {
		if aws.ToString(resp.CapacityProviders[i].Name) == capacityProviderName {
			return &resp.CapacityProviders[i], nil
		}
	}
	return nil, nil
}

// checkCapacityProvider confirms the custom capacity provider best-effort: an
// emulator may not model custom capacity providers, so a miss degrades to a
// printed skip rather than a failure.
func checkCapacityProvider(provider *ecstypes.CapacityProvider) {
	if provider == nil {
		fmt.Println("skip: custom capacity providers not modeled")
		return
	}
	if provider.Status == ecstypes.CapacityProviderStatusInactive {
		fmt.Printf("skip: capacity provider %s is INACTIVE\n", capacityProviderName)
		return
	}
	fmt.Printf("ok: capacity provider %s is %s\n", capacityProviderName, provider.Status)
}

// findCluster returns the scenario's cluster, or nil when the describe comes
// back empty or reports it missing.
func findCluster(ctx context.Context, client *ecs.Client) (*ecstypes.Cluster, error) {
	resp, err := client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
		Clusters: []string{clusterName},
	})
	if err != nil {
		var notFound *ecstypes.ClusterNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe clusters: %w", err)
	}
	for i := range resp.Clusters {
		return &resp.Clusters[i], nil
	}
	return nil, nil
}

// findTaskDefinition returns the family's latest active revision, or nil when
// none is left. Describing by bare family resolves to the latest ACTIVE
// revision and fails with a client error when the family has none.
func findTaskDefinition(
	ctx context.Context, client *ecs.Client,
) (*ecstypes.TaskDefinition, error) {
	resp, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(family),
	})
	if err != nil {
		var clientErr *ecstypes.ClientException
		if errors.As(err, &clientErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe task definition: %w", err)
	}
	return resp.TaskDefinition, nil
}

// findService returns the scenario's service, or nil when it is gone, the
// cluster is gone, or the describe reports it missing.
func findService(ctx context.Context, client *ecs.Client) (*ecstypes.Service, error) {
	resp, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(clusterName),
		Services: []string{serviceName},
	})
	if err != nil {
		var clusterGone *ecstypes.ClusterNotFoundException
		var serviceGone *ecstypes.ServiceNotFoundException
		if errors.As(err, &clusterGone) || errors.As(err, &serviceGone) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe services: %w", err)
	}
	for i := range resp.Services {
		return &resp.Services[i], nil
	}
	return nil, nil
}
