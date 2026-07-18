package eks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterCreateRetryClassificationAndTimeout(t *testing.T) {
	messages := []string{
		"role does not exist",
		"Error in role params",
		"Role could not be assumed because the trusted entity is not correct",
		"The provided role doesn't have the Amazon EKS Managed Policies associated with it",
		"IAM role's policy must include ec2:DescribeSubnets",
	}
	for _, message := range messages {
		t.Run(message, func(t *testing.T) {
			err := &ekstypes.InvalidParameterException{Message: aws.String(message)}
			assert.True(t, isClusterCreateRetryable(err))
		})
	}
	assert.False(t, isClusterCreateRetryable(
		&ekstypes.InvalidParameterException{Message: aws.String("error in role params")},
	))
	assert.False(t, isClusterCreateRetryable(errors.New("role does not exist")))

	clock := newFakeClusterClock()
	sentinel := &ekstypes.InvalidParameterException{Message: aws.String("role does not exist")}
	calls := 0
	err := retryClusterCreate(context.Background(), clock, func(context.Context) error {
		calls++
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	assert.Greater(t, calls, 1)
	assert.Equal(t, clusterCreateRetryTimeout, clock.elapsed())
}

func TestWaitClusterCreatedToleratesInitialNotFound(t *testing.T) {
	client := &fakeClusterDescriber{
		results: []clusterDescribeResult{
			{err: clusterNotFoundError()},
			{err: clusterNotFoundError()},
			{cluster: &ekstypes.Cluster{Status: ekstypes.ClusterStatusPending}},
			{cluster: &ekstypes.Cluster{Status: ekstypes.ClusterStatusCreating}},
			{cluster: &ekstypes.Cluster{Status: ekstypes.ClusterStatusActive}},
		},
	}

	err := waitClusterCreated(context.Background(), client, "example", newFakeClusterClock())

	require.NoError(t, err)
	assert.Equal(t, 5, client.calls)
}

func TestWaitClusterCreatedRejectsTooManyMissesAndUnexpectedStatus(t *testing.T) {
	misses := make([]clusterDescribeResult, 21)
	for index := range misses {
		misses[index].err = clusterNotFoundError()
	}
	err := waitClusterCreated(
		context.Background(),
		&fakeClusterDescriber{results: misses},
		"example",
		newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "not found after 20")

	err = waitClusterCreated(
		context.Background(),
		&fakeClusterDescriber{results: []clusterDescribeResult{{
			cluster: &ekstypes.Cluster{Status: ekstypes.ClusterStatusFailed},
		}}},
		"example",
		newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "FAILED")
}

func TestWaitClusterUpdateSerialStateAndFailureDetails(t *testing.T) {
	client := &fakeUpdateDescriber{results: []updateDescribeResult{
		{err: clusterNotFoundError()},
		{update: &ekstypes.Update{Status: ekstypes.UpdateStatusInProgress}},
		{update: &ekstypes.Update{Status: ekstypes.UpdateStatusSuccessful}},
	}}
	require.NoError(t, waitClusterUpdate(
		context.Background(), client, "example", "update-1", newFakeClusterClock(),
	))
	assert.Equal(t, 3, client.calls)

	failed := &fakeUpdateDescriber{results: []updateDescribeResult{{
		update: &ekstypes.Update{
			Status: ekstypes.UpdateStatusFailed,
			Errors: []ekstypes.ErrorDetail{
				{
					ResourceIds:  []string{"subnet-a", "subnet-b"},
					ErrorCode:    ekstypes.ErrorCodeSubnetNotFound,
					ErrorMessage: aws.String("missing subnet"),
				},
				{
					ResourceIds:  []string{"sg-a"},
					ErrorCode:    ekstypes.ErrorCodeSecurityGroupNotFound,
					ErrorMessage: aws.String("missing group"),
				},
			},
		},
	}}}
	err := waitClusterUpdate(
		context.Background(), failed, "example", "update-2", newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "subnet-a, subnet-b: SubnetNotFound: missing subnet")
	assert.ErrorContains(t, err, "sg-a: SecurityGroupNotFound: missing group")
}

func TestDeleteClusterRetriesOnlyInProgressAndUsesCadence(t *testing.T) {
	clock := newFakeClusterClock()
	clock.randomDelay = 17 * time.Second
	client := &fakeClusterDeleter{errors: []error{
		&ekstypes.ResourceInUseException{Message: aws.String("update in progress")},
		nil,
	}}

	accepted, err := deleteCluster(context.Background(), client, "example", clock)

	require.NoError(t, err)
	assert.True(t, accepted)
	assert.Equal(t, []time.Duration{17 * time.Second, 30 * time.Second}, clock.sleeps)

	unrelated := &ekstypes.ResourceInUseException{Message: aws.String("deletion protection")}
	accepted, err = deleteCluster(
		context.Background(),
		&fakeClusterDeleter{errors: []error{unrelated}},
		"example",
		newFakeClusterClock(),
	)
	assert.False(t, accepted)
	require.ErrorIs(t, err, unrelated)
}

func TestWaitClusterDeletedNeedsThreeConsecutiveMisses(t *testing.T) {
	client := &fakeClusterDescriber{results: []clusterDescribeResult{
		{err: clusterNotFoundError()},
		{err: clusterNotFoundError()},
		{cluster: &ekstypes.Cluster{Status: ekstypes.ClusterStatusDeleting}},
		{err: clusterNotFoundError()},
		{err: clusterNotFoundError()},
		{err: clusterNotFoundError()},
	}}
	clock := newFakeClusterClock()

	err := waitClusterDeleted(context.Background(), client, "example", clock)

	require.NoError(t, err)
	assert.Equal(t, 6, client.calls)
	assert.Equal(t, 50*time.Second, clock.elapsed())
}

func TestClusterWaitHelpersPreserveCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clock := newFakeClusterClock()
	called := false
	err := retryClusterCreate(ctx, clock, func(context.Context) error {
		called = true
		return nil
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, called)

	err = waitClusterDeleted(
		ctx,
		&fakeClusterDescriber{results: []clusterDescribeResult{{err: clusterNotFoundError()}}},
		"example",
		clock,
	)
	assert.ErrorIs(t, err, context.Canceled)
}

type fakeClusterClock struct {
	start       time.Time
	now         time.Time
	randomDelay time.Duration
	sleeps      []time.Duration
}

func newFakeClusterClock() *fakeClusterClock {
	start := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	return &fakeClusterClock{start: start, now: start}
}

func (c *fakeClusterClock) Now() time.Time { return c.now }

func (c *fakeClusterClock) Sleep(ctx context.Context, delay time.Duration) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	c.sleeps = append(c.sleeps, delay)
	c.now = c.now.Add(delay)
	return nil
}

func (c *fakeClusterClock) RandomDelay(time.Duration) time.Duration { return c.randomDelay }

func (c *fakeClusterClock) elapsed() time.Duration { return c.now.Sub(c.start) }

type clusterDescribeResult struct {
	cluster *ekstypes.Cluster
	err     error
}

type fakeClusterDescriber struct {
	results []clusterDescribeResult
	calls   int
}

func (c *fakeClusterDescriber) DescribeCluster(
	_ context.Context,
	_ *eks.DescribeClusterInput,
	_ ...func(*eks.Options),
) (*eks.DescribeClusterOutput, error) {
	index := c.calls
	c.calls++
	if index >= len(c.results) {
		return nil, errors.New("unexpected DescribeCluster call")
	}
	result := c.results[index]
	if result.err != nil {
		return nil, result.err
	}
	return &eks.DescribeClusterOutput{Cluster: result.cluster}, nil
}

type updateDescribeResult struct {
	update *ekstypes.Update
	err    error
}

type fakeUpdateDescriber struct {
	results []updateDescribeResult
	calls   int
}

func (c *fakeUpdateDescriber) DescribeUpdate(
	_ context.Context,
	_ *eks.DescribeUpdateInput,
	_ ...func(*eks.Options),
) (*eks.DescribeUpdateOutput, error) {
	index := c.calls
	c.calls++
	if index >= len(c.results) {
		return nil, errors.New("unexpected DescribeUpdate call")
	}
	result := c.results[index]
	if result.err != nil {
		return nil, result.err
	}
	return &eks.DescribeUpdateOutput{Update: result.update}, nil
}

type fakeClusterDeleter struct {
	errors []error
	calls  int
}

func (c *fakeClusterDeleter) DeleteCluster(
	_ context.Context,
	_ *eks.DeleteClusterInput,
	_ ...func(*eks.Options),
) (*eks.DeleteClusterOutput, error) {
	index := c.calls
	c.calls++
	if index >= len(c.errors) {
		return nil, errors.New("unexpected DeleteCluster call")
	}
	return &eks.DeleteClusterOutput{}, c.errors[index]
}

func clusterNotFoundError() error {
	return &ekstypes.ResourceNotFoundException{Message: aws.String("missing")}
}
