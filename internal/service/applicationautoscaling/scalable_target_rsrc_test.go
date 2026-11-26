package applicationautoscaling

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareTarget(t *testing.T) {
	for _, capacity := range []int64{0, -1, math.MaxInt32 + 1} {
		r := ScalableTargetResource{ResourceID: "table/a/b", ServiceNamespace: "dynamodb",
			ScalableDimension: "dynamodb:table:ReadCapacityUnits",
			MinCapacity:       capacity, MaxCapacity: capacity}
		got := r.prepare(nil)
		assert.Equal(t, aws.Int32(int32(capacity)), got.MinCapacity)
		assert.Equal(t, aws.Int32(int32(capacity)), got.MaxCapacity)
		assert.Equal(t, aws.String(r.ResourceID), got.ResourceId)
		assert.Equal(t, r.ServiceNamespace, string(got.ServiceNamespace))
		assert.Equal(t, r.ScalableDimension, string(got.ScalableDimension))
		assert.Nil(t, got.RoleARN)
		assert.Nil(t, got.SuspendedState)
		assert.Nil(t, got.Tags)
	}
}

type fakeClock struct {
	now              time.Time
	delays           []time.Duration
	cancel           context.CancelFunc
	cancelAfterSleep bool
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(ctx context.Context, delay time.Duration) error {
	c.delays = append(c.delays, delay)
	c.now = c.now.Add(delay)
	if c.cancel != nil {
		if c.cancelAfterSleep {
			defer c.cancel()
		} else {
			c.cancel()
		}
	}
	return context.Cause(ctx)
}

func TestTargetRetryDeadline(t *testing.T) {
	clock := &fakeClock{}
	var attempts []time.Duration
	sentinel := errors.New("pending")
	err := retryTarget(t.Context(), clock, 2*time.Second,
		func(context.Context) error {
			attempts = append(attempts, clock.Now().Sub(time.Time{}))
			return sentinel
		},
		func(error) bool { return true })
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond,
		3500 * time.Millisecond, 7500 * time.Millisecond}, attempts)
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second,
		4 * time.Second}, clock.delays)
}

func TestTargetRetryCancellationPreservesOperationError(t *testing.T) {
	for _, mode := range []string{"sleep", "completed sleep", "operation"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			cause := errors.New("retry cancelled")
			clock := &fakeClock{}
			if mode != "operation" {
				clock.cancelAfterSleep = mode == "completed sleep"
				clock.cancel = func() {
					if clock.Now().Sub(time.Time{}) >= time.Second {
						cancel(cause)
					}
				}
			}
			var attempts []time.Duration
			var lastError error
			var lastTyped *types.ValidationException
			err := retryTarget(ctx, clock, 2*time.Minute, func(operationCtx context.Context) error {
				assert.Same(t, ctx, operationCtx)
				assert.NoError(t, context.Cause(operationCtx))
				attempts = append(attempts, clock.Now().Sub(time.Time{}))
				lastTyped = &types.ValidationException{
					Message: aws.String(fmt.Sprintf("pending attempt %d", len(attempts))),
				}
				lastError = fmt.Errorf("register scalable target: %w", lastTyped)
				if mode == "operation" {
					cancel(cause)
				}
				return lastError
			}, func(error) bool { return true })
			require.ErrorIs(t, err, lastError)
			assert.Same(t, lastError, err)
			var typed *types.ValidationException
			require.ErrorAs(t, err, &typed)
			assert.Same(t, lastTyped, typed)
			assert.NotErrorIs(t, err, cause)
			assert.ErrorIs(t, context.Cause(ctx), cause)
			if mode != "operation" {
				assert.Equal(t, []time.Duration{0, 500 * time.Millisecond}, attempts)
				assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, clock.delays)
			} else {
				assert.Equal(t, []time.Duration{0}, attempts)
				assert.Empty(t, clock.delays)
			}
		})
	}
}

func TestTargetRetryPreCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, errors.New("parent stopped")} {
		t.Run(cause.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(cause)
			clock := &fakeClock{}
			calls := 0
			err := retryTarget(ctx, clock, 2*time.Minute, func(context.Context) error {
				calls++
				return nil
			}, func(error) bool { return true })
			require.ErrorIs(t, err, cause)
			assert.Zero(t, calls)
			assert.Empty(t, clock.delays)
		})
	}
}

func TestTargetStableIdentity(t *testing.T) {
	r := testTarget()
	definition := r.ResourceDefinition()
	first, err := definition.StableID(r, r.key())
	require.NoError(t, err)
	assert.Equal(t, `"dynamodb"/"table/a/b"/"dynamodb:table:ReadCapacityUnits"`, first)
	other := r
	other.ServiceNamespace = "ecs"
	second, err := definition.StableID(other, other.key())
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
	other = r
	other.ScalableDimension = "other"
	third, err := definition.StableID(other, other.key())
	require.NoError(t, err)
	assert.NotEqual(t, first, third)
	_, err = definition.StableID(r, nil)
	require.Error(t, err)
}
