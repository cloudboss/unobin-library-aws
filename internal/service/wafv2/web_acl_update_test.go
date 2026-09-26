package wafv2

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLUpdateWithoutChangesOnlyReads(t *testing.T) {
	resource := validWebACLResource()
	prior := validWebACLUpdatePrior(*validWebACLResource())
	calls := []string{}
	client := &fakeWAFClient{get: func(
		ctx context.Context,
		in *awssvc.GetWebACLInput,
	) (*awssvc.GetWebACLOutput, error) {
		calls = append(calls, "get")
		return successfulWebACLGet(ctx, in)
	}}

	out, err := resource.update(context.Background(), client, prior)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "id-1", out.ID)
	assert.Equal(t, []string{"get"}, calls)
}

func TestWebACLConfigurationChangedFields(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*WebACLResource)
		expected bool
	}{
		{
			name: "association config",
			mutate: func(resource *WebACLResource) {
				resource.AssociationConfig = &WebACLAssociationConfig{}
			},
			expected: true,
		},
		{
			name: "captcha config",
			mutate: func(resource *WebACLResource) {
				resource.CaptchaConfig = &WebACLCaptchaConfig{}
			},
			expected: true,
		},
		{
			name: "challenge config",
			mutate: func(resource *WebACLResource) {
				resource.ChallengeConfig = &WebACLChallengeConfig{}
			},
			expected: true,
		},
		{
			name: "custom response bodies",
			mutate: func(resource *WebACLResource) {
				bodies := map[string]WebACLCustomResponseBody{}
				resource.CustomResponseBodies = &bodies
			},
			expected: true,
		},
		{
			name: "data protection config",
			mutate: func(resource *WebACLResource) {
				resource.DataProtectionConfig = &WebACLDataProtectionConfig{}
			},
			expected: true,
		},
		{
			name: "on-source DDoS config",
			mutate: func(resource *WebACLResource) {
				resource.OnSourceDDoSConfig = &WebACLOnSourceDDoSConfig{}
			},
			expected: true,
		},
		{
			name: "default action",
			mutate: func(resource *WebACLResource) {
				resource.DefaultAction = WebACLDefaultAction{Block: &WebACLBlockAction{}}
			},
			expected: true,
		},
		{
			name: "visibility config",
			mutate: func(resource *WebACLResource) {
				resource.VisibilityConfig.MetricName = "changed"
			},
			expected: true,
		},
		{
			name: "description",
			mutate: func(resource *WebACLResource) {
				resource.Description = aws.String("changed")
			},
			expected: true,
		},
		{
			name: "rules",
			mutate: func(resource *WebACLResource) {
				rules := []WebACLRule{}
				resource.Rules = &rules
			},
			expected: true,
		},
		{
			name: "token domains",
			mutate: func(resource *WebACLResource) {
				domains := []string{}
				resource.TokenDomains = &domains
			},
			expected: true,
		},
		{
			name: "replacement name excluded",
			mutate: func(resource *WebACLResource) {
				resource.Name = aws.String("replacement")
			},
		},
		{
			name: "replacement scope excluded",
			mutate: func(resource *WebACLResource) {
				resource.Scope = "CLOUDFRONT"
			},
		},
		{
			name: "replacement application config excluded",
			mutate: func(resource *WebACLResource) {
				resource.ApplicationConfig = &WebACLApplicationConfig{}
			},
		},
		{
			name: "tags excluded",
			mutate: func(resource *WebACLResource) {
				tags := map[string]string{"team": "edge"}
				resource.Tags = &tags
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prior := *validWebACLResource()
			current := validWebACLResource()
			tt.mutate(current)
			assert.Equal(t, tt.expected, current.configurationChanged(prior))
		})
	}
}

func TestWebACLUpdateTagOnlyOrderAndInputs(t *testing.T) {
	priorTags := map[string]string{"change": "old", "keep": "same", "old": "gone"}
	desiredTags := map[string]string{
		"z-new":  "last",
		"a-new":  "first",
		"change": "new",
		"keep":   "same",
	}
	resource, prior := webACLUpdateWithTags(&priorTags, &desiredTags)
	script := newWebACLUpdateScript(t, []webACLTagListResult{{
		output: webACLTagListOutput(map[string]string{
			"z-drop":    "last",
			"a-drop":    "first",
			"change":    "old",
			"keep":      "same",
			"aws:owned": "preserved",
		}),
	}})

	out, err := resource.update(context.Background(), script.client(), prior)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, []string{"list", "untag", "tag", "get"}, script.calls)
	require.Len(t, script.listInputs, 1)
	assert.Equal(t, testWebACLARN, aws.ToString(script.listInputs[0].ResourceARN))
	require.Len(t, script.untagInputs, 1)
	assert.Equal(t, &awssvc.UntagResourceInput{
		ResourceARN: aws.String(testWebACLARN),
		TagKeys:     []string{"a-drop", "z-drop"},
	}, script.untagInputs[0])
	require.Len(t, script.tagInputs, 1)
	assert.Equal(t, &awssvc.TagResourceInput{
		ResourceARN: aws.String(testWebACLARN),
		Tags: []awstypes.Tag{
			{Key: aws.String("a-new"), Value: aws.String("first")},
			{Key: aws.String("change"), Value: aws.String("new")},
			{Key: aws.String("z-new"), Value: aws.String("last")},
		},
	}, script.tagInputs[0])
}

