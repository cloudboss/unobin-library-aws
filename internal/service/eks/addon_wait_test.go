package eks

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitAddonCreatedToleratesInitialAbsenceAndDegradation(t *testing.T) {
	results := make([]addonDescribeResult, 0, 23)
	for range 20 {
		results = append(results, addonDescribeResult{
			err: &ekstypes.ResourceNotFoundException{Message: aws.String("missing")},
		})
	}
	results = append(results,
		addonDescribeResult{output: addonDescribe(ekstypes.AddonStatusDegraded)},
		addonDescribeResult{output: addonDescribe(ekstypes.AddonStatusActive)},
	)
	client := &fakeAddonClient{describeResults: results}

	err := waitAddonCreated(
		context.Background(), client, "example", "coredns", newFakeClusterClock(),
	)

	require.NoError(t, err)
	assert.Len(t, client.describeInputs, 22)
}

func TestWaitAddonCreatedRejectsPersistentAbsence(t *testing.T) {
	results := make([]addonDescribeResult, 21)
	for index := range results {
		results[index].err = &ekstypes.ResourceNotFoundException{
			Message: aws.String("missing"),
		}
	}
	err := waitAddonCreated(
		context.Background(),
		&fakeAddonClient{describeResults: results},
		"example",
		"coredns",
		newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "not found after 20 checks")
}

func TestWaitAddonCreatedPersistentDegradationTimesOutWithIssues(t *testing.T) {
	clock := newFakeClusterClock()
	degraded := addonDescribeWithIssues(ekstypes.AddonStatusDegraded)
	client := &fakeAddonClient{describeFallback: degraded}

	err := waitAddonCreated(context.Background(), client, "example", "coredns", clock)

	require.Error(t, err)
	assert.ErrorContains(t, err, "timed out after 20m0s")
	assert.ErrorContains(t, err, "deployment/coredns: ConfigurationConflict: conflict")
	assert.Equal(t, addonCreateWaitTimeout, clock.elapsed())
	assert.Contains(t, clock.sleeps, 10*time.Second)
}

func TestWaitAddonCreatedReportsTerminalHealthIssues(t *testing.T) {
	client := &fakeAddonClient{describeResults: []addonDescribeResult{{
		output: addonDescribeWithIssues(ekstypes.AddonStatusCreateFailed),
	}}}
	err := waitAddonCreated(
		context.Background(), client, "example", "coredns", newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "CREATE_FAILED")
	assert.ErrorContains(t, err, "deployment/coredns: ConfigurationConflict: conflict")
}

func TestWaitAddonUpdateTransitionsAndUsesCompositeIdentity(t *testing.T) {
	client := &fakeAddonClient{describeUpdateResults: []addonDescribeUpdateResult{
		{output: addonUpdateDescription("update-1", ekstypes.UpdateStatusInProgress)},
		{output: addonUpdateDescription("update-1", ekstypes.UpdateStatusSuccessful)},
	}}

	err := waitAddonUpdate(
		context.Background(), client, "prior-cluster", "prior-addon", "update-1",
		newFakeClusterClock(),
	)

	require.NoError(t, err)
	require.Len(t, client.describeUpdateInputs, 2)
	input := client.describeUpdateInputs[0]
	assert.Equal(t, "prior-cluster", aws.ToString(input.Name))
	assert.Equal(t, "prior-addon", aws.ToString(input.AddonName))
	assert.Equal(t, "update-1", aws.ToString(input.UpdateId))
}

func TestWaitAddonUpdateReportsEveryFailureDetail(t *testing.T) {
	output := addonUpdateDescription("update-1", ekstypes.UpdateStatusFailed)
	output.Update.Errors = []ekstypes.ErrorDetail{
		{ErrorCode: ekstypes.ErrorCodeAccessDenied, ErrorMessage: aws.String("denied"),
			ResourceIds: []string{"role/a"}},
		{ErrorCode: ekstypes.ErrorCodeConfigurationConflict,
			ErrorMessage: aws.String("conflict"), ResourceIds: []string{"deployment/dns"}},
	}
	client := &fakeAddonClient{describeUpdateResults: []addonDescribeUpdateResult{{
		output: output,
	}}}

	err := waitAddonUpdate(
		context.Background(), client, "example", "coredns", "update-1",
		newFakeClusterClock(),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "role/a: AccessDenied: denied")
	assert.ErrorContains(t, err, "deployment/dns: ConfigurationConflict: conflict")
}

func TestWaitAddonDeletedTransitionsToAbsence(t *testing.T) {
	client := &fakeAddonClient{describeResults: []addonDescribeResult{
		{output: addonDescribe(ekstypes.AddonStatusActive)},
		{output: addonDescribe(ekstypes.AddonStatusDeleting)},
		{err: &ekstypes.ResourceNotFoundException{Message: aws.String("gone")}},
	}}
	err := waitAddonDeleted(
		context.Background(), client, "example", "coredns", newFakeClusterClock(),
	)
	require.NoError(t, err)
}

func TestWaitAddonDeletedTimesOut(t *testing.T) {
	clock := newFakeClusterClock()
	client := &fakeAddonClient{
		describeFallback: addonDescribe(ekstypes.AddonStatusDeleting),
	}
	err := waitAddonDeleted(context.Background(), client, "example", "coredns", clock)
	require.Error(t, err)
	assert.ErrorContains(t, err, "timed out after 40m0s")
	assert.Equal(t, addonDeleteWaitTimeout, clock.elapsed())
}

func TestWaitAddonDeletedReportsHealthFailure(t *testing.T) {
	client := &fakeAddonClient{describeResults: []addonDescribeResult{{
		output: addonDescribeWithIssues(ekstypes.AddonStatusDeleteFailed),
	}}}
	err := waitAddonDeleted(
		context.Background(), client, "example", "coredns", newFakeClusterClock(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "DELETE_FAILED")
	assert.ErrorContains(t, err, "deployment/coredns: ConfigurationConflict: conflict")
}

func TestWaitAddonRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &fakeAddonClient{}
	err := waitAddonCreated(ctx, client, "example", "coredns", newFakeClusterClock())
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, client.describeInputs)
}

func addonUpdateDescription(
	id string,
	status ekstypes.UpdateStatus,
) *eks.DescribeUpdateOutput {
	return &eks.DescribeUpdateOutput{Update: &ekstypes.Update{
		Id: aws.String(id), Status: status,
	}}
}
