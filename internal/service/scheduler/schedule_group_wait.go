package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	scheduleGroupWaitTimeout = 5 * time.Minute
	scheduleGroupInitialPoll = 200 * time.Millisecond
	scheduleGroupStablePoll  = 100 * time.Millisecond
	scheduleGroupMaxPoll     = 10 * time.Second
	scheduleGroupMaxMisses   = 20
)

type systemScheduleGroupClock struct{}

func (systemScheduleGroupClock) Now() time.Time { return time.Now() }

func (systemScheduleGroupClock) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

func waitScheduleGroup(
	ctx context.Context, client scheduleGroupClient, identity ScheduleGroupResourceOutput,
	clock scheduleGroupClock, creating bool,
) error {
	ctx, cancel := context.WithTimeout(ctx, scheduleGroupWaitTimeout)
	defer cancel()
	deadline := clock.Now().Add(scheduleGroupWaitTimeout)

	var delay time.Duration
	var active, misses int
	for clock.Now().Before(deadline) {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		out, err := getScheduleGroup(ctx, client, identity)
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		switch {
		case errors.Is(err, runtime.ErrNotFound):
			if !creating {
				return nil
			}
			misses++
			if misses > scheduleGroupMaxMisses {
				return fmt.Errorf("group missing on %d consecutive checks", misses)
			}
		case err != nil:
			return err
		default:
			misses = 0
			switch out.State {
			case schedulertypes.ScheduleGroupStateActive:
				if creating {
					active++
					if active == 2 {
						return nil
					}
				}
			case schedulertypes.ScheduleGroupStateDeleting:
			default:
				if !creating {
					return fmt.Errorf("unexpected state %s", out.State)
				}
			}
		}

		// Keep the cadence of the first ACTIVE observation until creation settles.
		switch {
		case active == 0 && delay == 0:
			delay = scheduleGroupInitialPoll
		case active == 0:
			delay = min(2*delay, scheduleGroupMaxPoll)
		case delay == 0:
			delay = scheduleGroupStablePoll
		}
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			break
		}
		if err := clock.Sleep(ctx, min(delay, remaining)); err != nil {
			return err
		}
	}
	return context.DeadlineExceeded
}
