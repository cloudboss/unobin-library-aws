// Package wait polls an eventually-consistent resource until it settles.
// AWS creates often return before the new resource is fully visible: a read
// can briefly report it absent, or return it with a field such as an ARN that
// is not yet well-formed.
//
// Until returns on the first ready observation, for a condition that only
// moves one way -- a resource that has appeared does not un-appear under the
// same read. UntilStable requires several ready observations in a row, for a
// condition that can still flap because AWS reads are eventually consistent
// across replicas: one ready read may have hit a caught-up replica while the
// next hits a lagging one. Both are cheap on a settled resource, where the
// first probes already report ready.
package wait

import (
	"context"
	"fmt"
	"time"
)

// config tunes the wait. It is a struct so the timing can be set small in
// tests without waiting real seconds.
type config struct {
	timeout  time.Duration
	interval time.Duration
}

// untilConfig paces Until, which waits for a resource to appear. A few seconds
// between polls keeps it from hammering the API while a create propagates; the
// first poll usually already finds the resource, so this interval rarely
// applies.
func untilConfig() config {
	return config{
		timeout:  2 * time.Minute,
		interval: 5 * time.Second,
	}
}

// stableConfig paces UntilStable, which re-confirms a value that is already
// present. The value is there, so poll quickly to confirm it is consistent;
// a slow interval here is wasted wall-clock. One second matches the poll
// interval the Terraform provider uses for the same confirmation.
func stableConfig() config {
	return config{
		timeout:  2 * time.Minute,
		interval: 1 * time.Second,
	}
}

// Option overrides a wait default.
type Option func(*config)

// WithInterval sets how long the wait sleeps between polls. Until defaults to
// five seconds, suited to a create that propagates over several seconds where
// the first poll usually already finds the resource; a wait for something to
// disappear after a delete settles in about a second, so a shorter interval
// keeps it from sleeping a full five.
func WithInterval(interval time.Duration) Option {
	return func(c *config) { c.interval = interval }
}

func withOptions(cfg config, opts []Option) config {
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// Until polls probe until it reports ready once, then returns. A probe error
// stops the wait at once: a probe returns one to abort, such as turning a
// missing resource that is not merely propagating into a not-found error the
// caller reports as drift. The wait gives up at a fixed timeout; what names
// the resource being waited on in the timeout error.
func Until(
	ctx context.Context,
	what string,
	probe func(context.Context) (ready bool, err error),
	opts ...Option,
) error {
	return until(ctx, what, 1, probe, withOptions(untilConfig(), opts))
}

// UntilStable polls probe until it reports ready on consecutive observations
// in a row, so a single ready read against a caught-up replica does not end
// the wait while the value is not yet consistent everywhere. A not-ready
// observation resets the run. Errors and timeout behave as in Until.
func UntilStable(
	ctx context.Context,
	what string,
	consecutive int,
	probe func(context.Context) (ready bool, err error),
	opts ...Option,
) error {
	return until(ctx, what, consecutive, probe, withOptions(stableConfig(), opts))
}

func until(
	ctx context.Context,
	what string,
	consecutive int,
	probe func(context.Context) (bool, error),
	cfg config,
) error {
	deadline := time.Now().Add(cfg.timeout)
	runs := 0
	for {
		ready, err := probe(ctx)
		if err != nil {
			return err
		}
		if ready {
			runs++
			if runs >= consecutive {
				return nil
			}
		} else {
			runs = 0
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s to be ready", what)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.interval):
		}
	}
}
