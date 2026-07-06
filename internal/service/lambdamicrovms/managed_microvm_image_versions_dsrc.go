package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type ManagedMicrovmImageVersionsDataSource struct {
	ImageIdentifier string `ub:"image-identifier"`
}

func (r *ManagedMicrovmImageVersionsDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*ManagedMicrovmImageVersionsDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	items := []ManagedMicrovmImageVersion{}
	paginator := awslambdamicrovms.NewListManagedMicrovmImageVersionsPaginator(client,
		&awslambdamicrovms.ListManagedMicrovmImageVersionsInput{
			ImageIdentifier: aws.String(r.ImageIdentifier),
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list managed Microvm image versions: %w", err)
		}
		for _, item := range page.Items {
			items = append(items, managedMicrovmImageVersionFromSDK(item))
		}
	}
	return &ManagedMicrovmImageVersionsDataSourceOutput{Items: items}, nil
}
