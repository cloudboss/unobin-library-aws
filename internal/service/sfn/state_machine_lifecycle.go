package sfn

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

func (r *StateMachineResource) createStateMachine(
	ctx context.Context,
	client stateMachineClient,
	options stateMachineOperationOptions,
) (*StateMachineResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	stateMachineType := resolvedStateMachineType(r.Type)
	if err := validateStateMachineDefinition(
		ctx, client, r.Definition, stateMachineType,
	); err != nil {
		return nil, err
	}
	name, err := stateMachineName(r.Name, options.random)
	if err != nil {
		return nil, err
	}
	input := &sfn.CreateStateMachineInput{
		Definition:              aws.String(r.Definition),
		Name:                    aws.String(name),
		RoleArn:                 aws.String(r.RoleARN),
		Type:                    stateMachineType,
		EncryptionConfiguration: stateMachineEncryption(r.EncryptionConfiguration),
		LoggingConfiguration:    stateMachineLogging(r.LoggingConfiguration),
		TracingConfiguration:    stateMachineTracing(r.TracingConfiguration),
		Tags:                    stateMachineTags(stateMachineTagMap(r.Tags)),
	}
	var output *sfn.CreateStateMachineOutput
	err = retryStateMachineCreate(ctx, options.clock, options.createWindow,
		func(ctx context.Context) error {
			var callErr error
			output, callErr = client.CreateStateMachine(ctx, input)
			return callErr
		})
	if err != nil {
		return nil, fmt.Errorf("create state machine %s: %w", name, err)
	}
	if output == nil || aws.ToString(output.StateMachineArn) == "" {
		return nil, errors.New("create state machine returned no ARN")
	}
	return r.read(ctx, client, &StateMachineResourceOutput{
		ARN: aws.ToString(output.StateMachineArn),
	})
}

func (r *StateMachineResource) updateStateMachine(
	ctx context.Context,
	client stateMachineClient,
	prior runtime.Prior[StateMachineResource, *StateMachineResourceOutput],
	options stateMachineOperationOptions,
) (*StateMachineResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	if prior.Outputs == nil || prior.Outputs.ARN == "" {
		return nil, errors.New("update state machine requires a prior ARN")
	}
	definitionChanged := runtime.Changed(prior.Inputs.Definition, r.Definition)
	if definitionChanged {
		if err := validateStateMachineDefinition(
			ctx, client, r.Definition, resolvedStateMachineType(r.Type),
		); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(stateMachineTagMap(prior.Inputs.Tags), stateMachineTagMap(r.Tags)) {
		if err := r.syncTags(ctx, client, prior.Outputs.ARN); err != nil {
			return nil, err
		}
	}
	encryptionChanged := runtime.Changed(
		prior.Inputs.EncryptionConfiguration, r.EncryptionConfiguration)
	loggingChanged := runtime.Changed(
		prior.Inputs.LoggingConfiguration, r.LoggingConfiguration)
	tracingChanged := runtime.Changed(
		prior.Inputs.TracingConfiguration, r.TracingConfiguration)
	roleChanged := runtime.Changed(prior.Inputs.RoleARN, r.RoleARN)
	if !definitionChanged && !roleChanged && !encryptionChanged &&
		!loggingChanged && !tracingChanged {
		return r.read(ctx, client, prior.Outputs)
	}
	input := &sfn.UpdateStateMachineInput{
		StateMachineArn: aws.String(prior.Outputs.ARN),
		Definition:      aws.String(r.Definition),
		RoleArn:         aws.String(r.RoleARN),
	}
	if encryptionChanged && r.EncryptionConfiguration != nil {
		input.EncryptionConfiguration = stateMachineEncryption(r.EncryptionConfiguration)
	}
	if loggingChanged && r.LoggingConfiguration != nil {
		input.LoggingConfiguration = stateMachineLogging(r.LoggingConfiguration)
	}
	if tracingChanged && r.TracingConfiguration != nil {
		input.TracingConfiguration = stateMachineTracing(r.TracingConfiguration)
	}
	if _, err := client.UpdateStateMachine(ctx, input); err != nil {
		return nil, fmt.Errorf("update state machine %s: %w", prior.Outputs.ARN, err)
	}
	targets := stateMachineUpdateTargets{
		definition: r.Definition,
		roleARN:    r.RoleARN,
	}
	if input.EncryptionConfiguration != nil {
		targets.encryption = input.EncryptionConfiguration
	}
	if input.LoggingConfiguration != nil {
		targets.logging = input.LoggingConfiguration
	}
	if input.TracingConfiguration != nil {
		targets.tracing = input.TracingConfiguration
	}
	includedData := sfntypes.IncludedDataMetadataOnly
	if definitionChanged {
		includedData = sfntypes.IncludedDataAllData
	}
	err := pollStateMachine(ctx, options.clock, options.updateWindow,
		500*time.Millisecond,
		func(ctx context.Context) (bool, error) {
			output, err := describeStateMachine(ctx, client, prior.Outputs.ARN, includedData)
			if err != nil {
				return false, err
			}
			return targets.matches(output, definitionChanged), nil
		},
		func() error {
			return fmt.Errorf("timed out waiting for state machine %s update", prior.Outputs.ARN)
		})
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Outputs)
}

