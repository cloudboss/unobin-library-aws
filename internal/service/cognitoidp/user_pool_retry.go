package cognitoidp

import (
	"context"
	"time"
)

const userPoolRetryTimeout = 2 * time.Minute

type userPoolClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type systemUserPoolClock struct{}

func (systemUserPoolClock) Now() time.Time {
	return time.Now()
}

func (systemUserPoolClock) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryUserPool(
	ctx context.Context,
	clock userPoolClock,
	call func(context.Context) error,
) error {
	deadline := clock.Now().Add(userPoolRetryTimeout)
	delay := 500 * time.Millisecond
	for {
		err := call(ctx)
		if err == nil || !isUserPoolRetryable(err) {
			return err
		}
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			return err
		}
		sleep := min(delay, remaining)
		if err := clock.Sleep(ctx, sleep); err != nil {
			return err
		}
		if !clock.Now().Before(deadline) {
			return err
		}
		delay = min(delay*2, 10*time.Second)
	}
}
