package cloudwatch

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatch "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dashboardName = "unobin-dashboard"
	dashboardArn  = "arn:aws:cloudwatch::123456789012:dashboard/unobin-dashboard"
)

func TestDashboardValidateInputs(t *testing.T) {
	valid := DashboardResource{Name: dashboardName, Body: `{"widgets":[]}`}
	tests := []struct {
		name    string
		mutate  func(*DashboardResource)
		wantErr string
	}{
		{name: "valid"},
		{name: "one character name", mutate: func(r *DashboardResource) { r.Name = "a" }},
		{
			name:   "255 character name",
			mutate: func(r *DashboardResource) { r.Name = strings.Repeat("a", 255) },
		},
		{name: "empty body reaches AWS", mutate: func(r *DashboardResource) { r.Body = "" }},
		{
			name: "one-character tag key",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"k": "value"}
			},
		},
		{
			name: "50 tags",
			mutate: func(r *DashboardResource) {
				tags := numberedDashboardTags(50)
				r.Tags = &tags
			},
		},
		{
			name: "multibyte tag length boundaries",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{
					strings.Repeat("界", 128): strings.Repeat("界", 256),
				}
			},
		},
		{
			name: "empty tag value",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"key": ""}
			},
		},
		{
			name: "permitted tag characters",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{
					"Key _.:/=+-@界9": "Value _.:/=+-@界9",
				}
			},
		},
		{
			name: "case-distinct tag keys",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"Key": "upper", "key": "lower"}
			},
		},
		{
			name: "aws prefix in tag value",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"key": "aws:managed"}
			},
		},
		{
			name:    "empty name",
			mutate:  func(r *DashboardResource) { r.Name = "" },
			wantErr: "name must be 1 to 255 ASCII characters",
		},
		{
			name:    "long name",
			mutate:  func(r *DashboardResource) { r.Name = strings.Repeat("a", 256) },
			wantErr: "name must be 1 to 255 ASCII characters",
		},
		{
			name:    "name with whitespace",
			mutate:  func(r *DashboardResource) { r.Name = "two words" },
			wantErr: "name must contain only",
		},
		{
			name:    "name with punctuation",
			mutate:  func(r *DashboardResource) { r.Name = "bad.name" },
			wantErr: "name must contain only",
		},
		{
			name:    "non-ASCII name",
			mutate:  func(r *DashboardResource) { r.Name = "dáshboard" },
			wantErr: "name must contain only",
		},
		{
			name:    "malformed body",
			mutate:  func(r *DashboardResource) { r.Body = `{"widgets":` },
			wantErr: "body must be valid JSON",
		},
		{
			name: "too many tags",
			mutate: func(r *DashboardResource) {
				tags := numberedDashboardTags(51)
				r.Tags = &tags
			},
			wantErr: "tags must have at most 50 entries",
		},
		{
			name: "empty tag key",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"": "value"}
			},
			wantErr: "tag key must be 1 to 128 characters",
		},
		{
			name: "long tag key",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{strings.Repeat("界", 129): "value"}
			},
			wantErr: "tag key must be 1 to 128 characters",
		},
		{
			name: "long tag value",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"key": strings.Repeat("界", 257)}
			},
			wantErr: "tag value must be at most 256 characters",
		},
		{
			name: "reserved tag key",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"aws:managed": "value"}
			},
			wantErr: "tag key must not begin with aws:",
		},
		{
			name: "control whitespace in tag key",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"bad\tkey": "value"}
			},
			wantErr: "tag key contains invalid characters",
		},
		{
			name: "control whitespace in tag value",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"key": "bad\nvalue"}
			},
			wantErr: "tag value contains invalid characters",
		},
		{
			name: "exclamation mark in tag key",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"bad!key": "value"}
			},
			wantErr: "tag key contains invalid characters",
		},
		{
			name: "hash mark in tag value",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"key": "bad#value"}
			},
			wantErr: "tag value contains invalid characters",
		},
		{
			name: "emoji in tag key",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"bad😀key": "value"}
			},
			wantErr: "tag key contains invalid characters",
		},
		{
			name: "emoji in tag value",
			mutate: func(r *DashboardResource) {
				r.Tags = &map[string]string{"key": "bad😀value"}
			},
			wantErr: "tag value contains invalid characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			err := r.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestDashboardEquivalentInput(t *testing.T) {
	prior := DashboardResource{Body: `{"widgets":[{"x":1,"y":2}]}`}
	tests := []struct {
		name  string
		field string
		body  string
		equal bool
	}{
		{
			name:  "whitespace and key order",
			field: "body",
			body:  "{\n  \"widgets\": [{\"y\": 2, \"x\": 1}]\n}",
			equal: true,
		},
		{
			name:  "equivalent number",
			field: "body",
			body:  `{"widgets":[{"x":1.0,"y":2e0}]}`,
			equal: true,
		},
		{
			name:  "different JSON value",
			field: "body",
			body:  `{"widgets":[{"x":2,"y":2}]}`,
			equal: false,
		},
		{name: "invalid JSON", field: "body", body: `{`, equal: false},
		{name: "trailing JSON", field: "body", body: prior.Body + ` {}`, equal: false},
		{name: "other field", field: "name", body: prior.Body, equal: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := prior
			current.Body = tt.body
			r := &current
			assert.Equal(t, tt.equal, r.EquivalentInput(tt.field, prior, current))
		})
	}
}

