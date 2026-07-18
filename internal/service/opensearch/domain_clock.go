package opensearch

import (
	"context"
	"time"
)

const (
	domainCreateTimeout        = 120 * time.Minute
	domainUpdateTimeout        = 180 * time.Minute
	domainDeleteTimeout        = 90 * time.Minute
	domainCreateCleanupTimeout = 210 * time.Minute
	domainPropagationTimeout   = 30 * time.Minute
	domainCreateInitialDelay   = 10 * time.Minute
	domainUpdateInitialDelay   = time.Minute
	domainDeleteInitialDelay   = 10 * time.Minute
	domainUpgradeInitialDelay  = 30 * time.Second
	domainPollInterval         = 10 * time.Second
)

type domainClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type realDomainClock struct{}

func (realDomainClock) Now() time.Time { return time.Now() }

func (realDomainClock) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

type domainOperationOptions struct {
	clock              domainClock
	createTimeout      time.Duration
	updateTimeout      time.Duration
	deleteTimeout      time.Duration
	propagationTimeout time.Duration
}

func (options domainOperationOptions) withDefaults() domainOperationOptions {
	if options.clock == nil {
		options.clock = newDomainClock()
	}
	if options.createTimeout == 0 {
		options.createTimeout = domainCreateTimeout
	}
	if options.updateTimeout == 0 {
		options.updateTimeout = domainUpdateTimeout
	}
	if options.deleteTimeout == 0 {
		options.deleteTimeout = domainDeleteTimeout
	}
	if options.propagationTimeout == 0 {
		options.propagationTimeout = domainPropagationTimeout
	}
	return options
}

func defaultDomainOperationOptions() domainOperationOptions {
	return domainOperationOptions{}.withDefaults()
}
