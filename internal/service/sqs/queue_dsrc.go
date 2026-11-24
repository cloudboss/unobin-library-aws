package sqs

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	sqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/cloudboss/unobin/pkg/constraint"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

type QueueDataSource struct {
	QueueName string `ub:"queue-name"`
}

type QueueDataSourceOutput struct {
	QueueURL string            `ub:"queue-url"`
	QueueARN string            `ub:"queue-arn"`
	Tags     map[string]string `ub:"tags"`
}

func (r QueueDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Expr("input.name != null",
			constraint.Bind("name", r.QueueName))).Message("queue-name is required"),
	}
}

type queueLookupAPI interface {
	GetQueueUrl(context.Context, *sqs.GetQueueUrlInput,
		...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error)
	GetQueueAttributes(context.Context, *sqs.GetQueueAttributesInput,
		...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	ListQueueTags(context.Context, *sqs.ListQueueTagsInput,
		...func(*sqs.Options)) (*sqs.ListQueueTagsOutput, error)
}

func (r *QueueDataSource) Read(
	ctx context.Context, cfg *awsCfg,
) (*QueueDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, client.Options().Region)
}

func (r *QueueDataSource) read(
	ctx context.Context, client queueLookupAPI, region string,
) (*QueueDataSourceOutput, error) {
	url, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(r.QueueName),
	})
	if err != nil {
		return nil, fmt.Errorf("get queue URL %q: %w", r.QueueName, err)
	}
	if url == nil || url.QueueUrl == nil {
		return nil, fmt.Errorf("get queue URL %q: empty response", r.QueueName)
	}
	attributes, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       url.QueueUrl,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return nil, fmt.Errorf("get queue ARN %q: %w", *url.QueueUrl, err)
	}
	if attributes == nil || attributes.Attributes["QueueArn"] == "" {
		return nil, fmt.Errorf("get queue ARN %q: empty response", *url.QueueUrl)
	}
	tags := map[string]string{}
	if *url.QueueUrl != "" {
		response, err := client.ListQueueTags(ctx, &sqs.ListQueueTagsInput{
			QueueUrl: url.QueueUrl,
		})
		if err != nil && !partition.UnsupportedOperation(region, err) {
			return nil, fmt.Errorf("list queue tags %q: %w", *url.QueueUrl, err)
		}
		if err == nil {
			if response == nil {
				return nil, fmt.Errorf("list queue tags %q: empty response", *url.QueueUrl)
			}
			for key, value := range response.Tags {
				if !strings.HasPrefix(key, "aws:") {
					tags[key] = value
				}
			}
		}
	}
	return &QueueDataSourceOutput{
		QueueURL: *url.QueueUrl, QueueARN: attributes.Attributes["QueueArn"], Tags: tags,
	}, nil
}
