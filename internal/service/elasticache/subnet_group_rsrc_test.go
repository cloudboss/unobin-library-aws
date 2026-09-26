package elasticache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

const (
	testSubnetGroupName = "mixed-name"
	testSubnetGroupArn  = "arn:aws:elasticache:us-east-1:123456789012:subnetgroup:mixed-name"
	testVpcID           = "vpc-12345678"
)

func TestSubnetGroupValidateInputs(t *testing.T) {
	tests := []struct {
		name     string
		resource SubnetGroupResource
		wantErr  string
	}{
		{
			name:     "one character name",
			resource: SubnetGroupResource{Name: "a", SubnetIds: []string{"subnet-a"}},
		},
		{
			name: "255 character name",
			resource: SubnetGroupResource{
				Name: strings.Repeat("A", 255), SubnetIds: []string{"subnet-a"},
			},
		},
		{
			name:     "hyphens allowed",
			resource: SubnetGroupResource{Name: "A-1", SubnetIds: []string{"subnet-a"}},
		},
		{
			name:     "empty name",
			resource: SubnetGroupResource{SubnetIds: []string{"subnet-a"}},
			wantErr:  "name must be 1 to 255 ASCII characters",
		},
		{
			name: "name too long",
			resource: SubnetGroupResource{
				Name: strings.Repeat("a", 256), SubnetIds: []string{"subnet-a"},
			},
			wantErr: "name must be 1 to 255 ASCII characters",
		},
		{
			name:     "underscore rejected",
			resource: SubnetGroupResource{Name: "bad_name", SubnetIds: []string{"subnet-a"}},
			wantErr:  "name must contain only ASCII letters, digits, or hyphen",
		},
		{
			name:     "unicode rejected",
			resource: SubnetGroupResource{Name: "café", SubnetIds: []string{"subnet-a"}},
			wantErr:  "name must contain only ASCII letters, digits, or hyphen",
		},
		{
			name:     "empty subnet list",
			resource: SubnetGroupResource{Name: "valid"},
			wantErr:  "subnet-ids must not be empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.resource.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestSubnetGroupTagValidationBeforeMutation(t *testing.T) {
	tests := []struct {
		name    string
		tags    map[string]string
		wantErr string
	}{
		{name: "50 tags", tags: numberedSubnetGroupTags(50)},
		{
			name:    "51 tags",
			tags:    numberedSubnetGroupTags(51),
			wantErr: "tags must have at most 50 entries",
		},
		{name: "one character key", tags: map[string]string{"x": "value"}},
		{
			name:    "empty key",
			tags:    map[string]string{"": "value"},
			wantErr: "tag key must be 1 to 128 characters",
		},
		{
			name: "128 character key",
			tags: map[string]string{strings.Repeat("k", 128): "value"},
		},
		{
			name:    "129 character key",
			tags:    map[string]string{strings.Repeat("k", 129): "value"},
			wantErr: "tag key must be 1 to 128 characters",
		},
		{name: "empty value", tags: map[string]string{"key": ""}},
		{
			name: "256 character value",
			tags: map[string]string{"key": strings.Repeat("v", 256)},
		},
		{
			name:    "257 character value",
			tags:    map[string]string{"key": strings.Repeat("v", 257)},
			wantErr: "tag value must be at most 256 characters",
		},
		{
			name: "128 multibyte character key",
			tags: map[string]string{strings.Repeat("界", 128): "value"},
		},
		{
			name:    "129 multibyte character key",
			tags:    map[string]string{strings.Repeat("界", 129): "value"},
			wantErr: "tag key must be 1 to 128 characters",
		},
		{
			name: "256 multibyte character value",
			tags: map[string]string{"key": strings.Repeat("界", 256)},
		},
		{
			name:    "257 multibyte character value",
			tags:    map[string]string{"key": strings.Repeat("界", 257)},
			wantErr: "tag value must be at most 256 characters",
		},
		{
			name:    "reserved desired key",
			tags:    map[string]string{"aws:reserved": "value"},
			wantErr: "tag key must not begin with aws:",
		},
		{
			name: "reserved text in value",
			tags: map[string]string{"key": "ordinary aws:value"},
		},
		{
			name: "reserved prefix is case sensitive",
			tags: map[string]string{"AWS:allowed": "value"},
		},
		{
			name: "arbitrary characters",
			tags: map[string]string{"team /+=._:@ café 🚀": "value /+=._:@ 🚀"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("create", func(t *testing.T) {
				r := testSubnetGroupResource()
				r.Tags = &tt.tags
				client := newFakeSubnetGroupClient()

				_, err := r.create(context.Background(), client, "us-east-1",
					fastRetryOptions()...)

				if tt.wantErr != "" {
					require.EqualError(t, err, tt.wantErr)
					assert.Empty(t, client.calls)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, []string{"create", "describe"}, client.calls)
				assert.Equal(t, tt.tags, tagMap(client.createInputs[0].Tags))
			})

			t.Run("update", func(t *testing.T) {
				r := testSubnetGroupResource()
				r.Description = "changed"
				r.Tags = &tt.tags
				client := newFakeSubnetGroupClient()

				_, err := r.update(context.Background(), client,
					testSubnetGroupPrior(), fastRetryOptions()...)

				if tt.wantErr != "" {
					require.EqualError(t, err, tt.wantErr)
					assert.Empty(t, client.calls)
					return
				}
				require.NoError(t, err)
				assert.Equal(t,
					[]string{"modify", "list-tags", "add-tags", "describe"},
					client.calls)
				assert.Equal(t, tt.tags, tagMap(client.addTagsInputs[0].Tags))
			})
		})
	}
}

func TestSubnetGroupMetadata(t *testing.T) {
	r := &SubnetGroupResource{}
	assert.Equal(t, 1, r.SchemaVersion())
	assert.Equal(t, []string{"name"}, r.ReplaceFields())
}

func TestSubnetGroupCreate(t *testing.T) {
	t.Run("sends complete request and reads lowercase identity", func(t *testing.T) {
		tags := map[string]string{
			"team": "platform", "env": "test", "note": "ordinary aws:value",
		}
		r := &SubnetGroupResource{
			Name: "MiXeD-NaMe", SubnetIds: []string{"subnet-a", "subnet-b"}, Tags: &tags,
		}
		client := newFakeSubnetGroupClient()

		out, err := r.create(context.Background(), client, "us-east-1",
			fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, []string{"create", "describe"}, client.calls)
		require.Len(t, client.createInputs, 1)
		in := client.createInputs[0]
		assert.Equal(t, "MiXeD-NaMe", aws.ToString(in.CacheSubnetGroupName))
		assert.Equal(t, "", aws.ToString(in.CacheSubnetGroupDescription))
		assert.Equal(t, []string{"subnet-a", "subnet-b"}, in.SubnetIds)
		assert.Equal(t, map[string]string{
			"env": "test", "note": "ordinary aws:value", "team": "platform",
		},
			tagMap(in.Tags))
		require.Len(t, client.describeInputs, 1)
		assert.Equal(t, "mixed-name",
			aws.ToString(client.describeInputs[0].CacheSubnetGroupName))
		assert.Equal(t, &SubnetGroupResourceOutput{
			Name: testSubnetGroupName, Arn: testSubnetGroupArn, VpcId: testVpcID,
		}, out)
	})

	t.Run("keeps an explicit empty description", func(t *testing.T) {
		r := testSubnetGroupResource()
		r.Description = ""
		client := newFakeSubnetGroupClient()

		_, err := r.create(context.Background(), client, "us-east-1",
			fastRetryOptions()...)

		require.NoError(t, err)
		assert.NotNil(t, client.createInputs[0].CacheSubnetGroupDescription)
		assert.Equal(t, "", aws.ToString(client.createInputs[0].CacheSubnetGroupDescription))
	})

	t.Run("omits an empty tag set", func(t *testing.T) {
		r := testSubnetGroupResource()
		r.Tags = nil
		client := newFakeSubnetGroupClient()

		_, err := r.create(context.Background(), client, "us-east-1",
			fastRetryOptions()...)

		require.NoError(t, err)
		assert.Nil(t, client.createInputs[0].Tags)
		assert.Equal(t, []string{"create", "describe"}, client.calls)
	})

	t.Run("retries tagged create in a nonstandard partition", func(t *testing.T) {
		r := testSubnetGroupResource()
		client := newFakeSubnetGroupClient()
		client.createErrors = []error{
			&smithy.GenericAPIError{Code: "UnsupportedOperation", Message: "tags unavailable"},
			nil,
		}
		client.createOutputs = []*elasticache.CreateCacheSubnetGroupOutput{
			nil,
			{CacheSubnetGroup: &elasticachetypes.CacheSubnetGroup{ARN: aws.String(
				testSubnetGroupArn)}},
		}

		_, err := r.create(context.Background(), client, "us-iso-east-1",
			fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, []string{"create", "create", "add-tags", "describe"}, client.calls)
		require.Len(t, client.createInputs, 2)
		assert.NotEmpty(t, client.createInputs[0].Tags)
		assert.Nil(t, client.createInputs[1].Tags)
		require.Len(t, client.addTagsInputs, 1)
		assert.Equal(t, testSubnetGroupArn,
			aws.ToString(client.addTagsInputs[0].ResourceName))
		assert.Equal(t, tagMap(client.createInputs[0].Tags),
			tagMap(client.addTagsInputs[0].Tags))
	})

	t.Run("does not fall back in the standard partition", func(t *testing.T) {
		r := testSubnetGroupResource()
		client := newFakeSubnetGroupClient()
		sentinel := &smithy.GenericAPIError{
			Code: "UnsupportedOperation", Message: "failed",
		}
		client.createErrors = []error{sentinel}

		out, err := r.create(context.Background(), client, "us-east-1",
			fastRetryOptions()...)

		assert.Nil(t, out)
		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"create"}, client.calls)
	})

	t.Run("does not fall back for an unrelated failure", func(t *testing.T) {
		r := testSubnetGroupResource()
		client := newFakeSubnetGroupClient()
		sentinel := &smithy.GenericAPIError{Code: "ThrottlingException", Message: "failed"}
		client.createErrors = []error{sentinel}

		_, err := r.create(context.Background(), client, "us-iso-east-1",
			fastRetryOptions()...)

		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"create"}, client.calls)
	})

	t.Run("does not fall back without user tags", func(t *testing.T) {
		r := testSubnetGroupResource()
		r.Tags = nil
		client := newFakeSubnetGroupClient()
		client.createErrors = []error{
			&smithy.GenericAPIError{Code: "UnsupportedOperation", Message: "failed"},
		}

		_, err := r.create(context.Background(), client, "us-iso-east-1",
			fastRetryOptions()...)

		assert.Error(t, err)
		assert.Equal(t, []string{"create"}, client.calls)
	})

	t.Run("returns post-create tag failure without rollback", func(t *testing.T) {
		r := testSubnetGroupResource()
		client := newFakeSubnetGroupClient()
		client.createErrors = []error{
			&smithy.GenericAPIError{Code: "UnsupportedOperation", Message: "failed"}, nil,
		}
		client.createOutputs = []*elasticache.CreateCacheSubnetGroupOutput{
			nil,
			{CacheSubnetGroup: &elasticachetypes.CacheSubnetGroup{ARN: aws.String(
				testSubnetGroupArn)}},
		}
		sentinel := errors.New("tag failed")
		client.addTagsErrors = []error{sentinel}

		_, err := r.create(context.Background(), client, "us-iso-east-1",
			fastRetryOptions()...)

		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"create", "create", "add-tags"}, client.calls)
	})

	t.Run("returns immediate post-create absence", func(t *testing.T) {
		r := testSubnetGroupResource()
		client := newFakeSubnetGroupClient()
		client.describeErrors = []error{&elasticachetypes.CacheSubnetGroupNotFoundFault{}}

		out, err := r.create(context.Background(), client, "us-east-1",
			fastRetryOptions()...)

		assert.Nil(t, out)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
		assert.Equal(t, []string{"create", "describe"}, client.calls)
	})
}

