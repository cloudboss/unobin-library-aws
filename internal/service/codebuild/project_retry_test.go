package codebuild

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/stretchr/testify/assert"
)

func TestProjectRetry(t *testing.T) {
	retryErr := &types.InvalidInputException{Message: aws.String("not authorized to perform test")}
	for _, tc := range []struct {
		name     string
		err      error
		attempts int
	}{
		{"success", nil, 1}, {"nonretryable", errors.New("failure"), 1},
		{"case sensitive", &types.InvalidInputException{
			Message: aws.String("Not Authorized To Perform")}, 1},
		{"retry", retryErr, 18},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(0, 0)
			delays := []time.Duration{}
			calls := 0
			clock := retryClock{now: func() time.Time { return now },
				sleep: func(_ context.Context, d time.Duration) error {
					delays = append(delays, d)
					now = now.Add(d)
					return nil
				}}
			err := propagate(t.Context(), clock, func(context.Context) error { calls++; return tc.err })
			assert.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.attempts, calls)
			if tc.name == "retry" {
				want := []time.Duration{time.Second / 2, time.Second,
					2 * time.Second, 4 * time.Second, 8 * time.Second}
				for range 12 {
					want = append(want, 10*time.Second)
				}
				assert.Equal(t, want, delays)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	err := propagate(ctx, realRetryClock(), func(context.Context) error { called = true; return nil })
	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, called)
	err = propagate(t.Context(), retryClock{now: time.Now,
		sleep: func(context.Context, time.Duration) error { return context.Canceled }},
		func(context.Context) error { return retryErr })
	assert.ErrorIs(t, err, retryErr)
}

func TestProjectRetryDeadlineAndGrace(t *testing.T) {
	ctx := t.Context()
	start := time.Unix(0, 0)
	now := start
	retryErr := &types.InvalidInputException{Message: aws.String("not authorized to perform")}
	delays := []time.Duration{}
	attempts := []time.Duration{}
	err := propagate(ctx, retryClock{now: func() time.Time { return now },
		sleep: func(got context.Context, delay time.Duration) error {
			assert.Same(t, ctx, got)
			delays = append(delays, delay)
			now = now.Add(delay)
			return nil
		}}, func(got context.Context) error {
		assert.Same(t, ctx, got)
		_, bounded := got.Deadline()
		assert.False(t, bounded)
		attempts = append(attempts, now.Sub(start))
		now = start.Add(2 * time.Minute)
		return retryErr
	})
	assert.ErrorIs(t, err, retryErr)
	assert.Equal(t, []time.Duration{0, 2*time.Minute + time.Second/2}, attempts)
	assert.Equal(t, []time.Duration{time.Second / 2}, delays)
}

func TestProjectRetryStopsAfterPropagation(t *testing.T) {
	retryErr := &types.InvalidInputException{Message: aws.String("not authorized to perform")}
	for _, final := range []error{nil, errors.New("denied")} {
		now := time.Unix(0, 0)
		delays, calls := []time.Duration{}, 0
		clock := retryClock{now: func() time.Time { return now },
			sleep: func(_ context.Context, d time.Duration) error {
				delays = append(delays, d)
				now = now.Add(d)
				return nil
			}}
		err := propagate(t.Context(), clock, func(context.Context) error {
			calls++
			if calls < 3 {
				return retryErr
			}
			return final
		})
		assert.ErrorIs(t, err, final)
		assert.Equal(t, 3, calls)
		assert.Equal(t, []time.Duration{time.Second / 2, time.Second}, delays)
	}
}
