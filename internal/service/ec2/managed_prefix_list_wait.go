package ec2

import (
	"context"
	"errors"
	"fmt"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type managedPrefixListWaiter struct {
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
	timeout time.Duration
}

func (w managedPrefixListWaiter) wait(
	ctx context.Context, pending, target string,
	refresh func(context.Context) (*ec2types.ManagedPrefixList, error),
) (*ec2types.ManagedPrefixList, error) {
	deadline := w.now().Add(w.timeout)
	remaining := func() time.Duration { return max(deadline.Sub(w.now()), 0) }
	var result *ec2types.ManagedPrefixList
	var lastErr error
	var state, priorState string
	var delay time.Duration
	missing := 0
	grace := true
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if remaining() == 0 {
				if !grace {
					break
				}
				grace = false
			}
			if delay == 0 {
				delay = 100 * time.Millisecond
			}
			delay = min(delay*2, 10*time.Second)
			if err := w.sleep(ctx, delay); err != nil {
				break
			}
		}
		if context.Cause(ctx) != nil {
			break
		}
		child, cancel := context.WithTimeout(ctx, remaining())
		result, lastErr = refresh(child)
		cancel()
		if errors.Is(lastErr, context.DeadlineExceeded) {
			state = priorState
			break
		}
		if errors.Is(lastErr, runtime.ErrNotFound) {
			result, lastErr = nil, nil
		}
		if lastErr != nil {
			return result, lastErr
		}
		state = ""
		if result != nil {
			state = string(result.State)
		}
		priorState = state
		if result == nil {
			if target == "" {
				return nil, nil
			}
			missing++
			if missing > 20 {
				return nil, fmt.Errorf("not found after %d consecutive observations; expected %q",
					missing, target)
			}
			continue
		}
		missing = 0
		if target != "" && state == target {
			return result, nil
		}
		if state != pending {
			return result, fmt.Errorf("unexpected state %q; expected %q", state, target)
		}
	}
	if remaining() == 0 {
		err := fmt.Errorf("timeout after %s waiting for %q; last state %q", w.timeout, target, state)
		if lastErr != nil {
			err = fmt.Errorf("%w: %w", err, lastErr)
		}
		return nil, err
	}
	return result, context.Cause(ctx)
}
