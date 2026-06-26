package ec2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/defaults"
)

// SubnetData resolves exactly one existing EC2 subnet with DescribeSubnets. The
// lookup combines the optional id selector, scalar filters, tag filters, and
// generic filters as one conjunctive query. A missing or ambiguous lookup is a
// normal data-source error, not runtime.ErrNotFound. The selected subnet's IPv6
// association and private-DNS launch options are flattened into the output.
type SubnetData struct {
	Id                 *string            `ub:"id"`
	AvailabilityZone   *string            `ub:"availability-zone"`
	AvailabilityZoneId *string            `ub:"availability-zone-id"`
	DefaultForAz       *bool              `ub:"default-for-az"`
	State              *string            `ub:"state"`
	VpcId              *string            `ub:"vpc-id"`
	CidrBlock          *string            `ub:"cidr-block"`
	Ipv6CidrBlock      *string            `ub:"ipv6-cidr-block"`
	Tags               map[string]string  `ub:"tags"`
	Filter             []SubnetDataFilter `ub:"filter"`
}

// SubnetDataFilter is one DescribeSubnets filter. The name and values pass to
// EC2 unchanged; empty-string values and empty value lists are preserved.
type SubnetDataFilter struct {
	Name   string   `ub:"name"`
	Values []string `ub:"values"`
}

// SubnetDataOutput holds the attributes of the selected subnet. The IPv6 CIDR
// fields are set only from an association whose state is associated. The
// private-DNS launch option fields are unset when EC2 omits the nested options.
type SubnetDataOutput struct {
	Id                                      string            `ub:"id"`
	Arn                                     string            `ub:"arn"`
	AssignIpv6AddressOnCreation             bool              `ub:"assign-ipv6-address-on-creation"`
	AvailabilityZone                        string            `ub:"availability-zone"`
	AvailabilityZoneId                      string            `ub:"availability-zone-id"`
	AvailableIpAddressCount                 int64             `ub:"available-ip-address-count"`
	CidrBlock                               string            `ub:"cidr-block"`
	CustomerOwnedIpv4Pool                   string            `ub:"customer-owned-ipv4-pool"`
	DefaultForAz                            bool              `ub:"default-for-az"`
	EnableDns64                             bool              `ub:"enable-dns64"`
	EnableLniAtDeviceIndex                  int64             `ub:"enable-lni-at-device-index"`
	EnableResourceNameDnsAAAARecordOnLaunch *bool             `ub:"enable-resource-name-dns-aaaa-record-on-launch"`
	EnableResourceNameDnsARecordOnLaunch    *bool             `ub:"enable-resource-name-dns-a-record-on-launch"`
	Ipv6CidrBlock                           *string           `ub:"ipv6-cidr-block"`
	Ipv6CidrBlockAssociationId              *string           `ub:"ipv6-cidr-block-association-id"`
	Ipv6Native                              bool              `ub:"ipv6-native"`
	MapCustomerOwnedIpOnLaunch              bool              `ub:"map-customer-owned-ip-on-launch"`
	MapPublicIpOnLaunch                     bool              `ub:"map-public-ip-on-launch"`
	OutpostArn                              string            `ub:"outpost-arn"`
	OwnerId                                 string            `ub:"owner-id"`
	PrivateDnsHostnameTypeOnLaunch          *string           `ub:"private-dns-hostname-type-on-launch"`
	State                                   string            `ub:"state"`
	Tags                                    map[string]string `ub:"tags"`
	VpcId                                   string            `ub:"vpc-id"`
}

// Defaults marks the collection inputs a subnet lookup may omit.
func (r SubnetData) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
		defaults.Optional(r.Filter),
	}
}

// Read resolves the subnet data source. DescribeSubnets is paginated in full
// before singularity is asserted, so a single page cannot look unique by
// accident.
func (r *SubnetData) Read(ctx context.Context, cfg *awsCfg) (*SubnetDataOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	subnet, err := r.findSubnet(ctx, client)
	if err != nil {
		return nil, err
	}
	return subnetDataOutput(subnet), nil
}

func (r *SubnetData) findSubnet(
	ctx context.Context,
	client *ec2.Client,
) (ec2types.Subnet, error) {
	subnets, err := r.findSubnets(ctx, client)
	if err != nil {
		return ec2types.Subnet{}, err
	}
	switch len(subnets) {
	case 0:
		return ec2types.Subnet{}, errors.New("no matching EC2 Subnet found")
	case 1:
		return subnets[0], nil
	default:
		return ec2types.Subnet{}, errors.New(
			"multiple EC2 Subnets matched; use additional constraints")
	}
}

func (r *SubnetData) findSubnets(
	ctx context.Context,
	client *ec2.Client,
) ([]ec2types.Subnet, error) {
	var subnets []ec2types.Subnet
	paginator := ec2.NewDescribeSubnetsPaginator(client, r.describeInput())
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isNotFound(err, "InvalidSubnetID.NotFound") {
				return nil, nil
			}
			return nil, fmt.Errorf("describe subnets: %w", err)
		}
		subnets = append(subnets, page.Subnets...)
	}
	return subnets, nil
}

