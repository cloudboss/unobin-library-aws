package meta

import (
	"context"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// Regions lists AWS regions returned by EC2 DescribeRegions.
type Regions struct {
	AllRegions *bool            `ub:"all-regions"`
	Filters    *[]RegionsFilter `ub:"filters"`
}

// RegionsFilter is one EC2 DescribeRegions filter.
type RegionsFilter struct {
	Name   string   `ub:"name"`
	Values []string `ub:"values"`
}

// RegionsOutput contains the matched region names.
type RegionsOutput struct {
	Names     []string `ub:"names"`
	Partition string   `ub:"partition"`
}

func (d *Regions) Read(ctx context.Context, cfg *awsCfg) (*RegionsOutput, error) {
	client, sdkCfg, err := newEC2Client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.DescribeRegions(ctx, d.describeInput())
	if err != nil {
		return nil, fmt.Errorf("describe regions: %w", err)
	}
	info, err := findRegionByName(sdkCfg.Region)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(resp.Regions))
	for _, region := range resp.Regions {
		name := aws.ToString(region.RegionName)
		if name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return &RegionsOutput{Names: names, Partition: info.Partition.ID()}, nil
}

func (d *Regions) describeInput() *ec2.DescribeRegionsInput {
	in := &ec2.DescribeRegionsInput{}
	if d.AllRegions != nil {
		in.AllRegions = aws.Bool(*d.AllRegions)
	}
	if d.Filters != nil && len(*d.Filters) > 0 {
		in.Filters = regionsFilters(*d.Filters)
	}
	return in
}

func regionsFilters(filters []RegionsFilter) []ec2types.Filter {
	out := make([]ec2types.Filter, 0, len(filters))
	for _, filter := range filters {
		out = append(out, ec2types.Filter{
			Name:   aws.String(filter.Name),
			Values: filter.Values,
		})
	}
	return out
}
