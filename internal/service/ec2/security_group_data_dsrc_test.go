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

const securityGroupDataID = "sg-0123456789abcdef0"

func describeSecurityGroupDataSourcePageXML(nextToken string, items ...string) string {
	next := ""
	if nextToken != "" {
		next = fmt.Sprintf("<nextToken>%s</nextToken>", nextToken)
	}
	return fmt.Sprintf(`<DescribeSecurityGroupsResponse>
  <requestId>req-security-groups</requestId>
  <securityGroupInfo>%s</securityGroupInfo>
  %s
</DescribeSecurityGroupsResponse>`, strings.Join(items, ""), next)
}

func securityGroupDataItemXML(id string) string {
	return fmt.Sprintf(`<item>
  <ownerId>123456789012</ownerId>
  <groupId>%s</groupId>
  <groupName>unobin-it-sg</groupName>
  <groupDescription>unobin integration test security group</groupDescription>
  <vpcId>vpc-0123456789abcdef0</vpcId>
  <securityGroupArn>ignored</securityGroupArn>
  <tagSet>
    <item><key>Name</key><value>unobin-sg-data</value></item>
    <item><key>unobin</key><value>ec2-security-group-data</value></item>
    <item><key>aws:cloudformation:stack-name</key><value>ignored</value></item>
  </tagSet>
</item>`, id)
}

func TestSecurityGroupDataSourceReadPaginatesSendsFiltersAndFlattens(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSecurityGroups", func(n int, form url.Values) (int, string) {
		switch n {
		case 1:
			assert.Empty(t, form.Get("NextToken"))
			return 200, describeSecurityGroupDataSourcePageXML("token-2")
		case 2:
			assert.Equal(t, "token-2", form.Get("NextToken"))
			return 200, describeSecurityGroupDataSourcePageXML("",
				securityGroupDataItemXML(securityGroupDataID))
		default:
			t.Fatalf("unexpected DescribeSecurityGroups call %d", n)
			return 500, ""
		}
	})
	cfg := fake.configuration()

	r := &SecurityGroupDataSource{
		Id:    aws.String(securityGroupDataID),
		Name:  aws.String("unobin-it-sg"),
		VpcId: aws.String("vpc-0123456789abcdef0"),
		Tags:  new(map[string]string{"unobin": "ec2-security-group-data"}),
		Filter: new([]SecurityGroupDataSourceFilter{
			{Name: "owner-id", Values: []string{"123456789012"}},
			{Name: "description", Values: []string{""}},
			{Name: "empty-values", Values: []string{}},
		}),
	}
	out, err := r.Read(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, securityGroupDataID, out.Id)
	assert.Equal(t,
		"arn:aws:ec2:us-east-1:123456789012:security-group/"+securityGroupDataID,
		out.Arn)
	assert.Equal(t, "unobin integration test security group", out.Description)
	assert.Equal(t, "unobin-it-sg", out.Name)
	assert.Equal(t, "vpc-0123456789abcdef0", out.VpcId)
	assert.Equal(t, map[string]string{
		"Name":   "unobin-sg-data",
		"unobin": "ec2-security-group-data",
	}, out.Tags)

	sent := fake.sent("DescribeSecurityGroups")
	require.Len(t, sent, 2)
	assert.Equal(t, securityGroupDataID, sent[0].Get("GroupId.1"))
	assertEC2Filter(t, sent[0], "group-name", []string{"unobin-it-sg"})
	assertEC2Filter(t, sent[0], "vpc-id", []string{"vpc-0123456789abcdef0"})
	assertEC2Filter(t, sent[0], "tag:unobin", []string{"ec2-security-group-data"})
	assertEC2Filter(t, sent[0], "owner-id", []string{"123456789012"})
	assertEC2Filter(t, sent[0], "description", []string{""})
	assertEC2Filter(t, sent[0], "empty-values", []string{})
}

func TestSecurityGroupDataSourceReadWithNoFiltersQueriesAllAndRequiresOne(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSecurityGroups", func(_ int, form url.Values) (int, string) {
		for key := range form {
			assert.False(t, strings.HasPrefix(key, "Filter."), "unexpected filter %s", key)
			assert.False(t, strings.HasPrefix(key, "GroupId."), "unexpected group id %s", key)
		}
		return 200, describeSecurityGroupDataSourcePageXML("")
	})
	cfg := fake.configuration()

	r := &SecurityGroupDataSource{Id: aws.String(""), Name: aws.String(""), VpcId: aws.String("")}
	out, err := r.Read(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
	assert.EqualError(t, err, "no matching EC2 Security Group found")
}

func TestSecurityGroupDataSourceReadTreatsNotFoundAsLookupError(t *testing.T) {
	cases := []string{"InvalidGroup.NotFound", "InvalidSecurityGroupID.NotFound"}
	for _, code := range cases {
		t.Run(code, func(t *testing.T) {
			fake := newFakeEC2(t)
			fake.on("DescribeSecurityGroups", func(int, url.Values) (int, string) {
				return 400, ec2ErrorXML(code, "The security group does not exist")
			})
			cfg := fake.configuration()

			r := &SecurityGroupDataSource{Id: aws.String("sg-missing")}
			out, err := r.Read(context.Background(), cfg)
			require.Error(t, err)
			assert.Nil(t, out)
			assert.NotErrorIs(t, err, runtime.ErrNotFound)
			assert.EqualError(t, err, "no matching EC2 Security Group found")
		})
	}
}

func TestSecurityGroupDataSourceReadErrorsOnMultipleMatches(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSecurityGroups", func(int, url.Values) (int, string) {
		return 200, describeSecurityGroupDataSourcePageXML("",
			securityGroupDataItemXML(securityGroupDataID),
			securityGroupDataItemXML("sg-11111111111111111"))
	})
	cfg := fake.configuration()

	out, err := (&SecurityGroupDataSource{Name: aws.String("unobin-it-sg")}).Read(
		context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.EqualError(t, err,
		"multiple EC2 Security Groups matched; use more specific filters")
}
