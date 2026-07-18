package eks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeNodeGroupMapsAbsenceForms(t *testing.T) {
	tests := []struct {
		name   string
		result nodeGroupDescribeResult
	}{
		{name: "typed error", result: nodeGroupDescribeResult{err: nodeGroupNotFoundError()}},
		{name: "nil response"},
		{name: "nil node group", result: nodeGroupDescribe(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeNodeGroupClient{describeResults: []nodeGroupDescribeResult{tt.result}}
			_, err := describeNodeGroup(t.Context(), client, "cluster", "workers")
			assert.ErrorIs(t, err, runtime.ErrNotFound)
		})
	}

	sentinel := errors.New("access denied")
	client := &fakeNodeGroupClient{describeResults: []nodeGroupDescribeResult{{err: sentinel}}}
	_, err := describeNodeGroup(t.Context(), client, "cluster", "workers")
	require.ErrorIs(t, err, sentinel)
}

func TestWaitNodeGroupCreatedAllowsTwentyMisses(t *testing.T) {
	results := make([]nodeGroupDescribeResult, 20)
	for index := range results {
		results[index].err = nodeGroupNotFoundError()
	}
	results = append(results,
		nodeGroupDescribe(sdkNodeGroup(ekstypes.NodegroupStatusCreating)),
		nodeGroupDescribe(sdkNodeGroup(ekstypes.NodegroupStatusActive)),
	)
	client := &fakeNodeGroupClient{describeResults: results}

	err := waitNodeGroupCreated(
		t.Context(), client, "prior-cluster", "prior-workers", newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Len(t, client.describeInputs, 22)
}

func TestWaitNodeGroupCreatedRejectsMissLimitAndHealthFailure(t *testing.T) {
	results := make([]nodeGroupDescribeResult, 21)
	for index := range results {
		results[index].err = nodeGroupNotFoundError()
	}
	err := waitNodeGroupCreated(
		t.Context(), &fakeNodeGroupClient{describeResults: results},
		"cluster", "workers", newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "not found after 20")

	failed := sdkNodeGroup(ekstypes.NodegroupStatusCreateFailed)
	failed.Health = &ekstypes.NodegroupHealth{Issues: []ekstypes.Issue{{
		Code:        ekstypes.NodegroupIssueCodeNodeCreationFailure,
		Message:     aws.String("nodes did not register"),
		ResourceIds: []string{"i-123", "asg-workers"},
	}}}
	err = waitNodeGroupCreated(
		t.Context(),
		&fakeNodeGroupClient{describeResults: []nodeGroupDescribeResult{
			nodeGroupDescribe(failed),
		}},
		"cluster", "workers", newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "CREATE_FAILED")
	assert.ErrorContains(t, err,
		"i-123, asg-workers: NodeCreationFailure: nodes did not register")
}

func TestWaitNodeGroupUpdateUsesNodeIdentityAndReportsFailure(t *testing.T) {
	client := &fakeNodeGroupClient{updateResults: []nodeGroupUpdateResult{
		{err: nodeGroupNotFoundError()},
		{output: &eks.DescribeUpdateOutput{Update: &ekstypes.Update{
			Status: ekstypes.UpdateStatusInProgress,
		}}},
		{output: &eks.DescribeUpdateOutput{Update: &ekstypes.Update{
			Status: ekstypes.UpdateStatusSuccessful,
		}}},
	}}
	err := waitNodeGroupUpdate(
		t.Context(), client, "prior-cluster", "prior-workers", "update-1",
		newFakeClusterClock(),
	)
	require.NoError(t, err)
	require.Len(t, client.updateInputs, 3)
	assert.Equal(t, "prior-cluster", aws.ToString(client.updateInputs[0].Name))
	assert.Equal(t, "prior-workers", aws.ToString(client.updateInputs[0].NodegroupName))

	for _, status := range []ekstypes.UpdateStatus{
		ekstypes.UpdateStatusFailed,
		ekstypes.UpdateStatusCancelled,
	} {
		client := &fakeNodeGroupClient{updateResults: []nodeGroupUpdateResult{{
			output: &eks.DescribeUpdateOutput{Update: &ekstypes.Update{
				Status: status,
				Errors: []ekstypes.ErrorDetail{{
					ResourceIds:  []string{"subnet-a"},
					ErrorCode:    ekstypes.ErrorCodeSubnetNotFound,
					ErrorMessage: aws.String("subnet disappeared"),
				}},
			}},
		}}}
		err = waitNodeGroupUpdate(
			t.Context(), client, "cluster", "workers", "update-2",
			newFakeClusterClock(),
		)
		require.Error(t, err)
		assert.ErrorContains(t, err, string(status))
		assert.ErrorContains(t, err, "subnet-a: SubnetNotFound: subnet disappeared")
	}
}

func TestDescribeNodeGroupUpdateMapsEmptyResponsesToNotFound(t *testing.T) {
	for _, result := range []nodeGroupUpdateResult{
		{},
		{output: &eks.DescribeUpdateOutput{}},
	} {
		client := &fakeNodeGroupClient{updateResults: []nodeGroupUpdateResult{result}}
		_, err := describeNodeGroupUpdate(
			t.Context(), client, "cluster", "workers", "update-id",
		)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	}
}

func TestWaitNodeGroupDeletedRequiresAbsence(t *testing.T) {
	client := &fakeNodeGroupClient{describeResults: []nodeGroupDescribeResult{
		nodeGroupDescribe(sdkNodeGroup(ekstypes.NodegroupStatusActive)),
		nodeGroupDescribe(sdkNodeGroup(ekstypes.NodegroupStatusDeleting)),
		{err: nodeGroupNotFoundError()},
	}}

	err := waitNodeGroupDeleted(
		t.Context(), client, "prior-cluster", "prior-workers", newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Len(t, client.describeInputs, 3)

	failed := sdkNodeGroup(ekstypes.NodegroupStatusDeleteFailed)
	failed.Health = &ekstypes.NodegroupHealth{Issues: []ekstypes.Issue{{
		Code:        ekstypes.NodegroupIssueCodeEc2SecurityGroupDeletionFailure,
		Message:     aws.String("group is in use"),
		ResourceIds: []string{"sg-123"},
	}}}
	err = waitNodeGroupDeleted(
		t.Context(),
		&fakeNodeGroupClient{describeResults: []nodeGroupDescribeResult{
			nodeGroupDescribe(failed),
		}},
		"cluster", "workers", newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "DELETE_FAILED")
	assert.ErrorContains(t, err,
		"sg-123: Ec2SecurityGroupDeletionFailure: group is in use")
}

func TestNodeGroupWaitersRespectTimeoutAndCancellation(t *testing.T) {
	clock := newFakeClusterClock()
	client := &fakeNodeGroupClient{describeFallback: &eks.DescribeNodegroupOutput{
		Nodegroup: sdkNodeGroup(ekstypes.NodegroupStatusCreating),
	}}
	err := waitNodeGroupCreated(t.Context(), client, "cluster", "workers", clock)
	require.Error(t, err)
	assert.ErrorContains(t, err, "timed out")
	assert.Equal(t, 60*time.Minute, clock.elapsed())

	clock = newFakeClusterClock()
	client = &fakeNodeGroupClient{updateFallback: &eks.DescribeUpdateOutput{
		Update: &ekstypes.Update{Status: ekstypes.UpdateStatusInProgress},
	}}
	err = waitNodeGroupUpdate(
		t.Context(), client, "cluster", "workers", "update-id", clock,
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "timed out")
	assert.Equal(t, 60*time.Minute, clock.elapsed())

	clock = newFakeClusterClock()
	client = &fakeNodeGroupClient{describeFallback: &eks.DescribeNodegroupOutput{
		Nodegroup: sdkNodeGroup(ekstypes.NodegroupStatusDeleting),
	}}
	err = waitNodeGroupDeleted(t.Context(), client, "cluster", "workers", clock)
	require.Error(t, err)
	assert.ErrorContains(t, err, "timed out")
	assert.Equal(t, 60*time.Minute, clock.elapsed())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = waitNodeGroupDeleted(
		ctx, &fakeNodeGroupClient{}, "cluster", "workers", newFakeClusterClock(),
	)
	assert.ErrorIs(t, err, context.Canceled)
}