func (r *StateMachineResource) deleteStateMachine(
	ctx context.Context,
	client stateMachineClient,
	prior *StateMachineResourceOutput,
	options stateMachineOperationOptions,
) error {
	if prior == nil || prior.ARN == "" {
		return errors.New("delete state machine requires a prior ARN")
	}
	_, err := client.DeleteStateMachine(ctx, &sfn.DeleteStateMachineInput{
		StateMachineArn: aws.String(prior.ARN),
	})
	if err != nil {
		return fmt.Errorf("delete state machine %s: %w", prior.ARN, err)
	}
	return pollStateMachine(ctx, options.clock, options.deleteWindow,
		200*time.Millisecond,
		func(ctx context.Context) (bool, error) {
			output, err := describeStateMachine(
				ctx, client, prior.ARN, sfntypes.IncludedDataMetadataOnly)
			if errors.Is(err, runtime.ErrNotFound) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			switch output.Status {
			case sfntypes.StateMachineStatusActive, sfntypes.StateMachineStatusDeleting:
				return false, nil
			default:
				return false, fmt.Errorf("state machine %s entered unexpected status %q",
					prior.ARN, output.Status)
			}
		},
		func() error {
			return fmt.Errorf("timed out waiting for state machine %s deletion", prior.ARN)
		})
}

func readStateMachine(
	ctx context.Context,
	client stateMachineClient,
	stateMachineARN string,
	includedData sfntypes.IncludedData,
) (*StateMachineResourceOutput, error) {
	output, err := describeStateMachine(ctx, client, stateMachineARN, includedData)
	if err != nil {
		return nil, err
	}
	return &StateMachineResourceOutput{
		ARN:        aws.ToString(output.StateMachineArn),
		RevisionID: aws.ToString(output.RevisionId),
	}, nil
}

func describeStateMachine(
	ctx context.Context,
	client stateMachineClient,
	stateMachineARN string,
	includedData sfntypes.IncludedData,
) (*sfn.DescribeStateMachineOutput, error) {
	output, err := client.DescribeStateMachine(ctx, &sfn.DescribeStateMachineInput{
		StateMachineArn: aws.String(stateMachineARN),
		IncludedData:    includedData,
	})
	if err != nil {
		var notFound *sfntypes.StateMachineDoesNotExist
		if errors.As(err, &notFound) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe state machine %s: %w", stateMachineARN, err)
	}
	if output == nil {
		return nil, runtime.ErrNotFound
	}
	return output, nil
}

