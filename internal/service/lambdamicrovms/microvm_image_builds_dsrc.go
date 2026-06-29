package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	lambdamicrovmstypes "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms/types"
	"github.com/cloudboss/unobin/pkg/constraint"
)

type MicrovmImageBuilds struct {
	ImageIdentifier   string  `ub:"image-identifier"`
	ImageVersion      string  `ub:"image-version"`
	Architecture      *string `ub:"architecture"`
	Chipset           *string `ub:"chipset"`
	ChipsetGeneration *string `ub:"chipset-generation"`
}

func (r MicrovmImageBuilds) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.Architecture)).
			Require(constraint.Equals(r.Architecture, "ARM_64")).
			Message("architecture must be ARM_64"),
		constraint.When(constraint.Present(r.Chipset)).
			Require(constraint.Equals(r.Chipset, "GRAVITON")).
			Message("chipset must be GRAVITON"),
	}
}

func (r *MicrovmImageBuilds) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageBuildsOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &awslambdamicrovms.ListMicrovmImageBuildsInput{
		ImageIdentifier:   aws.String(r.ImageIdentifier),
		ImageVersion:      aws.String(r.ImageVersion),
		ChipsetGeneration: r.ChipsetGeneration,
	}
	if r.Architecture != nil {
		in.Architecture = lambdamicrovmstypes.Architecture(*r.Architecture)
	}
	if r.Chipset != nil {
		in.Chipset = lambdamicrovmstypes.Chipset(*r.Chipset)
	}
	items := []MicrovmImageBuildSummary{}
	paginator := awslambdamicrovms.NewListMicrovmImageBuildsPaginator(client, in)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list Microvm image builds: %w", err)
		}
		for _, item := range page.Items {
			items = append(items, microvmImageBuildSummaryFromSDK(item))
		}
	}
	return &MicrovmImageBuildsOutput{Items: items}, nil
}
