package sfn

import (
	"context"
	"time"
)

type realStateMachineClock struct{}

func (realStateMachineClock) Now() time.Time {
	return time.Now()
}

func (realStateMachineClock) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryStateMachineCreate(
	ctx context.Context,
	clock stateMachineClock,
	window time.Duration,
	operation func(context.Context) error,
) error {
	deadline := clock.Now().Add(window)
	delay := 500 * time.Millisecond
	for {
		err := operation(ctx)
		if err == nil || !retryableStateMachineCreate(err) {
			return err
		}
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			return err
		}
		sleep := min(delay, remaining)
		if sleepErr := clock.Sleep(ctx, sleep); sleepErr != nil {
			return sleepErr
		}
		if sleep == remaining {
			return err
		}
		delay = min(delay*2, 10*time.Second)
	}
}

func pollStateMachine(
	ctx context.Context,
	clock stateMachineClock,
	window time.Duration,
	initialDelay time.Duration,
	probe func(context.Context) (bool, error),
	timeoutError func() error,
) error {
	deadline := clock.Now().Add(window)
	delay := initialDelay
	for {
		ready, err := probe(ctx)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			return timeoutError()
		}
		sleep := min(delay, remaining)
		if err := clock.Sleep(ctx, sleep); err != nil {
			return err
		}
		if sleep == remaining {
			return timeoutError()
		}
		delay = min(delay*2, 10*time.Second)
	}
}
