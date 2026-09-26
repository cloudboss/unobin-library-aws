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

func TestDescribeFargateProfileMapsAbsenceForms(t *testing.T) {
	tests := []struct {
		name   string
		result fargateProfileDescribeResult
	}{
		{name: "typed error", result: fargateProfileDescribeResult{
			err: fargateProfileNotFoundError(),
		}},
		{name: "nil response"},
		{name: "nil profile", result: fargateProfileDescribe(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeFargateProfileClient{
				describeResults: []fargateProfileDescribeResult{tt.result},
			}
			_, err := describeFargateProfile(t.Context(), client, "cluster", "pods")
			assert.ErrorIs(t, err, runtime.ErrNotFound)
		})
	}

	sentinel := errors.New("access denied")
	client := &fakeFargateProfileClient{
		describeResults: []fargateProfileDescribeResult{{err: sentinel}},
	}
	_, err := describeFargateProfile(t.Context(), client, "cluster", "pods")
	require.ErrorIs(t, err, sentinel)
}

func TestWaitFargateProfileCreated(t *testing.T) {
	client := &fakeFargateProfileClient{describeResults: []fargateProfileDescribeResult{
		fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusCreating)),
		fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
	}}

	err := waitFargateProfileCreated(
		t.Context(), client, "prior-cluster", "prior-pods", newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Len(t, client.describeInputs, 2)

	failed := sdkFargateProfile(ekstypes.FargateProfileStatusCreateFailed)
	failed.Health = &ekstypes.FargateProfileHealth{
		Issues: []ekstypes.FargateProfileIssue{{
			Code:        ekstypes.FargateProfileIssueCodeAccessDenied,
			Message:     aws.String("role denied"),
			ResourceIds: []string{"role/pods"},
		}},
	}
	err = waitFargateProfileCreated(
		t.Context(),
		&fakeFargateProfileClient{describeResults: []fargateProfileDescribeResult{
			fargateProfileDescribe(failed),
		}},
		"cluster",
		"pods",
		newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "CREATE_FAILED")
	assert.ErrorContains(t, err, "role/pods: AccessDenied: role denied")
}

func TestWaitFargateProfileDeleted(t *testing.T) {
	client := &fakeFargateProfileClient{describeResults: []fargateProfileDescribeResult{
		fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
		fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusDeleting)),
		{err: fargateProfileNotFoundError()},
	}}

	err := waitFargateProfileDeleted(
		t.Context(), client, "prior-cluster", "prior-pods", newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Len(t, client.describeInputs, 3)

	failed := sdkFargateProfile(ekstypes.FargateProfileStatusDeleteFailed)
	failed.Health = &ekstypes.FargateProfileHealth{
		Issues: []ekstypes.FargateProfileIssue{{
			Code:        ekstypes.FargateProfileIssueCodeClusterUnreachable,
			Message:     aws.String("cluster gone"),
			ResourceIds: []string{"example"},
		}},
	}
	err = waitFargateProfileDeleted(
		t.Context(),
		&fakeFargateProfileClient{describeResults: []fargateProfileDescribeResult{
			fargateProfileDescribe(failed),
		}},
		"cluster",
		"pods",
		newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "DELETE_FAILED")
	assert.ErrorContains(t, err, "example: ClusterUnreachable: cluster gone")
}

func TestFargateProfileWaitersRespectTimeoutAndCancellation(t *testing.T) {
	clock := newFakeClusterClock()
	client := &fakeFargateProfileClient{describeFallback: &eks.DescribeFargateProfileOutput{
		FargateProfile: sdkFargateProfile(ekstypes.FargateProfileStatusCreating),
	}}
	err := waitFargateProfileCreated(t.Context(), client, "cluster", "pods", clock)
	require.Error(t, err)
	assert.ErrorContains(t, err, "timed out")
	assert.Equal(t, 10*time.Minute, clock.elapsed())

	clock = newFakeClusterClock()
	client = &fakeFargateProfileClient{describeFallback: &eks.DescribeFargateProfileOutput{
		FargateProfile: sdkFargateProfile(ekstypes.FargateProfileStatusDeleting),
	}}
	err = waitFargateProfileDeleted(t.Context(), client, "cluster", "pods", clock)
	require.Error(t, err)
	assert.ErrorContains(t, err, "timed out")
	assert.Equal(t, 10*time.Minute, clock.elapsed())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = waitFargateProfileDeleted(
		ctx, &fakeFargateProfileClient{}, "cluster", "pods", newFakeClusterClock(),
	)
	assert.ErrorIs(t, err, context.Canceled)
}
