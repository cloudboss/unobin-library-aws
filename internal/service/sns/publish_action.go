package sns

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/hashicorp/aws-sdk-go-base/v2/endpoints"
)

type PublishAction struct {
	TopicARN          string                     `ub:"topic-arn"`
	Message           string                     `ub:"message"`
	Subject           *string                    `ub:"subject"`
	MessageStructure  *string                    `ub:"message-structure"`
	MessageAttributes *[]PublishMessageAttribute `ub:"message-attributes"`
	Region            *string                    `ub:"region"`
}

type PublishMessageAttribute struct {
	Name        string `ub:"name"`
	DataType    string `ub:"data-type"`
	StringValue string `ub:"string-value"`
}

type PublishActionOutput struct{}

type publishClient interface {
	Publish(context.Context, *sns.PublishInput, ...func(*sns.Options)) (*sns.PublishOutput, error)
}

func (r PublishAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.TopicARN, nil)).
			Message("topic-arn is required"),
		constraint.Must(constraint.NotEquals(r.Message, nil)).
			Message("message is required"),
		constraint.When(constraint.NotEquals(r.MessageStructure, nil)).
			Require(constraint.Equals(r.MessageStructure, "json")).
			Message("message-structure must be json"),
		constraint.ForEach(
			r.MessageAttributes,
			func(attr PublishMessageAttribute) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.NotEquals(attr.Name, nil)).
						Message("name is required"),
					constraint.Must(constraint.OneOf(attr.DataType, "String", "Number", "Binary")).
						Message("data-type must be String, Number, or Binary"),
					constraint.Must(constraint.NotEquals(attr.StringValue, nil)).
						Message("string-value is required"),
				}
			},
		),
	}
}

func (r *PublishAction) Run(ctx context.Context, cfg *awsCfg) (*PublishActionOutput, error) {
	sdkConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if region := aws.ToString(r.Region); region != "" {
		configuredPartition := publishRegionPartition(sdkConfig.Region)
		if partition := publishRegionPartition(region); partition != configuredPartition {
			return nil, fmt.Errorf("region %q belongs to partition %q, expected %q",
				region, partition, configuredPartition)
		}
		sdkConfig.Region = region
	}
	return r.run(ctx, sns.NewFromConfig(sdkConfig))
}

func (r *PublishAction) run(
	ctx context.Context, client publishClient,
) (*PublishActionOutput, error) {
	if _, err := client.Publish(ctx, r.prepareRequest()); err != nil {
		return nil, fmt.Errorf("publish SNS message: %w", err)
	}
	return &PublishActionOutput{}, nil
}

func (r *PublishAction) prepareRequest() *sns.PublishInput {
	request := &sns.PublishInput{
		TopicArn:         aws.String(r.TopicARN),
		Message:          aws.String(r.Message),
		MessageStructure: r.MessageStructure,
		Subject:          r.Subject,
	}
	if r.MessageAttributes != nil && len(*r.MessageAttributes) > 0 {
		request.MessageAttributes = make(map[string]types.MessageAttributeValue,
			len(*r.MessageAttributes))
		for _, attr := range *r.MessageAttributes {
			request.MessageAttributes[attr.Name] = types.MessageAttributeValue{
				DataType:    aws.String(attr.DataType),
				StringValue: aws.String(attr.StringValue),
			}
		}
	}
	return request
}

func publishRegionPartition(region string) string {
	partition, ok := endpoints.PartitionForRegion(endpoints.DefaultPartitions(), region)
	if ok {
		return partition.ID()
	}
	return "aws"
}
