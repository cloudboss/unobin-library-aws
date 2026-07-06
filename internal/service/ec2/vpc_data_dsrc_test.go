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

const vpcDataID = "vpc-0123456789abcdef0"

func describeVpcDataSourcePageXML(nextToken string, items ...string) string {
	next := ""
	if nextToken != "" {
		next = fmt.Sprintf("<nextToken>%s</nextToken>", nextToken)
	}
	return fmt.Sprintf(`<DescribeVpcsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
  <requestId>req-vpcs</requestId>
  <vpcSet>%s</vpcSet>
  %s
</DescribeVpcsResponse>`, strings.Join(items, ""), next)
}

func vpcDataItemXML(id string) string {
	return fmt.Sprintf(`<item>
  <vpcId>%s</vpcId>
  <state>available</state>
  <cidrBlock>10.42.0.0/16</cidrBlock>
  <dhcpOptionsId>dopt-0123456789abcdef0</dhcpOptionsId>
  <instanceTenancy>default</instanceTenancy>
  <isDefault>false</isDefault>
  <ownerId>123456789012</ownerId>
  <cidrBlockAssociationSet>
    <item>
      <associationId>vpc-cidr-assoc-1</associationId>
      <cidrBlock>10.42.0.0/16</cidrBlock>
      <cidrBlockState><state>associated</state></cidrBlockState>
    </item>
    <item>
      <associationId>vpc-cidr-assoc-2</associationId>
      <cidrBlock>10.43.0.0/16</cidrBlock>
      <cidrBlockState><state>associating</state></cidrBlockState>
    </item>
  </cidrBlockAssociationSet>
  <ipv6CidrBlockAssociationSet>
    <item>
      <associationId>vpc-cidr-assoc-ipv6-first</associationId>
      <ipv6CidrBlock>2600:1f18:1234:5600::/56</ipv6CidrBlock>
    </item>
    <item>
      <associationId>vpc-cidr-assoc-ipv6-second</associationId>
      <ipv6CidrBlock>2600:1f18:abcd:ef00::/56</ipv6CidrBlock>
    </item>
  </ipv6CidrBlockAssociationSet>
  <tagSet>
    <item><key>Name</key><value>unobin-vpc-data</value></item>
    <item><key>unobin</key><value>ec2-vpc-data</value></item>
    <item><key>aws:cloudformation:stack-name</key><value>ignored</value></item>
  </tagSet>
</item>`, id)
}

func describeVpcAttributeXML(attribute string, value bool) string {
	return fmt.Sprintf(`<DescribeVpcAttributeResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
  <requestId>req-vpc-attribute</requestId>
  <vpcId>%s</vpcId>
  <%s><value>%t</value></%s>
</DescribeVpcAttributeResponse>`, vpcDataID, attribute, value, attribute)
}

func describeVpcDataSourceRouteTablesXML(nextToken string, ids ...string) string {
	var items strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&items, `<item>
  <routeTableId>%s</routeTableId>
  <vpcId>%s</vpcId>
  <associationSet><item><main>true</main></item></associationSet>
</item>`, id, vpcDataID)
	}
	next := ""
	if nextToken != "" {
		next = fmt.Sprintf("<nextToken>%s</nextToken>", nextToken)
	}
	return fmt.Sprintf(`<DescribeRouteTablesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
  <requestId>req-route-tables</requestId>
  <routeTableSet>%s</routeTableSet>
  %s
</DescribeRouteTablesResponse>`, items.String(), next)
}

func TestVpcDataSourceARNUsesEuscPartition(t *testing.T) {
	assert.Equal(t,
		"arn:aws-eusc:ec2:eusc-de-east-1:123456789012:vpc/"+vpcDataID,
		vpcDataARN("eusc-de-east-1", "123456789012", vpcDataID),
	)
}

func registerVpcDataSourceAttributeHandlers(t *testing.T, fake *fakeEC2) {
	fake.on("DescribeVpcAttribute", func(_ int, form url.Values) (int, string) {
		require.Equal(t, vpcDataID, form.Get("VpcId"))
		switch form.Get("Attribute") {
		case "enableDnsHostnames":
			return 200, describeVpcAttributeXML("enableDnsHostnames", true)
		case "enableDnsSupport":
			return 200, describeVpcAttributeXML("enableDnsSupport", true)
		case "enableNetworkAddressUsageMetrics":
			return 200, describeVpcAttributeXML("enableNetworkAddressUsageMetrics", false)
		default:
			t.Fatalf("unexpected DescribeVpcAttribute attribute %q", form.Get("Attribute"))
			return 500, ""
		}
	})
}

