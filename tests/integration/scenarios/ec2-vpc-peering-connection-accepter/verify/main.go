package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"

	fixture "github.com/cloudboss/unobin-library-aws/tests/integration/peeringfixture"
)

//go:embed testdata/ub/config-relinquish.ub
var relinquishConfig []byte

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	client, err := fixture.Client(ctx)
	if err != nil {
		return err
	}
	phase := os.Getenv("VERIFY_PHASE")
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	idFile := filepath.Join(buildDir, "adopted-id")
	if phase == "destroyed" {
		saved, err := os.ReadFile(idFile)
		if err != nil {
			return err
		}
		page, err := client.DescribeVpcPeeringConnections(ctx, &ec2.DescribeVpcPeeringConnectionsInput{
			VpcPeeringConnectionIds: []string{string(saved)},
		})
		var apiErr smithy.APIError
		if err != nil && (!errors.As(err, &apiErr) ||
			apiErr.ErrorCode() != "InvalidVpcPeeringConnectionID.NotFound") {
			return err
		}
		if page == nil {
			page = &ec2.DescribeVpcPeeringConnectionsOutput{}
		}
		for _, conn := range page.VpcPeeringConnections {
			if conn.Status == nil || conn.Status.Code != types.VpcPeeringConnectionStateReasonCodeDeleted {
				return fmt.Errorf("fixture connection survived final destroy: %+v", conn)
			}
		}
		ids, err := os.ReadFile(filepath.Join(buildDir, "vpc-ids"))
		if err != nil {
			return err
		}
		for id := range strings.SplitSeq(string(ids), "\n") {
			page, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: []string{id}})
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidVpcID.NotFound" {
				continue
			}
			if err != nil {
				return err
			}
			if len(page.Vpcs) != 0 {
				return fmt.Errorf("fixture VPC survived final destroy: %+v", page.Vpcs)
			}
		}
		return nil
	}
	conn, err := fixture.Find(ctx, client)
	if err != nil {
		return err
	}
	id := aws.ToString(conn.VpcPeeringConnectionId)
	stage, status := "initial", types.VpcPeeringConnectionStateReasonCodePendingAcceptance
	switch phase {
	case "applied":
		if err := os.WriteFile(idFile, []byte(id), 0o600); err != nil {
			return err
		}
	case "updated":
		saved, err := os.ReadFile(idFile)
		if err != nil {
			return err
		}
		if string(saved) != id {
			return fmt.Errorf("adopted identity changed: %s to %s", saved, id)
		}
		stage, status = "updated", types.VpcPeeringConnectionStateReasonCodeActive
	default:
		return fmt.Errorf("unknown verify phase %q", phase)
	}
	if conn.Status == nil || conn.Status.Code != status ||
		fixture.Tag(conn.Tags, "stage") != stage || fixture.Tag(conn.Tags, "seed") != "preserve" {
		return fmt.Errorf("unexpected %s connection state: %+v", phase, conn)
	}
	if conn.RequesterVpcInfo == nil || conn.AccepterVpcInfo == nil {
		return fmt.Errorf("connection %s has incomplete VPC descriptions", id)
	}
	if phase != "applied" {
		for _, side := range []*types.VpcPeeringConnectionVpcInfo{
			conn.RequesterVpcInfo, conn.AccepterVpcInfo,
		} {
			if side.PeeringOptions == nil ||
				!aws.ToBool(side.PeeringOptions.AllowDnsResolutionFromRemoteVpc) {
				return fmt.Errorf("connection %s lost its DNS settings on %s", id, aws.ToString(side.VpcId))
			}
		}
	}
	out, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: []string{
		aws.ToString(conn.RequesterVpcInfo.VpcId), aws.ToString(conn.AccepterVpcInfo.VpcId),
	}})
	if err != nil {
		return err
	}
	cidrs := map[string]string{}
	for _, vpc := range out.Vpcs {
		cidrs[aws.ToString(vpc.VpcId)] = aws.ToString(vpc.CidrBlock)
	}
	if cidrs[aws.ToString(conn.RequesterVpcInfo.VpcId)] != "10.65.0.0/16" ||
		cidrs[aws.ToString(conn.AccepterVpcInfo.VpcId)] != "10.66.0.0/16" {
		return fmt.Errorf("connection %s changed its VPCs: %v", id, cidrs)
	}
	if phase == "applied" {
		ids := aws.ToString(conn.RequesterVpcInfo.VpcId) + "\n" + aws.ToString(conn.AccepterVpcInfo.VpcId)
		if err := os.WriteFile(filepath.Join(buildDir, "vpc-ids"), []byte(ids), 0o600); err != nil {
			return err
		}
		for vpcID := range cidrs {
			_, err := client.ModifyVpcAttribute(ctx, &ec2.ModifyVpcAttributeInput{
				VpcId:              aws.String(vpcID),
				EnableDnsHostnames: &types.AttributeBooleanValue{Value: aws.Bool(true)},
			})
			if err != nil {
				return err
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Join(buildDir, "relinquish"), 0o700); err != nil {
		return err
	}
	path := filepath.Join(buildDir, "relinquish", "config.ub")
	if err := os.WriteFile(path, relinquishConfig, 0o600); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"pin", "-c", "relinquish/config.ub"},
		{"plan", "-c", "relinquish/config.ub", "-o", "plan-relinquish.json"},
		{"apply", "plan-relinquish.json"},
	} {
		cmd := exec.CommandContext(ctx, "./ec2-vpc-peering-connection-accepter", args...)
		cmd.Dir, cmd.Stdout, cmd.Stderr = buildDir, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("relinquish accepter management (%s): %w", args[0], err)
		}
	}
	surviving, err := fixture.Find(ctx, client)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(conn, surviving) {
		return errors.New("connection changed after relinquishing accepter management")
	}
	return nil
}
