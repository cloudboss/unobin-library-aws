package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func main() {
	if err := run(context.Background(), os.Getenv("VERIFY_PHASE")); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, phase string) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := lambda.NewFromConfig(cfg)
	out, err := client.GetFunctionEventInvokeConfig(ctx, &lambda.GetFunctionEventInvokeConfigInput{
		FunctionName: aws.String("unobin-it-event-invoke"),
	})
	if phase == "destroyed" {
		var missing *lambdatypes.ResourceNotFoundException
		if errors.As(err, &missing) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get destroyed settings: %w", err)
		}
		return errors.New("asynchronous invocation settings still exist")
	}
	if err != nil {
		return fmt.Errorf("get asynchronous invocation settings: %w", err)
	}
	switch phase {
	case "applied":
		if out.MaximumRetryAttempts == nil || *out.MaximumRetryAttempts != 0 ||
			aws.ToInt32(out.MaximumEventAgeInSeconds) != 60 {
			return fmt.Errorf("expected retry count 0 and event age 60, got %+v", out)
		}
	case "updated":
		age := aws.ToInt32(out.MaximumEventAgeInSeconds)
		if aws.ToInt32(out.MaximumRetryAttempts) != 1 || (age != 21600 && age != 0) {
			return fmt.Errorf("expected retry count 1 and cleared event age, got %+v", out)
		}
	default:
		return fmt.Errorf("unknown verification phase %q", phase)
	}
	return nil
}
