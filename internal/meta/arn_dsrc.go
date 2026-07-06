package meta

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
)

// ARNDataSource parses an Amazon Resource Name into its standard components.
type ARNDataSource struct {
	ARN string `ub:"arn"`
}

// ARNDataSourceOutput holds the standard ARN components.
type ARNDataSourceOutput struct {
	Account   string `ub:"account"`
	Partition string `ub:"partition"`
	Region    string `ub:"region"`
	Resource  string `ub:"resource"`
	Service   string `ub:"service"`
}

func (d *ARNDataSource) Read(_ context.Context, _ *awsCfg) (*ARNDataSourceOutput, error) {
	parsed, err := arn.Parse(d.ARN)
	if err != nil {
		return nil, fmt.Errorf("parse arn: %w", err)
	}
	return &ARNDataSourceOutput{
		Account:   parsed.AccountID,
		Partition: parsed.Partition,
		Region:    parsed.Region,
		Resource:  parsed.Resource,
		Service:   parsed.Service,
	}, nil
}
