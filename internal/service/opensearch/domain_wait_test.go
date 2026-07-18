package opensearch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitDomainCreatedUsesExactTiming(t *testing.T) {
	clock := newRecordingDomainClock()
	responses := []*awstypes.DomainStatus{
		{Processing: aws.Bool(true)},
		{Processing: aws.Bool(false), Endpoint: aws.String("example.test")},
	}
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			status := responses[0]
			responses = responses[1:]
			return &awssdk.DescribeDomainOutput{DomainStatus: status}, nil
		},
	}
	status, err := waitDomainCreated(
		context.Background(), client, "example", clock, domainCreateTimeout,
	)
	require.NoError(t, err)
	assert.Equal(t, "example.test", aws.ToString(status.Endpoint))
	assert.Equal(t, []time.Duration{domainCreateInitialDelay, domainPollInterval},
		clock.sleeps)
}

func TestWaitDomainCreatedAcceptsMappedEndpoint(t *testing.T) {
	clock := newRecordingDomainClock()
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			return &awssdk.DescribeDomainOutput{DomainStatus: &awstypes.DomainStatus{
				Processing: aws.Bool(false),
				Endpoints:  map[string]string{"vpc": "example.test"},
			}}, nil
		},
	}
	_, err := waitDomainCreated(
		context.Background(), client, "example", clock, domainCreateTimeout,
	)
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{domainCreateInitialDelay}, clock.sleeps)
}

func TestWaitDomainUpdatedUsesExactTiming(t *testing.T) {
	clock := newRecordingDomainClock()
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			return &awssdk.DescribeDomainOutput{DomainStatus: &awstypes.DomainStatus{
				Processing: aws.Bool(false),
			}}, nil
		},
	}
	_, err := waitDomainUpdated(
		context.Background(), client, "example", clock, domainUpdateTimeout,
	)
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{domainUpdateInitialDelay}, clock.sleeps)
}

func TestWaitDomainUpgraded(t *testing.T) {
	clock := newRecordingDomainClock()
	statuses := []awstypes.UpgradeStatus{
		awstypes.UpgradeStatusInProgress,
		awstypes.UpgradeStatusSucceeded,
	}
	client := &fakeDomainClient{
		getUpgradeStatus: func(
			context.Context,
			*awssdk.GetUpgradeStatusInput,
		) (*awssdk.GetUpgradeStatusOutput, error) {
			status := statuses[0]
			statuses = statuses[1:]
			return &awssdk.GetUpgradeStatusOutput{
				UpgradeStep: awstypes.UpgradeStepUpgrade,
				StepStatus:  status,
			}, nil
		},
	}
	err := waitDomainUpgraded(
		context.Background(), client, "example", clock, domainUpdateTimeout,
	)
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{domainUpgradeInitialDelay, domainPollInterval},
		clock.sleeps)
}

func TestWaitDomainUpgradedRejectsUnexpectedTerminalState(t *testing.T) {
	clock := newRecordingDomainClock()
	client := &fakeDomainClient{
		getUpgradeStatus: func(
			context.Context,
			*awssdk.GetUpgradeStatusInput,
		) (*awssdk.GetUpgradeStatusOutput, error) {
			return &awssdk.GetUpgradeStatusOutput{
				UpgradeStep: awstypes.UpgradeStepUpgrade,
				StepStatus:  awstypes.UpgradeStatusFailed,
			}, nil
		},
	}
	err := waitDomainUpgraded(
		context.Background(), client, "example", clock, domainUpdateTimeout,
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "FAILED")
}

func TestWaitDomainDeletedRunsBothChecks(t *testing.T) {
	clock := newRecordingDomainClock()
	domainChecks := 0
	configChecks := 0
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			domainChecks++
			if domainChecks == 1 {
				return &awssdk.DescribeDomainOutput{DomainStatus: &awstypes.DomainStatus{
					Processing: aws.Bool(true),
				}}, nil
			}
			return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
		},
		describeDomainConfig: func(
			context.Context,
			*awssdk.DescribeDomainConfigInput,
		) (*awssdk.DescribeDomainConfigOutput, error) {
			configChecks++
			return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
		},
	}
	require.NoError(t, waitDomainDeleted(
		context.Background(), client, "example", clock, domainDeleteTimeout,
	))
	require.NoError(t, waitDomainConfigDeleted(
		context.Background(), client, "example", clock, domainDeleteTimeout,
	))
	assert.Equal(t, 2, domainChecks)
	assert.Equal(t, 3, configChecks)
	assert.Equal(t, []time.Duration{
		domainDeleteInitialDelay,
		domainPollInterval,
		domainPollInterval,
		domainPollInterval,
	}, clock.sleeps)
}

