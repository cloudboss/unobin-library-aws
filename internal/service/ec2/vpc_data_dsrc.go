package ec2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// VpcData resolves exactly one existing EC2 VPC with DescribeVpcs. The lookup
// combines the optional vpc-id selector, scalar filters, generic filters, and
// tag filters as one conjunctive query. After selecting one VPC it reads the
// three VPC attributes EC2 exposes through DescribeVpcAttribute, looks up the
// main route table on a best-effort basis, and projects CIDR associations and
// tags from the selected VPC.
type VpcData struct {
	VpcId         *string            `ub:"vpc-id"`
	CidrBlock     *string            `ub:"cidr-block"`
	DhcpOptionsId *string            `ub:"dhcp-options-id"`
	Default       *bool              `ub:"default"`
	State         *string            `ub:"state"`
	Filter        *[]VpcDataFilter   `ub:"filter"`
	Tags          *map[string]string `ub:"tags"`
}

// VpcDataFilter is one DescribeVpcs filter. The name and values pass to EC2
// unchanged; empty-string values and empty value lists are preserved.
type VpcDataFilter struct {
	Name   string   `ub:"name"`
	Values []string `ub:"values"`
}

// VpcDataCidrBlockAssociation is one IPv4 CIDR association from the selected
// VPC, kept in the order EC2 returns it.
type VpcDataCidrBlockAssociation struct {
	AssociationId string `ub:"association-id"`
	CidrBlock     string `ub:"cidr-block"`
	State         string `ub:"state"`
}

// VpcDataOutput holds the attributes of the selected VPC. Arn is composed from
// the selected VPC's owner id, the client region, and the region's partition.
// MainRouteTableId and the IPv6 fields are optional because the lookup omits
// them when EC2 has no singular value to report.
type VpcDataOutput struct {
	VpcId                            string                        `ub:"vpc-id"`
	Arn                              string                        `ub:"arn"`
	CidrBlock                        string                        `ub:"cidr-block"`
	CidrBlockAssociations            []VpcDataCidrBlockAssociation `ub:"cidr-block-associations"`
	Default                          bool                          `ub:"default"`
	DhcpOptionsId                    string                        `ub:"dhcp-options-id"`
	EnableDnsHostnames               bool                          `ub:"enable-dns-hostnames"`
	EnableDnsSupport                 bool                          `ub:"enable-dns-support"`
	EnableNetworkAddressUsageMetrics bool                          `ub:"enable-network-address-usage-metrics"`
	InstanceTenancy                  string                        `ub:"instance-tenancy"`
	Ipv6AssociationId                *string                       `ub:"ipv6-association-id"`
	Ipv6CidrBlock                    *string                       `ub:"ipv6-cidr-block"`
	MainRouteTableId                 *string                       `ub:"main-route-table-id"`
	OwnerId                          string                        `ub:"owner-id"`
	Tags                             map[string]string             `ub:"tags"`
}

// Read resolves the VPC data source. A missing or ambiguous lookup is a normal
// data-source error rather than runtime.ErrNotFound.
func (r *VpcData) Read(ctx context.Context, cfg *awsCfg) (*VpcDataOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	vpc, err := r.findVpc(ctx, client)
	if err != nil {
		return nil, err
	}
	return r.output(ctx, client, vpc)
}

func (r *VpcData) findVpc(ctx context.Context, client *ec2.Client) (ec2types.Vpc, error) {
	vpcs, err := r.findVpcs(ctx, client)
	if err != nil {
		return ec2types.Vpc{}, err
	}
	switch len(vpcs) {
	case 0:
		return ec2types.Vpc{}, errors.New("no matching EC2 VPC found")
	case 1:
		return vpcs[0], nil
	default:
		return ec2types.Vpc{}, errors.New(
			"multiple EC2 VPCs matched; use more specific filters")
	}
}

func (r *VpcData) findVpcs(
	ctx context.Context,
	client *ec2.Client,
) ([]ec2types.Vpc, error) {
	var vpcs []ec2types.Vpc
	paginator := ec2.NewDescribeVpcsPaginator(client, r.describeInput())
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isNotFound(err, "InvalidVpcID.NotFound") {
				return nil, nil
			}
			return nil, fmt.Errorf("describe vpcs: %w", err)
		}
		vpcs = append(vpcs, page.Vpcs...)
	}
	return vpcs, nil
}

func (r *VpcData) describeInput() *ec2.DescribeVpcsInput {
	in := &ec2.DescribeVpcsInput{}
	if r.VpcId != nil && *r.VpcId != "" {
		in.VpcIds = []string{*r.VpcId}
	}
	filters := r.describeFilters()
	if len(filters) > 0 {
		in.Filters = filters
	}
	return in
}

func (r *VpcData) describeFilters() []ec2types.Filter {
	filters := make([]ec2types.Filter, 0, len(ptr.Value(r.Filter))+len(ptr.Value(r.Tags))+4)
	filters = appendStringFilter(filters, "cidr", r.CidrBlock)
	filters = appendStringFilter(filters, "dhcp-options-id", r.DhcpOptionsId)
	if r.Default != nil && *r.Default {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String("isDefault"),
			Values: []string{"true"},
		})
	}
	filters = appendStringFilter(filters, "state", r.State)
	for _, filter := range ptr.Value(r.Filter) {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String(filter.Name),
			Values: filter.Values,
		})
	}
	for key, value := range ptr.Value(r.Tags) {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String("tag:" + key),
			Values: []string{value},
		})
	}
	return filters
}

func appendStringFilter(
	filters []ec2types.Filter,
	name string,
	value *string,
) []ec2types.Filter {
	if value == nil || *value == "" {
		return filters
	}
	return append(filters, ec2types.Filter{
		Name:   aws.String(name),
		Values: []string{*value},
	})
}

