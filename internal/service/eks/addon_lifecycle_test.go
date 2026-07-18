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

func TestAddonCreateRetriesWithStableTokenThenReadsFinalState(t *testing.T) {
	clock := newFakeClusterClock()
	client := &fakeAddonClient{
		createResults: []addonCreateResult{
			{err: addonInvalidParameter("CREATE_FAILED")},
			{err: addonInvalidParameter("cluster does not exist")},
			{output: &eks.CreateAddonOutput{}},
		},
		describeResults: []addonDescribeResult{
			{output: addonDescribe(ekstypes.AddonStatusCreating)},
			{output: addonDescribe(ekstypes.AddonStatusActive)},
			{output: addonDescribe(ekstypes.AddonStatusActive)},
		},
	}
	resource := validAddonResource()
	token := "11111111-1111-4111-8111-111111111111"

	output, err := resource.createAddon(
		context.Background(), client, clock, fixedAddonToken(token),
	)

	require.NoError(t, err)
	require.Len(t, client.createInputs, 3)
	for _, input := range client.createInputs {
		assert.Equal(t, token, aws.ToString(input.ClientRequestToken))
	}
	assert.Equal(t, "example", output.ClusterName)
	assert.Equal(t, "coredns", output.AddonName)
	assert.Equal(t, "arn:addon", output.ARN)
	assert.Equal(t, "v1.12.0-eksbuild.1", output.AddonVersion)
	assert.Equal(t, "kube-system", aws.ToString(output.Namespace))
	assert.Equal(t, "2026-07-17T12:30:00Z", output.CreatedAt)
	assert.Equal(t, "2026-07-17T12:31:00Z", output.ModifiedAt)
}

func TestAddonCreateFailureBeforeAcceptanceDoesNotRollback(t *testing.T) {
	sentinel := errors.New("access denied")
	client := &fakeAddonClient{createResults: []addonCreateResult{{err: sentinel}}}
	resource := validAddonResource()

	_, err := resource.createAddon(
		context.Background(), client, newFakeClusterClock(), fixedAddonToken("token"),
	)

	require.ErrorIs(t, err, sentinel)
	assert.Empty(t, client.deleteInputs)
}

func TestAddonCreateRetryClassificationIsExactAndCaseSensitive(t *testing.T) {
	for _, test := range []struct {
		message string
		want    bool
	}{
		{message: "CREATE_FAILED", want: true},
		{message: "resource does not exist", want: true},
		{message: "create_failed", want: false},
		{message: "Does not exist", want: false},
		{message: "CREATE FAILED", want: false},
	} {
		assert.Equal(t, test.want, isAddonCreateRetryable(addonInvalidParameter(test.message)))
	}
	assert.False(t, isAddonCreateRetryable(errors.New("CREATE_FAILED")))
}

func TestAddonCreateRetryTimesOutAtTwoMinutes(t *testing.T) {
	clock := newFakeClusterClock()
	calls := 0
	err := retryAddonCreate(context.Background(), clock, func(context.Context) error {
		calls++
		return addonInvalidParameter("CREATE_FAILED")
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "timed out after 2m0s")
	assert.Equal(t, addonCreateRetryTimeout, clock.elapsed())
	assert.Greater(t, calls, 1)
}

func TestAddonCreateCancellationAfterAcceptanceRollsBackWithPreserve(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeAddonClient{
		createResults: []addonCreateResult{{output: &eks.CreateAddonOutput{}}},
		createHook:    cancel,
		describeResults: []addonDescribeResult{{
			err: &ekstypes.ResourceNotFoundException{Message: aws.String("gone")},
		}},
	}
	resource := validAddonResource()

	_, err := resource.createAddon(ctx, client, newFakeClusterClock(), fixedAddonToken("token"))

	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, client.deleteInputs, 1)
	assert.True(t, client.deleteInputs[0].Preserve)
	assert.Equal(t, "example", aws.ToString(client.deleteInputs[0].ClusterName))
	assert.Equal(t, "coredns", aws.ToString(client.deleteInputs[0].AddonName))
}

func TestAddonCreateTerminalFailureRollsBackAndReturnsOriginalError(t *testing.T) {
	client := &fakeAddonClient{
		createResults: []addonCreateResult{{output: &eks.CreateAddonOutput{}}},
		describeResults: []addonDescribeResult{
			{output: addonDescribeWithIssues(ekstypes.AddonStatusCreateFailed)},
			{err: &ekstypes.ResourceNotFoundException{Message: aws.String("gone")}},
		},
	}
	resource := validAddonResource()

	_, err := resource.createAddon(
		context.Background(), client, newFakeClusterClock(), fixedAddonToken("token"),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "CREATE_FAILED")
	assert.ErrorContains(t, err, "deployment/coredns: ConfigurationConflict: conflict")
	require.Len(t, client.deleteInputs, 1)
	assert.True(t, client.deleteInputs[0].Preserve)
}