func validateStateMachineDefinition(
	ctx context.Context,
	client stateMachineClient,
	definition string,
	stateMachineType sfntypes.StateMachineType,
) error {
	output, err := client.ValidateStateMachineDefinition(ctx,
		&sfn.ValidateStateMachineDefinitionInput{
			Definition: aws.String(definition),
			Type:       stateMachineType,
		})
	if err != nil {
		return fmt.Errorf("validate state machine definition: %w", err)
	}
	if output == nil {
		return errors.New("validate state machine definition returned no result")
	}
	if output.Result == sfntypes.ValidateStateMachineDefinitionResultCodeOk {
		return nil
	}
	parts := make([]string, 0, len(output.Diagnostics)+1)
	for _, diagnostic := range output.Diagnostics {
		part := strings.TrimSpace(strings.Join([]string{
			aws.ToString(diagnostic.Code),
			aws.ToString(diagnostic.Message),
			aws.ToString(diagnostic.Location),
		}, ": "))
		parts = append(parts, part)
	}
	if aws.ToBool(output.Truncated) {
		parts = append(parts, "diagnostics truncated")
	}
	if len(parts) == 0 {
		parts = append(parts, "no diagnostics returned")
	}
	return fmt.Errorf("state machine definition validation failed: %s",
		strings.Join(parts, "; "))
}

func stateMachineName(explicit *string, random io.Reader) (string, error) {
	if explicit != nil {
		return *explicit, nil
	}
	bytes := make([]byte, 12)
	if _, err := io.ReadFull(random, bytes); err != nil {
		return "", fmt.Errorf("generate state machine name: %w", err)
	}
	return "unobin-" + hex.EncodeToString(bytes), nil
}

func retryableStateMachineCreate(err error) bool {
	var apiError smithy.APIError
	if !errors.As(err, &apiError) {
		return false
	}
	return apiError.ErrorCode() == "StateMachineDeleting" ||
		apiError.ErrorCode() == "AccessDeniedException"
}

func stateMachineEncryption(
	configuration *StateMachineEncryptionConfiguration,
) *sfntypes.EncryptionConfiguration {
	if configuration == nil {
		return nil
	}
	output := &sfntypes.EncryptionConfiguration{
		Type:     sfntypes.EncryptionType(configuration.Type),
		KmsKeyId: configuration.KMSKeyID,
	}
	if configuration.KMSDataKeyReusePeriodSeconds != nil {
		value := int32(*configuration.KMSDataKeyReusePeriodSeconds)
		output.KmsDataKeyReusePeriodSeconds = &value
	}
	return output
}

func stateMachineLogging(
	configuration *StateMachineLoggingConfiguration,
) *sfntypes.LoggingConfiguration {
	if configuration == nil {
		return nil
	}
	level := sfntypes.LogLevelOff
	if configuration.Level != nil {
		level = sfntypes.LogLevel(*configuration.Level)
	}
	output := &sfntypes.LoggingConfiguration{Level: level}
	if level == sfntypes.LogLevelOff {
		return output
	}
	output.IncludeExecutionData = aws.ToBool(configuration.IncludeExecutionData)
	if configuration.LogDestination != nil {
		output.Destinations = []sfntypes.LogDestination{
			{CloudWatchLogsLogGroup: &sfntypes.CloudWatchLogsLogGroup{
				LogGroupArn: configuration.LogDestination,
			}},
		}
	}
	return output
}

func stateMachineTracing(
	configuration *StateMachineTracingConfiguration,
) *sfntypes.TracingConfiguration {
	if configuration == nil {
		return nil
	}
	return &sfntypes.TracingConfiguration{Enabled: aws.ToBool(configuration.Enabled)}
}

