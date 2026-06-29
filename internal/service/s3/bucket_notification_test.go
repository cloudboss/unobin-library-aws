package s3

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBucketNotificationConfiguration(t *testing.T) {
	empty := ""
	prefix := "images/"
	suffix := ".jpg"
	id := "topic-id"
	r := &BucketNotification{
		Eventbridge: aws.Bool(true),
		LambdaFunction: new([]BucketNotificationLambdaFunction{
			{
				LambdaFunctionArn: aws.String("arn:aws:lambda:us-east-1:123:function:f"),
				Events:            []string{"", "s3:ObjectCreated:*"},
				FilterPrefix:      &empty,
				FilterSuffix:      &suffix,
			},
		}),
		Queue: new([]BucketNotificationQueue{
			{
				Id:           &empty,
				QueueArn:     "arn:aws:sqs:us-east-1:123:q",
				Events:       []string{"s3:ObjectRemoved:*", ""},
				FilterPrefix: &prefix,
			},
		}),
		Topic: new([]BucketNotificationTopic{
			{
				Id:       &id,
				TopicArn: "arn:aws:sns:us-east-1:123:t",
				Events:   []string{"", "s3:ReducedRedundancyLostObject", ""},
			},
		}),
	}
	prior := bucketNotificationEffectiveIDSource{
		prior: &BucketNotificationOutput{
			LambdaFunctionSummaries: []BucketNotificationLambdaSummary{
				{Id: "lambda-prior"},
			},
		},
	}

	desired, err := r.notificationConfiguration(prior)
	require.NoError(t, err)
	got := desired.notification
	require.NotNil(t, got.EventBridgeConfiguration)

	assert.Equal(t, []BucketNotificationLambdaSummary{
		{
			Id:                "lambda-prior",
			LambdaFunctionArn: "arn:aws:lambda:us-east-1:123:function:f",
			Events:            []string{"s3:ObjectCreated:*"},
			FilterSuffix:      suffix,
		},
	}, desired.lambdaFunctionSummaries)
	assert.Equal(t, []BucketNotificationTopicSummary{
		{
			Id:       id,
			TopicArn: "arn:aws:sns:us-east-1:123:t",
			Events:   []string{"s3:ReducedRedundancyLostObject"},
		},
	}, desired.topicSummaries)

	require.Len(t, got.LambdaFunctionConfigurations, 1)
	lambda := got.LambdaFunctionConfigurations[0]
	assert.Equal(t, "lambda-prior", aws.ToString(lambda.Id))
	assert.Equal(t, "arn:aws:lambda:us-east-1:123:function:f",
		aws.ToString(lambda.LambdaFunctionArn))
	assert.Equal(t, []s3types.Event{s3types.EventS3ObjectCreated}, lambda.Events)
	prefixValue, suffixValue := bucketNotificationFilterValues(lambda.Filter)
	assert.Empty(t, prefixValue)
	assert.Equal(t, suffix, suffixValue)

	require.Len(t, got.QueueConfigurations, 1)
	queue := got.QueueConfigurations[0]
	queueID := aws.ToString(queue.Id)
	assert.True(t, strings.HasPrefix(queueID, "tf-s3-queue-"))
	assert.Equal(t, []BucketNotificationQueueSummary{
		{
			Id:           queueID,
			QueueArn:     "arn:aws:sqs:us-east-1:123:q",
			Events:       []string{"s3:ObjectRemoved:*"},
			FilterPrefix: prefix,
		},
	}, desired.queueSummaries)
	assert.Equal(t, "arn:aws:sqs:us-east-1:123:q", aws.ToString(queue.QueueArn))
	assert.Equal(t, []s3types.Event{s3types.EventS3ObjectRemoved}, queue.Events)
	prefixValue, suffixValue = bucketNotificationFilterValues(queue.Filter)
	assert.Equal(t, prefix, prefixValue)
	assert.Empty(t, suffixValue)

	require.Len(t, got.TopicConfigurations, 1)
	topic := got.TopicConfigurations[0]
	assert.Equal(t, id, aws.ToString(topic.Id))
	assert.Equal(t, "arn:aws:sns:us-east-1:123:t", aws.ToString(topic.TopicArn))
	assert.Equal(t,
		[]s3types.Event{s3types.EventS3ReducedRedundancyLostObject}, topic.Events)
	assert.Nil(t, topic.Filter)
}

