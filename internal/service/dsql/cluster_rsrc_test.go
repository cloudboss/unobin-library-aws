package dsql

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdksql "github.com/aws/aws-sdk-go-v2/service/dsql"
	dsqltypes "github.com/aws/aws-sdk-go-v2/service/dsql/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testClusterARN        = "arn:aws:dsql:us-east-1:123456789012:cluster/cluster-1"
	testPeerClusterARN    = "arn:aws:dsql:us-west-2:123456789012:cluster/peer-1"
	testClusterIdentifier = "cluster-1"
	testKMSARN            = "arn:aws:kms:us-east-1:123456789012:key/11111111-1111-1111-1111-111111111111"
	testEndpointService   = "com.amazonaws.vpce.us-east-1.vpce-svc-1234567890abcdef0"
)

func TestClusterCreateEmptyInputUsesDefaultsAndReadsComputedOutputs(t *testing.T) {
	client := &fakeClusterClient{
		createOutput: &awssdksql.CreateClusterOutput{
			Arn:        aws.String(testClusterARN),
			Identifier: aws.String(testClusterIdentifier),
			Status:     dsqltypes.ClusterStatusCreating,
		},
		getOutputs: []*awssdksql.GetClusterOutput{
			fakeClusterOutput(dsqltypes.ClusterStatusCreating),
			fakeClusterOutput(dsqltypes.ClusterStatusActive),
			fakeClusterOutput(dsqltypes.ClusterStatusActive),
			fakeClusterOutput(dsqltypes.ClusterStatusActive),
		},
		vpcEndpointOutput: &awssdksql.GetVpcEndpointServiceNameOutput{
			ServiceName: aws.String(testEndpointService),
		},
		listTagsOutput: &awssdksql.ListTagsForResourceOutput{Tags: map[string]string{}},
	}

	out, err := (&ClusterResource{}).createWithClient(
		context.Background(),
		client,
		fastClusterOperationOptions(),
	)

	require.NoError(t, err)
	require.Len(t, client.createInputs, 1)
	assert.Equal(t, aws.Bool(false), client.createInputs[0].DeletionProtectionEnabled)
	assert.Nil(t, client.createInputs[0].KmsEncryptionKey)
	assert.Nil(t, client.createInputs[0].Tags)
	assert.NotEmpty(t, aws.ToString(client.createInputs[0].ClientToken))
	assert.Equal(t, testClusterARN, out.Arn)
	assert.Equal(t, testClusterIdentifier, out.Identifier)
	assert.Equal(t, awsOwnedKMSKey, out.KmsEncryptionKey)
	assert.Equal(t, "ENABLED", out.EncryptionDetails.EncryptionStatus)
	assert.Equal(t, awsOwnedKMSKey, out.EncryptionDetails.EncryptionType)
	assert.Equal(t, testEndpointService, out.VpcEndpointServiceName)
}