func (r *VpcData) output(
	ctx context.Context,
	client *ec2.Client,
	vpc ec2types.Vpc,
) (*VpcDataOutput, error) {
	vpcID := aws.ToString(vpc.VpcId)
	ownerID := aws.ToString(vpc.OwnerId)
	out := &VpcDataOutput{
		VpcId:                 vpcID,
		Arn:                   vpcDataARN(region(client), ownerID, vpcID),
		CidrBlock:             aws.ToString(vpc.CidrBlock),
		CidrBlockAssociations: vpcDataCidrBlockAssociations(vpc.CidrBlockAssociationSet),
		Default:               aws.ToBool(vpc.IsDefault),
		DhcpOptionsId:         aws.ToString(vpc.DhcpOptionsId),
		InstanceTenancy:       string(vpc.InstanceTenancy),
		OwnerId:               ownerID,
		Tags:                  vpcDataTags(vpc.Tags),
	}
	setVpcDataIpv6(out, vpc.Ipv6CidrBlockAssociationSet)

	value, err := readVpcAttribute(ctx, client, vpcID,
		ec2types.VpcAttributeNameEnableDnsHostnames)
	if err != nil {
		return nil, err
	}
	out.EnableDnsHostnames = value
	value, err = readVpcAttribute(ctx, client, vpcID,
		ec2types.VpcAttributeNameEnableDnsSupport)
	if err != nil {
		return nil, err
	}
	out.EnableDnsSupport = value
	value, err = readVpcAttribute(ctx, client, vpcID,
		ec2types.VpcAttributeNameEnableNetworkAddressUsageMetrics)
	if err != nil {
		return nil, err
	}
	out.EnableNetworkAddressUsageMetrics = value
	out.MainRouteTableId = findVpcDataMainRouteTableID(ctx, client, vpcID)
	return out, nil
}

func readVpcAttribute(
	ctx context.Context,
	client *ec2.Client,
	vpcID string,
	attribute ec2types.VpcAttributeName,
) (bool, error) {
	resp, err := client.DescribeVpcAttribute(ctx, &ec2.DescribeVpcAttributeInput{
		Attribute: attribute,
		VpcId:     aws.String(vpcID),
	})
	if err != nil {
		return false, fmt.Errorf("describe vpc attribute %s: %w", attribute, err)
	}
	value, ok := vpcAttributeValue(resp, attribute)
	if !ok {
		return false, fmt.Errorf("describe vpc attribute %s returned no value", attribute)
	}
	return value, nil
}

func vpcAttributeValue(
	resp *ec2.DescribeVpcAttributeOutput,
	attribute ec2types.VpcAttributeName,
) (bool, bool) {
	if resp == nil {
		return false, false
	}
	var value *ec2types.AttributeBooleanValue
	switch attribute {
	case ec2types.VpcAttributeNameEnableDnsHostnames:
		value = resp.EnableDnsHostnames
	case ec2types.VpcAttributeNameEnableDnsSupport:
		value = resp.EnableDnsSupport
	case ec2types.VpcAttributeNameEnableNetworkAddressUsageMetrics:
		value = resp.EnableNetworkAddressUsageMetrics
	}
	if value == nil || value.Value == nil {
		return false, false
	}
	return *value.Value, true
}

func findVpcDataMainRouteTableID(
	ctx context.Context,
	client *ec2.Client,
	vpcID string,
) *string {
	routeTables, err := findVpcDataMainRouteTables(ctx, client, vpcID)
	if err != nil || len(routeTables) != 1 {
		return nil
	}
	id := aws.ToString(routeTables[0].RouteTableId)
	if id == "" {
		return nil
	}
	return aws.String(id)
}

func findVpcDataMainRouteTables(
	ctx context.Context,
	client *ec2.Client,
	vpcID string,
) ([]ec2types.RouteTable, error) {
	in := &ec2.DescribeRouteTablesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("association.main"), Values: []string{"true"}},
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	}
	var routeTables []ec2types.RouteTable
	paginator := ec2.NewDescribeRouteTablesPaginator(client, in)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		routeTables = append(routeTables, page.RouteTables...)
	}
	return routeTables, nil
}

func vpcDataARN(regionName, ownerID, vpcID string) string {
	return fmt.Sprintf("arn:%s:ec2:%s:%s:vpc/%s",
		partition.Of(regionName), regionName, ownerID, vpcID)
}

func vpcDataCidrBlockAssociations(
	associations []ec2types.VpcCidrBlockAssociation,
) []VpcDataCidrBlockAssociation {
	out := make([]VpcDataCidrBlockAssociation, 0, len(associations))
	for _, association := range associations {
		out = append(out, VpcDataCidrBlockAssociation{
			AssociationId: aws.ToString(association.AssociationId),
			CidrBlock:     aws.ToString(association.CidrBlock),
			State:         vpcCidrBlockAssociationState(association),
		})
	}
	return out
}

func vpcCidrBlockAssociationState(association ec2types.VpcCidrBlockAssociation) string {
	if association.CidrBlockState == nil {
		return ""
	}
	return string(association.CidrBlockState.State)
}

func setVpcDataIpv6(
	out *VpcDataOutput,
	associations []ec2types.VpcIpv6CidrBlockAssociation,
) {
	if len(associations) == 0 {
		return
	}
	out.Ipv6AssociationId = copyString(associations[0].AssociationId)
	out.Ipv6CidrBlock = copyString(associations[0].Ipv6CidrBlock)
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func vpcDataTags(tags []ec2types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := aws.ToString(tag.Key)
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	return out
}