func TestDashboardReplaceFields(t *testing.T) {
	assert.Equal(t, []string{"name"}, (&DashboardResource{}).ReplaceFields())
}

func TestDashboardCreate(t *testing.T) {
	t.Run("put then read", func(t *testing.T) {
		client := newFakeDashboardClient()
		r := &DashboardResource{Name: dashboardName, Body: `{"widgets":[]}`}

		out, err := r.create(context.Background(), client)

		require.NoError(t, err)
		assert.Equal(t, &DashboardResourceOutput{Arn: dashboardArn, Name: dashboardName}, out)
		assert.Equal(t, []string{"put", "get"}, client.calls)
		require.Len(t, client.putInputs, 1)
		assert.Equal(t, dashboardName, aws.ToString(client.putInputs[0].DashboardName))
		assert.Equal(t, r.Body, aws.ToString(client.putInputs[0].DashboardBody))
	})

	t.Run("tags after ARN discovery", func(t *testing.T) {
		client := newFakeDashboardClient()
		tags := map[string]string{"team": "core", "env": "test"}
		r := &DashboardResource{Name: dashboardName, Body: `{}`, Tags: &tags}

		out, err := r.create(context.Background(), client)

		require.NoError(t, err)
		assert.Equal(t, dashboardArn, out.Arn)
		assert.Equal(t, []string{"put", "get", "tag"}, client.calls)
		require.Len(t, client.tagInputs, 1)
		assert.Equal(t, dashboardArn, aws.ToString(client.tagInputs[0].ResourceARN))
		assert.Equal(t, []cloudwatchtypes.Tag{
			{Key: aws.String("env"), Value: aws.String("test")},
			{Key: aws.String("team"), Value: aws.String("core")},
		}, client.tagInputs[0].Tags)
	})

	t.Run("empty values and case-sensitive keys are preserved", func(t *testing.T) {
		client := newFakeDashboardClient()
		tags := map[string]string{"Key": "", "key": "aws:value"}
		r := &DashboardResource{Name: dashboardName, Body: `{}`, Tags: &tags}

		out, err := r.create(context.Background(), client)

		require.NoError(t, err)
		assert.Equal(t, dashboardArn, out.Arn)
		assert.Equal(t, []string{"put", "get", "tag"}, client.calls)
		require.Len(t, client.tagInputs, 1)
		assert.Equal(t, []cloudwatchtypes.Tag{
			{Key: aws.String("Key"), Value: aws.String("")},
			{Key: aws.String("key"), Value: aws.String("aws:value")},
		}, client.tagInputs[0].Tags)
	})

	t.Run("invalid tags stop before AWS calls", func(t *testing.T) {
		tests := []struct {
			name string
			tags map[string]string
		}{
			{name: "reserved key", tags: map[string]string{"aws:managed": "value"}},
			{name: "invalid character", tags: map[string]string{"bad!key": "value"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client := newFakeDashboardClient()
				r := &DashboardResource{Name: dashboardName, Body: `{}`, Tags: &tt.tags}

				out, err := r.create(context.Background(), client)

				assert.Nil(t, out)
				assert.Error(t, err)
				assert.Empty(t, client.calls)
			})
		}
	})

	t.Run("empty body is sent", func(t *testing.T) {
		client := newFakeDashboardClient()
		r := &DashboardResource{Name: dashboardName, Body: ""}

		_, err := r.create(context.Background(), client)

		require.NoError(t, err)
		require.Len(t, client.putInputs, 1)
		assert.Equal(t, "", aws.ToString(client.putInputs[0].DashboardBody))
	})

	t.Run("non-error validation messages still succeed", func(t *testing.T) {
		client := newFakeDashboardClient()
		client.putOutput = &cloudwatch.PutDashboardOutput{
			DashboardValidationMessages: []cloudwatchtypes.DashboardValidationMessage{
				{Message: aws.String("widget warning")},
			},
		}
		r := &DashboardResource{Name: dashboardName, Body: `{}`}

		out, err := r.create(context.Background(), client)

		require.NoError(t, err)
		assert.Equal(t, dashboardName, out.Name)
		assert.Equal(t, []string{"put", "get"}, client.calls)
	})

	t.Run("put error stops immediately", func(t *testing.T) {
		sentinel := errors.New("put failed")
		client := newFakeDashboardClient()
		client.putErr = sentinel
		r := &DashboardResource{Name: dashboardName, Body: `{}`}

		out, err := r.create(context.Background(), client)

		assert.Nil(t, out)
		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"put"}, client.calls)
	})

	t.Run("read error does not retry or clean up", func(t *testing.T) {
		sentinel := errors.New("get failed")
		client := newFakeDashboardClient()
		client.getErr = sentinel
		r := &DashboardResource{Name: dashboardName, Body: `{}`}

		out, err := r.create(context.Background(), client)

		assert.Nil(t, out)
		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"put", "get"}, client.calls)
	})
}

