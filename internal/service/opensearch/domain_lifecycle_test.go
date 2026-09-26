package opensearch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateDomainRejectsExistingDomainBeforeMutation(t *testing.T) {
	created := false
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			return &awssdk.DescribeDomainOutput{DomainStatus: completeDomainStatus("example")}, nil
		},
		createDomain: func(
			context.Context,
			*awssdk.CreateDomainInput,
		) (*awssdk.CreateDomainOutput, error) {
			created = true
			return &awssdk.CreateDomainOutput{}, nil
		},
	}
	resource := DomainResource{DomainName: "example"}
	_, err := resource.create(context.Background(), client, domainOperationOptions{
		clock: newRecordingDomainClock(),
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "already exists")
	assert.False(t, created)
}

func TestCreateDomainPreflightErrorsProceed(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "not found",
			err:  &awstypes.ResourceNotFoundException{Message: aws.String("missing")},
		},
		{name: "generic", err: errors.New("temporary describe failure")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			describes := 0
			created := 0
			client := &fakeDomainClient{
				describeDomain: func(
					context.Context,
					*awssdk.DescribeDomainInput,
				) (*awssdk.DescribeDomainOutput, error) {
					describes++
					if describes == 1 {
						return nil, test.err
					}
					return &awssdk.DescribeDomainOutput{
						DomainStatus: completeDomainStatus("example"),
					}, nil
				},
				createDomain: func(
					context.Context,
					*awssdk.CreateDomainInput,
				) (*awssdk.CreateDomainOutput, error) {
					created++
					return validCreateDomainResponse("example"), nil
				},
			}
			resource := DomainResource{DomainName: "example"}
			output, err := resource.create(
				context.Background(), client,
				domainOperationOptions{clock: newRecordingDomainClock()},
			)
			require.NoError(t, err)
			assert.Equal(t, "example", output.DomainName)
			assert.Equal(t, 1, created)
		})
	}
}

func TestCreateDomainAppliesFollowUpOptionsInOrder(t *testing.T) {
	operations := []string{}
	describes := 0
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			describes++
			operations = append(operations, "describe")
			if describes == 1 {
				return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
			}
			return &awssdk.DescribeDomainOutput{
				DomainStatus: completeDomainStatus("example"),
			}, nil
		},
		createDomain: func(
			_ context.Context,
			input *awssdk.CreateDomainInput,
		) (*awssdk.CreateDomainOutput, error) {
			operations = append(operations, "create")
			require.NotNil(t, input.AutoTuneOptions)
			assert.Equal(t, awstypes.AutoTuneDesiredState("DISABLED"),
				input.AutoTuneOptions.DesiredState)
			require.Len(t, input.AutoTuneOptions.MaintenanceSchedules, 1)
			return validCreateDomainResponse("example"), nil
		},
		updateDomainConfig: func(
			_ context.Context,
			input *awssdk.UpdateDomainConfigInput,
		) (*awssdk.UpdateDomainConfigOutput, error) {
			switch {
			case input.AutoTuneOptions != nil:
				operations = append(operations, "auto-tune")
				assert.Equal(t, awstypes.RollbackOnDisable("DEFAULT_ROLLBACK"),
					input.AutoTuneOptions.RollbackOnDisable)
				require.Len(t, input.AutoTuneOptions.MaintenanceSchedules, 1)
			case input.IdentityCenterOptions != nil:
				operations = append(operations, "identity-center")
			default:
				t.Fatalf("unexpected empty follow-up request")
			}
			return &awssdk.UpdateDomainConfigOutput{}, nil
		},
	}
	resource := DomainResource{
		DomainName: "example",
		AutoTuneOptions: &DomainAutoTuneOptions{
			DesiredState: "DISABLED",
			MaintenanceSchedule: &[]DomainAutoTuneMaintenanceSchedule{
				domainMaintenanceSchedule("2026-07-18T12:00:00Z"),
			},
			RollbackOnDisable: stringPointer("DEFAULT_ROLLBACK"),
		},
		IdentityCenterOptions: &DomainIdentityCenterOptions{
			EnabledAPIAccess: aws.Bool(true),
		},
	}
	output, err := resource.create(context.Background(), client, domainOperationOptions{
		clock:         newRecordingDomainClock(),
		createTimeout: domainCreateInitialDelay,
	})
	require.NoError(t, err)
	assert.Equal(t, "example", output.DomainName)
	assert.Equal(t, []string{
		"describe", "create", "describe", "auto-tune", "describe",
		"identity-center", "describe", "describe",
	}, operations)
}