func TestVpcDataSourceReadPaginatesAndEnrichesSelectedVpc(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeVpcs", func(n int, form url.Values) (int, string) {
		switch n {
		case 1:
			assert.Empty(t, form.Get("NextToken"))
			return 200, describeVpcDataSourcePageXML("token-2")
		case 2:
			assert.Equal(t, "token-2", form.Get("NextToken"))
			return 200, describeVpcDataSourcePageXML("", vpcDataItemXML(vpcDataID))
		default:
			t.Fatalf("unexpected DescribeVpcs call %d", n)
			return 500, ""
		}
	})
	registerVpcDataSourceAttributeHandlers(t, fake)
	fake.on("DescribeRouteTables", func(_ int, form url.Values) (int, string) {
		assertEC2Filter(t, form, "association.main", []string{"true"})
		assertEC2Filter(t, form, "vpc-id", []string{vpcDataID})
		return 200, describeVpcDataSourceRouteTablesXML("", "rtb-0123456789abcdef0")
	})
	cfg := fake.configuration()

	r := &VpcDataSource{
		VpcId:         aws.String(vpcDataID),
		CidrBlock:     aws.String("10.42.0.0/16"),
		DhcpOptionsId: aws.String("dopt-0123456789abcdef0"),
		Default:       aws.Bool(true),
		State:         aws.String("available"),
		Filter: new([]VpcDataSourceFilter{
			{Name: "owner-id", Values: []string{"123456789012"}},
			{Name: "description", Values: []string{""}},
			{Name: "empty-values", Values: []string{}},
		}),
		Tags: new(map[string]string{"unobin": "ec2-vpc-data"}),
	}
	out, err := r.Read(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, vpcDataID, out.VpcId)
	assert.Equal(t, "arn:aws:ec2:us-east-1:123456789012:vpc/"+vpcDataID, out.Arn)
	assert.Equal(t, "10.42.0.0/16", out.CidrBlock)
	assert.False(t, out.Default)
	assert.Equal(t, "dopt-0123456789abcdef0", out.DhcpOptionsId)
	assert.True(t, out.EnableDnsHostnames)
	assert.True(t, out.EnableDnsSupport)
	assert.False(t, out.EnableNetworkAddressUsageMetrics)
	assert.Equal(t, "default", out.InstanceTenancy)
	assert.Equal(t, "123456789012", out.OwnerId)
	assert.Equal(t, []VpcDataSourceCidrBlockAssociation{
		{AssociationId: "vpc-cidr-assoc-1", CidrBlock: "10.42.0.0/16", State: "associated"},
		{AssociationId: "vpc-cidr-assoc-2", CidrBlock: "10.43.0.0/16", State: "associating"},
	}, out.CidrBlockAssociations)
	require.NotNil(t, out.Ipv6AssociationId)
	assert.Equal(t, "vpc-cidr-assoc-ipv6-first", *out.Ipv6AssociationId)
	require.NotNil(t, out.Ipv6CidrBlock)
	assert.Equal(t, "2600:1f18:1234:5600::/56", *out.Ipv6CidrBlock)
	require.NotNil(t, out.MainRouteTableId)
	assert.Equal(t, "rtb-0123456789abcdef0", *out.MainRouteTableId)
	assert.Equal(t, map[string]string{
		"Name":   "unobin-vpc-data",
		"unobin": "ec2-vpc-data",
	}, out.Tags)

	sent := fake.sent("DescribeVpcs")
	require.Len(t, sent, 2)
	assert.Equal(t, vpcDataID, sent[0].Get("VpcId.1"))
	assertEC2Filter(t, sent[0], "cidr", []string{"10.42.0.0/16"})
	assertEC2Filter(t, sent[0], "dhcp-options-id", []string{"dopt-0123456789abcdef0"})
	assertEC2Filter(t, sent[0], "isDefault", []string{"true"})
	assertEC2Filter(t, sent[0], "state", []string{"available"})
	assertEC2Filter(t, sent[0], "owner-id", []string{"123456789012"})
	assertEC2Filter(t, sent[0], "description", []string{""})
	assertEC2Filter(t, sent[0], "empty-values", []string{})
	assertEC2Filter(t, sent[0], "tag:unobin", []string{"ec2-vpc-data"})
}

func TestVpcDataSourceReadWithNoFiltersQueriesAllAndRequiresOne(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeVpcs", func(_ int, form url.Values) (int, string) {
		for key := range form {
			assert.False(t, strings.HasPrefix(key, "Filter."), "unexpected filter %s", key)
			assert.False(t, strings.HasPrefix(key, "VpcId."), "unexpected vpc id %s", key)
		}
		return 200, describeVpcDataSourcePageXML("")
	})
	cfg := fake.configuration()

	out, err := (&VpcDataSource{}).Read(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
	assert.EqualError(t, err, "no matching EC2 VPC found")
}

func TestVpcDataSourceReadTreatsVpcIdNotFoundAsLookupError(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeVpcs", func(int, url.Values) (int, string) {
		return 400, ec2ErrorXML("InvalidVpcID.NotFound",
			"The vpc ID 'vpc-missing' does not exist")
	})
	cfg := fake.configuration()

	r := &VpcDataSource{VpcId: aws.String("vpc-missing")}
	out, err := r.Read(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
	assert.EqualError(t, err, "no matching EC2 VPC found")
}

func TestVpcDataSourceReadErrorsOnMultipleMatches(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeVpcs", func(int, url.Values) (int, string) {
		return 200, describeVpcDataSourcePageXML("",
			vpcDataItemXML(vpcDataID),
			vpcDataItemXML("vpc-11111111111111111"))
	})
	cfg := fake.configuration()

	out, err := (&VpcDataSource{CidrBlock: aws.String("10.42.0.0/16")}).Read(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "multiple EC2 VPCs matched")
}

func TestVpcDataSourceReadSwallowsMainRouteTableErrors(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeVpcs", func(int, url.Values) (int, string) {
		return 200, describeVpcDataSourcePageXML("", vpcDataItemXML(vpcDataID))
	})
	registerVpcDataSourceAttributeHandlers(t, fake)
	fake.on("DescribeRouteTables", func(int, url.Values) (int, string) {
		return 400, ec2ErrorXML("InvalidRouteTableID.NotFound", "missing route table")
	})
	cfg := fake.configuration()

	out, err := (&VpcDataSource{VpcId: aws.String(vpcDataID)}).Read(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Nil(t, out.MainRouteTableId)
}
