package ec2

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlowLogRequestSelectors(t *testing.T) {
	for _, tc := range []struct {
		kind  ec2types.FlowLogsResourceType
		input FlowLogResource
	}{
		{ec2types.FlowLogsResourceTypeVpc, FlowLogResource{VpcId: aws.String("selected")}},
		{ec2types.FlowLogsResourceTypeSubnet, FlowLogResource{SubnetId: aws.String("selected")}},
		{ec2types.FlowLogsResourceTypeNetworkInterface, FlowLogResource{EniId: aws.String("selected")}},
		{ec2types.FlowLogsResourceTypeTransitGateway,
			FlowLogResource{TransitGatewayId: aws.String("selected")}},
		{ec2types.FlowLogsResourceTypeTransitGatewayAttachment,
			FlowLogResource{TransitGatewayAttachmentId: aws.String("selected")}},
		{ec2types.FlowLogsResourceTypeRegionalNatGateway,
			FlowLogResource{RegionalNatGatewayId: aws.String("selected")}},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			for _, traffic := range []string{"", "ALL"} {
				tc.input.TrafficType = aws.String(traffic)
				got, err := tc.input.prepareRequest("token")
				require.NoError(t, err)
				expectedTraffic := ec2types.TrafficType(traffic)
				if tc.kind == ec2types.FlowLogsResourceTypeTransitGateway ||
					tc.kind == ec2types.FlowLogsResourceTypeTransitGatewayAttachment {
					expectedTraffic = ""
				}
				assert.Equal(t, &ec2.CreateFlowLogsInput{
					ClientToken: aws.String("token"), ResourceIds: []string{"selected"},
					ResourceType: tc.kind, LogDestinationType: ec2types.LogDestinationTypeCloudWatchLogs,
					MaxAggregationInterval: aws.Int32(600), TrafficType: expectedTraffic,
				}, got)
			}
		})
	}
}

func TestFlowLogRequestDefaultsAndOptions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   FlowLogResource
		options *ec2types.DestinationOptionsRequest
	}{
		{"absent", FlowLogResource{}, nil},
		{"present", FlowLogResource{DestinationOptions: &FlowLogDestinationOptions{}},
			&ec2types.DestinationOptionsRequest{FileFormat: "plain-text",
				HiveCompatiblePartitions: aws.Bool(false), PerHourPartition: aws.Bool(false)}},
		{"explicit false", FlowLogResource{DestinationOptions: &FlowLogDestinationOptions{
			FileFormat: aws.String("parquet"), HiveCompatiblePartitions: aws.Bool(false),
			PerHourPartition: aws.Bool(false)}},
			&ec2types.DestinationOptionsRequest{FileFormat: "parquet",
				HiveCompatiblePartitions: aws.Bool(false), PerHourPartition: aws.Bool(false)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.input.VpcId = aws.String("vpc-1")
			got, err := tc.input.prepareRequest("token")
			require.NoError(t, err)
			assert.Equal(t, &ec2.CreateFlowLogsInput{
				ClientToken: aws.String("token"), ResourceIds: []string{"vpc-1"}, ResourceType: "VPC",
				LogDestinationType: "cloud-watch-logs", MaxAggregationInterval: aws.Int32(600),
				DestinationOptions: tc.options,
			}, got)
		})
	}
	input := FlowLogResource{
		VpcId: aws.String("vpc-1"), LogDestination: aws.String("arn:aws:s3:::bucket:*:*"),
		IamRoleArn: aws.String(""), DeliverCrossAccountRole: aws.String(""),
		LogFormat: aws.String(""), TrafficType: aws.String(""),
	}
	got, err := input.prepareRequest("token")
	require.NoError(t, err)
	assert.Equal(t, &ec2.CreateFlowLogsInput{
		ClientToken: aws.String("token"), ResourceIds: []string{"vpc-1"}, ResourceType: "VPC",
		LogDestinationType: "cloud-watch-logs", MaxAggregationInterval: aws.Int32(600),
		LogDestination: aws.String("arn:aws:s3:::bucket:*"),
	}, got)
	input.LogDestination = aws.String("")
	got, err = input.prepareRequest("token")
	require.NoError(t, err)
	assert.Nil(t, got.LogDestination)
}

