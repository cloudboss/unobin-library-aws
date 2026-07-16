package elasticache

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

func TestReplicationGroupDeleteUsesPriorHandlesAndCleansGlobalMember(t *testing.T) {
	r := testReplicationGroupResource()
	r.ReplicationGroupID = "new-id"
	r.FinalSnapshotIdentifier = new("final-snapshot")
	prior := &ReplicationGroupResourceOutput{
		ReplicationGroupID:       "prior-id",
		GlobalReplicationGroupID: "global-test",
		ParameterGroupName:       "global-datastore-prior-id",
	}
	client := newFakeReplicationGroupClient()

	err := r.delete(context.Background(), client, "us-east-1", prior,
		fastReplicationGroupOptions())

	require.NoError(t, err)
	require.Len(t, client.disassociateInputs, 1)
	assert.Equal(t, "prior-id",
		aws.ToString(client.disassociateInputs[0].ReplicationGroupId))
	assert.Equal(t, "global-test",
		aws.ToString(client.disassociateInputs[0].GlobalReplicationGroupId))
	assert.Equal(t, "us-east-1",
		aws.ToString(client.disassociateInputs[0].ReplicationGroupRegion))
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, "prior-id", aws.ToString(client.deleteInputs[0].ReplicationGroupId))
	assert.Equal(t, "final-snapshot",
		aws.ToString(client.deleteInputs[0].FinalSnapshotIdentifier))
	require.Len(t, client.deleteParameterGroupInputs, 1)
	assert.Equal(t, "global-datastore-prior-id",
		aws.ToString(client.deleteParameterGroupInputs[0].CacheParameterGroupName))
	assert.Equal(t, []string{"disassociate", "delete", "delete-parameter-group"},
		mutationCalls(client.calls))
}

func TestReplicationGroupDeleteRetriesInvalidState(t *testing.T) {
	r := testReplicationGroupResource()
	client := newFakeReplicationGroupClient()
	client.deleteErrors = []error{
		&elasticachetypes.InvalidReplicationGroupStateFault{},
		nil,
	}

	err := r.delete(context.Background(), client, "us-east-1",
		&ReplicationGroupResourceOutput{ReplicationGroupID: testReplicationGroupID},
		fastReplicationGroupOptions())

	require.NoError(t, err)
	assert.Len(t, client.deleteInputs, 2)
}

func TestReplicationGroupDeleteAlreadyGoneStillCleansGlobalMember(t *testing.T) {
	r := testReplicationGroupResource()
	client := newFakeReplicationGroupClient()
	client.disassociateErrors = []error{
		&elasticachetypes.GlobalReplicationGroupNotFoundFault{},
	}
	client.deleteErrors = []error{
		&elasticachetypes.ReplicationGroupNotFoundFault{},
	}
	client.deleteParameterGroupErrors = []error{
		&elasticachetypes.InvalidCacheParameterGroupStateFault{},
		&elasticachetypes.CacheParameterGroupNotFoundFault{},
	}
	prior := &ReplicationGroupResourceOutput{
		ReplicationGroupID:       "prior-id",
		GlobalReplicationGroupID: "missing-global",
		ParameterGroupName:       "generated-parameter-group",
	}

	err := r.delete(context.Background(), client, "us-east-1", prior,
		fastReplicationGroupOptions())

	require.NoError(t, err)
	assert.Len(t, client.deleteInputs, 1)
	assert.Len(t, client.deleteParameterGroupInputs, 2)
}

func TestReplicationGroupDeleteRetriesWrappedGlobalStateFault(t *testing.T) {
	r := testReplicationGroupResource()
	client := newFakeReplicationGroupClient()
	stateFault := &elasticachetypes.InvalidGlobalReplicationGroupStateFault{}
	client.disassociateErrors = []error{fmt.Errorf("wrapped: %w", stateFault), nil}
	prior := &ReplicationGroupResourceOutput{
		ReplicationGroupID:       testReplicationGroupID,
		GlobalReplicationGroupID: "global-test",
	}

	err := r.delete(context.Background(), client, "us-east-1", prior,
		fastReplicationGroupOptions())

	require.NoError(t, err)
	assert.Len(t, client.disassociateInputs, 2)
}

