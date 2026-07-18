package wafv2

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLAssociationUnavailablePredicate(t *testing.T) {
	tests := map[string]struct {
		err  error
		want bool
	}{
		"typed unavailable": {
			err:  &awstypes.WAFUnavailableEntityException{},
			want: true,
		},
		"wrapped unavailable": {
			err:  fmt.Errorf("associate: %w", &awstypes.WAFUnavailableEntityException{}),
			want: true,
		},
		"typed nonexistent": {err: &awstypes.WAFNonexistentItemException{}},
		"plain error":       {err: errors.New("WAFUnavailableEntityException")},
		"nil":               {},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, isWebACLUnavailable(tt.err))
		})
	}
}

func TestRetryWebACLAssociationUsesProviderCadence(t *testing.T) {
	clock := &instantAssociationClock{}
	attempts := 0
	err := retryWebACLAssociation(context.Background(), clock, func(context.Context) error {
		attempts++
		if attempts < 8 {
			return &awstypes.WAFUnavailableEntityException{}
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

func TestRetryWebACLAssociationStopsAtTenMinutes(t *testing.T) {
	clock := &instantAssociationClock{}
	attempts := 0
	sentinel := &awstypes.WAFUnavailableEntityException{}
	err := retryWebACLAssociation(context.Background(), clock, func(context.Context) error {
		attempts++
		return sentinel
	})

	assert.Same(t, sentinel, err)
	assert.Equal(t, 64, attempts)
	assert.Equal(t, webACLAssociationRetryTimeout, clock.now.Sub(time.Time{}))
	require.Len(t, clock.sleeps, 64)
	assert.Equal(t, 500*time.Millisecond, clock.sleeps[0])
	assert.Equal(t, time.Second, clock.sleeps[1])
	assert.Equal(t, 2*time.Second, clock.sleeps[2])
	assert.Equal(t, 4*time.Second, clock.sleeps[3])
	assert.Equal(t, 8*time.Second, clock.sleeps[4])
	assert.Equal(t, 10*time.Second, clock.sleeps[5])
	assert.Equal(t, 4500*time.Millisecond, clock.sleeps[63])
}

func TestRetryWebACLAssociationStopsImmediatelyForOtherErrors(t *testing.T) {
	clock := &instantAssociationClock{}
	sentinel := errors.New("denied")
	attempts := 0
	err := retryWebACLAssociation(context.Background(), clock, func(context.Context) error {
		attempts++
		return sentinel
	})

	assert.Same(t, sentinel, err)
	assert.Equal(t, 1, attempts)
	assert.Empty(t, clock.sleeps)
}

func TestRetryWebACLAssociationHonorsCancellation(t *testing.T) {
	clock := &instantAssociationClock{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	err := retryWebACLAssociation(ctx, clock, func(context.Context) error {
		attempts++
		return &awstypes.WAFUnavailableEntityException{}
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, attempts)
	assert.Empty(t, clock.sleeps)
}
