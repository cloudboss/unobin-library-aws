package ec2

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	smithy "github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flowLogTestClock struct {
	elapsed time.Duration
	waits   []time.Duration
	cancel  bool
}

func (c *flowLogTestClock) Now() time.Time { return time.Unix(0, 0).Add(c.elapsed) }
func (c *flowLogTestClock) Sleep(ctx context.Context, delay time.Duration) error {
	c.waits = append(c.waits, delay)
	if c.cancel {
		return context.Canceled
	}
	c.elapsed += delay
	return ctx.Err()
}

func TestFlowLogRetrySoftTimeoutAndBackoff(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		times   []time.Duration
		waits   []time.Duration
	}{
		{"soft timeout", time.Second,
			[]time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond, 3500 * time.Millisecond},
			[]time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}},
		{"deadline equality", 500 * time.Millisecond,
			[]time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond},
			[]time.Duration{500 * time.Millisecond, time.Second}},
		{"cap", 20 * time.Second,
			[]time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond, 3500 * time.Millisecond,
				7500 * time.Millisecond, 15500 * time.Millisecond,
				25500 * time.Millisecond, 35500 * time.Millisecond},
			[]time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
				8 * time.Second, 10 * time.Second, 10 * time.Second}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &flowLogTestClock{}
			var calls []time.Duration
			var last error
			err := retryFlowLog(t.Context(), clock, tc.timeout, func(context.Context) error {
				calls = append(calls, clock.elapsed)
				last = &smithy.GenericAPIError{Code: "InvalidParameter",
					Message: "Unable to assume given IAM role"}
				return last
			})
			require.Error(t, err)
			assert.Same(t, last, err)
			assert.Equal(t, tc.times, calls)
			assert.Equal(t, tc.waits, clock.waits)
		})
	}
}

func TestFlowLogRetryCancellationAndFinalResult(t *testing.T) {
	for _, mode := range []string{"success", "terminal", "cancellation"} {
		t.Run(mode, func(t *testing.T) {
			clock := &flowLogTestClock{cancel: mode == "cancellation"}
			calls := 0
			terminal := errors.New("terminal")
			err := retryFlowLog(t.Context(), clock, time.Second, func(context.Context) error {
				calls++
				if calls == 4 {
					if mode == "success" {
						return nil
					}
					return terminal
				}
				return &smithy.GenericAPIError{Code: "InvalidParameter",
					Message: "Unable to assume given IAM role"}
			})
			switch mode {
			case "success":
				require.NoError(t, err)
			case "terminal":
				require.ErrorIs(t, err, terminal)
			default:
				require.ErrorIs(t, err, context.Canceled)
				assert.Equal(t, 1, calls)
				assert.Equal(t, []time.Duration{500 * time.Millisecond}, clock.waits)
			}
		})
	}
}

func TestFlowLogRetryKeepsCallerContextAndInflightResults(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
			defer cancel()
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			clock := &flowLogTestClock{}
			sentinel := errors.New("terminal")
			calls := 0
			err := retryFlowLog(ctx, clock, time.Second, func(actual context.Context) error {
				calls++
				actualDeadline, ok := actual.Deadline()
				require.True(t, ok)
				assert.Equal(t, deadline, actualDeadline)
				assert.Same(t, ctx, actual)
				clock.elapsed = 3 * time.Second
				if terminal {
					return sentinel
				}
				return nil
			})
			if terminal {
				require.ErrorIs(t, err, sentinel)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 1, calls)
			assert.Empty(t, clock.waits)
		})
	}
}

func TestFlowLogWallClockCancellation(t *testing.T) {
	clock := flowLogWallClock{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, clock.Sleep(ctx, time.Hour), context.Canceled)
	require.NoError(t, clock.Sleep(t.Context(), time.Millisecond))
}
