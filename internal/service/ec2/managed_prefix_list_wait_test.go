package ec2

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type prefixWaitClock struct {
	elapsed time.Duration
	sleeps  []time.Duration
}

func (c *prefixWaitClock) now() time.Time { return time.Now().Add(c.elapsed) }
func (c *prefixWaitClock) sleep(_ context.Context, delay time.Duration) error {
	c.sleeps = append(c.sleeps, delay)
	c.elapsed += delay
	return nil
}

func TestManagedPrefixListWaitSchedule(t *testing.T) {
	for _, states := range [][]string{
		{"", "", "modify-in-progress", "modify-complete"},
		{"modify-in-progress", "", "", "modify-in-progress", "modify-complete"},
		{"", "", "", "", "", "", "", "", "modify-complete"},
	} {
		clock := &prefixWaitClock{}
		w := managedPrefixListWaiter{clock.now, clock.sleep, 15 * time.Minute}
		var polls []time.Duration
		i := 0
		out, err := w.wait(t.Context(), "modify-in-progress", "modify-complete",
			func(ctx context.Context) (*ec2types.ManagedPrefixList, error) {
				require.NoError(t, ctx.Err())
				polls = append(polls, clock.elapsed)
				state := states[i]
				i++
				if state == "" {
					return nil, runtime.ErrNotFound
				}
				return &ec2types.ManagedPrefixList{State: ec2types.PrefixListState(state)}, nil
			})
		require.NoError(t, err)
		assert.Equal(t, ec2types.PrefixListStateModifyComplete, out.State)
		delays := []time.Duration{200 * time.Millisecond, 400 * time.Millisecond,
			800 * time.Millisecond, 1600 * time.Millisecond, 3200 * time.Millisecond,
			6400 * time.Millisecond, 10 * time.Second, 10 * time.Second}
		assert.Equal(t, delays[:len(states)-1], clock.sleeps)
		expected := []time.Duration{0}
		for _, d := range delays[:len(states)-1] {
			expected = append(expected, expected[len(expected)-1]+d)
		}
		assert.Equal(t, expected, polls)
	}
}

func TestManagedPrefixListWaitAbsenceAndStates(t *testing.T) {
	for _, operation := range []string{"create", "modify", "delete"} {
		for _, missing := range []int{0, 20, 21} {
			t.Run(operation+time.Duration(missing).String(), func(t *testing.T) {
				clock := &prefixWaitClock{}
				w := managedPrefixListWaiter{clock.now, clock.sleep, 15 * time.Minute}
				calls := 0
				target := operation + "-complete"
				if operation == "delete" {
					target = ""
				}
				out, err := w.wait(t.Context(), operation+"-in-progress", target,
					func(context.Context) (*ec2types.ManagedPrefixList, error) {
						calls++
						if calls <= missing || operation == "delete" {
							return nil, runtime.ErrNotFound
						}
						return &ec2types.ManagedPrefixList{State: ec2types.PrefixListState(target)}, nil
					})
				if missing == 21 && operation != "delete" {
					require.ErrorContains(t, err, "21")
				} else {
					require.NoError(t, err)
				}
				if operation == "delete" {
					assert.Equal(t, 1, calls)
					assert.Nil(t, out)
				}
			})
		}
		clock := &prefixWaitClock{}
		w := managedPrefixListWaiter{clock.now, clock.sleep, 15 * time.Minute}
		failed := &ec2types.ManagedPrefixList{State: ec2types.PrefixListState(operation + "-failed"),
			StateMessage: aws.String("remote failure")}
		out, err := w.wait(t.Context(), operation+"-in-progress", operation+"-complete",
			func(context.Context) (*ec2types.ManagedPrefixList, error) { return failed, nil })
		assert.Equal(t, failed, out)
		require.ErrorContains(t, err, operation+"-failed")
	}
}

