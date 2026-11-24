package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

const functionName = "unobin-it-lambda-function-lookup"

func main() {
	if err := run(context.Background(), os.Getenv("VERIFY_BUILD_DIR"),
		os.Getenv("VERIFY_PHASE")); err != nil {
		log.Fatal(err)
	}
}

type lookupOutput struct {
	Arn                 string            `json:"arn"`
	QualifiedArn        string            `json:"qualified-arn"`
	Version             string            `json:"version"`
	Hash                string            `json:"hash"`
	CompatibilityHash   string            `json:"compatibility-hash"`
	Size                int64             `json:"size"`
	Description         string            `json:"description"`
	Memory              int32             `json:"memory"`
	Timeout             int32             `json:"timeout"`
	Handler             string            `json:"handler"`
	Runtime             string            `json:"runtime"`
	Role                string            `json:"role"`
	Environment         map[string]string `json:"environment"`
	Tags                map[string]string `json:"tags"`
	Concurrency         int32             `json:"concurrency"`
	ExplicitConcurrency int32             `json:"explicit-concurrency"`
	Signing             string            `json:"signing"`
	Invoke              string            `json:"invoke"`
	QualifiedInvoke     string            `json:"qualified-invoke"`
	StreamingInvoke     string            `json:"streaming-invoke"`
}

func run(ctx context.Context, directory, phase string) error {
	if directory == "" {
		return errors.New("VERIFY_BUILD_DIR is required")
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := lambda.NewFromConfig(cfg)
	if phase == "destroyed" {
		_, err := client.GetFunction(ctx,
			&lambda.GetFunctionInput{FunctionName: aws.String(functionName)})
		if _, ok := errors.AsType[*types.ResourceNotFoundException](err); ok {
			return nil
		}
		if err != nil {
			return err
		}
		return errors.New("managed function still exists")
	}
	stack, description, memory, timeout := "config.ub", "initial", int32(128), int32(3)
	switch phase {
	case "applied":
	case "updated":
		stack, description, memory, timeout = "update/config.ub", "updated", 256, 10
	default:
		return fmt.Errorf("unknown phase %q", phase)
	}
	if err := verify(ctx, client, directory, stack, description, memory, timeout); err != nil {
		return err
	}
	if phase == "applied" {
		if _, err := client.PutFunctionConcurrency(ctx, &lambda.PutFunctionConcurrencyInput{
			FunctionName: aws.String(functionName), ReservedConcurrentExecutions: aws.Int32(5),
		}); err != nil {
			return err
		}
		if err := refreshLookup(ctx, directory, stack); err != nil {
			return err
		}
		if err := verify(ctx, client, directory, stack, description, memory, timeout); err != nil {
			return err
		}
	}
	return nil
}

func verify(
	ctx context.Context, client *lambda.Client, directory, stack, description string,
	memory, timeout int32,
) error {
	command := exec.CommandContext(ctx, filepath.Join(directory, "lambda-function-lookup"),
		"output", "lookup", "-c", stack, "--format", "json")
	command.Dir, command.Stderr = directory, os.Stderr
	data, err := command.Output()
	if err != nil {
		return err
	}
	var result struct {
		Value lookupOutput `json:"value"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	remote, err := client.GetFunction(ctx,
		&lambda.GetFunctionInput{FunctionName: aws.String(functionName)})
	if err != nil {
		return err
	}
	if remote.Code == nil || remote.Configuration == nil {
		return errors.New("empty cloud response")
	}
	concurrency, err := client.GetFunctionConcurrency(ctx, &lambda.GetFunctionConcurrencyInput{
		FunctionName: aws.String(functionName),
	})
	if err != nil {
		return err
	}
	signing, err := client.GetFunctionCodeSigningConfig(ctx, &lambda.GetFunctionCodeSigningConfigInput{
		FunctionName: aws.String(functionName),
	})
	if err != nil {
		return err
	}
	c := remote.Configuration
	if aws.ToString(c.Description) != description || aws.ToInt32(c.MemorySize) != memory ||
		aws.ToInt32(c.Timeout) != timeout || c.Environment == nil ||
		!reflect.DeepEqual(c.Environment.Variables, map[string]string{"PHASE": description}) ||
		!reflect.DeepEqual(remote.Tags, map[string]string{"phase": description}) {
		return fmt.Errorf("cloud configuration differs: %#v; tags %#v", c, remote.Tags)
	}
	archive,
		err := os.ReadFile("tests/integration/scenarios/lambda-function-lookup/_output/function.zip")
	if err != nil {
		return err
	}
	digest := sha256.Sum256(archive)
	checksum := base64.StdEncoding.EncodeToString(digest[:])
	if aws.ToString(c.CodeSha256) != checksum || c.CodeSize != int64(len(archive)) {
		return errors.New("cloud deployment package metadata differs")
	}
	prefix := "arn:" + partition.Of(client.Options().Region) + ":apigateway:" +
		client.Options().Region + ":lambda:path/"
	rawArn := aws.ToString(c.FunctionArn)
	qualifiedArn := rawArn + ":" + aws.ToString(c.Version)
	latestConcurrency := int32(-1)
	if remote.Concurrency != nil {
		latestConcurrency = aws.ToInt32(remote.Concurrency.ReservedConcurrentExecutions)
	}
	signingArn := ""
	if signing != nil {
		signingArn = aws.ToString(signing.CodeSigningConfigArn)
	}
	want := lookupOutput{
		Arn: rawArn, QualifiedArn: qualifiedArn, Version: aws.ToString(c.Version),
		Hash: checksum, CompatibilityHash: checksum, Size: int64(len(archive)),
		Description: description, Memory: memory, Timeout: timeout, Handler: "index.handler",
		Runtime: "python3.12", Role: aws.ToString(c.Role), Environment: c.Environment.Variables,
		Tags: remote.Tags, Concurrency: latestConcurrency,
		ExplicitConcurrency: aws.ToInt32(concurrency.ReservedConcurrentExecutions), Signing: signingArn,
		Invoke:          prefix + "2015-03-31/functions/" + rawArn + "/invocations",
		QualifiedInvoke: prefix + "2015-03-31/functions/" + qualifiedArn + "/invocations",
		StreamingInvoke: prefix + "2021-11-15/functions/" + rawArn + "/response-streaming-invocations",
	}
	if !reflect.DeepEqual(want, result.Value) {
		return fmt.Errorf("lookup = %#v; want %#v", result.Value, want)
	}
	return nil
}

func refreshLookup(ctx context.Context, directory, stack string) error {
	command := exec.CommandContext(ctx, filepath.Join(directory, "lambda-function-lookup"),
		"plan", "-c", stack, "-o", "observed.ubp")
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("plan drifted lookup: %w\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(directory, "observed.ubp"))
	if err != nil {
		return err
	}
	plan, err := runtime.OpenPlan(data, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	if err != nil {
		return err
	}
	found := false
	for _, step := range plan.Steps {
		if step.Address == "data-source.explicit" {
			found = true
			if step.ObservedOutputs["reserved-concurrent-executions"] != int64(5) {
				return fmt.Errorf("lookup did not observe remote concurrency: %#v", step.ObservedOutputs)
			}
		}
	}
	if !found {
		return errors.New("missing explicit lookup in saved plan")
	}
	command = exec.CommandContext(ctx, filepath.Join(directory, "lambda-function-lookup"),
		"apply", "observed.ubp")
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("apply drifted lookup: %w\n%s", err, output)
	}
	return nil
}
