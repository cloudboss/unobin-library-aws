package dsql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/dsql"
	awstypes "github.com/aws/aws-sdk-go-v2/service/dsql/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPeeringClusterIdentifier = "cluster-a"
	testPeeringClusterARN        = "arn:aws:dsql:us-east-1:123456789012:cluster/cluster-a"
	testPeeringPeerClusterARN    = "arn:aws:dsql:us-west-2:123456789012:cluster/cluster-b"
	testPeeringOtherPeerARN      = "arn:aws:dsql:eu-west-1:123456789012:cluster/cluster-c"
)

func TestClusterPeeringValidateInputs(t *testing.T) {
	tests := map[string]struct {
		resource *ClusterPeeringResource
		wantErr  string
	}{
		"valid": {
			resource: validClusterPeeringResource(),
		},
		"missing identifier": {
			resource: &ClusterPeeringResource{
				Clusters:      []string{testPeeringPeerClusterARN},
				WitnessRegion: "us-west-2",
			},
			wantErr: "identifier must not be empty",
		},
		"empty clusters": {
			resource: &ClusterPeeringResource{
				Identifier:    testPeeringClusterIdentifier,
				WitnessRegion: "us-west-2",
			},
			wantErr: "clusters must contain at least one ARN",
		},
		"invalid cluster ARN": {
			resource: &ClusterPeeringResource{
				Identifier:    testPeeringClusterIdentifier,
				Clusters:      []string{"not an arn"},
				WitnessRegion: "us-west-2",
			},
			wantErr: "clusters[0] must be a valid ARN",
		},
		"invalid witness region": {
			resource: &ClusterPeeringResource{
				Identifier:    testPeeringClusterIdentifier,
				Clusters:      []string{testPeeringPeerClusterARN},
				WitnessRegion: "US-EAST-1",
			},
			wantErr: "witness-region must be a valid AWS region",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.resource.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestClusterPeeringCreateRejectsActiveClusterWithoutUpdate(t *testing.T) {
	resource := validClusterPeeringResource()
	client := &fakeClusterPeeringClient{
		getResults: []clusterPeeringGetResult{{
			output: clusterPeeringOutput(testPeeringClusterIdentifier, testPeeringClusterARN, awstypes.ClusterStatusActive),
		}},
	}

	_, err := resource.create(context.Background(), client, newFakeClusterPeeringClock())

	require.ErrorContains(t, err, "PENDING_SETUP")
	assert.Empty(t, client.updateInputs)
	assert.Equal(t, []string{"get"}, client.calls)
}

func TestClusterPeeringCreateUpdatesAndWaitsUntilActiveTwice(t *testing.T) {
	resource := validClusterPeeringResource()
	client := &fakeClusterPeeringClient{
		getResults: []clusterPeeringGetResult{
			{
				output: clusterPeeringOutput(
					testPeeringClusterIdentifier,
					testPeeringClusterARN,
					awstypes.ClusterStatusPendingSetup,
				),
			},
			{
				output: clusterPeeringOutput(
					testPeeringClusterIdentifier,
					testPeeringClusterARN,
					awstypes.ClusterStatusUpdating,
				),
			},
			{
				output: clusterPeeringOutputWithMultiRegion(
					testPeeringClusterIdentifier,
					testPeeringClusterARN,
					awstypes.ClusterStatusActive,
					[]string{testPeeringPeerClusterARN},
					"us-west-2",
				),
			},
			{
				output: clusterPeeringOutputWithMultiRegion(
					testPeeringClusterIdentifier,
					testPeeringClusterARN,
					awstypes.ClusterStatusActive,
					[]string{testPeeringPeerClusterARN},
					"us-west-2",
				),
			},
		},
	}

	out, err := resource.create(context.Background(), client, newFakeClusterPeeringClock())

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	update := client.updateInputs[0]
	assert.Equal(t, testPeeringClusterIdentifier, aws.ToString(update.Identifier))
	assert.NotEmpty(t, aws.ToString(update.ClientToken))
	require.NotNil(t, update.MultiRegionProperties)
	assert.Equal(t, []string{testPeeringPeerClusterARN}, update.MultiRegionProperties.Clusters)
	assert.Equal(t, "us-west-2", aws.ToString(update.MultiRegionProperties.WitnessRegion))
	assert.Equal(t, &ClusterPeeringResourceOutput{
		Identifier:    testPeeringClusterIdentifier,
		Clusters:      []string{testPeeringPeerClusterARN},
		WitnessRegion: "us-west-2",
	}, out)
	assert.Equal(t, []string{"get", "update", "get", "get", "get"}, client.calls)
}

func TestClusterPeeringCreateFailsWhenWaitedClusterHasNoMultiRegionProperties(t *testing.T) {
	resource := validClusterPeeringResource()
	client := &fakeClusterPeeringClient{
		getResults: []clusterPeeringGetResult{
			{
				output: clusterPeeringOutput(
					testPeeringClusterIdentifier,
					testPeeringClusterARN,
					awstypes.ClusterStatusPendingSetup,
				),
			},
			{
				output: clusterPeeringOutput(
					testPeeringClusterIdentifier,
					testPeeringClusterARN,
					awstypes.ClusterStatusActive,
				),
			},
			{
				output: clusterPeeringOutput(
					testPeeringClusterIdentifier,
					testPeeringClusterARN,
					awstypes.ClusterStatusActive,
				),
			},
		},
	}

	out, err := resource.create(context.Background(), client, newFakeClusterPeeringClock())

	require.ErrorContains(t, err, "multi-region properties")
	assert.Nil(t, out)
	assert.Len(t, client.updateInputs, 1)
	assert.Equal(t, []string{"get", "update", "get", "get"}, client.calls)
}

func TestWaitClusterPeeringActive(t *testing.T) {
	t.Run("pending then two active observations", func(t *testing.T) {
		client := &fakeClusterPeeringClient{
			getResults: []clusterPeeringGetResult{
				{output: clusterPeeringOutput(testPeeringClusterIdentifier, testPeeringClusterARN,
					awstypes.ClusterStatusCreating)},
				{output: clusterPeeringOutput(testPeeringClusterIdentifier, testPeeringClusterARN,
					awstypes.ClusterStatusPendingSetup)},
				{output: clusterPeeringOutput(testPeeringClusterIdentifier, testPeeringClusterARN,
					awstypes.ClusterStatusUpdating)},
				{output: clusterPeeringOutput(testPeeringClusterIdentifier, testPeeringClusterARN,
					awstypes.ClusterStatusActive)},
				{output: clusterPeeringOutput(testPeeringClusterIdentifier, testPeeringClusterARN,
					awstypes.ClusterStatusActive)},
			},
		}

		out, err := waitClusterPeeringActive(
			context.Background(),
			client,
			testPeeringClusterIdentifier,
			newFakeClusterPeeringClock(),
			clusterPeeringCreateTimeout,
		)

		require.NoError(t, err)
		assert.Equal(t, awstypes.ClusterStatusActive, out.Status)
		assert.Equal(t, []string{"get", "get", "get", "get", "get"}, client.calls)
	})

	t.Run("error stops wait", func(t *testing.T) {
		sentinel := errors.New("read failed")
		client := &fakeClusterPeeringClient{
			getResults: []clusterPeeringGetResult{{err: sentinel}},
		}

		_, err := waitClusterPeeringActive(
			context.Background(),
			client,
			testPeeringClusterIdentifier,
			newFakeClusterPeeringClock(),
			clusterPeeringCreateTimeout,
		)

		require.ErrorIs(t, err, sentinel)
	})

	t.Run("typed not found retries through bounded window", func(t *testing.T) {
		client := &fakeClusterPeeringClient{
			getResults: append(
				clusterPeeringNotFoundResults(20),
				clusterPeeringActiveResult(),
				clusterPeeringActiveResult(),
			),
		}

		out, err := waitClusterPeeringActive(
			context.Background(),
			client,
			testPeeringClusterIdentifier,
			newFakeClusterPeeringClock(),
			clusterPeeringCreateTimeout,
		)

		require.NoError(t, err)
		assert.Equal(t, awstypes.ClusterStatusActive, out.Status)
		assert.Len(t, client.calls, 22)
	})

	t.Run("nil and empty cluster retries through bounded window", func(t *testing.T) {
		tests := map[string]clusterPeeringGetResult{
			"nil":   {},
			"empty": {output: &awssvc.GetClusterOutput{}},
		}
		for name, result := range tests {
			t.Run(name, func(t *testing.T) {
				client := &fakeClusterPeeringClient{
					getResults: append(
						clusterPeeringRepeatGetResult(result, 20),
						clusterPeeringActiveResult(),
						clusterPeeringActiveResult(),
					),
				}

				out, err := waitClusterPeeringActive(
					context.Background(),
					client,
					testPeeringClusterIdentifier,
					newFakeClusterPeeringClock(),
					clusterPeeringCreateTimeout,
				)

				require.NoError(t, err)
				assert.Equal(t, awstypes.ClusterStatusActive, out.Status)
				assert.Len(t, client.calls, 22)
			})
		}
	})

	t.Run("twenty first consecutive not found fails", func(t *testing.T) {
		client := &fakeClusterPeeringClient{
			getResults: clusterPeeringNotFoundResults(21),
		}

		_, err := waitClusterPeeringActive(
			context.Background(),
			client,
			testPeeringClusterIdentifier,
			newFakeClusterPeeringClock(),
			clusterPeeringCreateTimeout,
		)

		require.ErrorIs(t, err, runtime.ErrNotFound)
		assert.Len(t, client.calls, 21)
	})

	t.Run("nonzero cluster refresh resets not found counter", func(t *testing.T) {
		client := &fakeClusterPeeringClient{
			getResults: append(
				append(
					clusterPeeringNotFoundResults(20),
					clusterPeeringGetResult{output: clusterPeeringOutput(
						testPeeringClusterIdentifier,
						testPeeringClusterARN,
						awstypes.ClusterStatusUpdating,
					)},
				),
				append(
					clusterPeeringNotFoundResults(20),
					clusterPeeringActiveResult(),
					clusterPeeringActiveResult(),
				)...,
			),
		}

		out, err := waitClusterPeeringActive(
			context.Background(),
			client,
			testPeeringClusterIdentifier,
			newFakeClusterPeeringClock(),
			clusterPeeringCreateTimeout,
		)

		require.NoError(t, err)
		assert.Equal(t, awstypes.ClusterStatusActive, out.Status)
		assert.Len(t, client.calls, 43)
	})

	t.Run("ordinary error after not found stops wait", func(t *testing.T) {
		sentinel := errors.New("read failed")
		client := &fakeClusterPeeringClient{
			getResults: []clusterPeeringGetResult{
				{err: &awstypes.ResourceNotFoundException{}},
				{err: sentinel},
			},
		}

		_, err := waitClusterPeeringActive(
			context.Background(),
			client,
			testPeeringClusterIdentifier,
			newFakeClusterPeeringClock(),
			clusterPeeringCreateTimeout,
		)

		require.ErrorIs(t, err, sentinel)
		assert.Len(t, client.calls, 2)
	})

	t.Run("timeout returns error", func(t *testing.T) {
		client := &fakeClusterPeeringClient{
			getFallback: clusterPeeringGetResult{
				output: clusterPeeringOutput(
					testPeeringClusterIdentifier,
					testPeeringClusterARN,
					awstypes.ClusterStatusUpdating,
				),
			},
		}
		clock := newFakeClusterPeeringClock()
		clock.sleepStep = time.Second

		_, err := waitClusterPeeringActive(
			context.Background(),
			client,
			testPeeringClusterIdentifier,
			clock,
			time.Second,
		)

		require.ErrorContains(t, err, "timed out")
	})
}

func TestClusterPeeringReadMapsAbsenceToNotFound(t *testing.T) {
	tests := map[string]clusterPeeringGetResult{
		"typed not found": {
			err: &awstypes.ResourceNotFoundException{},
		},
		"nil response": {},
		"empty identifier": {
			output: &awssvc.GetClusterOutput{},
		},
		"nil multi region properties": {
			output: clusterPeeringOutput(
				testPeeringClusterIdentifier,
				testPeeringClusterARN,
				awstypes.ClusterStatusActive,
			),
		},
	}
	for name, result := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeClusterPeeringClient{getResults: []clusterPeeringGetResult{result}}

			_, err := validClusterPeeringResource().read(context.Background(), client, nil)

			require.ErrorIs(t, err, runtime.ErrNotFound)
			assert.Equal(t, []string{"get"}, client.calls)
		})
	}
}

