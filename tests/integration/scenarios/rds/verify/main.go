// verify checks the RDS group the scenario applied against the phase named in
// the VERIFY_PHASE environment variable. It looks resources up by their stable
// names because the driver passes no plan outputs into verify, and it reads
// only cloud state: applied requires the subnet group to span two subnets, both
// parameter groups to hold their declared parameters, the Aurora cluster to be
// available with its one member, and the standalone instance to be available in
// the subnet group; destroyed requires all six to be gone. Tearing the group
// down is the destroy plan's job, not the verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

const (
	subnetGroupName       = "unobin-it-rds"
	parameterGroupName    = "unobin-it-rds-pg"
	clusterParamGroupName = "unobin-it-rds-cpg"
	clusterIdentifier     = "unobin-it-aurora"
	memberIdentifier      = "unobin-it-aurora-one"
	instanceIdentifier    = "unobin-it-db"
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
	client := rds.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *rds.Client) error {
	if err := checkSubnetGroup(ctx, client); err != nil {
		return err
	}
	if err := checkParameters(ctx, client); err != nil {
		return err
	}
	if err := checkClusterParameters(ctx, client); err != nil {
		return err
	}
	if err := checkCluster(ctx, client); err != nil {
		return err
	}
	if err := checkInstances(ctx, client); err != nil {
		return err
	}
	fmt.Printf("ok: subnet group, parameter groups, cluster %s with member %s, "+
		"and instance %s are in place\n", clusterIdentifier, memberIdentifier,
		instanceIdentifier)
	return nil
}

func verifyDestroyed(ctx context.Context, client *rds.Client) error {
	if sg, err := findSubnetGroup(ctx, client); err != nil {
		return err
	} else if sg != nil {
		return fmt.Errorf("subnet group %s still exists", subnetGroupName)
	}
	if pg, err := findParameterGroup(ctx, client); err != nil {
		return err
	} else if pg != nil {
		return fmt.Errorf("parameter group %s still exists", parameterGroupName)
	}
	if cpg, err := findClusterParameterGroup(ctx, client); err != nil {
		return err
	} else if cpg != nil {
		return fmt.Errorf("cluster parameter group %s still exists", clusterParamGroupName)
	}
	if cluster, err := findCluster(ctx, client); err != nil {
		return err
	} else if cluster != nil {
		return fmt.Errorf("cluster %s still exists", clusterIdentifier)
	}
	for _, id := range []string{memberIdentifier, instanceIdentifier} {
		if inst, err := findInstance(ctx, client, id); err != nil {
			return err
		} else if inst != nil {
			return fmt.Errorf("db instance %s still exists", id)
		}
	}
	fmt.Println("ok: subnet group, parameter groups, cluster, and instances are gone")
	return nil
}

func checkSubnetGroup(ctx context.Context, client *rds.Client) error {
	sg, err := findSubnetGroup(ctx, client)
	if err != nil {
		return err
	}
	if sg == nil {
		return fmt.Errorf("subnet group %s not found", subnetGroupName)
	}
	if len(sg.Subnets) != 2 {
		return fmt.Errorf("subnet group %s has %d subnets, want 2",
			subnetGroupName, len(sg.Subnets))
	}
	if aws.ToString(sg.VpcId) == "" {
		return fmt.Errorf("subnet group %s reports no VPC", subnetGroupName)
	}
	return nil
}

// checkParameters confirms the DB parameter group exists and holds the two
// parameters the scenario declared, proving the chunked modify ran.
func checkParameters(ctx context.Context, client *rds.Client) error {
	pg, err := findParameterGroup(ctx, client)
	if err != nil {
		return err
	}
	if pg == nil {
		return fmt.Errorf("parameter group %s not found", parameterGroupName)
	}
	want := map[string]string{"log_connections": "1", "log_disconnections": "1"}
	pager := rds.NewDescribeDBParametersPaginator(client, &rds.DescribeDBParametersInput{
		DBParameterGroupName: aws.String(parameterGroupName),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("describe db parameters %s: %w", parameterGroupName, err)
		}
		for _, p := range page.Parameters {
			name := aws.ToString(p.ParameterName)
			if v, ok := want[name]; ok && aws.ToString(p.ParameterValue) == v {
				delete(want, name)
			}
		}
	}
	if len(want) != 0 {
		return fmt.Errorf("parameter group %s is missing declared parameters %v",
			parameterGroupName, want)
	}
	return nil
}

// checkClusterParameters confirms the cluster parameter group exists and holds
// the parameter the scenario declared.
func checkClusterParameters(ctx context.Context, client *rds.Client) error {
	cpg, err := findClusterParameterGroup(ctx, client)
	if err != nil {
		return err
	}
	if cpg == nil {
		return fmt.Errorf("cluster parameter group %s not found", clusterParamGroupName)
	}
	found := false
	pager := rds.NewDescribeDBClusterParametersPaginator(client,
		&rds.DescribeDBClusterParametersInput{
			DBClusterParameterGroupName: aws.String(clusterParamGroupName),
		})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("describe db cluster parameters %s: %w",
				clusterParamGroupName, err)
		}
		for _, p := range page.Parameters {
			if aws.ToString(p.ParameterName) == "log_connections" &&
				aws.ToString(p.ParameterValue) == "1" {
				found = true
			}
		}
	}
	if !found {
		return fmt.Errorf("cluster parameter group %s is missing log_connections=1",
			clusterParamGroupName)
	}
	return nil
}

