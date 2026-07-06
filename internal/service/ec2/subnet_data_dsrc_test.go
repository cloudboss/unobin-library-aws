package ec2

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const subnetDataID = "subnet-0123456789abcdef0"

func describeSubnetDataSourcePageXML(nextToken string, items ...string) string {
	next := ""
	if nextToken != "" {
		next = fmt.Sprintf("<nextToken>%s</nextToken>", nextToken)
	}
	return fmt.Sprintf(`<DescribeSubnetsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
  <requestId>req-subnets</requestId>
  <subnetSet>%s</subnetSet>
  %s
</DescribeSubnetsResponse>`, strings.Join(items, ""), next)
}

func subnetDataItemXML(id string) string {
	return fmt.Sprintf(`<item>
  <subnetId>%s</subnetId>
  <subnetArn>arn:aws:ec2:us-east-1:123456789012:subnet/%s</subnetArn>
  <state>available</state>
  <vpcId>vpc-0123456789abcdef0</vpcId>
  <cidrBlock>10.62.1.0/24</cidrBlock>
  <availableIpAddressCount>251</availableIpAddressCount>
  <availabilityZone>us-east-1a</availabilityZone>
  <availabilityZoneId>use1-az1</availabilityZoneId>
  <defaultForAz>false</defaultForAz>
  <mapPublicIpOnLaunch>true</mapPublicIpOnLaunch>
  <assignIpv6AddressOnCreation>true</assignIpv6AddressOnCreation>
  <ipv6Native>false</ipv6Native>
  <ownerId>123456789012</ownerId>
  <enableDns64>true</enableDns64>
  <enableLniAtDeviceIndex>1</enableLniAtDeviceIndex>
  <customerOwnedIpv4Pool>ipv4pool-coip-0123456789abcdef0</customerOwnedIpv4Pool>
  <mapCustomerOwnedIpOnLaunch>true</mapCustomerOwnedIpOnLaunch>
  <outpostArn>arn:aws:outposts:us-east-1:123456789012:outpost/op-0123456789abcdef0</outpostArn>
  <ipv6CidrBlockAssociationSet>
    <item>
      <associationId>subnet-cidr-assoc-ignored</associationId>
      <ipv6CidrBlock>2600:1f18:1234:5600::/64</ipv6CidrBlock>
      <ipv6CidrBlockState><state>associating</state></ipv6CidrBlockState>
    </item>
    <item>
      <associationId>subnet-cidr-assoc-associated</associationId>
      <ipv6CidrBlock>2600:1f18:abcd:ef00::/64</ipv6CidrBlock>
      <ipv6CidrBlockState><state>associated</state></ipv6CidrBlockState>
    </item>
  </ipv6CidrBlockAssociationSet>
  <privateDnsNameOptionsOnLaunch>
    <hostnameType>resource-name</hostnameType>
    <enableResourceNameDnsARecord>true</enableResourceNameDnsARecord>
    <enableResourceNameDnsAAAARecord>false</enableResourceNameDnsAAAARecord>
  </privateDnsNameOptionsOnLaunch>
  <tagSet>
    <item><key>Name</key><value>unobin-subnet-data</value></item>
    <item><key>unobin</key><value>ec2-subnet-data</value></item>
    <item><key>aws:cloudformation:stack-name</key><value>ignored</value></item>
  </tagSet>
</item>`, id, id)
}

func subnetDataItemWithoutNestedXML(id string) string {
	return fmt.Sprintf(`<item>
  <subnetId>%s</subnetId>
  <subnetArn>arn:aws:ec2:us-east-1:123456789012:subnet/%s</subnetArn>
  <state>available</state>
  <vpcId>vpc-0123456789abcdef0</vpcId>
  <cidrBlock>10.62.1.0/24</cidrBlock>
  <availabilityZone>us-east-1a</availabilityZone>
  <availabilityZoneId>use1-az1</availabilityZoneId>
  <defaultForAz>false</defaultForAz>
  <ownerId>123456789012</ownerId>
  <ipv6CidrBlockAssociationSet>
    <item>
      <associationId>subnet-cidr-assoc-ignored</associationId>
      <ipv6CidrBlock>2600:1f18:1234:5600::/64</ipv6CidrBlock>
      <ipv6CidrBlockState><state>associating</state></ipv6CidrBlockState>
    </item>
  </ipv6CidrBlockAssociationSet>
</item>`, id, id)
}