func TestSubnetGroupRead(t *testing.T) {
	t.Run("maps typed not found", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		client.describeErrors = []error{fmt.Errorf("describe: %w",
			&elasticachetypes.CacheSubnetGroupNotFoundFault{})}

		out, err := testSubnetGroupResource().read(
			context.Background(), client, testSubnetGroupName)

		assert.Nil(t, out)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	})

	t.Run("maps empty result to not found", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		client.describeOutputs = []*elasticache.DescribeCacheSubnetGroupsOutput{{}}

		out, err := testSubnetGroupResource().read(
			context.Background(), client, testSubnetGroupName)

		assert.Nil(t, out)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	})

	t.Run("returns only computed outputs", func(t *testing.T) {
		client := newFakeSubnetGroupClient()

		out, err := testSubnetGroupResource().read(
			context.Background(), client, testSubnetGroupName)

		require.NoError(t, err)
		assert.Equal(t, &SubnetGroupResourceOutput{
			Name: testSubnetGroupName, Arn: testSubnetGroupArn, VpcId: testVpcID,
		}, out)
		assert.Equal(t, testSubnetGroupName,
			aws.ToString(client.describeInputs[0].CacheSubnetGroupName))
	})

	t.Run("rejects multiple results", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		group := testCacheSubnetGroup()
		client.describeOutputs = []*elasticache.DescribeCacheSubnetGroupsOutput{
			{CacheSubnetGroups: []elasticachetypes.CacheSubnetGroup{group, group}},
		}

		out, err := testSubnetGroupResource().read(
			context.Background(), client, testSubnetGroupName)

		assert.Nil(t, out)
		assert.EqualError(t, err,
			"describe cache subnet groups: 2 results for name mixed-name")
	})

	t.Run("paginates before returning one result", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		client.describeOutputs = []*elasticache.DescribeCacheSubnetGroupsOutput{
			{Marker: aws.String("next")},
			{CacheSubnetGroups: []elasticachetypes.CacheSubnetGroup{testCacheSubnetGroup()}},
		}

		out, err := testSubnetGroupResource().read(
			context.Background(), client, testSubnetGroupName)

		require.NoError(t, err)
		assert.Equal(t, testSubnetGroupName, out.Name)
		require.Len(t, client.describeInputs, 2)
		assert.Nil(t, client.describeInputs[0].Marker)
		assert.Equal(t, "next", aws.ToString(client.describeInputs[1].Marker))
		for _, in := range client.describeInputs {
			assert.Equal(t, testSubnetGroupName, aws.ToString(in.CacheSubnetGroupName))
		}
	})

	t.Run("rejects results split across pages", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		group := testCacheSubnetGroup()
		client.describeOutputs = []*elasticache.DescribeCacheSubnetGroupsOutput{
			{CacheSubnetGroups: []elasticachetypes.CacheSubnetGroup{group},
				Marker: aws.String("next")},
			{CacheSubnetGroups: []elasticachetypes.CacheSubnetGroup{group}},
		}

		_, err := testSubnetGroupResource().read(
			context.Background(), client, testSubnetGroupName)

		assert.EqualError(t, err,
			"describe cache subnet groups: 2 results for name mixed-name")
	})

	t.Run("wraps another describe failure", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		sentinel := errors.New("network failed")
		client.describeErrors = []error{sentinel}

		_, err := testSubnetGroupResource().read(
			context.Background(), client, testSubnetGroupName)

		assert.ErrorIs(t, err, sentinel)
		assert.ErrorContains(t, err, "describe cache subnet groups")
	})
}

