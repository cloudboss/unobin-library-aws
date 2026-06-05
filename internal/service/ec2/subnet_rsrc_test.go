package ec2

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const createSubnetResponseXML = `
<CreateSubnetResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
  <requestId>req-1</requestId>
  <subnet>
    <subnetId>subnet-0123456789abcdef0</subnetId>
    <state>pending</state>
    <vpcId>vpc-0123456789abcdef0</vpcId>
    <cidrBlock>10.0.0.0/24</cidrBlock>
    <availabilityZone>us-east-1a</availabilityZone>
    <ownerId>123456789012</ownerId>
  </subnet>
</CreateSubnetResponse>`

// describeSubnetsXML renders the subnet available, with the given IPv6 block
// association state when ipv6State is non-empty.
func describeSubnetsXML(ipv6State string) string {
	assoc := ""
	if ipv6State != "" {
		assoc = fmt.Sprintf(`
      <ipv6CidrBlockAssociationSet>
        <item>
          <associationId>subnet-cidr-assoc-0123456789abcdef0</associationId>
          <ipv6CidrBlock>2600:1f18:1234:5600::/64</ipv6CidrBlock>
          <ipv6CidrBlockState><state>%s</state></ipv6CidrBlockState>
        </item>
      </ipv6CidrBlockAssociationSet>`, ipv6State)
	}
	return fmt.Sprintf(`<DescribeSubnetsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
  <requestId>req-2</requestId>
  <subnetSet>
    <item>
      <subnetId>subnet-0123456789abcdef0</subnetId>
      <state>available</state>
      <vpcId>vpc-0123456789abcdef0</vpcId>
      <cidrBlock>10.0.0.0/24</cidrBlock>
      <subnetArn>arn:aws:ec2:us-east-1:123456789012:subnet/subnet-0123456789abcdef0</subnetArn>
      <ownerId>123456789012</ownerId>
      <availabilityZone>us-east-1a</availabilityZone>
      <availabilityZoneId>use1-az1</availabilityZoneId>%s
    </item>
  </subnetSet>
</DescribeSubnetsResponse>`, assoc)
}

// TestSubnetCreateReturnsExplicitIpv6BlockOnceAssociated drives Create with an
// explicit IPv6 block. EC2 can report the subnet available while the block is
// still associating, so the fake answers "associating" for the first two
// describes and "associated" from the third on. Create's outputs document the
// IPv6 block and its association id as settled values from a describe, so it
// must wait out the association and return them filled, not empty.
func TestSubnetCreateReturnsExplicitIpv6BlockOnceAssociated(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("CreateSubnet", func(int, url.Values) (int, string) {
		return 200, createSubnetResponseXML
	})
	fake.on("DescribeSubnets", func(n int, _ url.Values) (int, string) {
		if n <= 2 {
			return 200, describeSubnetsXML("associating")
		}
		return 200, describeSubnetsXML("associated")
	})
	cfg := fake.configuration()

	r := &Subnet{
		VpcId:         "vpc-0123456789abcdef0",
		CidrBlock:     aws.String("10.0.0.0/24"),
		Ipv6CidrBlock: aws.String("2600:1f18:1234:5600::/64"),
	}
	out, err := r.Create(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "2600:1f18:1234:5600::/64", out.Ipv6CidrBlock,
		"Create must return the IPv6 block once it has associated")
	assert.Equal(t, "subnet-cidr-assoc-0123456789abcdef0", out.Ipv6CidrBlockAssociationId,
		"Create must return the IPv6 block's association id")
}

const modifySubnetAttributeResponseXML = `
<ModifySubnetAttributeResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
  <requestId>req-3</requestId>
  <return>true</return>
</ModifySubnetAttributeResponse>`

// TestSubnetUpdateSendsNoAttributelessModify removes one launch-time option
// between applies and checks every ModifySubnetAttribute call still names an
// attribute. The integer and enum options serialize nothing when their desired
// value is nil, so an update that reacts to their removal with a modify call
// sends a request that holds only the subnet id, which reconciles nothing.
func TestSubnetUpdateSendsNoAttributelessModify(t *testing.T) {
	base := Subnet{
		VpcId:     "vpc-0123456789abcdef0",
		CidrBlock: aws.String("10.0.0.0/24"),
	}
	tests := []struct {
		name  string
		prior func(s *Subnet)
	}{
		{
			name:  "enable-lni-at-device-index removed",
			prior: func(s *Subnet) { s.EnableLniAtDeviceIndex = aws.Int64(1) },
		},
		{
			name:  "private-dns-hostname-type-on-launch removed",
			prior: func(s *Subnet) { s.PrivateDnsHostnameTypeOnLaunch = aws.String("ip-name") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeEC2(t)
			fake.on("DescribeSubnets", func(int, url.Values) (int, string) {
				return 200, describeSubnetsXML("")
			})
			fake.on("ModifySubnetAttribute", func(int, url.Values) (int, string) {
				return 200, modifySubnetAttributeResponseXML
			})
			cfg := fake.configuration()

			priorInputs := base
			tt.prior(&priorInputs)
			current := base
			prior := runtime.Prior[Subnet, *SubnetOutput]{
				Inputs: priorInputs,
				Outputs: &SubnetOutput{
					Id:        "subnet-0123456789abcdef0",
					CidrBlock: "10.0.0.0/24",
				},
			}
			_, err := current.Update(context.Background(), cfg, prior)
			require.NoError(t, err)
			for _, form := range fake.sent("ModifySubnetAttribute") {
				attrs := 0
				for key := range form {
					switch key {
					case "Action", "Version", "SubnetId":
					default:
						attrs++
					}
				}
				assert.NotZero(t, attrs,
					"ModifySubnetAttribute must name the attribute it modifies, got only %v",
					form)
			}
		})
	}
}