func TestWebACLUpdateNilAndEmptyDesiredTagsRemoveUserTags(t *testing.T) {
	empty := map[string]string{}
	tests := []struct {
		name    string
		desired *map[string]string
	}{
		{name: "nil"},
		{name: "empty", desired: &empty},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priorTags := map[string]string{"prior": "value"}
			resource, prior := webACLUpdateWithTags(&priorTags, tt.desired)
			script := newWebACLUpdateScript(t, []webACLTagListResult{{
				output: webACLTagListOutput(map[string]string{
					"z-user":    "last",
					"a-user":    "first",
					"aws:owned": "preserved",
				}),
			}})

			_, err := resource.update(context.Background(), script.client(), prior)
			require.NoError(t, err)
			assert.Equal(t, []string{"list", "untag", "get"}, script.calls)
			require.Len(t, script.untagInputs, 1)
			assert.Equal(t, []string{"a-user", "z-user"}, script.untagInputs[0].TagKeys)
			assert.Empty(t, script.tagInputs)
		})
	}
}

func TestWebACLUpdateNilTagListShapesAreEmpty(t *testing.T) {
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
			priorTags := map[string]string{"prior": "value"}
			resource, prior := webACLUpdateWithTags(&priorTags, nil)
			script := newWebACLUpdateScript(t, []webACLTagListResult{{output: tt.output}})

			_, err := resource.update(context.Background(), script.client(), prior)
			require.NoError(t, err)
			assert.Equal(t, []string{"list", "get"}, script.calls)
			assert.Empty(t, script.untagInputs)
			assert.Empty(t, script.tagInputs)
		})
	}
}

func TestWebACLUpdateRejectsReservedDesiredTagBeforeCalls(t *testing.T) {
	desiredTags := map[string]string{"aws:reserved": "value"}
	resource, prior := webACLUpdateWithTags(nil, &desiredTags)
	script := newWebACLUpdateScript(t, nil)

	out, err := resource.update(context.Background(), script.client(), prior)
	assert.Nil(t, out)
	assert.ErrorContains(t, err, "tag key must not start with aws:")
	assert.Empty(t, script.calls)
}

func TestWebACLUpdateRequiresPriorOutputARNBeforeCalls(t *testing.T) {
	tests := []struct {
		name    string
		outputs *WebACLResourceOutput
		wantErr string
	}{
		{name: "nil output", wantErr: "prior web ACL output has no ARN"},
		{
			name:    "empty ARN",
			outputs: &WebACLResourceOutput{},
			wantErr: "prior web ACL output has no ARN",
		},
		{
			name:    "invalid ARN",
			outputs: &WebACLResourceOutput{ARN: "invalid"},
			wantErr: "parse web ACL ARN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validWebACLResource()
			prior := validWebACLUpdatePrior(*validWebACLResource())
			prior.Outputs = tt.outputs
			script := newWebACLUpdateScript(t, nil)

			out, err := resource.update(context.Background(), script.client(), prior)
			assert.Nil(t, out)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, script.calls)
		})
	}
}

func TestWebACLUpdateErrorsShortCircuit(t *testing.T) {
	listFailure := errors.New("list failed")
	untagFailure := errors.New("untag failed")
	tagFailure := errors.New("tag failed")
	readFailure := errors.New("read failed")
	tests := []struct {
		name        string
		configure   func(*webACLUpdateScript)
		priorTags   *map[string]string
		desiredTags *map[string]string
		wantCalls   []string
		wantErr     error
	}{
		{
			name: "list",
			configure: func(script *webACLUpdateScript) {
				script.listResults = []webACLTagListResult{{err: listFailure}}
			},
			priorTags:   stringMapPointer("old", "value"),
			desiredTags: stringMapPointer("new", "value"),
			wantCalls:   []string{"list"},
			wantErr:     listFailure,
		},
		{
			name: "untag",
			configure: func(script *webACLUpdateScript) {
				script.listResults = []webACLTagListResult{{
					output: webACLTagListOutput(map[string]string{"drop": "value"}),
				}}
				script.untagErr = untagFailure
			},
			priorTags:   stringMapPointer("old", "value"),
			desiredTags: stringMapPointer("new", "value"),
			wantCalls:   []string{"list", "untag"},
			wantErr:     untagFailure,
		},
		{
			name: "tag",
			configure: func(script *webACLUpdateScript) {
				script.listResults = []webACLTagListResult{{
					output: webACLTagListOutput(map[string]string{"drop": "value"}),
				}}
				script.tagErr = tagFailure
			},
			priorTags:   stringMapPointer("old", "value"),
			desiredTags: stringMapPointer("new", "value"),
			wantCalls:   []string{"list", "untag", "tag"},
			wantErr:     tagFailure,
		},
		{
			name: "final read",
			configure: func(script *webACLUpdateScript) {
				script.getErr = readFailure
			},
			wantCalls: []string{"get"},
			wantErr:   readFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, prior := webACLUpdateWithTags(tt.priorTags, tt.desiredTags)
			script := newWebACLUpdateScript(t, nil)
			tt.configure(script)

			out, err := resource.update(context.Background(), script.client(), prior)
			assert.Nil(t, out)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantCalls, script.calls)
		})
	}
}