func TestSubnetGroupIdentity(t *testing.T) {
	assert.Equal(t, "mixed-name", subnetGroupIdentity("MiXeD-NaMe", nil))
	assert.Equal(t, "cloud-name", subnetGroupIdentity("new-name",
		&SubnetGroupResourceOutput{Name: "cloud-name"}))
	assert.Equal(t, "new-name", subnetGroupIdentity("NeW-NaMe",
		&SubnetGroupResourceOutput{}))
}

func TestSubnetGroupUpdate(t *testing.T) {
	t.Run("description change sends both desired fields", func(t *testing.T) {
		r := testSubnetGroupResource()
		r.Description = "new description"
		prior := testSubnetGroupPrior()
		prior.Inputs.Description = "old description"
		client := newFakeSubnetGroupClient()

		out, err := r.update(context.Background(), client, prior,
			fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, testSubnetGroupName, out.Name)
		assert.Equal(t, []string{"modify", "describe"}, client.calls)
		require.Len(t, client.modifyInputs, 1)
		in := client.modifyInputs[0]
		assert.Equal(t, testSubnetGroupName, aws.ToString(in.CacheSubnetGroupName))
		assert.Equal(t, "new description",
			aws.ToString(in.CacheSubnetGroupDescription))
		assert.Equal(t, r.SubnetIds, in.SubnetIds)
	})

	t.Run("subnet change sends both desired fields", func(t *testing.T) {
		r := testSubnetGroupResource()
		r.SubnetIds = []string{"subnet-b", "subnet-c"}
		prior := testSubnetGroupPrior()
		client := newFakeSubnetGroupClient()

		_, err := r.update(context.Background(), client, prior,
			fastRetryOptions()...)

		require.NoError(t, err)
		require.Len(t, client.modifyInputs, 1)
		assert.Equal(t, r.Description,
			aws.ToString(client.modifyInputs[0].CacheSubnetGroupDescription))
		assert.Equal(t, []string{"subnet-b", "subnet-c"},
			client.modifyInputs[0].SubnetIds)
	})

	t.Run("description removal explicitly sends empty", func(t *testing.T) {
		r := testSubnetGroupResource()
		r.Description = ""
		prior := testSubnetGroupPrior()
		client := newFakeSubnetGroupClient()

		_, err := r.update(context.Background(), client, prior,
			fastRetryOptions()...)

		require.NoError(t, err)
		require.Len(t, client.modifyInputs, 1)
		assert.NotNil(t, client.modifyInputs[0].CacheSubnetGroupDescription)
		assert.Equal(t, "",
			aws.ToString(client.modifyInputs[0].CacheSubnetGroupDescription))
	})

	t.Run("tag change reconciles by prior ARN", func(t *testing.T) {
		currentTags := map[string]string{"keep": "1", "change": "new", "add": "3"}
		priorTags := map[string]string{"keep": "1", "change": "old", "drop": "2"}
		r := testSubnetGroupResource()
		r.Tags = &currentTags
		prior := testSubnetGroupPrior()
		prior.Inputs.Tags = &priorTags
		client := newFakeSubnetGroupClient()
		client.listTagsOutputs = []*elasticache.ListTagsForResourceOutput{{
			TagList: []elasticachetypes.Tag{
				{Key: aws.String("keep"), Value: aws.String("1")},
				{Key: aws.String("change"), Value: aws.String("old")},
				{Key: aws.String("drop"), Value: aws.String("2")},
			},
		}}

		_, err := r.update(context.Background(), client, prior,
			fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t,
			[]string{"list-tags", "remove-tags", "add-tags", "describe"},
			client.calls)
		assert.Equal(t, testSubnetGroupArn,
			aws.ToString(client.listTagsInputs[0].ResourceName))
		assert.Equal(t, testSubnetGroupArn,
			aws.ToString(client.removeTagsInputs[0].ResourceName))
		assert.Equal(t, []string{"drop"}, client.removeTagsInputs[0].TagKeys)
		assert.Equal(t, testSubnetGroupArn,
			aws.ToString(client.addTagsInputs[0].ResourceName))
		assert.Equal(t, map[string]string{"add": "3", "change": "new"},
			tagMap(client.addTagsInputs[0].Tags))
	})

	t.Run("empty desired tags clears every user tag", func(t *testing.T) {
		currentTags := map[string]string{}
		priorTags := map[string]string{"drop": "1"}
		r := testSubnetGroupResource()
		r.Tags = &currentTags
		prior := testSubnetGroupPrior()
		prior.Inputs.Tags = &priorTags
		client := newFakeSubnetGroupClient()
		client.listTagsOutputs = []*elasticache.ListTagsForResourceOutput{{
			TagList: []elasticachetypes.Tag{
				{Key: aws.String("drop"), Value: aws.String("1")},
				{Key: aws.String("aws:system"), Value: aws.String("kept")},
			},
		}}

		_, err := r.update(context.Background(), client, prior,
			fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, []string{"list-tags", "remove-tags", "describe"}, client.calls)
		assert.Equal(t, []string{"drop"}, client.removeTagsInputs[0].TagKeys)
	})

	t.Run("50 desired tags coexist with a live system tag", func(t *testing.T) {
		desired := numberedSubnetGroupTags(50)
		r := testSubnetGroupResource()
		r.Tags = &desired
		prior := testSubnetGroupPrior()
		client := newFakeSubnetGroupClient()
		liveTags := subnetGroupTags(desired)
		liveTags = append(liveTags, elasticachetypes.Tag{
			Key: aws.String("aws:system"), Value: aws.String("kept"),
		})
		client.listTagsOutputs = []*elasticache.ListTagsForResourceOutput{{
			TagList: liveTags,
		}}

		_, err := r.update(context.Background(), client, prior,
			fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, []string{"list-tags", "describe"}, client.calls)
	})

	t.Run("unchanged inputs only read", func(t *testing.T) {
		r := testSubnetGroupResource()
		prior := testSubnetGroupPrior()
		client := newFakeSubnetGroupClient()

		_, err := r.update(context.Background(), client, prior,
			fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, []string{"describe"}, client.calls)
	})

	t.Run("modify failure stops reconciliation", func(t *testing.T) {
		r := testSubnetGroupResource()
		r.Description = "changed"
		prior := testSubnetGroupPrior()
		client := newFakeSubnetGroupClient()
		sentinel := errors.New("modify failed")
		client.modifyErrors = []error{sentinel}

		out, err := r.update(context.Background(), client, prior,
			fastRetryOptions()...)

		assert.Nil(t, out)
		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"modify"}, client.calls)
	})
}

