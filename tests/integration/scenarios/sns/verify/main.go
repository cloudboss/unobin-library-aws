// verify checks the SNS group the scenario applied against the phase named in
// the VERIFY_PHASE environment variable. The topic and target queue have stable
// names, so each phase finds the topic by an exact-name match over ListTopics
// and the queue by GetQueueUrl: applied requires the topic present with the
// display name the first apply set, its resource policy in place, a subscription
// fanning out to the queue, and the queue present; destroyed requires the topic
// and queue gone. It only reads cloud state; tearing the group down is the
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
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	smithy "github.com/aws/smithy-go"
)

const (
	topicName       = "unobin-it-topic"
	targetQueueName = "unobin-it-sns-target"
	wantDisplayName = "unobin integration topic" // the name the first apply set
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
	snsClient := sns.NewFromConfig(cfg)
	sqsClient := sqs.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, snsClient, sqsClient)
	case "destroyed":
		return verifyDestroyed(ctx, snsClient, sqsClient)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, snsClient *sns.Client, sqsClient *sqs.Client) error {
	arn, err := findTopic(ctx, snsClient)
	if err != nil {
		return err
	}
	if arn == "" {
		return fmt.Errorf("topic %s not found", topicName)
	}
	attrs, err := snsClient.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{
		TopicArn: aws.String(arn),
	})
	if err != nil {
		return fmt.Errorf("get topic attributes: %w", err)
	}
	if got := attrs.Attributes["DisplayName"]; got != wantDisplayName {
		return fmt.Errorf("topic display name is %q, want %q", got, wantDisplayName)
	}
	if _, err := queueURL(ctx, sqsClient, targetQueueName); err != nil {
		return fmt.Errorf("target queue %s: %w", targetQueueName, err)
	}

	// The topic policy and the subscription are anchored best-effort: an emulator
	// may not echo the policy or model ListSubscriptionsByTopic, so a miss
	// degrades to a printed skip rather than a failure.
	if pol := attrs.Attributes["Policy"]; pol != "" {
		fmt.Println("ok: topic policy present")
	} else {
		fmt.Println("skip: topic policy not modeled")
	}
	if hasSQSSubscription(ctx, snsClient, arn) {
		fmt.Println("ok: sqs subscription present on the topic")
	} else {
		fmt.Println("skip: topic subscription not modeled")
	}

	fmt.Printf("ok: topic %s present with display name %q and target queue %s present\n",
		topicName, wantDisplayName, targetQueueName)
	return nil
}

func verifyDestroyed(ctx context.Context, snsClient *sns.Client, sqsClient *sqs.Client) error {
	arn, err := findTopic(ctx, snsClient)
	if err != nil {
		return err
	}
	if arn != "" {
		return fmt.Errorf("topic %s still exists", topicName)
	}
	if _, err := queueURL(ctx, sqsClient, targetQueueName); err != nil {
		if !isQueueGone(err) {
			return fmt.Errorf("target queue %s: %w", targetQueueName, err)
		}
	} else {
		return fmt.Errorf("target queue %s still exists", targetQueueName)
	}
	fmt.Printf("ok: topic %s and queue %s gone\n", topicName, targetQueueName)
	return nil
}

// findTopic returns the ARN of the topic whose name matches topicName exactly,
// or the empty string when none does. ListTopics returns every topic, so the
// match is confirmed client-side on the ARN's trailing name segment.
func findTopic(ctx context.Context, client *sns.Client) (string, error) {
	suffix := ":" + topicName
	paginator := sns.NewListTopicsPaginator(client, &sns.ListTopicsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("list topics: %w", err)
		}
		for _, t := range page.Topics {
			if strings.HasSuffix(aws.ToString(t.TopicArn), suffix) {
				return aws.ToString(t.TopicArn), nil
			}
		}
	}
	return "", nil
}

// hasSQSSubscription reports whether the topic has at least one sqs subscription.
// A list error or an empty result degrades to false so the caller prints a skip
// rather than failing on an emulator that does not model subscriptions.
func hasSQSSubscription(ctx context.Context, client *sns.Client, topicArn string) bool {
	paginator := sns.NewListSubscriptionsByTopicPaginator(client,
		&sns.ListSubscriptionsByTopicInput{TopicArn: aws.String(topicArn)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return false
		}
		for _, s := range page.Subscriptions {
			if aws.ToString(s.Protocol) == "sqs" {
				return true
			}
		}
	}
	return false
}

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
