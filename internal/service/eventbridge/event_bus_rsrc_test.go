package eventbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventBusValidate(t *testing.T) {
	validPartner := "aws.partner/example.com/source/account"
	queueArn := "arn:aws:sqs:us-east-1:123456789012:queue"
	longName := string(make([]byte, 257))

	cases := []struct {
		name    string
		mutate  func(*EventBusResource)
		wantErr string
	}{
		{name: "valid"},
		{
			name:    "default name",
			mutate:  func(r *EventBusResource) { r.Name = defaultEventBusName },
			wantErr: `name cannot be "default"`,
		},
		{
			name:    "long name",
			mutate:  func(r *EventBusResource) { r.Name = longName },
			wantErr: "name must be between 1 and 256 characters",
		},
		{
			name:    "valid partner source",
			mutate:  func(r *EventBusResource) { r.EventSourceName = &validPartner },
			wantErr: "",
		},
		{
			name: "invalid partner source",
			mutate: func(r *EventBusResource) {
				v := "aws.partner/only-one-segment"
				r.EventSourceName = &v
			},
			wantErr: "event-source-name",
		},
		{
			name: "empty kms key",
			mutate: func(r *EventBusResource) {
				v := ""
				r.KmsKeyIdentifier = &v
			},
			wantErr: "kms-key-identifier must be between 1 and 2048 characters",
		},
		{
			name: "valid dead-letter arn",
			mutate: func(r *EventBusResource) {
				r.DeadLetterConfig = &EventBusDeadLetterConfig{Arn: &queueArn}
			},
		},
		{
			name: "invalid dead-letter arn",
			mutate: func(r *EventBusResource) {
				v := "not-an-arn"
				r.DeadLetterConfig = &EventBusDeadLetterConfig{Arn: &v}
			},
			wantErr: "dead-letter-config.arn must be a valid ARN",
		},
		{
			name: "invalid dead-letter arn partition",
			mutate: func(r *EventBusResource) {
				v := "arn:aws123:sqs:us-east-1:123456789012:queue"
				r.DeadLetterConfig = &EventBusDeadLetterConfig{Arn: &v}
			},
			wantErr: "dead-letter-config.arn must be a valid ARN",
		},
		{
			name: "invalid dead-letter arn region",
			mutate: func(r *EventBusResource) {
				v := "arn:aws:sqs:useast1:123456789012:queue"
				r.DeadLetterConfig = &EventBusDeadLetterConfig{Arn: &v}
			},
			wantErr: "dead-letter-config.arn must be a valid ARN",
		},
		{
			name: "invalid dead-letter arn account",
			mutate: func(r *EventBusResource) {
				v := "arn:aws:sqs:us-east-1:123:queue"
				r.DeadLetterConfig = &EventBusDeadLetterConfig{Arn: &v}
			},
			wantErr: "dead-letter-config.arn must be a valid ARN",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := EventBusResource{Name: "unobin-test-bus"}
			if tt.mutate != nil {
				tt.mutate(&r)
			}

			err := r.validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestEventBusUserTagsDropsSystemTags(t *testing.T) {
	got := eventBusUserTags(map[string]string{
		"env":       "test",
		"aws:owner": "system",
	})

	assert.Equal(t, map[string]string{"env": "test"}, got)
}

func TestEventbridgeLimitRetryer(t *testing.T) {
	retryer := eventbridgeLimitRetryer{Retryer: eventbridgeStubRetryer{retryable: true}}

	limitErr := &eventbridgetypes.LimitExceededException{
		Message: aws.String("The requested resource exceeds the maximum number allowed"),
	}
	assert.False(t, retryer.IsErrorRetryable(limitErr))

	throttleErr := &eventbridgetypes.LimitExceededException{Message: aws.String("Rate exceeded")}
	assert.True(t, retryer.IsErrorRetryable(throttleErr))

	assert.True(t, retryer.IsErrorRetryable(errors.New("delegate")))
}

type eventbridgeStubRetryer struct {
	retryable bool
}

func (s eventbridgeStubRetryer) IsErrorRetryable(error) bool { return s.retryable }

func (s eventbridgeStubRetryer) MaxAttempts() int { return 3 }

func (s eventbridgeStubRetryer) RetryDelay(int, error) (time.Duration, error) { return 0, nil }

func (s eventbridgeStubRetryer) GetRetryToken(
	context.Context, error,
) (func(error) error, error) {
	return func(error) error { return nil }, nil
}

func (s eventbridgeStubRetryer) GetInitialToken() func(error) error {
	return func(error) error { return nil }
}