func TestSubnetGroupTagSync(t *testing.T) {
	t.Run("never changes system tags and removes before adding", func(t *testing.T) {
		desired := map[string]string{
			"keep": "1", "change": "new", "add": "3",
		}
		client := newFakeSubnetGroupClient()
		client.listTagsOutputs = []*elasticache.ListTagsForResourceOutput{{
			TagList: []elasticachetypes.Tag{
				{Key: aws.String("keep"), Value: aws.String("1")},
				{Key: aws.String("change"), Value: aws.String("old")},
				{Key: aws.String("remove"), Value: aws.String("2")},
				{Key: aws.String("aws:system"), Value: aws.String("kept")},
			},
		}}

		err := syncSubnetGroupTags(context.Background(), client, testSubnetGroupArn,
			desired, fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, []string{"list-tags", "remove-tags", "add-tags"}, client.calls)
		assert.Equal(t, []string{"remove"}, client.removeTagsInputs[0].TagKeys)
		assert.Equal(t, map[string]string{"add": "3", "change": "new"},
			tagMap(client.addTagsInputs[0].Tags))
	})

	t.Run("unchanged tags only list", func(t *testing.T) {
		desired := map[string]string{"keep": "1"}
		client := newFakeSubnetGroupClient()
		client.listTagsOutputs = []*elasticache.ListTagsForResourceOutput{{
			TagList: []elasticachetypes.Tag{{
				Key: aws.String("keep"), Value: aws.String("1"),
			}},
		}}

		err := syncSubnetGroupTags(context.Background(), client, testSubnetGroupArn,
			desired, fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, []string{"list-tags"}, client.calls)
	})

	t.Run("retries each matching operation", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(*fakeSubnetGroupClient)
			want  []string
		}{
			{
				name: "list",
				setup: func(c *fakeSubnetGroupClient) {
					c.listTagsErrors = []error{tagStateError(), nil}
					c.listTagsOutputs = []*elasticache.ListTagsForResourceOutput{
						nil,
						{TagList: []elasticachetypes.Tag{{
							Key: aws.String("keep"), Value: aws.String("1"),
						}}},
					}
				},
				want: []string{"list-tags", "list-tags"},
			},
			{
				name: "remove",
				setup: func(c *fakeSubnetGroupClient) {
					c.listTagsOutputs = []*elasticache.ListTagsForResourceOutput{{
						TagList: []elasticachetypes.Tag{{
							Key: aws.String("drop"), Value: aws.String("1"),
						}},
					}}
					c.removeTagsErrors = []error{tagStateError(), nil}
				},
				want: []string{"list-tags", "remove-tags", "remove-tags"},
			},
			{
				name: "add",
				setup: func(c *fakeSubnetGroupClient) {
					c.addTagsErrors = []error{tagStateError(), nil}
				},
				want: []string{"list-tags", "add-tags", "add-tags"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client := newFakeSubnetGroupClient()
				tt.setup(client)
				desired := map[string]string{"keep": "1"}
				if tt.name == "remove" {
					desired = map[string]string{}
				}

				err := syncSubnetGroupTags(context.Background(), client,
					testSubnetGroupArn, desired, fastRetryOptions()...)

				require.NoError(t, err)
				assert.Equal(t, tt.want, client.calls)
			})
		}
	})

	t.Run("matching error stops at timeout", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		client.listTagsErrors = []error{tagStateError()}

		err := syncSubnetGroupTags(context.Background(), client, testSubnetGroupArn,
			map[string]string{}, retry.WithTimeout(0), retry.WithInterval(0))

		assert.Error(t, err)
		assert.Equal(t, []string{"list-tags"}, client.calls)
	})

	t.Run("other errors propagate without retry", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(*fakeSubnetGroupClient, error)
			want  []string
		}{
			{
				name: "list",
				setup: func(c *fakeSubnetGroupClient, err error) {
					c.listTagsErrors = []error{err}
				},
				want: []string{"list-tags"},
			},
			{
				name: "remove",
				setup: func(c *fakeSubnetGroupClient, err error) {
					c.listTagsOutputs = []*elasticache.ListTagsForResourceOutput{{
						TagList: []elasticachetypes.Tag{{Key: aws.String("drop")}},
					}}
					c.removeTagsErrors = []error{err}
				},
				want: []string{"list-tags", "remove-tags"},
			},
			{
				name: "add",
				setup: func(c *fakeSubnetGroupClient, err error) {
					c.addTagsErrors = []error{err}
				},
				want: []string{"list-tags", "add-tags"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client := newFakeSubnetGroupClient()
				sentinel := errors.New("failed")
				tt.setup(client, sentinel)
				desired := map[string]string{"add": "1"}
				if tt.name == "remove" {
					desired = map[string]string{}
				}

				err := syncSubnetGroupTags(context.Background(), client,
					testSubnetGroupArn, desired, fastRetryOptions()...)

				assert.ErrorIs(t, err, sentinel)
				assert.Equal(t, tt.want, client.calls)
			})
		}
	})
}

