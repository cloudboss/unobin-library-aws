package wafv2

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWebACLARN = "arn:aws:wafv2:us-east-1:123456789012:regional/webacl/example/id-1"

type fakeWAFClient struct {
	create func(context.Context, *awssvc.CreateWebACLInput) (*awssvc.CreateWebACLOutput, error)
	delete func(context.Context, *awssvc.DeleteWebACLInput) (*awssvc.DeleteWebACLOutput, error)
	get    func(context.Context, *awssvc.GetWebACLInput) (*awssvc.GetWebACLOutput, error)
	list   func(
		context.Context,
		*awssvc.ListTagsForResourceInput,
	) (*awssvc.ListTagsForResourceOutput, error)
	tag   func(context.Context, *awssvc.TagResourceInput) (*awssvc.TagResourceOutput, error)
	untag func(
		context.Context,
		*awssvc.UntagResourceInput,
	) (*awssvc.UntagResourceOutput, error)
	update func(context.Context, *awssvc.UpdateWebACLInput) (*awssvc.UpdateWebACLOutput, error)
}

func (f *fakeWAFClient) CreateWebACL(
	ctx context.Context,
	in *awssvc.CreateWebACLInput,
	_ ...func(*awssvc.Options),
) (*awssvc.CreateWebACLOutput, error) {
	return f.create(ctx, in)
}

func (f *fakeWAFClient) DeleteWebACL(
	ctx context.Context,
	in *awssvc.DeleteWebACLInput,
	_ ...func(*awssvc.Options),
) (*awssvc.DeleteWebACLOutput, error) {
	return f.delete(ctx, in)
}

func (f *fakeWAFClient) GetWebACL(
	ctx context.Context,
	in *awssvc.GetWebACLInput,
	_ ...func(*awssvc.Options),
) (*awssvc.GetWebACLOutput, error) {
	return f.get(ctx, in)
}

func (f *fakeWAFClient) ListTagsForResource(
	ctx context.Context,
	in *awssvc.ListTagsForResourceInput,
	_ ...func(*awssvc.Options),
) (*awssvc.ListTagsForResourceOutput, error) {
	return f.list(ctx, in)
}

func (f *fakeWAFClient) TagResource(
	ctx context.Context,
	in *awssvc.TagResourceInput,
	_ ...func(*awssvc.Options),
) (*awssvc.TagResourceOutput, error) {
	return f.tag(ctx, in)
}

func (f *fakeWAFClient) UntagResource(
	ctx context.Context,
	in *awssvc.UntagResourceInput,
	_ ...func(*awssvc.Options),
) (*awssvc.UntagResourceOutput, error) {
	return f.untag(ctx, in)
}

func (f *fakeWAFClient) UpdateWebACL(
	ctx context.Context,
	in *awssvc.UpdateWebACLInput,
	_ ...func(*awssvc.Options),
) (*awssvc.UpdateWebACLOutput, error) {
	return f.update(ctx, in)
}

func TestWebACLCreate(t *testing.T) {
	resource := validWebACLResource()
	resource.Tags = &map[string]string{"environment": "test"}

	client := &fakeWAFClient{}
	client.create = func(
		_ context.Context,
		in *awssvc.CreateWebACLInput,
	) (*awssvc.CreateWebACLOutput, error) {
		expected := &awssvc.CreateWebACLInput{
			DefaultAction: &awstypes.DefaultAction{Allow: &awstypes.AllowAction{}},
			Name:          aws.String("example"),
			Scope:         awstypes.ScopeRegional,
			Tags: []awstypes.Tag{{
				Key:   aws.String("environment"),
				Value: aws.String("test"),
			}},
			VisibilityConfig: &awstypes.VisibilityConfig{
				CloudWatchMetricsEnabled: true,
				MetricName:               aws.String("example"),
				SampledRequestsEnabled:   true,
			},
		}
		assert.Equal(t, expected, in)
		return &awssvc.CreateWebACLOutput{
			Summary: &awstypes.WebACLSummary{Id: aws.String("id-1")},
		}, nil
	}
	client.get = successfulWebACLGet

	out, err := resource.create(context.Background(), client, nil)
	require.NoError(t, err)
	assert.Equal(t, &WebACLResourceOutput{
		ARN:            testWebACLARN,
		ID:             "id-1",
		Capacity:       8,
		LabelNamespace: "awswaf:123456789012:webacl:example:",
		LockToken:      "token-1",
	}, out)
}

func TestWebACLReadMapsTypedNotFound(t *testing.T) {
	client := &fakeWAFClient{
		get: func(
			context.Context,
			*awssvc.GetWebACLInput,
		) (*awssvc.GetWebACLOutput, error) {
			return nil, &awstypes.WAFNonexistentItemException{}
		},
	}
	identity, err := parseWebACLARN(testWebACLARN)
	require.NoError(t, err)

	_, err = readWebACL(context.Background(), client, identity, true)
	assert.ErrorIs(t, err, runtime.ErrNotFound)
}

func successfulWebACLGet(
	_ context.Context,
	in *awssvc.GetWebACLInput,
) (*awssvc.GetWebACLOutput, error) {
	if aws.ToString(in.Id) != "id-1" || aws.ToString(in.Name) != "example" ||
		in.Scope != awstypes.ScopeRegional {
		return nil, assert.AnError
	}
	return &awssvc.GetWebACLOutput{
		LockToken: aws.String("token-1"),
		WebACL: &awstypes.WebACL{
			ARN:            aws.String(testWebACLARN),
			Id:             aws.String("id-1"),
			Name:           aws.String("example"),
			Capacity:       8,
			LabelNamespace: aws.String("awswaf:123456789012:webacl:example:"),
		},
	}, nil
}

func validWebACLResource() *WebACLResource {
	name := "example"
	return &WebACLResource{
		Name:  &name,
		Scope: "REGIONAL",
		DefaultAction: WebACLDefaultAction{
			Allow: &WebACLAllowAction{},
		},
		VisibilityConfig: WebACLVisibilityConfig{
			CloudWatchMetricsEnabled: true,
			MetricName:               "example",
			SampledRequestsEnabled:   true,
		},
	}
}
