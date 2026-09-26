package eks

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFargateProfileCreateRetriesRoleTrustAndReadsFinalState(t *testing.T) {
	client := &fakeFargateProfileClient{
		createErrors: []error{fargateProfileRetryableRoleError(), nil},
		describeResults: []fargateProfileDescribeResult{
			fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusCreating)),
			fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
			fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
		},
	}

	output, err := validFargateProfileResource().createFargateProfile(
		t.Context(), client, newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"create", "create", "describe", "describe", "describe"},
		client.calls)
	require.Len(t, client.createInputs, 2)
	assert.NotEmpty(t, aws.ToString(client.createInputs[0].ClientRequestToken))
	assert.Equal(t, "prior-cluster", output.ClusterName)
	assert.Equal(t, "prior-pods", output.FargateProfileName)
	assert.Equal(t, "arn:fargate-profile", output.ARN)
}

func TestFargateProfileCreateReturnsDifferentErrorsImmediately(t *testing.T) {
	sentinel := errors.New("bad role")
	client := &fakeFargateProfileClient{createErrors: []error{sentinel}}

	_, err := validFargateProfileResource().createFargateProfile(
		t.Context(), client, newFakeClusterClock(),
	)

	require.ErrorIs(t, err, sentinel)
	assert.Len(t, client.createInputs, 1)
}

func TestFargateProfileCreateFinalReadFailureRollsBack(t *testing.T) {
	sentinel := errors.New("read denied")
	client := &fakeFargateProfileClient{
		describeResults: []fargateProfileDescribeResult{
			fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
			{err: sentinel},
			{err: fargateProfileNotFoundError()},
		},
	}

	_, err := validFargateProfileResource().createFargateProfile(
		t.Context(), client, newFakeClusterClock(),
	)

	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, []string{"create", "describe", "describe", "delete", "describe"},
		client.calls)
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, "example", aws.ToString(client.deleteInputs[0].ClusterName))
	assert.Equal(t, "pods", aws.ToString(client.deleteInputs[0].FargateProfileName))
}

func TestFargateProfileCreateJoinsRollbackFailure(t *testing.T) {
	rollbackErr := errors.New("delete denied")
	profile := sdkFargateProfile(ekstypes.FargateProfileStatusCreateFailed)
	profile.Health = &ekstypes.FargateProfileHealth{
		Issues: []ekstypes.FargateProfileIssue{{
			Code:        ekstypes.FargateProfileIssueCodePodExecutionRoleAlreadyInUse,
			Message:     aws.String("role conflict"),
			ResourceIds: []string{"pod-role"},
		}},
	}
	client := &fakeFargateProfileClient{
		describeResults: []fargateProfileDescribeResult{
			fargateProfileDescribe(profile),
		},
		deleteErr: rollbackErr,
	}

	_, err := validFargateProfileResource().createFargateProfile(
		t.Context(), client, newFakeClusterClock(),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "CREATE_FAILED")
	assert.ErrorContains(t, err, "pod-role")
	assert.ErrorIs(t, err, rollbackErr)
	require.Len(t, client.deleteInputs, 1)
}

