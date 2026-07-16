package sfn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateMachineUpdateValidatesWaitsAndReads(t *testing.T) {
	priorResource := baseStateMachine()
	current := priorResource
	current.Definition = `{"States":{"Done":{"End":true,"Type":"Pass"}},"StartAt":"Done"}`
	old := stateMachineDescription(testDefinition, testRoleARN, "revision-1")
	converged := stateMachineDescription(
		"{\n  \"StartAt\": \"Done\",\n  \"States\": {\"Done\": {\"Type\": \"Pass\", \"End\": true}}\n}",
		testRoleARN,
		"revision-2",
	)
	client := &fakeStateMachineClient{
		describeOutputs: []*sfn.DescribeStateMachineOutput{old, converged, converged},
	}
	clock := &fakeStateMachineClock{}
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs:  priorResource,
		Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN, RevisionID: "revision-1"},
	}

	out, err := current.update(context.Background(), client, prior, stateMachineOptions(clock))
	require.NoError(t, err)
	assert.Equal(t, "revision-2", out.RevisionID)
	assert.Equal(t, []string{
		"validate", "update", "describe", "describe", "describe",
	}, client.calls)
	assert.Equal(t, []time.Duration{500 * time.Millisecond}, clock.sleeps)
	require.Len(t, client.updateInputs, 1)
	assert.Equal(t, current.Definition, aws.ToString(client.updateInputs[0].Definition))
	assert.Equal(t, testRoleARN, aws.ToString(client.updateInputs[0].RoleArn))
	assert.Nil(t, client.updateInputs[0].EncryptionConfiguration)
	assert.Nil(t, client.updateInputs[0].LoggingConfiguration)
	assert.Nil(t, client.updateInputs[0].TracingConfiguration)
	assert.Equal(t, sfntypes.IncludedDataAllData, client.describeInputs[0].IncludedData)
	assert.Equal(t, sfntypes.IncludedDataAllData, client.describeInputs[1].IncludedData)
	assert.Equal(t, sfntypes.IncludedDataMetadataOnly, client.describeInputs[2].IncludedData)
}

func TestStateMachineUpdateReconcilesTagsBeforeConfiguration(t *testing.T) {
	priorTags := map[string]string{"old": "value", "team": "old"}
	currentTags := map[string]string{"team": "new", "new": "value"}
	priorResource := baseStateMachine()
	priorResource.Tags = &priorTags
	current := priorResource
	current.RoleARN = "arn:aws:iam::123456789012:role/new-workflow"
	current.Tags = &currentTags
	converged := stateMachineDescription(testDefinition, current.RoleARN, "revision-2")
	client := &fakeStateMachineClient{
		listTagOutputs: []*sfn.ListTagsForResourceOutput{
			{Tags: []sfntypes.Tag{
				{Key: aws.String("old"), Value: aws.String("value")},
				{Key: aws.String("team"), Value: aws.String("old")},
				{Key: aws.String("aws:managed"), Value: aws.String("keep")},
			}},
		},
		describeOutputs: []*sfn.DescribeStateMachineOutput{converged, converged},
	}
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs:  priorResource,
		Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN, RevisionID: "revision-1"},
	}

	_, err := current.update(context.Background(), client, prior,
		stateMachineOptions(&fakeStateMachineClock{}))
	require.NoError(t, err)
	assert.Equal(t, []string{
		"list-tags", "untag", "tag", "update", "describe", "describe",
	}, client.calls)
	require.Len(t, client.untagInputs, 1)
	assert.Equal(t, []string{"old"}, client.untagInputs[0].TagKeys)
	require.Len(t, client.tagInputs, 1)
	assert.Equal(t, []sfntypes.Tag{
		{Key: aws.String("new"), Value: aws.String("value")},
		{Key: aws.String("team"), Value: aws.String("new")},
	}, client.tagInputs[0].Tags)
}

