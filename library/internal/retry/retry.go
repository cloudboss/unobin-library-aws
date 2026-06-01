// Package retry re-attempts an AWS call that fails with a transient,
// self-clearing error. Some AWS operations briefly fail right after a related
// change: IAM raises ConcurrentModification when two mutations of one entity
// race, and rejects a role whose trust-policy principal or whose own name was
// created moments earlier and has not propagated. The call succeeds once the
// service catches up, so OnError retries it over a bounded window. Only the
// caller knows which errors are transient, so it passes a predicate.
package retry

import (
	"context"
	"time"
)

// config tunes OnError. It is a struct so the timing can be set small in tests
// without waiting real seconds.
type config struct {
	timeout  time.Duration
	interval time.Duration
}

func defaultConfig() config {
	return config{
		timeout:  2 * time.Minute,
		interval: 5 * time.Second,
	}
}

// Option overrides a retry default.
type Option func(*config)

// WithTimeout sets how long OnError keeps retrying before it gives up and
// returns the last error. The default is two minutes; a slow-propagating
// dependency may warrant a longer window.
func WithTimeout(timeout time.Duration) Option {
	return func(c *config) { c.timeout = timeout }
}

// OnError runs fn and returns its result. When fn returns an error that
// retryable reports as transient, it waits and runs fn again, until fn
// succeeds, returns an error retryable does not recognize, or the timeout
// elapses. A non-retryable error returns at once, so a real failure is not
// hidden behind the full window. The last error is returned on timeout.
func OnError(
	ctx context.Context,
	retryable func(error) bool,
	fn func(context.Context) error,
	opts ...Option,
) error {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return onError(ctx, retryable, fn, cfg)
}

func onError(
	ctx context.Context,
	retryable func(error) bool,
	fn func(context.Context) error,
	cfg config,
) error {
	deadline := time.Now().Add(cfg.timeout)
	for {
		err := fn(ctx)
		if err == nil || !retryable(err) {
			return err
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.interval):
		}
	}
}
