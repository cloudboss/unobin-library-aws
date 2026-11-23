package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
)

const (
	requesterCIDR = "10.63.0.0/16"
	accepterCIDR  = "10.64.0.0/16"
	marker        = "vpc-peering-connection-it"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := ec2.NewFromConfig(cfg)
	phase := os.Getenv("VERIFY_PHASE")
	idFile := filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "peering-id")
	switch phase {
	case "applied":
		conn, err := findConnection(ctx, client, nil)
		if err != nil {
			return err
		}
		if err := checkConnection(ctx, client, conn, "initial",
			ec2types.VpcPeeringConnectionStateReasonCodePendingAcceptance); err != nil {
			return err
		}
		return os.WriteFile(idFile, []byte(aws.ToString(conn.VpcPeeringConnectionId)), 0o600)
	case "updated", "destroyed":
		id, err := os.ReadFile(idFile)
		if err != nil {
			return err
		}
		conn, err := findConnection(ctx, client, []string{string(id)})
		if err != nil {
			return err
		}
		if phase == "updated" {
			return checkConnection(ctx, client, conn, "updated",
				ec2types.VpcPeeringConnectionStateReasonCodeActive)
		}
		if conn != nil && (conn.Status == nil ||
			conn.Status.Code != ec2types.VpcPeeringConnectionStateReasonCodeDeleted) {
			return fmt.Errorf("connection %s still exists: %+v", id, conn.Status)
		}
		return nil
	default:
		return fmt.Errorf("unknown verify phase %q", phase)
	}
}

func findConnection(
	ctx context.Context, client *ec2.Client, ids []string,
) (*ec2types.VpcPeeringConnection, error) {
	pages := ec2.NewDescribeVpcPeeringConnectionsPaginator(client,
		&ec2.DescribeVpcPeeringConnectionsInput{VpcPeeringConnectionIds: ids})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			var apiError smithy.APIError
			if len(ids) > 0 && errors.As(err, &apiError) &&
				apiError.ErrorCode() == "InvalidVpcPeeringConnectionID.NotFound" {
				return nil, nil
			}
			return nil, err
		}
		for _, conn := range page.VpcPeeringConnections {
			id := aws.ToString(conn.VpcPeeringConnectionId)
			if (len(ids) == 0 && tag(conn.Tags, "unobin") == marker) ||
				(len(ids) > 0 && id == ids[0]) {
				return &conn, nil
			}
		}
	}
	return nil, nil
}

func checkConnection(
	ctx context.Context,
	client *ec2.Client,
	conn *ec2types.VpcPeeringConnection,
	stage string,
	status ec2types.VpcPeeringConnectionStateReasonCode,
) error {
	if conn == nil || aws.ToString(conn.VpcPeeringConnectionId) == "" ||
		conn.Status == nil || conn.Status.Code != status || tag(conn.Tags, "stage") != stage {
		return fmt.Errorf("expected a connection in status %s with stage %s, got %+v",
			status, stage, conn)
	}
	if conn.RequesterVpcInfo == nil || conn.AccepterVpcInfo == nil {
		return fmt.Errorf("connection %s lacks its VPC descriptions",
			aws.ToString(conn.VpcPeeringConnectionId))
	}
	ids := []string{
		aws.ToString(conn.RequesterVpcInfo.VpcId),
		aws.ToString(conn.AccepterVpcInfo.VpcId),
	}
	out, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: ids})
	if err != nil {
		return err
	}
	cidrs := map[string]string{}
	for _, vpc := range out.Vpcs {
		cidrs[aws.ToString(vpc.VpcId)] = aws.ToString(vpc.CidrBlock)
	}
	if cidrs[ids[0]] != requesterCIDR || cidrs[ids[1]] != accepterCIDR {
		return fmt.Errorf("unexpected requester %s and accepter %s in %v", ids[0], ids[1], cidrs)
	}
	return nil
}

func tag(tags []ec2types.Tag, key string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}
