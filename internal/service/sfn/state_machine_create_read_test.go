package sfn

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateMachineCreateSendsCompleteRequestAndReadsMetadata(t *testing.T) {
	client := &fakeStateMachineClient{}
	clock := &fakeStateMachineClock{}
	tags := map[string]string{"team": "platform", "environment": "test"}
	destination := "arn:aws:logs:us-east-1:123456789012:log-group:workflow:*"
	resource := baseStateMachine()
	resource.Type = "EXPRESS"
	resource.EncryptionConfiguration = &StateMachineEncryptionConfiguration{
		Type:                         "CUSTOMER_MANAGED_KMS_KEY",
		KMSKeyID:                     aws.String("alias/workflow"),
		KMSDataKeyReusePeriodSeconds: aws.Int64(120),
	}
	resource.LoggingConfiguration = &StateMachineLoggingConfiguration{
		IncludeExecutionData: aws.Bool(true),
		Level:                aws.String("ERROR"),
		LogDestination:       &destination,
	}
	resource.TracingConfiguration = &StateMachineTracingConfiguration{Enabled: aws.Bool(true)}
	resource.Tags = &tags

	out, err := resource.create(context.Background(), client, stateMachineOptions(clock))
	require.NoError(t, err)
	assert.Equal(t, &StateMachineResourceOutput{
		ARN:        testStateMachineARN,
		RevisionID: "revision-1",
	}, out)
	assert.Equal(t, []string{"validate", "create", "describe"}, client.calls)

	require.Len(t, client.validateInputs, 1)
	assert.Equal(t, testDefinition, aws.ToString(client.validateInputs[0].Definition))
	assert.Equal(t, sfntypes.StateMachineTypeExpress, client.validateInputs[0].Type)

	require.Len(t, client.createInputs, 1)
	input := client.createInputs[0]
	assert.Equal(t, "workflow", aws.ToString(input.Name))
	assert.Equal(t, testDefinition, aws.ToString(input.Definition))
	assert.Equal(t, testRoleARN, aws.ToString(input.RoleArn))
	assert.Equal(t, sfntypes.StateMachineTypeExpress, input.Type)
	assert.Equal(t, sfntypes.EncryptionTypeCustomerManagedKmsKey,
		input.EncryptionConfiguration.Type)
	assert.Equal(t, "alias/workflow", aws.ToString(input.EncryptionConfiguration.KmsKeyId))
	assert.Equal(t, int32(120), aws.ToInt32(
		input.EncryptionConfiguration.KmsDataKeyReusePeriodSeconds))
	assert.Equal(t, sfntypes.LogLevelError, input.LoggingConfiguration.Level)
	assert.True(t, input.LoggingConfiguration.IncludeExecutionData)
	require.Len(t, input.LoggingConfiguration.Destinations, 1)
	assert.Equal(t, destination, aws.ToString(
		input.LoggingConfiguration.Destinations[0].CloudWatchLogsLogGroup.LogGroupArn))
	assert.True(t, input.TracingConfiguration.Enabled)
	assert.Equal(t, []sfntypes.Tag{
		{Key: aws.String("environment"), Value: aws.String("test")},
		{Key: aws.String("team"), Value: aws.String("platform")},
	}, input.Tags)
	require.Len(t, client.describeInputs, 1)
	assert.Equal(t, sfntypes.IncludedDataMetadataOnly, client.describeInputs[0].IncludedData)
}

func TestStateMachineCreateGeneratesName(t *testing.T) {
	client := &fakeStateMachineClient{}
	clock := &fakeStateMachineClock{}
	resource := baseStateMachine()
	resource.Name = nil
	resource.Type = ""
	options := stateMachineOptions(clock)
	options.random = bytes.NewReader([]byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05,
		0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b,
	})

	_, err := resource.create(context.Background(), client, options)
	require.NoError(t, err)
	require.Len(t, client.createInputs, 1)
	assert.Equal(t, "unobin-000102030405060708090a0b",
		aws.ToString(client.createInputs[0].Name))
	assert.Equal(t, sfntypes.StateMachineTypeStandard, client.createInputs[0].Type)
	assert.Equal(t, sfntypes.StateMachineTypeStandard, client.validateInputs[0].Type)
	assert.Nil(t, client.createInputs[0].EncryptionConfiguration)
	assert.Nil(t, client.createInputs[0].LoggingConfiguration)
	assert.Nil(t, client.createInputs[0].TracingConfiguration)
}