func TestDashboardRead(t *testing.T) {
	t.Run("returns identity outputs", func(t *testing.T) {
		client := newFakeDashboardClient()
		r := &DashboardResource{Name: "new-name"}

		out, err := r.read(context.Background(), client, "old-name")

		require.NoError(t, err)
		assert.Equal(t, &DashboardResourceOutput{Arn: dashboardArn, Name: dashboardName}, out)
		require.Len(t, client.getInputs, 1)
		assert.Equal(t, "old-name", aws.ToString(client.getInputs[0].DashboardName))
	})

	t.Run("exact ResourceNotFound maps to runtime not found", func(t *testing.T) {
		client := newFakeDashboardClient()
		client.getErr = &cloudwatchtypes.ResourceNotFound{Message: aws.String("gone")}

		out, err := (&DashboardResource{}).read(context.Background(), client, dashboardName)

		assert.Nil(t, out)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
		assert.Equal(t, []string{"get"}, client.calls)
	})

	t.Run("different API code propagates", func(t *testing.T) {
		sentinel := &smithy.GenericAPIError{Code: "InternalServiceError", Message: "failed"}
		client := newFakeDashboardClient()
		client.getErr = sentinel

		out, err := (&DashboardResource{}).read(context.Background(), client, dashboardName)

		assert.Nil(t, out)
		assert.ErrorIs(t, err, sentinel)
		assert.NotErrorIs(t, err, runtime.ErrNotFound)
	})

	t.Run("nil output is not found", func(t *testing.T) {
		client := newFakeDashboardClient()
		client.getOutput = nil

		out, err := (&DashboardResource{}).read(context.Background(), client, dashboardName)

		assert.Nil(t, out)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	})
}

