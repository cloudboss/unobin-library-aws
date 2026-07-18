package opensearch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
)

func retryDomainOperation(
	ctx context.Context,
	clock domainClock,
	timeout time.Duration,
	operation func(context.Context) error,
) error {
	deadline := clock.Now().Add(timeout)
	var last error
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if attempt > 0 && !clock.Now().Before(deadline) {
			return fmt.Errorf("domain operation timed out after %s: %w", timeout, last)
		}
		last = operation(ctx)
		if last == nil || !isDomainPropagationError(last) {
			return last
		}
		continued, err := sleepDomainBeforeDeadline(
			ctx, clock, deadline, domainRetryDelay(attempt),
		)
		if err != nil {
			return err
		}
		if !continued {
			return fmt.Errorf("domain operation timed out after %s: %w", timeout, last)
		}
	}
}

func isDomainPropagationError(err error) bool {
	if err == nil {
		return false
	}
	var invalidType *awstypes.InvalidTypeException
	if errors.As(err, &invalidType) && strings.Contains(
		invalidType.ErrorMessage(), "Error setting policy",
	) {
		return true
	}
	var validation *awstypes.ValidationException
	if !errors.As(err, &validation) {
		return false
	}
	message := validation.ErrorMessage()
	fragments := [...]string{
		"enable a service-linked role to give Amazon ES permissions",
		"Domain is still being deleted",
		"Amazon OpenSearch Service must be allowed to use the passed role",
		"The passed role has not propagated yet",
		"Authentication error",
		"Unauthorized Operation: OpenSearch Service must be authorised to describe",
		"The passed role must authorize Amazon OpenSearch Service to describe",
		"A change/update is in progress. Please wait for it to complete before " +
			"requesting another change.",
		"The Resource Access Policy specified for the CloudWatch Logs log group",
	}
	for _, fragment := range fragments {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func domainRetryDelay(attempt int) time.Duration {
	delay := 500 * time.Millisecond
	for range attempt {
		delay *= 2
		if delay >= 10*time.Second {
			return 10 * time.Second
		}
	}
	return delay
}

func sleepDomainBeforeDeadline(
	ctx context.Context,
	clock domainClock,
	deadline time.Time,
	delay time.Duration,
) (bool, error) {
	if err := context.Cause(ctx); err != nil {
		return false, err
	}
	remaining := deadline.Sub(clock.Now())
	if remaining <= 0 {
		return false, nil
	}
	continued := delay <= remaining
	if !continued {
		delay = remaining
	}
	if err := clock.Sleep(ctx, delay); err != nil {
		return false, err
	}
	return continued, nil
}
