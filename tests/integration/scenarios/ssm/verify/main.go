// verify checks the SSM parameter the scenario applied against the phase named
// in the VERIFY_PHASE environment variable. The parameter has a stable name, so
// applied requires it present with the value the first apply set, and destroyed
// requires it gone. It only reads cloud state; tearing the parameter down is the
// destroy plan's job.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

const (
	paramName = "/unobin/it/string"
	wantValue = "initial" // the value the first apply set
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
	client := ssm.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *ssm.Client) error {
	resp, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(paramName),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("get parameter %s: %w", paramName, err)
	}
	if got := aws.ToString(resp.Parameter.Value); got != wantValue {
		return fmt.Errorf("parameter value is %q, want %q", got, wantValue)
	}
	fmt.Printf("ok: parameter %s present with value %q\n", paramName, wantValue)
	return nil
}

func verifyDestroyed(ctx context.Context, client *ssm.Client) error {
	_, err := client.GetParameter(ctx, &ssm.GetParameterInput{Name: aws.String(paramName)})
	if err != nil {
		if isParamGone(err) {
			fmt.Printf("ok: parameter %s gone\n", paramName)
			return nil
		}
		return fmt.Errorf("get parameter %s: %w", paramName, err)
	}
	return fmt.Errorf("parameter %s still exists", paramName)
}

// isParamGone reports whether err is SSM's parameter-not-found error.
func isParamGone(err error) bool {
	var notFound *ssmtypes.ParameterNotFound
	return errors.As(err, &notFound)
}