func TestReplicationGroupDeleteRetriesGlobalStateFaultWithArbitraryMessage(t *testing.T) {
	r := testReplicationGroupResource()
	client := newFakeReplicationGroupClient()
	client.disassociateErrors = []error{
		&elasticachetypes.InvalidGlobalReplicationGroupStateFault{
			Message: aws.String("anything at all"),
		},
		nil,
	}
	prior := &ReplicationGroupResourceOutput{
		ReplicationGroupID:       testReplicationGroupID,
		GlobalReplicationGroupID: "global-test",
	}

	err := r.delete(context.Background(), client, "us-east-1", prior,
		fastReplicationGroupOptions())

	require.NoError(t, err)
	assert.Len(t, client.disassociateInputs, 2)
}

func TestReplicationGroupDeleteStopsAfterUnrelatedDisassociateError(t *testing.T) {
	r := testReplicationGroupResource()
	client := newFakeReplicationGroupClient()
	sentinel := errors.New("disassociate failed")
	client.disassociateErrors = []error{sentinel}
	prior := &ReplicationGroupResourceOutput{
		ReplicationGroupID:       testReplicationGroupID,
		GlobalReplicationGroupID: "global-test",
		ParameterGroupName:       "generated-parameter-group",
	}

	err := r.delete(context.Background(), client, "us-east-1", prior,
		fastReplicationGroupOptions())

	assert.ErrorIs(t, err, sentinel)
	assert.Len(t, client.disassociateInputs, 1)
	assert.Empty(t, client.describeGlobalInputs)
	assert.Empty(t, client.deleteInputs)
	assert.Empty(t, client.deleteParameterGroupInputs)
}

func TestReplicationGroupDeleteReturnsLastGlobalStateFaultAtTimeout(t *testing.T) {
	r := testReplicationGroupResource()
	client := newFakeReplicationGroupClient()
	lastFault := &elasticachetypes.InvalidGlobalReplicationGroupStateFault{
		Message: aws.String("still unavailable"),
	}
	client.disassociateErrors = []error{lastFault}
	prior := &ReplicationGroupResourceOutput{
		ReplicationGroupID:       testReplicationGroupID,
		GlobalReplicationGroupID: "global-test",
		ParameterGroupName:       "generated-parameter-group",
	}
	options := fastReplicationGroupOptions()
	options.retryOptions = []retry.Option{
		retry.WithTimeout(0),
		retry.WithInterval(0),
	}

	err := r.delete(context.Background(), client, "us-east-1", prior, options)

	assert.ErrorIs(t, err, lastFault)
	assert.Len(t, client.disassociateInputs, 1)
	assert.Empty(t, client.describeGlobalInputs)
	assert.Empty(t, client.deleteInputs)
	assert.Empty(t, client.deleteParameterGroupInputs)
}

func TestReplicationGroupGlobalDetachRetryBounds(t *testing.T) {
	assert.Equal(t, 45*time.Minute, globalReplicationGroupDetachTimeout)
	assert.Equal(t, 5*time.Second, globalReplicationGroupDetachInterval)
}

func TestReplicationGroupGlobalMemberAbsentWaitBound(t *testing.T) {
	assert.Equal(t, 45*time.Minute, globalMemberAbsentWaitTimeout)
}

func TestReplicationGroupDeleteWaitRejectsUnexpectedState(t *testing.T) {
	group := testReplicationGroup()
	group.Status = aws.String("create-failed")
	client := newFakeReplicationGroupClient()
	client.describeOutputs = []*elasticache.DescribeReplicationGroupsOutput{
		{ReplicationGroups: []elasticachetypes.ReplicationGroup{*group}},
	}

	err := waitReplicationGroupDeleted(context.Background(), client,
		testReplicationGroupID, fastReplicationGroupOptions().waitOptions...)

	assert.EqualError(t, err,
		`replication group unobin-test-rg entered unexpected status "create-failed"`)
}