func TestFlowLogARNConversion(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"", true}, {"arn:aws:s3:::bucket", true}, {"arn:aws-us-gov:iam::aws:role/x", true},
		{"arn:aws:iam::aws-managed:role/x", true}, {"arn:aws:iam::third-party:role/x", true},
		{"arn:aws:iam::aws-marketplace:role/x", true},
		{"arn:aws:iam::partner-managed:role/x", true},
		{"arn:aws:logs:us-east-12:123456789012:log-group:x:*", true},
		{"arn:aws:logs:us-east-1:cw!@#$%^&*():group", true},
		{"arn:aws:logs:abcd-word-extra-99::group", true},
		{"arn:aws::us-east-1::resource", true},
		{"invalid", false}, {"arn:other:s3:::bucket", false},
		{"arn:AWS:s3:::bucket", false}, {"arn:aws-123:s3:::bucket", false},
		{"arn:aws:s3:US-east-1::bucket", false}, {"arn:aws:s3:us-east-123::bucket", false},
		{"arn:aws:s3:us-1::bucket", false}, {"arn:aws:s3::123:bucket", false},
		{"arn:aws:s3::account-alias:bucket", false}, {"arn:aws:s3::cw123456789:bucket", false},
		{"arn:aws:s3::cw12345\n7890:bucket", false}, {"arn:aws:s3:::*", true},
		{"arn:aws:s3::::*", true}, {"arn:aws:s3:::", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			for _, field := range []string{"iam", "cross", "destination"} {
				input := FlowLogResource{VpcId: aws.String("vpc-1")}
				switch field {
				case "iam":
					input.IamRoleArn = aws.String(tc.value)
				case "cross":
					input.DeliverCrossAccountRole = aws.String(tc.value)
				default:
					input.LogDestination = aws.String(tc.value)
				}
				_, err := input.prepareRequest("token")
				if tc.valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
		})
	}
}

func TestFlowLogCompleteCreateRequest(t *testing.T) {
	input := FlowLogResource{
		SubnetId:                aws.String("subnet-one"),
		DeliverCrossAccountRole: aws.String("arn:aws:iam::partner-managed:role/cross"),
		IamRoleArn:              aws.String("arn:aws:iam::123456789012:role/publish"),
		LogDestination:          aws.String("arn:aws:s3:::logs:*"), LogDestinationType: aws.String("s3"),
		LogFormat: aws.String("${version}"), MaxAggregationInterval: aws.Int64(60),
		TrafficType: aws.String("REJECT"), DestinationOptions: &FlowLogDestinationOptions{
			FileFormat: aws.String("parquet"), HiveCompatiblePartitions: aws.Bool(true),
			PerHourPartition: aws.Bool(true)},
		Tags: &map[string]string{"blank": "", "aws:reserved": "ignored"},
	}
	actual, err := input.prepareRequest("token")
	require.NoError(t, err)
	assert.Equal(t, &ec2.CreateFlowLogsInput{
		ClientToken: aws.String("token"), ResourceIds: []string{"subnet-one"}, ResourceType: "Subnet",
		DeliverCrossAccountRole:  aws.String("arn:aws:iam::partner-managed:role/cross"),
		DeliverLogsPermissionArn: aws.String("arn:aws:iam::123456789012:role/publish"),
		LogDestination:           aws.String("arn:aws:s3:::logs"), LogDestinationType: "s3",
		LogFormat: aws.String("${version}"), MaxAggregationInterval: aws.Int32(60), TrafficType: "REJECT",
		DestinationOptions: &ec2types.DestinationOptionsRequest{FileFormat: "parquet",
			HiveCompatiblePartitions: aws.Bool(true), PerHourPartition: aws.Bool(true)},
		TagSpecifications: []ec2types.TagSpecification{{ResourceType: "vpc-flow-log",
			Tags: []ec2types.Tag{{Key: aws.String("blank"), Value: aws.String("")}}}},
	}, actual)
	input.Tags = &map[string]string{}
	actual, err = input.prepareRequest("token")
	require.NoError(t, err)
	assert.Nil(t, actual.TagSpecifications)
}