func TestClusterPeeringReadNormalizesRemoteClusters(t *testing.T) {
	client := &fakeClusterPeeringClient{
		getResults: []clusterPeeringGetResult{{
			output: clusterPeeringOutputWithMultiRegion(
				testPeeringClusterIdentifier,
				testPeeringClusterARN,
				awstypes.ClusterStatusActive,
				[]string{testPeeringOtherPeerARN, testPeeringClusterARN, testPeeringPeerClusterARN},
				"us-west-2",
			),
		}},
	}

	out, err := validClusterPeeringResource().read(context.Background(), client, nil)

	require.NoError(t, err)
	assert.Equal(t, &ClusterPeeringResourceOutput{
		Identifier:    testPeeringClusterIdentifier,
		Clusters:      []string{testPeeringOtherPeerARN, testPeeringPeerClusterARN},
		WitnessRegion: "us-west-2",
	}, out)
}

func TestClusterPeeringReadUsesPriorIdentifier(t *testing.T) {
	prior := &ClusterPeeringResourceOutput{Identifier: "cluster-old"}
	client := &fakeClusterPeeringClient{
		getResults: []clusterPeeringGetResult{{
			output: clusterPeeringOutputWithMultiRegion(
				"cluster-old",
				testPeeringClusterARN,
				awstypes.ClusterStatusActive,
				[]string{testPeeringPeerClusterARN},
				"us-west-2",
			),
		}},
	}

	out, err := validClusterPeeringResource().read(context.Background(), client, prior)

	require.NoError(t, err)
	assert.Equal(t, "cluster-old", client.getIdentifiers[0])
	assert.Equal(t, "cluster-old", out.Identifier)
}

