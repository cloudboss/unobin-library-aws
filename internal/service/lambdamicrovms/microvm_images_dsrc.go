package lambdamicrovms

import (
	"context"
	"fmt"

	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type MicrovmImages struct {
	NameFilter *string `ub:"name-filter"`
}

func (r *MicrovmImages) Read(ctx context.Context, cfg *awsCfg) (*MicrovmImagesOutput, error) {
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
	return &MicrovmImagesOutput{Items: items}, nil
}