func TestStateMachineCreateSendsExplicitClears(t *testing.T) {
	client := &fakeStateMachineClient{}
	resource := baseStateMachine()
	resource.EncryptionConfiguration = &StateMachineEncryptionConfiguration{
		Type: "AWS_OWNED_KEY",
	}
	resource.LoggingConfiguration = &StateMachineLoggingConfiguration{
		IncludeExecutionData: aws.Bool(true),
		Level:                aws.String("OFF"),
		LogDestination: aws.String(
			"arn:aws:logs:us-east-1:123456789012:log-group:workflow:*"),
	}
	resource.TracingConfiguration = &StateMachineTracingConfiguration{Enabled: aws.Bool(false)}

	_, err := resource.create(context.Background(), client,
		stateMachineOptions(&fakeStateMachineClock{}))
	require.NoError(t, err)
	input := client.createInputs[0]
	require.NotNil(t, input.EncryptionConfiguration)
	assert.Equal(t, sfntypes.EncryptionTypeAwsOwnedKey,
		input.EncryptionConfiguration.Type)
	assert.Nil(t, input.EncryptionConfiguration.KmsKeyId)
	require.NotNil(t, input.LoggingConfiguration)
	assert.Equal(t, sfntypes.LogLevelOff, input.LoggingConfiguration.Level)
	assert.False(t, input.LoggingConfiguration.IncludeExecutionData)
	assert.Empty(t, input.LoggingConfiguration.Destinations)
	require.NotNil(t, input.TracingConfiguration)
	assert.False(t, input.TracingConfiguration.Enabled)
}

func TestStateMachineCreateEntropyFailurePrecedesCreate(t *testing.T) {
	client := &fakeStateMachineClient{}
	resource := baseStateMachine()
	resource.Name = nil
	options := stateMachineOptions(&fakeStateMachineClock{})
	options.random = errorReader{}

	_, err := resource.create(context.Background(), client, options)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generate state machine name")
	assert.Equal(t, []string{"validate"}, client.calls)
}

func TestStateMachineCreateDefinitionValidationFailure(t *testing.T) {
	client := &fakeStateMachineClient{
		validateOutputs: []*sfn.ValidateStateMachineDefinitionOutput{
			{
				Result: sfntypes.ValidateStateMachineDefinitionResultCodeFail,
				Diagnostics: []sfntypes.ValidateStateMachineDefinitionDiagnostic{
					{
						Code:     aws.String("SCHEMA_VALIDATION_FAILED"),
						Message:  aws.String("missing End or Next"),
						Location: aws.String("/States/Pass"),
						Severity: sfntypes.ValidateStateMachineDefinitionSeverityError,
					},
				},
				Truncated: aws.Bool(true),
			},
		},
	}
	resource := baseStateMachine()

	_, err := resource.create(context.Background(), client,
		stateMachineOptions(&fakeStateMachineClock{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SCHEMA_VALIDATION_FAILED")
	assert.Contains(t, err.Error(), "missing End or Next")
	assert.Contains(t, err.Error(), "truncated")
	assert.Equal(t, []string{"validate"}, client.calls)
}

func TestStateMachineCreateAcceptsValidationWarnings(t *testing.T) {
	client := &fakeStateMachineClient{
		validateOutputs: []*sfn.ValidateStateMachineDefinitionOutput{
			{
				Result: sfntypes.ValidateStateMachineDefinitionResultCodeOk,
				Diagnostics: []sfntypes.ValidateStateMachineDefinitionDiagnostic{
					{
						Code:     aws.String("NO_PATH"),
						Message:  aws.String("looks like a path"),
						Severity: sfntypes.ValidateStateMachineDefinitionSeverityWarning,
					},
				},
			},
		},
	}
	resource := baseStateMachine()

	_, err := resource.create(context.Background(), client,
		stateMachineOptions(&fakeStateMachineClock{}))
	require.NoError(t, err)
	assert.Equal(t, []string{"validate", "create", "describe"}, client.calls)
}

func TestStateMachineCreateValidationAPIErrorPrecedesMutation(t *testing.T) {
	client := &fakeStateMachineClient{validateErrors: []error{errors.New("validation unavailable")}}
	resource := baseStateMachine()

	_, err := resource.create(context.Background(), client,
		stateMachineOptions(&fakeStateMachineClock{}))
	require.Error(t, err)
	assert.ErrorContains(t, err, "validation unavailable")
	assert.Equal(t, []string{"validate"}, client.calls)
}

func TestStateMachineCreateRetriesOnlyAcceptedCodes(t *testing.T) {
	client := &fakeStateMachineClient{
		createErrors: []error{
			&smithy.GenericAPIError{Code: "AccessDeniedException", Message: "role pending"},
			&sfntypes.StateMachineDeleting{},
			nil,
		},
	}
	clock := &fakeStateMachineClock{}
	resource := baseStateMachine()

	_, err := resource.create(context.Background(), client, stateMachineOptions(clock))
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, clock.sleeps)
	require.Len(t, client.createInputs, 3)
	assert.Equal(t, client.createInputs[0], client.createInputs[1])
	assert.Equal(t, client.createInputs[0], client.createInputs[2])
}

func TestStateMachineCreateDoesNotRetryOtherErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "kms access denied",
			err:  &sfntypes.KmsAccessDeniedException{},
		},
		{
			name: "already exists",
			err:  &smithy.GenericAPIError{Code: "StateMachineAlreadyExists"},
		},
		{
			name: "unrelated",
			err:  errors.New("broken"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeStateMachineClient{createErrors: []error{tt.err}}
			clock := &fakeStateMachineClock{}
			resource := baseStateMachine()

			_, err := resource.create(context.Background(), client, stateMachineOptions(clock))
			require.Error(t, err)
			assert.Empty(t, clock.sleeps)
			require.Len(t, client.createInputs, 1)
		})
	}
}

