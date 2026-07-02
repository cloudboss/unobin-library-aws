package meta

import "context"

// Partition resolves the AWS partition that contains the configured region.
type Partition struct{}

// PartitionOutput describes the resolved AWS partition.
type PartitionOutput struct {
	DNSSuffix        string `ub:"dns-suffix"`
	Partition        string `ub:"partition"`
	ReverseDNSPrefix string `ub:"reverse-dns-prefix"`
}

func (d *Partition) Read(ctx context.Context, cfg *awsCfg) (*PartitionOutput, error) {
	region, err := configuredRegion(ctx, cfg)
	if err != nil {
		return nil, err
	}
	partition, err := findRegionByName(region)
	if err != nil {
		return nil, err
	}
	dnsSuffix := partition.Partition.DNSSuffix()
	return &PartitionOutput{
		DNSSuffix:        dnsSuffix,
		Partition:        partition.Partition.ID(),
		ReverseDNSPrefix: reverseDNSPrefix(dnsSuffix),
	}, nil
}
