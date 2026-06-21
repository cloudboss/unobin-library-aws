package ec2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/defaults"
)

// Subnets looks up every EC2 subnet matching tag filters and generic
// DescribeSubnets filters. Empty matches are successful and return an empty ids
// list; AWS errors fail the read as ordinary data-source errors.
type Subnets struct {
	Tags   map[string]string `ub:"tags"`
	Filter []SubnetsFilter   `ub:"filter"`
}

// SubnetsFilter is one DescribeSubnets filter. The name is passed to EC2
// unchanged and values are sent as-is, including empty strings and empty lists.
type SubnetsFilter struct {
	Name   string   `ub:"name"`
	Values []string `ub:"values"`
}

// SubnetsOutput holds the matching subnet ids in the order EC2 returns them.
type SubnetsOutput struct {
	Ids []string `ub:"ids"`
}

// Defaults marks the optional collection inputs.
func (r Subnets) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
		defaults.Optional(r.Filter),
	}
}

// Read pages DescribeSubnets in full and returns the matching subnet ids in
// AWS order. A lookup with no filters intentionally asks for all regional
// subnets, and a lookup with no matches is a successful empty list.
func (r *Subnets) Read(ctx context.Context, cfg *awsCfg) (*SubnetsOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	ids, err := r.findIDs(ctx, client)
	if err != nil {
		return nil, err
	}
	return &SubnetsOutput{Ids: ids}, nil
}

func (r *Subnets) findIDs(ctx context.Context, client *ec2.Client) ([]string, error) {
	ids := []string{}
	paginator := ec2.NewDescribeSubnetsPaginator(client, r.describeInput())
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading EC2 subnets: %w", err)
		}
		for _, subnet := range page.Subnets {
			ids = append(ids, aws.ToString(subnet.SubnetId))
		}
	}
	return ids, nil
}

func (r *Subnets) describeInput() *ec2.DescribeSubnetsInput {
	in := &ec2.DescribeSubnetsInput{}
	filters := r.describeFilters()
	if len(filters) > 0 {
		in.Filters = filters
	}
	return in
}

func (r *Subnets) describeFilters() []ec2types.Filter {
	filters := make([]ec2types.Filter, 0, len(r.Tags)+len(r.Filter))
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