func TestClusterValidateInputsAcceptsOnlyKMSARNOrAWSOwnedLiteral(t *testing.T) {
	tests := []struct {
		name    string
		key     *string
		wantErr bool
	}{
		{name: "nil"},
		{name: "AWS owned", key: aws.String(awsOwnedKMSKey)},
		{name: "KMS ARN", key: aws.String(testKMSARN)},
		{
			name: "non-KMS ARN",
			key:  aws.String("arn:aws:s3:::customer-managed-key-reference"),
		},
		{name: "invalid literal", key: aws.String("alias/example"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&ClusterResource{KmsEncryptionKey: tt.key}).ValidateInputs(
				context.Background(), nil)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, "kms-encryption-key")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestClusterUpdateKMSWaitsForEncryptionEnabled(t *testing.T) {
	client := &fakeClusterClient{
		updateOutput: &awssdksql.UpdateClusterOutput{
			Arn:        aws.String(testClusterARN),
			Identifier: aws.String(testClusterIdentifier),
			Status:     dsqltypes.ClusterStatusUpdating,
		},
		getOutputs: []*awssdksql.GetClusterOutput{
			fakeClusterOutput(dsqltypes.ClusterStatusUpdating),
			fakeClusterOutput(dsqltypes.ClusterStatusActive),
			clusterWithEncryption(dsqltypes.EncryptionStatusEnabling, awsOwnedKMSKey),
			clusterWithEncryption(dsqltypes.EncryptionStatusEnabled, awsOwnedKMSKey),
			clusterWithEncryption(dsqltypes.EncryptionStatusEnabled, awsOwnedKMSKey),
		},
		vpcEndpointOutput: &awssdksql.GetVpcEndpointServiceNameOutput{
			ServiceName: aws.String(testEndpointService),
		},
		listTagsOutput: &awssdksql.ListTagsForResourceOutput{Tags: map[string]string{}},
	}

	current := &ClusterResource{KmsEncryptionKey: aws.String(awsOwnedKMSKey)}
	out, err := current.updateWithClient(
		context.Background(),
		client,
		runtime.Prior[ClusterResource, *ClusterResourceOutput, *awsCfg]{
			Inputs:  ClusterResource{KmsEncryptionKey: aws.String(testKMSARN)},
			Outputs: priorOutput(),
		},
		fastClusterOperationOptions(),
	)

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	assert.Equal(t, awsOwnedKMSKey, aws.ToString(client.updateInputs[0].KmsEncryptionKey))
	assert.NotEmpty(t, aws.ToString(client.updateInputs[0].ClientToken))
	assert.Equal(t, awsOwnedKMSKey, out.KmsEncryptionKey)
	assert.GreaterOrEqual(t, len(client.getInputs), 5)
}

func TestClusterUpdateDeletionProtectionWaitsForActive(t *testing.T) {
	client := &fakeClusterClient{
		updateOutput: &awssdksql.UpdateClusterOutput{
			Arn:        aws.String(testClusterARN),
			Identifier: aws.String(testClusterIdentifier),
			Status:     dsqltypes.ClusterStatusUpdating,
		},
		getOutputs: []*awssdksql.GetClusterOutput{
			fakeClusterOutput(dsqltypes.ClusterStatusUpdating),
			fakeClusterOutput(dsqltypes.ClusterStatusActive),
			fakeClusterOutput(dsqltypes.ClusterStatusActive),
		},
		vpcEndpointOutput: &awssdksql.GetVpcEndpointServiceNameOutput{
			ServiceName: aws.String(testEndpointService),
		},
		listTagsOutput: &awssdksql.ListTagsForResourceOutput{Tags: map[string]string{}},
	}

	out, err := (&ClusterResource{DeletionProtectionEnabled: false}).updateWithClient(
		context.Background(),
		client,
		runtime.Prior[ClusterResource, *ClusterResourceOutput, *awsCfg]{
			Inputs:  ClusterResource{DeletionProtectionEnabled: true},
			Outputs: priorOutput(),
		},
		fastClusterOperationOptions(),
	)

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	assert.Equal(t, aws.Bool(false), client.updateInputs[0].DeletionProtectionEnabled)
	assert.Nil(t, client.updateInputs[0].KmsEncryptionKey)
	assert.Equal(t, false, out.DeletionProtectionEnabled)
}

func TestClusterForceDestroyOnlyAffectsDelete(t *testing.T) {
	client := &fakeClusterClient{
		getOutputs: []*awssdksql.GetClusterOutput{
			fakeClusterOutput(dsqltypes.ClusterStatusDeleting),
		},
		deleteErrors: []error{notFoundErr()},
	}

	err := (&ClusterResource{ForceDestroy: true}).deleteWithClient(
		context.Background(),
		client,
		priorOutput(),
		fastClusterOperationOptions(),
	)

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, aws.Bool(false), client.updateInputs[0].DeletionProtectionEnabled)
	assert.Nil(t, client.updateInputs[0].KmsEncryptionKey)
}

func TestClusterDeleteMissingSucceeds(t *testing.T) {
	client := &fakeClusterClient{deleteErrors: []error{notFoundErr()}}

	err := (&ClusterResource{}).deleteWithClient(
		context.Background(),
		client,
		priorOutput(),
		fastClusterOperationOptions(),
	)

	require.NoError(t, err)
	assert.Empty(t, client.getInputs)
}

func TestClusterReadMissingMapsToRuntimeNotFound(t *testing.T) {
	client := &fakeClusterClient{getErrors: []error{notFoundErr()}}

	_, err := (&ClusterResource{}).readWithClient(context.Background(), client, priorOutput())

	require.ErrorIs(t, err, runtime.ErrNotFound)
}

func TestClusterReadWithoutPriorIdentifierMapsToRuntimeNotFound(t *testing.T) {
	_, err := (&ClusterResource{}).readWithClient(
		context.Background(),
		&fakeClusterClient{},
		&ClusterResourceOutput{},
	)

	require.ErrorIs(t, err, runtime.ErrNotFound)
}

func TestClusterWaitStatusRules(t *testing.T) {
	t.Run("create accepts pending setup after two observations", func(t *testing.T) {
		client := &fakeClusterClient{getOutputs: []*awssdksql.GetClusterOutput{
			fakeClusterOutput(dsqltypes.ClusterStatusCreating),
			fakeClusterOutput(dsqltypes.ClusterStatusPendingSetup),
			fakeClusterOutput(dsqltypes.ClusterStatusPendingSetup),
		}}
		require.NoError(t, waitClusterCreated(
			context.Background(), client, testClusterIdentifier, fastClusterOperationOptions()))
		assert.Len(t, client.getInputs, 3)
	})
	t.Run("update rejects pending setup", func(t *testing.T) {
		client := &fakeClusterClient{getOutputs: []*awssdksql.GetClusterOutput{
			fakeClusterOutput(dsqltypes.ClusterStatusPendingSetup),
		}}
		err := waitClusterUpdated(
			context.Background(), client, testClusterIdentifier, fastClusterOperationOptions())
		require.Error(t, err)
		assert.ErrorContains(t, err, "unexpected status")
	})
	t.Run("delete waits until not found", func(t *testing.T) {
		client := &fakeClusterClient{
			getOutputs: []*awssdksql.GetClusterOutput{
				fakeClusterOutput(dsqltypes.ClusterStatusDeleting),
				fakeClusterOutput(dsqltypes.ClusterStatusPendingDelete),
			},
			getErrors: []error{nil, nil, notFoundErr()},
		}
		require.NoError(t, waitClusterDeleted(
			context.Background(), client, testClusterIdentifier, fastClusterOperationOptions()))
		assert.Len(t, client.getInputs, 3)
	})
	t.Run("timeout propagates", func(t *testing.T) {
		client := &fakeClusterClient{getOutputs: []*awssdksql.GetClusterOutput{
			fakeClusterOutput(dsqltypes.ClusterStatusCreating),
		}}
		err := waitClusterCreated(context.Background(), client, testClusterIdentifier,
			clusterOperationOptions{waitInterval: 0, waitTimeout: 0})
		require.Error(t, err)
		assert.ErrorContains(t, err, "timed out")
	})
}

func TestClusterVpcEndpointServiceNameIsRequired(t *testing.T) {
	client := &fakeClusterClient{
		getOutputs:        []*awssdksql.GetClusterOutput{fakeClusterOutput(dsqltypes.ClusterStatusActive)},
		vpcEndpointOutput: &awssdksql.GetVpcEndpointServiceNameOutput{},
	}

	_, err := (&ClusterResource{}).readWithClient(context.Background(), client, priorOutput())

	require.Error(t, err)
	assert.ErrorContains(t, err, "vpc endpoint service name")
}

func TestClusterVpcEndpointNotFoundIsReadError(t *testing.T) {
	client := &fakeClusterClient{
		getOutputs:        []*awssdksql.GetClusterOutput{fakeClusterOutput(dsqltypes.ClusterStatusActive)},
		vpcEndpointErrors: []error{notFoundErr()},
	}

	_, err := (&ClusterResource{}).readWithClient(context.Background(), client, priorOutput())

	require.Error(t, err)
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
	assert.ErrorContains(t, err, "get vpc endpoint service name")
}

func TestClusterMultiRegionNormalizesSelfARNAndComparesClusterOrder(t *testing.T) {
	output := fakeClusterOutput(dsqltypes.ClusterStatusActive)
	output.MultiRegionProperties = &dsqltypes.MultiRegionProperties{
		Clusters:      []string{testPeerClusterARN, "ARN:AWS:DSQL:US-EAST-1:123456789012:CLUSTER/CLUSTER-1"},
		WitnessRegion: aws.String("us-west-1"),
	}
	client := &fakeClusterClient{
		getOutputs: []*awssdksql.GetClusterOutput{output},
		vpcEndpointOutput: &awssdksql.GetVpcEndpointServiceNameOutput{
			ServiceName: aws.String(testEndpointService),
		},
		listTagsOutput: &awssdksql.ListTagsForResourceOutput{Tags: map[string]string{}},
	}

	out, err := (&ClusterResource{}).readWithClient(context.Background(), client, priorOutput())

	require.NoError(t, err)
	require.NotNil(t, out.MultiRegionProperties)
	assert.Equal(t, []string{testPeerClusterARN}, *out.MultiRegionProperties.Clusters)
	assert.Equal(t, "us-west-1", aws.ToString(out.MultiRegionProperties.WitnessRegion))

	prior := ClusterResource{MultiRegionProperties: &ClusterMultiRegionProperties{
		Clusters:      &[]string{"b", "a"},
		WitnessRegion: aws.String("us-west-1"),
	}}
	current := ClusterResource{MultiRegionProperties: &ClusterMultiRegionProperties{
		Clusters:      &[]string{"a", "b"},
		WitnessRegion: aws.String("us-west-1"),
	}}
	assert.True(t, (&ClusterResource{}).EquivalentInput(
		"multi-region-properties.clusters", prior, current))
	registration := runtime.MakeResource[ClusterResource, *ClusterResourceOutput, *awsCfg](
		(&ClusterResource{}).ResourceDefinition(),
	)
	equal, err := registration.InputsEqual(&current, map[string]any{
		"multi-region-properties": map[string]any{
			"clusters":       []any{"b", "a"},
			"witness-region": "us-west-1",
		},
	})
	require.NoError(t, err)
	assert.True(t, equal)
}

func TestClusterWitnessRegionChangeRequiresReplacement(t *testing.T) {
	prior := ClusterResource{MultiRegionProperties: &ClusterMultiRegionProperties{
		WitnessRegion: aws.String("us-west-1"),
	}}
	current := ClusterResource{MultiRegionProperties: &ClusterMultiRegionProperties{
		WitnessRegion: aws.String("us-west-2"),
	}}
	err := conditionalClusterReplacement(prior, current)

	require.Error(t, err)
	assert.ErrorContains(t, err, "multi-region-properties.witness-region")
	assert.ErrorContains(t, err, "requires replacement")

	client := &fakeClusterClient{}
	_, err = (&current).updateWithClient(
		context.Background(),
		client,
		runtime.Prior[ClusterResource, *ClusterResourceOutput, *awsCfg]{
			Inputs:  prior,
			Outputs: priorOutput(),
		},
		fastClusterOperationOptions(),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "requires replacement")
	assert.Empty(t, client.updateInputs)
}

func TestClusterTags(t *testing.T) {
	t.Run("create sends nil for empty or reserved tags", func(t *testing.T) {
		tags := map[string]string{"aws:system": "kept"}
		client := &fakeClusterClient{
			createOutput: &awssdksql.CreateClusterOutput{
				Arn:        aws.String(testClusterARN),
				Identifier: aws.String(testClusterIdentifier),
			},
			getOutputs: []*awssdksql.GetClusterOutput{
				fakeClusterOutput(dsqltypes.ClusterStatusActive),
				fakeClusterOutput(dsqltypes.ClusterStatusActive),
				fakeClusterOutput(dsqltypes.ClusterStatusActive),
			},
			vpcEndpointOutput: &awssdksql.GetVpcEndpointServiceNameOutput{
				ServiceName: aws.String(testEndpointService),
			},
			listTagsOutput: &awssdksql.ListTagsForResourceOutput{Tags: map[string]string{}},
		}

		_, err := (&ClusterResource{Tags: &tags}).createWithClient(
			context.Background(), client, fastClusterOperationOptions())

		require.NoError(t, err)
		require.Len(t, client.createInputs, 1)
		assert.Nil(t, client.createInputs[0].Tags)
	})
	t.Run("update removes before upsert and ignores system tags", func(t *testing.T) {
		tags := map[string]string{"keep": "new", "add": "x", "aws:skip": "ignored"}
		client := &fakeClusterClient{
			getOutputs: []*awssdksql.GetClusterOutput{
				fakeClusterOutput(dsqltypes.ClusterStatusActive),
			},
			vpcEndpointOutput: &awssdksql.GetVpcEndpointServiceNameOutput{
				ServiceName: aws.String(testEndpointService),
			},
			listTagsOutput: &awssdksql.ListTagsForResourceOutput{Tags: map[string]string{
				"keep": "old", "drop": "y", "aws:live": "z",
			}},
		}

		_, err := (&ClusterResource{Tags: &tags}).updateWithClient(
			context.Background(),
			client,
			runtime.Prior[ClusterResource, *ClusterResourceOutput, *awsCfg]{
				Inputs:  ClusterResource{Tags: &map[string]string{"keep": "old", "drop": "y"}},
				Outputs: priorOutput(),
			},
			fastClusterOperationOptions(),
		)

		require.NoError(t, err)
		assert.Equal(t, []string{"drop"}, client.untagInputs[0].TagKeys)
		assert.Equal(t, map[string]string{"add": "x", "keep": "new"}, client.tagInputs[0].Tags)
	})
}

type fakeClusterClient struct {
	createInputs      []*awssdksql.CreateClusterInput
	getInputs         []*awssdksql.GetClusterInput
	updateInputs      []*awssdksql.UpdateClusterInput
	deleteInputs      []*awssdksql.DeleteClusterInput
	vpcEndpointInputs []*awssdksql.GetVpcEndpointServiceNameInput
	listTagsInputs    []*awssdksql.ListTagsForResourceInput
	tagInputs         []*awssdksql.TagResourceInput
	untagInputs       []*awssdksql.UntagResourceInput

	createOutput      *awssdksql.CreateClusterOutput
	getOutputs        []*awssdksql.GetClusterOutput
	updateOutput      *awssdksql.UpdateClusterOutput
	vpcEndpointOutput *awssdksql.GetVpcEndpointServiceNameOutput
	listTagsOutput    *awssdksql.ListTagsForResourceOutput

	createErrors      []error
	getErrors         []error
	updateErrors      []error
	deleteErrors      []error
	vpcEndpointErrors []error
}

func (f *fakeClusterClient) CreateCluster(
	ctx context.Context,
	input *awssdksql.CreateClusterInput,
	optFns ...func(*awssdksql.Options),
) (*awssdksql.CreateClusterOutput, error) {
	f.createInputs = append(f.createInputs, input)
	if err := nextError(&f.createErrors); err != nil {
		return nil, err
	}
	return f.createOutput, nil
}

func (f *fakeClusterClient) GetCluster(
	ctx context.Context,
	input *awssdksql.GetClusterInput,
	optFns ...func(*awssdksql.Options),
) (*awssdksql.GetClusterOutput, error) {
	f.getInputs = append(f.getInputs, input)
	if err := nextError(&f.getErrors); err != nil {
		return nil, err
	}
	if len(f.getOutputs) == 0 {
		return nil, nil
	}
	out := f.getOutputs[0]
	if len(f.getOutputs) > 1 {
		f.getOutputs = f.getOutputs[1:]
	}
	return out, nil
}

func (f *fakeClusterClient) UpdateCluster(
	ctx context.Context,
	input *awssdksql.UpdateClusterInput,
	optFns ...func(*awssdksql.Options),
) (*awssdksql.UpdateClusterOutput, error) {
	f.updateInputs = append(f.updateInputs, input)
	if err := nextError(&f.updateErrors); err != nil {
		return nil, err
	}
	return f.updateOutput, nil
}

func (f *fakeClusterClient) DeleteCluster(
	ctx context.Context,
	input *awssdksql.DeleteClusterInput,
	optFns ...func(*awssdksql.Options),
) (*awssdksql.DeleteClusterOutput, error) {
	f.deleteInputs = append(f.deleteInputs, input)
	if err := nextError(&f.deleteErrors); err != nil {
		return nil, err
	}
	return &awssdksql.DeleteClusterOutput{}, nil
}

func (f *fakeClusterClient) GetVpcEndpointServiceName(
	ctx context.Context,
	input *awssdksql.GetVpcEndpointServiceNameInput,
	optFns ...func(*awssdksql.Options),
) (*awssdksql.GetVpcEndpointServiceNameOutput, error) {
	f.vpcEndpointInputs = append(f.vpcEndpointInputs, input)
	if err := nextError(&f.vpcEndpointErrors); err != nil {
		return nil, err
	}
	return f.vpcEndpointOutput, nil
}

func (f *fakeClusterClient) ListTagsForResource(
	ctx context.Context,
	input *awssdksql.ListTagsForResourceInput,
	optFns ...func(*awssdksql.Options),
) (*awssdksql.ListTagsForResourceOutput, error) {
	f.listTagsInputs = append(f.listTagsInputs, input)
	return f.listTagsOutput, nil
}

func (f *fakeClusterClient) TagResource(
	ctx context.Context,
	input *awssdksql.TagResourceInput,
	optFns ...func(*awssdksql.Options),
) (*awssdksql.TagResourceOutput, error) {
	f.tagInputs = append(f.tagInputs, input)
	return &awssdksql.TagResourceOutput{}, nil
}

func (f *fakeClusterClient) UntagResource(
	ctx context.Context,
	input *awssdksql.UntagResourceInput,
	optFns ...func(*awssdksql.Options),
) (*awssdksql.UntagResourceOutput, error) {
	f.untagInputs = append(f.untagInputs, input)
	return &awssdksql.UntagResourceOutput{}, nil
}

func nextError(errors *[]error) error {
	if len(*errors) == 0 {
		return nil
	}
	err := (*errors)[0]
	*errors = (*errors)[1:]
	return err
}

func fastClusterOperationOptions() clusterOperationOptions {
	return clusterOperationOptions{
		waitInterval:       0,
		waitTimeout:        time.Second,
		deleteInitialDelay: 0,
	}
}

func priorOutput() *ClusterResourceOutput {
	return &ClusterResourceOutput{Arn: testClusterARN, Identifier: testClusterIdentifier}
}

func fakeClusterOutput(status dsqltypes.ClusterStatus) *awssdksql.GetClusterOutput {
	return clusterWithEncryption(dsqltypes.EncryptionStatusEnabled, awsOwnedKMSKey, status)
}

func clusterWithEncryption(
	status dsqltypes.EncryptionStatus,
	key string,
	clusterStatus ...dsqltypes.ClusterStatus,
) *awssdksql.GetClusterOutput {
	cs := dsqltypes.ClusterStatusActive
	if len(clusterStatus) > 0 {
		cs = clusterStatus[0]
	}
	out := &awssdksql.GetClusterOutput{
		Arn:                       aws.String(testClusterARN),
		Identifier:                aws.String(testClusterIdentifier),
		DeletionProtectionEnabled: aws.Bool(false),
		Status:                    cs,
		EncryptionDetails: &dsqltypes.EncryptionDetails{
			EncryptionStatus: status,
			EncryptionType:   dsqltypes.EncryptionTypeAwsOwnedKmsKey,
		},
	}
	if key != awsOwnedKMSKey {
		out.EncryptionDetails.EncryptionType = dsqltypes.EncryptionTypeCustomerManagedKmsKey
		out.EncryptionDetails.KmsKeyArn = aws.String(key)
	}
	return out
}

func notFoundErr() error {
	return &dsqltypes.ResourceNotFoundException{Message: aws.String("missing")}
}
