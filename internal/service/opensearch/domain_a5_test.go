package opensearch

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainCreateAllowsJWTWhenEngineVersionIsOmitted(t *testing.T) {
	resource := DomainResource{
		DomainName: "example",
		AdvancedSecurityOptions: enabledJWT(
			stringPointer("public-key"),
			nil,
		),
	}

	err := resource.ValidateInputs(context.Background(), nil)

	assert.NoError(t, err)
}

func TestDomainUpdateUsesObservedVersionForJWTValidation(t *testing.T) {
	prior := runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg]{
		Inputs: DomainResource{
			DomainName: "example",
			AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
				Enabled: true,
			},
		},
		Observed: &DomainResourceOutput{
			DomainName: "example", EngineVersion: "OpenSearch_2.11",
		},
		Outputs: &DomainResourceOutput{DomainName: "example"},
	}
	resource := DomainResource{
		DomainName: "example",
		AdvancedSecurityOptions: enabledJWT(
			stringPointer("public-key"),
			nil,
		),
	}
	updates := 0
	client := &fakeDomainClient{
		updateDomainConfig: func(
			context.Context,
			*awssdk.UpdateDomainConfigInput,
		) (*awssdk.UpdateDomainConfigOutput, error) {
			updates++
			return &awssdk.UpdateDomainConfigOutput{}, nil
		},
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			return &awssdk.DescribeDomainOutput{
				DomainStatus: completeDomainStatus("example"),
			}, nil
		},
	}

	output, err := resource.update(
		context.Background(), client, prior,
		shortDomainOptions(newRecordingDomainClock()),
	)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 1, updates)
}

func TestDomainUpdateUsesObservedVersionForConditionalReplacement(t *testing.T) {
	prior := runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg]{
		Inputs: DomainResource{
			DomainName:    "example",
			EncryptAtRest: &DomainEncryptionAtRestOptions{},
		},
		Observed: &DomainResourceOutput{
			DomainName: "example", EngineVersion: "Elasticsearch_6.6",
		},
		Outputs: &DomainResourceOutput{DomainName: "example"},
	}
	resource := DomainResource{
		DomainName:    "example",
		EncryptAtRest: &DomainEncryptionAtRestOptions{Enabled: true},
	}

	output, err := resource.update(
		context.Background(), &fakeDomainClient{}, prior,
		shortDomainOptions(newRecordingDomainClock()),
	)

	assert.Nil(t, output)
	require.ErrorContains(t, err, "requires replacement")
	assert.ErrorContains(t, err, "Elasticsearch_6.6")
}

func TestDomainUpdateUsesPriorStateVersionForColdStorageProjection(t *testing.T) {
	tests := []struct {
		name     string
		observed *DomainResourceOutput
		outputs  *DomainResourceOutput
	}{
		{
			name: "observed",
			observed: &DomainResourceOutput{
				DomainName: "example", EngineVersion: "Elasticsearch_7.8",
			},
			outputs: &DomainResourceOutput{DomainName: "example"},
		},
		{
			name: "outputs fallback",
			outputs: &DomainResourceOutput{
				DomainName: "example", EngineVersion: "Elasticsearch_7.8",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prior := runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg]{
				Inputs: DomainResource{
					DomainName: "example",
					ClusterConfig: &DomainClusterConfig{
						InstanceCount: 1, InstanceType: "t3.small.search",
					},
				},
				Observed: test.observed,
				Outputs:  test.outputs,
			}
			resource := DomainResource{
				DomainName: "example",
				ClusterConfig: &DomainClusterConfig{
					InstanceCount: 2,
					InstanceType:  "t3.small.search",
					ColdStorageOptions: &DomainColdStorageOptions{
						Enabled: aws.Bool(true),
					},
				},
			}
			var updateInput *awssdk.UpdateDomainConfigInput
			client := &fakeDomainClient{
				updateDomainConfig: func(
					_ context.Context,
					input *awssdk.UpdateDomainConfigInput,
				) (*awssdk.UpdateDomainConfigOutput, error) {
					updateInput = input
					return &awssdk.UpdateDomainConfigOutput{}, nil
				},
				describeDomain: func(
					context.Context,
					*awssdk.DescribeDomainInput,
				) (*awssdk.DescribeDomainOutput, error) {
					return &awssdk.DescribeDomainOutput{
						DomainStatus: completeDomainStatus("example"),
					}, nil
				},
			}

			_, err := resource.update(
				context.Background(), client, prior,
				shortDomainOptions(newRecordingDomainClock()),
			)

			require.NoError(t, err)
			require.NotNil(t, updateInput)
			require.NotNil(t, updateInput.ClusterConfig)
			assert.Nil(t, updateInput.ClusterConfig.ColdStorageOptions)
		})
	}
}

func TestEffectiveDomainEngineVersionPrecedence(t *testing.T) {
	observed := &DomainResourceOutput{EngineVersion: "Elasticsearch_7.8"}
	outputs := &DomainResourceOutput{EngineVersion: "Elasticsearch_7.7"}

	assert.Equal(t, "OpenSearch_2.11", aws.ToString(effectiveDomainEngineVersion(
		stringPointer("OpenSearch_2.11"), observed, outputs,
	)))
	assert.Equal(t, "Elasticsearch_7.8", aws.ToString(effectiveDomainEngineVersion(
		nil, observed, outputs,
	)))
	assert.Equal(t, "Elasticsearch_7.7", aws.ToString(effectiveDomainEngineVersion(
		nil, nil, outputs,
	)))
	assert.Nil(t, effectiveDomainEngineVersion(nil, nil, nil))
}
