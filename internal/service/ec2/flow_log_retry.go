package ec2

import (
	"context"
	"errors"
	"strings"
	"time"

	smithy "github.com/aws/smithy-go"
)

type flowLogClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type flowLogWallClock struct{}

func (flowLogWallClock) Now() time.Time { return time.Now() }
func (flowLogWallClock) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryFlowLog(
	ctx context.Context, clock flowLogClock, timeout time.Duration, call func(context.Context) error,
) error {
	deadline := clock.Now().Add(timeout)
	err := call(ctx)
	delay := 500 * time.Millisecond
	grace := false
	for flowLogAssumeError(err) {
		if !clock.Now().Before(deadline) {
			if grace {
				return err
			}
			grace = true
		}
		if err := clock.Sleep(ctx, delay); err != nil {
			return err
		}
		err = call(ctx)
		delay = min(delay*2, 10*time.Second)
	}
	return err
}

func flowLogAssumeError(err error) bool {
	var apiError smithy.APIError
	return errors.As(err, &apiError) && apiError.ErrorCode() == "InvalidParameter" &&
		strings.Contains(apiError.ErrorMessage(), "Unable to assume given IAM role")
}