func TestStateMachineUpdateTagOnlySkipsStateMachineMutation(t *testing.T) {
	priorTags := map[string]string{"old": "value"}
	currentTags := map[string]string{}
	priorResource := baseStateMachine()
	priorResource.Tags = &priorTags
	current := priorResource
	current.Tags = &currentTags
	client := &fakeStateMachineClient{
		listTagOutputs: []*sfn.ListTagsForResourceOutput{
			{Tags: []sfntypes.Tag{{Key: aws.String("old"), Value: aws.String("value")}}},
		},
	}
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs:  priorResource,
		Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN, RevisionID: "revision-1"},
	}

	_, err := current.update(context.Background(), client, prior,
		stateMachineOptions(&fakeStateMachineClock{}))
	require.NoError(t, err)
	assert.Equal(t, []string{"list-tags", "untag", "describe"}, client.calls)
	assert.Empty(t, client.updateInputs)
	assert.Empty(t, client.tagInputs)
}

func TestStateMachineUpdateUnchangedReadsOnly(t *testing.T) {
	resource := baseStateMachine()
	client := &fakeStateMachineClient{}
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs:  resource,
		Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN, RevisionID: "revision-1"},
	}

	out, err := resource.update(context.Background(), client, prior,
		stateMachineOptions(&fakeStateMachineClock{}))
	require.NoError(t, err)
	assert.Equal(t, "revision-1", out.RevisionID)
	assert.Equal(t, []string{"describe"}, client.calls)
}

func TestStateMachineUpdateOptionalConfigurationRequests(t *testing.T) {
	destination := "arn:aws:logs:us-east-1:123456789012:log-group:workflow:*"
	tests := []struct {
		name       string
		mutate     func(*StateMachineResource)
		describe   func(*sfn.DescribeStateMachineOutput)
		assertions func(*testing.T, *sfn.UpdateStateMachineInput)
	}{
		{
			name: "encryption",
			mutate: func(r *StateMachineResource) {
				r.EncryptionConfiguration = &StateMachineEncryptionConfiguration{
					Type:                         "CUSTOMER_MANAGED_KMS_KEY",
					KMSKeyID:                     aws.String("alias/workflow"),
					KMSDataKeyReusePeriodSeconds: aws.Int64(60),
				}
			},
			describe: func(out *sfn.DescribeStateMachineOutput) {
				out.EncryptionConfiguration = &sfntypes.EncryptionConfiguration{
					Type:                         sfntypes.EncryptionTypeCustomerManagedKmsKey,
					KmsKeyId:                     aws.String("alias/workflow"),
					KmsDataKeyReusePeriodSeconds: aws.Int32(60),
				}
			},
			assertions: func(t *testing.T, input *sfn.UpdateStateMachineInput) {
				require.NotNil(t, input.EncryptionConfiguration)
				assert.Nil(t, input.LoggingConfiguration)
				assert.Nil(t, input.TracingConfiguration)
			},
		},
		{
			name: "logging",
			mutate: func(r *StateMachineResource) {
				r.LoggingConfiguration = &StateMachineLoggingConfiguration{
					IncludeExecutionData: aws.Bool(true),
					Level:                aws.String("ALL"),
					LogDestination:       &destination,
				}
			},
			describe: func(out *sfn.DescribeStateMachineOutput) {
				out.LoggingConfiguration = &sfntypes.LoggingConfiguration{
					IncludeExecutionData: true,
					Level:                sfntypes.LogLevelAll,
					Destinations: []sfntypes.LogDestination{
						{CloudWatchLogsLogGroup: &sfntypes.CloudWatchLogsLogGroup{
							LogGroupArn: &destination,
						}},
					},
				}
			},
			assertions: func(t *testing.T, input *sfn.UpdateStateMachineInput) {
				assert.Nil(t, input.EncryptionConfiguration)
				require.NotNil(t, input.LoggingConfiguration)
				assert.Nil(t, input.TracingConfiguration)
			},
		},
		{
			name: "tracing false",
			mutate: func(r *StateMachineResource) {
				r.TracingConfiguration = &StateMachineTracingConfiguration{Enabled: aws.Bool(false)}
			},
			describe: func(out *sfn.DescribeStateMachineOutput) {
				out.TracingConfiguration = &sfntypes.TracingConfiguration{Enabled: false}
			},
			assertions: func(t *testing.T, input *sfn.UpdateStateMachineInput) {
				assert.Nil(t, input.EncryptionConfiguration)
				assert.Nil(t, input.LoggingConfiguration)
				require.NotNil(t, input.TracingConfiguration)
				assert.False(t, input.TracingConfiguration.Enabled)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priorResource := baseStateMachine()
			current := priorResource
			tt.mutate(&current)
			observed := stateMachineDescription(testDefinition, testRoleARN, "revision-2")
			tt.describe(observed)
			client := &fakeStateMachineClient{
				describeOutputs: []*sfn.DescribeStateMachineOutput{observed, observed},
			}
			prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
				Inputs: priorResource,
				Outputs: &StateMachineResourceOutput{
					ARN: testStateMachineARN, RevisionID: "revision-1",
				},
			}

			_, err := current.update(context.Background(), client, prior,
				stateMachineOptions(&fakeStateMachineClock{}))
			require.NoError(t, err)
			require.Len(t, client.updateInputs, 1)
			tt.assertions(t, client.updateInputs[0])
			assert.Equal(t, sfntypes.IncludedDataMetadataOnly,
				client.describeInputs[0].IncludedData)
		})
	}
}

