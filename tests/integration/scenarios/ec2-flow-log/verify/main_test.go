package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckFlowLogDestinationOptions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options *ec2types.DestinationOptionsResponse
		invalid bool
	}{
		{name: "omitted"},
		{name: "returned defaults", options: &ec2types.DestinationOptionsResponse{
			FileFormat: "plain-text", HiveCompatiblePartitions: aws.Bool(false),
			PerHourPartition: aws.Bool(false),
		}},
		{name: "wrong format", options: &ec2types.DestinationOptionsResponse{
			FileFormat: "parquet",
		}, invalid: true},
		{name: "hive partitions", options: &ec2types.DestinationOptionsResponse{
			FileFormat: "plain-text", HiveCompatiblePartitions: aws.Bool(true),
		}, invalid: true},
		{name: "hour partitions", options: &ec2types.DestinationOptionsResponse{
			FileFormat: "plain-text", PerHourPartition: aws.Bool(true),
		}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, r.ParseForm())
				requests = append(requests, r.PostForm.Encode())
				w.Header().Set("Content-Type", "text/xml")
				fmt.Fprint(w, `<DescribeVpcsResponse><vpcSet><item><vpcId>vpc-fixture</vpcId>`+
					`<cidrBlock>10.67.0.0/16</cidrBlock></item></vpcSet></DescribeVpcsResponse>`)
			}))
			t.Cleanup(server.Close)
			client := ec2.NewFromConfig(aws.Config{
				Region: "us-east-1", Credentials: aws.AnonymousCredentials{},
			}, func(options *ec2.Options) { options.BaseEndpoint = aws.String(server.URL) })
			flow := &ec2types.FlowLog{
				FlowLogId: aws.String("fl-fixture"), ResourceId: aws.String("vpc-fixture"),
				LogDestinationType: "s3", LogDestination: aws.String("arn:aws:s3:::" + bucketName),
				TrafficType: "ALL", MaxAggregationInterval: aws.Int32(60),
				DestinationOptions: tc.options,
				Tags: []ec2types.Tag{
					{Key: aws.String("unobin"), Value: aws.String("ec2-flow-log-it")},
					{Key: aws.String("stage"), Value: aws.String("initial")},
				},
			}
			err := checkFlowLog(t.Context(), client, flow, "initial")
			if tc.invalid {
				require.ErrorContains(t, err, "unexpected flow log values")
				assert.Empty(t, requests)
			} else {
				require.NoError(t, err)
				assert.Equal(t, []string{
					"Action=DescribeVpcs&Version=2016-11-15&VpcId.1=vpc-fixture",
				}, requests)
			}
		})
	}
}
