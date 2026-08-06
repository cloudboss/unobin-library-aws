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
	"github.com/aws/aws-sdk-go-v2/service/dsql"
	dsqltypes "github.com/aws/aws-sdk-go-v2/service/dsql/types"
)

const (
	markerKey     = "unobin"
	markerValue   = "dsql-it"
	phaseKey      = "phase"
	primaryRegion = "us-east-1"
	peerRegion    = "us-west-2"
	witnessRegion = "us-east-2"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	primaryClient, err := dsqlClient(ctx, primaryRegion)
	if err != nil {
		return err
	}
	peerClient, err := dsqlClient(ctx, peerRegion)
	if err != nil {
		return err
	}

	switch phase {
	case "applied":
		return verifyPresent(ctx, primaryClient, peerClient, true, "applied")
	case "updated":
		return verifyPresent(ctx, primaryClient, peerClient, false, "updated")
	case "destroyed":
		return verifyDestroyed(ctx, primaryClient, peerClient)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied, updated, or destroyed, got %q", phase)
	}
}

func dsqlClient(ctx context.Context, region string) (*dsql.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config for %s: %w", region, err)
	}
	return dsql.NewFromConfig(cfg), nil
}

func verifyPresent(
	ctx context.Context,
	primaryClient *dsql.Client,
	peerClient *dsql.Client,
	wantDeletionProtection bool,
	wantPhase string,
) error {
	primary, primaryTags, err := findMarkedCluster(ctx, primaryClient)
	if err != nil {
		return err
	}
	if primary == nil {
		return fmt.Errorf("no DSQL cluster tagged %s=%s in %s",
			markerKey, markerValue, primaryRegion)
	}
	peer, peerTags, err := findMarkedCluster(ctx, peerClient)
	if err != nil {
		return err
	}
	if peer == nil {
		return fmt.Errorf("no DSQL cluster tagged %s=%s in %s",
			markerKey, markerValue, peerRegion)
	}

	if err := verifyClusterState(
		ctx,
		primaryClient,
		primary,
		primaryTags,
		wantDeletionProtection,
		wantPhase,
	); err != nil {
		return err
	}
	if err := verifyClusterState(
		ctx,
		peerClient,
		peer,
		peerTags,
		wantDeletionProtection,
		wantPhase,
	); err != nil {
		return err
	}

	if err := verifyClusterPeering(primary, peer); err != nil {
		return err
	}
	if err := verifyClusterPeering(peer, primary); err != nil {
		return err
	}

	fmt.Printf("ok: DSQL peering present with phase %s\n", wantPhase)
	return nil
}

func verifyClusterState(
	ctx context.Context,
	client *dsql.Client,
	cluster *dsql.GetClusterOutput,
	tags map[string]string,
	wantDeletionProtection bool,
	wantPhase string,
) error {
	id := aws.ToString(cluster.Identifier)
	if aws.ToBool(cluster.DeletionProtectionEnabled) != wantDeletionProtection {
		return fmt.Errorf("cluster %s deletion protection is %t, want %t",
			id, aws.ToBool(cluster.DeletionProtectionEnabled), wantDeletionProtection)
	}
	if tags[phaseKey] != wantPhase {
		return fmt.Errorf("cluster %s tag %s is %q, want %q",
			id, phaseKey, tags[phaseKey], wantPhase)
	}
	if err := verifyAWSOwnedEncryption(cluster); err != nil {
		return err
	}
	service, err := client.GetVpcEndpointServiceName(ctx, &dsql.GetVpcEndpointServiceNameInput{
		Identifier: cluster.Identifier,
	})
	if err != nil {
		return fmt.Errorf("get vpc endpoint service name %s: %w", id, err)
	}
	if aws.ToString(service.ServiceName) == "" {
		return fmt.Errorf("cluster %s has no VPC endpoint service name", id)
	}
	return nil
}

func verifyClusterPeering(cluster, peer *dsql.GetClusterOutput) error {
	id := aws.ToString(cluster.Identifier)
	props := cluster.MultiRegionProperties
	if props == nil {
		return fmt.Errorf("cluster %s has no multi-region properties", id)
	}
	if aws.ToString(props.WitnessRegion) != witnessRegion {
		return fmt.Errorf("cluster %s witness region is %q, want %q",
			id, aws.ToString(props.WitnessRegion), witnessRegion)
	}
	peerARN := aws.ToString(peer.Arn)
	if !slices.ContainsFunc(props.Clusters, func(clusterARN string) bool {
		return strings.EqualFold(clusterARN, peerARN)
	}) {
		return fmt.Errorf("cluster %s peering clusters are %v, want %s",
			id, props.Clusters, peerARN)
	}
	return nil
}

func verifyAWSOwnedEncryption(cluster *dsql.GetClusterOutput) error {
	if cluster.EncryptionDetails == nil {
		return fmt.Errorf("cluster %s has no encryption details",
			aws.ToString(cluster.Identifier))
	}
	details := cluster.EncryptionDetails
	if details.EncryptionType != dsqltypes.EncryptionTypeAwsOwnedKmsKey {
		return fmt.Errorf("cluster %s encryption type is %s, want %s",
			aws.ToString(cluster.Identifier),
			details.EncryptionType,
			dsqltypes.EncryptionTypeAwsOwnedKmsKey)
	}
	if details.EncryptionStatus != dsqltypes.EncryptionStatusEnabled {
		return fmt.Errorf("cluster %s encryption status is %s, want %s",
			aws.ToString(cluster.Identifier),
			details.EncryptionStatus,
			dsqltypes.EncryptionStatusEnabled)
	}
	return nil
}

func verifyDestroyed(ctx context.Context, primaryClient, peerClient *dsql.Client) error {
	cluster, _, err := findMarkedCluster(ctx, primaryClient)
	if err != nil {
		return err
	}
	if cluster != nil {
		return fmt.Errorf("cluster %s still exists", aws.ToString(cluster.Identifier))
	}
	cluster, _, err = findMarkedCluster(ctx, peerClient)
	if err != nil {
		return err
	}
	if cluster != nil {
		return fmt.Errorf("cluster %s still exists", aws.ToString(cluster.Identifier))
	}
	fmt.Printf("ok: no DSQL clusters tagged %s=%s\n", markerKey, markerValue)
	return nil
}

func findMarkedCluster(
	ctx context.Context,
	client *dsql.Client,
) (*dsql.GetClusterOutput, map[string]string, error) {
	pager := dsql.NewListClustersPaginator(client, &dsql.ListClustersInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("list clusters: %w", err)
		}
		for _, summary := range page.Clusters {
			cluster, tags, err := readClusterTags(ctx, client, summary)
			if err != nil {
				return nil, nil, err
			}
			if tags[markerKey] == markerValue {
				return cluster, tags, nil
			}
		}
	}
	return nil, nil, nil
}

func readClusterTags(
	ctx context.Context,
	client *dsql.Client,
	summary dsqltypes.ClusterSummary,
) (*dsql.GetClusterOutput, map[string]string, error) {
	id := aws.ToString(summary.Identifier)
	cluster, err := client.GetCluster(ctx, &dsql.GetClusterInput{
		Identifier: summary.Identifier,
	})
	if err != nil {
		if isNotFound(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("get cluster %s: %w", id, err)
	}
	tags, err := client.ListTagsForResource(ctx, &dsql.ListTagsForResourceInput{
		ResourceArn: cluster.Arn,
	})
	if err != nil {
		if isNotFound(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("list tags for cluster %s: %w", id, err)
	}
	return cluster, tags.Tags, nil
}

func isNotFound(err error) bool {
	var notFound *dsqltypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}
