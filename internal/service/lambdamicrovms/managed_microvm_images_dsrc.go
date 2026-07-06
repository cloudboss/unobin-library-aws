package lambdamicrovms

import (
	"context"
	"fmt"

	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type ManagedMicrovmImagesDataSource struct{}

func (r *ManagedMicrovmImagesDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*ManagedMicrovmImagesDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	items := []ManagedMicrovmImageSummary{}
	paginator := awslambdamicrovms.NewListManagedMicrovmImagesPaginator(client,
		&awslambdamicrovms.ListManagedMicrovmImagesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list managed Microvm images: %w", err)
		}
		for _, item := range page.Items {
			items = append(items, managedMicrovmImageSummaryFromSDK(item))
		}
	}
	return &ManagedMicrovmImagesDataSourceOutput{Items: items}, nil
}
