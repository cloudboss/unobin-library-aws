package ecr

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type imageAPIStub struct {
	calls             []string
	imageInputs       []*ecr.DescribeImagesInput
	imageOutputs      []*ecr.DescribeImagesOutput
	imageErrors       []error
	repositoryInputs  []*ecr.DescribeRepositoriesInput
	repositoryOutputs []*ecr.DescribeRepositoriesOutput
	repositoryErrors  []error
}

func (s *imageAPIStub) DescribeImages(
	_ context.Context,
	in *ecr.DescribeImagesInput,
	_ ...func(*ecr.Options),
) (*ecr.DescribeImagesOutput, error) {
	s.calls = append(s.calls, "DescribeImages")
	copyInput := *in
	copyInput.ImageIds = append([]ecrtypes.ImageIdentifier(nil), in.ImageIds...)
	s.imageInputs = append(s.imageInputs, &copyInput)
	index := len(s.imageInputs) - 1
	if index < len(s.imageErrors) && s.imageErrors[index] != nil {
		return nil, s.imageErrors[index]
	}
	return s.imageOutputs[index], nil
}

func (s *imageAPIStub) DescribeRepositories(
	_ context.Context,
	in *ecr.DescribeRepositoriesInput,
	_ ...func(*ecr.Options),
) (*ecr.DescribeRepositoriesOutput, error) {
	s.calls = append(s.calls, "DescribeRepositories")
	copyInput := *in
	copyInput.RepositoryNames = append([]string(nil), in.RepositoryNames...)
	s.repositoryInputs = append(s.repositoryInputs, &copyInput)
	index := len(s.repositoryInputs) - 1
	if index < len(s.repositoryErrors) && s.repositoryErrors[index] != nil {
		return nil, s.repositoryErrors[index]
	}
	return s.repositoryOutputs[index], nil
}

func TestImageDataSourceValidate(t *testing.T) {
	empty := ""
	digest := "sha256:digest"
	tag := "latest"
	registryID := "123456789012"
	falseValue := false
	trueValue := true

	cases := []struct {
		name    string
		lookup  ImageDataSource
		wantErr string
	}{
		{
			name:    "rejects omitted selectors",
			lookup:  ImageDataSource{RepositoryName: "repo"},
			wantErr: "at least one",
		},
		{
			name:   "accepts digest",
			lookup: ImageDataSource{RepositoryName: "repo", ImageDigest: &digest},
		},
		{
			name:   "accepts tag",
			lookup: ImageDataSource{RepositoryName: "repo", ImageTag: &tag},
		},
		{
			name: "accepts digest and tag",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageDigest:    &digest,
				ImageTag:       &tag,
			},
		},
		{
			name:   "accepts false most recent alone",
			lookup: ImageDataSource{RepositoryName: "repo", MostRecent: &falseValue},
		},
		{
			name:   "accepts true most recent alone",
			lookup: ImageDataSource{RepositoryName: "repo", MostRecent: &trueValue},
		},
		{
			name: "rejects false most recent with digest",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageDigest:    &digest,
				MostRecent:     &falseValue,
			},
			wantErr: "most-recent",
		},
		{
			name: "rejects true most recent with tag",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageTag:       &tag,
				MostRecent:     &trueValue,
			},
			wantErr: "most-recent",
		},
		{
			name: "rejects empty registry id",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageDigest:    &digest,
				RegistryID:     &empty,
			},
			wantErr: "registry-id",
		},
		{
			name: "accepts nonempty registry id",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageDigest:    &digest,
				RegistryID:     &registryID,
			},
		},
		{
			name: "accepts configured empty digest and tag",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageDigest:    &empty,
				ImageTag:       &empty,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.lookup.validate()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestImageDataSourceDecodeKeepsEmptySelectorPresence(t *testing.T) {
	for _, field := range []string{"image-digest", "image-tag"} {
		t.Run(field, func(t *testing.T) {
			var lookup ImageDataSource
			require.NoError(t, runtime.Decode(&lookup, map[string]any{
				"repository-name": "repo",
				field:             "",
			}))
			require.NoError(t, lookup.validate())
		})
	}
}

func TestImageDataSourceDescribeImagesInput(t *testing.T) {
	digest := "sha256:digest"
	tag := "release"
	empty := ""
	registryID := "123456789012"
	trueValue := true

	cases := []struct {
		name   string
		lookup ImageDataSource
		want   *ecr.DescribeImagesInput
	}{
		{
			name: "digest only",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageDigest:    &digest,
			},
			want: &ecr.DescribeImagesInput{
				RepositoryName: aws.String("repo"),
				ImageIds: []ecrtypes.ImageIdentifier{
					{ImageDigest: aws.String(digest)},
				},
			},
		},
		{
			name: "tag only",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageTag:       &tag,
			},
			want: &ecr.DescribeImagesInput{
				RepositoryName: aws.String("repo"),
				ImageIds: []ecrtypes.ImageIdentifier{
					{ImageTag: aws.String(tag)},
				},
			},
		},
		{
			name: "digest and tag share one identifier",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageDigest:    &digest,
				ImageTag:       &tag,
			},
			want: &ecr.DescribeImagesInput{
				RepositoryName: aws.String("repo"),
				ImageIds: []ecrtypes.ImageIdentifier{
					{ImageDigest: aws.String(digest), ImageTag: aws.String(tag)},
				},
			},
		},
		{
			name: "empty selectors do not make an identifier",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageDigest:    &empty,
				ImageTag:       &empty,
			},
			want: &ecr.DescribeImagesInput{RepositoryName: aws.String("repo")},
		},
		{
			name: "most recent stays client side",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				MostRecent:     &trueValue,
			},
			want: &ecr.DescribeImagesInput{RepositoryName: aws.String("repo")},
		},
		{
			name: "configured registry id is sent",
			lookup: ImageDataSource{
				RepositoryName: "repo",
				ImageDigest:    &digest,
				RegistryID:     &registryID,
			},
			want: &ecr.DescribeImagesInput{
				RepositoryName: aws.String("repo"),
				RegistryId:     aws.String(registryID),
				ImageIds: []ecrtypes.ImageIdentifier{
					{ImageDigest: aws.String(digest)},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.lookup.describeImagesInput())
		})
	}
}

