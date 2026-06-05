package ec2

import (
	"context"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const createVpcResponseXML = `<CreateVpcResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
  <requestId>req-1</requestId>
  <vpc>
    <vpcId>vpc-0123456789abcdef0</vpcId>
    <state>pending</state>
    <cidrBlock>10.0.0.0/16</cidrBlock>
    <dhcpOptionsId>dopt-0123456789abcdef0</dhcpOptionsId>
    <ownerId>123456789012</ownerId>
  </vpc>
</CreateVpcResponse>`

const describeVpcsAvailableXML = `
<DescribeVpcsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
  <requestId>req-2</requestId>
  <vpcSet>
    <item>
      <vpcId>vpc-0123456789abcdef0</vpcId>
      <state>available</state>
      <cidrBlock>10.0.0.0/16</cidrBlock>
      <dhcpOptionsId>dopt-0123456789abcdef0</dhcpOptionsId>
      <ownerId>123456789012</ownerId>
    </item>
  </vpcSet>
</DescribeVpcsResponse>`

// TestVpcCreateSucceedsThroughPostCreatePropagation exercises the eventual
// consistency window right after CreateVpc, where a describe of the new VPC
// can briefly answer InvalidVpcID.NotFound from a lagging replica. The first
// describe reports the VPC missing and every later one reports it available,
// so Create must ride out the window and succeed, the way the security group
// and subnet resources in this package do for the same window on their APIs.
func TestVpcCreateSucceedsThroughPostCreatePropagation(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("CreateVpc", func(int, url.Values) (int, string) {
		return 200, createVpcResponseXML
	})
	fake.on("DescribeVpcs", func(n int, _ url.Values) (int, string) {
		if n == 1 {
			return 400, ec2ErrorXML("InvalidVpcID.NotFound",
				"The vpc ID 'vpc-0123456789abcdef0' does not exist")
		}
		return 200, describeVpcsAvailableXML
	})
	cfg := fake.configuration()

	r := &Vpc{CidrBlock: aws.String("10.0.0.0/16")}
	out, err := r.Create(context.Background(), cfg)
	require.NoError(t, err,
		"a transient post-create InvalidVpcID.NotFound must not fail Create")
	assert.Equal(t, "vpc-0123456789abcdef0", out.VpcId)
	assert.Equal(t, "dopt-0123456789abcdef0", out.DhcpOptionsId)
	assert.Equal(t, "123456789012", out.OwnerId)
}
