package wafv2

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	awsretry "github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLUpdateInitialRetryResults(t *testing.T) {
	typedUnavailable := &awstypes.WAFUnavailableEntityException{}
	wrappedUnavailable := &awstypes.WAFUnavailableEntityException{}
	untypedUnavailable := errors.New(typedUnavailable.Error())
	associated := &awstypes.WAFAssociatedItemException{}
	untypedConflict := errors.New((&awstypes.WAFOptimisticLockException{}).Error())
	other := errors.New("update failed")
	timeoutUnavailable := &awstypes.WAFUnavailableEntityException{}
	canceledUnavailable := &awstypes.WAFUnavailableEntityException{}
	tests := []struct {
		name         string
		updateErrors []error
		options      []awsretry.Option
		cancel       bool
		wantCalls    []string
		wantErr      error
	}{
		{
			name:         "typed unavailable then success",
			updateErrors: []error{typedUnavailable, nil},
			wantCalls:    []string{"update", "update", "get final state"},
		},
		{
			name: "wrapped unavailable then success",
			updateErrors: []error{
				fmt.Errorf("wrapped unavailable: %w", wrappedUnavailable),
				nil,
			},
			wantCalls: []string{"update", "update", "get final state"},
		},
		{
			name:         "similarly worded untyped error",
			updateErrors: []error{untypedUnavailable},
			wantCalls:    []string{"update"},
			wantErr:      untypedUnavailable,
		},
		{
			name:         "associated error",
			updateErrors: []error{associated},
			wantCalls:    []string{"update"},
			wantErr:      associated,
		},
		{
			name:         "similarly worded untyped optimistic conflict",
			updateErrors: []error{untypedConflict},
			wantCalls:    []string{"update"},
			wantErr:      untypedConflict,
		},
		{
			name:         "other error",
			updateErrors: []error{other},
			wantCalls:    []string{"update"},
			wantErr:      other,
		},
		{
			name:         "timeout",
			updateErrors: []error{timeoutUnavailable},
			options: []awsretry.Option{
				awsretry.WithTimeout(0),
				awsretry.WithInterval(0),
			},
			wantCalls: []string{"update"},
			wantErr:   timeoutUnavailable,
		},
		{
			name:         "cancellation",
			updateErrors: []error{canceledUnavailable},
			options: []awsretry.Option{
				awsretry.WithTimeout(time.Second),
				awsretry.WithInterval(time.Hour),
			},
			cancel:    true,
			wantCalls: []string{"update"},
			wantErr:   context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, prior, expected := webACLInitialRetryFixture(t)
			if tt.wantErr != nil {
				priorTags := map[string]string{"old": "value"}
				desiredTags := map[string]string{"new": "value"}
				prior.Inputs.Tags = &priorTags
				resource.Tags = &desiredTags
			}
			script := newWebACLInitialRetryScript(t, tt.updateErrors)
			ctx := context.Background()
			if tt.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			options := tt.options
			if options == nil {
				options = fastWebACLUpdateRetryOptions()
			}

			out, err := resource.update(ctx, script.client(), prior, options...)
			if tt.wantErr == nil {
				require.NoError(t, err)
				require.NotNil(t, out)
			} else {
				assert.Nil(t, out)
				assert.ErrorIs(t, err, tt.wantErr)
			}
			assert.Equal(t, tt.wantCalls, script.calls)
			require.Len(t, script.updateInputs, len(tt.updateErrors))
			for _, input := range script.updateInputs {
				assert.Equal(t, expected, input)
			}
		})
	}
}

