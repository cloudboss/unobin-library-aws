package meta

import (
	"context"
	"errors"
	"fmt"
)

// RegionDataSource resolves one AWS region by name, EC2 endpoint, or the current
// library configuration.
type RegionDataSource struct {
	Endpoint *string `ub:"endpoint"`
	Region   *string `ub:"region"`
}

// RegionDataSourceOutput describes an AWS region and its EC2 endpoint.
type RegionDataSourceOutput struct {
	Description string `ub:"description"`
	Endpoint    string `ub:"endpoint"`
	Partition   string `ub:"partition"`
	Region      string `ub:"region"`
}

func (d *RegionDataSource) Read(ctx context.Context, cfg *awsCfg) (*RegionDataSourceOutput, error) {
	info, err := d.resolveRegion(ctx, cfg)
	if err != nil {
		return nil, err
	}
	endpoint, err := ec2Endpoint(ctx, info.Name)
	if err != nil {
		return nil, fmt.Errorf("resolve EC2 endpoint: %w", err)
	}
	return &RegionDataSourceOutput{
		Description: info.Description,
		Endpoint:    endpoint,
		Partition:   info.Partition.ID(),
		Region:      info.Name,
	}, nil
}

func (d *RegionDataSource) resolveRegion(ctx context.Context, cfg *awsCfg) (regionInfo, error) {
	var byEndpoint *regionInfo
	if endpoint := stringValue(d.Endpoint); endpoint != "" {
		info, err := findRegionByEndpoint(ctx, endpoint)
		if err != nil {
			return regionInfo{}, err
		}
		byEndpoint = &info
	}

	name := stringValue(d.Region)
	if name == "" && byEndpoint == nil {
		var err error
		name, err = configuredRegion(ctx, cfg)
		if err != nil {
			return regionInfo{}, err
		}
	}
	if name == "" {
		return *byEndpoint, nil
	}
	byName, err := findRegionByName(name)
	if err != nil {
		return regionInfo{}, err
	}
	if byEndpoint != nil && byEndpoint.Name != byName.Name {
		return regionInfo{}, errors.New("region and endpoint matched different AWS regions")
	}
	return byName, nil
}
