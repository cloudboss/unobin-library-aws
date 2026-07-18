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

func TestWebACLUpdateConfigurationAndTagsOrder(t *testing.T) {
	t.Run("direct configuration success", func(t *testing.T) {
		resource, prior, expectedUpdate := webACLInitialRetryFixture(t)
		setWebACLConfigTagValues(resource, &prior.Inputs)
		script := newWebACLConfigTagsScript(t)
		script.updateErrors = []error{nil}
		script.getOutputs = []*awssvc.GetWebACLOutput{
			shieldSnapshot("token-final", nil),
		}
		script.remoteTags = map[string]string{"keep": "same"}

		out, err := resource.update(
			context.Background(),
			script.client(),
			prior,
			fastWebACLUpdateRetryOptions()...,
		)
		require.NoError(t, err)
		require.NotNil(t, out)
		assert.Equal(t, []string{
			"update token-1",
			"list",
			"tag",
			"get final state",
		}, script.calls)
		assert.Equal(t, []*awssvc.UpdateWebACLInput{expectedUpdate}, script.updateInputs)
		assert.Equal(t, []*awssvc.ListTagsForResourceInput{{
			ResourceARN: aws.String(testWebACLARN),
		}}, script.listInputs)
		assert.Empty(t, script.untagInputs)
		assert.Equal(t, []*awssvc.TagResourceInput{{
			ResourceARN: aws.String(testWebACLARN),
			Tags: []awstypes.Tag{
				{Key: aws.String("a-new"), Value: aws.String("first")},
				{Key: aws.String("change"), Value: aws.String("new")},
				{Key: aws.String("z-new"), Value: aws.String("last")},
			},
		}}, script.tagInputs)
	})

	t.Run("conflict refresh success", func(t *testing.T) {
		resource, prior, expectedFirst := webACLInitialRetryFixture(t)
		setWebACLConfigTagValues(resource, &prior.Inputs)
		expectedSecond := cloneWebACLUpdateInput(expectedFirst)
		expectedSecond.LockToken = aws.String("token-2")
		script := newWebACLConfigTagsScript(t)
		script.updateErrors = []error{&awstypes.WAFOptimisticLockException{}, nil}
		script.getOutputs = []*awssvc.GetWebACLOutput{
			shieldSnapshot("token-2", nil),
			shieldSnapshot("token-final", nil),
		}
		script.remoteTags = map[string]string{
			"a-drop": "first",
			"change": "old",
			"keep":   "same",
			"z-drop": "last",
		}

		out, err := resource.update(
			context.Background(),
			script.client(),
			prior,
			fastWebACLUpdateRetryOptions()...,
		)
		require.NoError(t, err)
		require.NotNil(t, out)
		assert.Equal(t, []string{
			"update token-1",
			"get refresh",
			"update token-2",
			"list",
			"untag",
			"tag",
			"get final state",
		}, script.calls)
		assert.Equal(t, []*awssvc.UpdateWebACLInput{
			expectedFirst,
			expectedSecond,
		}, script.updateInputs)
		assert.Equal(t, []*awssvc.ListTagsForResourceInput{{
			ResourceARN: aws.String(testWebACLARN),
		}}, script.listInputs)
		assert.Equal(t, []*awssvc.UntagResourceInput{{
			ResourceARN: aws.String(testWebACLARN),
			TagKeys:     []string{"a-drop", "z-drop"},
		}}, script.untagInputs)
		assert.Equal(t, []*awssvc.TagResourceInput{{
			ResourceARN: aws.String(testWebACLARN),
			Tags: []awstypes.Tag{
				{Key: aws.String("a-new"), Value: aws.String("first")},
				{Key: aws.String("change"), Value: aws.String("new")},
				{Key: aws.String("z-new"), Value: aws.String("last")},
			},
		}}, script.tagInputs)
	})
}