func TestSubnetGroupTagRetryable(t *testing.T) {
	retryable := tagStateError()
	assert.True(t, subnetGroupTagRetryable(retryable))
	assert.True(t, subnetGroupTagRetryable(fmt.Errorf("tag: %w", retryable)))
	assert.False(t, subnetGroupTagRetryable(
		&elasticachetypes.InvalidReplicationGroupStateFault{
			Message: aws.String("replication group is updating"),
		}))
	assert.False(t, subnetGroupTagRetryable(errors.New("not in available state")))
}

func TestSubnetGroupDelete(t *testing.T) {
	t.Run("uses prior lowercase identity without a describe", func(t *testing.T) {
		r := &SubnetGroupResource{Name: "NEW-NAME"}
		client := newFakeSubnetGroupClient()

		err := r.delete(context.Background(), client,
			&SubnetGroupResourceOutput{Name: testSubnetGroupName},
			fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, []string{"delete"}, client.calls)
		assert.Equal(t, testSubnetGroupName,
			aws.ToString(client.deleteInputs[0].CacheSubnetGroupName))
	})

	t.Run("already gone succeeds", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		client.deleteErrors = []error{&elasticachetypes.CacheSubnetGroupNotFoundFault{}}

		err := testSubnetGroupResource().delete(context.Background(), client,
			testSubnetGroupOutput(), fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, []string{"delete"}, client.calls)
	})

	t.Run("dependency violation retries to success", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		client.deleteErrors = []error{
			&smithy.GenericAPIError{Code: "DependencyViolation", Message: "busy"}, nil,
		}

		err := testSubnetGroupResource().delete(context.Background(), client,
			testSubnetGroupOutput(), fastRetryOptions()...)

		require.NoError(t, err)
		assert.Equal(t, []string{"delete", "delete"}, client.calls)
	})

	t.Run("dependency violation stops at timeout", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		client.deleteErrors = []error{
			&smithy.GenericAPIError{Code: "DependencyViolation", Message: "busy"},
		}

		err := testSubnetGroupResource().delete(context.Background(), client,
			testSubnetGroupOutput(), retry.WithTimeout(0), retry.WithInterval(0))

		assert.Error(t, err)
		assert.Equal(t, []string{"delete"}, client.calls)
	})

	t.Run("in-use fault is immediate", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		sentinel := &elasticachetypes.CacheSubnetGroupInUse{}
		client.deleteErrors = []error{sentinel}

		err := testSubnetGroupResource().delete(context.Background(), client,
			testSubnetGroupOutput(), fastRetryOptions()...)

		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"delete"}, client.calls)
	})

	t.Run("another API code is immediate", func(t *testing.T) {
		client := newFakeSubnetGroupClient()
		sentinel := &smithy.GenericAPIError{Code: "DependencyViolationLater", Message: "bad"}
		client.deleteErrors = []error{sentinel}

		err := testSubnetGroupResource().delete(context.Background(), client,
			testSubnetGroupOutput(), fastRetryOptions()...)

		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"delete"}, client.calls)
	})
}

