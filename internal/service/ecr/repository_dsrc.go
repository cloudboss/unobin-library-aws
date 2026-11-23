package ecr

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/cloudboss/unobin/pkg/constraint"
)

type RepositoryDataSource struct {
	Name       string  `ub:"name"`
	RegistryID *string `ub:"registry-id"`
}

type RepositoryDataSourceOutput struct {
	Arn                        string                      `ub:"arn"`
	Name                       string                      `ub:"name"`
	RegistryID                 string                      `ub:"registry-id"`
	RepositoryURI              string                      `ub:"repository-uri"`
	EncryptionConfiguration    *repositoryLookupEncryption `ub:"encryption-configuration"`
	ImageScanningConfiguration *repositoryLookupScanning   `ub:"image-scanning-configuration"`
	ImageTagMutability         string                      `ub:"image-tag-mutability"`

	ExclusionFilters []RepositoryExclusionFilter `ub:"image-tag-mutability-exclusion-filters"`

	MostRecentImageTags []string          `ub:"most-recent-image-tags"`
	Tags                map[string]string `ub:"tags"`
}

type repositoryLookupEncryption struct {
	EncryptionType string `ub:"encryption-type"`
	KMSKey         string `ub:"kms-key"`
}

type repositoryLookupScanning struct {
	ScanOnPush bool `ub:"scan-on-push"`
}

func (r RepositoryDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.Name, nil)).Message("name is required"),
	}
}

type repositoryLookupAPI interface {
	ecr.DescribeRepositoriesAPIClient
	ecr.DescribeImagesAPIClient
	ListTagsForResource(context.Context, *ecr.ListTagsForResourceInput,
		...func(*ecr.Options)) (*ecr.ListTagsForResourceOutput, error)
}

func (r *RepositoryDataSource) Read(
	ctx context.Context, cfg *awsCfg,
) (*RepositoryDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

func (r *RepositoryDataSource) read(
	ctx context.Context, client repositoryLookupAPI,
) (*RepositoryDataSourceOutput, error) {
	input := &ecr.DescribeRepositoriesInput{RepositoryNames: []string{r.Name}}
	if aws.ToString(r.RegistryID) != "" {
		input.RegistryId = r.RegistryID
	}
	var repositories []types.Repository
	pager := ecr.NewDescribeRepositoriesPaginator(client, input)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe repository %q: %w", r.Name, err)
		}
		if page == nil {
			return nil, fmt.Errorf("describe repository %q: empty response", r.Name)
		}
		repositories = append(repositories, page.Repositories...)
	}
	if len(repositories) != 1 {
		return nil, fmt.Errorf("describe repository %q: expected one result, got %d",
			r.Name, len(repositories))
	}
	repository := repositories[0]
	var images []types.ImageDetail
	imagePager := ecr.NewDescribeImagesPaginator(client, &ecr.DescribeImagesInput{
		RepositoryName: repository.RepositoryName, RegistryId: repository.RegistryId,
	})
	for imagePager.HasMorePages() {
		page, err := imagePager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe repository images %q: %w", r.Name, err)
		}
		if page == nil {
			return nil, fmt.Errorf("describe repository images %q: empty response", r.Name)
		}
		images = append(images, page.ImageDetails...)
	}
	tags := map[string]string{}
	if aws.ToString(repository.RepositoryArn) != "" {
		page, err := client.ListTagsForResource(ctx, &ecr.ListTagsForResourceInput{
			ResourceArn: repository.RepositoryArn,
		})
		if err != nil {
			return nil, fmt.Errorf("list repository tags %q: %w", r.Name, err)
		}
		if page == nil {
			return nil, fmt.Errorf("list repository tags %q: empty response", r.Name)
		}
		for _, tag := range page.Tags {
			if key := aws.ToString(tag.Key); !strings.HasPrefix(key, "aws:") {
				tags[key] = aws.ToString(tag.Value)
			}
		}
	}
	out := &RepositoryDataSourceOutput{
		Arn:                 aws.ToString(repository.RepositoryArn),
		Name:                aws.ToString(repository.RepositoryName),
		RegistryID:          aws.ToString(repository.RegistryId),
		RepositoryURI:       aws.ToString(repository.RepositoryUri),
		ImageTagMutability:  string(repository.ImageTagMutability),
		ExclusionFilters:    []RepositoryExclusionFilter{},
		MostRecentImageTags: []string{}, Tags: tags,
	}
	if value := repository.EncryptionConfiguration; value != nil {
		out.EncryptionConfiguration = &repositoryLookupEncryption{
			EncryptionType: string(value.EncryptionType), KMSKey: aws.ToString(value.KmsKey),
		}
	}
	if value := repository.ImageScanningConfiguration; value != nil {
		out.ImageScanningConfiguration = &repositoryLookupScanning{ScanOnPush: value.ScanOnPush}
	}
	for _, filter := range repository.ImageTagMutabilityExclusionFilters {
		out.ExclusionFilters = append(out.ExclusionFilters, RepositoryExclusionFilter{
			Filter: aws.ToString(filter.Filter), FilterType: string(filter.FilterType),
		})
	}
	if len(images) > 0 {
		out.MostRecentImageTags = append(out.MostRecentImageTags, newestImageDetail(images).ImageTags...)
	}
	return out, nil
}
