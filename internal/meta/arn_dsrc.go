package meta

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
)

// ARN parses an Amazon Resource Name into its standard components.
type ARN struct {
	ARN string `ub:"arn"`
}

// ARNOutput holds the standard ARN components.
type ARNOutput struct {
	Account   string `ub:"account"`
	Partition string `ub:"partition"`
	Region    string `ub:"region"`
	Resource  string `ub:"resource"`
	Service   string `ub:"service"`
}

func (d *ARN) Read(_ context.Context, _ *awsCfg) (*ARNOutput, error) {
	parsed, err := arn.Parse(d.ARN)
	if err != nil {
		return nil, fmt.Errorf("parse arn: %w", err)
	}
	return &ARNOutput{
		Account:   parsed.AccountID,
		Partition: parsed.Partition,
		Region:    parsed.Region,
		Resource:  parsed.Resource,
		Service:   parsed.Service,
	}, nil
}
