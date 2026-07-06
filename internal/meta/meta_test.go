package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stringPtr(v string) *string { return &v }

func TestARNRead(t *testing.T) {
	out, err := (&ARNDataSource{
		ARN: "arn:aws:lambda:us-east-1:123456789012:function:api",
	}).Read(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, &ARNDataSourceOutput{
		Account:   "123456789012",
		Partition: "aws",
		Region:    "us-east-1",
		Resource:  "function:api",
		Service:   "lambda",
	}, out)
}

func TestPartitionRead(t *testing.T) {
	out, err := (&PartitionDataSource{}).Read(context.Background(), &awscfg.Configuration{
		Region: stringPtr("eusc-de-east-1"),
	})
	require.NoError(t, err)
	assert.Equal(t, &PartitionDataSourceOutput{
		DNSSuffix:        "amazonaws.eu",
		Partition:        "aws-eusc",
		ReverseDNSPrefix: "eu.amazonaws",
	}, out)
}

func TestRegionReadByName(t *testing.T) {
	out, err := (&RegionDataSource{
		Region: stringPtr("cn-north-1"),
	}).Read(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "China (Beijing)", out.Description)
	assert.Equal(t, "ec2.cn-north-1.amazonaws.com.cn", out.Endpoint)
	assert.Equal(t, "aws-cn", out.Partition)
	assert.Equal(t, "cn-north-1", out.Region)
}

func TestRegionReadByEndpoint(t *testing.T) {
	out, err := (&RegionDataSource{Endpoint: stringPtr("ec2.us-east-2.amazonaws.com")}).Read(
		context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "us-east-2", out.Region)
	assert.Equal(t, "aws", out.Partition)
}

func TestServicePrincipalRead(t *testing.T) {
	out, err := (&ServicePrincipalDataSource{ServiceName: "logs"}).Read(
		context.Background(), &awscfg.Configuration{Region: stringPtr("cn-north-1")})
	require.NoError(t, err)
	assert.Equal(t, &ServicePrincipalDataSourceOutput{
		Name:   "logs.amazonaws.com.cn",
		Region: "cn-north-1",
		Suffix: "amazonaws.com.cn",
	}, out)
}

func TestServiceRead(t *testing.T) {
	out, err := (&ServiceDataSource{ServiceID: stringPtr("ec2")}).Read(
		context.Background(), &awscfg.Configuration{Region: stringPtr("us-east-1")})
	require.NoError(t, err)
	assert.Equal(t, &ServiceDataSourceOutput{
		DNSName:          "ec2.us-east-1.amazonaws.com",
		Partition:        "aws",
		Region:           "us-east-1",
		ReverseDNSName:   "com.amazonaws.us-east-1.ec2",
		ReverseDNSPrefix: "com.amazonaws",
		ServiceID:        "ec2",
		Supported:        true,
	}, out)
}

func TestServiceReadFromDNSName(t *testing.T) {
	out, err := (&ServiceDataSource{
		DNSName: stringPtr("S3.CN-NORTH-1.AMAZONAWS.COM.CN"),
	}).Read(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "s3.cn-north-1.amazonaws.com.cn", out.DNSName)
	assert.Equal(t, "aws-cn", out.Partition)
	assert.Equal(t, "cn-north-1", out.Region)
	assert.Equal(t, "cn.com.amazonaws", out.ReverseDNSPrefix)
	assert.Equal(t, "s3", out.ServiceID)
	assert.True(t, out.Supported)
}

func TestServiceReadUnsupported(t *testing.T) {
	out, err := (&ServiceDataSource{ServiceID: stringPtr("not-a-service")}).Read(
		context.Background(), &awscfg.Configuration{Region: stringPtr("us-east-1")})
	require.NoError(t, err)
	assert.Equal(t, "not-a-service.us-east-1.amazonaws.com", out.DNSName)
	assert.False(t, out.Supported)
}

func TestIPRangesRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`{
			"syncToken": "42",
			"createDate": "2026-07-02-00-00-00",
			"prefixes": [
				{"ip_prefix": "10.1.0.0/16", "region": "us-east-1", "service": "AMAZON"},
				{"ip_prefix": "10.2.0.0/16", "region": "us-west-2", "service": "AMAZON"},
				{"ip_prefix": "10.3.0.0/16", "region": "us-east-1", "service": "EC2"}
			],
			"ipv6_prefixes": [
				{"ipv6_prefix": "2001:db8:1::/48", "region": "us-east-1", "service": "AMAZON"},
				{"ipv6_prefix": "2001:db8:2::/48", "region": "us-east-1", "service": "EC2"}
			]
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	out, err := (&IPRangesDataSource{
		Regions:  &[]string{"US-EAST-1"},
		Services: []string{"amazon"},
		URL:      &server.URL,
	}).Read(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, &IPRangesDataSourceOutput{
		CIDRBlocks:     []string{"10.1.0.0/16"},
		CreateDate:     "2026-07-02-00-00-00",
		IPv6CIDRBlocks: []string{"2001:db8:1::/48"},
		SyncToken:      42,
	}, out)
}

func TestRegionsDescribeInput(t *testing.T) {
	allRegions := true
	in := (&RegionsDataSource{
		AllRegions: &allRegions,
		Filters: &[]RegionsFilter{{
			Name:   "opt-in-status",
			Values: []string{"opted-in"},
		}},
	}).describeInput()
	require.NotNil(t, in.AllRegions)
	assert.True(t, *in.AllRegions)
	require.Len(t, in.Filters, 1)
	assert.Equal(t, "opt-in-status", *in.Filters[0].Name)
	assert.Equal(t, []string{"opted-in"}, in.Filters[0].Values)
}
