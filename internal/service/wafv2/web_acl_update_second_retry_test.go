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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLUpdateSecondRetryResults(t *testing.T) {
	typedUnavailable := &awstypes.WAFUnavailableEntityException{}
	wrappedUnavailable := &awstypes.WAFUnavailableEntityException{}
	typedAssociated := &awstypes.WAFAssociatedItemException{}
	wrappedAssociated := &awstypes.WAFAssociatedItemException{}
	untypedUnavailable := errors.New(typedUnavailable.Error())
	untypedAssociated := errors.New(typedAssociated.Error())
	other := errors.New("second update failed")
	timeoutAssociated := &awstypes.WAFAssociatedItemException{}
	canceledUnavailable := &awstypes.WAFUnavailableEntityException{}
	secondConflict := &awstypes.WAFOptimisticLockException{}
	tests := []struct {
		name            string
		secondErrors    []error
		options         []awsretry.Option
		cancelOnRefresh bool
		wantErr         error
		wantPlanAgain   bool
	}{
		{
			name: "retries typed and wrapped unavailable and associated",
			secondErrors: []error{
				typedUnavailable,
				fmt.Errorf("wrapped associated: %w", wrappedAssociated),
				typedAssociated,
				fmt.Errorf("wrapped unavailable: %w", wrappedUnavailable),
				nil,
			},
		},
		{
			name:         "similarly worded untyped unavailable",
			secondErrors: []error{untypedUnavailable},
			wantErr:      untypedUnavailable,
		},
		{
			name:         "similarly worded untyped associated",
			secondErrors: []error{untypedAssociated},
			wantErr:      untypedAssociated,
		},
		{name: "other error", secondErrors: []error{other}, wantErr: other},
		{
			name:         "timeout",
			secondErrors: []error{timeoutAssociated},
			options: []awsretry.Option{
				awsretry.WithTimeout(0),
				awsretry.WithInterval(0),
			},
			wantErr: timeoutAssociated,
		},
		{
			name:         "cancellation",
			secondErrors: []error{canceledUnavailable},
			options: []awsretry.Option{
				awsretry.WithTimeout(time.Second),
				awsretry.WithInterval(time.Hour),
			},
			cancelOnRefresh: true,
			wantErr:         context.Canceled,
		},
		{
			name:          "second conflict plans again",
			secondErrors:  []error{secondConflict},
			wantErr:       secondConflict,
			wantPlanAgain: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, prior, expectedFirst := webACLInitialRetryFixture(t)
			if tt.wantErr != nil {
				setWebACLUpdateTestTagsChanged(resource, &prior.Inputs)
			}
			expectedSecond := cloneWebACLUpdateInput(expectedFirst)
			expectedSecond.LockToken = aws.String("token-2")
			firstConflict := &awstypes.WAFOptimisticLockException{}
			ctx := context.Background()
			var cancel context.CancelFunc
			if tt.cancelOnRefresh {
				ctx, cancel = context.WithCancel(ctx)
			}
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
					index := len(updateInputs) - 2
					require.Less(t, index, len(tt.secondErrors))
					return &awssvc.UpdateWebACLOutput{}, tt.secondErrors[index]
				},
				get: func(
					_ context.Context,
					in *awssvc.GetWebACLInput,
				) (*awssvc.GetWebACLOutput, error) {
					assertWebACLUpdateGetIdentity(t, in)
					getCalls++
					if getCalls == 1 {
						calls = append(calls, "get refresh")
						if cancel != nil {
							cancel()
						}
						return shieldSnapshot("token-2", nil), nil
					}
					calls = append(calls, "get final state")
					return shieldSnapshot("token-final", nil), nil
				},
				list: unexpectedWebACLUpdateList(t),
			}
			options := tt.options
			if options == nil {
				options = fastWebACLUpdateRetryOptions()
			}

			out, err := resource.update(ctx, client, prior, options...)
			if tt.wantErr == nil {
				require.NoError(t, err)
				require.NotNil(t, out)
				assert.Equal(t, "token-final", out.LockToken)
			} else {
				assert.Nil(t, out)
				assert.ErrorIs(t, err, tt.wantErr)
			}
			assert.Equal(t, tt.wantPlanAgain, containsWebACLPlanAgain(err))
			expectedCalls := []string{"update token-1", "get refresh"}
			expectedInputs := []*awssvc.UpdateWebACLInput{expectedFirst}
			for range tt.secondErrors {
				expectedCalls = append(expectedCalls, "update token-2")
				expectedInputs = append(expectedInputs, expectedSecond)
			}
			if tt.wantErr == nil {
				expectedCalls = append(expectedCalls, "get final state")
			}
			assert.Equal(t, expectedCalls, calls)
			assert.Equal(t, expectedInputs, updateInputs)
		})
	}
}
