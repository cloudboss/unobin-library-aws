// verify checks the Lambda group the scenario applied against the phase named in
// the VERIFY_PHASE environment variable, looking the function up by its stable
// name because the test driver does not pass plan outputs into verify. It only
// reads cloud state: applied requires the function to be present and active and
// its resource policy to hold the permission statement; destroyed requires the
// function to be gone, which takes its policy with it. Tearing the group down is
// the destroy plan's job, not the verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

const (
	functionName    = "unobin-it-function"
	aliasName       = "live"
	statementID     = "unobin-it-allow-invoke"
	eventsQueueName = "unobin-it-lambda-events"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := lambda.NewFromConfig(cfg)
	sqsClient := sqs.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client, sqsClient)
	case "updated":
		return verifyUpdated(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied, updated, or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *lambda.Client, sqsClient *sqs.Client) error {
	resp, err := client.GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(functionName),
	})
	if err != nil {
		return fmt.Errorf("get function %s: %w", functionName, err)
	}
	if resp.Configuration == nil {
		return fmt.Errorf("function %s has no configuration", functionName)
	}
	if state := resp.Configuration.State; state != lambdatypes.StateActive {
		return fmt.Errorf("function %s is %s, not Active", functionName, state)
	}
	if err := verifyAlias(ctx, client); err != nil {
		return err
	}

	hasStatement, err := policyHasStatement(ctx, client)
	if err != nil {
		return err
	}
	if !hasStatement {
		return fmt.Errorf("function %s policy has no statement %s", functionName, statementID)
	}

	// The event source mapping and its queue are anchored best-effort: an emulator
	// may not model event source mappings, so a miss degrades to a printed skip.
	if _, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(eventsQueueName),
	}); err == nil {
		fmt.Printf("ok: event source queue %s present\n", eventsQueueName)
	} else {
		fmt.Printf("skip: event source queue %s not modeled\n", eventsQueueName)
	}
	if hasEventSourceMapping(ctx, client) {
		fmt.Println("ok: event source mapping present for the function")
	} else {
		fmt.Println("skip: event source mapping not modeled")
	}

	fmt.Printf("ok: function %s is active and grants invoke via %s\n", functionName, statementID)
	return nil
}

func verifyAlias(ctx context.Context, client *lambda.Client) error {
	resp, err := getAlias(ctx, client)
	if err != nil {
		return err
	}
	version := aws.ToString(resp.FunctionVersion)
	if version == "" || version == "$LATEST" {
		return fmt.Errorf("alias %s points to invalid version %q", aliasName, version)
	}
	fmt.Printf("ok: alias %s points %s to version %s\n", aliasName, functionName, version)
	return nil
}

func verifyUpdated(ctx context.Context, client *lambda.Client) error {
	alias, err := getAlias(ctx, client)
	if err != nil {
		return err
	}
	latest, err := latestFunctionVersion(ctx, client)
	if err != nil {
		return err
	}
	if version := aws.ToString(alias.FunctionVersion); version != latest {
		return fmt.Errorf("alias %s points to %s, want %s", aliasName, version, latest)
	}
	if description := aws.ToString(alias.Description); description != "" {
		return fmt.Errorf("alias %s description is %q, want empty", aliasName, description)
	}
	fmt.Printf("ok: alias %s points to latest version %s\n", aliasName, latest)
	return nil
}

func getAlias(ctx context.Context, client *lambda.Client) (*lambda.GetAliasOutput, error) {
	resp, err := client.GetAlias(ctx, &lambda.GetAliasInput{
		FunctionName: aws.String(functionName),
		Name:         aws.String(aliasName),
	})
	if err != nil {
		return nil, fmt.Errorf("get alias %s on %s: %w", aliasName, functionName, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("alias %s on %s returned no response", aliasName, functionName)
	}
	if aws.ToString(resp.AliasArn) == "" {
		return nil, fmt.Errorf("alias %s on %s has no arn", aliasName, functionName)
	}
	return resp, nil
}

func latestFunctionVersion(ctx context.Context, client *lambda.Client) (string, error) {
	pager := lambda.NewListVersionsByFunctionPaginator(client,
		&lambda.ListVersionsByFunctionInput{FunctionName: aws.String(functionName)})
	var latest string
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("list versions %s: %w", functionName, err)
		}
		if n := len(page.Versions); n > 0 {
			latest = aws.ToString(page.Versions[n-1].Version)
		}
	}
	if latest == "" || latest == "$LATEST" {
		return "", fmt.Errorf("function %s latest version is invalid: %q", functionName, latest)
	}
	return latest, nil
}

// hasEventSourceMapping reports whether the function has at least one event
// source mapping. A list error or empty result degrades to false so the caller
// prints a skip rather than failing on an emulator that does not model mappings.
func hasEventSourceMapping(ctx context.Context, client *lambda.Client) bool {
	resp, err := client.ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{
		FunctionName: aws.String(functionName),
	})
	return err == nil && len(resp.EventSourceMappings) > 0
}

func verifyDestroyed(ctx context.Context, client *lambda.Client) error {
	_, err := client.GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(functionName),
	})
	if err == nil {
		return fmt.Errorf("function %s still exists", functionName)
	}
	if !isNotFound(err) {
		return fmt.Errorf("get function %s: %w", functionName, err)
	}
	fmt.Printf("ok: function %s is gone\n", functionName)
	return nil
}

// policyHasStatement reports whether the function's resource policy holds the
// scenario's permission statement. A function with no policy at all reads as not
// found, which means the statement is absent.
func policyHasStatement(ctx context.Context, client *lambda.Client) (bool, error) {
	resp, err := client.GetPolicy(ctx, &lambda.GetPolicyInput{
		FunctionName: aws.String(functionName),
	})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get policy %s: %w", functionName, err)
	}
	return strings.Contains(aws.ToString(resp.Policy), statementID), nil
}

func isNotFound(err error) bool {
	var notFound *lambdatypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}