func TestClusterPeeringEquivalentInputUsesUnorderedClusters(t *testing.T) {
	resource := &ClusterPeeringResource{}
	prior := validClusterPeeringResource()
	current := validClusterPeeringResource()
	prior.Clusters = []string{testPeeringPeerClusterARN, testPeeringOtherPeerARN}
	current.Clusters = []string{testPeeringOtherPeerARN, testPeeringPeerClusterARN}

	assert.True(t, resource.EquivalentInput("clusters", *prior, *current))
	current.Clusters = []string{testPeeringPeerClusterARN}
	assert.False(t, resource.EquivalentInput("clusters", *prior, *current))
	assert.False(t, resource.EquivalentInput("identifier", *prior, *current))
}

func TestClusterPeeringUpdateBehavior(t *testing.T) {
	t.Run("changed inputs are unsupported", func(t *testing.T) {
		resource := validClusterPeeringResource()
		resource.WitnessRegion = "eu-west-1"
		_, err := resource.Update(
			context.Background(),
			nil,
			runtime.Prior[ClusterPeeringResource, *ClusterPeeringResourceOutput]{
				Inputs:  *validClusterPeeringResource(),
				Outputs: validClusterPeeringOutput(),
			},
		)

		require.ErrorContains(t, err, "not supported")
		assert.Empty(t, resource.ReplaceFields())
	})

	t.Run("drift only returns observed output", func(t *testing.T) {
		observed := &ClusterPeeringResourceOutput{
			Identifier:    testPeeringClusterIdentifier,
			Clusters:      []string{testPeeringPeerClusterARN, testPeeringOtherPeerARN},
			WitnessRegion: "us-west-2",
		}

		out, err := validClusterPeeringResource().Update(
			context.Background(),
			nil,
			runtime.Prior[ClusterPeeringResource, *ClusterPeeringResourceOutput]{
				Inputs:   *validClusterPeeringResource(),
				Outputs:  validClusterPeeringOutput(),
				Observed: observed,
			},
		)

		require.NoError(t, err)
		assert.Same(t, observed, out)
	})
}