func TestBucketNotificationConfigurationUsesObservedEffectiveIDs(t *testing.T) {
	r := &BucketNotification{
		Queue: new([]BucketNotificationQueue{
			{
				QueueArn: "arn:aws:sqs:us-east-1:123:q",
				Events:   []string{"s3:ObjectCreated:*"},
			},
		}),
		Topic: new([]BucketNotificationTopic{
			{
				TopicArn: "arn:aws:sns:us-east-1:123:t",
				Events:   []string{"s3:ObjectRemoved:*"},
			},
		}),
	}
	source := bucketNotificationEffectiveIDSource{
		observed: &BucketNotificationOutput{
			QueueSummaries: []BucketNotificationQueueSummary{{Id: "queue-observed"}},
		},
		prior: &BucketNotificationOutput{
			QueueSummaries: []BucketNotificationQueueSummary{{Id: "queue-prior"}},
			TopicSummaries: []BucketNotificationTopicSummary{{Id: "topic-prior"}},
		},
	}

	desired, err := r.notificationConfiguration(source)

	require.NoError(t, err)
	got := desired.notification
	require.Len(t, got.QueueConfigurations, 1)
	assert.Equal(t, "queue-observed", aws.ToString(got.QueueConfigurations[0].Id))
	require.Len(t, got.TopicConfigurations, 1)
	assert.Equal(t, "topic-prior", aws.ToString(got.TopicConfigurations[0].Id))
}