func (r *SubnetData) describeInput() *ec2.DescribeSubnetsInput {
	in := &ec2.DescribeSubnetsInput{}
	if r.Id != nil && *r.Id != "" {
		in.SubnetIds = []string{*r.Id}
	}
	filters := r.describeFilters()
	if len(filters) > 0 {
		in.Filters = filters
	}
	return in
}

func (r *SubnetData) describeFilters() []ec2types.Filter {
	filters := make([]ec2types.Filter, 0, len(r.Tags)+len(r.Filter)+7)
	filters = appendSubnetDataStringFilter(filters, "availabilityZone", r.AvailabilityZone)
	filters = appendSubnetDataStringFilter(filters, "availabilityZoneId", r.AvailabilityZoneId)
	if r.DefaultForAz != nil && *r.DefaultForAz {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String("defaultForAz"),
			Values: []string{"true"},
		})
	}
	filters = appendSubnetDataStringFilter(filters, "state", r.State)
	filters = appendSubnetDataStringFilter(filters, "vpc-id", r.VpcId)
	filters = appendSubnetDataStringFilter(filters, "cidrBlock", r.CidrBlock)
	filters = appendSubnetDataStringFilter(filters,
		"ipv6-cidr-block-association.ipv6-cidr-block", r.Ipv6CidrBlock)
	for key, value := range r.Tags {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String("tag:" + key),
			Values: []string{value},
		})
	}
	for _, filter := range r.Filter {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String(filter.Name),
			Values: filter.Values,
		})
	}
	return filters
}

func appendSubnetDataStringFilter(
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

func subnetDataOutput(subnet ec2types.Subnet) *SubnetDataOutput {
	out := &SubnetDataOutput{
		Id:                          aws.ToString(subnet.SubnetId),
		Arn:                         aws.ToString(subnet.SubnetArn),
		AssignIpv6AddressOnCreation: aws.ToBool(subnet.AssignIpv6AddressOnCreation),
		AvailabilityZone:            aws.ToString(subnet.AvailabilityZone),
		AvailabilityZoneId:          aws.ToString(subnet.AvailabilityZoneId),
		AvailableIpAddressCount:     int64(aws.ToInt32(subnet.AvailableIpAddressCount)),
		CidrBlock:                   aws.ToString(subnet.CidrBlock),
		CustomerOwnedIpv4Pool:       aws.ToString(subnet.CustomerOwnedIpv4Pool),
		DefaultForAz:                aws.ToBool(subnet.DefaultForAz),
		EnableDns64:                 aws.ToBool(subnet.EnableDns64),
		EnableLniAtDeviceIndex:      int64(aws.ToInt32(subnet.EnableLniAtDeviceIndex)),
		Ipv6Native:                  aws.ToBool(subnet.Ipv6Native),
		MapCustomerOwnedIpOnLaunch:  aws.ToBool(subnet.MapCustomerOwnedIpOnLaunch),
		MapPublicIpOnLaunch:         aws.ToBool(subnet.MapPublicIpOnLaunch),
		OutpostArn:                  aws.ToString(subnet.OutpostArn),
		OwnerId:                     aws.ToString(subnet.OwnerId),
		State:                       string(subnet.State),
		Tags:                        subnetDataTags(subnet.Tags),
		VpcId:                       aws.ToString(subnet.VpcId),
	}
	setSubnetDataIpv6(out, subnet.Ipv6CidrBlockAssociationSet)
	setSubnetDataPrivateDNS(out, subnet.PrivateDnsNameOptionsOnLaunch)
	return out
}

func setSubnetDataIpv6(
	out *SubnetDataOutput,
	associations []ec2types.SubnetIpv6CidrBlockAssociation,
) {
	for _, association := range associations {
		if association.Ipv6CidrBlockState == nil ||
			association.Ipv6CidrBlockState.State !=
				ec2types.SubnetCidrBlockStateCodeAssociated {
			continue
		}
		out.Ipv6CidrBlock = copySubnetDataString(association.Ipv6CidrBlock)
		out.Ipv6CidrBlockAssociationId = copySubnetDataString(association.AssociationId)
		return
	}
}

func setSubnetDataPrivateDNS(
	out *SubnetDataOutput,
	options *ec2types.PrivateDnsNameOptionsOnLaunch,
) {
	if options == nil {
		return
	}
	out.EnableResourceNameDnsAAAARecordOnLaunch = copyBool(
		options.EnableResourceNameDnsAAAARecord)
	out.EnableResourceNameDnsARecordOnLaunch = copyBool(
		options.EnableResourceNameDnsARecord)
	hostnameType := string(options.HostnameType)
	out.PrivateDnsHostnameTypeOnLaunch = &hostnameType
}

func copySubnetDataString(value *string) *string {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func copyBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

// subnetDataTags returns the selected subnet's tags with AWS system tags
// removed. The library configuration has no provider-level ignore-tags rule,
// so there is no second configured filter to apply here.
func subnetDataTags(tags []ec2types.Tag) map[string]string {
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
