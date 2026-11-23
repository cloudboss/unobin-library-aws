package ecr

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

type repositoryLookupStub struct {
	imageAPIStub
	tagInput *ecr.ListTagsForResourceInput
	tags     []types.Tag
	tagError error
}

func (s *repositoryLookupStub) ListTagsForResource(
	_ context.Context, in *ecr.ListTagsForResourceInput, _ ...func(*ecr.Options),
) (*ecr.ListTagsForResourceOutput, error) {
	s.calls = append(s.calls, "ListTagsForResource")
	s.tagInput = in
	return &ecr.ListTagsForResourceOutput{Tags: s.tags}, s.tagError
}

func lookupStub(repository types.Repository) *repositoryLookupStub {
	return &repositoryLookupStub{imageAPIStub: imageAPIStub{
		repositoryOutputs: []*ecr.DescribeRepositoriesOutput{{
			Repositories: []types.Repository{repository},
		}},
		imageOutputs: []*ecr.DescribeImagesOutput{{}},
	}}
}

func TestRepositoryLookupRegistryOmission(t *testing.T) {
	for _, input := range []map[string]any{
		{"name": ""}, {"name": "", "registry-id": nil},
		{"name": "", "registry-id": ""}, {"name": "", "registry-id": "explicit"},
	} {
		var lookup RepositoryDataSource
		require.NoError(t, runtime.Decode(&lookup, input))
		client := lookupStub(types.Repository{RegistryId: aws.String("returned")})
		out, err := lookup.read(t.Context(), client)
		require.NoError(t, err)
		want := &ecr.DescribeRepositoriesInput{RepositoryNames: []string{""}}
		if input["registry-id"] == "explicit" {
			want.RegistryId = aws.String("explicit")
		}
		require.Equal(t, []*ecr.DescribeRepositoriesInput{want}, client.repositoryInputs)
		require.Equal(t, "returned", out.RegistryID)
		require.Equal(t, []string{"DescribeRepositories", "DescribeImages"}, client.calls)
	}
}

func TestRepositoryLookupPagesAndOutput(t *testing.T) {
	repository := types.Repository{
		RepositoryName: aws.String("returned"), RegistryId: aws.String("registry"),
		RepositoryArn: aws.String("arn"), RepositoryUri: aws.String("uri"),
		EncryptionConfiguration: &types.EncryptionConfiguration{
			EncryptionType: "future-encryption", KmsKey: aws.String("key"),
		},
		ImageScanningConfiguration: &types.ImageScanningConfiguration{ScanOnPush: false},
		ImageTagMutability:         "future-mutability",
		ImageTagMutabilityExclusionFilters: []types.ImageTagMutabilityExclusionFilter{
			{Filter: aws.String("b*"), FilterType: "future-filter"},
			{Filter: aws.String("a*"), FilterType: "WILDCARD"},
		},
	}
	client := lookupStub(repository)
	client.repositoryOutputs = []*ecr.DescribeRepositoriesOutput{
		{NextToken: aws.String("repository-next")},
		{Repositories: []types.Repository{repository}},
	}
	client.imageOutputs = []*ecr.DescribeImagesOutput{
		{ImageDetails: []types.ImageDetail{{
			ImagePushedAt: aws.Time(time.Unix(1, 0)), ImageTags: []string{"old"},
		}}, NextToken: aws.String("image-next")},
		{ImageDetails: []types.ImageDetail{{
			ImagePushedAt: aws.Time(time.Unix(2, 0)), ImageTags: []string{"z", "a"},
		}}},
	}
	client.tags = []types.Tag{
		{Key: aws.String("aws:hidden"), Value: aws.String("reserved")},
		{Key: aws.String("empty"), Value: aws.String("")},
		{Key: aws.String("ordinary"), Value: aws.String("value")},
	}
	out, err := (&RepositoryDataSource{Name: "selector"}).read(t.Context(), client)
	require.NoError(t, err)
	require.Equal(t, &RepositoryDataSourceOutput{
		Arn: "arn", Name: "returned", RegistryID: "registry", RepositoryURI: "uri",
		EncryptionConfiguration:    &repositoryLookupEncryption{"future-encryption", "key"},
		ImageScanningConfiguration: &repositoryLookupScanning{false},
		ImageTagMutability:         "future-mutability",
		ExclusionFilters: []RepositoryExclusionFilter{
			{"b*", "future-filter"}, {"a*", "WILDCARD"},
		},
		MostRecentImageTags: []string{"z", "a"},
		Tags:                map[string]string{"empty": "", "ordinary": "value"},
	}, out)
	require.Equal(t, []string{
		"DescribeRepositories", "DescribeRepositories", "DescribeImages", "DescribeImages",
		"ListTagsForResource",
	}, client.calls)
	require.Equal(t, []*ecr.DescribeRepositoriesInput{
		{RepositoryNames: []string{"selector"}},
		{RepositoryNames: []string{"selector"}, NextToken: aws.String("repository-next")},
	}, client.repositoryInputs)
	require.Equal(t, []*ecr.DescribeImagesInput{
		{RepositoryName: aws.String("returned"), RegistryId: aws.String("registry")},
		{RepositoryName: aws.String("returned"), RegistryId: aws.String("registry"),
			NextToken: aws.String("image-next")},
	}, client.imageInputs)
	require.Equal(t, &ecr.ListTagsForResourceInput{ResourceArn: aws.String("arn")}, client.tagInput)
}

