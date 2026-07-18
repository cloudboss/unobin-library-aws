package wafv2

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLCreateTagsAreSorted(t *testing.T) {
	tags := map[string]string{
		"z-last":   "third",
		"a-first":  "first",
		"m-middle": "second",
	}
	resource := validWebACLResource()
	resource.Tags = &tags

	input, err := resource.createInput("example")
	require.NoError(t, err)
	assert.Equal(t, []awstypes.Tag{
		{Key: aws.String("a-first"), Value: aws.String("first")},
		{Key: aws.String("m-middle"), Value: aws.String("second")},
		{Key: aws.String("z-last"), Value: aws.String("third")},
	}, input.Tags)
}

func TestListWebACLTagsNilResponseShapesAreEmpty(t *testing.T) {
	tests := []struct {
		name   string
		output *awssvc.ListTagsForResourceOutput
	}{
		{name: "nil output"},
		{name: "nil tag info", output: &awssvc.ListTagsForResourceOutput{}},
		{
			name: "nil tag list",
			output: &awssvc.ListTagsForResourceOutput{
				TagInfoForResource: &awstypes.TagInfoForResource{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := &fakeWAFClient{list: func(
				_ context.Context,
				in *awssvc.ListTagsForResourceInput,
			) (*awssvc.ListTagsForResourceOutput, error) {
				calls++
				assert.Equal(t, testWebACLARN, aws.ToString(in.ResourceARN))
				assert.Nil(t, in.NextMarker)
				return tt.output, nil
			}}

			actual, err := listWebACLTags(context.Background(), client, testWebACLARN)
			require.NoError(t, err)
			assert.Equal(t, map[string]string{}, actual)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestListWebACLTagsPaginatesAndAdaptsTagList(t *testing.T) {
	markers := []string{}
	client := &fakeWAFClient{list: func(
		_ context.Context,
		in *awssvc.ListTagsForResourceInput,
	) (*awssvc.ListTagsForResourceOutput, error) {
		assert.Equal(t, testWebACLARN, aws.ToString(in.ResourceARN))
		markers = append(markers, aws.ToString(in.NextMarker))
		if len(markers) == 1 {
			return &awssvc.ListTagsForResourceOutput{
				NextMarker: aws.String("next"),
				TagInfoForResource: &awstypes.TagInfoForResource{
					TagList: []awstypes.Tag{
						{Key: aws.String("b"), Value: aws.String("second")},
					},
				},
			}, nil
		}
		return &awssvc.ListTagsForResourceOutput{
			TagInfoForResource: &awstypes.TagInfoForResource{
				TagList: []awstypes.Tag{
					{Key: aws.String("a"), Value: aws.String("first")},
					{Key: aws.String("aws:owned"), Value: aws.String("preserved")},
				},
			},
		}, nil
	}}

	actual, err := listWebACLTags(context.Background(), client, testWebACLARN)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"a":         "first",
		"b":         "second",
		"aws:owned": "preserved",
	}, actual)
	assert.Equal(t, []string{"", "next"}, markers)
}

func TestListWebACLTagsErrorStopsPagination(t *testing.T) {
	sentinel := errors.New("list failed")
	calls := 0
	client := &fakeWAFClient{list: func(
		context.Context,
		*awssvc.ListTagsForResourceInput,
	) (*awssvc.ListTagsForResourceOutput, error) {
		calls++
		if calls == 1 {
			return &awssvc.ListTagsForResourceOutput{NextMarker: aws.String("next")}, nil
		}
		return nil, sentinel
	}}

	actual, err := listWebACLTags(context.Background(), client, testWebACLARN)
	assert.Nil(t, actual)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 2, calls)
}
