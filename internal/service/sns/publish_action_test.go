package sns

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleTopic = "arn:aws:sns:us-east-1:012345678901:topic"

type publishFunc func(context.Context, *sns.PublishInput) (*sns.PublishOutput, error)

func (fn publishFunc) Publish(
	ctx context.Context, req *sns.PublishInput, _ ...func(*sns.Options),
) (*sns.PublishOutput, error) {
	return fn(ctx, req)
}

func TestPublishActionRequest(t *testing.T) {
	for _, tt := range []struct {
		name   string
		action PublishAction
		want   *sns.PublishInput
	}{
		{
			name:   "omitted options and empty required strings",
			action: PublishAction{},
			want:   &sns.PublishInput{TopicArn: aws.String(""), Message: aws.String("")},
		},
		{
			name: "empty list leaves attributes unset",
			action: PublishAction{
				TopicARN: sampleTopic, Message: "  message\n",
				MessageAttributes: &[]PublishMessageAttribute{},
			},
			want: &sns.PublishInput{TopicArn: aws.String(sampleTopic), Message: aws.String("  message\n")},
		},
		{
			name: "JSON mode, empty subject and last attribute wins",
			action: PublishAction{
				TopicARN: sampleTopic, Message: "{malformed", Subject: aws.String(""),
				MessageStructure: aws.String("json"),
				MessageAttributes: &[]PublishMessageAttribute{
					{Name: "label", DataType: "String", StringValue: "first"},
					{Name: "amount", DataType: "Number", StringValue: "001.20"},
					{Name: "blob", DataType: "Binary", StringValue: "YWJj"},
					{Name: "label", DataType: "String", StringValue: "last"},
				},
			},
			want: &sns.PublishInput{
				TopicArn: aws.String(sampleTopic), Message: aws.String("{malformed"),
				Subject: aws.String(""), MessageStructure: aws.String("json"),
				MessageAttributes: map[string]types.MessageAttributeValue{
					"amount": {DataType: aws.String("Number"), StringValue: aws.String("001.20")},
					"blob":   {DataType: aws.String("Binary"), StringValue: aws.String("YWJj")},
					"label":  {DataType: aws.String("String"), StringValue: aws.String("last")},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var requests []*sns.PublishInput
			client := publishFunc(func(gotCtx context.Context, req *sns.PublishInput) (
				*sns.PublishOutput, error,
			) {
				assert.Same(t, ctx, gotCtx)
				requests = append(requests, req)
				return &sns.PublishOutput{
					MessageId: aws.String("message-id"), SequenceNumber: aws.String("51"),
				}, nil
			})
			out, err := tt.action.run(ctx, client)
			require.NoError(t, err)
			assert.Equal(t, &PublishActionOutput{}, out)
			assert.Equal(t, []*sns.PublishInput{tt.want}, requests)
		})
	}
}

func TestPublishActionAPIFailures(t *testing.T) {
	for _, failure := range []error{
		&types.AuthorizationErrorException{Message: aws.String("denied")},
		&types.NotFoundException{Message: aws.String("topic not found")},
		&types.InvalidParameterException{Message: aws.String("bad message")},
		&smithy.GenericAPIError{Code: "Unexpected", Message: "failed"},
	} {
		t.Run(fmt.Sprintf("%T", failure), func(t *testing.T) {
			calls := 0
			client := publishFunc(func(context.Context, *sns.PublishInput) (*sns.PublishOutput, error) {
				calls++
				return nil, failure
			})
			action := PublishAction{TopicARN: sampleTopic, Message: "message"}
			out, err := action.run(t.Context(), client)
			require.ErrorIs(t, err, failure)
			assert.ErrorContains(t, err, "publish SNS message")
			assert.Nil(t, out)
			assert.Equal(t, 1, calls)
		})
	}
}

func publishTestConfig(t *testing.T, endpoint string, attempts int64) *awsCfg {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	return &awsCfg{
		EndpointURL: aws.String(endpoint), Region: aws.String("us-east-1"),
		MaxAttempts: &attempts,
	}
}

func TestPublishActionSDKRetries(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, `<ErrorResponse><Error><Code>ServiceUnavailable</Code>
<Message>try again</Message></Error></ErrorResponse>`)
	}))
	t.Cleanup(server.Close)

	action := PublishAction{TopicARN: sampleTopic, Message: "message"}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	out, err := action.Run(ctx, publishTestConfig(t, server.URL, 2))
	var apiErr smithy.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "ServiceUnavailable", apiErr.ErrorCode())
	assert.ErrorContains(t, err, "publish SNS message")
	assert.Nil(t, out)
	assert.EqualValues(t, 2, requests.Load())
}

func TestPublishActionSDKCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, err := io.Copy(io.Discard, r.Body)
		assert.NoError(t, err)
		cancel()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("SNS HTTP request context stayed active after cancellation")
		}
	}))
	t.Cleanup(server.Close)
	action := PublishAction{TopicARN: sampleTopic, Message: "message"}
	out, err := action.Run(ctx, publishTestConfig(t, server.URL, 2))
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorContains(t, err, "publish SNS message")
	assert.Nil(t, out)
	assert.EqualValues(t, 1, requests.Load())
}
