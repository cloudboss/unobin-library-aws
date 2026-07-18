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

func TestWebACLDeleteUsesPriorARNIdentityAndLockToken(t *testing.T) {
	var actual *awssvc.DeleteWebACLInput
	client := &fakeWAFClient{delete: func(
		_ context.Context,
		in *awssvc.DeleteWebACLInput,
	) (*awssvc.DeleteWebACLOutput, error) {
		actual = in
		return &awssvc.DeleteWebACLOutput{}, nil
	}}
	prior := validWebACLDeletePrior()
	prior.ID = "ignored-id"

	err := (&WebACLResource{}).delete(
		context.Background(),
		client,
		prior,
		fastWebACLDeleteRetryOptions()...,
	)
	require.NoError(t, err)
	assert.Equal(t, &awssvc.DeleteWebACLInput{
		Id:        aws.String("id-1"),
		LockToken: aws.String("token-1"),
		Name:      aws.String("example"),
		Scope:     awstypes.ScopeRegional,
	}, actual)
}

func TestWebACLDeleteRejectsInvalidPriorBeforeCallingAWS(t *testing.T) {
	tests := []struct {
		name    string
		prior   *WebACLResourceOutput
		wantErr string
	}{
		{name: "nil prior", wantErr: "prior web ACL output has no ARN"},
		{
			name:    "empty ARN",
			prior:   &WebACLResourceOutput{LockToken: "token-1"},
			wantErr: "prior web ACL output has no ARN",
		},
		{
			name:    "invalid ARN",
			prior:   &WebACLResourceOutput{ARN: "invalid", LockToken: "token-1"},
			wantErr: "parse web ACL ARN",
		},
		{
			name:    "empty lock token",
			prior:   &WebACLResourceOutput{ARN: testWebACLARN},
			wantErr: "prior web ACL output has no lock-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := newWebACLDeleteScript(t, nil, nil)
			err := (&WebACLResource{}).delete(
				context.Background(),
				script.client(),
				tt.prior,
				fastWebACLDeleteRetryOptions()...,
			)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, script.calls)
		})
	}
}

func TestIsWebACLDeleteRetryable(t *testing.T) {
	unavailable := &awstypes.WAFUnavailableEntityException{}
	associated := &awstypes.WAFAssociatedItemException{}
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "unavailable", err: unavailable, expected: true},
		{name: "wrapped unavailable", err: fmt.Errorf("wrapped: %w", unavailable), expected: true},
		{name: "associated", err: associated, expected: true},
		{name: "wrapped associated", err: fmt.Errorf("wrapped: %w", associated), expected: true},
		{name: "optimistic conflict", err: &awstypes.WAFOptimisticLockException{}},
		{name: "not found", err: &awstypes.WAFNonexistentItemException{}},
		{name: "untyped", err: errors.New(unavailable.Error())},
		{name: "nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isWebACLDeleteRetryable(tt.err))
		})
	}
}

func TestWebACLDeleteInitialAttemptResults(t *testing.T) {
	untyped := errors.New("delete failed")
	tests := []struct {
		name         string
		deleteErrors []error
		options      []awsretry.Option
		cancel       bool
		wantCalls    int
		checkError   func(*testing.T, error)
	}{
		{
			name:         "success",
			deleteErrors: []error{nil},
			wantCalls:    1,
			checkError:   requireWebACLDeleteNoError,
		},
		{
			name:         "typed not found is success",
			deleteErrors: []error{&awstypes.WAFNonexistentItemException{}},
			wantCalls:    1,
			checkError:   requireWebACLDeleteNoError,
		},
		{
			name: "retries wrapped unavailable and associated",
			deleteErrors: []error{
				fmt.Errorf("wrapped: %w", &awstypes.WAFUnavailableEntityException{}),
				&awstypes.WAFAssociatedItemException{},
				nil,
			},
			wantCalls:  3,
			checkError: requireWebACLDeleteNoError,
		},
		{
			name:         "untyped error is not retried",
			deleteErrors: []error{untyped},
			wantCalls:    1,
			checkError: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, untyped)
			},
		},
		{
			name:         "transient timeout",
			deleteErrors: []error{&awstypes.WAFUnavailableEntityException{}},
			options: []awsretry.Option{
				awsretry.WithTimeout(0),
				awsretry.WithInterval(0),
			},
			wantCalls: 1,
			checkError: func(t *testing.T, err error) {
				assert.True(t, isWebACLUnavailable(err))
			},
		},
		{
			name:         "canceled while waiting",
			deleteErrors: []error{&awstypes.WAFAssociatedItemException{}},
			options: []awsretry.Option{
				awsretry.WithTimeout(time.Second),
				awsretry.WithInterval(time.Hour),
			},
			cancel:    true,
			wantCalls: 1,
			checkError: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, context.Canceled)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			options := tt.options
			if options == nil {
				options = fastWebACLDeleteRetryOptions()
			}
			script := newWebACLDeleteScript(t, tt.deleteErrors, nil)
			err := (&WebACLResource{}).delete(
				ctx,
				script.client(),
				validWebACLDeletePrior(),
				options...,
			)
			tt.checkError(t, err)
			assert.Len(t, script.deleteInputs, tt.wantCalls)
			assert.Empty(t, script.getInputs)
			for _, input := range script.deleteInputs {
				assert.Equal(t, "token-1", input.lockToken)
			}
		})
	}
}