func TestDashboardUpdate(t *testing.T) {
	prior := DashboardResource{
		Name: dashboardName,
		Body: `{"widgets":[{"x":1,"y":2}]}`,
		Tags: &map[string]string{"env": "old", "remove": "yes"},
	}
	priorOutput := &DashboardResourceOutput{Arn: dashboardArn, Name: dashboardName}
	fiftyTags := numberedDashboardTags(50)

	tests := []struct {
		name        string
		current     DashboardResource
		currentTags map[string]string
		wantCalls   []string
		wantTag     map[string]string
		wantUntag   []string
	}{
		{
			name:        "no change",
			current:     prior,
			currentTags: map[string]string{"env": "old", "remove": "yes"},
			wantCalls:   []string{"get"},
		},
		{
			name: "body change",
			current: DashboardResource{
				Name: dashboardName,
				Body: `{"widgets":[{"x":2,"y":2}]}`,
				Tags: prior.Tags,
			},
			wantCalls: []string{"put", "get"},
		},
		{
			name: "semantic body change only",
			current: DashboardResource{
				Name: dashboardName,
				Body: "{\n\"widgets\":[{\"y\":2.0,\"x\":1.0}]}",
				Tags: prior.Tags,
			},
			wantCalls: []string{"get"},
		},
		{
			name: "tag add overwrite and remove",
			current: DashboardResource{
				Name: dashboardName,
				Body: prior.Body,
				Tags: &map[string]string{"env": "new", "add": "yes"},
			},
			currentTags: map[string]string{"env": "old", "remove": "yes"},
			wantCalls:   []string{"list-tags", "untag", "tag", "get"},
			wantTag:     map[string]string{"env": "new", "add": "yes"},
			wantUntag:   []string{"remove"},
		},
		{
			name: "empty-value add",
			current: DashboardResource{
				Name: dashboardName,
				Body: prior.Body,
				Tags: &map[string]string{"env": "old", "remove": "yes", "add": ""},
			},
			currentTags: map[string]string{"env": "old", "remove": "yes"},
			wantCalls:   []string{"list-tags", "tag", "get"},
			wantTag:     map[string]string{"add": ""},
		},
		{
			name: "overwrite to empty value",
			current: DashboardResource{
				Name: dashboardName,
				Body: prior.Body,
				Tags: &map[string]string{"env": "", "remove": "yes"},
			},
			currentTags: map[string]string{"env": "old", "remove": "yes"},
			wantCalls:   []string{"list-tags", "tag", "get"},
			wantTag:     map[string]string{"env": ""},
		},
		{
			name: "semantic body plus tag change",
			current: DashboardResource{
				Name: dashboardName,
				Body: "{ \"widgets\": [ { \"y\": 2, \"x\": 1 } ] }",
				Tags: &map[string]string{"env": "new", "remove": "yes"},
			},
			currentTags: map[string]string{"env": "old", "remove": "yes"},
			wantCalls:   []string{"list-tags", "tag", "get"},
			wantTag:     map[string]string{"env": "new"},
		},
		{
			name: "combined body and tag change",
			current: DashboardResource{
				Name: dashboardName,
				Body: `{"widgets":[]}`,
				Tags: &map[string]string{"env": "new"},
			},
			currentTags: map[string]string{"env": "old", "remove": "yes"},
			wantCalls:   []string{"put", "list-tags", "untag", "tag", "get"},
			wantTag:     map[string]string{"env": "new"},
			wantUntag:   []string{"remove"},
		},
		{
			name: "clear all tags",
			current: DashboardResource{
				Name: dashboardName,
				Body: prior.Body,
				Tags: nil,
			},
			currentTags: map[string]string{"env": "old", "remove": "yes"},
			wantCalls:   []string{"list-tags", "untag", "get"},
			wantUntag:   []string{"env", "remove"},
		},
		{
			name: "system tags are protected while user tags clear",
			current: DashboardResource{
				Name: dashboardName,
				Body: prior.Body,
				Tags: nil,
			},
			currentTags: map[string]string{
				"aws:owner": "system",
				"env":       "old",
				"remove":    "yes",
			},
			wantCalls: []string{"list-tags", "untag", "get"},
			wantUntag: []string{"env", "remove"},
		},
		{
			name: "50 user tags do not count system tags",
			current: DashboardResource{
				Name: dashboardName,
				Body: prior.Body,
				Tags: &fiftyTags,
			},
			currentTags: map[string]string{
				"aws:owner":  "system",
				"aws:source": "service",
			},
			wantCalls: []string{"list-tags", "tag", "get"},
			wantTag:   fiftyTags,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeDashboardClient()
			client.listTagsOutput = &cloudwatch.ListTagsForResourceOutput{
				Tags: fakeDashboardTags(tt.currentTags),
			}
			out, err := tt.current.update(context.Background(), client,
				runtime.Prior[DashboardResource, *DashboardResourceOutput]{
					Inputs:  prior,
					Outputs: priorOutput,
				})

			require.NoError(t, err)
			assert.Equal(t, &DashboardResourceOutput{
				Arn: dashboardArn, Name: dashboardName,
			}, out)
			assert.Equal(t, tt.wantCalls, client.calls)
			if tt.wantTag != nil {
				require.Len(t, client.tagInputs, 1)
				assert.Equal(t, tt.wantTag, dashboardTagMap(client.tagInputs[0].Tags))
			} else {
				assert.Empty(t, client.tagInputs)
			}
			if tt.wantUntag != nil {
				require.Len(t, client.untagInputs, 1)
				assert.Equal(t, tt.wantUntag, client.untagInputs[0].TagKeys)
			} else {
				assert.Empty(t, client.untagInputs)
			}
		})
	}

	t.Run("put failure stops after one call", func(t *testing.T) {
		sentinel := errors.New("put failed")
		client := newFakeDashboardClient()
		client.putErr = sentinel
		current := prior
		current.Body = `{"widgets":[]}`

		out, err := current.update(context.Background(), client,
			runtime.Prior[DashboardResource, *DashboardResourceOutput]{
				Inputs: prior, Outputs: priorOutput,
			})

		assert.Nil(t, out)
		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"put"}, client.calls)
	})

	t.Run("invalid tags stop before body or tag mutations", func(t *testing.T) {
		client := newFakeDashboardClient()
		current := prior
		current.Body = `{"widgets":[]}`
		current.Tags = &map[string]string{"aws:managed": "value"}

		out, err := current.update(context.Background(), client,
			runtime.Prior[DashboardResource, *DashboardResourceOutput]{
				Inputs: prior, Outputs: priorOutput,
			})

		assert.Nil(t, out)
		assert.Error(t, err)
		assert.Empty(t, client.calls)
	})

	t.Run("tag failures stop without a retry", func(t *testing.T) {
		tests := []struct {
			name      string
			configure func(*fakeDashboardClient)
			current   map[string]string
			wantCalls []string
		}{
			{
				name: "list",
				configure: func(client *fakeDashboardClient) {
					client.listTagsErr = errors.New("list failed")
				},
				current:   map[string]string{"remove": "yes"},
				wantCalls: []string{"list-tags"},
			},
			{
				name: "untag",
				configure: func(client *fakeDashboardClient) {
					client.untagErr = errors.New("untag failed")
				},
				current:   map[string]string{"remove": "yes"},
				wantCalls: []string{"list-tags", "untag"},
			},
			{
				name: "tag",
				configure: func(client *fakeDashboardClient) {
					client.tagErr = errors.New("tag failed")
				},
				current:   map[string]string{},
				wantCalls: []string{"list-tags", "tag"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client := newFakeDashboardClient()
				client.listTagsOutput = &cloudwatch.ListTagsForResourceOutput{
					Tags: fakeDashboardTags(tt.current),
				}
				tt.configure(client)
				current := prior
				current.Tags = &map[string]string{"add": "yes"}

				out, err := current.update(context.Background(), client,
					runtime.Prior[DashboardResource, *DashboardResourceOutput]{
						Inputs: prior, Outputs: priorOutput,
					})

				assert.Nil(t, out)
				assert.Error(t, err)
				assert.Equal(t, tt.wantCalls, client.calls)
			})
		}
	})
}

