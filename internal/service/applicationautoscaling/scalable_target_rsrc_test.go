package applicationautoscaling

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
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
	now    time.Time
	delays []time.Duration
	cancel context.CancelFunc
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(ctx context.Context, delay time.Duration) error {
	c.delays = append(c.delays, delay)
	c.now = c.now.Add(delay)
	if c.cancel != nil {
		c.cancel()
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