func TestSubnetDataSourceReadPaginatesSendsFiltersAndFlattens(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSubnets", func(n int, form url.Values) (int, string) {
		switch n {
		case 1:
			assert.Empty(t, form.Get("NextToken"))
			return 200, describeSubnetDataSourcePageXML("token-2")
		case 2:
			assert.Equal(t, "token-2", form.Get("NextToken"))
			return 200, describeSubnetDataSourcePageXML("", subnetDataItemXML(subnetDataID))
		default:
			t.Fatalf("unexpected DescribeSubnets call %d", n)
			return 500, ""
		}
	})
	cfg := fake.configuration()

	r := &SubnetDataSource{
		Id:                 aws.String(subnetDataID),
		AvailabilityZone:   aws.String("us-east-1a"),
		AvailabilityZoneId: aws.String("use1-az1"),
		DefaultForAz:       aws.Bool(true),
		State:              aws.String("available"),
		VpcId:              aws.String("vpc-0123456789abcdef0"),
		CidrBlock:          aws.String("10.62.1.0/24"),
		Ipv6CidrBlock:      aws.String("2600:1f18:abcd:ef00::/64"),
		Tags:               new(map[string]string{"unobin": "ec2-subnet-data"}),
		Filter: new([]SubnetDataSourceFilter{
			{Name: "owner-id", Values: []string{"123456789012"}},
			{Name: "description", Values: []string{""}},
			{Name: "empty-values", Values: []string{}},
		}),
	}
	out, err := r.Read(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, subnetDataID, out.Id)
	assert.Equal(t, "arn:aws:ec2:us-east-1:123456789012:subnet/"+subnetDataID, out.Arn)
	assert.True(t, out.AssignIpv6AddressOnCreation)
	assert.Equal(t, "us-east-1a", out.AvailabilityZone)
	assert.Equal(t, "use1-az1", out.AvailabilityZoneId)
	assert.Equal(t, int64(251), out.AvailableIpAddressCount)
	assert.Equal(t, "10.62.1.0/24", out.CidrBlock)
	assert.Equal(t, "ipv4pool-coip-0123456789abcdef0", out.CustomerOwnedIpv4Pool)
	assert.False(t, out.DefaultForAz)
	assert.True(t, out.EnableDns64)
	assert.Equal(t, int64(1), out.EnableLniAtDeviceIndex)
	require.NotNil(t, out.EnableResourceNameDnsAAAARecordOnLaunch)
	assert.False(t, *out.EnableResourceNameDnsAAAARecordOnLaunch)
	require.NotNil(t, out.EnableResourceNameDnsARecordOnLaunch)
	assert.True(t, *out.EnableResourceNameDnsARecordOnLaunch)
	require.NotNil(t, out.Ipv6CidrBlock)
	assert.Equal(t, "2600:1f18:abcd:ef00::/64", *out.Ipv6CidrBlock)
	require.NotNil(t, out.Ipv6CidrBlockAssociationId)
	assert.Equal(t, "subnet-cidr-assoc-associated", *out.Ipv6CidrBlockAssociationId)
	assert.False(t, out.Ipv6Native)
	assert.True(t, out.MapCustomerOwnedIpOnLaunch)
	assert.True(t, out.MapPublicIpOnLaunch)
	assert.Equal(t,
		"arn:aws:outposts:us-east-1:123456789012:outpost/op-0123456789abcdef0",
		out.OutpostArn)
	assert.Equal(t, "123456789012", out.OwnerId)
	require.NotNil(t, out.PrivateDnsHostnameTypeOnLaunch)
	assert.Equal(t, "resource-name", *out.PrivateDnsHostnameTypeOnLaunch)
	assert.Equal(t, "available", out.State)
	assert.Equal(t, "vpc-0123456789abcdef0", out.VpcId)
	assert.Equal(t, map[string]string{
		"Name":   "unobin-subnet-data",
		"unobin": "ec2-subnet-data",
	}, out.Tags)

	sent := fake.sent("DescribeSubnets")
	require.Len(t, sent, 2)
	assert.Equal(t, subnetDataID, sent[0].Get("SubnetId.1"))
	assertEC2Filter(t, sent[0], "availabilityZone", []string{"us-east-1a"})
	assertEC2Filter(t, sent[0], "availabilityZoneId", []string{"use1-az1"})
	assertEC2Filter(t, sent[0], "defaultForAz", []string{"true"})
	assertEC2Filter(t, sent[0], "state", []string{"available"})
	assertEC2Filter(t, sent[0], "vpc-id", []string{"vpc-0123456789abcdef0"})
	assertEC2Filter(t, sent[0], "cidrBlock", []string{"10.62.1.0/24"})
	assertEC2Filter(t, sent[0], "ipv6-cidr-block-association.ipv6-cidr-block",
		[]string{"2600:1f18:abcd:ef00::/64"})
	assertEC2Filter(t, sent[0], "tag:unobin", []string{"ec2-subnet-data"})
	assertEC2Filter(t, sent[0], "owner-id", []string{"123456789012"})
	assertEC2Filter(t, sent[0], "description", []string{""})
	assertEC2Filter(t, sent[0], "empty-values", []string{})
}

