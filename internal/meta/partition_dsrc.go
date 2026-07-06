package meta

import "context"

// PartitionDataSource resolves the AWS partition that contains the configured region.
type PartitionDataSource struct{}

// PartitionDataSourceOutput describes the resolved AWS partition.
type PartitionDataSourceOutput struct {
	DNSSuffix        string `ub:"dns-suffix"`
	Partition        string `ub:"partition"`
	ReverseDNSPrefix string `ub:"reverse-dns-prefix"`
}

func (d *PartitionDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*PartitionDataSourceOutput, error) {
	region, err := configuredRegion(ctx, cfg)
	if err != nil {
		return nil, err
	}
	partition, err := findRegionByName(region)
	if err != nil {
		return nil, err
	}
	dnsSuffix := partition.Partition.DNSSuffix()
	return &PartitionDataSourceOutput{
		DNSSuffix:        dnsSuffix,
		Partition:        partition.Partition.ID(),
		ReverseDNSPrefix: reverseDNSPrefix(dnsSuffix),
	}, nil
}
