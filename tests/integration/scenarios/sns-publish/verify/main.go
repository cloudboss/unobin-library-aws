package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

const name = "unobin-it-sns-publish"

type messageAttribute struct {
	Type  string
	Value string
}

type notification struct {
	Type              string
	TopicArn          string
	Message           string
	MessageAttributes map[string]messageAttribute
}

func main() {
	if err := run(os.Getenv("VERIFY_PHASE")); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run(phase string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	snsClient := sns.NewFromConfig(cfg)
	queue := sqs.NewFromConfig(cfg)

	switch phase {
	case "applied", "updated":
		return verifyDelivery(ctx, queue, phase)
	case "destroyed":
		return verifyDestroyed(ctx, snsClient, queue)
	default:
		return fmt.Errorf("unexpected VERIFY_PHASE %q", phase)
	}
}

func verifyDelivery(ctx context.Context, queue *sqs.Client, phase string) error {
	url, err := queue.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
	if err != nil {
		return fmt.Errorf("get queue %s: %w", name, err)
	}
	wantBody := "SNS publish " + phase
	wantAttributes := map[string]messageAttribute{"batch": {Type: "String", Value: phase}}
	for {
		result, err := queue.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: url.QueueUrl, WaitTimeSeconds: 5, MaxNumberOfMessages: 1,
		})
		if err != nil {
			return fmt.Errorf("receive %s SNS delivery from %s: %w", phase, name, err)
		}
		for _, message := range result.Messages {
			var got notification
			if err := json.Unmarshal([]byte(aws.ToString(message.Body)), &got); err != nil {
				return fmt.Errorf("decode SNS notification: %w", err)
			}
			if got.Type != "Notification" || !strings.HasSuffix(got.TopicArn, ":"+name) ||
				got.Message != wantBody || !maps.Equal(wantAttributes, got.MessageAttributes) {
				return fmt.Errorf("unexpected %s SNS notification: %+v", phase, got)
			}
			_, err := queue.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl: url.QueueUrl, ReceiptHandle: message.ReceiptHandle,
			})
			if err != nil {
				return fmt.Errorf("delete verified SQS message: %w", err)
			}
			fmt.Printf("ok: received %q with batch attribute %q on %s\n", wantBody, phase, name)
			return nil
		}
	}
}

func verifyDestroyed(ctx context.Context, client *sns.Client, queue *sqs.Client) error {
	topics := sns.NewListTopicsPaginator(client, &sns.ListTopicsInput{})
	for topics.HasMorePages() {
		page, err := topics.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list SNS topics: %w", err)
		}
		for _, topic := range page.Topics {
			if strings.HasSuffix(aws.ToString(topic.TopicArn), ":"+name) {
				return fmt.Errorf("topic %s still exists", name)
			}
		}
	}
	_, err := queue.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
	var missingQueue *sqstypes.QueueDoesNotExist
	if !errors.As(err, &missingQueue) {
		return fmt.Errorf("expected queue %s to be gone, got %v", name, err)
	}
	fmt.Printf("ok: topic and queue %s are gone\n", name)
	return nil
}