func checkCluster(ctx context.Context, client *rds.Client) error {
	cluster, err := findCluster(ctx, client)
	if err != nil {
		return err
	}
	if cluster == nil {
		return fmt.Errorf("cluster %s not found", clusterIdentifier)
	}
	if aws.ToString(cluster.Status) != "available" {
		return fmt.Errorf("cluster %s status is %s, want available",
			clusterIdentifier, aws.ToString(cluster.Status))
	}
	if aws.ToString(cluster.Engine) != "aurora-postgresql" {
		return fmt.Errorf("cluster %s engine is %s, want aurora-postgresql",
			clusterIdentifier, aws.ToString(cluster.Engine))
	}
	for _, m := range cluster.DBClusterMembers {
		if aws.ToString(m.DBInstanceIdentifier) == memberIdentifier {
			return nil
		}
	}
	return fmt.Errorf("cluster %s has no member %s", clusterIdentifier, memberIdentifier)
}

func checkInstances(ctx context.Context, client *rds.Client) error {
	member, err := findInstance(ctx, client, memberIdentifier)
	if err != nil {
		return err
	}
	if member == nil {
		return fmt.Errorf("db instance %s not found", memberIdentifier)
	}
	if aws.ToString(member.DBClusterIdentifier) != clusterIdentifier {
		return fmt.Errorf("db instance %s belongs to cluster %q, want %s",
			memberIdentifier, aws.ToString(member.DBClusterIdentifier), clusterIdentifier)
	}
	inst, err := findInstance(ctx, client, instanceIdentifier)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("db instance %s not found", instanceIdentifier)
	}
	if aws.ToString(inst.DBInstanceStatus) != "available" {
		return fmt.Errorf("db instance %s status is %s, want available",
			instanceIdentifier, aws.ToString(inst.DBInstanceStatus))
	}
	if inst.DBSubnetGroup == nil ||
		aws.ToString(inst.DBSubnetGroup.DBSubnetGroupName) != subnetGroupName {
		return fmt.Errorf("db instance %s is not in subnet group %s",
			instanceIdentifier, subnetGroupName)
	}
	return nil
}

func findSubnetGroup(ctx context.Context, client *rds.Client) (*rdstypes.DBSubnetGroup, error) {
	resp, err := client.DescribeDBSubnetGroups(ctx, &rds.DescribeDBSubnetGroupsInput{
		DBSubnetGroupName: aws.String(subnetGroupName),
	})
	if err != nil {
		var notFound *rdstypes.DBSubnetGroupNotFoundFault
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe db subnet groups: %w", err)
	}
	if len(resp.DBSubnetGroups) == 0 {
		return nil, nil
	}
	return &resp.DBSubnetGroups[0], nil
}

func findParameterGroup(
	ctx context.Context, client *rds.Client,
) (*rdstypes.DBParameterGroup, error) {
	resp, err := client.DescribeDBParameterGroups(ctx, &rds.DescribeDBParameterGroupsInput{
		DBParameterGroupName: aws.String(parameterGroupName),
	})
	if err != nil {
		var notFound *rdstypes.DBParameterGroupNotFoundFault
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe db parameter groups: %w", err)
	}
	if len(resp.DBParameterGroups) == 0 {
		return nil, nil
	}
	return &resp.DBParameterGroups[0], nil
}

func findClusterParameterGroup(
	ctx context.Context, client *rds.Client,
) (*rdstypes.DBClusterParameterGroup, error) {
	resp, err := client.DescribeDBClusterParameterGroups(ctx,
		&rds.DescribeDBClusterParameterGroupsInput{
			DBClusterParameterGroupName: aws.String(clusterParamGroupName),
		})
	if err != nil {
		var notFound *rdstypes.DBParameterGroupNotFoundFault
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe db cluster parameter groups: %w", err)
	}
	if len(resp.DBClusterParameterGroups) == 0 {
		return nil, nil
	}
	return &resp.DBClusterParameterGroups[0], nil
}

func findCluster(ctx context.Context, client *rds.Client) (*rdstypes.DBCluster, error) {
	resp, err := client.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
		DBClusterIdentifier: aws.String(clusterIdentifier),
	})
	if err != nil {
		var notFound *rdstypes.DBClusterNotFoundFault
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe db clusters: %w", err)
	}
	if len(resp.DBClusters) == 0 {
		return nil, nil
	}
	return &resp.DBClusters[0], nil
}

func findInstance(
	ctx context.Context, client *rds.Client, id string,
) (*rdstypes.DBInstance, error) {
	resp, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(id),
	})
	if err != nil {
		var notFound *rdstypes.DBInstanceNotFoundFault
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe db instances %s: %w", id, err)
	}
	if len(resp.DBInstances) == 0 {
		return nil, nil
	}
	return &resp.DBInstances[0], nil
}