func (r *StateMachineResource) syncTags(
	ctx context.Context,
	client stateMachineClient,
	stateMachineARN string,
) error {
	return tagsync.Sync(ctx, stateMachineTagMap(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			output, err := client.ListTagsForResource(ctx, &sfn.ListTagsForResourceInput{
				ResourceArn: aws.String(stateMachineARN),
			})
			if err != nil {
				return nil, fmt.Errorf("list state machine tags %s: %w", stateMachineARN, err)
			}
			values := map[string]string{}
			if output != nil {
				for _, tag := range output.Tags {
					values[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}
			}
			return values, nil
		},
		func(ctx context.Context, values map[string]string) error {
			_, err := client.TagResource(ctx, &sfn.TagResourceInput{
				ResourceArn: aws.String(stateMachineARN),
				Tags:        stateMachineTags(values),
			})
			if err != nil {
				return fmt.Errorf("tag state machine %s: %w", stateMachineARN, err)
			}
			return nil
		},
		func(ctx context.Context, keys []string) error {
			_, err := client.UntagResource(ctx, &sfn.UntagResourceInput{
				ResourceArn: aws.String(stateMachineARN),
				TagKeys:     keys,
			})
			if err != nil {
				return fmt.Errorf("untag state machine %s: %w", stateMachineARN, err)
			}
			return nil
		})
}

type stateMachineUpdateTargets struct {
	definition string
	roleARN    string
	encryption *sfntypes.EncryptionConfiguration
	logging    *sfntypes.LoggingConfiguration
	tracing    *sfntypes.TracingConfiguration
}

func (t stateMachineUpdateTargets) matches(
	output *sfn.DescribeStateMachineOutput,
	compareDefinition bool,
) bool {
	if compareDefinition && !semanticJSONEqual(t.definition, aws.ToString(output.Definition)) {
		return false
	}
	if aws.ToString(output.RoleArn) != t.roleARN {
		return false
	}
	if !encryptionConfigurationsEqual(t.encryption, output.EncryptionConfiguration) {
		return false
	}
	if t.logging != nil && !loggingConfigurationsEqual(t.logging, output.LoggingConfiguration) {
		return false
	}
	return t.tracing == nil || reflect.DeepEqual(t.tracing, output.TracingConfiguration)
}

func encryptionConfigurationsEqual(
	expected *sfntypes.EncryptionConfiguration,
	actual *sfntypes.EncryptionConfiguration,
) bool {
	if expected == nil {
		return true
	}
	if actual == nil || expected.Type != actual.Type {
		return false
	}
	if expected.KmsKeyId != nil &&
		(actual.KmsKeyId == nil ||
			aws.ToString(expected.KmsKeyId) != aws.ToString(actual.KmsKeyId)) {
		return false
	}
	return expected.KmsDataKeyReusePeriodSeconds == nil ||
		(actual.KmsDataKeyReusePeriodSeconds != nil &&
			aws.ToInt32(expected.KmsDataKeyReusePeriodSeconds) ==
				aws.ToInt32(actual.KmsDataKeyReusePeriodSeconds))
}

func semanticJSONEqual(left, right string) bool {
	var leftValue any
	if err := json.Unmarshal([]byte(left), &leftValue); err != nil {
		return false
	}
	var rightValue any
	if err := json.Unmarshal([]byte(right), &rightValue); err != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func loggingConfigurationsEqual(
	expected *sfntypes.LoggingConfiguration,
	actual *sfntypes.LoggingConfiguration,
) bool {
	if expected == nil || actual == nil || expected.Level != actual.Level ||
		expected.IncludeExecutionData != actual.IncludeExecutionData ||
		len(expected.Destinations) != len(actual.Destinations) {
		return false
	}
	for index := range expected.Destinations {
		expectedGroup := expected.Destinations[index].CloudWatchLogsLogGroup
		actualGroup := actual.Destinations[index].CloudWatchLogsLogGroup
		if expectedGroup == nil || actualGroup == nil ||
			aws.ToString(expectedGroup.LogGroupArn) != aws.ToString(actualGroup.LogGroupArn) {
			return false
		}
	}
	return true
}