func TestManagedPrefixListWaitDeadlineSelection(t *testing.T) {
	for _, mode := range []string{"grace pending", "grace target", "deadline error",
		"initial deadline error", "ordinary crossing", "cancel", "cancel exhausted", "refresh error"} {
		t.Run(mode, func(t *testing.T) {
			clock := &prefixWaitClock{}
			now := time.Now()
			w := managedPrefixListWaiter{
				now:   func() time.Time { return now.Add(clock.elapsed) },
				sleep: clock.sleep, timeout: time.Second,
			}
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			failure := errors.New("refresh failure")
			canceled := errors.New("caller canceled")
			calls := 0
			out, err := w.wait(ctx, "modify-in-progress", "modify-complete",
				func(child context.Context) (*ec2types.ManagedPrefixList, error) {
					calls++
					if calls == 1 && mode != "initial deadline error" {
						if mode == "ordinary crossing" {
							clock.elapsed = 900 * time.Millisecond
						} else {
							clock.elapsed = time.Second
						}
						if mode == "cancel" {
							clock.elapsed = 0
							cancel(canceled)
						}
						if mode == "cancel exhausted" {
							cancel(canceled)
						}
						if mode == "refresh error" {
							return nil, failure
						}
						return &ec2types.ManagedPrefixList{State: "modify-in-progress"}, nil
					}
					if mode != "initial deadline error" {
						require.ErrorIs(t, child.Err(), context.DeadlineExceeded)
						assert.NotSame(t, ctx, child)
					}
					if mode == "deadline error" || mode == "initial deadline error" {
						return &ec2types.ManagedPrefixList{State: "bad"}, context.DeadlineExceeded
					}
					if mode == "grace target" {
						return &ec2types.ManagedPrefixList{State: "modify-complete"}, nil
					}
					return &ec2types.ManagedPrefixList{State: "modify-in-progress"}, nil
				})
			switch mode {
			case "grace target":
				require.NoError(t, err)
				require.NotNil(t, out)
			case "initial deadline error":
				require.NoError(t, err)
				assert.Equal(t, "bad", string(out.State))
			case "cancel":
				require.ErrorIs(t, err, canceled)
				require.NotNil(t, out)
			case "refresh error":
				require.ErrorIs(t, err, failure)
			default:
				require.ErrorContains(t, err, "timeout")
				require.ErrorContains(t, err, "modify-in-progress")
				assert.Nil(t, out)
				if mode == "deadline error" {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}
			}
			if mode == "grace pending" {
				assert.Equal(t, []time.Duration{200 * time.Millisecond}, clock.sleeps)
				assert.Equal(t, 2, calls)
			}
		})
	}
}

func TestManagedPrefixListWaitResetsAbsenceAndGraceAtCap(t *testing.T) {
	clock := &prefixWaitClock{}
	start := time.Now()
	w := managedPrefixListWaiter{now: func() time.Time { return start.Add(clock.elapsed) },
		sleep: clock.sleep, timeout: 15 * time.Minute}
	calls := 0
	out, err := w.wait(t.Context(), "create-in-progress", "create-complete",
		func(context.Context) (*ec2types.ManagedPrefixList, error) {
			calls++
			if calls == 21 {
				return &ec2types.ManagedPrefixList{State: "create-in-progress"}, nil
			}
			if calls == 42 {
				return &ec2types.ManagedPrefixList{State: "create-complete"}, nil
			}
			return nil, runtime.ErrNotFound
		})
	require.NoError(t, err)
	assert.Equal(t, ec2types.PrefixListStateCreateComplete, out.State)
	assert.Equal(t, 42, calls)
	clock = &prefixWaitClock{}
	w.now = func() time.Time { return start.Add(clock.elapsed) }
	w.sleep = clock.sleep
	w.timeout = 22 * time.Second
	calls = 0
	out, err = w.wait(t.Context(), "create-in-progress", "create-complete",
		func(child context.Context) (*ec2types.ManagedPrefixList, error) {
			calls++
			if clock.elapsed >= w.timeout {
				require.ErrorIs(t, child.Err(), context.DeadlineExceeded)
			}
			return &ec2types.ManagedPrefixList{State: "create-in-progress"}, nil
		})
	require.ErrorContains(t, err, "timeout")
	assert.Nil(t, out)
	assert.Equal(t, []time.Duration{200 * time.Millisecond, 400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond, 3200 * time.Millisecond, 6400 * time.Millisecond, 10 * time.Second,
		10 * time.Second}, clock.sleeps)
	assert.Equal(t, 9, calls)
}

func TestManagedPrefixListWaitErrorAndResultPrecedence(t *testing.T) {
	for _, mode := range []string{"deadline initial exhausted", "ordinary error", "canceled refresh"} {
		t.Run(mode, func(t *testing.T) {
			clock := &prefixWaitClock{}
			now := time.Now()
			w := managedPrefixListWaiter{now: func() time.Time { return now.Add(clock.elapsed) },
				sleep: clock.sleep, timeout: time.Second}
			expected := &ec2types.ManagedPrefixList{State: "error-state"}
			failure := errors.New("ordinary refresh error")
			if mode == "canceled refresh" {
				failure = context.Canceled
			}
			out, err := w.wait(t.Context(), "create-in-progress", "create-complete",
				func(context.Context) (*ec2types.ManagedPrefixList, error) {
					if mode == "deadline initial exhausted" {
						clock.elapsed = time.Second
						return expected, context.DeadlineExceeded
					}
					return expected, failure
				})
			if mode == "deadline initial exhausted" {
				require.ErrorContains(t, err, `last state ""`)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Nil(t, out)
			} else {
				require.ErrorIs(t, err, failure)
				assert.Same(t, expected, out)
			}
		})
	}
}