func TestRepositoryLookupSparseAndNewest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		images []types.ImageDetail
		tags   []string
	}{
		{"empty", nil, []string{}},
		{"nil timestamps", []types.ImageDetail{
			{ImageTags: []string{"first", "second"}}, {ImageTags: []string{"later"}},
		}, []string{"first", "second"}},
		{"reverse order", []types.ImageDetail{
			{ImagePushedAt: aws.Time(time.Unix(2, 0)), ImageTags: []string{"new"}},
			{ImagePushedAt: aws.Time(time.Unix(1, 0)), ImageTags: []string{"old"}},
		}, []string{"new"}},
		{"nil before positive", []types.ImageDetail{
			{ImageTags: []string{"nil"}},
			{ImagePushedAt: aws.Time(time.Unix(1, 0)), ImageTags: []string{"new"}},
		}, []string{"new"}},
		{"equal timestamps", []types.ImageDetail{
			{ImagePushedAt: aws.Time(time.Unix(1, 0)), ImageTags: []string{"first"}},
			{ImagePushedAt: aws.Time(time.Unix(1, 0)), ImageTags: []string{"second"}},
		}, []string{"first"}},
		{"untagged newest", []types.ImageDetail{
			{ImageTags: []string{"old"}}, {ImagePushedAt: aws.Time(time.Unix(1, 0))},
		}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := lookupStub(types.Repository{})
			client.imageOutputs[0].ImageDetails = tc.images
			out, err := (&RepositoryDataSource{}).read(t.Context(), client)
			require.NoError(t, err)
			require.Equal(t, &RepositoryDataSourceOutput{
				ExclusionFilters:    []RepositoryExclusionFilter{},
				MostRecentImageTags: tc.tags, Tags: map[string]string{},
			}, out)
		})
	}
}

func TestRepositoryLookupEmptyTags(t *testing.T) {
	client := lookupStub(types.Repository{RepositoryArn: aws.String("arn")})
	out, err := (&RepositoryDataSource{Name: "repo"}).read(t.Context(), client)
	require.NoError(t, err)
	require.Equal(t, map[string]string{}, out.Tags)
	require.Equal(t, &ecr.ListTagsForResourceInput{ResourceArn: aws.String("arn")}, client.tagInput)
	require.Equal(t, []string{
		"DescribeRepositories", "DescribeImages", "ListTagsForResource",
	}, client.calls)
}

func TestRepositoryLookupFailures(t *testing.T) {
	apiError := errors.New("service failure")
	for _, tc := range []struct {
		name  string
		stage string
		err   error
	}{
		{"repository missing", "repository", &types.RepositoryNotFoundException{}},
		{"repository error", "repository", apiError},
		{"image missing", "image", &types.ImageNotFoundException{}},
		{"image repository missing", "image", &types.RepositoryNotFoundException{}},
		{"image error", "image", apiError},
		{"tag error", "tag", apiError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := lookupStub(types.Repository{RepositoryArn: aws.String("arn")})
			wantCalls := []string{"DescribeRepositories"}
			switch tc.stage {
			case "repository":
				client.repositoryErrors = []error{tc.err}
			case "image":
				client.imageErrors = []error{tc.err}
				wantCalls = append(wantCalls, "DescribeImages")
			case "tag":
				client.tagError = tc.err
				wantCalls = append(wantCalls, "DescribeImages", "ListTagsForResource")
			}
			out, err := (&RepositoryDataSource{Name: "selector"}).read(t.Context(), client)
			require.Nil(t, out)
			require.ErrorIs(t, err, tc.err)
			require.ErrorContains(t, err, tc.stage)
			require.Equal(t, wantCalls, client.calls)
		})
	}
	for _, tc := range []struct {
		name  string
		pages []*ecr.DescribeRepositoriesOutput
	}{
		{"empty", []*ecr.DescribeRepositoriesOutput{{}}},
		{"multiple", []*ecr.DescribeRepositoriesOutput{
			{Repositories: []types.Repository{{}}, NextToken: aws.String("next")},
			{Repositories: []types.Repository{{}}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := lookupStub(types.Repository{})
			client.repositoryOutputs = tc.pages
			out, err := (&RepositoryDataSource{Name: "selector"}).read(t.Context(), client)
			require.Nil(t, out)
			require.ErrorContains(t, err, "repository")
			wantCalls := []string{}
			for range tc.pages {
				wantCalls = append(wantCalls, "DescribeRepositories")
			}
			require.Equal(t, wantCalls, client.calls)
		})
	}
}