func TestImageDataSourceReadSelectsNewestAcrossPages(t *testing.T) {
	oldTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	newTime := time.Date(
		2024, 2, 3, 4, 5, 6, 0,
		time.FixedZone("test-zone", -5*60*60),
	)
	mostRecent := true
	client := &imageAPIStub{
		imageOutputs: []*ecr.DescribeImagesOutput{
			{
				ImageDetails: []ecrtypes.ImageDetail{
					{
						ImageDigest:      aws.String("sha256:old"),
						ImagePushedAt:    &oldTime,
						RegistryId:       aws.String("old-registry"),
						RepositoryName:   aws.String("old-repository"),
						ImageSizeInBytes: aws.Int64(11),
					},
				},
				NextToken: aws.String("next-images"),
			},
			{
				ImageDetails: []ecrtypes.ImageDetail{
					{
						ImageDigest:      aws.String("sha256:new"),
						ImagePushedAt:    &newTime,
						ImageSizeInBytes: aws.Int64(22),
						ImageTags:        []string{"release", "latest"},
						RegistryId:       aws.String("selected-registry"),
						RepositoryName:   aws.String("selected-repository"),
					},
				},
			},
		},
		repositoryOutputs: []*ecr.DescribeRepositoriesOutput{
			{
				Repositories: []ecrtypes.Repository{
					{RepositoryUri: aws.String("registry.example/selected-repository")},
				},
			},
		},
	}
	lookup := &ImageDataSource{
		RepositoryName: "query-repository",
		MostRecent:     &mostRecent,
	}

	out, err := lookup.read(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, &ImageDataSourceOutput{
		ImageDigest:      "sha256:new",
		ImagePushedAt:    newTime.Unix(),
		ImageSizeInBytes: 22,
		ImageTags:        []string{"release", "latest"},
		ImageURI:         "registry.example/selected-repository@sha256:new",
		RegistryID:       "selected-registry",
	}, out)
	assert.Equal(t, []string{
		"DescribeImages",
		"DescribeImages",
		"DescribeRepositories",
	}, client.calls)
	require.Len(t, client.imageInputs, 2)
	assert.Nil(t, client.imageInputs[0].RegistryId)
	assert.Nil(t, client.imageInputs[0].NextToken)
	assert.Equal(t, "next-images", aws.ToString(client.imageInputs[1].NextToken))
	require.Len(t, client.repositoryInputs, 1)
	assert.Equal(t, &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{"selected-repository"},
		RegistryId:      aws.String("selected-registry"),
	}, client.repositoryInputs[0])
}