func TestWebACLDeleteOptimisticConflictRefresh(t *testing.T) {
	getFailure := errors.New("refresh failed")
	secondFailure := errors.New("second delete failed")
	firstConflict := &awstypes.WAFOptimisticLockException{}
	secondConflict := &awstypes.WAFOptimisticLockException{}
	tests := []struct {
		name         string
		deleteErrors []error
		getResults   []webACLDeleteGetResult
		options      []awsretry.Option
		cancelOnGet  bool
		wantCalls    []string
		checkError   func(*testing.T, error)
	}{
		{
			name:         "changed token succeeds",
			deleteErrors: []error{firstConflict, nil},
			getResults:   []webACLDeleteGetResult{getResultWithLockToken("token-2")},
			wantCalls:    []string{"delete:token-1", "get", "delete:token-2"},
			checkError:   requireWebACLDeleteNoError,
		},
		{
			name: "refreshed attempt retries transients",
			deleteErrors: []error{
				firstConflict,
				fmt.Errorf("wrapped: %w", &awstypes.WAFUnavailableEntityException{}),
				&awstypes.WAFAssociatedItemException{},
				nil,
			},
			getResults: []webACLDeleteGetResult{getResultWithLockToken("token-2")},
			wantCalls: []string{
				"delete:token-1",
				"get",
				"delete:token-2",
				"delete:token-2",
				"delete:token-2",
			},
			checkError: requireWebACLDeleteNoError,
		},
		{
			name:         "unchanged token returns original conflict",
			deleteErrors: []error{firstConflict},
			getResults:   []webACLDeleteGetResult{getResultWithLockToken("token-1")},
			wantCalls:    []string{"delete:token-1", "get"},
			checkError: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, firstConflict)
			},
		},
		{
			name:         "empty refreshed token is rejected",
			deleteErrors: []error{firstConflict},
			getResults: []webACLDeleteGetResult{{
				output: &awssvc.GetWebACLOutput{},
			}},
			wantCalls: []string{"delete:token-1", "get"},
			checkError: func(t *testing.T, err error) {
				assert.ErrorContains(t, err, "response has no lock-token")
			},
		},
		{
			name:         "nil refresh response is rejected",
			deleteErrors: []error{firstConflict},
			getResults:   []webACLDeleteGetResult{{}},
			wantCalls:    []string{"delete:token-1", "get"},
			checkError: func(t *testing.T, err error) {
				assert.ErrorContains(t, err, "response has no lock-token")
			},
		},
		{
			name:         "refresh error propagates",
			deleteErrors: []error{firstConflict},
			getResults:   []webACLDeleteGetResult{{err: getFailure}},
			wantCalls:    []string{"delete:token-1", "get"},
			checkError: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, getFailure)
			},
		},
		{
			name:         "refresh not found propagates",
			deleteErrors: []error{firstConflict},
			getResults: []webACLDeleteGetResult{{
				err: &awstypes.WAFNonexistentItemException{},
			}},
			wantCalls: []string{"delete:token-1", "get"},
			checkError: func(t *testing.T, err error) {
				assert.True(t, isWebACLNotFound(err))
			},
		},
		{
			name:         "refresh transient is not retried",
			deleteErrors: []error{firstConflict},
			getResults: []webACLDeleteGetResult{{
				err: &awstypes.WAFUnavailableEntityException{},
			}},
			wantCalls: []string{"delete:token-1", "get"},
			checkError: func(t *testing.T, err error) {
				assert.True(t, isWebACLUnavailable(err))
			},
		},
		{
			name:         "second conflict requests new plan",
			deleteErrors: []error{firstConflict, secondConflict},
			getResults:   []webACLDeleteGetResult{getResultWithLockToken("token-2")},
			wantCalls:    []string{"delete:token-1", "get", "delete:token-2"},
			checkError: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, secondConflict)
				assert.ErrorContains(t, err, "run a new plan")
			},
		},
		{
			name:         "second not found is success",
			deleteErrors: []error{firstConflict, &awstypes.WAFNonexistentItemException{}},
			getResults:   []webACLDeleteGetResult{getResultWithLockToken("token-2")},
			wantCalls:    []string{"delete:token-1", "get", "delete:token-2"},
			checkError:   requireWebACLDeleteNoError,
		},
		{
			name:         "second untyped error propagates",
			deleteErrors: []error{firstConflict, secondFailure},
			getResults:   []webACLDeleteGetResult{getResultWithLockToken("token-2")},
			wantCalls:    []string{"delete:token-1", "get", "delete:token-2"},
			checkError: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, secondFailure)
			},
		},
		{
			name:         "refreshed transient timeout",
			deleteErrors: []error{firstConflict, &awstypes.WAFAssociatedItemException{}},
			getResults:   []webACLDeleteGetResult{getResultWithLockToken("token-2")},
			options: []awsretry.Option{
				awsretry.WithTimeout(0),
				awsretry.WithInterval(0),
			},
			wantCalls: []string{"delete:token-1", "get", "delete:token-2"},
			checkError: func(t *testing.T, err error) {
				assert.True(t, isWebACLAssociated(err))
			},
		},
		{
			name:         "refreshed retry honors cancellation",
			deleteErrors: []error{firstConflict, &awstypes.WAFUnavailableEntityException{}},
			getResults:   []webACLDeleteGetResult{getResultWithLockToken("token-2")},
			options: []awsretry.Option{
				awsretry.WithTimeout(time.Second),
				awsretry.WithInterval(time.Hour),
			},
			cancelOnGet: true,
			wantCalls:   []string{"delete:token-1", "get", "delete:token-2"},
			checkError: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, context.Canceled)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			script := newWebACLDeleteScript(t, tt.deleteErrors, tt.getResults)
			if tt.cancelOnGet {
				canceled, cancel := context.WithCancel(ctx)
				ctx = canceled
				script.afterGet = cancel
			}
			options := tt.options
			if options == nil {
				options = fastWebACLDeleteRetryOptions()
			}

			err := (&WebACLResource{}).delete(
				ctx,
				script.client(),
				validWebACLDeletePrior(),
				options...,
			)
			tt.checkError(t, err)
			assert.Equal(t, tt.wantCalls, script.calls)
			assertDeleteIdentityInputs(t, script)
		})
	}
}