func TestStateMachineUpdateWaitIgnoresUnsentEncryptionPeriod(t *testing.T) {
	priorResource := baseStateMachine()
	current := priorResource
	current.EncryptionConfiguration = &StateMachineEncryptionConfiguration{
		Type:     "CUSTOMER_MANAGED_KMS_KEY",
		KMSKeyID: aws.String("alias/workflow"),
	}
	observed := stateMachineDescription(testDefinition, testRoleARN, "revision-2")
	observed.EncryptionConfiguration = &sfntypes.EncryptionConfiguration{
		Type:                         sfntypes.EncryptionTypeCustomerManagedKmsKey,
		KmsKeyId:                     aws.String("alias/workflow"),
		KmsDataKeyReusePeriodSeconds: aws.Int32(300),
	}
	client := &fakeStateMachineClient{
		describeOutputs: []*sfn.DescribeStateMachineOutput{observed, observed},
	}
	clock := &fakeStateMachineClock{}
	options := stateMachineOptions(clock)
	options.updateWindow = time.Second
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs: priorResource,
		Outputs: &StateMachineResourceOutput{
			ARN: testStateMachineARN, RevisionID: "revision-1",
		},
	}

	out, err := current.update(context.Background(), client, prior, options)
	require.NoError(t, err)
	assert.Equal(t, "revision-2", out.RevisionID)
	assert.Empty(t, clock.sleeps)
}

func TestStateMachineUpdateRemovingConfigurationSendsNil(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*StateMachineResource)
	}{
		{
			name: "encryption",
			mutate: func(r *StateMachineResource) {
				r.EncryptionConfiguration = &StateMachineEncryptionConfiguration{
					Type: "AWS_OWNED_KEY",
				}
			},
		},
		{
			name: "logging",
			mutate: func(r *StateMachineResource) {
				r.LoggingConfiguration = &StateMachineLoggingConfiguration{
					Level: aws.String("OFF"),
				}
			},
		},
		{
			name: "tracing",
			mutate: func(r *StateMachineResource) {
				r.TracingConfiguration = &StateMachineTracingConfiguration{
					Enabled: aws.Bool(true),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priorResource := baseStateMachine()
			tt.mutate(&priorResource)
			current := baseStateMachine()
			converged := stateMachineDescription(testDefinition, testRoleARN, "revision-2")
			client := &fakeStateMachineClient{
				describeOutputs: []*sfn.DescribeStateMachineOutput{converged, converged},
			}
			prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
				Inputs: priorResource,
				Outputs: &StateMachineResourceOutput{
					ARN: testStateMachineARN, RevisionID: "revision-1",
				},
			}

			_, err := current.update(context.Background(), client, prior,
				stateMachineOptions(&fakeStateMachineClock{}))
			require.NoError(t, err)
			require.Len(t, client.updateInputs, 1)
			assert.Nil(t, client.updateInputs[0].EncryptionConfiguration)
			assert.Nil(t, client.updateInputs[0].LoggingConfiguration)
			assert.Nil(t, client.updateInputs[0].TracingConfiguration)
			assert.Equal(t, []string{"update", "describe", "describe"}, client.calls)
		})
	}
}

