package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

const (
	name   = "unobin-it-put-events"
	source = "unobin.integration.put-events"
)

type event struct {
	Source     string `json:"source"`
	DetailType string `json:"detail-type"`
	Detail     struct {
		Batch    string `json:"batch"`
		Position int    `json:"position"`
	} `json:"detail"`
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
		return fmt.Errorf("load aws config: %w", err)
	}
	queue := sqs.NewFromConfig(cfg)
	bus := eventbridge.NewFromConfig(cfg)

	switch phase {
	case "applied", "updated":
		return verifyDelivery(ctx, queue, phase)
	case "destroyed":
		return verifyDestroyed(ctx, bus, queue)
	default:
		return fmt.Errorf("unexpected VERIFY_PHASE %q", phase)
	}
}

func verifyDelivery(ctx context.Context, queue *sqs.Client, phase string) error {
	url, err := queue.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
	if err != nil {
		return fmt.Errorf("get queue %s: %w", name, err)
	}
	want := map[string]int{"first": 1, "second": 2}
	got := make(map[string]int, len(want))
	for !maps.Equal(got, want) {
		result, err := queue.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: url.QueueUrl, MaxNumberOfMessages: 10, WaitTimeSeconds: 5,
		})
		if err != nil {
			return fmt.Errorf("receive %s events, saw %v: %w", phase, got, err)
		}
		for _, message := range result.Messages {
			var entry event
			if err := json.Unmarshal([]byte(aws.ToString(message.Body)), &entry); err != nil {
				return fmt.Errorf("decode SQS event: %w", err)
			}
			if entry.Detail.Batch != phase {
				continue
			}
			position, known := want[entry.DetailType]
			if !known || position != entry.Detail.Position || entry.Source != source {
				return fmt.Errorf("unexpected %s batch event: %+v", phase, entry)
			}
			got[entry.DetailType] = entry.Detail.Position
		}
	}
	fmt.Printf("ok: received both %s events through %s\n", phase, name)
	return nil
}

func verifyDestroyed(ctx context.Context, bus *eventbridge.Client, queue *sqs.Client) error {
	_, err := bus.DescribeEventBus(ctx, &eventbridge.DescribeEventBusInput{Name: aws.String(name)})
	var missingBus *eventtypes.ResourceNotFoundException
	if !errors.As(err, &missingBus) {
		return fmt.Errorf("expected event bus %s to be gone, got %v", name, err)
	}
	_, err = queue.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
	var missingQueue *sqstypes.QueueDoesNotExist
	if !errors.As(err, &missingQueue) {
		return fmt.Errorf("expected queue %s to be gone, got %v", name, err)
	}
	fmt.Printf("ok: event bus and queue %s are gone\n", name)
	return nil
}