func TestCreateDomainRejectsInvalidAutoTuneBeforeMutation(t *testing.T) {
	mutations := 0
	describes := 0
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			describes++
			if describes == 1 {
				return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
			}
			return &awssdk.DescribeDomainOutput{
				DomainStatus: completeDomainStatus("example"),
			}, nil
		},
		createDomain: func(
			context.Context,
			*awssdk.CreateDomainInput,
		) (*awssdk.CreateDomainOutput, error) {
			mutations++
			return &awssdk.CreateDomainOutput{}, nil
		},
		updateDomainConfig: func(
			context.Context,
			*awssdk.UpdateDomainConfigInput,
		) (*awssdk.UpdateDomainConfigOutput, error) {
			mutations++
			return &awssdk.UpdateDomainConfigOutput{}, nil
		},
	}
	resource := DomainResource{
		DomainName:      "example",
		AutoTuneOptions: autoTuneWithSchedule("invalid", "HOURS", 2),
	}
	_, err := resource.create(
		context.Background(), client, shortDomainOptions(newRecordingDomainClock()),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "start-at")
	assert.Zero(t, mutations)
}

func TestReadDomainPrefersPriorIdentity(t *testing.T) {
	var requested string
	client := &fakeDomainClient{
		describeDomain: func(
			_ context.Context,
			input *awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			requested = aws.ToString(input.DomainName)
			return &awssdk.DescribeDomainOutput{
				DomainStatus: completeDomainStatus(requested),
			}, nil
		},
	}
	resource := DomainResource{DomainName: "new-name"}
	output, err := resource.read(context.Background(), client, &DomainResourceOutput{
		DomainName: "old-name",
	})
	require.NoError(t, err)
	assert.Equal(t, "old-name", requested)
	assert.Equal(t, "old-name", output.DomainName)
}

func TestReadDomainMapsNotFound(t *testing.T) {
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
		},
	}
	resource := DomainResource{DomainName: "example"}
	output, err := resource.read(context.Background(), client, nil)
	require.ErrorIs(t, err, runtime.ErrNotFound)
	assert.Nil(t, output)
}

func TestUpdateDomainMutationOrder(t *testing.T) {
	operations := []string{}
	priorTags := map[string]string{"remove": "old", "change": "old"}
	currentTags := map[string]string{"change": "new", "add": "value"}
	prior := runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg]{
		Inputs: DomainResource{
			DomainName:    "example",
			EngineVersion: stringPointer("OpenSearch_2.11"),
			ClusterConfig: &DomainClusterConfig{
				InstanceCount: 1, InstanceType: "t3.small.search",
			},
			Tags: &priorTags,
		},
		Outputs: &DomainResourceOutput{
			DomainName: "example", ARN: "arn:aws:es:us-east-1:123:domain/example",
		},
	}
	resource := DomainResource{
		DomainName:    "example",
		EngineVersion: stringPointer("OpenSearch_2.13"),
		ClusterConfig: &DomainClusterConfig{
			InstanceCount: 2, InstanceType: "t3.small.search",
		},
		Tags: &currentTags,
	}
	client := &fakeDomainClient{
		getCompatibleVersions: func(
			context.Context,
			*awssdk.GetCompatibleVersionsInput,
		) (*awssdk.GetCompatibleVersionsOutput, error) {
			operations = append(operations, "compatible")
			return &awssdk.GetCompatibleVersionsOutput{
				CompatibleVersions: []awstypes.CompatibleVersionsMap{
					{TargetVersions: []string{"OpenSearch_2.13"}},
				},
			}, nil
		},
		listTags: func(
			context.Context,
			*awssdk.ListTagsInput,
		) (*awssdk.ListTagsOutput, error) {
			operations = append(operations, "list-tags")
			return &awssdk.ListTagsOutput{TagList: []awstypes.Tag{
				{Key: aws.String("remove"), Value: aws.String("old")},
				{Key: aws.String("change"), Value: aws.String("old")},
			}}, nil
		},
		removeTags: func(
			context.Context,
			*awssdk.RemoveTagsInput,
		) (*awssdk.RemoveTagsOutput, error) {
			operations = append(operations, "remove-tags")
			return &awssdk.RemoveTagsOutput{}, nil
		},
		addTags: func(
			context.Context,
			*awssdk.AddTagsInput,
		) (*awssdk.AddTagsOutput, error) {
			operations = append(operations, "add-tags")
			return &awssdk.AddTagsOutput{}, nil
		},
		updateDomainConfig: func(
			context.Context,
			*awssdk.UpdateDomainConfigInput,
		) (*awssdk.UpdateDomainConfigOutput, error) {
			operations = append(operations, "update-config")
			return &awssdk.UpdateDomainConfigOutput{}, nil
		},
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			operations = append(operations, "describe")
			return &awssdk.DescribeDomainOutput{
				DomainStatus: completeDomainStatus("example"),
			}, nil
		},
		upgradeDomain: func(
			context.Context,
			*awssdk.UpgradeDomainInput,
		) (*awssdk.UpgradeDomainOutput, error) {
			operations = append(operations, "upgrade")
			return &awssdk.UpgradeDomainOutput{}, nil
		},
		getUpgradeStatus: func(
			context.Context,
			*awssdk.GetUpgradeStatusInput,
		) (*awssdk.GetUpgradeStatusOutput, error) {
			operations = append(operations, "upgrade-status")
			return &awssdk.GetUpgradeStatusOutput{
				UpgradeStep: awstypes.UpgradeStepUpgrade,
				StepStatus:  awstypes.UpgradeStatusSucceeded,
			}, nil
		},
	}
	output, err := resource.update(
		context.Background(), client, prior,
		shortDomainOptions(newRecordingDomainClock()),
	)
	require.NoError(t, err)
	assert.Equal(t, "example", output.DomainName)
	assert.Equal(t, []string{
		"compatible", "list-tags", "remove-tags", "add-tags", "update-config",
		"describe", "upgrade", "upgrade-status", "describe",
	}, operations)
}