func TestFargateProfileReadUsesPriorHandles(t *testing.T) {
	resource := validFargateProfileResource()
	resource.ClusterName = "replacement-cluster"
	resource.FargateProfileName = "replacement-pods"
	client := &fakeFargateProfileClient{describeResults: []fargateProfileDescribeResult{
		fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
	}}

	_, err := resource.readFargateProfile(
		t.Context(),
		client,
		&FargateProfileResourceOutput{
			ClusterName:        "prior-cluster",
			FargateProfileName: "prior-pods",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.describeInputs, 1)
	assert.Equal(t, "prior-cluster", aws.ToString(client.describeInputs[0].ClusterName))
	assert.Equal(t, "prior-pods", aws.ToString(client.describeInputs[0].FargateProfileName))
}

func TestFargateProfileReadWithoutPriorUsesCurrentNames(t *testing.T) {
	resource := validFargateProfileResource()
	client := &fakeFargateProfileClient{describeResults: []fargateProfileDescribeResult{
		fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
	}}

	_, err := resource.readFargateProfile(t.Context(), client, nil)

	require.NoError(t, err)
	require.Len(t, client.describeInputs, 1)
	assert.Equal(t, "example", aws.ToString(client.describeInputs[0].ClusterName))
	assert.Equal(t, "pods", aws.ToString(client.describeInputs[0].FargateProfileName))
}

func TestFargateProfileUpdateTagsThenReads(t *testing.T) {
	priorInputs := validFargateProfileResource()
	priorInputs.Tags = &map[string]string{"keep": "old", "drop": "value"}
	resource := validFargateProfileResource()
	resource.Tags = &map[string]string{"keep": "new", "add": "value", "aws:x": "skip"}
	client := &fakeFargateProfileClient{
		listTags: map[string]string{
			"keep": "old", "drop": "value", "aws:managed": "yes",
		},
		describeResults: []fargateProfileDescribeResult{
			fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
		},
	}
	prior := runtime.Prior[FargateProfileResource, *FargateProfileResourceOutput]{
		Inputs: priorInputs,
		Outputs: &FargateProfileResourceOutput{
			ClusterName: "prior-cluster", FargateProfileName: "prior-pods",
			ARN: "output-arn",
		},
		Observed: &FargateProfileResourceOutput{ARN: "observed-arn"},
	}

	_, err := resource.updateFargateProfile(t.Context(), client, prior)

	require.NoError(t, err)
	assert.Equal(t, []string{"list-tags", "untag", "tag", "describe"}, client.calls)
	assert.Equal(t, "observed-arn", aws.ToString(client.listTagsInputs[0].ResourceArn))
	assert.Equal(t, []string{"drop"}, client.untagInputs[0].TagKeys)
	assert.Equal(t, map[string]string{"add": "value", "keep": "new"},
		client.tagInputs[0].Tags)
}

func TestFargateProfileUpdateSkipsTagCallsWhenDesiredMatchesLiveTags(t *testing.T) {
	priorInputs := validFargateProfileResource()
	resource := validFargateProfileResource()
	resource.Tags = &map[string]string{"keep": "same", "aws:ignored": "value"}
	client := &fakeFargateProfileClient{
		listTags: map[string]string{"keep": "same", "aws:managed": "value"},
		describeResults: []fargateProfileDescribeResult{
			fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
		},
	}

	_, err := resource.updateFargateProfile(t.Context(), client,
		runtime.Prior[FargateProfileResource, *FargateProfileResourceOutput]{
			Inputs: priorInputs,
			Outputs: &FargateProfileResourceOutput{
				ClusterName: "cluster", FargateProfileName: "pods", ARN: "output-arn",
			},
		},
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"list-tags", "describe"}, client.calls)
}

func TestFargateProfileDeleteIsIdempotentAndWaitsForAbsence(t *testing.T) {
	resource := validFargateProfileResource()
	prior := &FargateProfileResourceOutput{
		ClusterName: "prior-cluster", FargateProfileName: "prior-pods",
	}
	err := resource.deleteFargateProfile(t.Context(),
		&fakeFargateProfileClient{deleteErr: fargateProfileNotFoundError()},
		prior,
		newFakeClusterClock(),
	)
	require.NoError(t, err)

	client := &fakeFargateProfileClient{describeResults: []fargateProfileDescribeResult{
		fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusDeleting)),
		{err: fargateProfileNotFoundError()},
	}}
	err = resource.deleteFargateProfile(t.Context(), client, prior, newFakeClusterClock())

	require.NoError(t, err)
	assert.Equal(t, []string{"delete", "describe", "describe"}, client.calls)
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, "prior-cluster", aws.ToString(client.deleteInputs[0].ClusterName))
	assert.Equal(t, "prior-pods", aws.ToString(client.deleteInputs[0].FargateProfileName))
}

func TestFargateProfileClusterLockKeys(t *testing.T) {
	fargateProfileClusterLocks = sync.Map{}

	assert.Same(t, fargateProfileClusterLock("cluster-a"),
		fargateProfileClusterLock("cluster-a"))
	assert.NotSame(t, fargateProfileClusterLock("cluster-a"),
		fargateProfileClusterLock("cluster-b"))
}

func TestFargateProfileCreateHonorsClusterLock(t *testing.T) {
	fargateProfileClusterLocks = sync.Map{}
	lock := fargateProfileClusterLock("example")
	lock.Lock()
	called := make(chan struct{})
	done := make(chan struct{})
	client := &fakeFargateProfileClient{
		createFn: func(int) { close(called) },
		describeResults: []fargateProfileDescribeResult{
			fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
			fargateProfileDescribe(sdkFargateProfile(ekstypes.FargateProfileStatusActive)),
		},
	}
	go func() {
		_, _ = validFargateProfileResource().createFargateProfile(
			t.Context(), client, newFakeClusterClock(),
		)
		close(done)
	}()

	select {
	case <-called:
		t.Fatal("same-cluster create bypassed lock")
	case <-time.After(20 * time.Millisecond):
	}
	lock.Unlock()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("create did not finish after lock release")
	}
}
