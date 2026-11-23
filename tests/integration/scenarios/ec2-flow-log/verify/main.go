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
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
)

const bucketName = "unobin-it-ec2-flow-log"

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
	mode := os.Getenv("VERIFY_PHASE")
	idFile := filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "flow-log-id")
	vpcFile := filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "vpc-id")
	var ids []string
	if mode != "applied" {
		id, err := os.ReadFile(idFile)
		if err != nil {
			return err
		}
		ids = []string{string(id)}
	}
	flow, err := findFlowLog(ctx, client, ids)
	if err != nil {
		return err
	}
	if mode == "destroyed" {
		if flow != nil {
			return fmt.Errorf("flow log still exists: %s", aws.ToString(flow.FlowLogId))
		}
		vpcID, err := os.ReadFile(vpcFile)
		if err != nil {
			return err
		}
		vpcs, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: []string{string(vpcID)}})
		if err != nil {
			var api smithy.APIError
			if !errors.As(err, &api) || api.ErrorCode() != "InvalidVpcID.NotFound" {
				return err
			}
		} else if len(vpcs.Vpcs) > 0 {
			return fmt.Errorf("monitored VPC still exists: %s", vpcID)
		}
		_, err = s3.NewFromConfig(cfg).HeadBucket(ctx, &s3.HeadBucketInput{
			Bucket: aws.String(bucketName),
		})
		var response interface{ HTTPStatusCode() int }
		if !errors.As(err, &response) || response.HTTPStatusCode() != 404 {
			return fmt.Errorf("expected removed bucket, got %v", err)
		}
		return nil
	}
	if mode != "applied" && mode != "updated" {
		return fmt.Errorf("unknown verification mode %q", mode)
	}
	stage := "initial"
	if mode == "updated" {
		stage = "updated"
	}
	if err := checkFlowLog(ctx, client, flow, stage); err != nil {
		return err
	}
	if err := os.WriteFile(vpcFile, []byte(aws.ToString(flow.ResourceId)), 0o600); err != nil {
		return err
	}
	return os.WriteFile(idFile, []byte(aws.ToString(flow.FlowLogId)), 0o600)
}

func findFlowLog(
	ctx context.Context, client *ec2.Client, ids []string,
) (*ec2types.FlowLog, error) {
	input := &ec2.DescribeFlowLogsInput{FlowLogIds: ids}
	if len(ids) == 0 {
		input.Filter = []ec2types.Filter{{Name: aws.String("tag:unobin"),
			Values: []string{"ec2-flow-log-it"}}}
	}
	pages := ec2.NewDescribeFlowLogsPaginator(client, input)
	var found *ec2types.FlowLog
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			var api smithy.APIError
			if len(ids) > 0 && errors.As(err, &api) && api.ErrorCode() == "InvalidFlowLogId.NotFound" {
				return nil, nil
			}
			return nil, err
		}
		for _, flow := range page.FlowLogs {
			if found != nil {
				return nil, fmt.Errorf("multiple matching flow logs")
			}
			found = &flow
		}
	}
	return found, nil
}

func checkFlowLog(
	ctx context.Context, client *ec2.Client, flow *ec2types.FlowLog, stage string,
) error {
	if flow == nil || aws.ToString(flow.FlowLogId) == "" {
		return fmt.Errorf("flow log not found")
	}
	tags := map[string]string{}
	for _, tag := range flow.Tags {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	if tags["unobin"] != "ec2-flow-log-it" || tags["stage"] != stage ||
		flow.LogDestinationType != "s3" ||
		aws.ToString(flow.LogDestination) != "arn:aws:s3:::"+bucketName ||
		flow.TrafficType != "ALL" || aws.ToInt32(flow.MaxAggregationInterval) != 60 {
		return fmt.Errorf("unexpected flow log values: %+v", flow)
	}
	if flow.DestinationOptions != nil && (flow.DestinationOptions.FileFormat != "plain-text" ||
		aws.ToBool(flow.DestinationOptions.HiveCompatiblePartitions) ||
		aws.ToBool(flow.DestinationOptions.PerHourPartition)) {
		return fmt.Errorf("unexpected flow log values: %+v", flow)
	}
	vpcs, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: []string{
		aws.ToString(flow.ResourceId)}})
	if err != nil {
		return err
	}
	if len(vpcs.Vpcs) != 1 || aws.ToString(vpcs.Vpcs[0].CidrBlock) != "10.67.0.0/16" {
		return fmt.Errorf("unexpected monitored VPC: %+v", vpcs.Vpcs)
	}
	return nil
}
