package opensearch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func waitDomainCreated(
	ctx context.Context,
	client domainClient,
	name string,
	clock domainClock,
	timeout time.Duration,
) (*awstypes.DomainStatus, error) {
	deadline := clock.Now().Add(timeout)
	if err := sleepDomainInitial(ctx, clock, deadline, domainCreateInitialDelay,
		"domain creation", timeout); err != nil {
		return nil, err
	}
	for {
		status, err := describeDomainStatus(ctx, client, name)
		if errors.Is(err, runtime.ErrNotFound) {
			status = nil
		} else if err != nil {
			return nil, err
		}
		if status != nil {
			if aws.ToBool(status.Deleted) {
				return nil, fmt.Errorf("waiting for domain %s creation: domain is deleted", name)
			}
			ready := !aws.ToBool(status.Processing) &&
				(aws.ToString(status.Endpoint) != "" || len(status.Endpoints) > 0)
			if ready {
				return status, nil
			}
		}
		if err := sleepDomainPoll(ctx, clock, deadline, domainPollInterval,
			"domain creation", timeout); err != nil {
			return nil, err
		}
	}
}

func waitDomainUpdated(
	ctx context.Context,
	client domainClient,
	name string,
	clock domainClock,
	timeout time.Duration,
) (*awstypes.DomainStatus, error) {
	deadline := clock.Now().Add(timeout)
	if err := sleepDomainInitial(ctx, clock, deadline, domainUpdateInitialDelay,
		"domain update", timeout); err != nil {
		return nil, err
	}
	for {
		status, err := describeDomainStatus(ctx, client, name)
		if err != nil {
			return nil, err
		}
		if aws.ToBool(status.Deleted) {
			return nil, fmt.Errorf("waiting for domain %s update: domain is deleted", name)
		}
		if !aws.ToBool(status.Processing) {
			return status, nil
		}
		if err := sleepDomainPoll(ctx, clock, deadline, domainPollInterval,
			"domain update", timeout); err != nil {
			return nil, err
		}
	}
}

func waitDomainUpgraded(
	ctx context.Context,
	client domainClient,
	name string,
	clock domainClock,
	timeout time.Duration,
) error {
	deadline := clock.Now().Add(timeout)
	if err := sleepDomainInitial(ctx, clock, deadline, domainUpgradeInitialDelay,
		"domain upgrade", timeout); err != nil {
		return err
	}
	for {
		output, err := client.GetUpgradeStatus(ctx, &awssdk.GetUpgradeStatusInput{
			DomainName: aws.String(name),
		})
		if err != nil {
			return fmt.Errorf("get domain %s upgrade status: %w", name, err)
		}
		if output == nil {
			return fmt.Errorf("get domain %s upgrade status: empty response", name)
		}
		if output.StepStatus == awstypes.UpgradeStatusSucceeded &&
			output.UpgradeStep == awstypes.UpgradeStepUpgrade {
			return nil
		}
		if output.StepStatus != awstypes.UpgradeStatusInProgress &&
			output.StepStatus != awstypes.UpgradeStatusSucceeded {
			return fmt.Errorf("domain %s upgrade reached status %s at step %s",
				name, output.StepStatus, output.UpgradeStep)
		}
		if err := sleepDomainPoll(ctx, clock, deadline, domainPollInterval,
			"domain upgrade", timeout); err != nil {
			return err
		}
	}
}

func waitDomainDeleted(
	ctx context.Context,
	client domainClient,
	name string,
	clock domainClock,
	timeout time.Duration,
) error {
	deadline := clock.Now().Add(timeout)
	if err := sleepDomainInitial(ctx, clock, deadline, domainDeleteInitialDelay,
		"domain deletion", timeout); err != nil {
		return err
	}
	for {
		status, err := describeDomainStatus(ctx, client, name)
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !aws.ToBool(status.Processing) {
			return nil
		}
		if err := sleepDomainPoll(ctx, clock, deadline, domainPollInterval,
			"domain deletion", timeout); err != nil {
			return err
		}
	}
}

func waitDomainConfigDeleted(
	ctx context.Context,
	client domainClient,
	name string,
	clock domainClock,
	timeout time.Duration,
) error {
	deadline := clock.Now().Add(timeout)
	consecutive := 0
	for {
		_, err := client.DescribeDomainConfig(ctx, &awssdk.DescribeDomainConfigInput{
			DomainName: aws.String(name),
		})
		if isDomainNotFound(err) {
			consecutive++
			if consecutive == 3 {
				return nil
			}
		} else if err != nil {
			return fmt.Errorf("describe domain %s configuration: %w", name, err)
		} else {
			consecutive = 0
		}
		if err := sleepDomainPoll(ctx, clock, deadline, domainPollInterval,
			"domain configuration deletion", timeout); err != nil {
			return err
		}
	}
}

func describeDomainStatus(
	ctx context.Context,
	client domainClient,
	name string,
) (*awstypes.DomainStatus, error) {
	output, err := client.DescribeDomain(ctx, &awssdk.DescribeDomainInput{
		DomainName: aws.String(name),
	})
	if isDomainNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe domain %s: %w", name, err)
	}
	if output == nil || output.DomainStatus == nil {
		return nil, fmt.Errorf("describe domain %s: response has no domain status", name)
	}
	return output.DomainStatus, nil
}

func isDomainNotFound(err error) bool {
	var notFound *awstypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func sleepDomainInitial(
	ctx context.Context,
	clock domainClock,
	deadline time.Time,
	delay time.Duration,
	operation string,
	timeout time.Duration,
) error {
	continued, err := sleepDomainBeforeDeadline(ctx, clock, deadline, delay)
	if err != nil {
		return err
	}
	if !continued {
		return fmt.Errorf("waiting for %s timed out after %s", operation, timeout)
	}
	return nil
}

func sleepDomainPoll(
	ctx context.Context,
	clock domainClock,
	deadline time.Time,
	delay time.Duration,
	operation string,
	timeout time.Duration,
) error {
	return sleepDomainInitial(ctx, clock, deadline, delay, operation, timeout)
}
