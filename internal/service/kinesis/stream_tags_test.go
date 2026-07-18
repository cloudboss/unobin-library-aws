package kinesis

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileStreamTagsPaginatesProtectsSystemTagsAndChunks(t *testing.T) {
	first := make([]awstypes.Tag, 0, 6)
	second := make([]awstypes.Tag, 0, 6)
	for index := range 11 {
		tag := awstypes.Tag{
			Key:   aws.String(fmt.Sprintf("old-%02d", index)),
			Value: aws.String("old"),
		}
		if index < 6 {
			first = append(first, tag)
		} else {
			second = append(second, tag)
		}
	}
	second = append(second, awstypes.Tag{
		Key: aws.String("aws:owner"), Value: aws.String("system"),
	})
	desired := make(map[string]string, 12)
	for index := range 11 {
		desired[fmt.Sprintf("new-%02d", index)] = "new"
	}
	desired["aws:owner"] = "attempted-change"
	client := &fakeStreamClient{listTags: []*awssdk.ListTagsForStreamOutput{
		{Tags: first, HasMoreTags: aws.Bool(true)},
		{Tags: second, HasMoreTags: aws.Bool(false)},
	}}

	err := reconcileStreamTags(
		context.Background(),
		client,
		"arn:stream",
		&desired,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, countCall(client.calls, "ListTagsForStream"))
	require.Len(t, client.removeTagInputs, 2)
	assert.Len(t, client.removeTagInputs[0].TagKeys, 10)
	assert.Len(t, client.removeTagInputs[1].TagKeys, 1)
	for _, input := range client.removeTagInputs {
		assert.NotContains(t, input.TagKeys, "aws:owner")
	}
	require.Len(t, client.addTagInputs, 2)
	assert.Len(t, client.addTagInputs[0].Tags, 10)
	assert.Len(t, client.addTagInputs[1].Tags, 1)
	for _, input := range client.addTagInputs {
		assert.NotContains(t, input.Tags, "aws:owner")
	}
	assert.Equal(t, []string{
		"ListTagsForStream",
		"ListTagsForStream",
		"RemoveTagsFromStream",
		"RemoveTagsFromStream",
		"AddTagsToStream",
		"AddTagsToStream",
	}, client.calls)
}

func TestReconcileStreamTagsClearsAllUserTags(t *testing.T) {
	client := &fakeStreamClient{listTags: []*awssdk.ListTagsForStreamOutput{{
		Tags: []awstypes.Tag{
			{Key: aws.String("env"), Value: aws.String("test")},
			{Key: aws.String("aws:owner"), Value: aws.String("system")},
		},
	}}}
	empty := map[string]string{}

	err := reconcileStreamTags(context.Background(), client, "arn:stream", &empty)
	require.NoError(t, err)
	require.Len(t, client.removeTagInputs, 1)
	assert.Equal(t, []string{"env"}, client.removeTagInputs[0].TagKeys)
	assert.Empty(t, client.addTagInputs)
}

func countCall(calls []string, name string) int {
	count := 0
	for _, call := range calls {
		if call == name {
			count++
		}
	}
	return count
}