func TestBucketNotificationNeedsPut(t *testing.T) {
	configured := "configured"
	prefix := "images/"
	tests := []struct {
		name  string
		item  *BucketNotification
		prior runtime.Prior[BucketNotification, *BucketNotificationOutput]
		want  bool
	}{
		{
			name: "input change writes full configuration",
			item: &BucketNotification{Eventbridge: aws.Bool(true)},
			prior: runtime.Prior[BucketNotification, *BucketNotificationOutput]{
				Inputs:   BucketNotification{Eventbridge: aws.Bool(false)},
				Observed: &BucketNotificationOutput{Eventbridge: true},
			},
			want: true,
		},
		{
			name: "eventbridge drift writes full configuration",
			item: &BucketNotification{Eventbridge: aws.Bool(true)},
			prior: runtime.Prior[BucketNotification, *BucketNotificationOutput]{
				Inputs:   BucketNotification{Eventbridge: aws.Bool(true)},
				Observed: &BucketNotificationOutput{Eventbridge: false},
			},
			want: true,
		},
		{
			name: "explicit destination id drift writes full configuration",
			item: &BucketNotification{
				LambdaFunction: new([]BucketNotificationLambdaFunction{{Id: &configured}}),
			},
			prior: runtime.Prior[BucketNotification, *BucketNotificationOutput]{
				Inputs: BucketNotification{
					LambdaFunction: new([]BucketNotificationLambdaFunction{{Id: &configured}}),
				},
				Observed: &BucketNotificationOutput{
					LambdaFunctionSummaries: []BucketNotificationLambdaSummary{
						{Id: "drifted"},
					},
				},
			},
			want: true,
		},
		{
			name: "omitted destination id drift accepts observed effective id",
			item: &BucketNotification{
				Queue: new([]BucketNotificationQueue{
					{QueueArn: "arn", Events: []string{"s3:ObjectCreated:*"}},
				}),
			},
			prior: runtime.Prior[BucketNotification, *BucketNotificationOutput]{
				Inputs: BucketNotification{
					Queue: new([]BucketNotificationQueue{
						{QueueArn: "arn", Events: []string{"s3:ObjectCreated:*"}},
					}),
				},
				Outputs: &BucketNotificationOutput{
					QueueSummaries: []BucketNotificationQueueSummary{{Id: "old"}},
				},
				Observed: &BucketNotificationOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:       "new",
							QueueArn: "arn",
							Events:   []string{"s3:ObjectCreated:*"},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "destination count drift writes full configuration",
			item: &BucketNotification{
				Queue: new([]BucketNotificationQueue{
					{QueueArn: "arn", Events: []string{"s3:ObjectCreated:*"}},
				}),
			},
			prior: runtime.Prior[BucketNotification, *BucketNotificationOutput]{
				Inputs: BucketNotification{
					Queue: new([]BucketNotificationQueue{
						{QueueArn: "arn", Events: []string{"s3:ObjectCreated:*"}},
					}),
				},
				Outputs: &BucketNotificationOutput{
					QueueSummaries: []BucketNotificationQueueSummary{{Id: "old"}},
				},
				Observed: &BucketNotificationOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{Id: "new"},
						{Id: "extra"},
					},
				},
			},
			want: true,
		},
		{
			name: "destination arn drift writes full configuration",
			item: &BucketNotification{
				Queue: new([]BucketNotificationQueue{
					{
						Id:       &configured,
						QueueArn: "arn:desired",
						Events:   []string{"s3:ObjectCreated:*"},
					},
				}),
			},
			prior: runtime.Prior[BucketNotification, *BucketNotificationOutput]{
				Inputs: BucketNotification{
					Queue: new([]BucketNotificationQueue{
						{
							Id:       &configured,
							QueueArn: "arn:desired",
							Events:   []string{"s3:ObjectCreated:*"},
						},
					}),
				},
				Observed: &BucketNotificationOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:       configured,
							QueueArn: "arn:drifted",
							Events:   []string{"s3:ObjectCreated:*"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "destination event drift writes full configuration",
			item: &BucketNotification{
				Queue: new([]BucketNotificationQueue{
					{
						Id:       &configured,
						QueueArn: "arn",
						Events:   []string{"s3:ObjectCreated:*"},
					},
				}),
			},
			prior: runtime.Prior[BucketNotification, *BucketNotificationOutput]{
				Inputs: BucketNotification{
					Queue: new([]BucketNotificationQueue{
						{
							Id:       &configured,
							QueueArn: "arn",
							Events:   []string{"s3:ObjectCreated:*"},
						},
					}),
				},
				Observed: &BucketNotificationOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:       configured,
							QueueArn: "arn",
							Events:   []string{"s3:ObjectRemoved:*"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "destination filter drift writes full configuration",
			item: &BucketNotification{
				Queue: new([]BucketNotificationQueue{
					{
						Id:           &configured,
						QueueArn:     "arn",
						Events:       []string{"s3:ObjectCreated:*"},
						FilterPrefix: &prefix,
					},
				}),
			},
			prior: runtime.Prior[BucketNotification, *BucketNotificationOutput]{
				Inputs: BucketNotification{
					Queue: new([]BucketNotificationQueue{
						{
							Id:           &configured,
							QueueArn:     "arn",
							Events:       []string{"s3:ObjectCreated:*"},
							FilterPrefix: &prefix,
						},
					}),
				},
				Observed: &BucketNotificationOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:           configured,
							QueueArn:     "arn",
							Events:       []string{"s3:ObjectCreated:*"},
							FilterPrefix: "logs/",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "normalized destination events skip put",
			item: &BucketNotification{
				Queue: new([]BucketNotificationQueue{
					{
						Id:       &configured,
						QueueArn: "arn",
						Events: []string{
							"s3:ObjectRemoved:*", "", "s3:ObjectCreated:*",
							"s3:ObjectCreated:*",
						},
					},
				}),
			},
			prior: runtime.Prior[BucketNotification, *BucketNotificationOutput]{
				Inputs: BucketNotification{
					Queue: new([]BucketNotificationQueue{
						{
							Id:       &configured,
							QueueArn: "arn",
							Events: []string{
								"s3:ObjectRemoved:*", "", "s3:ObjectCreated:*",
								"s3:ObjectCreated:*",
							},
						},
					}),
				},
				Observed: &BucketNotificationOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:       configured,
							QueueArn: "arn",
							Events: []string{
								"s3:ObjectCreated:*", "s3:ObjectRemoved:*",
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "matching observed state skips put",
			item: &BucketNotification{
				Eventbridge: aws.Bool(true),
				Topic:       new([]BucketNotificationTopic{{Id: &configured}}),
			},
			prior: runtime.Prior[BucketNotification, *BucketNotificationOutput]{
				Inputs: BucketNotification{
					Eventbridge: aws.Bool(true),
					Topic:       new([]BucketNotificationTopic{{Id: &configured}}),
				},
				Observed: &BucketNotificationOutput{
					Eventbridge: true,
					TopicSummaries: []BucketNotificationTopicSummary{
						{Id: configured, Events: []string{}},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := bucketNotificationEffectiveIDSource{
				observed: tt.prior.Observed,
				prior:    tt.prior.Outputs,
			}
			desired, err := tt.item.notificationConfiguration(ids)
			require.NoError(t, err)
			assert.Equal(t, tt.want, tt.item.needsPut(tt.prior, desired))
		})
	}
}

func TestBucketNotificationEventsOmitsEmptyStrings(t *testing.T) {
	tests := []struct {
		name   string
		events []string
		want   []s3types.Event
	}{
		{
			name:   "no configured events returns nil",
			events: nil,
			want:   nil,
		},
		{
			name:   "nonempty events keep order",
			events: []string{"s3:ObjectCreated:*", "s3:ObjectRemoved:*"},
			want: []s3types.Event{
				s3types.EventS3ObjectCreated,
				s3types.EventS3ObjectRemoved,
			},
		},
		{
			name:   "empty strings are omitted",
			events: []string{"", "s3:ObjectCreated:*", ""},
			want:   []s3types.Event{s3types.EventS3ObjectCreated},
		},
		{
			name:   "all empty strings returns an empty list",
			events: []string{"", ""},
			want:   []s3types.Event{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bucketNotificationEvents(tt.events))
		})
	}
}

func TestBucketNotificationOutputSummaries(t *testing.T) {
	prefix := "images/"
	suffix := ".jpg"
	resp := &s3.GetBucketNotificationConfigurationOutput{
		EventBridgeConfiguration: &s3types.EventBridgeConfiguration{},
		LambdaFunctionConfigurations: []s3types.LambdaFunctionConfiguration{
			{
				Id:                aws.String("lambda-id"),
				LambdaFunctionArn: aws.String("arn:lambda"),
				Events: []s3types.Event{
					s3types.EventS3ObjectRemoved,
					"",
					s3types.EventS3ObjectCreated,
					s3types.EventS3ObjectCreated,
				},
				Filter: bucketNotificationFilter(&prefix, &suffix),
			},
		},
		QueueConfigurations: []s3types.QueueConfiguration{
			{
				Id:       aws.String("queue-id"),
				QueueArn: aws.String("arn:queue"),
				Events:   []s3types.Event{s3types.EventS3ObjectCreated},
				Filter:   bucketNotificationFilter(&prefix, nil),
			},
		},
		TopicConfigurations: []s3types.TopicConfiguration{
			{
				Id:       aws.String("topic-id"),
				TopicArn: aws.String("arn:topic"),
				Events:   []s3types.Event{s3types.EventS3ObjectRemoved},
				Filter:   bucketNotificationFilter(nil, &suffix),
			},
		},
	}

	got := bucketNotificationOutput("bucket", resp)

	assert.Equal(t, &BucketNotificationOutput{
		Bucket:      "bucket",
		Eventbridge: true,
		LambdaFunctionSummaries: []BucketNotificationLambdaSummary{
			{
				Id:                "lambda-id",
				LambdaFunctionArn: "arn:lambda",
				Events:            []string{"s3:ObjectCreated:*", "s3:ObjectRemoved:*"},
				FilterPrefix:      prefix,
				FilterSuffix:      suffix,
			},
		},
		QueueSummaries: []BucketNotificationQueueSummary{
			{
				Id:           "queue-id",
				QueueArn:     "arn:queue",
				Events:       []string{"s3:ObjectCreated:*"},
				FilterPrefix: prefix,
			},
		},
		TopicSummaries: []BucketNotificationTopicSummary{
			{
				Id:           "topic-id",
				TopicArn:     "arn:topic",
				Events:       []string{"s3:ObjectRemoved:*"},
				FilterSuffix: suffix,
			},
		},
	}, got)
}

func TestBucketNotificationOutputEmptyConfiguration(t *testing.T) {
	resp := &s3.GetBucketNotificationConfigurationOutput{}
	resp.ResultMetadata.Set("request-id", "id")

	got := bucketNotificationOutput("bucket", resp)

	assert.Equal(t, &BucketNotificationOutput{
		Bucket:                  "bucket",
		Eventbridge:             false,
		LambdaFunctionSummaries: []BucketNotificationLambdaSummary{},
		QueueSummaries:          []BucketNotificationQueueSummary{},
		TopicSummaries:          []BucketNotificationTopicSummary{},
	}, got)
}

func TestBucketNotificationMigrateV1Output(t *testing.T) {
	got, err := (&BucketNotification{}).Migrate(1, runtime.MigrationState{
		Inputs: map[string]any{"bucket": "bucket"},
		Outputs: map[string]any{
			"bucket":          "bucket",
			"lambda-function": []any{map[string]any{"id": "lambda-id"}},
			"queue":           []any{map[string]any{"id": "queue-id"}},
			"topic":           []any{map[string]any{"id": "topic-id"}},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, runtime.MigrationState{
		Inputs: map[string]any{"bucket": "bucket"},
		Outputs: map[string]any{
			"bucket":      "bucket",
			"eventbridge": false,
			"lambda-function-observed-summaries": []BucketNotificationLambdaSummary{
				{Id: "lambda-id"},
			},
			"queue-observed-summaries": []BucketNotificationQueueSummary{
				{Id: "queue-id"},
			},
			"topic-observed-summaries": []BucketNotificationTopicSummary{
				{Id: "topic-id"},
			},
		},
	}, got)
}

func TestBucketNotificationMigrateV2Output(t *testing.T) {
	got, err := (&BucketNotification{}).Migrate(2, runtime.MigrationState{
		Inputs: map[string]any{"bucket": "bucket"},
		Outputs: map[string]any{
			"bucket":                        "bucket",
			"eventbridge":                   true,
			"lambda-function-effective-ids": []any{"lambda-id"},
			"queue-effective-ids":           []any{"queue-id"},
			"topic-effective-ids":           []any{"topic-id"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, runtime.MigrationState{
		Inputs: map[string]any{"bucket": "bucket"},
		Outputs: map[string]any{
			"bucket":      "bucket",
			"eventbridge": true,
			"lambda-function-observed-summaries": []BucketNotificationLambdaSummary{
				{Id: "lambda-id"},
			},
			"queue-observed-summaries": []BucketNotificationQueueSummary{
				{Id: "queue-id"},
			},
			"topic-observed-summaries": []BucketNotificationTopicSummary{
				{Id: "topic-id"},
			},
		},
	}, got)
}

func TestBucketNotificationFilterValues(t *testing.T) {
	prefix := "logs/"
	suffix := ".gz"
	ignored := "ignored"
	filter := &s3types.NotificationConfigurationFilter{
		Key: &s3types.S3KeyFilter{
			FilterRules: []s3types.FilterRule{
				{Name: s3types.FilterRuleName("PREFIX"), Value: &prefix},
				{Name: s3types.FilterRuleName("unknown"), Value: &ignored},
				{Name: s3types.FilterRuleName("Suffix"), Value: &suffix},
			},
		},
	}

	gotPrefix, gotSuffix := bucketNotificationFilterValues(filter)

	assert.Equal(t, prefix, gotPrefix)
	assert.Equal(t, suffix, gotSuffix)
}

func TestBucketNotificationZero(t *testing.T) {
	assert.True(t, bucketNotificationZero(nil))
	assert.True(t, bucketNotificationZero(&s3.GetBucketNotificationConfigurationOutput{}))

	resp := &s3.GetBucketNotificationConfigurationOutput{}
	resp.ResultMetadata.Set("request-id", "id")
	assert.False(t, bucketNotificationZero(resp))

	assert.False(t, bucketNotificationZero(&s3.GetBucketNotificationConfigurationOutput{
		EventBridgeConfiguration: &s3types.EventBridgeConfiguration{},
	}))
	assert.False(t, bucketNotificationZero(&s3.GetBucketNotificationConfigurationOutput{
		QueueConfigurations: []s3types.QueueConfiguration{{Id: aws.String("id")}},
	}))
}

func TestBucketNotificationDeleteRetryable(t *testing.T) {
	assert.True(t, bucketNotificationDeleteRetryable(&smithy.GenericAPIError{
		Code:    "OperationAborted",
		Message: "A conflicting conditional operation is currently in progress.",
	}))
	assert.False(t, bucketNotificationDeleteRetryable(&smithy.GenericAPIError{
		Code:    "NoSuchBucket",
		Message: "missing",
	}))
}

func TestBucketNotificationDirectoryBucketUnsupported(t *testing.T) {
	err := &smithy.GenericAPIError{
		Code:    "InvalidArgument",
		Message: "NotificationConfiguration is not valid, expected CreateBucketConfiguration",
	}

	assert.True(t, isBucketNotificationDirectoryBucketUnsupported(err))
	assert.False(t, isBucketNotificationDirectoryBucketUnsupported(&smithy.GenericAPIError{
		Code:    "InvalidArgument",
		Message: "different",
	}))
}

func TestDirectoryBucketAWSConfigUsesRegionalEndpoint(t *testing.T) {
	got := directoryBucketAWSConfig(aws.Config{Region: awsGlobalRegion})
	assert.Equal(t, usEast1Region, got.Region)

	got = directoryBucketAWSConfig(aws.Config{Region: "us-west-2"})
	assert.Equal(t, "us-west-2", got.Region)
}

func TestBucketNotificationClientKind(t *testing.T) {
	assert.Equal(t, bucketNotificationClientS3Express,
		bucketNotificationClientKindFor("bucket--usw2-az1--x-s3"))
	assert.Equal(t, bucketNotificationClientS3Express,
		bucketNotificationClientKindFor("my--s3--bucket--abcd-ab1--x-s3"))
	assert.Equal(t, bucketNotificationClientS3,
		bucketNotificationClientKindFor("bucket--x-s3"))
	assert.Equal(t, bucketNotificationClientS3,
		bucketNotificationClientKindFor("bucket"))
}

func TestDirectoryBucketName(t *testing.T) {
	assert.True(t, isDirectoryBucketName("bucket--usw2-az1--x-s3"))
	assert.True(t, isDirectoryBucketName("my--s3--bucket--abcd-ab1--x-s3"))
	assert.False(t, isDirectoryBucketName("--usw2-az1--x-s3"))
	assert.False(t, isDirectoryBucketName("bucket--USW2-AZ1--x-s3"))
	assert.False(t, isDirectoryBucketName("bucket--x-s3"))
	assert.False(t, isDirectoryBucketName("bucket"))
}
