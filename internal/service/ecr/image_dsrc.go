package ecr

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/cloudboss/unobin/pkg/constraint"
)

// ImageDataSource selects one existing image in an ECR repository.
type ImageDataSource struct {
	RepositoryName string  `ub:"repository-name"`
	ImageDigest    *string `ub:"image-digest"`
	ImageTag       *string `ub:"image-tag"`
	MostRecent     *bool   `ub:"most-recent"`
	RegistryID     *string `ub:"registry-id"`
}

// ImageDataSourceOutput holds the selected image and its immutable URI.
type ImageDataSourceOutput struct {
	ImageDigest      string   `ub:"image-digest"`
	ImagePushedAt    int64    `ub:"image-pushed-at"`
	ImageSizeInBytes int64    `ub:"image-size-in-bytes"`
	ImageTags        []string `ub:"image-tags"`
	ImageURI         string   `ub:"image-uri"`
	RegistryID       string   `ub:"registry-id"`
}

func (r ImageDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtLeastOneOf(r.ImageDigest, r.ImageTag, r.MostRecent),
		constraint.ForbiddenWith(r.MostRecent, r.ImageDigest, r.ImageTag),
		constraint.When(constraint.Present(r.RegistryID)).
			Require(constraint.NotEmpty(r.RegistryID)).
			Message("registry-id must not be empty"),
	}
}

func (r *ImageDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*ImageDataSourceOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

type imageAPI interface {
	ecr.DescribeImagesAPIClient
	ecr.DescribeRepositoriesAPIClient
}

func (r *ImageDataSource) validate() error {
	if r.ImageDigest == nil && r.ImageTag == nil && r.MostRecent == nil {
		return errors.New(
			"at least one of image-digest, image-tag, or most-recent must be supplied")
	}
	if r.MostRecent != nil && (r.ImageDigest != nil || r.ImageTag != nil) {
		return errors.New("most-recent cannot be supplied with image-digest or image-tag")
	}
	if r.RegistryID != nil && *r.RegistryID == "" {
		return errors.New("registry-id must not be empty")
	}
	return nil
}

func (r *ImageDataSource) describeImagesInput() *ecr.DescribeImagesInput {
	in := &ecr.DescribeImagesInput{RepositoryName: aws.String(r.RepositoryName)}
	identifier := ecrtypes.ImageIdentifier{}
	if r.ImageDigest != nil && *r.ImageDigest != "" {
		identifier.ImageDigest = aws.String(*r.ImageDigest)
	}
	if r.ImageTag != nil && *r.ImageTag != "" {
		identifier.ImageTag = aws.String(*r.ImageTag)
	}
	if identifier.ImageDigest != nil || identifier.ImageTag != nil {
		in.ImageIds = []ecrtypes.ImageIdentifier{identifier}
	}
	if r.RegistryID != nil && *r.RegistryID != "" {
		in.RegistryId = aws.String(*r.RegistryID)
	}
	return in
}

func (r *ImageDataSource) read(
	ctx context.Context,
	client imageAPI,
) (*ImageDataSourceOutput, error) {
	images, err := r.findImages(ctx, client)
	if err != nil {
		return nil, err
	}
	image, err := r.selectImage(images)
	if err != nil {
		return nil, err
	}
	repository, err := findImageRepository(ctx, client, image)
	if err != nil {
		return nil, err
	}
	return imageDataSourceOutput(image, repository), nil
}

func (r *ImageDataSource) findImages(
	ctx context.Context,
	client ecr.DescribeImagesAPIClient,
) ([]ecrtypes.ImageDetail, error) {
	var images []ecrtypes.ImageDetail
	pager := ecr.NewDescribeImagesPaginator(client, r.describeImagesInput())
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isImageLookupNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("describe images: %w", err)
		}
		if page == nil {
			return nil, errors.New("describe images: empty response")
		}
		images = append(images, page.ImageDetails...)
	}
	return images, nil
}

func isImageLookupNotFound(err error) bool {
	var imageNotFound *ecrtypes.ImageNotFoundException
	return errors.As(err, &imageNotFound) || isNotFound(err)
}

func (r *ImageDataSource) selectImage(
	images []ecrtypes.ImageDetail,
) (ecrtypes.ImageDetail, error) {
	switch len(images) {
	case 0:
		return ecrtypes.ImageDetail{}, errors.New(
			"query returned no results; please change the search criteria and try again")
	case 1:
		return images[0], nil
	}
	if !aws.ToBool(r.MostRecent) {
		return ecrtypes.ImageDetail{}, errors.New(
			"query returned more than one result; use more specific search criteria " +
				"or set most-recent to true")
	}
	return newestImageDetail(images), nil
}

func newestImageDetail(images []ecrtypes.ImageDetail) ecrtypes.ImageDetail {
	selected := images[0]
	for _, image := range images[1:] {
		if aws.ToTime(image.ImagePushedAt).After(aws.ToTime(selected.ImagePushedAt)) {
			selected = image
		}
	}
	return selected
}

func findImageRepository(
	ctx context.Context,
	client ecr.DescribeRepositoriesAPIClient,
	image ecrtypes.ImageDetail,
) (ecrtypes.Repository, error) {
	input := &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{aws.ToString(image.RepositoryName)},
		RegistryId:      image.RegistryId,
	}
	var repositories []ecrtypes.Repository
	pager := ecr.NewDescribeRepositoriesPaginator(client, input)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return ecrtypes.Repository{}, fmt.Errorf(
					"repository %q for selected ECR image not found",
					aws.ToString(image.RepositoryName))
			}
			return ecrtypes.Repository{}, fmt.Errorf("describe repositories: %w", err)
		}
		if page == nil {
			return ecrtypes.Repository{}, errors.New("describe repositories: empty response")
		}
		repositories = append(repositories, page.Repositories...)
	}
	switch len(repositories) {
	case 0:
		return ecrtypes.Repository{}, fmt.Errorf(
			"repository %q for selected ECR image not found",
			aws.ToString(image.RepositoryName))
	case 1:
		return repositories[0], nil
	default:
		return ecrtypes.Repository{}, fmt.Errorf(
			"more than one repository matched selected ECR image %q",
			aws.ToString(image.ImageDigest))
	}
}

func imageDataSourceOutput(
	image ecrtypes.ImageDetail,
	repository ecrtypes.Repository,
) *ImageDataSourceOutput {
	digest := aws.ToString(image.ImageDigest)
	return &ImageDataSourceOutput{
		ImageDigest:      digest,
		ImagePushedAt:    aws.ToTime(image.ImagePushedAt).Unix(),
		ImageSizeInBytes: aws.ToInt64(image.ImageSizeInBytes),
		ImageTags:        append([]string(nil), image.ImageTags...),
		ImageURI: fmt.Sprintf(
			"%s@%s", aws.ToString(repository.RepositoryUri), digest),
		RegistryID: aws.ToString(image.RegistryId),
	}
}
