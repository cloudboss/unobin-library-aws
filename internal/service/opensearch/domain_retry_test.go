package opensearch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainPropagationErrorClassification(t *testing.T) {
	validationFragments := []string{
		"enable a service-linked role to give Amazon ES permissions",
		"Domain is still being deleted",
		"Amazon OpenSearch Service must be allowed to use the passed role",
		"The passed role has not propagated yet",
		"Authentication error",
		"Unauthorized Operation: OpenSearch Service must be authorised to describe",
		"The passed role must authorize Amazon OpenSearch Service to describe",
		"A change/update is in progress. Please wait for it to complete before " +
			"requesting another change.",
		"The Resource Access Policy specified for the CloudWatch Logs log group",
	}
	for _, fragment := range validationFragments {
		t.Run(fragment, func(t *testing.T) {
			err := &awstypes.ValidationException{Message: aws.String("prefix " + fragment)}
			assert.True(t, isDomainPropagationError(err))
		})
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "invalid type policy",
			err: &awstypes.InvalidTypeException{
				Message: aws.String("Error setting policy while role propagates"),
			},
			want: true,
		},
		{
			name: "invalid type near match",
			err: &awstypes.InvalidTypeException{
				Message: aws.String("error setting policy"),
			},
		},
		{
			name: "wrong type",
			err:  &awstypes.InternalException{Message: aws.String("Domain is still being deleted")},
		},
		{name: "plain error", err: errors.New("Domain is still being deleted")},
		{name: "nil"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, isDomainPropagationError(test.err))
		})
	}
}

func TestRetryDomainOperationEventuallySucceeds(t *testing.T) {
	clock := newRecordingDomainClock()
	attempts := 0
	err := retryDomainOperation(
		context.Background(), clock, 30*time.Minute,
		func(context.Context) error {
			attempts++
			if attempts < 4 {
				return &awstypes.ValidationException{
					Message: aws.String("Domain is still being deleted"),
				}
			}
			return nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 4, attempts)
	assert.Equal(t, []time.Duration{
		500 * time.Millisecond,
		time.Second,
		2 * time.Second,
	}, clock.sleeps)
}

func TestRetryDomainOperationTimesOut(t *testing.T) {
	clock := newRecordingDomainClock()
	err := retryDomainOperation(
		context.Background(), clock, 500*time.Millisecond,
		func(context.Context) error {
			return &awstypes.ValidationException{
				Message: aws.String("Domain is still being deleted"),
			}
		},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "timed out after 500ms")
}