type webACLDeleteInput struct {
	id        string
	lockToken string
	name      string
	scope     awstypes.Scope
}

type webACLDeleteGetInput struct {
	id    string
	name  string
	scope awstypes.Scope
}

type webACLDeleteGetResult struct {
	output *awssvc.GetWebACLOutput
	err    error
}

type webACLDeleteScript struct {
	t            *testing.T
	deleteErrors []error
	getResults   []webACLDeleteGetResult
	afterGet     func()
	calls        []string
	deleteInputs []webACLDeleteInput
	getInputs    []webACLDeleteGetInput
}

func newWebACLDeleteScript(
	t *testing.T,
	deleteErrors []error,
	getResults []webACLDeleteGetResult,
) *webACLDeleteScript {
	return &webACLDeleteScript{
		t:            t,
		deleteErrors: deleteErrors,
		getResults:   getResults,
	}
}

func (s *webACLDeleteScript) client() *fakeWAFClient {
	return &fakeWAFClient{
		delete: func(
			_ context.Context,
			in *awssvc.DeleteWebACLInput,
		) (*awssvc.DeleteWebACLOutput, error) {
			index := len(s.deleteInputs)
			require.Less(s.t, index, len(s.deleteErrors), "unexpected DeleteWebACL call")
			s.deleteInputs = append(s.deleteInputs, webACLDeleteInput{
				id:        aws.ToString(in.Id),
				lockToken: aws.ToString(in.LockToken),
				name:      aws.ToString(in.Name),
				scope:     in.Scope,
			})
			s.calls = append(s.calls, "delete:"+aws.ToString(in.LockToken))
			return &awssvc.DeleteWebACLOutput{}, s.deleteErrors[index]
		},
		get: func(
			_ context.Context,
			in *awssvc.GetWebACLInput,
		) (*awssvc.GetWebACLOutput, error) {
			index := len(s.getInputs)
			require.Less(s.t, index, len(s.getResults), "unexpected GetWebACL call")
			s.getInputs = append(s.getInputs, webACLDeleteGetInput{
				id:    aws.ToString(in.Id),
				name:  aws.ToString(in.Name),
				scope: in.Scope,
			})
			s.calls = append(s.calls, "get")
			if s.afterGet != nil {
				s.afterGet()
			}
			return s.getResults[index].output, s.getResults[index].err
		},
	}
}

func assertDeleteIdentityInputs(t *testing.T, script *webACLDeleteScript) {
	t.Helper()
	for _, input := range script.deleteInputs {
		assert.Equal(t, "id-1", input.id)
		assert.Equal(t, "example", input.name)
		assert.Equal(t, awstypes.ScopeRegional, input.scope)
	}
	for _, input := range script.getInputs {
		assert.Equal(t, webACLDeleteGetInput{
			id:    "id-1",
			name:  "example",
			scope: awstypes.ScopeRegional,
		}, input)
	}
}

func getResultWithLockToken(token string) webACLDeleteGetResult {
	return webACLDeleteGetResult{
		output: &awssvc.GetWebACLOutput{LockToken: aws.String(token)},
	}
}

func requireWebACLDeleteNoError(t *testing.T, err error) {
	t.Helper()
	require.NoError(t, err)
}

func validWebACLDeletePrior() *WebACLResourceOutput {
	return &WebACLResourceOutput{
		ARN:       testWebACLARN,
		ID:        "id-1",
		LockToken: "token-1",
	}
}

func fastWebACLDeleteRetryOptions() []awsretry.Option {
	return []awsretry.Option{
		awsretry.WithTimeout(time.Second),
		awsretry.WithInterval(0),
	}
}