func TestImageDataSourceReadTreatsNilPushTimeAsOldest(t *testing.T) {
	pushedAt := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	mostRecent := true
	client := imageClientWithDetails(
		ecrtypes.ImageDetail{
			ImageDigest:    aws.String("sha256:nil-time"),
			RegistryId:     aws.String("registry"),
			RepositoryName: aws.String("repo"),
		},
		ecrtypes.ImageDetail{
			ImageDigest:    aws.String("sha256:pushed"),
			ImagePushedAt:  &pushedAt,
			RegistryId:     aws.String("registry"),
			RepositoryName: aws.String("repo"),
		},
	)
	lookup := &ImageDataSource{RepositoryName: "repo", MostRecent: &mostRecent}

	out, err := lookup.read(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "sha256:pushed", out.ImageDigest)
}

func TestImageDataSourceReadCardinality(t *testing.T) {
	tag := "release"
	falseValue := false
	trueValue := true
	detail := ecrtypes.ImageDetail{
		ImageDigest:    aws.String("sha256:one"),
		RegistryId:     aws.String("registry"),
		RepositoryName: aws.String("repo"),
	}

	cases := []struct {
		name       string
		mostRecent *bool
		details    []ecrtypes.ImageDetail
		wantErr    string
	}{
		{name: "zero results", details: nil, wantErr: "no results"},
		{name: "one result", details: []ecrtypes.ImageDetail{detail}},
		{
			name:    "multiple without most recent",
			details: []ecrtypes.ImageDetail{detail, detail},
			wantErr: "more than one result",
		},
		{
			name:       "multiple with false most recent",
			mostRecent: &falseValue,
			details:    []ecrtypes.ImageDetail{detail, detail},
			wantErr:    "more than one result",
		},
		{
			name:       "multiple with true most recent",
			mostRecent: &trueValue,
			details:    []ecrtypes.ImageDetail{detail, detail},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := imageClientWithDetails(tc.details...)
			lookup := &ImageDataSource{
				RepositoryName: "repo",
				ImageTag:       &tag,
				MostRecent:     tc.mostRecent,
			}
			if tc.mostRecent != nil {
				lookup.ImageTag = nil
			}

			out, err := lookup.read(context.Background(), client)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, out)
				assert.Empty(t, client.repositoryInputs)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, out)
		})
	}
}

func TestImageDataSourceReadConvertsImageAbsence(t *testing.T) {
	digest := "sha256:missing"

	cases := []struct {
		name string
		err  error
	}{
		{name: "image not found", err: &ecrtypes.ImageNotFoundException{}},
		{
			name: "wrapped image not found",
			err:  fmt.Errorf("sdk: %w", &ecrtypes.ImageNotFoundException{}),
		},
		{name: "repository not found", err: &ecrtypes.RepositoryNotFoundException{}},
		{
			name: "wrapped repository not found",
			err:  fmt.Errorf("sdk: %w", &ecrtypes.RepositoryNotFoundException{}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &imageAPIStub{
				imageOutputs: []*ecr.DescribeImagesOutput{nil},
				imageErrors:  []error{tc.err},
			}
			lookup := &ImageDataSource{
				RepositoryName: "repo",
				ImageDigest:    &digest,
			}

			_, err := lookup.read(context.Background(), client)
			require.ErrorContains(t, err, "no results")
			assert.NotErrorIs(t, err, runtime.ErrNotFound)
			var imageNotFound *ecrtypes.ImageNotFoundException
			assert.False(t, errors.As(err, &imageNotFound))
			var repositoryNotFound *ecrtypes.RepositoryNotFoundException
			assert.False(t, errors.As(err, &repositoryNotFound))
		})
	}
}