func TestDashboardDelete(t *testing.T) {
	t.Run("uses prior name and accepts nil output", func(t *testing.T) {
		client := newFakeDashboardClient()
		client.deleteOutput = nil
		r := &DashboardResource{Name: "new-name"}

		err := r.delete(context.Background(), client,
			&DashboardResourceOutput{Name: "old-name"})

		require.NoError(t, err)
		assert.Equal(t, []string{"delete"}, client.calls)
		require.Len(t, client.deleteInputs, 1)
		assert.Equal(t, []string{"old-name"}, client.deleteInputs[0].DashboardNames)
	})

	t.Run("propagates one delete error", func(t *testing.T) {
		sentinel := errors.New("delete failed")
		client := newFakeDashboardClient()
		client.deleteErr = sentinel

		err := (&DashboardResource{Name: dashboardName}).delete(
			context.Background(), client, &DashboardResourceOutput{Name: dashboardName})

		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"delete"}, client.calls)
	})
}

type fakeDashboardClient struct {
	calls          []string
	putInputs      []*cloudwatch.PutDashboardInput
	getInputs      []*cloudwatch.GetDashboardInput
	deleteInputs   []*cloudwatch.DeleteDashboardsInput
	listTagsInputs []*cloudwatch.ListTagsForResourceInput
	tagInputs      []*cloudwatch.TagResourceInput
	untagInputs    []*cloudwatch.UntagResourceInput

	putOutput      *cloudwatch.PutDashboardOutput
	putErr         error
	getOutput      *cloudwatch.GetDashboardOutput
	getErr         error
	deleteOutput   *cloudwatch.DeleteDashboardsOutput
	deleteErr      error
	listTagsOutput *cloudwatch.ListTagsForResourceOutput
	listTagsErr    error
	tagErr         error
	untagErr       error
}