func TestStateMachineUpdateDefinitionValidationPrecedesTags(t *testing.T) {
	priorResource := baseStateMachine()
	current := priorResource
	current.Definition = `{"invalid":true}`
	tags := map[string]string{"team": "new"}
	current.Tags = &tags
	client := &fakeStateMachineClient{
		validateErrors: []error{errors.New("validation failed")},
	}
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs:  priorResource,
		Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN},
	}

	_, err := current.update(context.Background(), client, prior,
		stateMachineOptions(&fakeStateMachineClock{}))
	require.Error(t, err)
	assert.Equal(t, []string{"validate"}, client.calls)
}

func TestStateMachineUpdateDoesNotRetryMutationError(t *testing.T) {
	priorResource := baseStateMachine()
	current := priorResource
	current.RoleARN = "arn:aws:iam::123456789012:role/new-workflow"
	client := &fakeStateMachineClient{updateErrors: []error{errors.New("conflict")}}
	clock := &fakeStateMachineClock{}
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs:  priorResource,
		Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN},
	}

	_, err := current.update(context.Background(), client, prior, stateMachineOptions(clock))
	require.Error(t, err)
	assert.Empty(t, clock.sleeps)
	require.Len(t, client.updateInputs, 1)
	assert.Empty(t, client.describeInputs)
}

func TestStateMachineUpdateWaitChecksCompleteConfiguration(t *testing.T) {
	destination := "arn:aws:logs:us-east-1:123456789012:log-group:workflow:*"
	wrongDestination := "arn:aws:logs:us-east-1:123456789012:log-group:wrong:*"
	priorResource := baseStateMachine()
	current := priorResource
	current.EncryptionConfiguration = &StateMachineEncryptionConfiguration{
		Type:                         "CUSTOMER_MANAGED_KMS_KEY",
		KMSKeyID:                     aws.String("alias/workflow"),
		KMSDataKeyReusePeriodSeconds: aws.Int64(60),
	}
	current.LoggingConfiguration = &StateMachineLoggingConfiguration{
		IncludeExecutionData: aws.Bool(true),
		Level:                aws.String("ALL"),
		LogDestination:       &destination,
	}
	first := stateMachineDescription(testDefinition, testRoleARN, "revision-2")
	first.EncryptionConfiguration = &sfntypes.EncryptionConfiguration{
		Type:                         sfntypes.EncryptionTypeAwsOwnedKey,
		KmsKeyId:                     aws.String("alias/workflow"),
		KmsDataKeyReusePeriodSeconds: aws.Int32(60),
	}
	first.LoggingConfiguration = &sfntypes.LoggingConfiguration{
		IncludeExecutionData: true,
		Level:                sfntypes.LogLevelAll,
		Destinations: []sfntypes.LogDestination{
			{CloudWatchLogsLogGroup: &sfntypes.CloudWatchLogsLogGroup{
				LogGroupArn: &wrongDestination,
			}},
		},
	}
	second := stateMachineDescription(testDefinition, testRoleARN, "revision-2")
	second.EncryptionConfiguration = &sfntypes.EncryptionConfiguration{
		Type:                         sfntypes.EncryptionTypeCustomerManagedKmsKey,
		KmsKeyId:                     aws.String("alias/workflow"),
		KmsDataKeyReusePeriodSeconds: aws.Int32(60),
	}
	second.LoggingConfiguration = &sfntypes.LoggingConfiguration{
		IncludeExecutionData: true,
		Level:                sfntypes.LogLevelAll,
		Destinations: []sfntypes.LogDestination{
			{CloudWatchLogsLogGroup: &sfntypes.CloudWatchLogsLogGroup{
				LogGroupArn: &destination,
			}},
		},
	}
	client := &fakeStateMachineClient{
		describeOutputs: []*sfn.DescribeStateMachineOutput{first, second, second},
	}
	clock := &fakeStateMachineClock{}
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs:  priorResource,
		Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN},
	}

	_, err := current.update(context.Background(), client, prior, stateMachineOptions(clock))
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{500 * time.Millisecond}, clock.sleeps)
}

