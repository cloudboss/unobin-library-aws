package codebuild

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
)

type retryClock struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func realRetryClock() retryClock {
	return retryClock{now: time.Now, sleep: func(ctx context.Context, delay time.Duration) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}}
}

func propagate(ctx context.Context, clock retryClock, call func(context.Context) error) error {
	deadline := clock.now().Add(2 * time.Minute)
	delay := 500 * time.Millisecond
	var last error
	grace := true
	first := true
	for {
		if err := ctx.Err(); err != nil {
			if last != nil {
				return last
			}
			return err
		}
		if !first {
			if !clock.now().Before(deadline) {
				if !grace {
					return last
				}
				grace = false
			}
			if err := clock.sleep(ctx, delay); err != nil {
				if last != nil {
					return last
				}
				return err
			}
			delay = min(delay*2, 10*time.Second)
		}
		first = false
		last = call(ctx)
		if last == nil {
			return nil
		}
		var invalid *types.InvalidInputException
		if !errors.As(last, &invalid) ||
			!strings.Contains(invalid.ErrorMessage(), "ot authorized to perform") {
			return last
		}
	}
}