func TestClusterPeeringDeleteIsNoOp(t *testing.T) {
	err := validClusterPeeringResource().Delete(
		context.Background(),
		nil,
		validClusterPeeringOutput(),
	)

	require.NoError(t, err)
}

func TestClusterPeeringSeededIdentifierReadsRemotePeering(t *testing.T) {
	client := &fakeClusterPeeringClient{
		getResults: []clusterPeeringGetResult{{
			output: clusterPeeringOutputWithMultiRegion(
				testPeeringClusterIdentifier,
				testPeeringClusterARN,
				awstypes.ClusterStatusActive,
				[]string{testPeeringClusterARN, testPeeringPeerClusterARN},
				"us-west-2",
			),
		}},
	}

	out, err := (&ClusterPeeringResource{}).read(
		context.Background(),
		client,
		&ClusterPeeringResourceOutput{Identifier: testPeeringClusterIdentifier},
	)

	require.NoError(t, err)
	assert.Equal(t, testPeeringClusterIdentifier, client.getIdentifiers[0])
	assert.Equal(t, &ClusterPeeringResourceOutput{
		Identifier:    testPeeringClusterIdentifier,
		Clusters:      []string{testPeeringPeerClusterARN},
		WitnessRegion: "us-west-2",
	}, out)
}

func validClusterPeeringResource() *ClusterPeeringResource {
	return &ClusterPeeringResource{
		Identifier:    testPeeringClusterIdentifier,
		Clusters:      []string{testPeeringPeerClusterARN},
		WitnessRegion: "us-west-2",
	}
}

