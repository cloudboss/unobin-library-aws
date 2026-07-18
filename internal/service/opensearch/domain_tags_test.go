package opensearch

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncDomainTags(t *testing.T) {
	tests := []struct {
		name       string
		current    map[string]string
		desired    map[string]string
		wantRemove []string
		wantUpsert map[string]string
	}{
		{
			name:    "no-op",
			current: map[string]string{"same": "value"},
			desired: map[string]string{"same": "value"},
		},
		{
			name:       "add and change",
			current:    map[string]string{"change": "old"},
			desired:    map[string]string{"change": "new", "add": "value"},
			wantUpsert: map[string]string{"change": "new", "add": "value"},
		},
		{
			name:       "remove",
			current:    map[string]string{"keep": "value", "remove": "value"},
			desired:    map[string]string{"keep": "value"},
			wantRemove: []string{"remove"},
		},
		{
			name:       "clear",
			current:    map[string]string{"b": "value", "a": "value"},
			desired:    map[string]string{},
			wantRemove: []string{"a", "b"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operations := []string{}
			var removed []string
			var upserted map[string]string
			client := &fakeDomainClient{
				listTags: func(
					context.Context,
					*awssdk.ListTagsInput,
				) (*awssdk.ListTagsOutput, error) {
					operations = append(operations, "list")
					tags := make([]awstypes.Tag, 0, len(test.current))
					for key, value := range test.current {
						tags = append(tags, awstypes.Tag{
							Key: aws.String(key), Value: aws.String(value),
						})
					}
					return &awssdk.ListTagsOutput{TagList: tags}, nil
				},
				removeTags: func(
					_ context.Context,
					input *awssdk.RemoveTagsInput,
				) (*awssdk.RemoveTagsOutput, error) {
					operations = append(operations, "remove")
					removed = append([]string(nil), input.TagKeys...)
					return &awssdk.RemoveTagsOutput{}, nil
				},
				addTags: func(
					_ context.Context,
					input *awssdk.AddTagsInput,
				) (*awssdk.AddTagsOutput, error) {
					operations = append(operations, "add")
					upserted = map[string]string{}
					for _, tag := range input.TagList {
						upserted[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
					}
					return &awssdk.AddTagsOutput{}, nil
				},
			}
			require.NoError(t, syncDomainTags(
				context.Background(), client, "arn:test", test.desired,
			))
			assert.Equal(t, test.wantRemove, removed)
			assert.Equal(t, test.wantUpsert, upserted)
			if len(test.wantRemove) > 0 && len(test.wantUpsert) > 0 {
				assert.Equal(t, []string{"list", "remove", "add"}, operations)
			}
		})
	}
}