func TestUpdateDomainReplacementPreflightPreventsMutation(t *testing.T) {
	mutated := false
	client := &fakeDomainClient{
		addTags: func(
			context.Context,
			*awssdk.AddTagsInput,
		) (*awssdk.AddTagsOutput, error) {
			mutated = true
			return &awssdk.AddTagsOutput{}, nil
		},
		updateDomainConfig: func(
			context.Context,
			*awssdk.UpdateDomainConfigInput,
		) (*awssdk.UpdateDomainConfigOutput, error) {
			mutated = true
			return &awssdk.UpdateDomainConfigOutput{}, nil
		},
	}
	prior := runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg]{
		Inputs: DomainResource{
			DomainName: "example",
			EncryptAtRest: &DomainEncryptionAtRestOptions{
				Enabled: true,
			},
		},
		Outputs: &DomainResourceOutput{DomainName: "example", ARN: "arn:test"},
	}
	resource := DomainResource{
		DomainName: "example",
		EncryptAtRest: &DomainEncryptionAtRestOptions{
			Enabled: false,
		},
	}
	_, err := resource.update(
		context.Background(), client, prior,
		shortDomainOptions(newRecordingDomainClock()),
	)
	require.Error(t, err)
	assert.False(t, mutated)
}

func TestUpdateDomainBuildsFallibleRequestsBeforeTagMutation(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*DomainResource)
		wantError string
	}{
		{
			name: "invalid policy",
			configure: func(resource *DomainResource) {
				resource.AccessPolicies = stringPointer("{invalid")
			},
			wantError: "access-policies",
		},
		{
			name: "invalid Auto-Tune",
			configure: func(resource *DomainResource) {
				resource.AutoTuneOptions = autoTuneWithSchedule("invalid", "HOURS", 2)
			},
			wantError: "start-at",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			priorTags := map[string]string{}
			currentTags := map[string]string{"new": "value"}
			prior := runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg]{
				Inputs: DomainResource{
					DomainName: "example", Tags: &priorTags,
					AccessPolicies: stringPointer(`{"Statement":[]}`),
				},
				Outputs: &DomainResourceOutput{DomainName: "example", ARN: "arn:test"},
			}
			resource := DomainResource{
				DomainName: "example", Tags: &currentTags,
				AccessPolicies: stringPointer(`{"Statement":[]}`),
			}
			test.configure(&resource)
			mutations := 0
			client := &fakeDomainClient{
				listTags: func(
					context.Context,
					*awssdk.ListTagsInput,
				) (*awssdk.ListTagsOutput, error) {
					return &awssdk.ListTagsOutput{}, nil
				},
				addTags: func(
					context.Context,
					*awssdk.AddTagsInput,
				) (*awssdk.AddTagsOutput, error) {
					mutations++
					return &awssdk.AddTagsOutput{}, nil
				},
				removeTags: func(
					context.Context,
					*awssdk.RemoveTagsInput,
				) (*awssdk.RemoveTagsOutput, error) {
					mutations++
					return &awssdk.RemoveTagsOutput{}, nil
				},
				updateDomainConfig: func(
					context.Context,
					*awssdk.UpdateDomainConfigInput,
				) (*awssdk.UpdateDomainConfigOutput, error) {
					mutations++
					return &awssdk.UpdateDomainConfigOutput{}, nil
				},
				upgradeDomain: func(
					context.Context,
					*awssdk.UpgradeDomainInput,
				) (*awssdk.UpgradeDomainOutput, error) {
					mutations++
					return &awssdk.UpgradeDomainOutput{}, nil
				},
			}
			_, err := resource.update(
				context.Background(), client, prior,
				shortDomainOptions(newRecordingDomainClock()),
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, test.wantError)
			assert.Zero(t, mutations)
		})
	}
}