func TestWaitDomainConfigDeletedResetsConsecutiveCount(t *testing.T) {
	clock := newRecordingDomainClock()
	responses := []error{
		&awstypes.ResourceNotFoundException{Message: aws.String("missing")},
		nil,
		&awstypes.ResourceNotFoundException{Message: aws.String("missing")},
		&awstypes.ResourceNotFoundException{Message: aws.String("missing")},
		&awstypes.ResourceNotFoundException{Message: aws.String("missing")},
	}
	client := &fakeDomainClient{
		describeDomainConfig: func(
			context.Context,
			*awssdk.DescribeDomainConfigInput,
		) (*awssdk.DescribeDomainConfigOutput, error) {
			err := responses[0]
			responses = responses[1:]
			return &awssdk.DescribeDomainConfigOutput{}, err
		},
	}
	require.NoError(t, waitDomainConfigDeleted(
		context.Background(), client, "example", clock, domainDeleteTimeout,
	))
	assert.Equal(t, []time.Duration{
		domainPollInterval,
		domainPollInterval,
		domainPollInterval,
		domainPollInterval,
	}, clock.sleeps)
}

func TestWaitDomainCreatedPropagatesClockError(t *testing.T) {
	clock := newRecordingDomainClock()
	clock.err = errors.New("clock failed")
	_, err := waitDomainCreated(
		context.Background(), &fakeDomainClient{}, "example", clock, domainCreateTimeout,
	)
	require.ErrorIs(t, err, clock.err)
}

func TestDomainWaitersTimeOut(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		run       func(context.Context, domainClient, domainClock, time.Duration) error
	}{
		{
			name:      "create",
			operation: "domain creation",
			run: func(
				ctx context.Context,
				client domainClient,
				clock domainClock,
				timeout time.Duration,
			) error {
				_, err := waitDomainCreated(ctx, client, "example", clock, timeout)
				return err
			},
		},
		{
			name:      "update",
			operation: "domain update",
			run: func(
				ctx context.Context,
				client domainClient,
				clock domainClock,
				timeout time.Duration,
			) error {
				_, err := waitDomainUpdated(ctx, client, "example", clock, timeout)
				return err
			},
		},
		{
			name:      "upgrade",
			operation: "domain upgrade",
			run: func(
				ctx context.Context,
				client domainClient,
				clock domainClock,
				timeout time.Duration,
			) error {
				return waitDomainUpgraded(ctx, client, "example", clock, timeout)
			},
		},
		{
			name:      "delete",
			operation: "domain deletion",
			run: func(
				ctx context.Context,
				client domainClient,
				clock domainClock,
				timeout time.Duration,
			) error {
				return waitDomainDeleted(ctx, client, "example", clock, timeout)
			},
		},
		{
			name:      "configuration delete",
			operation: "domain configuration deletion",
			run: func(
				ctx context.Context,
				client domainClient,
				clock domainClock,
				timeout time.Duration,
			) error {
				return waitDomainConfigDeleted(ctx, client, "example", clock, timeout)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newRecordingDomainClock()
			err := test.run(context.Background(), &fakeDomainClient{}, clock, time.Second)
			require.Error(t, err)
			assert.ErrorContains(t, err, "waiting for "+test.operation+" timed out")
			assert.Equal(t, []time.Duration{time.Second}, clock.sleeps)
		})
	}
}

func TestDomainWaitersPropagateCancellation(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context, domainClient, domainClock) error
	}{
		{
			name: "create",
			run: func(ctx context.Context, client domainClient, clock domainClock) error {
				_, err := waitDomainCreated(
					ctx, client, "example", clock, domainCreateTimeout,
				)
				return err
			},
		},
		{
			name: "update",
			run: func(ctx context.Context, client domainClient, clock domainClock) error {
				_, err := waitDomainUpdated(
					ctx, client, "example", clock, domainUpdateTimeout,
				)
				return err
			},
		},
		{
			name: "upgrade",
			run: func(ctx context.Context, client domainClient, clock domainClock) error {
				return waitDomainUpgraded(
					ctx, client, "example", clock, domainUpdateTimeout,
				)
			},
		},
		{
			name: "delete",
			run: func(ctx context.Context, client domainClient, clock domainClock) error {
				return waitDomainDeleted(
					ctx, client, "example", clock, domainDeleteTimeout,
				)
			},
		},
		{
			name: "configuration delete",
			run: func(ctx context.Context, client domainClient, clock domainClock) error {
				return waitDomainConfigDeleted(
					ctx, client, "example", clock, domainDeleteTimeout,
				)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wantErr := errors.New("canceled by test")
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(wantErr)
			clock := newRecordingDomainClock()
			err := test.run(ctx, &fakeDomainClient{}, clock)
			require.ErrorIs(t, err, wantErr)
			assert.Empty(t, clock.sleeps)
		})
	}
}