func testSubnetGroupResource() *SubnetGroupResource {
	tags := map[string]string{"env": "test"}
	return &SubnetGroupResource{
		Name:        "MiXeD-NaMe",
		Description: "description",
		SubnetIds:   []string{"subnet-a", "subnet-b"},
		Tags:        &tags,
	}
}

func testSubnetGroupOutput() *SubnetGroupResourceOutput {
	return &SubnetGroupResourceOutput{
		Name: testSubnetGroupName, Arn: testSubnetGroupArn, VpcId: testVpcID,
	}
}

func testSubnetGroupPrior() runtime.Prior[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg] {
	r := testSubnetGroupResource()
	return runtime.Prior[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg]{
		Inputs: *r, Outputs: testSubnetGroupOutput(), Observed: testSubnetGroupOutput(),
	}
}

func testCacheSubnetGroup() elasticachetypes.CacheSubnetGroup {
	return elasticachetypes.CacheSubnetGroup{
		CacheSubnetGroupName: aws.String(testSubnetGroupName),
		ARN:                  aws.String(testSubnetGroupArn),
		VpcId:                aws.String(testVpcID),
	}
}

func fastRetryOptions() []retry.Option {
	return []retry.Option{
		retry.WithTimeout(time.Second),
		retry.WithInterval(time.Nanosecond),
	}
}

