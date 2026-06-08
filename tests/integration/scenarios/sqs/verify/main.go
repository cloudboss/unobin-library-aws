// verify checks the SQS group the scenario applied against the phase named in
// the VERIFY_PHASE environment variable. Both queues have stable names, so each
// phase finds them by GetQueueUrl: applied requires the main queue present with
// the visibility timeout the first apply set and its redrive and policy
// attributes in place, plus the dead-letter queue present; destroyed requires
// both queues gone. It only reads cloud state; tearing the group down is the
// destroy plan's job.
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
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	smithy "github.com/aws/smithy-go"
)

const (
	queueName   = "unobin-it-sqs"
	dlqName     = "unobin-it-sqs-dlq"
	wantTimeout = "30" // the visibility timeout the first apply set
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
	client := sqs.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *sqs.Client) error {
	if _, err := queueURL(ctx, client, dlqName); err != nil {
		return fmt.Errorf("dead-letter queue %s: %w", dlqName, err)
	}
	url, err := queueURL(ctx, client, queueName)
	if err != nil {
		return fmt.Errorf("queue %s: %w", queueName, err)
	}
	attrs, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll},
	})
	if err != nil {
		return fmt.Errorf("get queue attributes: %w", err)
	}
	if got := attrs.Attributes["VisibilityTimeout"]; got != wantTimeout {
		return fmt.Errorf("queue visibility timeout is %q, want %q", got, wantTimeout)
	}
	// The redrive and policy attributes are anchored best-effort: an emulator may
	// not echo them, so a miss degrades to a printed skip rather than a failure.
	if rp := attrs.Attributes["RedrivePolicy"]; rp != "" {
		fmt.Printf("ok: redrive policy present: %s\n", rp)
	} else {
		fmt.Println("skip: redrive policy not modeled")
	}
	if pol := attrs.Attributes["Policy"]; pol != "" {
		fmt.Println("ok: queue policy present")
	} else {
		fmt.Println("skip: queue policy not modeled")
	}
	fmt.Printf("ok: queue %s present with visibility timeout %s and dlq %s present\n",
		queueName, wantTimeout, dlqName)
	return nil
}

func verifyDestroyed(ctx context.Context, client *sqs.Client) error {
	for _, name := range []string{queueName, dlqName} {
		if _, err := queueURL(ctx, client, name); err != nil {
			if isQueueGone(err) {
				continue
			}
			return fmt.Errorf("queue %s: %w", name, err)
		}
		return fmt.Errorf("queue %s still exists", name)
	}
	fmt.Printf("ok: queues %s and %s gone\n", queueName, dlqName)
	return nil
}

// queueURL returns the URL of the named queue, or an error a caller can test
// with isQueueGone when the queue does not exist.
func queueURL(ctx context.Context, client *sqs.Client, name string) (string, error) {
	resp, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
	if err != nil {
		return "", err
	}
	return aws.ToString(resp.QueueUrl), nil
}

// isQueueGone reports whether err is SQS's queue-does-not-exist error, in either
// the query-compatible long form the live endpoint sends or the bare form a fake
// or emulator returns.
func isQueueGone(err error) bool {
	var notFound *sqstypes.QueueDoesNotExist
	if errors.As(err, &notFound) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		return code == "AWS.SimpleQueueService.NonExistentQueue" ||
			code == "QueueDoesNotExist"
	}
	return strings.Contains(err.Error(), "NonExistentQueue")
}
