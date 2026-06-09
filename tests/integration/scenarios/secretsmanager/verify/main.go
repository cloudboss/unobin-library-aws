// verify checks the Secrets Manager secret the scenario applied against the
// phase named in the VERIFY_PHASE environment variable. The secret has a stable
// name, so applied requires it present with the value the first apply set, and
// destroyed requires it gone. It only reads cloud state; tearing the secret down
// is the destroy plan's job.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

const (
	secretName = "unobin-it-secret"
	wantValue  = "initial-secret" // the value the first apply set
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
	client := secretsmanager.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *secretsmanager.Client) error {
	resp, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	})
	if err != nil {
		return fmt.Errorf("get secret value %s: %w", secretName, err)
	}
	if got := aws.ToString(resp.SecretString); got != wantValue {
		return fmt.Errorf("secret value is %q, want %q", got, wantValue)
	}
	fmt.Printf("ok: secret %s present with the value the first apply set\n", secretName)
	return nil
}

func verifyDestroyed(ctx context.Context, client *secretsmanager.Client) error {
	resp, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(secretName),
	})
	if err != nil {
		if isSecretGone(err) {
			fmt.Printf("ok: secret %s gone\n", secretName)
			return nil
		}
		return fmt.Errorf("describe secret %s: %w", secretName, err)
	}
	// A force-deleted secret is removed outright; a windowed delete would leave it
	// with a deletion date set. Either counts as destroyed.
	if resp.DeletedDate != nil {
		fmt.Printf("ok: secret %s scheduled for deletion\n", secretName)
		return nil
	}
	return fmt.Errorf("secret %s still exists", secretName)
}

// isSecretGone reports whether err is Secrets Manager's not-found error.
func isSecretGone(err error) bool {
	var notFound *smtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}