func TestWebACLUpdateConfigurationTagStageErrorsStop(t *testing.T) {
	listFailure := errors.New("list failed")
	untagFailure := errors.New("untag failed")
	tagFailure := errors.New("tag failed")
	tests := []struct {
		name      string
		listErr   error
		untagErr  error
		tagErr    error
		wantErr   error
		wantCalls []string
	}{
		{
			name:      "list",
			listErr:   listFailure,
			wantErr:   listFailure,
			wantCalls: []string{"update token-1", "list"},
		},
		{
			name:      "untag",
			untagErr:  untagFailure,
			wantErr:   untagFailure,
			wantCalls: []string{"update token-1", "list", "untag"},
		},
		{
			name:    "tag",
			tagErr:  tagFailure,
			wantErr: tagFailure,
			wantCalls: []string{
				"update token-1",
				"list",
				"untag",
				"tag",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, prior, expectedUpdate := webACLInitialRetryFixture(t)
			setWebACLConfigTagValues(resource, &prior.Inputs)
			script := newWebACLConfigTagsScript(t)
			script.updateErrors = []error{nil}
			script.remoteTags = map[string]string{"drop": "value"}
			script.listErr = tt.listErr
			script.untagErr = tt.untagErr
			script.tagErr = tt.tagErr

			out, err := resource.update(
				context.Background(),
				script.client(),
				prior,
				fastWebACLUpdateRetryOptions()...,
			)
			assert.Nil(t, out)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantCalls, script.calls)
			assert.Equal(t, []*awssvc.UpdateWebACLInput{expectedUpdate}, script.updateInputs)
			assert.Empty(t, script.getInputs)
		})
	}
}

type webACLConfigTagsScript struct {
	t            *testing.T
	updateErrors []error
	getOutputs   []*awssvc.GetWebACLOutput
	remoteTags   map[string]string
	listErr      error
	untagErr     error
	tagErr       error
	calls        []string
	updateInputs []*awssvc.UpdateWebACLInput
	getInputs    []*awssvc.GetWebACLInput
	listInputs   []*awssvc.ListTagsForResourceInput
	untagInputs  []*awssvc.UntagResourceInput
	tagInputs    []*awssvc.TagResourceInput
}

func newWebACLConfigTagsScript(t *testing.T) *webACLConfigTagsScript {
	return &webACLConfigTagsScript{t: t}
}

func (s *webACLConfigTagsScript) client() *fakeWAFClient {
	return &fakeWAFClient{
		update: func(
			_ context.Context,
			in *awssvc.UpdateWebACLInput,
		) (*awssvc.UpdateWebACLOutput, error) {
			index := len(s.updateInputs)
			require.Less(s.t, index, len(s.updateErrors), "unexpected UpdateWebACL call")
			s.updateInputs = append(s.updateInputs, cloneWebACLUpdateInput(in))
			s.calls = append(s.calls, "update "+aws.ToString(in.LockToken))
			return &awssvc.UpdateWebACLOutput{}, s.updateErrors[index]
		},
		get: func(
			_ context.Context,
			in *awssvc.GetWebACLInput,
		) (*awssvc.GetWebACLOutput, error) {
			index := len(s.getInputs)
			require.Less(s.t, index, len(s.getOutputs), "unexpected GetWebACL call")
			copyInput := *in
			s.getInputs = append(s.getInputs, &copyInput)
			assertWebACLUpdateGetIdentity(s.t, in)
			if index == len(s.getOutputs)-1 {
				s.calls = append(s.calls, "get final state")
			} else {
				s.calls = append(s.calls, "get refresh")
			}
			return s.getOutputs[index], nil
		},
		list: func(
			_ context.Context,
			in *awssvc.ListTagsForResourceInput,
		) (*awssvc.ListTagsForResourceOutput, error) {
			copyInput := *in
			s.listInputs = append(s.listInputs, &copyInput)
			s.calls = append(s.calls, "list")
			return webACLTagListOutput(s.remoteTags), s.listErr
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
	}
}

func setWebACLConfigTagValues(resource, prior *WebACLResource) {
	priorTags := map[string]string{"old": "value"}
	desiredTags := map[string]string{
		"a-new":  "first",
		"change": "new",
		"keep":   "same",
		"z-new":  "last",
	}
	prior.Tags = &priorTags
	resource.Tags = &desiredTags
}