func newFakeDashboardClient() *fakeDashboardClient {
	return &fakeDashboardClient{
		putOutput: &cloudwatch.PutDashboardOutput{},
		getOutput: &cloudwatch.GetDashboardOutput{
			DashboardArn:  aws.String(dashboardArn),
			DashboardName: aws.String(dashboardName),
			DashboardBody: aws.String(`{"widgets":[]}`),
		},
		deleteOutput:   &cloudwatch.DeleteDashboardsOutput{},
		listTagsOutput: &cloudwatch.ListTagsForResourceOutput{},
	}
}

func (c *fakeDashboardClient) PutDashboard(
	_ context.Context,
	in *cloudwatch.PutDashboardInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.PutDashboardOutput, error) {
	c.calls = append(c.calls, "put")
	c.putInputs = append(c.putInputs, in)
	return c.putOutput, c.putErr
}

func (c *fakeDashboardClient) GetDashboard(
	_ context.Context,
	in *cloudwatch.GetDashboardInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.GetDashboardOutput, error) {
	c.calls = append(c.calls, "get")
	c.getInputs = append(c.getInputs, in)
	return c.getOutput, c.getErr
}

func (c *fakeDashboardClient) DeleteDashboards(
	_ context.Context,
	in *cloudwatch.DeleteDashboardsInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.DeleteDashboardsOutput, error) {
	c.calls = append(c.calls, "delete")
	c.deleteInputs = append(c.deleteInputs, in)
	return c.deleteOutput, c.deleteErr
}

func (c *fakeDashboardClient) ListTagsForResource(
	_ context.Context,
	in *cloudwatch.ListTagsForResourceInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.ListTagsForResourceOutput, error) {
	c.calls = append(c.calls, "list-tags")
	c.listTagsInputs = append(c.listTagsInputs, in)
	return c.listTagsOutput, c.listTagsErr
}

func (c *fakeDashboardClient) TagResource(
	_ context.Context,
	in *cloudwatch.TagResourceInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.TagResourceOutput, error) {
	c.calls = append(c.calls, "tag")
	c.tagInputs = append(c.tagInputs, in)
	return &cloudwatch.TagResourceOutput{}, c.tagErr
}

func (c *fakeDashboardClient) UntagResource(
	_ context.Context,
	in *cloudwatch.UntagResourceInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.UntagResourceOutput, error) {
	c.calls = append(c.calls, "untag")
	c.untagInputs = append(c.untagInputs, in)
	return &cloudwatch.UntagResourceOutput{}, c.untagErr
}

func fakeDashboardTags(tags map[string]string) []cloudwatchtypes.Tag {
	out := make([]cloudwatchtypes.Tag, 0, len(tags))
	for key, value := range tags {
		out = append(out, cloudwatchtypes.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return out
}

func dashboardTagMap(tags []cloudwatchtypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return out
}

func numberedDashboardTags(count int) map[string]string {
	out := make(map[string]string, count)
	for i := range count {
		out["tag-"+strconv.Itoa(i)] = "value"
	}
	return out
}
