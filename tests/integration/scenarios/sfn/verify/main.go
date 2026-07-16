package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
)

const (
	stateMachineName  = "unobin-it-state-machine"
	initialDefinition = `{"StartAt":"Initial","States":` +
		`{"Initial":{"Type":"Pass","Result":"initial","End":true}}}`
	updatedDefinition = `{"StartAt":"Updated","States":` +
		`{"Updated":{"Type":"Pass","Result":"updated","End":true}}}`
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	configuration, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := sfn.NewFromConfig(configuration)
	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "updated":
		return verifyUpdated(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied, updated, or destroyed, got %q", phase)
	}
}

type verifierClient interface {
	DescribeStateMachine(
		context.Context,
		*sfn.DescribeStateMachineInput,
		...func(*sfn.Options),
	) (*sfn.DescribeStateMachineOutput, error)
	ListStateMachines(
		context.Context,
		*sfn.ListStateMachinesInput,
		...func(*sfn.Options),
	) (*sfn.ListStateMachinesOutput, error)
	ListTagsForResource(
		context.Context,
		*sfn.ListTagsForResourceInput,
		...func(*sfn.Options),
	) (*sfn.ListTagsForResourceOutput, error)
}

func verifyApplied(ctx context.Context, client verifierClient) error {
	stateMachineARN, err := findStateMachineARN(ctx, client)
	if err != nil {
		return err
	}
	creationDate, err := verifyPresent(
		ctx, client, stateMachineARN, initialDefinition, true, "initial", nil,
	)
	if err != nil {
		return err
	}
	if err := os.WriteFile(stateMachineARNPath(), []byte(stateMachineARN), 0o600); err != nil {
		return fmt.Errorf("record state machine arn: %w", err)
	}
	data := []byte(creationDate.UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(stateMachineCreationDatePath(), data, 0o600); err != nil {
		return fmt.Errorf("record state machine creation date: %w", err)
	}
	return nil
}

func verifyUpdated(ctx context.Context, client verifierClient) error {
	data, err := os.ReadFile(stateMachineARNPath())
	if err != nil {
		return fmt.Errorf("read state machine arn: %w", err)
	}
	expectedARN := string(data)
	observedARN, err := findStateMachineARN(ctx, client)
	if err != nil {
		return err
	}
	if observedARN != expectedARN {
		return fmt.Errorf("state machine ARN is %q, want %q", observedARN, expectedARN)
	}
	expectedCreationDate, err := readStateMachineCreationDate()
	if err != nil {
		return err
	}
	_, err = verifyPresent(
		ctx, client, observedARN, updatedDefinition, false, "updated",
		&expectedCreationDate,
	)
	return err
}

func verifyPresent(
	ctx context.Context,
	client verifierClient,
	stateMachineARN string,
	definition string,
	tracingEnabled bool,
	tagValue string,
	expectedCreationDate *time.Time,
) (time.Time, error) {
	output, err := client.DescribeStateMachine(ctx, &sfn.DescribeStateMachineInput{
		StateMachineArn: aws.String(stateMachineARN),
		IncludedData:    sfntypes.IncludedDataAllData,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"describe state machine %s: %w", stateMachineName, err)
	}
	if output.CreationDate == nil {
		return time.Time{}, fmt.Errorf(
			"state machine %s has no creation date", stateMachineName)
	}
	creationDate := output.CreationDate.UTC()
	if expectedCreationDate != nil && !creationDate.Equal(*expectedCreationDate) {
		return time.Time{}, fmt.Errorf(
			"state machine creation date is %s, want %s",
			creationDate.Format(time.RFC3339Nano),
			expectedCreationDate.Format(time.RFC3339Nano),
		)
	}
	if output.Type != sfntypes.StateMachineTypeStandard {
		return time.Time{}, fmt.Errorf(
			"state machine type is %q, want STANDARD", output.Type)
	}
	if !strings.HasSuffix(aws.ToString(output.RoleArn), ":role/unobin-it-sfn-role") {
		return time.Time{}, fmt.Errorf(
			"state machine role is %q", aws.ToString(output.RoleArn))
	}
	if output.TracingConfiguration == nil ||
		output.TracingConfiguration.Enabled != tracingEnabled {
		return time.Time{}, fmt.Errorf(
			"state machine tracing enabled is not %t", tracingEnabled)
	}
	if !sameJSON(aws.ToString(output.Definition), definition) {
		return time.Time{}, fmt.Errorf(
			"state machine definition does not match expected definition")
	}
	tags, err := client.ListTagsForResource(ctx, &sfn.ListTagsForResourceInput{
		ResourceArn: aws.String(stateMachineARN),
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("list state machine tags: %w", err)
	}
	for _, tag := range tags.Tags {
		if aws.ToString(tag.Key) == "unobin" && aws.ToString(tag.Value) == tagValue {
			fmt.Printf("ok: state machine %s matches the %s configuration\n",
				stateMachineName, tagValue)
			return creationDate, nil
		}
	}
	return time.Time{}, fmt.Errorf("state machine tag unobin is not %q", tagValue)
}

func verifyDestroyed(ctx context.Context, client *sfn.Client) error {
	data, err := os.ReadFile(stateMachineARNPath())
	if err != nil {
		return fmt.Errorf("read state machine arn: %w", err)
	}
	_, err = client.DescribeStateMachine(ctx, &sfn.DescribeStateMachineInput{
		StateMachineArn: aws.String(string(data)),
		IncludedData:    sfntypes.IncludedDataMetadataOnly,
	})
	if err == nil {
		return fmt.Errorf("state machine %s still exists", stateMachineName)
	}
	var notFound *sfntypes.StateMachineDoesNotExist
	if !errors.As(err, &notFound) {
		return err
	}
	fmt.Printf("ok: state machine %s is gone\n", stateMachineName)
	return nil
}

func stateMachineARNPath() string {
	return filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "state-machine-arn")
}

func stateMachineCreationDatePath() string {
	return filepath.Join(
		os.Getenv("VERIFY_BUILD_DIR"), "state-machine-creation-date")
}

func readStateMachineCreationDate() (time.Time, error) {
	data, err := os.ReadFile(stateMachineCreationDatePath())
	if err != nil {
		return time.Time{}, fmt.Errorf("read state machine creation date: %w", err)
	}
	creationDate, err := time.Parse(time.RFC3339Nano, string(data))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse state machine creation date: %w", err)
	}
	return creationDate, nil
}

func findStateMachineARN(ctx context.Context, client verifierClient) (string, error) {
	paginator := sfn.NewListStateMachinesPaginator(client, &sfn.ListStateMachinesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("list state machines: %w", err)
		}
		for _, stateMachine := range page.StateMachines {
			if aws.ToString(stateMachine.Name) == stateMachineName {
				return aws.ToString(stateMachine.StateMachineArn), nil
			}
		}
	}
	return "", &sfntypes.StateMachineDoesNotExist{}
}

func sameJSON(left, right string) bool {
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
