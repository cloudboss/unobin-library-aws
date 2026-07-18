package kinesis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/stretchr/testify/assert"
)

func TestStreamRetryerAddsOnlyTransientLimitMessages(t *testing.T) {
	retryer := streamRetryer{Retryer: stubStreamRetryer{retryable: false}}
	for _, message := range []string{
		"Only 5 streams can simultaneously be in CREATING or DELETING states",
		"Rate exceeded for stream events",
	} {
		err := &awstypes.LimitExceededException{Message: aws.String(message)}
		assert.True(t, retryer.IsErrorRetryable(err), message)
	}
	for _, message := range []string{
		"Only 5 streams can be in CREATING state",
		"Rate exceeded",
	} {
		err := &awstypes.LimitExceededException{Message: aws.String(message)}
		assert.False(t, retryer.IsErrorRetryable(err), message)
	}
	assert.False(t, retryer.IsErrorRetryable(errors.New("delegate")))

	delegating := streamRetryer{Retryer: stubStreamRetryer{retryable: true}}
	assert.True(t, delegating.IsErrorRetryable(errors.New("delegate")))
}

type stubStreamRetryer struct {
	retryable bool
}

func (s stubStreamRetryer) IsErrorRetryable(error) bool { return s.retryable }

func (s stubStreamRetryer) MaxAttempts() int { return 3 }

func (s stubStreamRetryer) RetryDelay(int, error) (time.Duration, error) { return 0, nil }

func (s stubStreamRetryer) GetRetryToken(
	context.Context,
	error,
) (func(error) error, error) {
	return func(error) error { return nil }, nil
}

func (s stubStreamRetryer) GetInitialToken() func(error) error {
	return func(error) error { return nil }
}