func TestStateMachineCreateRetryConsumesWindow(t *testing.T) {
	client := &fakeStateMachineClient{}
	for range 20 {
		client.createErrors = append(client.createErrors,
			&smithy.GenericAPIError{Code: "AccessDeniedException"})
	}
	clock := &fakeStateMachineClock{}
	options := stateMachineOptions(clock)
	options.createWindow = 30 * time.Second
	resource := baseStateMachine()

	_, err := resource.create(context.Background(), client, options)
	require.Error(t, err)
	assert.Equal(t, 30*time.Second, clock.now.Sub(time.Time{}))
	assert.Equal(t, []time.Duration{
		500 * time.Millisecond,
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		10 * time.Second,
		4500 * time.Millisecond,
	}, clock.sleeps)
}

func TestStateMachineCreateRetryHonorsCancellation(t *testing.T) {
	client := &fakeStateMachineClient{
		createErrors: []error{&sfntypes.StateMachineDeleting{}},
	}
	clock := &fakeStateMachineClock{}
	resource := baseStateMachine()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resource.create(ctx, client, stateMachineOptions(clock))
	assert.ErrorIs(t, err, context.Canceled)
	require.Len(t, client.createInputs, 1)
}

func TestStateMachineCreateRejectsMalformedSuccess(t *testing.T) {
	tests := []struct {
		name string
		out  *sfn.CreateStateMachineOutput
	}{
		{name: "nil output"},
		{name: "empty arn", out: &sfn.CreateStateMachineOutput{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeStateMachineClient{
				createOutputs: []*sfn.CreateStateMachineOutput{tt.out},
			}
			if tt.out == nil {
				client.createNil = true
			}
			resource := baseStateMachine()
			_, err := resource.create(context.Background(), client,
				stateMachineOptions(&fakeStateMachineClock{}))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "ARN")
			assert.NotContains(t, client.calls, "describe")
		})
	}
}

func TestStateMachineCreateDoesNotRetryImmediateRead(t *testing.T) {
	client := &fakeStateMachineClient{
		describeErrors: []error{&sfntypes.StateMachineDoesNotExist{}},
	}
	resource := baseStateMachine()

	_, err := resource.create(context.Background(), client,
		stateMachineOptions(&fakeStateMachineClock{}))
	assert.ErrorIs(t, err, runtime.ErrNotFound)
	require.Len(t, client.describeInputs, 1)
}

func TestStateMachineRead(t *testing.T) {
	resource := baseStateMachine()
	prior := &StateMachineResourceOutput{ARN: testStateMachineARN, RevisionID: "old"}

	t.Run("success", func(t *testing.T) {
		client := &fakeStateMachineClient{}
		out, err := resource.read(context.Background(), client, prior)
		require.NoError(t, err)
		assert.Equal(t, &StateMachineResourceOutput{
			ARN: testStateMachineARN, RevisionID: "revision-1",
		}, out)
		require.Len(t, client.describeInputs, 1)
		assert.Equal(t, sfntypes.IncludedDataMetadataOnly,
			client.describeInputs[0].IncludedData)
	})

	t.Run("typed absence", func(t *testing.T) {
		client := &fakeStateMachineClient{
			describeErrors: []error{&sfntypes.StateMachineDoesNotExist{}},
		}
		_, err := resource.read(context.Background(), client, prior)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	})

	t.Run("nil response", func(t *testing.T) {
		client := &fakeStateMachineClient{describeNil: true}
		_, err := resource.read(context.Background(), client, prior)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
	})

	t.Run("unrelated error", func(t *testing.T) {
		client := &fakeStateMachineClient{describeErrors: []error{errors.New("denied")}}
		_, err := resource.read(context.Background(), client, prior)
		require.Error(t, err)
		assert.ErrorContains(t, err, "denied")
		assert.False(t, errors.Is(err, runtime.ErrNotFound))
	})

	t.Run("missing prior", func(t *testing.T) {
		client := &fakeStateMachineClient{}
		_, err := resource.read(context.Background(), client, nil)
		assert.ErrorIs(t, err, runtime.ErrNotFound)
		assert.Empty(t, client.calls)
	})
}
