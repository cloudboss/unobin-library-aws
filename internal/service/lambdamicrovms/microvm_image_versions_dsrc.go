package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
)

type MicrovmImageVersionsDataSource struct {
	ImageIdentifier string `ub:"image-identifier"`
}

func (r *MicrovmImageVersionsDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageVersionsDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	items := []MicrovmImageVersionSummary{}
	paginator := awslambdamicrovms.NewListMicrovmImageVersionsPaginator(client,
		&awslambdamicrovms.ListMicrovmImageVersionsInput{
			ImageIdentifier: aws.String(r.ImageIdentifier),
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list Microvm image versions: %w", err)
		}
		for _, item := range page.Items {
			converted, err := microvmImageVersionSummaryFromSDK(item)
			if err != nil {
				return nil, fmt.Errorf("convert Microvm image version: %w", err)
			}
			items = append(items, converted)
		}
	}
	return &MicrovmImageVersionsDataSourceOutput{Items: items}, nil
}