func validClusterPeeringOutput() *ClusterPeeringResourceOutput {
	return &ClusterPeeringResourceOutput{
		Identifier:    testPeeringClusterIdentifier,
		Clusters:      []string{testPeeringPeerClusterARN},
		WitnessRegion: "us-west-2",
	}
}

type clusterPeeringGetResult struct {
	output *awssvc.GetClusterOutput
	err    error
}

type fakeClusterPeeringClient struct {
	calls          []string
	getIdentifiers []string
	getResults     []clusterPeeringGetResult
	getFallback    clusterPeeringGetResult
	updateInputs   []*awssvc.UpdateClusterInput
	updateErrs     []error
}

func (c *fakeClusterPeeringClient) GetCluster(
	_ context.Context,
	input *awssvc.GetClusterInput,
	_ ...func(*awssvc.Options),
) (*awssvc.GetClusterOutput, error) {
	c.calls = append(c.calls, "get")
	c.getIdentifiers = append(c.getIdentifiers, aws.ToString(input.Identifier))
	if len(c.getResults) == 0 {
		return c.getFallback.output, c.getFallback.err
	}
	result := c.getResults[0]
	c.getResults = c.getResults[1:]
	return result.output, result.err
}

func (c *fakeClusterPeeringClient) UpdateCluster(
	_ context.Context,
	input *awssvc.UpdateClusterInput,
	_ ...func(*awssvc.Options),
) (*awssvc.UpdateClusterOutput, error) {
	c.calls = append(c.calls, "update")
	c.updateInputs = append(c.updateInputs, input)
	if len(c.updateErrs) == 0 {
		return &awssvc.UpdateClusterOutput{}, nil
	}
	err := c.updateErrs[0]
	c.updateErrs = c.updateErrs[1:]
	return &awssvc.UpdateClusterOutput{}, err
}

type fakeClusterPeeringClock struct {
	now       time.Time
	sleepStep time.Duration
}

func newFakeClusterPeeringClock() *fakeClusterPeeringClock {
	return &fakeClusterPeeringClock{
		now:       time.Unix(1700000000, 0),
		sleepStep: time.Millisecond,
	}
}

func (c *fakeClusterPeeringClock) Now() time.Time { return c.now }

func (c *fakeClusterPeeringClock) Sleep(ctx context.Context, _ time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		c.now = c.now.Add(c.sleepStep)
		return nil
	}
}

func clusterPeeringOutput(
	identifier string,
	arn string,
	status awstypes.ClusterStatus,
) *awssvc.GetClusterOutput {
	return &awssvc.GetClusterOutput{
		Arn:        aws.String(arn),
		Identifier: aws.String(identifier),
		Status:     status,
	}
}

func clusterPeeringOutputWithMultiRegion(
	identifier string,
	arn string,
	status awstypes.ClusterStatus,
	clusters []string,
	witnessRegion string,
) *awssvc.GetClusterOutput {
	out := clusterPeeringOutput(identifier, arn, status)
	out.MultiRegionProperties = &awstypes.MultiRegionProperties{
		Clusters:      clusters,
		WitnessRegion: aws.String(witnessRegion),
	}
	return out
}

func clusterPeeringActiveResult() clusterPeeringGetResult {
	return clusterPeeringGetResult{
		output: clusterPeeringOutputWithMultiRegion(
			testPeeringClusterIdentifier,
			testPeeringClusterARN,
			awstypes.ClusterStatusActive,
			[]string{testPeeringPeerClusterARN},
			"us-west-2",
		),
	}
}

func clusterPeeringNotFoundResults(count int) []clusterPeeringGetResult {
	return clusterPeeringRepeatGetResult(
		clusterPeeringGetResult{err: &awstypes.ResourceNotFoundException{}},
		count,
	)
}

func clusterPeeringRepeatGetResult(
	result clusterPeeringGetResult,
	count int,
) []clusterPeeringGetResult {
	results := make([]clusterPeeringGetResult, count)
	for index := range results {
		results[index] = result
	}
	return results
}