func TestWebACLUpdateRetryDoesNotEncloseGets(t *testing.T) {
	t.Run("Shield snapshot", func(t *testing.T) {
		resource := validWebACLResource()
		resource.Description = aws.String("changed")
		prior := validWebACLUpdatePrior(*validWebACLResource())
		unavailable := &awstypes.WAFUnavailableEntityException{}
		getCalls := 0
		client := &fakeWAFClient{
			get: func(
				context.Context,
				*awssvc.GetWebACLInput,
			) (*awssvc.GetWebACLOutput, error) {
				getCalls++
				return nil, unavailable
			},
			update: func(
				context.Context,
				*awssvc.UpdateWebACLInput,
			) (*awssvc.UpdateWebACLOutput, error) {
				t.Fatal("unexpected UpdateWebACL call")
				return nil, nil
			},
		}

		out, err := resource.update(
			context.Background(),
			client,
			prior,
			fastWebACLUpdateRetryOptions()...,
		)
		assert.Nil(t, out)
		assert.ErrorIs(t, err, unavailable)
		assert.Equal(t, 1, getCalls)
	})

	t.Run("final read", func(t *testing.T) {
		resource, prior, _ := webACLInitialRetryFixture(t)
		unavailable := &awstypes.WAFUnavailableEntityException{}
		updateCalls := 0
		getCalls := 0
		client := &fakeWAFClient{
			update: func(
				context.Context,
				*awssvc.UpdateWebACLInput,
			) (*awssvc.UpdateWebACLOutput, error) {
				updateCalls++
				return &awssvc.UpdateWebACLOutput{}, nil
			},
			get: func(
				context.Context,
				*awssvc.GetWebACLInput,
			) (*awssvc.GetWebACLOutput, error) {
				getCalls++
				return nil, unavailable
			},
		}

		out, err := resource.update(
			context.Background(),
			client,
			prior,
			fastWebACLUpdateRetryOptions()...,
		)
		assert.Nil(t, out)
		assert.ErrorIs(t, err, unavailable)
		assert.Equal(t, 1, updateCalls)
		assert.Equal(t, 1, getCalls)
	})
}

type webACLInitialRetryScript struct {
	t            *testing.T
	updateErrors []error
	updateInputs []*awssvc.UpdateWebACLInput
	calls        []string
}

func newWebACLInitialRetryScript(
	t *testing.T,
	updateErrors []error,
) *webACLInitialRetryScript {
	return &webACLInitialRetryScript{t: t, updateErrors: updateErrors}
}

func (s *webACLInitialRetryScript) client() *fakeWAFClient {
	return &fakeWAFClient{
		update: func(
			_ context.Context,
			in *awssvc.UpdateWebACLInput,
		) (*awssvc.UpdateWebACLOutput, error) {
			index := len(s.updateInputs)
			require.Less(s.t, index, len(s.updateErrors), "unexpected UpdateWebACL call")
			copyInput := *in
			copyInput.Rules = append([]awstypes.Rule(nil), in.Rules...)
			s.updateInputs = append(s.updateInputs, &copyInput)
			s.calls = append(s.calls, "update")
			return &awssvc.UpdateWebACLOutput{}, s.updateErrors[index]
		},
		get: func(
			ctx context.Context,
			in *awssvc.GetWebACLInput,
		) (*awssvc.GetWebACLOutput, error) {
			s.calls = append(s.calls, "get final state")
			return successfulWebACLGet(ctx, in)
		},
		list: func(
			context.Context,
			*awssvc.ListTagsForResourceInput,
		) (*awssvc.ListTagsForResourceOutput, error) {
			s.t.Fatal("unexpected ListTagsForResource call")
			return nil, nil
		},
	}
}

func webACLInitialRetryFixture(
	t *testing.T,
) (*WebACLResource, runtime.Prior[WebACLResource, *WebACLResourceOutput, *awsCfg],
	*awssvc.UpdateWebACLInput) {
	t.Helper()
	desiredRules := []WebACLRule{
		webACLUpdateTestRule(testShieldRuleName, "desired-shield-rule", 1),
	}
	resource := validWebACLResource()
	resource.Description = aws.String("changed")
	resource.Rules = &desiredRules
	prior := validWebACLUpdatePrior(*validWebACLResource())
	expected, err := resource.updateInput(prior.Outputs)
	require.NoError(t, err)
	return resource, prior, expected
}

func fastWebACLUpdateRetryOptions() []awsretry.Option {
	return []awsretry.Option{
		awsretry.WithTimeout(time.Second),
		awsretry.WithInterval(0),
	}
}