func tagStateError() error {
	return &elasticachetypes.InvalidReplicationGroupStateFault{
		Message: aws.String("replication group is not in available state"),
	}
}

func tagMap(tags []elasticachetypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return out
}

func numberedSubnetGroupTags(count int) map[string]string {
	tags := make(map[string]string, count)
	for index := range count {
		tags[fmt.Sprintf("key-%02d", index)] = fmt.Sprintf("value-%02d", index)
	}
	return tags
}

type fakeSubnetGroupClient struct {
	calls []string

	createInputs  []*elasticache.CreateCacheSubnetGroupInput
	createOutputs []*elasticache.CreateCacheSubnetGroupOutput
	createErrors  []error

	describeInputs  []*elasticache.DescribeCacheSubnetGroupsInput
	describeOutputs []*elasticache.DescribeCacheSubnetGroupsOutput
	describeErrors  []error

	modifyInputs  []*elasticache.ModifyCacheSubnetGroupInput
	modifyOutputs []*elasticache.ModifyCacheSubnetGroupOutput
	modifyErrors  []error

	deleteInputs  []*elasticache.DeleteCacheSubnetGroupInput
	deleteOutputs []*elasticache.DeleteCacheSubnetGroupOutput
	deleteErrors  []error

	listTagsInputs  []*elasticache.ListTagsForResourceInput
	listTagsOutputs []*elasticache.ListTagsForResourceOutput
	listTagsErrors  []error

	addTagsInputs  []*elasticache.AddTagsToResourceInput
	addTagsOutputs []*elasticache.AddTagsToResourceOutput
	addTagsErrors  []error

	removeTagsInputs  []*elasticache.RemoveTagsFromResourceInput
	removeTagsOutputs []*elasticache.RemoveTagsFromResourceOutput
	removeTagsErrors  []error
}

