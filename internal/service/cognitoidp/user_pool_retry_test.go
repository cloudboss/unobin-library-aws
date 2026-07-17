package cognitoidp

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPoolRetryPredicates(t *testing.T) {
	trustMessage := "Role does not have a trust relationship allowing Cognito to assume the role"
	policyMessage := "Role does not have permission to publish with SNS"
	tests := map[string]struct {
		err  error
		want bool
	}{
		"trust relationship": {
			err: &cognitotypes.InvalidSmsRoleTrustRelationshipException{
				Message: aws.String(trustMessage),
			},
			want: true,
		},
		"wrapped trust relationship": {
			err: fmt.Errorf("create: %w",
				&cognitotypes.InvalidSmsRoleTrustRelationshipException{
					Message: aws.String(trustMessage),
				}),
			want: true,
		},
		"access policy": {
			err: &cognitotypes.InvalidSmsRoleAccessPolicyException{
				Message: aws.String(policyMessage),
			},
			want: true,
		},
		"trust type with other message": {
			err: &cognitotypes.InvalidSmsRoleTrustRelationshipException{
				Message: aws.String("different"),
			},
		},
		"policy type with other message": {
			err: &cognitotypes.InvalidSmsRoleAccessPolicyException{
				Message: aws.String("different"),
			},
		},
		"plain error": {err: errors.New(trustMessage)},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, isUserPoolRetryable(tt.err))
		})
	}
}

func TestRetryUserPoolUsesCappedBackoff(t *testing.T) {
	clock := &instantUserPoolClock{}
	attempts := 0
	err := retryUserPool(context.Background(), clock, func(context.Context) error {
		attempts++
		if attempts < 8 {
			return retryableUserPoolError()
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 8, attempts)
	assert.Equal(t, []time.Duration{
		500 * time.Millisecond,
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		10 * time.Second,
		10 * time.Second,
	}, clock.sleeps)
}

func TestRetryUserPoolStopsAtTimeout(t *testing.T) {
	clock := &instantUserPoolClock{}
	attempts := 0
	sentinel := retryableUserPoolError()
	err := retryUserPool(context.Background(), clock, func(context.Context) error {
		attempts++
		return sentinel
	})
	assert.Same(t, sentinel, err)
	assert.Equal(t, 16, attempts)
	assert.Equal(t, userPoolRetryTimeout, clock.now.Sub(time.Time{}))
	require.Len(t, clock.sleeps, 16)
	assert.Equal(t, 500*time.Millisecond, clock.sleeps[0])
	assert.Equal(t, time.Second, clock.sleeps[1])
	assert.Equal(t, 2*time.Second, clock.sleeps[2])
	assert.Equal(t, 4*time.Second, clock.sleeps[3])
	assert.Equal(t, 8*time.Second, clock.sleeps[4])
	assert.Equal(t, 10*time.Second, clock.sleeps[5])
	assert.Equal(t, 4500*time.Millisecond, clock.sleeps[15])
}

func TestRetryUserPoolStopsOnNonRetryableError(t *testing.T) {
	clock := &instantUserPoolClock{}
	sentinel := errors.New("denied")
	attempts := 0
	err := retryUserPool(context.Background(), clock, func(context.Context) error {
		attempts++
		return sentinel
	})
	assert.Same(t, sentinel, err)
	assert.Equal(t, 1, attempts)
	assert.Empty(t, clock.sleeps)
}

func TestRetryUserPoolHonorsCancellation(t *testing.T) {
	clock := &instantUserPoolClock{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := retryUserPool(ctx, clock, func(context.Context) error {
		return retryableUserPoolError()
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, clock.sleeps)
}

func retryableUserPoolError() error {
	return &cognitotypes.InvalidSmsRoleTrustRelationshipException{
		Message: aws.String(
			"Role does not have a trust relationship allowing Cognito to assume the role",
		),
	}
}
