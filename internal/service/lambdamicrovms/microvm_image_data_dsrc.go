package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	"github.com/cloudboss/unobin/pkg/constraint"
)

type MicrovmImageDataSource struct {
	ImageIdentifier *string `ub:"image-identifier"`
	Name            *string `ub:"name"`
}

func (r MicrovmImageDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.ImageIdentifier, r.Name),
	}
}

func (r *MicrovmImageDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if r.ImageIdentifier != nil {
		return r.readByIdentifier(ctx, client, *r.ImageIdentifier)
	}
	return r.readByName(ctx, client, *r.Name)
}

func (r *MicrovmImageDataSource) readByName(
	ctx context.Context,
	client *awslambdamicrovms.Client,
	name string,
) (*MicrovmImageDataSourceOutput, error) {
	paginator := awslambdamicrovms.NewListMicrovmImagesPaginator(client,
		&awslambdamicrovms.ListMicrovmImagesInput{NameFilter: aws.String(name)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("find Microvm image named %s: %w", name, err)
		}
		for _, item := range page.Items {
			if aws.ToString(item.Name) != name {
				continue
			}
			return r.readByIdentifier(ctx, client, aws.ToString(item.ImageArn))
		}
	}
	return nil, fmt.Errorf("Microvm image named %s not found", name)
}

func (r *MicrovmImageDataSource) readByIdentifier(
	ctx context.Context,
	client *awslambdamicrovms.Client,
	identifier string,
) (*MicrovmImageDataSourceOutput, error) {
	out, err := client.GetMicrovmImage(ctx, &awslambdamicrovms.GetMicrovmImageInput{
		ImageIdentifier: aws.String(identifier),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("Microvm image %s not found: %w", identifier, err)
		}
		return nil, fmt.Errorf("read Microvm image %s: %w", identifier, err)
	}
	return microvmImageDataOutputFromGet(out), nil
}
