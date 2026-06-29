package lambdamicrovms

import (
	"context"
	"fmt"

	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type ManagedMicrovmImages struct{}

func (r *ManagedMicrovmImages) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*ManagedMicrovmImagesOutput, error) {
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
	return &ManagedMicrovmImagesOutput{Items: items}, nil
}