func TestStateMachineUpdateWaitStopsOnDescribeError(t *testing.T) {
	priorResource := baseStateMachine()
	current := priorResource
	current.RoleARN = "arn:aws:iam::123456789012:role/new-workflow"
	client := &fakeStateMachineClient{describeErrors: []error{errors.New("describe denied")}}
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs:  priorResource,
		Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN},
	}

	_, err := current.update(context.Background(), client, prior,
		stateMachineOptions(&fakeStateMachineClock{}))
	require.Error(t, err)
	assert.ErrorContains(t, err, "describe denied")
	require.Len(t, client.describeInputs, 1)
}

func TestStateMachineUpdateWaitDoesNotAcceptMissingConfiguration(t *testing.T) {
	priorResource := baseStateMachine()
	current := priorResource
	current.TracingConfiguration = &StateMachineTracingConfiguration{Enabled: aws.Bool(true)}
	client := &fakeStateMachineClient{}
	clock := &fakeStateMachineClock{}
	options := stateMachineOptions(clock)
	options.updateWindow = time.Second
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs:  priorResource,
		Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN},
	}

	_, err := current.update(context.Background(), client, prior, options)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Equal(t, []time.Duration{500 * time.Millisecond, 500 * time.Millisecond},
		clock.sleeps)
}

func TestStateMachineUpdateWaitTimeoutAndCancellation(t *testing.T) {
	priorResource := baseStateMachine()
	current := priorResource
	current.RoleARN = "arn:aws:iam::123456789012:role/new-workflow"
	prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
		Inputs:  priorResource,
		Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN},
	}

	t.Run("timeout", func(t *testing.T) {
		client := &fakeStateMachineClient{}
		clock := &fakeStateMachineClock{}
		options := stateMachineOptions(clock)
		options.updateWindow = 3 * time.Second
		_, err := current.update(context.Background(), client, prior, options)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timed out")
		assert.Equal(t, []time.Duration{
			500 * time.Millisecond, time.Second, 1500 * time.Millisecond,
		}, clock.sleeps)
	})

	t.Run("cancellation", func(t *testing.T) {
		client := &fakeStateMachineClient{}
		clock := &fakeStateMachineClock{sleepErr: context.Canceled}
		_, err := current.update(context.Background(), client, prior,
			stateMachineOptions(clock))
		assert.ErrorIs(t, err, context.Canceled)
		require.Len(t, client.describeInputs, 1)
	})
}

func TestStateMachineUpdateStopsAfterTagErrors(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fakeStateMachineClient)
		calls     []string
	}{
		{
			name: "list",
			configure: func(c *fakeStateMachineClient) {
				c.listTagErrors = []error{errors.New("list failed")}
			},
			calls: []string{"list-tags"},
		},
		{
			name: "untag",
			configure: func(c *fakeStateMachineClient) {
				c.listTagOutputs = []*sfn.ListTagsForResourceOutput{
					{Tags: []sfntypes.Tag{{Key: aws.String("old"), Value: aws.String("value")}}},
				}
				c.untagErrors = []error{errors.New("untag failed")}
			},
			calls: []string{"list-tags", "untag"},
		},
		{
			name: "tag",
			configure: func(c *fakeStateMachineClient) {
				c.tagErrors = []error{errors.New("tag failed")}
			},
			calls: []string{"list-tags", "tag"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldTags := map[string]string{"old": "value"}
			newTags := map[string]string{"new": "value"}
			priorResource := baseStateMachine()
			priorResource.Tags = &oldTags
			current := priorResource
			current.Tags = &newTags
			client := &fakeStateMachineClient{}
			tt.configure(client)
			prior := runtime.Prior[StateMachineResource, *StateMachineResourceOutput]{
				Inputs:  priorResource,
				Outputs: &StateMachineResourceOutput{ARN: testStateMachineARN},
			}

			_, err := current.update(context.Background(), client, prior,
				stateMachineOptions(&fakeStateMachineClock{}))
			require.Error(t, err)
			assert.Equal(t, tt.calls, client.calls)
		})
	}
}
