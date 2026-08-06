package dsql

import (
	"context"

	awssdksql "github.com/aws/aws-sdk-go-v2/service/dsql"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

func newClient(ctx context.Context, cfg *awsCfg) (*awssdksql.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return awssdksql.NewFromConfig(awsCfg), nil
}
