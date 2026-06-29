package lambdamicrovms

import (
	"context"
	"fmt"

	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type Microvms struct {
	ImageIdentifier *string `ub:"image-identifier"`
	ImageVersion    *string `ub:"image-version"`
}

func (r *Microvms) Read(ctx context.Context, cfg *awsCfg) (*MicrovmsOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	items := []MicrovmSummary{}
	paginator := awslambdamicrovms.NewListMicrovmsPaginator(client,
		&awslambdamicrovms.ListMicrovmsInput{
			ImageIdentifier: r.ImageIdentifier,
			ImageVersion:    r.ImageVersion,
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list Microvms: %w", err)
		}
		for _, item := range page.Items {
			items = append(items, microvmSummaryFromSDK(item))
		}
	}
	return &MicrovmsOutput{Items: items}, nil
}