type webACLTagListResult struct {
	output *awssvc.ListTagsForResourceOutput
	err    error
}

type webACLUpdateScript struct {
	t           *testing.T
	listResults []webACLTagListResult
	listInputs  []*awssvc.ListTagsForResourceInput
	untagInputs []*awssvc.UntagResourceInput
	tagInputs   []*awssvc.TagResourceInput
	calls       []string
	untagErr    error
	tagErr      error
	getErr      error
}

func newWebACLUpdateScript(
	t *testing.T,
	listResults []webACLTagListResult,
) *webACLUpdateScript {
	return &webACLUpdateScript{t: t, listResults: listResults}
}

func (s *webACLUpdateScript) client() *fakeWAFClient {
	return &fakeWAFClient{
		list: func(
			_ context.Context,
			in *awssvc.ListTagsForResourceInput,
		) (*awssvc.ListTagsForResourceOutput, error) {
			index := len(s.listInputs)
			require.Less(s.t, index, len(s.listResults), "unexpected list tags call")
			copyInput := *in
			s.listInputs = append(s.listInputs, &copyInput)
			s.calls = append(s.calls, "list")
			return s.listResults[index].output, s.listResults[index].err
		},
		untag: func(
			_ context.Context,
			in *awssvc.UntagResourceInput,
		) (*awssvc.UntagResourceOutput, error) {
			copyInput := *in
			copyInput.TagKeys = append([]string(nil), in.TagKeys...)
			s.untagInputs = append(s.untagInputs, &copyInput)
			s.calls = append(s.calls, "untag")
			return &awssvc.UntagResourceOutput{}, s.untagErr
		},
		tag: func(
			_ context.Context,
			in *awssvc.TagResourceInput,
		) (*awssvc.TagResourceOutput, error) {
			copyInput := *in
			copyInput.Tags = append([]awstypes.Tag(nil), in.Tags...)
			s.tagInputs = append(s.tagInputs, &copyInput)
			s.calls = append(s.calls, "tag")
			return &awssvc.TagResourceOutput{}, s.tagErr
		},
		get: func(
			ctx context.Context,
			in *awssvc.GetWebACLInput,
		) (*awssvc.GetWebACLOutput, error) {
			s.calls = append(s.calls, "get")
			if s.getErr != nil {
				return nil, s.getErr
			}
			return successfulWebACLGet(ctx, in)
		},
		update: func(
			context.Context,
			*awssvc.UpdateWebACLInput,
		) (*awssvc.UpdateWebACLOutput, error) {
			s.t.Fatal("unexpected UpdateWebACL call")
			return nil, nil
		},
	}
}

func webACLUpdateWithTags(
	priorTags *map[string]string,
	desiredTags *map[string]string,
) (*WebACLResource, runtime.Prior[WebACLResource, *WebACLResourceOutput, *awsCfg]) {
	resource := validWebACLResource()
	resource.Tags = desiredTags
	priorInputs := *validWebACLResource()
	priorInputs.Tags = priorTags
	return resource, validWebACLUpdatePrior(priorInputs)
}

func webACLTagListOutput(tags map[string]string) *awssvc.ListTagsForResourceOutput {
	list := make([]awstypes.Tag, 0, len(tags))
	for key, value := range tags {
		list = append(list, awstypes.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return &awssvc.ListTagsForResourceOutput{
		TagInfoForResource: &awstypes.TagInfoForResource{TagList: list},
	}
}

func stringMapPointer(key, value string) *map[string]string {
	tags := map[string]string{key: value}
	return &tags
}

func validWebACLUpdatePrior(
	inputs WebACLResource,
) runtime.Prior[WebACLResource, *WebACLResourceOutput, *awsCfg] {
	return runtime.Prior[WebACLResource, *WebACLResourceOutput, *awsCfg]{
		Inputs: inputs,
		Outputs: &WebACLResourceOutput{
			ARN:       testWebACLARN,
			ID:        "ignored-id",
			LockToken: "token-1",
		},
	}
}