func TestDeleteDomainPrefersPriorIdentityAndUsesIndependentWaitBudgets(t *testing.T) {
	operations := []string{}
	domainChecks := 0
	client := &fakeDomainClient{
		deleteDomain: func(
			_ context.Context,
			input *awssdk.DeleteDomainInput,
		) (*awssdk.DeleteDomainOutput, error) {
			operations = append(operations, "delete:"+aws.ToString(input.DomainName))
			return &awssdk.DeleteDomainOutput{}, nil
		},
		describeDomain: func(
			_ context.Context,
			input *awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			domainChecks++
			operations = append(operations, "describe:"+aws.ToString(input.DomainName))
			return &awssdk.DescribeDomainOutput{DomainStatus: &awstypes.DomainStatus{
				DomainName: input.DomainName,
				Processing: aws.Bool(domainChecks == 1),
			}}, nil
		},
		describeDomainConfig: func(
			_ context.Context,
			input *awssdk.DescribeDomainConfigInput,
		) (*awssdk.DescribeDomainConfigOutput, error) {
			operations = append(operations, "config:"+aws.ToString(input.DomainName))
			return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
		},
	}
	clock := newRecordingDomainClock()
	resource := DomainResource{DomainName: "new-name"}
	err := resource.delete(
		context.Background(), client, &DomainResourceOutput{DomainName: "old-name"},
		domainOperationOptions{clock: clock, deleteTimeout: 10*time.Minute + 15*time.Second},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"delete:old-name", "describe:old-name", "describe:old-name",
		"config:old-name", "config:old-name", "config:old-name",
	}, operations)
	assert.Equal(t, []time.Duration{
		domainDeleteInitialDelay, domainPollInterval,
		domainPollInterval, domainPollInterval,
	}, clock.sleeps)
}

func TestDeleteDomainTreatsNotFoundAsSuccess(t *testing.T) {
	waited := false
	client := &fakeDomainClient{
		deleteDomain: func(
			context.Context,
			*awssdk.DeleteDomainInput,
		) (*awssdk.DeleteDomainOutput, error) {
			return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
		},
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			waited = true
			return nil, nil
		},
	}
	resource := DomainResource{DomainName: "example"}
	require.NoError(t, resource.delete(
		context.Background(), client, nil,
		domainOperationOptions{clock: newRecordingDomainClock()},
	))
	assert.False(t, waited)
}

func TestDeleteDomainPropagatesError(t *testing.T) {
	wantErr := errors.New("delete failed")
	client := &fakeDomainClient{
		deleteDomain: func(
			context.Context,
			*awssdk.DeleteDomainInput,
		) (*awssdk.DeleteDomainOutput, error) {
			return nil, wantErr
		},
	}
	resource := DomainResource{DomainName: "example"}
	err := resource.delete(
		context.Background(), client, nil,
		domainOperationOptions{clock: newRecordingDomainClock()},
	)
	require.ErrorIs(t, err, wantErr)
}

func completeDomainStatus(name string) *awstypes.DomainStatus {
	return &awstypes.DomainStatus{
		ARN:           aws.String("arn:aws:es:us-east-1:123456789012:domain/" + name),
		ClusterConfig: &awstypes.ClusterConfig{},
		Created:       aws.Bool(true),
		DomainId:      aws.String("123456789012/" + name),
		DomainName:    aws.String(name),
		Endpoint:      aws.String(name + ".test"),
		EngineVersion: aws.String("OpenSearch_2.13"),
		Processing:    aws.Bool(false),
	}
}

func shortDomainOptions(clock domainClock) domainOperationOptions {
	return domainOperationOptions{
		clock:              clock,
		createTimeout:      11 * time.Minute,
		updateTimeout:      2 * time.Minute,
		deleteTimeout:      11 * time.Minute,
		propagationTimeout: time.Minute,
	}
}