func TestAddonCreateJoinsRollbackFailure(t *testing.T) {
	rollbackErr := errors.New("delete denied")
	client := &fakeAddonClient{
		createResults: []addonCreateResult{{output: &eks.CreateAddonOutput{}}},
		describeResults: []addonDescribeResult{{
			output: addonDescribeWithIssues(ekstypes.AddonStatusCreateFailed),
		}},
		deleteResults: []addonDeleteResult{{err: rollbackErr}},
	}
	resource := validAddonResource()

	_, err := resource.createAddon(
		context.Background(), client, newFakeClusterClock(), fixedAddonToken("token"),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "CREATE_FAILED")
	assert.ErrorIs(t, err, rollbackErr)
}

func TestAddonCreateFinalReadFailureRollsBack(t *testing.T) {
	sentinel := errors.New("read denied")
	client := &fakeAddonClient{
		createResults: []addonCreateResult{{output: &eks.CreateAddonOutput{}}},
		describeResults: []addonDescribeResult{
			{output: addonDescribe(ekstypes.AddonStatusActive)},
			{err: sentinel},
			{err: &ekstypes.ResourceNotFoundException{Message: aws.String("gone")}},
		},
	}
	resource := validAddonResource()

	_, err := resource.createAddon(
		context.Background(), client, newFakeClusterClock(), fixedAddonToken("token"),
	)

	assert.ErrorIs(t, err, sentinel)
	require.Len(t, client.deleteInputs, 1)
	assert.True(t, client.deleteInputs[0].Preserve)
}

func TestAddonReadAndDeleteUsePriorIdentity(t *testing.T) {
	resource := AddonResource{ClusterName: "new-cluster", AddonName: "new-addon"}
	prior := &AddonResourceOutput{
		ClusterName: "old-cluster", AddonName: "old-addon", ARN: "arn:old",
	}
	client := &fakeAddonClient{describeResults: []addonDescribeResult{
		{output: addonDescribe(ekstypes.AddonStatusActive)},
		{err: &ekstypes.ResourceNotFoundException{Message: aws.String("gone")}},
	}}

	_, err := resource.readAddon(context.Background(), client, prior)
	require.NoError(t, err)
	require.NoError(t, resource.deleteAddon(
		context.Background(), client, prior, newFakeClusterClock(),
	))

	require.Len(t, client.describeInputs, 2)
	cluster, addon := addonInputNames(client.describeInputs[0])
	assert.Equal(t, "old-cluster", cluster)
	assert.Equal(t, "old-addon", addon)
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, "old-cluster", aws.ToString(client.deleteInputs[0].ClusterName))
	assert.Equal(t, "old-addon", aws.ToString(client.deleteInputs[0].AddonName))
	assert.False(t, client.deleteInputs[0].Preserve)
}

func TestAddonDeletePreservesOnlyWhenRequested(t *testing.T) {
	resource := validAddonResource()
	resource.Preserve = true
	prior := &AddonResourceOutput{ClusterName: "example", AddonName: "coredns"}
	client := &fakeAddonClient{describeResults: []addonDescribeResult{{
		err: &ekstypes.ResourceNotFoundException{Message: aws.String("gone")},
	}}}

	require.NoError(t, resource.deleteAddon(
		context.Background(), client, prior, newFakeClusterClock(),
	))

	require.Len(t, client.deleteInputs, 1)
	assert.True(t, client.deleteInputs[0].Preserve)
}

func TestAddonDescribeNotFoundAndNilAreAbsent(t *testing.T) {
	resource := validAddonResource()
	for _, result := range []addonDescribeResult{
		{err: &ekstypes.ResourceNotFoundException{Message: aws.String("gone")}},
		{output: &eks.DescribeAddonOutput{}},
	} {
		client := &fakeAddonClient{describeResults: []addonDescribeResult{result}}
		_, err := resource.readAddon(context.Background(), client, nil)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	}
}

func addonInvalidParameter(message string) error {
	return &ekstypes.InvalidParameterException{Message: aws.String(message)}
}

func addonDescribe(status ekstypes.AddonStatus) *eks.DescribeAddonOutput {
	created := time.Date(2026, time.July, 17, 12, 30, 0, 0, time.UTC)
	modified := created.Add(time.Minute)
	return &eks.DescribeAddonOutput{Addon: &ekstypes.Addon{
		AddonArn:     aws.String("arn:addon"),
		AddonName:    aws.String("coredns"),
		AddonVersion: aws.String("v1.12.0-eksbuild.1"),
		ClusterName:  aws.String("example"),
		CreatedAt:    &created,
		ModifiedAt:   &modified,
		NamespaceConfig: &ekstypes.AddonNamespaceConfigResponse{
			Namespace: aws.String("kube-system"),
		},
		Status: status,
	}}
}

func addonDescribeWithIssues(status ekstypes.AddonStatus) *eks.DescribeAddonOutput {
	output := addonDescribe(status)
	output.Addon.Health = &ekstypes.AddonHealth{Issues: []ekstypes.AddonIssue{{
		Code:        ekstypes.AddonIssueCodeConfigurationConflict,
		Message:     aws.String("conflict"),
		ResourceIds: []string{"deployment/coredns"},
	}, {
		Code:        ekstypes.AddonIssueCodeAccessDenied,
		Message:     aws.String("denied"),
		ResourceIds: []string{"role/addon"},
	}}}
	return output
}