func newFakeSubnetGroupClient() *fakeSubnetGroupClient {
	return &fakeSubnetGroupClient{}
}

func (c *fakeSubnetGroupClient) CreateCacheSubnetGroup(
	_ context.Context,
	in *elasticache.CreateCacheSubnetGroupInput,
	_ ...func(*elasticache.Options),
) (*elasticache.CreateCacheSubnetGroupOutput, error) {
	index := len(c.createInputs)
	c.calls = append(c.calls, "create")
	c.createInputs = append(c.createInputs, in)
	fallback := &elasticache.CreateCacheSubnetGroupOutput{
		CacheSubnetGroup: &elasticachetypes.CacheSubnetGroup{ARN: aws.String(
			testSubnetGroupArn)},
	}
	return valueAt(c.createOutputs, index, fallback), errorAt(c.createErrors, index)
}

func (c *fakeSubnetGroupClient) DescribeCacheSubnetGroups(
	_ context.Context,
	in *elasticache.DescribeCacheSubnetGroupsInput,
	_ ...func(*elasticache.Options),
) (*elasticache.DescribeCacheSubnetGroupsOutput, error) {
	index := len(c.describeInputs)
	c.calls = append(c.calls, "describe")
	copyInput := *in
	c.describeInputs = append(c.describeInputs, &copyInput)
	fallback := &elasticache.DescribeCacheSubnetGroupsOutput{
		CacheSubnetGroups: []elasticachetypes.CacheSubnetGroup{testCacheSubnetGroup()},
	}
	return valueAt(c.describeOutputs, index, fallback), errorAt(c.describeErrors, index)
}

func (c *fakeSubnetGroupClient) ModifyCacheSubnetGroup(
	_ context.Context,
	in *elasticache.ModifyCacheSubnetGroupInput,
	_ ...func(*elasticache.Options),
) (*elasticache.ModifyCacheSubnetGroupOutput, error) {
	index := len(c.modifyInputs)
	c.calls = append(c.calls, "modify")
	c.modifyInputs = append(c.modifyInputs, in)
	return valueAt(c.modifyOutputs, index, &elasticache.ModifyCacheSubnetGroupOutput{}),
		errorAt(c.modifyErrors, index)
}

func (c *fakeSubnetGroupClient) DeleteCacheSubnetGroup(
	_ context.Context,
	in *elasticache.DeleteCacheSubnetGroupInput,
	_ ...func(*elasticache.Options),
) (*elasticache.DeleteCacheSubnetGroupOutput, error) {
	index := len(c.deleteInputs)
	c.calls = append(c.calls, "delete")
	c.deleteInputs = append(c.deleteInputs, in)
	return valueAt(c.deleteOutputs, index, &elasticache.DeleteCacheSubnetGroupOutput{}),
		errorAt(c.deleteErrors, index)
}

func (c *fakeSubnetGroupClient) ListTagsForResource(
	_ context.Context,
	in *elasticache.ListTagsForResourceInput,
	_ ...func(*elasticache.Options),
) (*elasticache.ListTagsForResourceOutput, error) {
	index := len(c.listTagsInputs)
	c.calls = append(c.calls, "list-tags")
	c.listTagsInputs = append(c.listTagsInputs, in)
	return valueAt(c.listTagsOutputs, index, &elasticache.ListTagsForResourceOutput{}),
		errorAt(c.listTagsErrors, index)
}

func (c *fakeSubnetGroupClient) AddTagsToResource(
	_ context.Context,
	in *elasticache.AddTagsToResourceInput,
	_ ...func(*elasticache.Options),
) (*elasticache.AddTagsToResourceOutput, error) {
	index := len(c.addTagsInputs)
	c.calls = append(c.calls, "add-tags")
	c.addTagsInputs = append(c.addTagsInputs, in)
	return valueAt(c.addTagsOutputs, index, &elasticache.AddTagsToResourceOutput{}),
		errorAt(c.addTagsErrors, index)
}

func (c *fakeSubnetGroupClient) RemoveTagsFromResource(
	_ context.Context,
	in *elasticache.RemoveTagsFromResourceInput,
	_ ...func(*elasticache.Options),
) (*elasticache.RemoveTagsFromResourceOutput, error) {
	index := len(c.removeTagsInputs)
	c.calls = append(c.calls, "remove-tags")
	c.removeTagsInputs = append(c.removeTagsInputs, in)
	return valueAt(c.removeTagsOutputs, index, &elasticache.RemoveTagsFromResourceOutput{}),
		errorAt(c.removeTagsErrors, index)
}

func valueAt[T any](values []T, index int, fallback T) T {
	if index < len(values) {
		return values[index]
	}
	return fallback
}

func errorAt(errors []error, index int) error {
	if index < len(errors) {
		return errors[index]
	}
	return nil
}
