package lambdamicrovms

import (
	"context"
	"fmt"

	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type MicrovmImagesDataSource struct {
	NameFilter *string `ub:"name-filter"`
}

func (r *MicrovmImagesDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImagesDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	items := []MicrovmImageSummary{}
	paginator := awslambdamicrovms.NewListMicrovmImagesPaginator(client,
		&awslambdamicrovms.ListMicrovmImagesInput{NameFilter: r.NameFilter})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list Microvm images: %w", err)
		}
		for _, item := range page.Items {
			items = append(items, microvmImageSummaryFromSDK(item))
		}
	}
	return &MicrovmImagesDataSourceOutput{Items: items}, nil
}
