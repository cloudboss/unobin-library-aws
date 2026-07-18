package wafv2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLUpdateConflictRefreshFailuresStop(t *testing.T) {
	refreshFailure := errors.New("refresh failed")
	notFound := &awstypes.WAFNonexistentItemException{}
	mismatched := shieldSnapshot("token-2", nil)
	mismatched.WebACL.Name = aws.String("other")
	tests := []struct {
		name        string
		output      *awssvc.GetWebACLOutput
		getErr      error
		wantContain string
		unchanged   bool
	}{
		{name: "SDK error", getErr: refreshFailure},
		{name: "typed not found", getErr: notFound},
		{name: "nil output", wantContain: "empty response"},
		{
			name:        "nil web ACL",
			output:      &awssvc.GetWebACLOutput{LockToken: aws.String("token-2")},
			wantContain: "response has no web ACL",
		},
		{
			name:        "identity mismatch",
			output:      mismatched,
			wantContain: "response identity does not match request",
		},
		{
			name:        "empty token",
			output:      shieldSnapshot("", nil),
			wantContain: "response has no lock-token",
		},
		{
			name:      "unchanged token",
			output:    shieldSnapshot("token-1", nil),
			unchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, prior, expected := webACLInitialRetryFixture(t)
			setWebACLUpdateTestTagsChanged(resource, &prior.Inputs)
			conflict := &awstypes.WAFOptimisticLockException{}
			calls := []string{}
			updateInputs := []*awssvc.UpdateWebACLInput{}
			getCalls := 0
			client := &fakeWAFClient{
				update: func(
					_ context.Context,
					in *awssvc.UpdateWebACLInput,
				) (*awssvc.UpdateWebACLOutput, error) {
					updateInputs = append(updateInputs, cloneWebACLUpdateInput(in))
					calls = append(calls, "update")
					if len(updateInputs) > 1 {
						t.Fatal("unexpected second UpdateWebACL call")
					}
					return nil, conflict
				},
				get: func(
					_ context.Context,
					in *awssvc.GetWebACLInput,
				) (*awssvc.GetWebACLOutput, error) {
					getCalls++
					if getCalls > 1 {
						t.Fatal("unexpected final GetWebACL call")
					}
					assertWebACLUpdateGetIdentity(t, in)
					calls = append(calls, "get refresh")
					return tt.output, tt.getErr
				},
				list: unexpectedWebACLUpdateList(t),
			}

			out, err := resource.update(
				context.Background(),
				client,
				prior,
				fastWebACLUpdateRetryOptions()...,
			)
			assert.Nil(t, out)
			require.Error(t, err)
			switch {
			case tt.unchanged:
				assert.ErrorIs(t, err, conflict)
			case tt.getErr != nil:
				assert.ErrorIs(t, err, tt.getErr)
			default:
				assert.ErrorContains(t, err, tt.wantContain)
			}
			assert.Equal(t, []string{"update", "get refresh"}, calls)
			assert.Equal(t, []*awssvc.UpdateWebACLInput{expected}, updateInputs)
		})
	}
}

func TestWebACLUpdateSecondAttemptTerminalErrors(t *testing.T) {
	typedConflict := &awstypes.WAFOptimisticLockException{}
	wrappedConflict := &awstypes.WAFOptimisticLockException{}
	untypedConflict := errors.New(typedConflict.Error())
	other := errors.New("second update failed")
	notFound := &awstypes.WAFNonexistentItemException{}
	tests := []struct {
		name      string
		secondErr error
		wantIs    error
		planAgain bool
	}{
		{
			name:      "typed optimistic conflict",
			secondErr: typedConflict,
			wantIs:    typedConflict,
			planAgain: true,
		},
		{
			name:      "wrapped optimistic conflict",
			secondErr: fmt.Errorf("wrapped conflict: %w", wrappedConflict),
			wantIs:    wrappedConflict,
			planAgain: true,
		},
		{
			name:      "similarly worded untyped conflict",
			secondErr: untypedConflict,
			wantIs:    untypedConflict,
		},
		{name: "other error", secondErr: other, wantIs: other},
		{name: "not found is an error", secondErr: notFound, wantIs: notFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, prior, expectedFirst := webACLInitialRetryFixture(t)
			setWebACLUpdateTestTagsChanged(resource, &prior.Inputs)
			expectedSecond := cloneWebACLUpdateInput(expectedFirst)
			expectedSecond.LockToken = aws.String("token-2")
			firstConflict := &awstypes.WAFOptimisticLockException{}
			calls := []string{}
			updateInputs := []*awssvc.UpdateWebACLInput{}
			getCalls := 0
			client := &fakeWAFClient{
				update: func(
					_ context.Context,
					in *awssvc.UpdateWebACLInput,
				) (*awssvc.UpdateWebACLOutput, error) {
					updateInputs = append(updateInputs, cloneWebACLUpdateInput(in))
					calls = append(calls, "update "+aws.ToString(in.LockToken))
					if len(updateInputs) == 1 {
						return nil, firstConflict
					}
					if len(updateInputs) > 2 {
						t.Fatal("unexpected third UpdateWebACL call")
					}
					return nil, tt.secondErr
				},
				get: func(
					_ context.Context,
					in *awssvc.GetWebACLInput,
				) (*awssvc.GetWebACLOutput, error) {
					getCalls++
					if getCalls > 1 {
						t.Fatal("unexpected final GetWebACL call")
					}
					assertWebACLUpdateGetIdentity(t, in)
					calls = append(calls, "get refresh")
					return shieldSnapshot("token-2", nil), nil
				},
				list: unexpectedWebACLUpdateList(t),
			}

			out, err := resource.update(
				context.Background(),
				client,
				prior,
				fastWebACLUpdateRetryOptions()...,
			)
			assert.Nil(t, out)
			assert.ErrorIs(t, err, tt.wantIs)
			assert.Equal(t, tt.planAgain, containsWebACLPlanAgain(err))
			assert.Equal(t, []string{
				"update token-1",
				"get refresh",
				"update token-2",
			}, calls)
			assert.Equal(t, []*awssvc.UpdateWebACLInput{
				expectedFirst,
				expectedSecond,
			}, updateInputs)
			if tt.secondErr == notFound {
				assert.True(t, isWebACLNotFound(err))
			}
		})
	}
}

func setWebACLUpdateTestTagsChanged(resource, prior *WebACLResource) {
	priorTags := map[string]string{"old": "value"}
	desiredTags := map[string]string{"new": "value"}
	prior.Tags = &priorTags
	resource.Tags = &desiredTags
}

func unexpectedWebACLUpdateList(
	t *testing.T,
) func(context.Context, *awssvc.ListTagsForResourceInput) (
	*awssvc.ListTagsForResourceOutput,
	error,
) {
	return func(
		context.Context,
		*awssvc.ListTagsForResourceInput,
	) (*awssvc.ListTagsForResourceOutput, error) {
		t.Fatal("unexpected ListTagsForResource call")
		return nil, nil
	}
}

func containsWebACLPlanAgain(err error) bool {
	return err != nil && strings.Contains(err.Error(), "plan again")
}