func TestSubnetDataSourceReadWithNoFiltersQueriesAllAndRequiresOne(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSubnets", func(_ int, form url.Values) (int, string) {
		for key := range form {
			assert.False(t, strings.HasPrefix(key, "Filter."), "unexpected filter %s", key)
			assert.False(t, strings.HasPrefix(key, "SubnetId."), "unexpected subnet id %s", key)
		}
		return 200, describeSubnetDataSourcePageXML("")
	})
	cfg := fake.configuration()

	out, err := (&SubnetDataSource{}).Read(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
	assert.EqualError(t, err, "no matching EC2 Subnet found")
}

func TestSubnetDataSourceReadTreatsSubnetIdNotFoundAsLookupError(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSubnets", func(int, url.Values) (int, string) {
		return 400, ec2ErrorXML("InvalidSubnetID.NotFound",
			"The subnet ID 'subnet-missing' does not exist")
	})
	cfg := fake.configuration()

	r := &SubnetDataSource{Id: aws.String("subnet-missing")}
	out, err := r.Read(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
	assert.EqualError(t, err, "no matching EC2 Subnet found")
}

func TestSubnetDataSourceReadErrorsOnMultipleMatches(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSubnets", func(int, url.Values) (int, string) {
		return 200, describeSubnetDataSourcePageXML("",
			subnetDataItemWithoutNestedXML(subnetDataID),
			subnetDataItemWithoutNestedXML("subnet-11111111111111111"))
	})
	cfg := fake.configuration()

	out, err := (&SubnetDataSource{CidrBlock: aws.String("10.62.1.0/24")}).Read(
		context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.EqualError(t, err, "multiple EC2 Subnets matched; use additional constraints")
}

func TestSubnetDataSourceReadOmitsFalseDefaultForAzAndNilNestedOutputs(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSubnets", func(_ int, form url.Values) (int, string) {
		for key := range form {
			assert.False(t, strings.HasPrefix(key, "Filter."), "unexpected filter %s", key)
		}
		return 200, describeSubnetDataSourcePageXML("",
			subnetDataItemWithoutNestedXML(subnetDataID))
	})
	cfg := fake.configuration()

	out, err := (&SubnetDataSource{DefaultForAz: aws.Bool(false)}).Read(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Nil(t, out.Ipv6CidrBlock)
	assert.Nil(t, out.Ipv6CidrBlockAssociationId)
	assert.Nil(t, out.EnableResourceNameDnsAAAARecordOnLaunch)
	assert.Nil(t, out.EnableResourceNameDnsARecordOnLaunch)
	assert.Nil(t, out.PrivateDnsHostnameTypeOnLaunch)
}