func TestImageDataSourceReadPreservesImageError(t *testing.T) {
	digest := "sha256:digest"
	wantErr := errors.New("image service failed")
	client := &imageAPIStub{
		imageOutputs: []*ecr.DescribeImagesOutput{nil},
		imageErrors:  []error{wantErr},
	}
	lookup := &ImageDataSource{RepositoryName: "repo", ImageDigest: &digest}

	_, err := lookup.read(context.Background(), client)
	require.ErrorContains(t, err, "describe images")
	assert.ErrorIs(t, err, wantErr)
}

func TestImageDataSourceReadRepositoryCardinality(t *testing.T) {
	digest := "sha256:digest"
	detail := ecrtypes.ImageDetail{
		ImageDigest:    aws.String(digest),
		RegistryId:     aws.String("selected-registry"),
		RepositoryName: aws.String("selected-repository"),
	}

	cases := []struct {
		name    string
		outputs []*ecr.DescribeRepositoriesOutput
		errors  []error
		wantErr string
	}{
		{
			name:    "typed not found",
			outputs: []*ecr.DescribeRepositoriesOutput{nil},
			errors:  []error{&ecrtypes.RepositoryNotFoundException{}},
			wantErr: "not found",
		},
		{
			name:    "wrapped typed not found",
			outputs: []*ecr.DescribeRepositoriesOutput{nil},
			errors: []error{
				fmt.Errorf("sdk: %w", &ecrtypes.RepositoryNotFoundException{}),
			},
			wantErr: "not found",
		},
		{
			name:    "zero results",
			outputs: []*ecr.DescribeRepositoriesOutput{{}},
			wantErr: "not found",
		},
		{
			name: "multiple results across pages",
			outputs: []*ecr.DescribeRepositoriesOutput{
				{
					Repositories: []ecrtypes.Repository{{RepositoryUri: aws.String("one")}},
					NextToken:    aws.String("next-repositories"),
				},
				{Repositories: []ecrtypes.Repository{{RepositoryUri: aws.String("two")}}},
			},
			wantErr: "more than one repository",
		},
		{
			name: "one result",
			outputs: []*ecr.DescribeRepositoriesOutput{
				{Repositories: []ecrtypes.Repository{{RepositoryUri: aws.String("uri")}}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &imageAPIStub{
				imageOutputs: []*ecr.DescribeImagesOutput{
					{ImageDetails: []ecrtypes.ImageDetail{detail}},
				},
				repositoryOutputs: tc.outputs,
				repositoryErrors:  tc.errors,
			}
			lookup := &ImageDataSource{RepositoryName: "repo", ImageDigest: &digest}

			out, err := lookup.read(context.Background(), client)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, out)
				var notFound *ecrtypes.RepositoryNotFoundException
				assert.False(t, errors.As(err, &notFound))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "uri@sha256:digest", out.ImageURI)
		})
	}
}

func TestImageDataSourceReadPreservesRepositoryError(t *testing.T) {
	digest := "sha256:digest"
	wantErr := errors.New("repository service failed")
	client := &imageAPIStub{
		imageOutputs: []*ecr.DescribeImagesOutput{
			{ImageDetails: []ecrtypes.ImageDetail{
				{
					ImageDigest:    aws.String(digest),
					RegistryId:     aws.String("registry"),
					RepositoryName: aws.String("repo"),
				},
			}},
		},
		repositoryOutputs: []*ecr.DescribeRepositoriesOutput{nil},
		repositoryErrors:  []error{wantErr},
	}
	lookup := &ImageDataSource{RepositoryName: "repo", ImageDigest: &digest}

	_, err := lookup.read(context.Background(), client)
	require.ErrorContains(t, err, "describe repositories")
	assert.ErrorIs(t, err, wantErr)
}

func imageClientWithDetails(details ...ecrtypes.ImageDetail) *imageAPIStub {
	return &imageAPIStub{
		imageOutputs: []*ecr.DescribeImagesOutput{{ImageDetails: details}},
		repositoryOutputs: []*ecr.DescribeRepositoriesOutput{
			{
				Repositories: []ecrtypes.Repository{
					{RepositoryUri: aws.String("registry.example/repo")},
				},
			},
		},
	}
}
